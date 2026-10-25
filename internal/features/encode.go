package features

// Team mirrors draft.Team values (0 radiant / 1 dire) without importing the draft
// package, keeping features dependency-free.
const (
	teamRadiant = 0
	teamDire    = 1
)

// CMSteps is the number of actions in a full Captains Mode draft (7 bans + 5 picks
// per side). Used to size the draft-step one-hot.
const CMSteps = 24

// CMPhases is the number of phases in the CM sequence, grouped into six blocks.
// Phase is a coarser view of the step index: it lets the model share statistics
// between adjacent steps when per-step data is thin.
const CMPhases = 6

// maxActionInPhase is the longest phase (the opening seven bans), sizing the
// within-phase index.
const maxActionInPhase = 7

// WinProbPositions is the number of lane slots encoded per hero (positions 1-5).
const WinProbPositions = 5

// WinProbLen is the win-prob input length: N main effects + N*5 position slots.
func (h *HeroMap) WinProbLen() int { return h.Len() * (1 + WinProbPositions) }

// WinProbHeroes recovers the hero-map size from a feature vector length.
// Schema v2 vectors are 6N long; legacy v1 vectors are N long (N not divisible by
// 6, or treated as main-only when the caller knows the map size).
func WinProbHeroes(featLen int) int {
	if featLen > 0 && featLen%(1+WinProbPositions) == 0 {
		return featLen / (1 + WinProbPositions)
	}
	return featLen
}

// EncodeWinProb builds the win-prob feature vector.
//
// Layout (schema v2):
//
//	[0:N]     antisymmetric main effects (+1 Radiant / -1 Dire / 0 absent)
//	[N:6N]    (hero, position) block: for dense index i and position p in 1..5,
//	          slot N + i*5 + (p-1) gets the same sign when that assignment is set.
//
// Without position maps the position block stays zero — bulk matches and callers
// that have not yet attached a role prior still train/infer on main effects alone.
//
// This ANTISYMMETRIC encoding replaces the older two-block layout (radiant
// multi-hot followed by dire multi-hot). A draft is symmetric under swapping the
// sides — a hero worth +x to Radiant is worth -x to Dire — but two independent
// weight blocks have to learn that fact twice from half the data each, doubling
// the variance for nothing. Encoding the difference builds the symmetry in
// exactly, and the side asymmetry that IS real (Radiant's map advantage) survives
// as the model's bias term. It also makes side-swap augmentation unnecessary:
// swapping sides maps x to -x analytically rather than approximately.
func (h *HeroMap) EncodeWinProb(radiantPicks, direPicks []int) []float64 {
	return h.EncodeWinProbPositions(radiantPicks, direPicks, nil, nil)
}

// EncodeWinProbPositions is EncodeWinProb with hard hero→position labels
// (training on finished CM drafts where match_players.position is known).
func (h *HeroMap) EncodeWinProbPositions(radiantPicks, direPicks []int, radiantPos, direPos map[int]int) []float64 {
	n := h.Len()
	x := make([]float64, n*(1+WinProbPositions))
	set := func(ids []int, pos map[int]int, sign float64) {
		for _, id := range ids {
			i, ok := h.Index(id)
			if !ok {
				continue
			}
			x[i] += sign
			if pos == nil {
				continue
			}
			p, ok := pos[id]
			if !ok || p < 1 || p > WinProbPositions {
				continue
			}
			x[n+i*WinProbPositions+(p-1)] += sign
		}
	}
	set(radiantPicks, radiantPos, 1)
	set(direPicks, direPos, -1)
	return x
}

// EncodeWinProbSoft fills the position block with expected occupancy weights
// (serve-time marginalization: E[logit] = w·E[x] for a linear model).
//
// occ maps hero id → [5] posterior mass on positions 1..5. Mass may be soft;
// main effects stay hard ±1 for heroes already on the board.
func (h *HeroMap) EncodeWinProbSoft(radiantPicks, direPicks []int, radiantOcc, direOcc map[int][WinProbPositions]float64) []float64 {
	n := h.Len()
	x := h.EncodeWinProb(radiantPicks, direPicks)
	fill := func(ids []int, occ map[int][WinProbPositions]float64, sign float64) {
		if occ == nil {
			return
		}
		for _, id := range ids {
			i, ok := h.Index(id)
			if !ok {
				continue
			}
			row, ok := occ[id]
			if !ok {
				continue
			}
			base := n + i*WinProbPositions
			for p := 0; p < WinProbPositions; p++ {
				x[base+p] += sign * row[p]
			}
		}
	}
	fill(radiantPicks, radiantOcc, 1)
	fill(direPicks, direOcc, -1)
	return x
}

// EncodeWinProbExpected is EncodeWinProb with additional EXPECTED heroes: a
// probability mass per dense index for the slots each side has not filled yet.
//
// Expected mass lands on main effects only. Position mass for unfinished slots
// is not guessed here — the live recommender already soft-encodes heroes that
// are on the board. Passing nil for either side reduces to EncodeWinProb.
func (h *HeroMap) EncodeWinProbExpected(radiantPicks, direPicks []int, expRadiant, expDire []float64) []float64 {
	x := h.EncodeWinProb(radiantPicks, direPicks)
	n := h.Len()
	for i, v := range expRadiant {
		if i < n {
			x[i] += v
		}
	}
	for i, v := range expDire {
		if i < n {
			x[i] -= v
		}
	}
	return x
}

// predictorExtras is the number of non-hero features appended to the four
// multi-hot hero blocks: the step one-hot, the phase one-hot, the within-phase
// one-hot, the pick/ban flag, the "actor drafts first" flag and four board counts.
const predictorExtras = CMSteps + CMPhases + maxActionInPhase + 1 + 1 + 4

// PredictorLen is the next-pick predictor input length.
func (h *HeroMap) PredictorLen() int { return 4*h.Len() + predictorExtras }

// PredictorState is the bag-of-heroes view of a draft used to predict the next
// hero.
//
// Hero slices are given in absolute Radiant/Dire terms (that is how both the cache
// and the live draft state store them), but they are ENCODED relative to whoever
// is acting: "actor's picks" and "opponent's picks". A draft is symmetric under
// swapping sides, so folding the sides into actor/opponent halves the parameters
// the model must learn and doubles the data behind each one.
type PredictorState struct {
	RadiantPicks []int
	DirePicks    []int
	RadiantBans  []int
	DireBans     []int
	IsPick       bool // the upcoming action is a pick (false = ban)

	// Actor is the team about to act (0 radiant / 1 dire). It matters: the model
	// predicts what THAT team takes, and the two seats face different schedules.
	Actor int
	// Ord is the 0-based index of the upcoming action in the draft sequence.
	// Negative means unknown (falls back to the count already on the board).
	Ord int
	// FirstPick is the team that drafts first (0 radiant / 1 dire); the CM
	// schedule is not symmetric between the two seats.
	FirstPick int
}

// EncodePredictor builds the next-pick predictor feature vector. Layout:
//
//	[0:N]   actor picks
//	[N:2N]  opponent picks
//	[2N:3N] actor bans
//	[3N:4N] opponent bans
//	then: step one-hot (24), phase one-hot (6), action-in-phase one-hot (6),
//	pick/ban flag, "actor drafts first" flag, four normalized board counts.
func (h *HeroMap) EncodePredictor(s PredictorState) []float64 {
	n := h.Len()
	x := make([]float64, 4*n+predictorExtras)
	set := func(offset int, ids []int) {
		for _, id := range ids {
			if i, ok := h.Index(id); ok {
				x[offset+i] = 1
			}
		}
	}

	actorPicks, oppPicks := s.RadiantPicks, s.DirePicks
	actorBans, oppBans := s.RadiantBans, s.DireBans
	if s.Actor == teamDire {
		actorPicks, oppPicks = s.DirePicks, s.RadiantPicks
		actorBans, oppBans = s.DireBans, s.RadiantBans
	}
	set(0, actorPicks)
	set(n, oppPicks)
	set(2*n, actorBans)
	set(3*n, oppBans)

	base := 4 * n
	ord := s.Ord
	if ord < 0 {
		// Unknown step index: derive it from how much is already on the board.
		ord = len(s.RadiantPicks) + len(s.DirePicks) + len(s.RadiantBans) + len(s.DireBans)
	}
	if ord < CMSteps {
		x[base+ord] = 1
	}
	base += CMSteps

	phase, inPhase := PhaseOf(ord)
	if phase >= 0 {
		x[base+phase] = 1
		x[base+CMPhases+inPhase] = 1
	}
	base += CMPhases + maxActionInPhase

	if s.IsPick {
		x[base] = 1
	}
	base++
	if s.Actor == s.FirstPick {
		x[base] = 1
	}
	base++

	// Normalized board counts. Scaled to roughly [0,1] so they sit on the same
	// scale as the one-hot features and a single L2 penalty treats them alike.
	x[base] = float64(len(actorPicks)) / 5
	x[base+1] = float64(len(oppPicks)) / 5
	x[base+2] = float64(len(actorBans)) / 7
	x[base+3] = float64(len(oppBans)) / 7
	return x
}

// cmPhaseBounds is the number of actions in each Captains Mode phase, matching
// draft.DefaultOrder().Phases() — the 7.34-7.41 schedule: 7 bans, 2 picks,
// 3 bans, 6 picks, 4 bans, 2 picks. (A draft test pins the two together; the old
// 4/4/6/4/4/2 split described a schedule that has not existed since 7.33.)
var cmPhaseBounds = [CMPhases]int{7, 2, 3, 6, 4, 2}

// PhaseBounds returns the phase lengths the encoder assumes, so training can warn
// when the drafts it learns from were played under a different schedule.
func PhaseBounds() []int { return append([]int(nil), cmPhaseBounds[:]...) }

// PhaseOf maps a 0-based draft step index to its phase and its index within that
// phase. Returns (-1, -1) for out-of-range steps.
func PhaseOf(ord int) (phase, inPhase int) {
	if ord < 0 {
		return -1, -1
	}
	rem := ord
	for p, size := range cmPhaseBounds {
		if rem < size {
			return p, rem
		}
		rem -= size
	}
	return -1, -1
}
