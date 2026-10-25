package observe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExtractOptions controls a single Extract call.
type ExtractOptions struct {
	ReceivedAt time.Time
	Seq        uint64
}

// Extractor converts one GSI payload into a Batch. It has no tick memory.
type Extractor struct{}

// Extract parses body. Broken top-level JSON is the only fatal error.
func (Extractor) Extract(body []byte, opt ExtractOptions) (*Batch, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("gsi observe: invalid JSON")
	}
	var top map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&top); err != nil {
		return nil, fmt.Errorf("gsi observe: decode: %w", err)
	}

	receivedMs := int64(0)
	if !opt.ReceivedAt.IsZero() {
		receivedMs = opt.ReceivedAt.UTC().UnixMilli()
	}

	match := parseMatch(top["map"])
	var gameTime, clockTime *int
	if match.Present {
		gameTime, clockTime = match.GameTime, match.ClockTime
	}
	base := func(src Source) Meta {
		return gsiMeta(receivedMs, gameTime, clockTime, opt.Seq, src)
	}

	batch := &Batch{
		Meta:  base(SourceGSIMap),
		Match: match,
		Self:  parseSelf(top["player"], top["hero"], top["abilities"], top["items"]),
	}

	mm, heroes, creeps, wards, roshan, structs, present, diag := parseMinimap(top["minimap"])
	batch.Minimap = mm
	batch.MinimapPresent = present
	batch.Heroes = heroes
	batch.Creeps = creeps
	batch.Wards = wards
	batch.Roshan = roshan
	batch.Structures = structs
	batch.OwnBuildings = parseBuildings(top["buildings"])
	events, unknownEv := parseEvents(top["events"], base(SourceGSIEvents))
	batch.Events = events
	diag.UnknownEvents = unknownEv
	batch.Diagnostics = diag
	return batch, nil
}

func parseMatch(raw json.RawMessage) MatchObservation {
	fields, ok := asObject(raw)
	if !ok {
		return MatchObservation{}
	}
	m := MatchObservation{Present: true}
	m.GameTime = intVal(fields["game_time"])
	m.ClockTime = intVal(fields["clock_time"])
	m.GameState = stringPtr(fields["game_state"])
	m.RadiantScore = intVal(fields["radiant_score"])
	m.DireScore = intVal(fields["dire_score"])
	m.Daytime = boolVal(fields["daytime"])
	m.Paused = boolVal(fields["paused"])
	m.MatchID = stringPtr(fields["matchid"])
	m.WinTeam = stringPtr(fields["win_team"])
	return m
}

func parseSelf(playerRaw, heroRaw, abilRaw, itemRaw json.RawMessage) SelfObservation {
	player, hasPlayer := asObject(playerRaw)
	hero, hasHero := asObject(heroRaw)
	if !hasPlayer && !hasHero && len(abilRaw) == 0 && len(itemRaw) == 0 {
		return SelfObservation{}
	}
	s := SelfObservation{Present: true}
	if hasPlayer {
		s.Kills = intVal(player["kills"])
		s.Deaths = intVal(player["deaths"])
		s.Assists = intVal(player["assists"])
		s.LastHits = intVal(player["last_hits"])
		s.Denies = intVal(player["denies"])
		s.Gold = intVal(player["gold"])
		s.GoldReliable = intVal(player["gold_reliable"])
		s.GoldUnreliable = intVal(player["gold_unreliable"])
		s.GPM = intVal(player["gpm"])
		s.XPM = intVal(player["xpm"])
		s.PlayerSlot = intVal(player["player_slot"])
		s.TeamSlot = intVal(player["team_slot"])
		s.TeamName = stringPtr(player["team_name"])
		if s.TeamName != nil {
			s.Team = teamFromName(*s.TeamName)
		}
		if s.Team == TeamUnknown {
			if t := intVal(player["team"]); t != nil {
				s.Team = teamFromGSI(*t)
			}
		}
	}
	if hasHero {
		s.HeroID = intVal(hero["id"])
		s.HeroName = stringPtr(hero["name"])
		s.Level = intVal(hero["level"])
		s.XP = intVal(hero["xp"])
		s.Alive = boolVal(hero["alive"])
		s.RespawnSeconds = intVal(hero["respawn_seconds"])
		s.HP = intVal(hero["health"])
		s.MaxHP = intVal(hero["max_health"])
		s.HPPercent = intVal(hero["health_percent"])
		s.Mana = intVal(hero["mana"])
		s.MaxMana = intVal(hero["max_mana"])
		s.ManaPercent = intVal(hero["mana_percent"])
		s.BuybackCost = intVal(hero["buyback_cost"])
		s.BuybackCooldown = intVal(hero["buyback_cooldown"])
		s.Smoked = boolVal(hero["smoked"])
		s.AghanimsScepter = boolVal(hero["aghanims_scepter"])
		s.AghanimsShard = boolVal(hero["aghanims_shard"])
		if x := intVal(hero["xpos"]); x != nil {
			if y := intVal(hero["ypos"]); y != nil {
				s.Position = &Vec2{X: *x, Y: *y}
			}
		}
	}
	s.Abilities = parseAbilities(abilRaw)
	s.Items = parseItems(itemRaw)
	return s
}

func parseAbilities(raw json.RawMessage) []AbilityObservation {
	obj, ok := asObject(raw)
	if !ok {
		return nil
	}
	keys := sortedKeys(obj)
	out := make([]AbilityObservation, 0, len(keys))
	for _, k := range keys {
		if !strings.HasPrefix(k, "ability") {
			continue
		}
		fields, ok := asObject(obj[k])
		if !ok {
			continue
		}
		name := stringVal(fields["name"])
		if name == "" {
			continue
		}
		out = append(out, AbilityObservation{
			Slot:        k,
			Name:        name,
			Level:       intVal(fields["level"]),
			CanCast:     boolVal(fields["can_cast"]),
			Cooldown:    floatVal(fields["cooldown"]),
			MaxCooldown: floatVal(fields["max_cooldown"]),
			Ultimate:    boolVal(fields["ultimate"]),
			Charges:     intVal(fields["charges"]),
			MaxCharges:  intVal(fields["max_charges"]),
			Passive:     boolVal(fields["passive"]),
		})
	}
	return out
}

func parseItems(raw json.RawMessage) ItemSet {
	obj, ok := asObject(raw)
	if !ok {
		return ItemSet{}
	}
	var set ItemSet
	keys := sortedKeys(obj)
	for _, k := range keys {
		it := parseItemSlot(k, obj[k])
		if it == nil {
			continue
		}
		switch {
		case strings.HasPrefix(k, "slot"):
			n := trailingInt(k)
			if n >= 0 && n <= 5 {
				set.Inventory = append(set.Inventory, *it)
			} else if n >= 6 && n <= 8 {
				set.Backpack = append(set.Backpack, *it)
			}
		case strings.HasPrefix(k, "stash"):
			set.Stash = append(set.Stash, *it)
		case strings.HasPrefix(k, "teleport"):
			cp := *it
			set.TP = &cp
		case strings.HasPrefix(k, "neutral") && !strings.HasPrefix(k, "preserved_"):
			set.Neutral = append(set.Neutral, *it)
		}
	}
	return set
}

func parseItemSlot(slot string, raw json.RawMessage) *ItemObservation {
	fields, ok := asObject(raw)
	if !ok {
		return nil
	}
	name := stringVal(fields["name"])
	if name == "" || name == "empty" {
		return nil
	}
	charges := intVal(fields["charges"])
	if charges == nil {
		charges = intVal(fields["item_charges"])
	}
	return &ItemObservation{
		Slot:        slot,
		Name:        name,
		CanCast:     boolVal(fields["can_cast"]),
		Cooldown:    floatVal(fields["cooldown"]),
		MaxCooldown: floatVal(fields["max_cooldown"]),
		Charges:     charges,
		Passive:     boolVal(fields["passive"]),
		Purchaser:   intVal(fields["purchaser"]),
	}
}

func parseBuildings(raw json.RawMessage) []BuildingObservation {
	obj, ok := asObject(raw)
	if !ok {
		return nil
	}
	var out []BuildingObservation
	for _, teamKey := range sortedKeys(obj) {
		team := teamFromName(teamKey)
		inner, ok := asObject(obj[teamKey])
		if !ok {
			continue
		}
		for _, name := range sortedKeys(inner) {
			fields, ok := asObject(inner[name])
			if !ok {
				continue
			}
			unit := CanonicalUnitName(name)
			class, lane, namedTeam := classifyStructure(unit)
			if team == TeamUnknown {
				team = namedTeam
			}
			hp := intVal(fields["health"])
			maxHP := intVal(fields["max_health"])
			var alive *bool
			if hp != nil {
				v := *hp > 0
				alive = &v
			}
			out = append(out, BuildingObservation{
				UnitName:  unit,
				Team:      team,
				Class:     class,
				Lane:      lane,
				Health:    hp,
				MaxHealth: maxHP,
				Alive:     alive,
			})
		}
	}
	return out
}

func parseMinimap(raw json.RawMessage) (
	entities []MinimapEntity,
	heroes []HeroPosition,
	creeps []CreepObservation,
	wards []WardObservation,
	roshan []RoshanObservation,
	structs []StructureObservation,
	present bool,
	diag Diagnostics,
) {
	obj, ok := asObject(raw)
	if !ok {
		return
	}
	present = true
	keys := sortedKeys(obj)
	entities = make([]MinimapEntity, 0, len(keys))
	for _, k := range keys {
		fields, ok := asObject(obj[k])
		if !ok {
			diag.UnknownMinimapUnits++
			continue
		}
		ent := MinimapEntity{
			PayloadKey:      k,
			UnitName:        stringVal(fields["unitname"]),
			DisplayName:     stringVal(fields["name"]),
			Image:           stringVal(fields["image"]),
			ObservationTime: intVal(fields["observation_time"]),
			Yaw:             intVal(fields["yaw"]),
			VisionRange:     intVal(fields["visionrange"]),
		}
		if t := intVal(fields["team"]); t != nil {
			ent.Team = teamFromGSI(*t)
		}
		x := intVal(fields["xpos"])
		y := intVal(fields["ypos"])
		if x != nil && y != nil {
			ent.Position = Vec2{X: *x, Y: *y}
		}
		ent.Kind = classifyUnitName(ent.UnitName)
		entities = append(entities, ent)
		diag.MinimapEntities++

		switch ent.Kind {
		case KindHero:
			if hp, drop := heroFromEntity(ent); drop {
				diag.DroppedHeroNoise++
			} else if hp != nil {
				heroes = append(heroes, *hp)
			} else {
				diag.DroppedHeroNoise++
			}
		case KindCreep:
			creeps = append(creeps, CreepObservation{
				UnitName: ent.UnitName,
				Team:     ent.Team,
				Class:    classifyCreep(ent.UnitName),
				Position: ent.Position,
				Image:    ent.Image,
			})
		case KindWard:
			if ent.Image == ImagePlainCircle || ent.Position.Origin() {
				continue
			}
			wards = append(wards, WardObservation{
				UnitName: ent.UnitName,
				Team:     ent.Team,
				Class:    classifyWard(ent.UnitName),
				Position: ent.Position,
				Image:    ent.Image,
			})
		case KindRoshan:
			if ent.Position.Origin() {
				continue
			}
			roshan = append(roshan, RoshanObservation{
				Position:    ent.Position,
				Image:       ent.Image,
				Team:        ent.Team,
				VisionRange: ent.VisionRange,
			})
		case KindStructure:
			pos := ent.Position
			structs = append(structs, StructureObservation{
				UnitName: ent.UnitName,
				Team:     ent.Team,
				Class:    firstStructureClass(ent.UnitName),
				Lane:     firstStructureLane(ent.UnitName),
				Position: &pos,
				Image:    ent.Image,
			})
		case KindUnknown:
			diag.UnknownMinimapUnits++
		}
	}
	sortHeroes(heroes)
	sortCreeps(creeps)
	sortWards(wards)
	sortRoshan(roshan)
	sortStructures(structs)
	return
}

func firstStructureClass(unit string) StructureClass {
	c, _, _ := classifyStructure(unit)
	return c
}

func firstStructureLane(unit string) LaneHint {
	_, l, _ := classifyStructure(unit)
	return l
}

func heroFromEntity(ent MinimapEntity) (*HeroPosition, bool) {
	kind := ClassifyHeroImage(ent.Image)
	if kind == HeroImageNoise {
		return nil, true
	}
	if ent.Position.Origin() {
		return nil, true
	}
	if kind == HeroImageUnknown {
		return nil, true
	}
	if kind == HeroImageAllied && ent.Team == TeamRadiant {
		// Allied icons belong on our team; a herocircle on the enemy team id
		// does not appear in the capture. Keep the observation anyway if the
		// image is allied — roster+side filtering happens in the tracker.
	}
	return &HeroPosition{
		Hero:        ent.UnitName,
		Team:        ent.Team,
		Position:    ent.Position,
		Image:       ent.Image,
		ImageKind:   kind,
		Yaw:         ent.Yaw,
		VisionRange: ent.VisionRange,
	}, false
}

func sortHeroes(hs []HeroPosition) {
	sort.Slice(hs, func(i, j int) bool {
		a, b := hs[i], hs[j]
		if a.Hero != b.Hero {
			return a.Hero < b.Hero
		}
		if a.Position.X != b.Position.X {
			return a.Position.X < b.Position.X
		}
		if a.Position.Y != b.Position.Y {
			return a.Position.Y < b.Position.Y
		}
		return a.Image < b.Image
	})
}

func sortCreeps(cs []CreepObservation) {
	sort.Slice(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.UnitName != b.UnitName {
			return a.UnitName < b.UnitName
		}
		if a.Position.X != b.Position.X {
			return a.Position.X < b.Position.X
		}
		return a.Position.Y < b.Position.Y
	})
}

func sortWards(ws []WardObservation) {
	sort.Slice(ws, func(i, j int) bool {
		a, b := ws[i], ws[j]
		if a.UnitName != b.UnitName {
			return a.UnitName < b.UnitName
		}
		if a.Position.X != b.Position.X {
			return a.Position.X < b.Position.X
		}
		return a.Position.Y < b.Position.Y
	})
}

func sortRoshan(rs []RoshanObservation) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Position.X != rs[j].Position.X {
			return rs[i].Position.X < rs[j].Position.X
		}
		return rs[i].Position.Y < rs[j].Position.Y
	})
}

func sortStructures(ss []StructureObservation) {
	sort.Slice(ss, func(i, j int) bool {
		return ss[i].UnitName < ss[j].UnitName
	})
}

func asObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 || trim[0] != '{' {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}
	return obj, true
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func intVal(raw json.RawMessage) *int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return &n
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		v := int(f)
		return &v
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.Atoi(s); err == nil {
			return &v
		}
	}
	return nil
}

func floatVal(raw json.RawMessage) *float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return &f
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		v := float64(n)
		return &v
	}
	return nil
}

func boolVal(raw json.RawMessage) *bool {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return &b
	}
	return nil
}

func stringVal(raw json.RawMessage) string {
	if p := stringPtr(raw); p != nil {
		return *p
	}
	return ""
}

func stringPtr(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	return &s
}

func trailingInt(s string) int {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return -1
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return -1
	}
	return n
}
