package lanestate

import (
	"sync"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
)

// Tracker holds lane-specific history (enemy last-seen). Apply is sequential.
type Tracker struct {
	mu  sync.Mutex
	geo *worldmap.LaneGeometry

	matchID        string
	ourSide        observe.Team
	seq            uint64
	gameTime       *int
	clockTime      *int
	paused         bool
	minimapPresent bool

	top, mid, bot laneMem

	stats Stats
}

type laneMem struct {
	radiant     []WaveGroup
	dire        []WaveGroup
	enemyLast   []WaveGroup
	enemyLastAt *int
	contact     bool
	frontGap    *float64
	observedAt  *int
}

// Stats are cheap counters.
type Stats struct {
	TicksApplied        int
	TicksWithoutMinimap int
	LaneCreepsSeen      int
	LaneCreepsAssigned  int
	UnknownTooFar       int
	Ambiguous           int
	NonLaneSkipped      int
	WaveGroupsCreated   int
	EnemyVisibilityLost int
	Resets              int
}

// New returns a LaneTracker bound to geometry loaded once by the caller.
func New(geo *worldmap.LaneGeometry) *Tracker {
	return &Tracker{geo: geo}
}

// Reset clears match memory. Geometry is kept.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.resetLocked()
}

func (t *Tracker) resetLocked() {
	t.matchID = ""
	t.ourSide = observe.TeamUnknown
	t.seq = 0
	t.gameTime = nil
	t.clockTime = nil
	t.paused = false
	t.minimapPresent = false
	t.top = laneMem{}
	t.mid = laneMem{}
	t.bot = laneMem{}
	t.stats.Resets++
}

// Apply folds one observation. Callers feed ticks in seq order.
func (t *Tracker) Apply(obs Observation) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if obs.MatchID != "" && t.matchID != "" && obs.MatchID != t.matchID {
		t.resetLocked()
	}
	if obs.MatchID != "" {
		t.matchID = obs.MatchID
	}
	t.seq = obs.Seq
	t.gameTime = cloneInt(obs.GameTime)
	t.clockTime = cloneInt(obs.ClockTime)
	t.paused = obs.Paused
	if obs.OurSide == observe.TeamRadiant || obs.OurSide == observe.TeamDire {
		t.ourSide = obs.OurSide
	}
	t.stats.TicksApplied++
	t.stats.LaneCreepsSeen += obs.Stats.LaneCreepsSeen
	t.stats.LaneCreepsAssigned += obs.Stats.LaneCreepsAssigned
	t.stats.UnknownTooFar += obs.Stats.UnknownTooFar
	t.stats.Ambiguous += obs.Stats.Ambiguous
	t.stats.NonLaneSkipped += obs.Stats.NonLaneSkipped
	t.stats.WaveGroupsCreated += obs.Stats.WaveGroupsCreated

	t.minimapPresent = obs.MinimapPresent
	if !obs.MinimapPresent {
		t.stats.TicksWithoutMinimap++
		return
	}

	t.applyLane(&t.top, obs.Top)
	t.applyLane(&t.mid, obs.Mid)
	t.applyLane(&t.bot, obs.Bot)
}

func (t *Tracker) applyLane(mem *laneMem, tick LaneTick) {
	now := t.gameTime
	if now == nil {
		now = t.clockTime
	}
	enemySide := opposite(t.ourSide)
	prevEnemy := mem.dire
	if enemySide == observe.TeamRadiant {
		prevEnemy = mem.radiant
	} else if enemySide != observe.TeamDire {
		prevEnemy = nil
	}
	hadVisible := len(prevEnemy) > 0

	mem.radiant = copyGroups(tick.Radiant)
	mem.dire = copyGroups(tick.Dire)
	mem.observedAt = cloneInt(now)

	enemy := groupsFor(tick, enemySide)
	if enemySide == observe.TeamRadiant || enemySide == observe.TeamDire {
		if len(enemy) > 0 {
			mem.enemyLast = copyGroups(enemy)
			mem.enemyLastAt = cloneInt(now)
		} else if hadVisible {
			t.stats.EnemyVisibilityLost++
		}
	}

	mem.contact = false
	mem.frontGap = nil
	if t.geo == nil {
		return
	}
	if len(mem.radiant) > 0 && len(mem.dire) > 0 {
		gap := minPathGap(mem.radiant, mem.dire)
		if gap <= t.geo.WaveContactGap() {
			mem.contact = true
		}
		rf := frontmostOf(mem.radiant)
		df := frontmostOf(mem.dire)
		if rf != nil && df != nil {
			d := rf.FrontAbsoluteDistance - df.FrontAbsoluteDistance
			if d < 0 {
				d = -d
			}
			mem.frontGap = &d
		}
	}
}

func opposite(side observe.Team) observe.Team {
	switch side {
	case observe.TeamRadiant:
		return observe.TeamDire
	case observe.TeamDire:
		return observe.TeamRadiant
	default:
		return observe.TeamUnknown
	}
}

func minPathGap(a, b []WaveGroup) float64 {
	best := 1e18
	for _, x := range a {
		for _, y := range b {
			d := x.CenterAbsoluteDistance - y.CenterAbsoluteDistance
			if d < 0 {
				d = -d
			}
			if d < best {
				best = d
			}
		}
	}
	return best
}

// Snapshot copies memory. Slices are independent of internal state.
func (t *Tracker) Snapshot() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

func (t *Tracker) snapshotLocked() State {
	st := State{
		MinimapPresent: t.minimapPresent,
		OurSide:        t.ourSide,
	}
	if t.geo != nil {
		st.GeometryVersion = t.geo.Version()
		st.MapGeometryVersion = t.geo.MapGeometryVersion()
	}
	now := t.gameTime
	if now == nil {
		now = t.clockTime
	}
	st.Top = t.view(worldmap.LaneTop, t.top, now)
	st.Mid = t.view(worldmap.LaneMid, t.mid, now)
	st.Bot = t.view(worldmap.LaneBot, t.bot, now)
	return st
}

func (t *Tracker) view(id worldmap.LaneID, mem laneMem, now *int) LaneView {
	v := LaneView{
		Lane:            id,
		ObservedContact: mem.contact,
	}
	if mem.frontGap != nil {
		d := *mem.frontGap
		v.VisibleFrontPathGap = &d
	}
	our, vis := splitPerspective(mem, t.ourSide)
	v.Our = sideFrom(our, t.geo)
	v.Enemy = enemyFrom(vis, mem.enemyLast, mem.enemyLastAt, now, t.geo)
	return v
}

func splitPerspective(mem laneMem, our observe.Team) (ours, enemy []WaveGroup) {
	switch our {
	case observe.TeamRadiant:
		return mem.radiant, mem.dire
	case observe.TeamDire:
		return mem.dire, mem.radiant
	default:
		return nil, nil
	}
}

func sideFrom(groups []WaveGroup, geo *worldmap.LaneGeometry) SideObservation {
	out := SideObservation{
		Waves:      copyGroups(groups),
		Frontmost:  frontmostOf(groups),
		FrontDepth: worldmap.DepthUnknown,
	}
	for _, g := range groups {
		out.CreepCount += g.CreepCount
	}
	if out.Frontmost != nil {
		out.HasFront = true
		out.FrontTeam = out.Frontmost.FrontTeamProgress
		out.FrontAbs = out.Frontmost.FrontAbsoluteProgress
		if geo != nil {
			out.FrontDepth = geo.Depth(out.FrontTeam)
		}
	}
	return out
}

func enemyFrom(visible, last []WaveGroup, lastAt, now *int, geo *worldmap.LaneGeometry) EnemyObservation {
	out := EnemyObservation{
		Visible:            copyGroups(visible),
		VisibleFrontmost:   frontmostOf(visible),
		VisibleFrontDepth:  worldmap.DepthUnknown,
		LastSeen:           copyGroups(last),
		LastSeenFrontmost:  frontmostOf(last),
		LastSeenAt:         cloneInt(lastAt),
		LastSeenAgeSeconds: ageSeconds(now, lastAt),
	}
	if out.VisibleFrontmost != nil {
		out.HasVisibleFront = true
		out.VisibleFrontTeam = out.VisibleFrontmost.FrontTeamProgress
		out.VisibleFrontAbs = out.VisibleFrontmost.FrontAbsoluteProgress
		if geo != nil {
			out.VisibleFrontDepth = geo.Depth(out.VisibleFrontTeam)
		}
	}
	if out.LastSeenFrontmost != nil {
		out.LastSeenFrontTeam = out.LastSeenFrontmost.FrontTeamProgress
	}
	return out
}

func ageSeconds(now, seen *int) *float64 {
	if now == nil || seen == nil {
		return nil
	}
	v := float64(*now - *seen)
	if v < 0 {
		v = 0
	}
	return &v
}

// StatsSnapshot copies counters.
func (t *Tracker) StatsSnapshot() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stats
}
