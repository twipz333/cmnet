package scout

import (
	"fmt"
	"sort"
)

// AssessSmurf builds a display-only smurf-risk heuristic from career volume and
// recent match outcomes. Nil when there is nothing useful to show.
//
// Signals (additive, clamped 0..100):
//   - thin career (<80 / <150 games)
//   - high recent win rate (≥65% over ≥15 games)
//   - recent WR spike (last 15 vs previous 15)
//   - party WR much higher than solo
//
// Reasons are sorted for deterministic snapshot hashing.
func AssessSmurf(careerGames int, matches []MatchRef) *SmurfRisk {
	if careerGames <= 0 && len(matches) == 0 {
		return nil
	}

	score := 0
	var reasons []string

	if careerGames > 0 && careerGames < 80 {
		score += 35
		reasons = append(reasons, fmt.Sprintf("мало игр на аккаунте (%d)", careerGames))
	} else if careerGames > 0 && careerGames < 150 {
		score += 25
		reasons = append(reasons, fmt.Sprintf("тонкий аккаунт (%d игр)", careerGames))
	}

	// Newest-first from OpenDota; keep order stable by sorting StartTime desc,
	// MatchID desc so unit tests and alternate sources agree.
	ordered := append([]MatchRef(nil), matches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].StartTime != ordered[j].StartTime {
			return ordered[i].StartTime > ordered[j].StartTime
		}
		return ordered[i].MatchID > ordered[j].MatchID
	})

	recentN := 20
	if len(ordered) < recentN {
		recentN = len(ordered)
	}
	if recentN >= 15 {
		wins := 0
		for i := 0; i < recentN; i++ {
			if ordered[i].Won {
				wins++
			}
		}
		wr := float64(wins) / float64(recentN)
		if wr >= 0.70 {
			score += 30
			reasons = append(reasons, fmt.Sprintf("высокий WR в последних %d (%.0f%%)", recentN, wr*100))
		} else if wr >= 0.65 {
			score += 20
			reasons = append(reasons, fmt.Sprintf("повышенный WR в последних %d (%.0f%%)", recentN, wr*100))
		}
	}

	if len(ordered) >= 30 {
		w1, w0 := 0, 0
		for i := 0; i < 15; i++ {
			if ordered[i].Won {
				w1++
			}
			if ordered[15+i].Won {
				w0++
			}
		}
		wr1 := float64(w1) / 15
		wr0 := float64(w0) / 15
		if wr1-wr0 >= 0.20 && wr1 >= 0.65 {
			score += 20
			reasons = append(reasons, fmt.Sprintf("скачок WR: %.0f%% → %.0f%%", wr0*100, wr1*100))
		}
	}

	var soloW, soloN, partyW, partyN int
	for _, m := range ordered {
		if m.PartySize >= 2 {
			partyN++
			if m.Won {
				partyW++
			}
		} else {
			soloN++
			if m.Won {
				soloW++
			}
		}
	}
	if soloN >= 8 && partyN >= 8 {
		soloWR := float64(soloW) / float64(soloN)
		partyWR := float64(partyW) / float64(partyN)
		if partyWR-soloWR >= 0.15 {
			score += 15
			reasons = append(reasons, fmt.Sprintf("пати WR выше соло (%.0f%% vs %.0f%%)", partyWR*100, soloWR*100))
		}
	}

	if score <= 0 {
		// Still surface a calm "low" when we looked and found nothing spicy —
		// useful so the UI shows the check ran.
		return &SmurfRisk{Score: 0, Level: "low", Label: "смурф: низкий"}
	}
	if score > 100 {
		score = 100
	}
	level, label := "medium", "смурф: средний"
	if score >= 50 {
		level, label = "high", "смурф: высокий"
	} else if score < 25 {
		level, label = "low", "смурф: низкий"
	}
	sort.Strings(reasons)
	return &SmurfRisk{Score: score, Level: level, Label: label, Reasons: reasons}
}
