package recommender

import (
	"fmt"
	"sort"

	"cmnet/internal/blend"
	"cmnet/internal/capability"
	"cmnet/internal/draft"
	"cmnet/internal/features"
	"cmnet/internal/matrices"
	"cmnet/internal/model"
	"cmnet/internal/roles"
	"cmnet/internal/rules"
)

// DefaultTopN is how many recommendations / enemy predictions to surface.
const DefaultTopN = 8

// BeamConfig tunes the M3 beam-search pick recommender. Zero values disable beam
// search (the recommender falls back to one-ply win-prob scoring).
type BeamConfig struct {
	PruneN int // candidate pool size (top-N meta by predictor prior)
	Depth  int // CM-sequence plies simulated past the candidate (§6.4 H)
	Width  int // beam width over our future actions
	// LambdaPos weights the role-coverage (lining) differential in boardStrength's
	// fallback for bundles without a stacker (#8). The stacker learns that weight
	// itself; the synergy/counter weights of the fallback are strengthLambda.
	LambdaPos float64

	// EnemyPolicy enables expectimax over the opponent predictor (± scout blend).
	// When false, the legacy value-greedy one-action rollout is used (gate/regression).
	EnemyPolicy bool
	// EnemyTopK caps how many enemy actions are expanded under the policy.
	EnemyTopK int
	// MassFloor stops expanding more enemy actions once kept policy mass reaches it
	// (whichever comes first with EnemyTopK). Tail mass uses V_baseline.
	MassFloor float64
}

// DefaultBeamConfig is the M3/§6.4 default tuning.
func DefaultBeamConfig() BeamConfig {
	return BeamConfig{
		PruneN: 24, Depth: 4, Width: 3,
		LambdaPos:   0.15,
		EnemyPolicy: true, EnemyTopK: 8, MassFloor: 0.85,
	}
}

// Recommender computes deterministic recommendations from the layer-1 models.
// Without matrices it uses one-ply win-prob scoring (MVP); with matrices and a
// non-zero BeamConfig it uses pruned beam search (M3).
type Recommender struct {
	WinProb   *model.WinProb
	Predictor *model.Predictor
	heroMap   *features.HeroMap

	synergy [][]float64
	counter [][]float64
	beam    BeamConfig

	// strengthLambda weights synergy/counter in the draft-strength readout (see
	// DefaultStrengthLambda). Zero falls back to the default so a zero-value
	// Recommender still blends when matrices are present. Superseded by stacker
	// when the bundle carries one.
	strengthLambda float64
	// stacker is the bundle's fitted strength combiner (validation-fitted weights
	// over win-prob logit + synergy/counter/role differentials). Nil on bundles
	// baked before it existed.
	stacker *model.Stacker
	// rollout marginalizes the unfilled pick slots in the strength readout.
	rollout bool

	// Scout blending (§9, layers 2-3) for the enemy-prediction head only.
	scoutEnabled bool
	enemyAff2    []float64
	enemyAff3    []float64
	// enemyAff3Bans is layer-3 BAN evidence (what the team banned this event).
	enemyAff3Bans []float64
	eventN        int
	blendW        blend.Weights

	// Ally comfort bias (#3): a gentle additive nudge toward heroes our own team
	// is comfortable on, applied to OUR pick scores. Normalized to [0,1] per hero.
	allyAff []float64
	allyW   float64
	// allyRaw is the un-normalized ally comfort vector, used as layer-2 evidence
	// for the ENEMY's bans (they ban what we play).
	allyRaw []float64

	// Capability roster (§6.5): soft player→hero feasibility on our picks.
	capRoster *capability.Roster
	capLambda float64
	// Enemy roster for lineup guesses (layer 2 SoftP); optional.
	enemyRoster *capability.Roster

	// Lineup fixes from the captain (session-only).
	heroPosOurs    map[int]int
	heroPosEnemy   map[int]int
	heroPlayerOurs map[int]int64
	// Hero pool (#4): our preferred draft heroes (by Valve id).
	heroPool map[int]bool

	// Hero positions (#8): Valve id → eligible lane positions (1-5). Enables a
	// role-coverage penalty so our drafts can plausibly field a 1-5.
	posByID   map[int][]int
	rolePrior *roles.Prior
	// metaScore caches the empty-draft predictor prior (≈ first-pick popularity),
	// used to rank the per-position meta panel (#9) ONLY as a fallback when no
	// STRATZ-baked per-position meta is present. Computed lazily.
	metaScore []float64
	// posMeta is the STRATZ-baked per-position meta (#9): position 1-5 → heroes
	// pre-ranked by real per-position popularity (with win rate). When set, the
	// position panel reads it directly instead of the coarse global-score fallback.
	posMeta map[int][]MetaHero

	// cmOrder is the draft schedule (bundle-inferred, else draft.DefaultOrder).
	cmOrder draft.Order

	// Item/advice rules (#10), evaluated against the enemy draft.
	rules []rules.Rule
}

// WithRules sets the advice rules (#10), parsed against the enemy draft each step.
func (r *Recommender) WithRules(rs []rules.Rule) *Recommender {
	r.rules = rs
	return r
}

// MetaTopPerPosition is how many heroes to list per lane position in the meta panel.
const MetaTopPerPosition = 6

// positionMeta builds the patch meta per lane position (#9): for each of positions
// 1-5, the top available heroes for that position with taken/banned heroes removed.
// It prefers the STRATZ-baked per-position meta (real per-position popularity +
// win rate); without it, it falls back to the coarse global predictor popularity
// filtered by role eligibility. Returns nil when neither source is present.
func (r *Recommender) positionMeta(d draft.View) []PositionMeta {
	if len(r.posMeta) > 0 {
		return r.positionMetaFromBaked(d)
	}
	if len(r.posByID) == 0 {
		return nil
	}
	if r.metaScore == nil {
		r.metaScore = r.Predictor.Distribution(r.firstPickState())
	}
	out := make([]PositionMeta, 0, 5)
	for pos := 1; pos <= 5; pos++ {
		heroes := make([]MetaHero, 0, MetaTopPerPosition)
		for idx := range r.metaScore {
			id := r.heroMap.ID(idx)
			if d.IsTaken(id) {
				continue
			}
			elig, ok := r.posByID[id]
			if !ok || !containsInt(elig, pos) {
				continue
			}
			heroes = append(heroes, MetaHero{HeroID: id, Score: r.metaScore[idx]})
		}
		sortMetaHeroes(heroes)
		if len(heroes) > MetaTopPerPosition {
			heroes = heroes[:MetaTopPerPosition]
		}
		out = append(out, PositionMeta{Position: pos, Heroes: heroes})
	}
	return out
}

// positionMetaFromBaked renders the STRATZ-baked per-position meta, dropping
// heroes already picked/banned. The baked lists are already ranked deterministically
// at train time, but we re-sort defensively to keep ordering stable (§6).
func (r *Recommender) positionMetaFromBaked(d draft.View) []PositionMeta {
	out := make([]PositionMeta, 0, 5)
	for pos := 1; pos <= 5; pos++ {
		src := r.posMeta[pos]
		heroes := make([]MetaHero, 0, MetaTopPerPosition)
		for _, mh := range src {
			if d.IsTaken(mh.HeroID) {
				continue
			}
			heroes = append(heroes, mh)
			if len(heroes) >= MetaTopPerPosition {
				break
			}
		}
		sortMetaHeroes(heroes)
		out = append(out, PositionMeta{Position: pos, Heroes: heroes})
	}
	return out
}

// WithPositionMeta sets the STRATZ-baked per-position meta (#9). The map is
// position (1-5) → heroes pre-ranked by per-position popularity. Empty leaves the
// coarse roles-based fallback in place.
func (r *Recommender) WithPositionMeta(pm map[int][]MetaHero) *Recommender {
	if len(pm) == 0 {
		return r
	}
	r.posMeta = pm
	return r
}

// WithPositions enables lining checks (#8) from a hero id → eligible positions
// map (typically bundle.HeroPositions). Empty leaves lining disabled.
func (r *Recommender) WithPositions(pos map[int][]int) *Recommender {
	if len(pos) == 0 {
		return r
	}
	r.posByID = pos
	return r
}

// WithRolePrior attaches P(position|hero) for soft win-prob encoding and MAP
// role tags on recommendations (§6.3).
func (r *Recommender) WithRolePrior(p *roles.Prior) *Recommender {
	if p == nil || len(p.Prob) == 0 {
		return r
	}
	r.rolePrior = p
	return r
}

// DefaultAllyWeight is the additive bonus applied to a pick's win score for a
// fully-comfortable ally hero. Small by design: it breaks ties toward comfort
// without overriding draft strength.
const DefaultAllyWeight = 0.03

// WithAllies biases our pick recommendations toward heroes our own team is
// comfortable on (ally affinity vector, indexed by dense hero index). The vector
// is normalized to [0,1] so the bonus is bounded. Nil leaves picks unbiased.
func (r *Recommender) WithAllies(allyAff []float64) *Recommender {
	if allyAff == nil {
		return r
	}
	max := 0.0
	for _, v := range allyAff {
		if v > max {
			max = v
		}
	}
	if max <= 0 {
		return r
	}
	norm := make([]float64, len(allyAff))
	for i, v := range allyAff {
		norm[i] = v / max
	}
	r.allyAff = norm
	r.allyRaw = append([]float64(nil), allyAff...)
	if r.allyW == 0 {
		r.allyW = DefaultAllyWeight
	}
	return r
}

// WithHeroPool sets our preferred draft pool (#4). Nil/empty leaves it unset.
func (r *Recommender) WithHeroPool(ids []int) *Recommender {
	if len(ids) == 0 {
		return r
	}
	r.heroPool = make(map[int]bool, len(ids))
	for _, id := range ids {
		r.heroPool[id] = true
	}
	return r
}

// WithCapability installs the §6.5 ally roster. Soft P(player plays hero) drives
// a feasibility penalty on our pick scores; heroes are never hard-filtered.
// Nil roster is a no-op. lambda <= 0 selects DefaultLambda.
func (r *Recommender) WithCapability(roster *capability.Roster, lambda float64) *Recommender {
	if roster == nil || len(roster.Players) < capability.MinRoster {
		return r
	}
	r.capRoster = roster
	if lambda <= 0 {
		lambda = capability.DefaultLambda
	}
	r.capLambda = lambda
	return r
}

// WithEnemyRoster installs SoftP for enemy lineup auto-assignment (display + sync).
func (r *Recommender) WithEnemyRoster(roster *capability.Roster) *Recommender {
	r.enemyRoster = roster
	return r
}

// EnemyRoster returns the enemy SoftP roster, if any.
func (r *Recommender) EnemyRoster() *capability.Roster { return r.enemyRoster }

// CapRoster returns the ally capability roster, if any.
func (r *Recommender) CapRoster() *capability.Roster { return r.capRoster }

// RolePrior returns the loaded role prior, if any.
func (r *Recommender) RolePrior() *roles.Prior { return r.rolePrior }

// PosByID returns hero→eligible positions, if any.
func (r *Recommender) PosByID() map[int][]int { return r.posByID }

// WithLineupFix pins hero→position and hero→player from the draft lineup panel.
func (r *Recommender) WithLineupFix(ourPos, enemyPos map[int]int, ourPlayer map[int]int64) *Recommender {
	r.heroPosOurs = cloneIntMap(ourPos)
	r.heroPosEnemy = cloneIntMap(enemyPos)
	r.heroPlayerOurs = cloneInt64Map(ourPlayer)
	return r
}

func cloneIntMap(m map[int]int) map[int]int {
	if len(m) == 0 {
		return nil
	}
	out := make(map[int]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneInt64Map(m map[int]int64) map[int]int64 {
	if len(m) == 0 {
		return nil
	}
	out := make(map[int]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// WithScout enables log-linear blending of the enemy prediction with the frozen
// snapshot's layer-2 (aff2) and layer-3 (aff3, with n drafts observed) vectors.
// Either vector may be nil for graceful degradation. Layers 2-3 affect ONLY the
// enemy prediction; pick recommendations stay on layer 1 (§9). Returns the
// receiver for chaining.
func (r *Recommender) WithScout(aff2, aff3 []float64, n int, w blend.Weights) *Recommender {
	if aff2 == nil && aff3 == nil {
		return r // nothing to blend -> stay on layer 1
	}
	r.scoutEnabled = true
	r.enemyAff2 = aff2
	r.enemyAff3 = aff3
	r.eventN = n
	r.blendW = w
	return r
}

// DefaultStrengthLambda weights the synergy/counter lift added to the draft-strength
// readout. Offline eval (internal/eval) showed the raw bag-of-heroes win-prob is
// near-random on Captains Mode (AUC ~0.55) while the pair-based matrix signal carries
// more of the outcome there, and that a blend at λ≈1 beats pure win-prob on every
// segment (CM, AP, all) at once. λ≈1 is needed because the matrices' spread (~±0.05)
// matches the win-prob base's spread; the beam's internal 0.10 barely moves it, wasting
// the pair signal. Applied only to the strength readout, not to beam candidate ranking.
const DefaultStrengthLambda = 1.0

// New builds a Recommender. WinProb and Predictor must share the same hero map.
func New(wp *model.WinProb, pred *model.Predictor) *Recommender {
	return &Recommender{WinProb: wp, Predictor: pred, heroMap: wp.HeroMap, strengthLambda: DefaultStrengthLambda}
}

// WithMatrices enables M3 beam search using the synergy/counter matrices (indexed
// by dense hero index, same hero map as the models). Returns the receiver for
// chaining. Passing nil matrices leaves the recommender in one-ply mode.
func (r *Recommender) WithMatrices(synergy, counter [][]float64) *Recommender {
	if synergy == nil || counter == nil {
		return r
	}
	r.synergy = synergy
	r.counter = counter
	r.beam = DefaultBeamConfig()
	return r
}

// WithStacker installs the bundle's fitted strength combiner. Nil is a no-op, so
// older bundles keep the fixed-lambda blend.
func (r *Recommender) WithStacker(s *model.Stacker) *Recommender {
	r.stacker = s
	return r
}

// WithRollout enables marginalizing over unfilled pick slots in the strength
// readout (rollout.go). Off by default so existing golden output is unaffected
// until a bundle is retrained with prefix-trained weights.
func (r *Recommender) WithRollout(on bool) *Recommender {
	r.rollout = on
	return r
}

// matrices wraps the loaded grids for the stacker's mean-lift helpers.
func (r *Recommender) matrices() *matrices.Matrices {
	if r.synergy == nil || r.counter == nil {
		return nil
	}
	return &matrices.Matrices{Synergy: r.synergy, Counter: r.counter}
}

// denseIdx maps hero ids to dense indices, dropping unknowns.
func (r *Recommender) denseIdx(ids []int) []int {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if i, ok := r.heroMap.Index(id); ok {
			out = append(out, i)
		}
	}
	return out
}

// roleCoverageDiff is Radiant's soft lane-coverage advantage under the
// assignment posterior (§6.3), normalized to roughly [-1,1].
func (r *Recommender) roleCoverageDiff(rPicks, dPicks []int) float64 {
	if r.rolePrior == nil && len(r.posByID) == 0 {
		return 0
	}
	return roles.CoverageDiff(rPicks, dPicks, r.rolePrior, r.posByID)
}

// mapRole returns the MAP position (1-5) for hero given the side's picks plus
// the candidate, or 0 when roles are unavailable.
func (r *Recommender) mapRole(picks []int, hero int) int {
	if pos, ok := r.heroPosOurs[hero]; ok && pos >= 1 && pos <= 5 {
		return pos
	}
	if r.rolePrior == nil {
		return 0
	}
	line := append([]int(nil), picks...)
	found := false
	for _, id := range line {
		if id == hero {
			found = true
			break
		}
	}
	if !found {
		line = append(line, hero)
	}
	asg := roles.MAP(line, r.rolePrior, r.posByID)
	if asg == nil {
		return r.rolePrior.Mode(hero)
	}
	if pos, ok := asg[hero]; ok {
		return pos
	}
	return r.rolePrior.Mode(hero)
}

// encodeWinProb builds schema-v2 features with soft position occupancy when a
// role prior is present (E[logit] = w·E[x] for the linear win-prob head).
func (r *Recommender) encodeWinProb(rPicks, dPicks []int) []float64 {
	if r.rolePrior == nil {
		return r.heroMap.EncodeWinProb(rPicks, dPicks)
	}
	rOcc := roles.ExpectedOccupancy(rPicks, r.rolePrior, r.posByID)
	dOcc := roles.ExpectedOccupancy(dPicks, r.rolePrior, r.posByID)
	rOcc = pinOccupancy(rOcc, r.heroPosForSide(draft.Radiant))
	dOcc = pinOccupancy(dOcc, r.heroPosForSide(draft.Dire))
	return r.heroMap.EncodeWinProbSoft(rPicks, dPicks, rOcc, dOcc)
}

func (r *Recommender) heroPosForSide(side draft.Team) map[int]int {
	// OurTeam is not stored on Recommender; callers pin ours/enemy maps. Radiant
	// seats use ours when we play Radiant — session applies maps already translated
	// via WithLineupFix as our/enemy, so encode overlays both onto both sides by
	// matching hero ids present on that side's picks only inside pinOccupancy.
	out := map[int]int{}
	for h, p := range r.heroPosOurs {
		out[h] = p
	}
	for h, p := range r.heroPosEnemy {
		out[h] = p
	}
	_ = side
	return out
}

func pinOccupancy(occ map[int][roles.NumPositions]float64, fix map[int]int) map[int][roles.NumPositions]float64 {
	if len(fix) == 0 || occ == nil {
		return occ
	}
	for h, pos := range fix {
		if pos < 1 || pos > roles.NumPositions {
			continue
		}
		if _, ok := occ[h]; !ok {
			continue
		}
		var row [roles.NumPositions]float64
		row[pos-1] = 1
		occ[h] = row
	}
	return occ
}

// rawWinProb is the uncalibrated win-prob with soft positions when available.
func (r *Recommender) rawWinProb(rPicks, dPicks []int) float64 {
	if r.WinProb == nil || r.WinProb.Model == nil {
		return 0.5
	}
	if r.rolePrior == nil {
		return r.WinProb.RawWinProb(rPicks, dPicks)
	}
	return r.WinProb.Model.Predict(r.encodeWinProb(rPicks, dPicks))
}

// WithCMOrder sets the Captains Mode schedule the planner and the enemy-step
// lookup follow — normally the bundle's inferred order. Nil or invalid keeps the
// built-in schedule.
func (r *Recommender) WithCMOrder(o draft.Order) *Recommender {
	if o.Valid() {
		r.cmOrder = append(draft.Order(nil), o...)
	}
	return r
}

// CMOrder returns the schedule in use (the built-in one when none was set), so a
// manual board can be laid out by the same order the recommender plans with.
func (r *Recommender) CMOrder() draft.Order { return r.cmOrder.OrDefault() }

// SetBeamConfig overrides the beam tuning (call after WithMatrices).
func (r *Recommender) SetBeamConfig(cfg BeamConfig) *Recommender {
	if cfg.EnemyTopK <= 0 {
		cfg.EnemyTopK = 8
	}
	if cfg.MassFloor <= 0 {
		cfg.MassFloor = 0.85
	}
	r.beam = cfg
	return r
}

// beamEnabled reports whether beam search should be used for pick scoring.
func (r *Recommender) beamEnabled() bool {
	return r.synergy != nil && r.counter != nil && r.beam.Depth > 0
}

// Recommend produces the ranked recommendations for the current step and the
// predicted next move. ourTeam is the side we are advising. d may be a mutable
// *draft.DraftState (manual input) or a *draft.Snapshot (GSI); both satisfy
// draft.View.
func (r *Recommender) Recommend(d draft.View, ourTeam draft.Team, topN int) Result {
	if topN <= 0 {
		topN = DefaultTopN
	}
	step, ok := d.Current()

	res := Result{}

	// Current draft strength: win-prob of the picks already on the board, so the user
	// sees how strong their draft is, not only the next-pick suggestion. Computed
	// before the draft-complete guard so the win-prob survives after the draft ends
	// (CM in-game: there is no current step, but the full lineup is still on the board).
	res.Strength = r.draftStrength(d, ourTeam)

	if !ok {
		return res // draft complete: no next-step recs, but keep the strength read
	}
	res.OurTurn = step.Team == ourTeam
	res.IsPick = step.Action == draft.Pick

	// The current step's distribution, for whoever is acting now (us, on our turn).
	// It prunes our pick candidates to the heroes this seat plausibly takes here.
	dist := r.Predictor.Distribution(predictorState(boardOf(d), step.Team, step.Action, boardCursor(d)))

	// What the ENEMY does next. On their turn that is the current step; on ours it
	// is their next step in the schedule — previously this panel showed the
	// distribution of the current step even on our turn, i.e. a prediction of our
	// own move labelled as the enemy's.
	enemyNext := r.enemyPolicy(d, ourTeam, false)
	enemyPick := enemyNext
	if !enemyNext.ok() || enemyNext.step.Action != draft.Pick {
		enemyPick = r.enemyPolicy(d, ourTeam, true)
	}
	if enemyNext.ok() {
		res.EnemyStep = &EnemyStep{Ord: enemyNext.ord, IsPick: enemyNext.step.Action == draft.Pick}
	}
	nextProbs := r.orZeros(enemyNext)
	pickProbs := r.orZeros(enemyPick)

	// Enemy prediction: their next move over untaken heroes.
	res.EnemyPred = r.topEnemy(d, ourTeam, nextProbs, enemyNext.step.Action, topN)

	// Pool contention (#4): our preferred heroes the enemy is most likely to take
	// with their next move, pick or ban.
	res.PoolAlert = r.poolAlert(d, ourTeam, nextProbs, enemyNext.step.Action, topN)

	// The other half of the ban decision: what they are likely to PICK and we can
	// afford to let them have.
	res.SafeToLet = r.safeToLet(d, ourTeam, pickProbs, topN)

	// Patch meta per position (#9): top available heroes for each lane, with
	// taken/banned heroes removed.
	res.PositionMeta = r.positionMeta(d)

	// Item/advice rules (#10): evaluate against the enemy's current picks.
	if len(r.rules) > 0 {
		res.ItemAdvice = rules.Eval(r.rules, d.Picks(ourTeam.Opponent()))
	}

	// Recommendations for the current step. Picks use the raw layer-1 prior for
	// pruning (§9: picks stay on layer 1); bans use the blended probability of the
	// enemy PICKING the hero as their "deny it" base.
	res.Recs = r.rankCandidates(d, ourTeam, step, dist, pickProbs, topN)
	return res
}

// HeroScore is a hero id paired with a matrix lift value (0.5 = neutral; see the
// matrices package). Used by the matrices inspection view.
type HeroScore struct {
	HeroID int     `json:"heroId"`
	Value  float64 `json:"value"`
}

// HasMatrices reports whether synergy/counter matrices are loaded (the matrices
// view needs them).
func (r *Recommender) HasMatrices() bool { return r.synergy != nil && r.counter != nil }

// Matchups returns, for hero heroID, the top-N best teammates (synergy lift), the
// top-N heroes heroID best counters (counter lift, heroID facing them), and the
// top-N heroes that best counter heroID. Values are lift (0.5 neutral). Cells with
// no observed games (exactly 0.5) are skipped so the view shows only real signal.
// Ordering is deterministic (value desc, hero_id tie-break). ok=false when matrices
// are absent or heroID is unknown.
func (r *Recommender) Matchups(heroID, topN int) (synergy, counters, counteredBy []HeroScore, ok bool) {
	if !r.HasMatrices() {
		return nil, nil, nil, false
	}
	ci, found := r.heroMap.Index(heroID)
	if !found {
		return nil, nil, nil, false
	}
	for idx, id := range r.heroMap.IDs() {
		if idx == ci {
			continue
		}
		if v := r.synergy[ci][idx]; v != 0.5 {
			synergy = append(synergy, HeroScore{HeroID: id, Value: v})
		}
		if v := r.counter[ci][idx]; v != 0.5 { // heroID's team facing id
			counters = append(counters, HeroScore{HeroID: id, Value: v})
		}
		if v := r.counter[idx][ci]; v != 0.5 { // id's team facing heroID
			counteredBy = append(counteredBy, HeroScore{HeroID: id, Value: v})
		}
	}
	return topHeroScores(synergy, topN), topHeroScores(counters, topN), topHeroScores(counteredBy, topN), true
}

// topHeroScores sorts by value desc with a hero_id tie-break (deterministic, §6)
// and truncates to topN.
func topHeroScores(s []HeroScore, topN int) []HeroScore {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Value != s[j].Value {
			return s[i].Value > s[j].Value
		}
		return s[i].HeroID < s[j].HeroID
	})
	if topN > 0 && len(s) > topN {
		s = s[:topN]
	}
	return s
}

// topEnemy ranks the predicted next move over untaken heroes from precomputed
// (possibly scout-blended) probabilities.
func (r *Recommender) topEnemy(d draft.View, ourTeam draft.Team, probs []float64, action draft.Action, topN int) []EnemyPick {
	picks := r.enemyPicks(d, ourTeam, probs, action, nil)
	rankEnemyPicks(picks)
	if len(picks) > topN {
		picks = picks[:topN]
	}
	return picks
}

// enemyPicks builds the full untaken-hero list with probabilities, provenance and
// threat decomposition. keep, when non-nil, restricts the list to hero ids it
// accepts (used by the pool panel). The list is in dense index order; callers rank
// it themselves.
func (r *Recommender) enemyPicks(d draft.View, ourTeam draft.Team, probs []float64, action draft.Action, keep func(id int) bool) []EnemyPick {
	ourPicks := d.Picks(ourTeam)
	enemyPicks := d.Picks(ourTeam.Opponent())

	out := make([]EnemyPick, 0, len(probs))
	for idx, p := range probs {
		id := r.heroMap.ID(idx)
		if d.IsTaken(id) || (keep != nil && !keep(id)) {
			continue
		}
		e := EnemyPick{HeroID: id, Prob: p, Source: r.provenanceFor(idx, action)}
		e.Threat, e.Fit, e.Danger = r.threatOf(id, ourPicks, enemyPicks)
		out = append(out, e)
	}
	return out
}

// threatOf decomposes what a hero would cost us: how well it counters our current
// picks (threat) and how well it fits the enemy's (fit), both centered on zero,
// plus their weighted combination (danger). All three are zero without matrices.
//
// The weights are the ban weights, so the ban ranking and the "safe to let"
// judgement are two readings of one quantity rather than two rival opinions.
func (r *Recommender) threatOf(candID int, ourPicks, enemyPicks []int) (threat, fit, danger float64) {
	if !r.HasMatrices() {
		return 0, 0, 0
	}
	ci, ok := r.heroMap.Index(candID)
	if !ok {
		return 0, 0, 0
	}
	threat = model.RoundScore(r.meanCounterFrom(ci, ourPicks) - 0.5)
	fit = model.RoundScore(r.meanSynergyWith(ci, enemyPicks) - 0.5)
	danger = model.RoundScore(lambdaBanThreat*threat + lambdaBanSyn*fit)
	return threat, fit, danger
}

// safePoolMult sizes the candidate pool the "safe to let" verdict reasons over:
// the top safePoolMult×topN most likely enemy moves. Wider than the displayed list
// so the median it compares against reflects the real board, not just the few rows
// that happen to fit on screen.
const safePoolMult = 3

// safeMargin is how far below the pool's median danger a hero must sit before we
// call it harmless. Without it, half the pool would be labelled "safe" by
// construction, including heroes that are merely the lesser of two threats — the
// advice has to be worth acting on, not just arithmetically true.
const safeMargin = 0.005

// safeToLet lists heroes the enemy is likely to take that we can afford to ignore:
// high pick probability, danger clearly below the median of the same pool, and no
// positive threat against our own picks. Ordered by enemy probability (most likely
// first), because that is the order the decision arises in.
//
// Returns nil without matrices: with no pair signal every danger is zero, and a
// "don't fear this" verdict backed by nothing is worse than no verdict at all.
func (r *Recommender) safeToLet(d draft.View, ourTeam draft.Team, eprobs []float64, topN int) []EnemyPick {
	if !r.HasMatrices() {
		return nil
	}
	pool := r.enemyPicks(d, ourTeam, eprobs, draft.Pick, nil)
	rankEnemyPicks(pool)
	if n := safePoolMult * topN; len(pool) > n {
		pool = pool[:n]
	}
	if len(pool) < 2 {
		return nil
	}

	dangers := make([]float64, len(pool))
	for i, e := range pool {
		dangers[i] = e.Danger
	}
	sort.Float64s(dangers)
	median := dangers[len(dangers)/2]

	safe := make([]EnemyPick, 0, len(pool))
	for _, e := range pool {
		if e.Danger <= median-safeMargin && e.Threat <= 0 {
			safe = append(safe, e)
		}
	}
	rankEnemyPicks(safe)
	if len(safe) > topN {
		safe = safe[:topN]
	}
	return safe
}

// poolAlert ranks our available preferred-pool heroes by how likely the enemy is
// to take them next (pick or ban) — the ones worth securing. Empty when no pool
// is configured. Uses the same enemy probabilities and stable ordering as the
// enemy panel so it stays deterministic (§6).
func (r *Recommender) poolAlert(d draft.View, ourTeam draft.Team, eprobs []float64, action draft.Action, topN int) []EnemyPick {
	if len(r.heroPool) == 0 {
		return nil
	}
	picks := r.enemyPicks(d, ourTeam, eprobs, action, func(id int) bool { return r.heroPool[id] })
	rankEnemyPicks(picks)
	if len(picks) > topN {
		picks = picks[:topN]
	}
	return picks
}

// rankCandidates scores every untaken hero for the current step. For a pick we
// score the resulting win probability from our perspective; for a ban we score by
// how likely the enemy is to take the hero (deny high-value picks).
func (r *Recommender) rankCandidates(d draft.View, ourTeam draft.Team, step draft.Step, dist, eprobs []float64, topN int) []Recommendation {
	rPicks := d.Picks(draft.Radiant)
	dPicks := d.Picks(draft.Dire)

	// For pick steps with beam search enabled, restrict scoring to a pruned meta
	// pool and evaluate each candidate with a bounded rollout.
	var pool []int
	beamPick := step.Action == draft.Pick && r.beamEnabled()
	if beamPick {
		pool = r.prunePool(d, dist)
	}

	recs := make([]Recommendation, 0, r.heroMap.Len())
	for idx, id := range r.heroMap.IDs() {
		if d.IsTaken(id) {
			continue
		}
		var score float64
		var why string
		var factors *Factors
		switch {
		case beamPick:
			if !containsInt(pool, id) {
				continue // pruned: outside the meta pool
			}
			score = r.beamPickScore(d, ourTeam, id, pool)
			why = fmt.Sprintf("вин ~%.0f%%", score*100)
			// The planner's value is an expectation of boardStrength — a probability
			// on the strength bar's own scale, not a bespoke rollout score.
			score, why, factors = r.applyAllyBias(idx, score, why, true)
			ourPicks := rPicks
			if ourTeam == draft.Dire {
				ourPicks = dPicks
			}
			score, why, factors = r.applyCapability(ourPicks, id, score, why, factors)
		case step.Action == draft.Pick:
			score = r.pickScore(ourTeam, rPicks, dPicks, id)
			why = fmt.Sprintf("вин ~%.0f%%", score*100)
			score, why, factors = r.applyAllyBias(idx, score, why, true)
			ourPicks := rPicks
			if ourTeam == draft.Dire {
				ourPicks = dPicks
			}
			score, why, factors = r.applyCapability(ourPicks, id, score, why, factors)
		default: // ban step
			score, why, factors = r.banScore(d, ourTeam, rPicks, dPicks, id, idx, eprobs)
		}
		role := 0
		player := ""
		if step.Action == draft.Pick {
			ourPicks := rPicks
			if ourTeam == draft.Dire {
				ourPicks = dPicks
			}
			role = r.mapRole(ourPicks, id)
			if r.capRoster != nil {
				lineup := append(append([]int(nil), ourPicks...), id)
				player = capability.PlayerForFixed(id, lineup, r.capRoster, r.heroPlayerOurs)
			}
		}
		recs = append(recs, Recommendation{HeroID: id, Score: score, Why: why, Factors: factors, Role: role, Player: player})
	}
	rankRecommendations(recs)
	if len(recs) > topN {
		recs = recs[:topN]
	}
	return recs
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Ban scoring weights (#7): how much hero threat to our draft and synergy with the
// enemy draft add to the base "enemy is likely to take this" signal.
const (
	lambdaBanThreat = 0.40 // candidate counters our current picks
	lambdaBanSyn    = 0.20 // candidate synergizes with enemy's current picks
	lambdaBanAlt    = 0.30 // §6.7 deficit of equivalent alternatives after the ban
)

// banScore values denying a hero (§1.4 / §6.7): P(enemy takes it) plus danger to
// our draft plus the alternative-deficit — how much the enemy's outlook worsens
// after this hero is removed and their policy is recomputed. Deterministic (rounded).
func (r *Recommender) banScore(d draft.View, ourTeam draft.Team, rPicks, dPicks []int, candID, idx int, eprobs []float64) (float64, string, *Factors) {
	base := 0.0
	if idx >= 0 && idx < len(eprobs) {
		base = eprobs[idx]
	}
	f := &Factors{EnemyProb: base}
	if !r.beamEnabled() {
		// Still apply alt-deficit when matrices are off — it only needs the predictor.
		def := r.altDeficit(d, ourTeam, candID, eprobs)
		f.AltDeficit = def
		score := model.RoundScore(base + lambdaBanAlt*def)
		why := fmt.Sprintf("враг ~%.0f%%", base*100)
		if def > 0.01 {
			why += fmt.Sprintf(" · дыра %.0f", def*100)
		}
		return score, why, f
	}
	ci, ok := r.heroMap.Index(candID)
	if !ok {
		return base, fmt.Sprintf("враг ~%.0f%%", base*100), f
	}
	var ourPicks, enemyPicks []int
	if ourTeam == draft.Radiant {
		ourPicks, enemyPicks = rPicks, dPicks
	} else {
		ourPicks, enemyPicks = dPicks, rPicks
	}
	threat := r.meanCounterFrom(ci, ourPicks)
	esyn := r.meanSynergyWith(ci, enemyPicks)
	def := r.altDeficit(d, ourTeam, candID, eprobs)
	f.Threat, f.EnemySynergy, f.AltDeficit = threat, esyn, def
	score := model.RoundScore(base + lambdaBanThreat*(threat-0.5) + lambdaBanSyn*(esyn-0.5) + lambdaBanAlt*def)
	why := fmt.Sprintf("враг ~%.0f%%", base*100)
	if threat > 0.52 {
		why += " · контрит нас"
	}
	if esyn > 0.52 {
		why += " · синергия врага"
	}
	if def > 0.01 {
		why += fmt.Sprintf(" · дыра %.0f", def*100)
	}
	return score, why, f
}

// altDeficit is max(0, outlook_before − outlook_after) where outlook is the
// danger-weighted mass of the enemy next-action policy. After = same predictor
// (± scout blend) with cand treated as already banned.
func (r *Recommender) altDeficit(d draft.View, ourTeam draft.Team, candID int, eprobs []float64) float64 {
	if r.Predictor == nil || r.heroMap == nil || candID <= 0 {
		return 0
	}
	rPicks := d.Picks(draft.Radiant)
	dPicks := d.Picks(draft.Dire)
	before := r.policyOutlook(ourTeam, rPicks, dPicks, eprobs)

	// Recompute the enemy's next PICK with cand already banned by us — the same
	// action eprobs describes (their next pick), so before and after are comparable.
	ord, step, ok := r.nextAction(d, ourTeam.Opponent(), true)
	if !ok {
		return 0
	}
	b := boardOf(d)
	b.rBans = append([]int(nil), b.rBans...)
	b.dBans = append([]int(nil), b.dBans...)
	if ourTeam == draft.Radiant {
		b.rBans = append(b.rBans, candID)
	} else {
		b.dBans = append(b.dBans, candID)
	}
	after := r.blendEnemy(r.Predictor.Distribution(predictorState(b, ourTeam.Opponent(), step.Action, ord)), step.Action)
	post := r.policyOutlook(ourTeam, rPicks, dPicks, after)
	def := before - post
	if def < 0 {
		return 0
	}
	return model.RoundScore(def)
}

// policyOutlook is the value of the enemy's best remaining option under probs:
// max_i p_i · util_i, where util folds threat/synergy the same way ban danger does.
// Using the best option (not the policy expectation) matches §6.7 "лучший доступный
// вариант" and stays informative when util is flat: banning a unique high-p hero
// drops the max, banning one of two twins barely moves it.
func (r *Recommender) policyOutlook(ourTeam draft.Team, rPicks, dPicks []int, probs []float64) float64 {
	if len(probs) == 0 {
		return 0
	}
	var ourPicks, enemyPicks []int
	if ourTeam == draft.Radiant {
		ourPicks, enemyPicks = rPicks, dPicks
	} else {
		ourPicks, enemyPicks = dPicks, rPicks
	}
	best := 0.0
	ids := r.heroMap.IDs()
	for i, p := range probs {
		if p <= 0 || i >= len(ids) {
			continue
		}
		threat, syn := 0.5, 0.5
		if r.counter != nil {
			threat = r.meanCounterFrom(i, ourPicks)
		}
		if r.synergy != nil {
			syn = r.meanSynergyWith(i, enemyPicks)
		}
		util := 1 + lambdaBanThreat*(threat-0.5) + lambdaBanSyn*(syn-0.5)
		v := p * util
		if v > best {
			best = v
		}
	}
	return best
}

// meanCounterFrom is the average counter win rate of hero ci against targets
// (0.5 when there are no targets / matrices).
func (r *Recommender) meanCounterFrom(ci int, targets []int) float64 {
	if r.counter == nil || len(targets) == 0 {
		return 0.5
	}
	var sum float64
	var n int
	for _, tid := range targets {
		if ti, ok := r.heroMap.Index(tid); ok {
			sum += r.counter[ci][ti]
			n++
		}
	}
	if n == 0 {
		return 0.5
	}
	return sum / float64(n)
}

// meanSynergyWith is the average synergy of hero ci with mates (0.5 when none).
func (r *Recommender) meanSynergyWith(ci int, mates []int) float64 {
	if r.synergy == nil || len(mates) == 0 {
		return 0.5
	}
	var sum float64
	var n int
	for _, mid := range mates {
		if mi, ok := r.heroMap.Index(mid); ok {
			sum += r.synergy[ci][mi]
			n++
		}
	}
	if n == 0 {
		return 0.5
	}
	return sum / float64(n)
}

// applyCapability subtracts a soft feasibility penalty for our prospective lineup
// (current our picks + cand). Never drops a hero — only reshapes the score (§6.5).
func (r *Recommender) applyCapability(ourPicks []int, cand int, score float64, why string, f *Factors) (float64, string, *Factors) {
	if f == nil {
		f = &Factors{Base: score}
	}
	if r.capRoster == nil || r.capLambda <= 0 {
		return score, why, f
	}
	lineup := append(append([]int(nil), ourPicks...), cand)
	feas := capability.FeasibilityFixed(lineup, r.capRoster, r.heroPlayerOurs)
	delta := model.RoundScore(-r.capLambda * (1 - feas))
	if delta == 0 {
		return score, why, f
	}
	f.Capability = delta
	return model.RoundScore(score + delta), why + fmt.Sprintf(" · исполнимость %.0f%%", feas*100), f
}

// applyAllyBias adds the (bounded, normalized) ally-comfort bonus to a pick score
// and tags the reason when it contributes. Deterministic: the result is rounded.
// isWinProb says whether the base score handed to applyAllyBias is a calibrated
// win probability (one-ply scoring) or a beam rollout value, which is neither
// calibrated nor bounded to [0,1].
func (r *Recommender) applyAllyBias(idx int, score float64, why string, isWinProb bool) (float64, string, *Factors) {
	f := &Factors{Base: score, BaseIsWinProb: isWinProb}
	if r.allyAff == nil || idx < 0 || idx >= len(r.allyAff) || r.allyAff[idx] <= 0 {
		return score, why, f
	}
	biased := model.RoundScore(score + r.allyW*r.allyAff[idx])
	f.Comfort = biased - score
	return biased, why + " · из наших", f
}

// draftStrength evaluates the picks currently on the board (both sides) with the
// calibrated win-prob model, from our perspective and Radiant's. Returns nil until
// at least one hero is picked (an empty board carries no signal). Deterministic:
// the underlying win-prob is already rounded (§6).
func (r *Recommender) draftStrength(d draft.View, ourTeam draft.Team) *DraftStrength {
	rPicks := d.Picks(draft.Radiant)
	dPicks := d.Picks(draft.Dire)
	if len(rPicks) == 0 && len(dPicks) == 0 {
		return nil
	}
	// Mid-draft, marginalize over the unfilled slots rather than scoring the board
	// as if those heroes will never exist (see rollout.go). The marginalized logit
	// replaces the raw one going into boardStrength — previously the stacker path
	// recomputed from the bare board and silently threw the rollout away.
	raw := r.rawWinProb(rPicks, dPicks)
	if r.rollout {
		if rr, ok := r.rolloutStrength(d, rPicks, dPicks); ok {
			raw = rr
		}
	}
	radiantProb := r.boardStrength(rPicks, dPicks, raw)

	ourProb := radiantProb
	if ourTeam == draft.Dire {
		ourProb = model.RoundScore(1 - radiantProb)
	}
	return &DraftStrength{
		OurProb:      ourProb,
		RadiantProb:  radiantProb,
		RadiantPicks: len(rPicks),
		DirePicks:    len(dPicks),
	}
}

// pickScore returns our-perspective strength after adding candidate to our picks
// (the one-ply path: same boardStrength the planner and the strength bar use).
func (r *Recommender) pickScore(ourTeam draft.Team, rPicks, dPicks []int, cand int) float64 {
	my, enemy := rPicks, dPicks
	if ourTeam == draft.Dire {
		my, enemy = dPicks, rPicks
	}
	return r.evalDraft(ourTeam, appendCopy(my, cand), enemy)
}

// boardStrength is P(Radiant wins) for a board given the win-prob head's raw
// output for it — the ONE scoring function behind the strength readout, the
// one-ply pick score and the planner's leaves.
//
// Preferred: the bundle's stacker, fitted out-of-sample, combining the win-prob
// logit with the synergy/counter and role-coverage differentials in log-odds.
// Fallback for bundles without one: board-size-calibrated win-prob, plus the
// fixed-lambda pair lift and lining terms in probability space.
func (r *Recommender) boardStrength(rPicks, dPicks []int, raw float64) float64 {
	if r.stacker != nil && r.matrices() != nil {
		mx := r.matrices()
		ri, di := r.denseIdx(rPicks), r.denseIdx(dPicks)
		return model.RoundScore(clampProb01(r.stacker.Apply(model.StackerFeatures(
			raw,
			mx.MeanSynergy(ri)-mx.MeanSynergy(di),
			mx.MeanCounter(ri, di),
			r.roleCoverageDiff(rPicks, dPicks),
		))))
	}
	p := r.WinProb.CalibrateAt(raw, len(rPicks)+len(dPicks))
	if r.synergy != nil && r.counter != nil {
		lam := r.strengthLambda
		if lam == 0 {
			lam = DefaultStrengthLambda
		}
		p += lam*(r.meanSynergy(rPicks)-r.meanSynergy(dPicks)) +
			lam*(r.meanCounter(rPicks, dPicks)-0.5)
	}
	if r.beam.LambdaPos > 0 {
		p += r.beam.LambdaPos * r.roleCoverageDiff(rPicks, dPicks)
	}
	return model.RoundScore(clampProb01(p))
}

// clampProb01 bounds a value to [0,1] so the blended strength stays a probability.
func clampProb01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func appendCopy(s []int, v int) []int {
	out := make([]int, len(s)+1)
	copy(out, s)
	out[len(s)] = v
	return out
}
