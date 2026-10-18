package observe

import (
	"encoding/json"
	"strconv"
	"strings"
)

// EventKind is a normalized GSI event. Names follow the values actually seen
// in a player-mode capture: top-level event_type plus CHAT_MESSAGE_* inside
// generic_event.data.
type EventKind string

const (
	EventUnknown EventKind = "unknown"

	EventHeroKill           EventKind = "HERO_KILL"
	EventTowerKill          EventKind = "TOWER_KILL"
	EventBarracksKill       EventKind = "BARRACKS_KILL"
	EventItemPurchase       EventKind = "ITEM_PURCHASE"
	EventSmokeActivated     EventKind = "SMOKE_ACTIVATED"
	EventGlyphUsed          EventKind = "GLYPH_USED"
	EventScanUsed           EventKind = "SCAN_USED"
	EventObserverWardKilled EventKind = "OBSERVER_WARD_KILLED"
	EventSentryWardKilled   EventKind = "SENTRY_WARD_KILLED"
	EventCourierLost        EventKind = "COURIER_LOST"
	EventCourierRespawned   EventKind = "COURIER_RESPAWNED"
	EventBountyRunePickup   EventKind = "BOUNTY_RUNE_PICKUP"

	EventChatMessage EventKind = "CHAT_MESSAGE"
	EventTip         EventKind = "TIP"
)

// Event is one GSI event after unfolding generic_event.data.
type Event struct {
	Kind EventKind
	Meta Meta

	RawType   string // event_type as sent (generic_event, bounty_rune_pickup, ...)
	InnerType string // CHAT_MESSAGE_* when present

	GameTime *int
	PlayerID *int

	PlayerIDs []int // playerid1..playerid6 when present, preserving order
	Value     *int
	Value2    *int
	Value3    *int
	InnerTime *float64

	Item     *string
	ItemID   *int
	Building *string
	Team     Team
	Message  *string

	BountyValue *int
	TeamGold    *int

	// Raw is a compact diagnostic dump of fields we did not map. Never used
	// as identity across ticks.
	Raw map[string]string
}

var chatKind = map[string]EventKind{
	"CHAT_MESSAGE_HERO_KILL":            EventHeroKill,
	"CHAT_MESSAGE_TOWER_KILL":           EventTowerKill,
	"CHAT_MESSAGE_BARRACKS_KILL":        EventBarracksKill,
	"CHAT_MESSAGE_ITEM_PURCHASE":        EventItemPurchase,
	"CHAT_MESSAGE_SMOKE_ACTIVATED":      EventSmokeActivated,
	"CHAT_MESSAGE_GLYPH_USED":           EventGlyphUsed,
	"CHAT_MESSAGE_SCAN_USED":            EventScanUsed,
	"CHAT_MESSAGE_OBSERVER_WARD_KILLED": EventObserverWardKilled,
	"CHAT_MESSAGE_SENTRY_WARD_KILLED":   EventSentryWardKilled,
	"CHAT_MESSAGE_COURIER_LOST":         EventCourierLost,
	"CHAT_MESSAGE_COURIER_RESPAWNED":    EventCourierRespawned,
}

var topKind = map[string]EventKind{
	"bounty_rune_pickup":   EventBountyRunePickup,
	"chat_message":         EventChatMessage,
	"tip":                  EventTip,
	"hero_kill":            EventHeroKill,
	"tower_kill":           EventTowerKill,
	"barracks_kill":        EventBarracksKill,
	"item_purchase":        EventItemPurchase,
	"smoke_activated":      EventSmokeActivated,
	"glyph_used":           EventGlyphUsed,
	"scan_used":            EventScanUsed,
	"observer_ward_killed": EventObserverWardKilled,
	"sentry_ward_killed":   EventSentryWardKilled,
	"courier_lost":         EventCourierLost,
	"courier_respawned":    EventCourierRespawned,
}

func parseEvents(raw json.RawMessage, meta Meta) (events []Event, unknown int) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" || string(raw) == "[]" {
		return nil, 0
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		// Object form: treat values as events.
		var obj map[string]json.RawMessage
		if err2 := json.Unmarshal(raw, &obj); err2 != nil {
			return nil, 1
		}
		keys := sortedKeys(obj)
		for _, k := range keys {
			ev, unk := parseOneEvent(obj[k], meta)
			if unk {
				unknown++
			}
			if ev != nil {
				events = append(events, *ev)
			}
		}
		return events, unknown
	}
	for _, item := range list {
		ev, unk := parseOneEvent(item, meta)
		if unk {
			unknown++
		}
		if ev != nil {
			events = append(events, *ev)
		}
	}
	return events, unknown
}

func parseOneEvent(raw json.RawMessage, meta Meta) (*Event, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, true
	}
	ev := Event{
		Meta:    meta,
		RawType: stringVal(fields["event_type"]),
		Kind:    EventUnknown,
		Raw:     map[string]string{},
	}
	if gt := intVal(fields["game_time"]); gt != nil {
		ev.GameTime = gt
	}
	if pid := intVal(fields["player_id"]); pid != nil {
		ev.PlayerID = pid
	}
	if msg := stringPtr(fields["message"]); msg != nil {
		ev.Message = msg
	}
	if team := stringVal(fields["team"]); team != "" {
		ev.Team = teamFromName(team)
	}
	if v := intVal(fields["bounty_value"]); v != nil {
		ev.BountyValue = v
	}
	if v := intVal(fields["team_gold"]); v != nil {
		ev.TeamGold = v
	}
	if item := stringPtr(fields["item"]); item != nil {
		ev.Item = item
	}

	if data, ok := fields["data"]; ok {
		parseEventData(&ev, data)
	}

	if k, ok := topKind[strings.ToLower(ev.RawType)]; ok {
		if ev.Kind == EventUnknown {
			ev.Kind = k
		}
	}
	if ev.RawType == "generic_event" && ev.InnerType != "" {
		if k, ok := chatKind[ev.InnerType]; ok {
			ev.Kind = k
		}
	}

	// Capture leftover fields for diagnostics / fingerprinting.
	for _, key := range sortedKeys(fields) {
		switch key {
		case "event_type", "game_time", "player_id", "data", "message", "team",
			"bounty_value", "team_gold", "item":
			continue
		default:
			ev.Raw[key] = trimRaw(fields[key])
		}
	}

	unknown := ev.Kind == EventUnknown
	return &ev, unknown
}

func parseEventData(ev *Event, data json.RawMessage) {
	if len(data) == 0 || string(data) == "null" {
		return
	}
	// data is usually a JSON string containing a JSON object.
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		data = json.RawMessage(asString)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		ev.Raw["data"] = trimRaw(data)
		return
	}
	ev.InnerType = stringVal(obj["type"])
	ev.Value = intVal(obj["value"])
	ev.Value2 = intVal(obj["value2"])
	ev.Value3 = intVal(obj["value3"])
	if t := floatVal(obj["time"]); t != nil {
		ev.InnerTime = t
	}
	var pids []int
	for i := 1; i <= 6; i++ {
		key := "playerid" + strconv.Itoa(i)
		if v := intVal(obj[key]); v != nil {
			pids = append(pids, *v)
			if i == 1 && ev.PlayerID == nil && *v >= 0 {
				id := *v
				ev.PlayerID = &id
			}
		}
	}
	ev.PlayerIDs = pids
	if ev.Kind == EventUnknown {
		if k, ok := chatKind[ev.InnerType]; ok {
			ev.Kind = k
		}
	}
	if ev.Kind == EventItemPurchase && ev.ItemID == nil && ev.Value != nil {
		id := *ev.Value
		ev.ItemID = &id
	}
	if team := teamFromGSI(derefInt(ev.Value)); ev.Kind == EventTowerKill || ev.Kind == EventBarracksKill {
		if ev.Value != nil {
			ev.Team = team
		}
	}
	for _, key := range sortedKeys(obj) {
		switch key {
		case "type", "value", "value2", "value3", "time",
			"playerid1", "playerid2", "playerid3", "playerid4", "playerid5", "playerid6":
			continue
		default:
			ev.Raw["data."+key] = trimRaw(obj[key])
		}
	}
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func trimRaw(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 256 {
		return s[:256]
	}
	return s
}

// Fingerprint is a stable identity for bounded event deduplication.
func (e Event) Fingerprint() string {
	var b strings.Builder
	b.WriteString(e.RawType)
	b.WriteByte('|')
	b.WriteString(e.InnerType)
	b.WriteByte('|')
	b.WriteString(string(e.Kind))
	b.WriteByte('|')
	writeIntPtr(&b, e.GameTime)
	b.WriteByte('|')
	writeIntPtr(&b, e.PlayerID)
	b.WriteByte('|')
	for i, id := range e.PlayerIDs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(id))
	}
	b.WriteByte('|')
	writeIntPtr(&b, e.Value)
	b.WriteByte('|')
	writeIntPtr(&b, e.Value2)
	b.WriteByte('|')
	writeIntPtr(&b, e.Value3)
	b.WriteByte('|')
	if e.InnerTime != nil {
		b.WriteString(strconv.FormatFloat(*e.InnerTime, 'f', 6, 64))
	}
	b.WriteByte('|')
	if e.Item != nil {
		b.WriteString(*e.Item)
	}
	b.WriteByte('|')
	writeIntPtr(&b, e.ItemID)
	b.WriteByte('|')
	if e.Building != nil {
		b.WriteString(*e.Building)
	}
	b.WriteByte('|')
	if e.Message != nil {
		b.WriteString(*e.Message)
	}
	b.WriteByte('|')
	writeIntPtr(&b, e.BountyValue)
	return b.String()
}

func writeIntPtr(b *strings.Builder, v *int) {
	if v == nil {
		b.WriteByte('-')
		return
	}
	b.WriteString(strconv.Itoa(*v))
}
