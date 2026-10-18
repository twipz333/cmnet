package worldmap

// LandmarkID is the stable identity of a named map objective.
type LandmarkID string

const (
	LandmarkRoshanPitA       LandmarkID = "ROSHAN_PIT_A"
	LandmarkRoshanPitB       LandmarkID = "ROSHAN_PIT_B"
	LandmarkRadiantAncient   LandmarkID = "RADIANT_ANCIENT"
	LandmarkDireAncient      LandmarkID = "DIRE_ANCIENT"
	LandmarkRadiantTormentor LandmarkID = "RADIANT_TORMENTOR"
	LandmarkDireTormentor    LandmarkID = "DIRE_TORMENTOR"
)

func (id LandmarkID) String() string { return string(id) }

const (
	KindRoshanPit = "roshan_pit"
	KindAncient   = "ancient"
	KindTormentor = "tormentor"
)

func requiredLandmarks() []LandmarkID {
	return []LandmarkID{
		LandmarkRoshanPitA, LandmarkRoshanPitB,
		LandmarkRadiantAncient, LandmarkDireAncient,
		LandmarkRadiantTormentor, LandmarkDireTormentor,
	}
}

// Radii are macro proximity thresholds. They live in geometry, not the builder.
type Radii struct {
	Roshan    float64 `json:"roshan"`
	Tormentor float64 `json:"tormentor"`
	Base      float64 `json:"base"`
	Grouped   float64 `json:"grouped"`
}
