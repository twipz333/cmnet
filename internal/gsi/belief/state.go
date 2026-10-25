package belief

import "cmnet/internal/dota/worldmap"

type Status string

const (
	StatusObserved          Status = "OBSERVED"
	StatusObservedAmbiguous Status = "OBSERVED_AMBIGUOUS"
	StatusMissing           Status = "MISSING"
	StatusUnlocalized       Status = "UNLOCALIZED"
)

type RegionReachability struct {
	Region            worldmap.RegionID
	MinWalkDistance   float64
	MinWalkTime       float64
	ReachableByWalkV0 bool
}

type RegionMass struct {
	Region worldmap.RegionID
	Mass   float64
}

type EnemyBelief struct {
	Hero string

	Status  Status
	Visible bool

	CurrentRegions  []worldmap.RegionID
	LastSeenRegions []worldmap.RegionID
	MissingAge      float64

	WalkingReachability []RegionReachability

	RegionMass    map[worldmap.RegionID]float64
	UnmodeledMass float64

	TopRegions []RegionMass
	Entropy    float64

	ModelKind ModelKind
	Reason    string
}

type BuildStats struct {
	BeliefBuilds                    int
	ObservedEnemies                 int
	MissingEnemies                  int
	UnlocalizedEnemies              int
	MultiSeedBeliefs                int
	WalkingUnreachableReappearances int
	BeliefErrors                    int
}

type State struct {
	ModelVersion    string
	GraphVersion    string
	GraphMapVersion string
	Enemies         []EnemyBelief
	Stats           BuildStats
}

func (e EnemyBelief) TopRegionsN(n int) []RegionMass {
	if n <= 0 || len(e.TopRegions) == 0 {
		return nil
	}
	if n > len(e.TopRegions) {
		n = len(e.TopRegions)
	}
	out := make([]RegionMass, n)
	copy(out, e.TopRegions[:n])
	return out
}
