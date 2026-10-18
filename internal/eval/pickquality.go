package eval

import (
	"cmnet/internal/cache"
	"cmnet/internal/draft"
	"cmnet/internal/features"
	"cmnet/internal/matrices"
	"cmnet/internal/model"
	"cmnet/internal/recommender"
)

// PickQualityMetrics scores the question the PRODUCT asks, which is not the
// question win-prob AUC answers.
//
// Win-prob AUC asks "can the model separate matches Radiant won from matches
// Radiant lost". The advisor asks something else: standing in ONE draft state,
// does it put the better candidate above the worse one? Those come apart.
// Comparing candidates inside a single state cancels much of a model's systematic
// bias — both estimates are wrong in the same direction and only the difference
// is used — so a model can be mediocre at calling matches and still order picks
// usefully.
//
// WHAT THIS IS: a POLICY-ALIGNMENT surrogate. It measures whether the teams whose
// real choices the ranker agreed with went on to win. It is observational.
// Nobody ever played the alternative pick, so this cannot show that a hero the
// ranker preferred WOULD have won the game; team skill, captain habits and the
// meta all remain confounded with the ordering. Read it as "the ordering lines up
// with what winning teams did", never as "the ordering is causally correct".
//
// The popularity control is what stops it being circular. Winning teams also pick
// popular heroes, so an ordering that merely reproduces the meta scores above
// 0.5 on its own. VsPopularity is the paired difference against exactly that
// ranker, on the same matches, and it is the number worth reading.
type PickQualityMetrics struct {
	Name    string // ranker being scored
	Group   string // population the matches came from
	N       int    // matches scored
	Actions int    // individual picks ranked
	// AUC of "the team whose picks this ranker liked better won", over matches.
	AUC CI
	// VsPopularity is the paired difference against the popularity ranker. Zero
	// for the popularity ranker itself.
	VsPopularity CI
	// MeanNormRank is where real picks land on average, 0 = the ranker's top
	// choice, 1 = its last. Low means the ranker agrees with what captains do,
	// which is imitation rather than quality — read it beside AUC, not alone.
	MeanNormRank float64
}

// popularityRankerName marks the frequency control every other ranker is
// measured against.
const popularityRankerName = "popularity|step"

// winProbRankerName is the reference every richer variant is compared against: the
// plain one-ply win-prob score, with no search, matrices or role penalty on top.
const winProbRankerName = "win-prob/1ply"

// pickRanker scores every available hero for the team currently picking. Higher
// is better FOR THAT TEAM, whichever side it is on.
type pickRanker struct {
	name  string
	score func(st pickState, taken []bool) []float64
}

// pickState is one decision point in a replayed draft.
type pickState struct {
	radiant, dire []int
	actor         int // 0 radiant, 1 dire — the team on the clock
	ord           int
}

// mine returns the board of the team on the clock and of its opponent.
func (st pickState) mine() (mine, theirs []int) {
	if st.actor == 1 {
		return st.dire, st.radiant
	}
	return st.radiant, st.dire
}

// winProbRanker ranks candidates by the win probability of the board that would
// result from taking them.
//
// This is the VALUE MODEL's ordering, not the shipped recommendation: the live
// recommender prunes with the enemy predictor and searches beyond one ply. What
// this measures is whether the quantity that search is built on points the right
// way at all — a search over a value model that cannot order one ply correctly
// has nothing to be right about.
func winProbRanker(wp *model.WinProb, hm *features.HeroMap) pickRanker {
	return pickRanker{
		name: winProbRankerName,
		score: func(st pickState, taken []bool) []float64 {
			ids := hm.IDs()
			out := make([]float64, len(taken))
			mine, theirs := st.mine()
			// One scratch board per call, reused across candidates: this runs
			// ~800k times per evaluation and a fresh slice per candidate dominates.
			buf := make([]int, len(mine)+1)
			copy(buf, mine)
			for i := range out {
				if taken[i] || i >= len(ids) {
					continue
				}
				buf[len(mine)] = ids[i]
				var p float64
				if st.actor == 0 {
					p = wp.RadiantWinProb(buf, theirs)
				} else {
					// Dire's gain is Radiant's loss; flip so higher is always
					// better for the team on the clock.
					p = 1 - wp.RadiantWinProb(theirs, buf)
				}
				out[i] = model.RoundScore(p)
			}
			return out
		},
	}
}

// advisorRanker ranks candidates the way the shipped advisor does: the beam's
// own scoring function, which is win-prob plus synergy, counters and the lining
// penalty. This is the ordering a user sees, so it is the one whose value has to
// be demonstrated — grading win-prob alone measures a part nobody consumes
// directly. It is still one ply: the search's deeper look-ahead is not replayed.
func advisorRanker(rec *recommender.Recommender, hm *features.HeroMap) pickRanker {
	return pickRanker{
		name: "advisor",
		score: func(st pickState, taken []bool) []float64 {
			ids := hm.IDs()
			out := make([]float64, len(taken))
			mine, theirs := st.mine()
			side := draft.Radiant
			if st.actor == 1 {
				side = draft.Dire
			}
			buf := make([]int, len(mine)+1)
			copy(buf, mine)
			for i := range out {
				if taken[i] || i >= len(ids) {
					continue
				}
				buf[len(mine)] = ids[i]
				out[i] = rec.ScoreDraft(side, buf, theirs)
			}
			return out
		},
	}
}

// matrixRanker ranks candidates by pair lift only — synergy with the team's own
// heroes plus counter value against what the enemy has shown. It is the component
// ablation: the linear win-prob head cannot express "good WITH X, bad INTO Y" at
// all, so if interactions are what ordering needs, this should beat it.
func matrixRanker(mx *matrices.Matrices, hm *features.HeroMap) pickRanker {
	return pickRanker{
		name: "matrices",
		score: func(st pickState, taken []bool) []float64 {
			out := make([]float64, len(taken))
			mine, theirs := st.mine()
			mineIdx, theirsIdx := denseIdx(hm, mine), denseIdx(hm, theirs)
			// Scratch buffers reused across candidates; a fresh slice per hero is
			// ~900k allocations over one evaluation.
			with := make([]int, len(mineIdx)+1)
			copy(with, mineIdx)
			one := make([]int, 1)
			for i := range out {
				if taken[i] {
					continue
				}
				with[len(mineIdx)] = i
				one[0] = i
				out[i] = model.RoundScore(mx.MeanSynergy(with) + mx.MeanCounter(one, theirsIdx))
			}
			return out
		},
	}
}

// baselinePickRanker ranks candidates by how often they get taken at this point
// of the draft — the control described on PickQualityMetrics.
func baselinePickRanker(b *Baseline) pickRanker {
	return pickRanker{
		name: b.Name,
		score: func(st pickState, taken []bool) []float64 {
			return b.Score(st.ord, true, st.actor, taken)
		},
	}
}

// rankOfChosen returns the 1-based rank of the chosen hero among the available
// ones, and how many candidates there were. Ties break by dense hero index, the
// same way the shipped ranking breaks them, so the rank is reproducible.
func rankOfChosen(scores []float64, taken []bool, chosen int) (rank, candidates int) {
	for i := range scores {
		if taken[i] {
			continue
		}
		candidates++
		if i == chosen {
			continue
		}
		if scores[i] > scores[chosen] || (scores[i] == scores[chosen] && i < chosen) {
			rank++
		}
	}
	return rank + 1, candidates
}

// evalPickQuality replays each draft ONCE and scores every ranker on it, so all
// rankers see exactly the same matches in the same order. That alignment is what
// makes the paired comparison against popularity valid.
func evalPickQuality(test []cache.Match, hm *features.HeroMap, rankers []pickRanker, group string, boot int) ([]PickQualityMetrics, []RankAgreement) {
	n := len(rankers)
	clusters := make([][]Cluster, n)
	rankSum := make([]float64, n)
	actions := make([]int, n)
	matches := 0

	// Ordering agreement rides along on this same pass: the scores are already
	// computed here, and re-deriving them would mean running the beam search over
	// every draft a second time.
	agreements := newAgreements(rankers)
	stateScores := make([][]float64, n)

	for _, m := range test {
		if !hasBan(m) {
			continue
		}
		taken := make([]bool, hm.Len())
		var radiant, dire []int
		sum := make([][2]float64, n)
		cnt := make([][2]int, n)

		for ord, pb := range orderedPickBans(m) {
			idx, ok := hm.Index(pb.HeroID)
			if !ok {
				continue
			}
			if pb.IsPick {
				st := pickState{radiant: radiant, dire: dire, actor: pb.Team, ord: ord}
				side := 0
				if pb.Team == 1 {
					side = 1
				}
				for ri, rk := range rankers {
					scores := rk.score(st, taken)
					stateScores[ri] = scores
					rank, candidates := rankOfChosen(scores, taken, idx)
					if candidates <= 1 {
						continue
					}
					norm := float64(rank-1) / float64(candidates-1)
					sum[ri][side] += norm
					cnt[ri][side]++
					rankSum[ri] += norm
					actions[ri]++
				}
				for _, acc := range agreements {
					acc.observe(stateScores, taken)
				}
			}
			taken[idx] = true
			switch {
			case pb.IsPick && pb.Team == 0:
				radiant = append(radiant, pb.HeroID)
			case pb.IsPick && pb.Team == 1:
				dire = append(dire, pb.HeroID)
			}
		}

		// Both sides must have been scored by every ranker, or the rankers would
		// be compared on different match sets.
		usable := true
		for ri := range rankers {
			if cnt[ri][0] == 0 || cnt[ri][1] == 0 {
				usable = false
				break
			}
		}
		if !usable {
			continue
		}

		y := 0.0
		if m.RadiantWin {
			y = 1
		}
		for ri := range rankers {
			// Lower normalized rank = the ranker liked that team's picks more, so
			// Dire-minus-Radiant is "how much more it liked Radiant's".
			s := sum[ri][1]/float64(cnt[ri][1]) - sum[ri][0]/float64(cnt[ri][0])
			// One cluster per MATCH: the ten picks inside a draft are one
			// observation, not ten independent ones.
			clusters[ri] = append(clusters[ri], Cluster{Scores: []float64{s}, Labels: []float64{y}})
		}
		matches++
	}

	// The control is whichever ranker is pure frequency; everything else is
	// reported as a paired difference against it.
	base := -1
	for i, rk := range rankers {
		if rk.name == popularityRankerName {
			base = i
			break
		}
	}

	out := make([]PickQualityMetrics, 0, n)
	for i, rk := range rankers {
		mean := 0.0
		if actions[i] > 0 {
			mean = rankSum[i] / float64(actions[i])
		}
		m := PickQualityMetrics{
			Name:         rk.name,
			Group:        group,
			N:            matches,
			Actions:      actions[i],
			AUC:          BootstrapCI(clusters[i], boot, AUC),
			MeanNormRank: mean,
		}
		if base >= 0 && i != base {
			m.VsPopularity = PairedBootstrapDiff(clusters[i], clusters[base], boot, AUC)
		}
		out = append(out, m)
	}

	agree := make([]RankAgreement, 0, len(agreements))
	for _, acc := range agreements {
		agree = append(agree, acc.result(group))
	}
	return out, agree
}

// cmSubset returns the CM drafts of test, optionally narrowed to one provenance.
func cmSubset(test []cache.Match, source string) []cache.Match {
	out := test[:0:0]
	for _, m := range test {
		if !hasBan(m) {
			continue
		}
		if source != "" && m.Source != source {
			continue
		}
		out = append(out, m)
	}
	return out
}
