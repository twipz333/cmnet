package observe

import "strings"

// Minimap image tokens observed in a real player-mode capture. Keep them in
// one place so hero filtering is not a magic string sprinkled across files.
const (
	ImageHeroCircle     = "minimap_herocircle"
	ImageHeroCircleSelf = "minimap_herocircle_self"
	ImageHeroInvis      = "minimap_heroinvis"
	ImageEnemyIcon      = "minimap_enemyicon"
	ImageHeroTeleport   = "minimap_ping_teleporting"
	ImageHeroSpeech     = "minimap_speech"

	ImagePlainCircle = "minimap_plaincircle"
	ImageCreep       = "minimap_creep"
	ImageSiege       = "minimap_siege"
	ImageWardObs     = "minimap_ward_obs"
	ImageWardInvis   = "minimap_ward_invis"
	ImageTower45     = "minimap_tower45"
	ImageTower90     = "minimap_tower90"
	ImageRacks45     = "minimap_racks45"
	ImageRacks90     = "minimap_racks90"
	ImageAncient     = "minimap_ancient"
)

var alliedHeroImages = map[string]struct{}{
	ImageHeroCircle:     {},
	ImageHeroCircleSelf: {},
	ImageHeroInvis:      {},
	ImageHeroTeleport:   {},
	ImageHeroSpeech:     {},
}

var enemyHeroImages = map[string]struct{}{
	ImageEnemyIcon: {},
}

// HeroImageKind classifies a minimap image for a hero-like entity.
type HeroImageKind int

const (
	HeroImageUnknown HeroImageKind = iota
	HeroImageAllied
	HeroImageEnemy
	HeroImageNoise
)

// ClassifyHeroImage reports how a minimap image should be treated for hero
// tracking. Plain-circle icons are noise, not a player observation.
func ClassifyHeroImage(image string) HeroImageKind {
	if image == ImagePlainCircle {
		return HeroImageNoise
	}
	if _, ok := alliedHeroImages[image]; ok {
		return HeroImageAllied
	}
	if _, ok := enemyHeroImages[image]; ok {
		return HeroImageEnemy
	}
	return HeroImageUnknown
}

// EntityKind is a cheap prefix classification of a minimap unitname.
type EntityKind int

const (
	KindUnknown EntityKind = iota
	KindHero
	KindCreep
	KindWard
	KindStructure
	KindRoshan
	KindOther
)

// CreepClass is the coarse creep type exposed to later lane builders.
type CreepClass int

const (
	CreepOther CreepClass = iota
	CreepMelee
	CreepRanged
	CreepFlagbearer
	CreepSiege
)

func (c CreepClass) String() string {
	switch c {
	case CreepMelee:
		return "melee"
	case CreepRanged:
		return "ranged"
	case CreepFlagbearer:
		return "flagbearer"
	case CreepSiege:
		return "siege"
	default:
		return "other"
	}
}

// IsStandardLane reports melee/ranged/flagbearer/siege, including upgraded/mega
// names that classify into those classes. Neutrals and summons are not this class.
func (c CreepClass) IsStandardLane() bool {
	switch c {
	case CreepMelee, CreepRanged, CreepFlagbearer, CreepSiege:
		return true
	default:
		return false
	}
}

// WardClass is observer vs sentry.
type WardClass int

const (
	WardUnknown WardClass = iota
	WardObserver
	WardSentry
)

func (c WardClass) String() string {
	switch c {
	case WardObserver:
		return "observer"
	case WardSentry:
		return "sentry"
	default:
		return "unknown"
	}
}

// StructureClass is a stable building kind parsed from unitname.
type StructureClass int

const (
	StructureUnknown StructureClass = iota
	StructureTowerT1
	StructureTowerT2
	StructureTowerT3
	StructureTowerT4
	StructureMeleeRax
	StructureRangedRax
	StructureAncient
)

func (c StructureClass) String() string {
	switch c {
	case StructureTowerT1:
		return "tower_t1"
	case StructureTowerT2:
		return "tower_t2"
	case StructureTowerT3:
		return "tower_t3"
	case StructureTowerT4:
		return "tower_t4"
	case StructureMeleeRax:
		return "melee_rax"
	case StructureRangedRax:
		return "ranged_rax"
	case StructureAncient:
		return "ancient"
	default:
		return "unknown"
	}
}

func (c StructureClass) IsTower() bool {
	return c == StructureTowerT1 || c == StructureTowerT2 || c == StructureTowerT3 || c == StructureTowerT4
}

func (c StructureClass) IsBarracks() bool {
	return c == StructureMeleeRax || c == StructureRangedRax
}

// LaneHint is a name-derived lane (from the unit suffix), not a coordinate classifier.
type LaneHint int

const (
	LaneUnknown LaneHint = iota
	LaneTop
	LaneMid
	LaneBot
)

func (l LaneHint) String() string {
	switch l {
	case LaneTop:
		return "top"
	case LaneMid:
		return "mid"
	case LaneBot:
		return "bot"
	default:
		return ""
	}
}

func classifyUnitName(unit string) EntityKind {
	if unit == "" {
		return KindUnknown
	}
	if strings.HasPrefix(unit, "npc_dota_hero_") {
		return KindHero
	}
	if unit == "npc_dota_roshan" {
		return KindRoshan
	}
	if unit == "npc_dota_observer_wards" || unit == "npc_dota_sentry_wards" {
		return KindWard
	}
	if isCreepUnit(unit) {
		return KindCreep
	}
	if isStructureUnit(unit) {
		return KindStructure
	}
	return KindOther
}

func isCreepUnit(unit string) bool {
	return strings.Contains(unit, "_creep_") || strings.Contains(unit, "_siege")
}

func isStructureUnit(unit string) bool {
	if !(strings.Contains(unit, "goodguys") || strings.Contains(unit, "badguys")) {
		return false
	}
	return strings.Contains(unit, "_tower") || strings.Contains(unit, "_rax_") || strings.HasSuffix(unit, "_fort")
}

func classifyCreep(unit string) CreepClass {
	switch {
	case strings.Contains(unit, "flagbearer"):
		return CreepFlagbearer
	case strings.Contains(unit, "ranged"):
		return CreepRanged
	case strings.Contains(unit, "melee"):
		return CreepMelee
	case strings.Contains(unit, "siege"):
		return CreepSiege
	default:
		return CreepOther
	}
}

func classifyWard(unit string) WardClass {
	switch unit {
	case "npc_dota_observer_wards":
		return WardObserver
	case "npc_dota_sentry_wards":
		return WardSentry
	default:
		return WardUnknown
	}
}

func classifyStructure(unit string) (StructureClass, LaneHint, Team) {
	u := strings.TrimPrefix(unit, "npc_")
	team := TeamUnknown
	switch {
	case strings.Contains(u, "goodguys"):
		team = TeamRadiant
	case strings.Contains(u, "badguys"):
		team = TeamDire
	}
	lane := LaneUnknown
	switch {
	case strings.HasSuffix(u, "_top"):
		lane = LaneTop
	case strings.HasSuffix(u, "_mid"):
		lane = LaneMid
	case strings.HasSuffix(u, "_bot"):
		lane = LaneBot
	}
	class := StructureUnknown
	switch {
	case strings.Contains(u, "tower1"):
		class = StructureTowerT1
	case strings.Contains(u, "tower2"):
		class = StructureTowerT2
	case strings.Contains(u, "tower3"):
		class = StructureTowerT3
	case strings.Contains(u, "tower4"):
		class = StructureTowerT4
	case strings.Contains(u, "melee_rax"):
		class = StructureMeleeRax
	case strings.Contains(u, "range_rax"):
		class = StructureRangedRax
	case strings.HasSuffix(u, "_fort"):
		class = StructureAncient
	}
	return class, lane, team
}

// CanonicalUnitName maps GSI buildings-block keys (dota_badguys_tower1_top)
// onto the minimap form (npc_dota_badguys_tower1_top).
func CanonicalUnitName(name string) string {
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "npc_") {
		return name
	}
	if strings.HasPrefix(name, "dota_") {
		return "npc_" + name
	}
	return name
}
