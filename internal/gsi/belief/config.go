package belief

import "fmt"

const ModelVersion = "heuristic-v0.1"

// ModelKind identifies how belief mass was produced.
type ModelKind string

const (
	ModelKindHeuristicV0 ModelKind = "HEURISTIC_V0"
)

// Config holds all heuristic walking and uncertainty knobs for BeliefBuilder v0.
type Config struct {
	ReferenceWalkSpeed    float64
	ReachabilityWalkSpeed float64
	ReachabilitySlack     float64
	TimeFloorSeconds      float64

	UnmodeledInitial         float64
	UnmodeledGrowthPerSecond float64
	UnmodeledMax             float64
	TopRegions               int
}

func DefaultConfig() Config {
	return Config{
		ReferenceWalkSpeed:       420,
		ReachabilityWalkSpeed:    500,
		ReachabilitySlack:        2,
		TimeFloorSeconds:         4,
		UnmodeledInitial:         0.05,
		UnmodeledGrowthPerSecond: 0.004,
		UnmodeledMax:             0.50,
		TopRegions:               5,
	}
}

func (c Config) Validate() error {
	if c.ReferenceWalkSpeed <= 0 {
		return fmt.Errorf("belief: reference_walk_speed must be > 0")
	}
	if c.ReachabilityWalkSpeed <= 0 {
		return fmt.Errorf("belief: reachability_walk_speed must be > 0")
	}
	if c.ReachabilitySlack < 0 {
		return fmt.Errorf("belief: reachability_slack must be >= 0")
	}
	if c.TimeFloorSeconds <= 0 {
		return fmt.Errorf("belief: time_floor_seconds must be > 0")
	}
	if c.UnmodeledInitial < 0 || c.UnmodeledInitial > 1 {
		return fmt.Errorf("belief: unmodeled_initial must be in [0,1]")
	}
	if c.UnmodeledGrowthPerSecond < 0 {
		return fmt.Errorf("belief: unmodeled_growth_per_second must be >= 0")
	}
	if c.UnmodeledMax < c.UnmodeledInitial || c.UnmodeledMax > 1 {
		return fmt.Errorf("belief: unmodeled_max must be in [initial,1]")
	}
	if c.TopRegions < 0 {
		return fmt.Errorf("belief: top_regions must be >= 0")
	}
	return nil
}

func (c Config) referenceTravelSeconds(distance float64) float64 {
	return distance / c.ReferenceWalkSpeed
}

func (c Config) reachabilityTravelSeconds(distance float64) float64 {
	return distance / c.ReachabilityWalkSpeed
}

func (c Config) unmodeledMass(age float64, invalidSeedShare float64) float64 {
	v := c.UnmodeledInitial + c.UnmodeledGrowthPerSecond*age
	if v > c.UnmodeledMax {
		v = c.UnmodeledMax
	}
	if invalidSeedShare > 0 {
		v = 1 - (1-v)*(1-invalidSeedShare)
	}
	return clamp01(v)
}
