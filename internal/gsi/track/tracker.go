package track

import (
	"sort"
	"sync"

	"cmnet/internal/gsi/observe"
)

const (
	defaultEventHistory = 256
	defaultFingerprint  = 512
	defaultPurchases    = 64
	defaultSmokes       = 32
)

// Options configure a Tracker.
type Options struct {
	Roster RosterProvider
	Log    func(string)

	EventHistory  int
	Fingerprints  int
	PurchaseLimit int
	SmokeLimit    int
}

// Tracker is the sequential match memory. Apply must not be called concurrently
// with itself; Snapshot is safe to call while Apply runs.
type Tracker struct {
	mu sync.Mutex

	roster     Roster
	haveRoster bool
	provider   RosterProvider
	log        func(string)

	clock Clock
	self  observe.SelfObservation

	allies  []AllyHeroTrack
	enemies []EnemyHeroTrack

	ownBuildings map[string]BuildingState
	enemyStruct  map[string]*EnemyStructureTrack

	creeps         []observe.CreepObservation
	minimapPresent bool

	alliedWards      []WardTrack
	enemyWards       []WardTrack
	lastEnemyWards   []WardTrack
	lastEnemyWardsAt *int

	roshan RoshanTrack

	events    *ring[observe.Event]
	purchases *ring[ItemPurchase]
	smokes    *ring[SmokeActivation]
	seen      *fingerprintSet

	structSeen map[string]struct{}

	stats Stats
}

// New creates a Tracker. Roster may be filled later via SetRoster.
func New(opt Options) *Tracker {
	if opt.EventHistory <= 0 {
		opt.EventHistory = defaultEventHistory
	}
	if opt.Fingerprints <= 0 {
		opt.Fingerprints = defaultFingerprint
	}
	if opt.PurchaseLimit <= 0 {
		opt.PurchaseLimit = defaultPurchases
	}
	if opt.SmokeLimit <= 0 {
		opt.SmokeLimit = defaultSmokes
	}
	lg := opt.Log
	if lg == nil {
		lg = func(string) {}
	}
	tr := &Tracker{
		provider:     opt.Roster,
		log:          lg,
		ownBuildings: map[string]BuildingState{},
		enemyStruct:  map[string]*EnemyStructureTrack{},
		events:       newRing[observe.Event](opt.EventHistory),
		purchases:    newRing[ItemPurchase](opt.PurchaseLimit),
		smokes:       newRing[SmokeActivation](opt.SmokeLimit),
		seen:         newFingerprintSet(opt.Fingerprints),
		stats: Stats{
			VisibilityAppeared:    map[string]int{},
			VisibilityDisappeared: map[string]int{},
			EventsByKind:          map[observe.EventKind]int{},
		},
	}
	if opt.Roster != nil {
		if r, ok := opt.Roster.Roster(); ok {
			tr.setRosterLocked(r)
			tr.log("state tracker: roster loaded")
		}
	}
	tr.log("state tracker started")
	return tr
}

// SetRoster replaces the composition. Call this after Reset for a new match.
func (t *Tracker) SetRoster(r Roster) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.setRosterLocked(r)
	t.log("state tracker: roster loaded")
}

func (t *Tracker) setRosterLocked(r Roster) {
	t.roster = r
	t.haveRoster = r.Valid()
	t.allies = make([]AllyHeroTrack, len(r.Allies))
	for i, h := range r.Allies {
		t.allies[i] = AllyHeroTrack{Hero: h.UnitName}
	}
	t.enemies = make([]EnemyHeroTrack, len(r.Enemies))
	for i, h := range r.Enemies {
		t.enemies[i] = EnemyHeroTrack{Hero: h.UnitName}
	}
}

// Reset clears match memory. Roster must be set again.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.resetLocked()
}

func (t *Tracker) resetLocked() {
	t.clock = Clock{}
	t.self = observe.SelfObservation{}
	t.allies = nil
	t.enemies = nil
	t.haveRoster = false
	t.roster = Roster{}
	t.ownBuildings = map[string]BuildingState{}
	t.enemyStruct = map[string]*EnemyStructureTrack{}
	t.creeps = nil
	t.minimapPresent = false
	t.alliedWards = nil
	t.enemyWards = nil
	t.lastEnemyWards = nil
	t.lastEnemyWardsAt = nil
	t.roshan = RoshanTrack{}
	t.events.Clear()
	t.purchases.Clear()
	t.smokes.Clear()
	t.seen.Clear()
	t.stats.TrackerResets++
	t.log("state tracker: match reset")
}

// NoteParseError counts a payload that was not applied.
func (t *Tracker) NoteParseError() {
	t.mu.Lock()
	t.stats.ParseErrors++
	t.mu.Unlock()
}

// Apply folds one observation batch into memory. Callers must feed batches
// in receive/seq order.
func (t *Tracker) Apply(batch *observe.Batch) {
	if batch == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.maybeReset(batch)
	if !t.haveRoster && t.provider != nil {
		if r, ok := t.provider.Roster(); ok {
			t.setRosterLocked(r)
			t.log("state tracker: roster loaded")
		}
	}

	t.updateClock(batch)
	if batch.Self.Present && (batch.Self.HeroName != nil || batch.Self.Position != nil) {
		t.self = batch.Self
	}
	t.stats.PayloadsProcessed++
	t.stats.MinimapEntities += batch.Diagnostics.MinimapEntities
	t.stats.UnknownMinimap += batch.Diagnostics.UnknownMinimapUnits
	t.stats.UnknownEvents += batch.Diagnostics.UnknownEvents
	t.stats.HeroObservations += len(batch.Heroes)
	t.stats.CreepObservations += len(batch.Creeps)
	t.stats.RoshanObservations += len(batch.Roshan)

	t.applyHeroes(batch)
	t.applyBuildings(batch)
	t.applyStructures(batch)
	t.minimapPresent = batch.MinimapPresent
	if batch.MinimapPresent {
		t.creeps = append([]observe.CreepObservation(nil), batch.Creeps...)
	} else {
		t.creeps = nil
	}
	t.applyWards(batch)
	t.applyRoshan(batch)
	t.applyEvents(batch)
}

func (t *Tracker) maybeReset(batch *observe.Batch) {
	id := ""
	if batch.Match.MatchID != nil {
		id = *batch.Match.MatchID
	}
	if id == "" || t.clock.MatchID == "" || id == t.clock.MatchID {
		return
	}
	t.log("state tracker: match detected")
	t.resetLocked()
	if t.provider != nil {
		if r, ok := t.provider.Roster(); ok {
			t.setRosterLocked(r)
			t.log("state tracker: roster loaded")
		}
	}
}

func (t *Tracker) updateClock(batch *observe.Batch) {
	c := t.clock
	c.Seq = batch.Meta.Seq
	c.ReceivedAt = batch.Meta.ReceivedAt
	if batch.Match.Present {
		if batch.Match.GameTime != nil {
			c.GameTime = batch.Match.GameTime
		}
		if batch.Match.ClockTime != nil {
			c.ClockTime = batch.Match.ClockTime
		}
		if batch.Match.Paused != nil {
			c.Paused = *batch.Match.Paused
		}
		if batch.Match.GameState != nil {
			c.GameState = *batch.Match.GameState
		}
		if batch.Match.MatchID != nil && *batch.Match.MatchID != "" {
			if c.MatchID == "" {
				t.log("state tracker: match detected")
			}
			c.MatchID = *batch.Match.MatchID
		}
		if batch.Match.RadiantScore != nil {
			c.RadiantScore = *batch.Match.RadiantScore
		}
		if batch.Match.DireScore != nil {
			c.DireScore = *batch.Match.DireScore
		}
	}
	t.clock = c
}

func (t *Tracker) applyHeroes(batch *observe.Batch) {
	if !t.haveRoster {
		return
	}
	allyIdx := t.roster.allySet()
	enemyIdx := t.roster.enemySet()
	allyPos := make([][]observe.Vec2, len(t.allies))
	enemyPos := make([][]observe.Vec2, len(t.enemies))
	multi := map[string]int{}

	for _, h := range batch.Heroes {
		if i, ok := allyIdx[h.Hero]; ok {
			if h.Team != observe.TeamUnknown && h.Team != t.roster.AllySide {
				continue
			}
			allyPos[i] = append(allyPos[i], h.Position)
			multi[h.Hero]++
			continue
		}
		if i, ok := enemyIdx[h.Hero]; ok {
			if h.Team != observe.TeamUnknown && h.Team != t.roster.EnemySide() {
				continue
			}
			if h.ImageKind != observe.HeroImageEnemy && h.ImageKind != observe.HeroImageUnknown {
				// Allied-looking icon of an enemy name is not a vision observation.
				if h.ImageKind == observe.HeroImageAllied {
					continue
				}
			}
			enemyPos[i] = append(enemyPos[i], h.Position)
			multi[h.Hero]++
		}
	}
	for _, n := range multi {
		if n > 1 {
			t.stats.MultiHeroTicks++
			break
		}
	}

	now, nowOK := t.clock.Instant()
	var nowPtr *int
	if nowOK {
		v := now
		nowPtr = &v
	}

	for i := range t.allies {
		pos := allyPos[i]
		a := &t.allies[i]
		a.CurrentlyObserved = len(pos) > 0
		a.PositionCandidates = pos
		if len(pos) > 0 {
			a.LastObservedAt = nowPtr
			a.LastObservedPositions = append([]observe.Vec2(nil), pos...)
		}
	}
	for i := range t.enemies {
		pos := enemyPos[i]
		e := &t.enemies[i]
		visible := len(pos) > 0
		switch {
		case visible && !e.VisibleNow:
			t.stats.VisibilityAppeared[e.Hero]++
			e.MissingSince = nil
			e.LastSeenAt = nowPtr
			e.LastSeenPositions = append([]observe.Vec2(nil), pos...)
		case visible && e.VisibleNow:
			e.LastSeenAt = nowPtr
			e.LastSeenPositions = append([]observe.Vec2(nil), pos...)
		case !visible && e.VisibleNow:
			t.stats.VisibilityDisappeared[e.Hero]++
			e.MissingSince = nowPtr
			// keep LastSeenAt / LastSeenPositions
		}
		e.VisibleNow = visible
		if visible {
			e.VisiblePositions = pos
		} else {
			e.VisiblePositions = nil
		}
		e.LastSeenAgeSeconds = ageSeconds(t.clock, e.LastSeenAt)
	}
}

func (t *Tracker) applyBuildings(batch *observe.Batch) {
	for _, b := range batch.OwnBuildings {
		t.ownBuildings[b.UnitName] = BuildingState{
			UnitName:  b.UnitName,
			Team:      b.Team,
			Class:     b.Class,
			Lane:      b.Lane,
			Health:    b.Health,
			MaxHealth: b.MaxHealth,
			Alive:     b.Alive,
		}
	}
}

func (t *Tracker) applyStructures(batch *observe.Batch) {
	t.structSeen = map[string]struct{}{}
	for _, s := range batch.Structures {
		if s.Class == observe.StructureUnknown {
			continue
		}
		if t.haveRoster && s.Team == t.roster.AllySide {
			continue
		}
		if t.haveRoster && s.Team != observe.TeamUnknown && s.Team != t.roster.EnemySide() {
			continue
		}
		t.structSeen[s.UnitName] = struct{}{}
		st, ok := t.enemyStruct[s.UnitName]
		if !ok {
			st = &EnemyStructureTrack{
				UnitName: s.UnitName,
				Team:     s.Team,
				Class:    s.Class,
				Lane:     s.Lane,
			}
			t.enemyStruct[s.UnitName] = st
		}
		// A live minimap observation is stronger than a previous kill guess.
		st.Life = StructureSeenAlive
		st.Position = s.Position
		if ts, ok := t.clock.Instant(); ok {
			v := ts
			st.LastSeen = &v
		}
	}
}

func (t *Tracker) applyWards(batch *observe.Batch) {
	var allied, enemy []WardTrack
	var nowPtr *int
	if ts, ok := t.clock.Instant(); ok {
		v := ts
		nowPtr = &v
	}
	for _, w := range batch.Wards {
		tr := WardTrack{
			Class:    w.Class,
			Team:     w.Team,
			Position: w.Position,
			LastSeen: nowPtr,
			Observed: true,
		}
		if t.haveRoster {
			switch w.Team {
			case t.roster.AllySide:
				allied = append(allied, tr)
			case t.roster.EnemySide():
				enemy = append(enemy, tr)
			}
			continue
		}
		allied = append(allied, tr)
	}
	t.alliedWards = allied
	if len(enemy) > 0 {
		t.enemyWards = enemy
		t.lastEnemyWards = append([]WardTrack(nil), enemy...)
		t.lastEnemyWardsAt = nowPtr
	} else {
		t.enemyWards = nil
	}
}

func (t *Tracker) applyRoshan(batch *observe.Batch) {
	var pos []observe.Vec2
	for _, r := range batch.Roshan {
		pos = append(pos, r.Position)
	}
	visible := len(pos) > 0
	var nowPtr *int
	if ts, ok := t.clock.Instant(); ok {
		v := ts
		nowPtr = &v
	}
	if visible {
		t.roshan.VisibleNow = true
		t.roshan.CurrentPositions = pos
		t.roshan.LastSeenAt = nowPtr
		t.roshan.LastSeenPositions = append([]observe.Vec2(nil), pos...)
		t.roshan.LastSeenAlive = true
	} else {
		t.roshan.VisibleNow = false
		t.roshan.CurrentPositions = nil
		// keep last seen; disappearance is not death
	}
}

func (t *Tracker) applyEvents(batch *observe.Batch) {
	for _, ev := range batch.Events {
		fp := ev.Fingerprint()
		if t.seen.Seen(fp) {
			t.stats.EventsDeduped++
			continue
		}
		t.seen.Add(fp)
		t.events.Push(ev)
		t.stats.EventsAccepted++
		if t.stats.EventsByKind == nil {
			t.stats.EventsByKind = map[observe.EventKind]int{}
		}
		t.stats.EventsByKind[ev.Kind]++
		t.handleEvent(ev)
	}
}

func (t *Tracker) handleEvent(ev observe.Event) {
	switch ev.Kind {
	case observe.EventItemPurchase:
		t.purchases.Push(ItemPurchase{
			PlayerID: ev.PlayerID,
			Item:     derefStr(ev.Item),
			ItemID:   ev.ItemID,
			GameTime: ev.GameTime,
		})
	case observe.EventSmokeActivated:
		t.smokes.Push(SmokeActivation{
			PlayerID:   ev.PlayerID,
			GameTime:   ev.GameTime,
			ReceivedAt: ev.Meta.ReceivedAt,
		})
	case observe.EventTowerKill:
		t.destroyByEvent(ev, true)
	case observe.EventBarracksKill:
		t.destroyByEvent(ev, false)
	}
}

func (t *Tracker) destroyByEvent(ev observe.Event, tower bool) {
	if ev.Building != nil {
		name := observe.CanonicalUnitName(*ev.Building)
		if st, ok := t.enemyStruct[name]; ok {
			st.Life = StructureConfirmedDestroyed
			return
		}
		st := &EnemyStructureTrack{UnitName: name, Team: ev.Team, Life: StructureConfirmedDestroyed}
		if tower {
			st.Class = observe.StructureTowerT1
		} else {
			st.Class = observe.StructureMeleeRax
		}
		t.enemyStruct[name] = st
		return
	}
	// TOWER_KILL without a unit name is not enough to mark a specific tower
	// CONFIRMED_DESTROYED. A unique missing candidate is only a guess.
	var candidates []*EnemyStructureTrack
	for _, st := range t.enemyStruct {
		if st.Life != StructureSeenAlive {
			continue
		}
		if tower && !st.Class.IsTower() {
			continue
		}
		if !tower && !st.Class.IsBarracks() {
			continue
		}
		if ev.Team != observe.TeamUnknown && st.Team != observe.TeamUnknown && st.Team != ev.Team {
			continue
		}
		if _, seen := t.structSeen[st.UnitName]; seen {
			continue
		}
		candidates = append(candidates, st)
	}
	if len(candidates) == 1 {
		candidates[0].Life = StructureLikelyDestroyed
	}
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Snapshot returns a deep-enough copy that callers cannot mutate tracker state
// through the result.
func (t *Tracker) Snapshot() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

func (t *Tracker) snapshotLocked() State {
	st := State{
		Clock:  t.clock,
		Roster: t.roster,
		Self:   SelfState{SelfObservation: t.self},
		Roshan: t.roshan,
	}
	st.Allies = copyAllies(t.allies)
	st.Enemies = copyEnemies(t.enemies, t.clock)
	st.Creeps = append([]observe.CreepObservation(nil), t.creeps...)
	st.MinimapPresent = t.minimapPresent
	st.AlliedWards = append([]WardTrack(nil), t.alliedWards...)
	if len(t.enemyWards) > 0 {
		st.ObservedEnemyWards = append([]WardTrack(nil), t.enemyWards...)
	} else {
		st.ObservedEnemyWards = append([]WardTrack(nil), t.lastEnemyWards...)
		for i := range st.ObservedEnemyWards {
			st.ObservedEnemyWards[i].Observed = false
		}
	}
	for _, name := range sortedBuildingNames(t.ownBuildings) {
		st.OwnBuildings = append(st.OwnBuildings, t.ownBuildings[name])
	}
	for _, name := range sortedStructNames(t.enemyStruct) {
		cp := *t.enemyStruct[name]
		st.EnemyStructures = append(st.EnemyStructures, cp)
	}
	st.RecentEvents = t.events.Slice()
	st.RecentPurchases = t.purchases.Slice()
	st.RecentSmokes = t.smokes.Slice()
	st.Roshan.CurrentPositions = append([]observe.Vec2(nil), t.roshan.CurrentPositions...)
	st.Roshan.LastSeenPositions = append([]observe.Vec2(nil), t.roshan.LastSeenPositions...)
	return st
}

func copyAllies(in []AllyHeroTrack) []AllyHeroTrack {
	out := make([]AllyHeroTrack, len(in))
	for i, a := range in {
		a.PositionCandidates = append([]observe.Vec2(nil), a.PositionCandidates...)
		a.LastObservedPositions = append([]observe.Vec2(nil), a.LastObservedPositions...)
		out[i] = a
	}
	return out
}

func copyEnemies(in []EnemyHeroTrack, clock Clock) []EnemyHeroTrack {
	out := make([]EnemyHeroTrack, len(in))
	for i, e := range in {
		e.VisiblePositions = append([]observe.Vec2(nil), e.VisiblePositions...)
		e.LastSeenPositions = append([]observe.Vec2(nil), e.LastSeenPositions...)
		e.LastSeenAgeSeconds = ageSeconds(clock, e.LastSeenAt)
		out[i] = e
	}
	return out
}

func sortedBuildingNames(m map[string]BuildingState) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func sortedStructNames(m map[string]*EnemyStructureTrack) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(s []string) { sort.Strings(s) }

// StatsSnapshot copies counters.
func (t *Tracker) StatsSnapshot() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.stats
	s.VisibilityAppeared = copyCounts(t.stats.VisibilityAppeared)
	s.VisibilityDisappeared = copyCounts(t.stats.VisibilityDisappeared)
	if t.stats.EventsByKind != nil {
		s.EventsByKind = make(map[observe.EventKind]int, len(t.stats.EventsByKind))
		for k, v := range t.stats.EventsByKind {
			s.EventsByKind[k] = v
		}
	}
	return s
}

func copyCounts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
