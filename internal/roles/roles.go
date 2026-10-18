package roles

import (
	"math"
	"sort"

	"cmnet/internal/cache"
)

// NumPositions is the Captains Mode lane count (1-5).
const NumPositions = 5

// Laplace is the default additive smoothing for P(pos|hero) counts.
const Laplace = 1.0

// Prior is P(position | hero) estimated from finished CM match_players rows.
//
// Positions are 1-5. Heroes never observed fall back to a uniform distribution
// so they do not zero out an otherwise valid matching.
type Prior struct {
	// Prob[heroID][pos-1] sums to 1 for every hero that has an entry. Missing
	// heroes are treated as uniform by LogProb/Mode.
	Prob map[int][NumPositions]float64
}

// BuildPrior counts (hero, position) pairs across matches and Laplace-smooths.
// Rows without a position are skipped. Deterministic: iteration order does not
// affect the resulting probabilities.
func BuildPrior(matches []cache.Match, alpha float64) *Prior {
	if alpha < 0 {
		alpha = Laplace
	}
	counts := map[int]*[NumPositions]float64{}
	for _, m := range matches {
		for _, p := range m.Players {
			if p.Position == nil {
				continue
			}
			pos := *p.Position
			if pos < 1 || pos > NumPositions || p.HeroID == 0 {
				continue
			}
			row := counts[p.HeroID]
			if row == nil {
				row = &[NumPositions]float64{}
				counts[p.HeroID] = row
			}
			row[pos-1]++
		}
	}
	out := make(map[int][NumPositions]float64, len(counts))
	for hero, row := range counts {
		var sum float64
		var soft [NumPositions]float64
		for i := 0; i < NumPositions; i++ {
			soft[i] = row[i] + alpha
			sum += soft[i]
		}
		for i := 0; i < NumPositions; i++ {
			soft[i] /= sum
		}
		out[hero] = soft
	}
	return &Prior{Prob: out}
}

// LogProb returns log P(pos|hero). Uniform prior when the hero is unseen.
func (p *Prior) LogProb(hero, pos int) float64 {
	if pos < 1 || pos > NumPositions {
		return math.Inf(-1)
	}
	if p == nil || len(p.Prob) == 0 {
		return -math.Log(NumPositions)
	}
	row, ok := p.Prob[hero]
	if !ok {
		return -math.Log(NumPositions)
	}
	v := row[pos-1]
	if v <= 0 {
		return math.Inf(-1)
	}
	return math.Log(v)
}

// Mode returns the most probable position for hero (1-5). Ties break toward the
// lower position id. Unseen heroes return 1.
func (p *Prior) Mode(hero int) int {
	if p == nil || len(p.Prob) == 0 {
		return 1
	}
	row, ok := p.Prob[hero]
	if !ok {
		return 1
	}
	bestPos, best := 1, row[0]
	for i := 1; i < NumPositions; i++ {
		if row[i] > best {
			best, bestPos = row[i], i+1
		}
	}
	return bestPos
}

// Assignment is a hero id -> position (1-5) bijection.
type Assignment map[int]int

// eligible returns the positions a hero may occupy. Unknown heroes may fill any
// lane — the same convention beam.coverage uses — so a missing role table does
// not invent false conflicts.
func eligible(hero int, elig map[int][]int) []int {
	if elig != nil {
		if ps, ok := elig[hero]; ok && len(ps) > 0 {
			out := make([]int, 0, len(ps))
			for _, p := range ps {
				if p >= 1 && p <= NumPositions {
					out = append(out, p)
				}
			}
			if len(out) > 0 {
				sort.Ints(out)
				return out
			}
		}
	}
	return []int{1, 2, 3, 4, 5}
}

// Enumerate lists every injection of heroes into distinct positions 1-5 that
// respects eligibility. Heroes are sorted by id first so the listing order is
// platform-stable (§6). Empty when no complete injection exists (role conflict).
func Enumerate(heroes []int, elig map[int][]int) []Assignment {
	if len(heroes) == 0 || len(heroes) > NumPositions {
		return nil
	}
	sorted := append([]int(nil), heroes...)
	sort.Ints(sorted)

	posOf := make([][]int, len(sorted))
	for i, h := range sorted {
		posOf[i] = eligible(h, elig)
	}

	var out []Assignment
	var used [NumPositions + 1]bool
	cur := make(Assignment, len(sorted))
	var walk func(i int)
	walk = func(i int) {
		if i == len(sorted) {
			cp := make(Assignment, len(cur))
			for h, p := range cur {
				cp[h] = p
			}
			out = append(out, cp)
			return
		}
		for _, pos := range posOf[i] {
			if used[pos] {
				continue
			}
			used[pos] = true
			cur[sorted[i]] = pos
			walk(i + 1)
			delete(cur, sorted[i])
			used[pos] = false
		}
	}
	walk(0)
	return out
}

// Posterior returns valid assignments with a softmax weight proportional to
// exp(Σ_hero log P(pos|hero)). Weights sum to 1. Empty when no assignment is
// legal — callers should fall back to max-matching coverage.
//
// Independent per-hero marginals are NOT multiplied into a joint: five heroes on
// five roles is a bijection, and a product of marginals is not a distribution
// over bijections (Codex HIGH on §6.3).
func Posterior(heroes []int, prior *Prior, elig map[int][]int) (assignments []Assignment, weights []float64) {
	assignments = Enumerate(heroes, elig)
	if len(assignments) == 0 {
		return nil, nil
	}
	logits := make([]float64, len(assignments))
	maxLog := math.Inf(-1)
	for i, a := range assignments {
		var s float64
		for h, pos := range a {
			s += prior.LogProb(h, pos)
		}
		logits[i] = s
		if s > maxLog {
			maxLog = s
		}
	}
	weights = make([]float64, len(assignments))
	var sum float64
	for i, z := range logits {
		w := math.Exp(z - maxLog)
		weights[i] = w
		sum += w
	}
	if sum <= 0 {
		u := 1 / float64(len(weights))
		for i := range weights {
			weights[i] = u
		}
		return assignments, weights
	}
	for i := range weights {
		weights[i] /= sum
	}
	return assignments, weights
}

// ExpectedOccupancy returns, for each hero, the posterior mass on each position.
func ExpectedOccupancy(heroes []int, prior *Prior, elig map[int][]int) map[int][NumPositions]float64 {
	asg, w := Posterior(heroes, prior, elig)
	out := make(map[int][NumPositions]float64, len(heroes))
	if len(asg) == 0 {
		for _, h := range heroes {
			var row [NumPositions]float64
			var sum float64
			for _, pos := range eligible(h, elig) {
				lp := math.Exp(prior.LogProb(h, pos))
				row[pos-1] = lp
				sum += lp
			}
			if sum <= 0 {
				u := 1 / float64(NumPositions)
				for i := range row {
					row[i] = u
				}
			} else {
				for i := range row {
					row[i] /= sum
				}
			}
			out[h] = row
		}
		return out
	}
	for i, a := range asg {
		for h, pos := range a {
			row := out[h]
			row[pos-1] += w[i]
			out[h] = row
		}
	}
	return out
}

// MAP returns the highest-weight assignment, or nil when none is legal.
// Ties keep the first assignment Enumerate produced (hero-id order).
func MAP(heroes []int, prior *Prior, elig map[int][]int) Assignment {
	asg, w := Posterior(heroes, prior, elig)
	if len(asg) == 0 {
		return nil
	}
	best := 0
	for i := 1; i < len(w); i++ {
		if w[i] > w[best] {
			best = i
		}
	}
	return asg[best]
}

// MaxCoverage is the size of a maximum hero->position matching (Kuhn), 0..5.
func MaxCoverage(heroes []int, elig map[int][]int) int {
	if len(heroes) == 0 {
		return 0
	}
	matchOf := [NumPositions + 1]int{}
	for i := range matchOf {
		matchOf[i] = -1
	}
	posOf := make([][]int, len(heroes))
	for i, h := range heroes {
		posOf[i] = eligible(h, elig)
	}
	var try func(hi int, seen *[NumPositions + 1]bool) bool
	try = func(hi int, seen *[NumPositions + 1]bool) bool {
		for _, pos := range posOf[hi] {
			if seen[pos] {
				continue
			}
			seen[pos] = true
			if matchOf[pos] == -1 || try(matchOf[pos], seen) {
				matchOf[pos] = hi
				return true
			}
		}
		return false
	}
	count := 0
	for hi := range heroes {
		var seen [NumPositions + 1]bool
		if try(hi, &seen) {
			count++
		}
	}
	return count
}

// SoftCoverage is the expected number of distinct positions the lineup occupies
// under the assignment posterior, capped at 1 mass per position.
func SoftCoverage(heroes []int, prior *Prior, elig map[int][]int) float64 {
	if len(heroes) == 0 {
		return 0
	}
	occ := ExpectedOccupancy(heroes, prior, elig)
	var fill [NumPositions]float64
	for _, row := range occ {
		for i := 0; i < NumPositions; i++ {
			fill[i] += row[i]
		}
	}
	var s float64
	for _, f := range fill {
		if f > 1 {
			f = 1
		}
		s += f
	}
	return s
}

// CoverageDiff is Radiant's soft lane-coverage advantage, normalized to roughly [-1, 1].
func CoverageDiff(radiant, dire []int, prior *Prior, elig map[int][]int) float64 {
	return (SoftCoverage(radiant, prior, elig) - SoftCoverage(dire, prior, elig)) / NumPositions
}
