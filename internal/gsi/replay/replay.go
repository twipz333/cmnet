// Package replay drives the production ObservationExtractor and StateTracker
// over a gsi-probe capture session.
package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/probe"
	"cmnet/internal/gsi/track"
)

// Session is an on-disk gsi-probe capture.
type Session struct {
	Dir     string
	Records []probe.IndexRecord
}

// Open reads index.ndjson in seq order. Invalid JSON lines are skipped with a count.
func Open(dir string) (*Session, error) {
	path := filepath.Join(dir, "index.ndjson")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer f.Close()
	var recs []probe.IndexRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec probe.IndexRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		recs = append(recs, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Seq < recs[j].Seq })
	return &Session{Dir: dir, Records: recs}, nil
}

func (s *Session) payload(rec probe.IndexRecord) ([]byte, error) {
	rel := rec.File
	if rel == "" {
		rel = filepath.ToSlash(filepath.Join("payloads", fmt.Sprintf("%08d.json", rec.Seq)))
	}
	return os.ReadFile(filepath.Join(s.Dir, filepath.FromSlash(rel)))
}

// Options configure a replay run.
type Options struct {
	Roster    track.Roster
	TraceHero string
	OnTick    func(seq uint64, st track.State)
	Geometry  *worldmap.Geometry
	Lanes     *worldmap.LaneGeometry
}

// Result is the end-of-run summary.
type Result struct {
	Processed   int
	ParseErrors int
	State       track.State
	Stats       track.Stats
	Trace       []TraceStep
	Map         MapCoverage
	LastMap     mapstate.State
	Samples     MapSamples
	Lane        LaneCoverage
	LastLanes   lanestate.State
	LaneSamples LaneSamples
}

// TraceStep is one visibility edge for --trace-hero.
type TraceStep struct {
	Seq             uint64
	GameTime        *int
	Visible         bool
	Positions       []observe.Vec2
	LastSeen        []observe.Vec2
	Age             *float64
	Regions         []worldmap.RegionID
	LastSeenRegions []worldmap.RegionID
}

// Run feeds every payload through the production extractor and tracker.
func Run(s *Session, opt Options) (Result, error) {
	ex := observe.Extractor{}
	tr := track.New(track.Options{
		Roster: track.StaticRoster{R: opt.Roster},
		Log:    func(string) {},
	})
	var laneTr *lanestate.Tracker
	if opt.Lanes != nil {
		laneTr = lanestate.New(opt.Lanes)
	}
	var res Result
	var lastVis *bool
	lanePrev := map[worldmap.LaneID]*lanePrev{}
	for _, rec := range s.Records {
		body, err := s.payload(rec)
		if err != nil {
			res.ParseErrors++
			tr.NoteParseError()
			continue
		}
		received := time.UnixMilli(rec.ReceivedUnixMs).UTC()
		if rec.ReceivedUnixMs == 0 && rec.ReceivedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, rec.ReceivedAt); err == nil {
				received = t
			}
		}
		batch, err := ex.Extract(body, observe.ExtractOptions{
			ReceivedAt: received,
			Seq:        rec.Seq,
		})
		if err != nil {
			res.ParseErrors++
			tr.NoteParseError()
			continue
		}
		tr.Apply(batch)
		res.Processed++
		stNow := tr.Snapshot()
		clock := 0
		if stNow.Clock.ClockTime != nil {
			clock = *stNow.Clock.ClockTime
		} else if stNow.Clock.GameTime != nil {
			clock = *stNow.Clock.GameTime
		}
		if opt.Geometry != nil {
			ms := mapstate.Build(stNow, opt.Geometry)
			res.LastMap = ms
			accumulateCoverage(&res.Map, ms, opt.Geometry)
			noteSamples(&res.Samples, rec.Seq, clock, ms)
		}
		if laneTr != nil {
			lobs := lanestate.Build(stNow, opt.Lanes)
			accumulateLaneObs(&res.Lane, lobs, opt.Lanes)
			laneTr.Apply(lobs)
			ls := laneTr.Snapshot()
			res.LastLanes = ls
			noteLaneSamples(&res.LaneSamples, &res.Lane, rec.Seq, clock, ls, lanePrev)
		}
		if opt.TraceHero != "" {
			st := stNow
			step := traceOf(st, rec.Seq, opt.TraceHero, opt.Geometry)
			if lastVis == nil || *lastVis != step.Visible || (step.Visible && len(step.Positions) > 0) {
				if lastVis == nil || *lastVis != step.Visible {
					res.Trace = append(res.Trace, step)
					v := step.Visible
					lastVis = &v
				}
			}
		}
		if opt.OnTick != nil {
			opt.OnTick(rec.Seq, tr.Snapshot())
		}
	}
	res.State = tr.Snapshot()
	res.Stats = tr.StatsSnapshot()
	res.ParseErrors += res.Stats.ParseErrors
	return res, nil
}

func traceOf(st track.State, seq uint64, hero string, g *worldmap.Geometry) TraceStep {
	step := TraceStep{Seq: seq, GameTime: st.Clock.GameTime}
	for _, e := range st.Enemies {
		if e.Hero != hero {
			continue
		}
		step.Visible = e.VisibleNow
		step.Positions = e.VisiblePositions
		step.LastSeen = e.LastSeenPositions
		step.Age = e.LastSeenAgeSeconds
		step.Regions, step.LastSeenRegions = classifyTrace(g, e.VisibleNow, e.VisiblePositions, e.LastSeenPositions)
		return step
	}
	for _, a := range st.Allies {
		if a.Hero != hero {
			continue
		}
		step.Visible = a.CurrentlyObserved
		step.Positions = a.PositionCandidates
		step.LastSeen = a.LastObservedPositions
		step.Regions, step.LastSeenRegions = classifyTrace(g, a.CurrentlyObserved, a.PositionCandidates, a.LastObservedPositions)
		return step
	}
	return step
}

func (r Result) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Processed payloads: %d\n", r.Processed)
	if r.ParseErrors > 0 {
		fmt.Fprintf(&b, "Parse errors:       %d\n", r.ParseErrors)
	}
	fmt.Fprintf(&b, "\nHero observations:  %d\n", r.Stats.HeroObservations)
	var allies, enemies int
	for _, a := range r.State.Allies {
		if a.LastObservedAt != nil {
			allies++
		}
	}
	for _, e := range r.State.Enemies {
		if e.LastSeenAt != nil {
			enemies++
		}
	}
	fmt.Fprintf(&b, "  allies with last observed: %d / %d\n", allies, len(r.State.Allies))
	fmt.Fprintf(&b, "  enemies with last seen:    %d / %d\n", enemies, len(r.State.Enemies))
	fmt.Fprintf(&b, "  multi-hero ticks:          %d\n", r.Stats.MultiHeroTicks)

	fmt.Fprintf(&b, "\nEnemy visibility transitions:\n")
	names := make([]string, 0, len(r.State.Enemies))
	for _, e := range r.State.Enemies {
		names = append(names, e.Hero)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "  %-28s appeared %d / disappeared %d\n",
			shortHero(name), r.Stats.VisibilityAppeared[name], r.Stats.VisibilityDisappeared[name])
	}

	fmt.Fprintf(&b, "\nCreeps observed (entity-ticks): %d\n", r.Stats.CreepObservations)
	fmt.Fprintf(&b, "Roshan observations:            %d\n", r.Stats.RoshanObservations)
	fmt.Fprintf(&b, "Roshan last seen alive:         %v visible_now=%v\n", r.State.Roshan.LastSeenAlive, r.State.Roshan.VisibleNow)

	fmt.Fprintf(&b, "\nStructures:\n")
	for _, st := range r.State.EnemyStructures {
		fmt.Fprintf(&b, "  %-40s %s\n", st.UnitName, st.Life)
	}
	fmt.Fprintf(&b, "\nOwn buildings: %d\n", len(r.State.OwnBuildings))
	fmt.Fprintf(&b, "Allied wards (now): %d\n", countObserved(r.State.AlliedWards))
	fmt.Fprintf(&b, "Enemy wards (now/recent): %d\n", len(r.State.ObservedEnemyWards))

	fmt.Fprintf(&b, "\nEvents accepted: %d (deduped %d)\n", r.Stats.EventsAccepted, r.Stats.EventsDeduped)
	if len(r.Stats.EventsByKind) > 0 {
		keys := make([]string, 0, len(r.Stats.EventsByKind))
		for k := range r.Stats.EventsByKind {
			keys = append(keys, string(k))
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %-24s %d\n", k, r.Stats.EventsByKind[observe.EventKind(k)])
		}
	}
	fmt.Fprintf(&b, "Recent event ring: %d\n", len(r.State.RecentEvents))
	fmt.Fprintf(&b, "Recent purchases:  %d\n", len(r.State.RecentPurchases))
	fmt.Fprintf(&b, "Recent smokes:     %d\n", len(r.State.RecentSmokes))
	if r.Map.Positions > 0 {
		b.WriteByte('\n')
		b.WriteString(r.Map.Format())
	}
	if r.Lane.Eligible > 0 || r.Lane.Version != "" {
		b.WriteByte('\n')
		b.WriteString(r.Lane.Format())
	}
	return b.String()
}

func countObserved(ws []track.WardTrack) int {
	n := 0
	for _, w := range ws {
		if w.Observed {
			n++
		}
	}
	return n
}

func shortHero(name string) string {
	return strings.TrimPrefix(name, "npc_dota_hero_")
}
