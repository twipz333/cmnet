package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"cmnet/internal/cache"
	dotastate "cmnet/internal/dota/state"
	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi"
	"cmnet/internal/gsi/belief"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/track"
	"cmnet/internal/startup"
)

type liveDebug struct {
	Enabled  bool
	Interval time.Duration
	Format   string
	Out      io.Writer
}

func (d liveDebug) normalized() liveDebug {
	if d.Interval <= 0 {
		d.Interval = time.Second
	}
	if d.Format == "" {
		d.Format = "text"
	}
	if d.Out == nil {
		d.Out = os.Stderr
	}
	return d
}

// startLiveTracker attaches one process-wide Tracker to srv via Pipe.
func startLiveTracker(ctx context.Context, srv *gsi.Server, dbPath string, dbg liveDebug) *gsi.Pipe {
	names := loadHeroUnitNames(dbPath)
	tr := track.New(track.Options{
		Roster: gsi.SnapshotRoster{Last: srv.LastSnapshot, Names: names},
		Log:    func(msg string) { startup.Infof("%s", msg) },
	})
	pipe := gsi.NewPipe(tr, gsi.DefaultPipeBuffer)
	pipe.Log = func(msg string) { startup.Warnf("%s", msg) }
	srv.SetPipe(pipe)
	go pipe.Run()
	dbg = dbg.normalized()
	var geo *worldmap.Geometry
	if g, err := worldmap.LoadCurrent(); err != nil {
		startup.Warnf("map geometry: %v", err)
	} else {
		geo = g
	}
	var lanes *worldmap.LaneGeometry
	var laneTr *lanestate.Tracker
	var regionGraph *worldmap.RegionGraph
	if g, err := worldmap.LoadLanesCurrent(); err != nil {
		startup.Warnf("lane geometry: %v", err)
	} else {
		lanes = g
		laneTr = lanestate.New(g)
		pipe.Lanes = laneTr
		pipe.LaneGeo = lanes
	}
	if g, err := worldmap.LoadRegionGraphCurrent(); err != nil {
		startup.Warnf("region graph: %v", err)
	} else {
		regionGraph = g
	}
	if dbg.Enabled {
		go runDebugState(ctx, tr, pipe, dbg, geo, laneTr, regionGraph)
	}
	return pipe
}

func loadHeroUnitNames(dbPath string) track.MapNames {
	out := track.MapNames{}
	if dbPath == "" {
		return out
	}
	store, err := cache.Open(dbPath)
	if err != nil {
		startup.Warnf("live state: hero cache unavailable (%v); roster waits until names exist (run sync once)", err)
		return out
	}
	defer store.Close()
	hs, err := store.Heroes()
	if err != nil {
		startup.Warnf("live state: could not read heroes: %v", err)
		return out
	}
	for _, h := range hs {
		if h.Name != "" {
			out[h.ID] = h.Name
		}
	}
	if len(out) == 0 {
		startup.Warnf("live state: hero cache is empty; roster cannot attach until you sync once")
	}
	return out
}

func runDebugState(ctx context.Context, tr *track.Tracker, pipe *gsi.Pipe, dbg liveDebug, geo *worldmap.Geometry, lanes *lanestate.Tracker, regionGraph *worldmap.RegionGraph) {
	printOnce := func() {
		st := tr.Snapshot()
		stats := tr.StatsSnapshot()
		ps := pipe.Stats()
		var body string
		if dbg.Format == "json" {
			raw, err := st.DebugJSON()
			if err != nil {
				fmt.Fprintf(dbg.Out, "debug-state json: %v\n", err)
				return
			}
			body = string(raw)
		} else {
			body = st.FormatLive()
			var ms *mapstate.State
			var ls *lanestate.State
			if geo != nil {
				built := mapstate.Build(st, geo)
				ms = &built
				body += "\n" + built.Format()
			}
			if lanes != nil {
				snap := lanes.Snapshot()
				ls = &snap
				body += "\n" + snap.Format()
			}
			var bs *belief.State
			if ms != nil && regionGraph != nil {
				if built, err := belief.Build(*ms, regionGraph, belief.DefaultConfig()); err == nil {
					bs = &built
					body += "\n" + built.Format()
				}
			}
			if ms != nil {
				if ds, err := dotastate.Build(st, ms, ls, bs); err == nil {
					body += "\n" + ds.Format()
				}
			}
		}
		fmt.Fprintf(dbg.Out, "%s\n  diag: recv=%d proc=%d drop=%d extract_err=%d apply_err=%d unknown_minimap=%d unknown_events=%d\n\n",
			body, ps.Received, ps.Processed, ps.Dropped, ps.ExtractErrors, ps.ApplyErrors,
			stats.UnknownMinimap, stats.UnknownEvents)
	}
	printOnce()
	tick := time.NewTicker(dbg.Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			printOnce()
		}
	}
}
