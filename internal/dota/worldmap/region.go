package worldmap

// RegionID is the stable identity of a macro region. Display names match the ID.
type RegionID string

const (
	RegionUnknown RegionID = "UNKNOWN"
	RegionOther   RegionID = "OTHER"

	RegionRadiantBase          RegionID = "RADIANT_BASE"
	RegionDireBase             RegionID = "DIRE_BASE"
	RegionRadiantHighground    RegionID = "RADIANT_HIGHGROUND"
	RegionDireHighground       RegionID = "DIRE_HIGHGROUND"
	RegionRadiantTriangle      RegionID = "RADIANT_TRIANGLE"
	RegionDireTriangle         RegionID = "DIRE_TRIANGLE"
	RegionTopLaneRadiant       RegionID = "TOP_LANE_RADIANT"
	RegionTopLaneCenter        RegionID = "TOP_LANE_CENTER"
	RegionTopLaneDire          RegionID = "TOP_LANE_DIRE"
	RegionMidLaneRadiant       RegionID = "MID_LANE_RADIANT"
	RegionMidLaneCenter        RegionID = "MID_LANE_CENTER"
	RegionMidLaneDire          RegionID = "MID_LANE_DIRE"
	RegionBotLaneRadiant       RegionID = "BOT_LANE_RADIANT"
	RegionBotLaneCenter        RegionID = "BOT_LANE_CENTER"
	RegionBotLaneDire          RegionID = "BOT_LANE_DIRE"
	RegionRadiantMainJungle    RegionID = "RADIANT_MAIN_JUNGLE"
	RegionDireMainJungle       RegionID = "DIRE_MAIN_JUNGLE"
	RegionRadiantOfflaneJungle RegionID = "RADIANT_OFFLANE_JUNGLE"
	RegionDireOfflaneJungle    RegionID = "DIRE_OFFLANE_JUNGLE"
	RegionTopRiver             RegionID = "TOP_RIVER"
	RegionBotRiver             RegionID = "BOT_RIVER"
)

func (r RegionID) String() string { return string(r) }

func requiredRegions() []RegionID {
	return []RegionID{
		RegionRadiantBase, RegionDireBase,
		RegionRadiantHighground, RegionDireHighground,
		RegionRadiantTriangle, RegionDireTriangle,
		RegionTopLaneRadiant, RegionTopLaneCenter, RegionTopLaneDire,
		RegionMidLaneRadiant, RegionMidLaneCenter, RegionMidLaneDire,
		RegionBotLaneRadiant, RegionBotLaneCenter, RegionBotLaneDire,
		RegionRadiantMainJungle, RegionDireMainJungle,
		RegionRadiantOfflaneJungle, RegionDireOfflaneJungle,
		RegionTopRiver, RegionBotRiver,
	}
}

func MacroRegions() []RegionID {
	out := append([]RegionID(nil), requiredRegions()...)
	return out
}
