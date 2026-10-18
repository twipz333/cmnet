package observe

// Source identifies where a field or observation came from.
type Source string

const (
	SourceGSIMap       Source = "GSI_MAP"
	SourceGSIPlayer    Source = "GSI_PLAYER"
	SourceGSIHero      Source = "GSI_HERO"
	SourceGSIAbilities Source = "GSI_ABILITIES"
	SourceGSIItems     Source = "GSI_ITEMS"
	SourceGSIBuildings Source = "GSI_BUILDINGS"
	SourceGSIMinimap   Source = "GSI_MINIMAP"
	SourceGSIEvents    Source = "GSI_EVENTS"

	SourceAdvisorDraft Source = "ADVISOR_DRAFT"
	SourceHistory      Source = "HISTORY"
	SourceDerived      Source = "DERIVED"
)

// Meta is attached to a direct observation. Direct GSI observations use
// Confidence 1.0; later layers may fill in lower values.
type Meta struct {
	ReceivedAt int64  // unix milliseconds; 0 if unknown
	GameTime   *int   // GSI map.game_time when present
	ClockTime  *int   // GSI map.clock_time when present
	Seq        uint64 // receive/replay order; 0 if unset
	Source     Source
	Confidence float64
}

func gsiMeta(receivedAtMs int64, gameTime, clockTime *int, seq uint64, src Source) Meta {
	return Meta{
		ReceivedAt: receivedAtMs,
		GameTime:   gameTime,
		ClockTime:  clockTime,
		Seq:        seq,
		Source:     src,
		Confidence: 1.0,
	}
}
