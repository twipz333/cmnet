package roles

import (
	"math"
	"testing"

	"cmnet/internal/cache"
)

func pos(p int) *int { return &p }

func TestPosteriorSumsToOne(t *testing.T) {
	prior := &Prior{Prob: map[int][NumPositions]float64{
		1: {0.8, 0.05, 0.05, 0.05, 0.05},
		2: {0.05, 0.8, 0.05, 0.05, 0.05},
		3: {0.05, 0.05, 0.8, 0.05, 0.05},
	}}
	elig := map[int][]int{1: {1, 2}, 2: {2, 3}, 3: {3, 4}}
	asg, w := Posterior([]int{1, 2, 3}, prior, elig)
	if len(asg) == 0 {
		t.Fatal("expected assignments")
	}
	var sum float64
	for _, v := range w {
		sum += v
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("weights sum to %v, want 1", sum)
	}
}

func TestSoftOccupancyDiffersFromHardOnPartial(t *testing.T) {
	prior := &Prior{Prob: map[int][NumPositions]float64{
		10: {0.5, 0.5, 0, 0, 0},
		20: {0.5, 0.5, 0, 0, 0},
	}}
	elig := map[int][]int{10: {1, 2}, 20: {1, 2}}
	occ := ExpectedOccupancy([]int{10, 20}, prior, elig)
	if occ[10][0] <= 0 || occ[10][1] <= 0 {
		t.Fatalf("hero 10 occupancy = %v, want mass on both pos 1 and 2", occ[10])
	}
	for _, h := range []int{10, 20} {
		ones := 0
		for _, v := range occ[h] {
			if v > 0.99 {
				ones++
			}
		}
		if ones == 1 {
			t.Fatalf("occupancy for %d is hard one-hot %v under equal prior", h, occ[h])
		}
	}
}

func TestMAPUniqueMatchingIsHard(t *testing.T) {
	prior := &Prior{Prob: map[int][NumPositions]float64{
		1: {1, 0, 0, 0, 0},
		2: {0, 1, 0, 0, 0},
	}}
	elig := map[int][]int{1: {1}, 2: {2}}
	occ := ExpectedOccupancy([]int{1, 2}, prior, elig)
	if math.Abs(occ[1][0]-1) > 1e-9 || math.Abs(occ[2][1]-1) > 1e-9 {
		t.Fatalf("unique matching should be hard: %v", occ)
	}
	if got := MAP([]int{1, 2}, prior, elig); got[1] != 1 || got[2] != 2 {
		t.Fatalf("MAP = %v", got)
	}
}

func TestBuildPriorFromMatches(t *testing.T) {
	matches := []cache.Match{{
		Players: []cache.MatchPlayer{
			{HeroID: 7, Position: pos(1)},
			{HeroID: 7, Position: pos(1)},
			{HeroID: 7, Position: pos(3)},
		},
	}}
	p := BuildPrior(matches, 0)
	if p.Mode(7) != 1 {
		t.Fatalf("mode = %d, want 1", p.Mode(7))
	}
	row := p.Prob[7]
	if math.Abs(row[0]-2.0/3) > 1e-9 || math.Abs(row[2]-1.0/3) > 1e-9 {
		t.Fatalf("prob = %v", row)
	}
}

func TestCoverageDiffNonZero(t *testing.T) {
	prior := &Prior{Prob: map[int][NumPositions]float64{
		1: {1, 0, 0, 0, 0},
		2: {1, 0, 0, 0, 0},
		3: {0, 0, 0, 0, 1},
		4: {0, 0, 0, 1, 0},
		5: {0, 0, 1, 0, 0},
	}}
	elig := map[int][]int{
		1: {1}, 2: {1}, 3: {5}, 4: {4}, 5: {3},
	}
	diff := CoverageDiff([]int{3, 4, 5}, []int{1, 2}, prior, elig)
	if diff <= 0 {
		t.Fatalf("diff = %v, want radiant ahead", diff)
	}
}

func TestEnumerateDeterministic(t *testing.T) {
	elig := map[int][]int{3: {1, 2}, 1: {1, 2}}
	a := Enumerate([]int{3, 1}, elig)
	b := Enumerate([]int{1, 3}, elig)
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("len a=%d b=%d", len(a), len(b))
	}
	for i := range a {
		if a[i][1] != b[i][1] || a[i][3] != b[i][3] {
			t.Fatalf("order differs at %d: %v vs %v", i, a[i], b[i])
		}
	}
}