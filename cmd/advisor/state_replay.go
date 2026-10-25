package main

import (
	"fmt"
	"time"

	dotastate "cmnet/internal/dota/state"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/belief"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/replay"
	"cmnet/internal/gsi/track"
)

func newStateReplayCmd() *cobra.Command {
	var (
		sessionDir       string
		rosterPath       string
		traceHero        string
		traceBeliefHero  string
		debugJSON        bool
		debugMap         bool
		debugLanes       bool
		debugBelief      bool
		debugDotaState   bool
		beliefEval       bool
		exportDotaState  string
		exportFeatureVec string
		exportInterval   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "state-replay",
		Short: "Replay a gsi-probe capture through ObservationExtractor and StateTracker",
		Long: "Reads index.ndjson and payloads/ from a gsi-probe session, feeds every body " +
			"through the production extractor and tracker (no separate parser), and prints a summary. " +
			"Does not sleep between payloads.\n\n" +
			"Roster is required because player-mode GSI does not include the enemy five as a list. " +
			"Pass a JSON file: {\"ally_side\":\"dire\",\"allies\":[\"npc_dota_hero_...\"],\"enemies\":[...]}.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionDir == "" {
				return fmt.Errorf("--session is required")
			}
			if rosterPath == "" {
				return fmt.Errorf("--roster is required")
			}
			roster, err := track.LoadRosterFile(rosterPath)
			if err != nil {
				return err
			}
			if !roster.Valid() {
				return fmt.Errorf("roster: ally_side must be radiant or dire, with allies and enemies")
			}
			sess, err := replay.Open(sessionDir)
			if err != nil {
				return err
			}
			geo, err := worldmap.LoadCurrent()
			if err != nil {
				return err
			}
			lanes, err := worldmap.LoadLanesCurrent()
			if err != nil {
				return err
			}
			graph, err := worldmap.LoadRegionGraphCurrent()
			if err != nil {
				return err
			}
			collector := newBeliefReplayCollector(geo, graph, belief.DefaultConfig(), traceBeliefHero)
			exporter, err := newDotaStateReplayExporter(geo, lanes, graph, belief.DefaultConfig(), exportDotaState, exportFeatureVec, int(exportInterval/time.Second))
			if err != nil {
				return err
			}
			defer exporter.Close()
			onTick := func(seq uint64, st track.State) {
				collector.OnTick(seq, st)
				exporter.OnTick(seq, st)
			}
			res, err := replay.Run(sess, replay.Options{Roster: roster, TraceHero: traceHero, Geometry: geo, Lanes: lanes, OnTick: onTick})
			if err != nil {
				return err
			}
			beliefOnly := beliefEval && traceHero == "" && traceBeliefHero == "" && !debugMap && !debugLanes && !debugBelief && !debugJSON
			traceBeliefOnly := traceBeliefHero != "" && traceHero == "" && !beliefEval && !debugMap && !debugLanes && !debugBelief && !debugJSON
			if beliefOnly {
				cmd.Print(collector.Summary())
				return nil
			}
			if traceBeliefOnly {
				cmd.Print(collector.Trace())
				return nil
			}
			cmd.Print(res.Summary())
			if exporter != nil {
				cmd.Print(exporter.Summary())
			}
			printMapSamples(cmd, res.Samples)
			printLaneSamples(cmd, res.LaneSamples)
			if traceHero != "" {
				cmd.Printf("\nTrace %s (%d edges):\n", traceHero, len(res.Trace))
				for _, step := range res.Trace {
					gt := "-"
					if step.GameTime != nil {
						gt = fmt.Sprintf("%d", *step.GameTime)
					}
					if step.Visible {
						pos := "-"
						if len(step.Positions) > 0 {
							pos = fmt.Sprintf("%v", step.Positions)
						}
						reg := "-"
						if len(step.Regions) > 0 {
							reg = fmt.Sprintf("%v", step.Regions)
						}
						cmd.Printf("  seq=%d game_time=%s visible=true pos=%s region=%s\n",
							step.Seq, gt, pos, reg)
						continue
					}
					last := "-"
					if len(step.LastSeenRegions) > 0 {
						last = fmt.Sprintf("%v", step.LastSeenRegions)
					}
					cmd.Printf("  seq=%d game_time=%s visible=false last_seen_region=%s age=%s\n",
						step.Seq, gt, last, fmtAge(step.Age))
				}
			}

			if debugMap {
				cmd.Printf("\n%s\n", res.LastMap.Format())
			}
			if debugLanes {
				cmd.Printf("\n%s\n", res.LastLanes.Format())
			}
			if debugBelief {
				if bs, err := belief.Build(res.LastMap, graph, belief.DefaultConfig()); err == nil {
					cmd.Printf("\n%s\n", bs.Format())
				}
			}
			if debugDotaState {
				var ls *lanestate.State
				if res.LastLanes.GeometryVersion != "" {
					ls = &res.LastLanes
				}
				var bs *belief.State
				if built, err := belief.Build(res.LastMap, graph, belief.DefaultConfig()); err == nil {
					bs = &built
				}
				if ds, err := dotastate.Build(res.State, &res.LastMap, ls, bs); err == nil {
					cmd.Printf("\n%s\n", ds.Format())
				}
			}
			if debugJSON {
				raw, err := res.State.DebugJSON()
				if err != nil {
					return err
				}
				cmd.Printf("\n%s\n", raw)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&sessionDir, "session", "", "path to a gsi-probe session directory")
	f.StringVar(&rosterPath, "roster", "", "path to roster JSON (ally_side, allies, enemies)")
	f.StringVar(&traceHero, "trace-hero", "", "print visibility edges for this npc_dota_hero_*")
	f.StringVar(&traceBeliefHero, "trace-belief-hero", "", "print belief transitions for this npc_dota_hero_*")
	f.BoolVar(&debugJSON, "debug-json", false, "print a diagnostic JSON snapshot at the end")
	f.BoolVar(&debugMap, "debug-map", false, "print the final MapState snapshot")
	f.BoolVar(&debugLanes, "debug-lanes", false, "print the final LaneState snapshot")
	f.BoolVar(&debugBelief, "debug-belief", false, "print the final BeliefState snapshot")
	f.BoolVar(&debugDotaState, "debug-dota-state", false, "print the final DotaState snapshot")
	f.BoolVar(&beliefEval, "belief-eval", false, "evaluate belief on missing->reappear intervals and validate RegionGraph transitions")
	f.StringVar(&exportDotaState, "export-dota-state", "", "write structured DotaState NDJSON to this path")
	f.StringVar(&exportFeatureVec, "export-feature-vector", "", "write numeric feature-vector NDJSON to this path")
	f.DurationVar(&exportInterval, "state-export-interval", time.Second, "game-time export cadence for DotaState rows")
	return cmd
}

func printMapSamples(cmd *cobra.Command, s replay.MapSamples) {
	cmd.Printf("\nMap samples:\n")
	printSample(cmd, "5 together", s.AllGrouped)
	printSample(cmd, "4+1", s.FourPlusOne)
	printSample(cmd, "split", s.Split)
	printSample(cmd, "allies near Roshan", s.RoshanNear)
}

func printSample(cmd *cobra.Command, label string, s *replay.MapTickSample) {
	if s == nil {
		cmd.Printf("  %s: (not seen)\n", label)
		return
	}
	cmd.Printf("  %s: seq=%d clock=%d cluster=%d/%d ratio=%.2f centroid=%s pitA=%d/%d pitB=%d/%d\n",
		label, s.Seq, s.Clock, s.Grouped, s.Observed, s.Ratio, s.CentroidRegion,
		s.PitAAllies, s.PitAVisEnemies, s.PitBAllies, s.PitBVisEnemies)
}

func fmtAge(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", *v)
}

func printLaneSamples(cmd *cobra.Command, s replay.LaneSamples) {
	cmd.Printf("\nLane samples:\n")
	if s.Snapshot != nil {
		cmd.Printf("  snapshot seq=%d clock=%d\n%s\n", s.Snapshot.Seq, s.Snapshot.Clock, s.Snapshot.Text)
	} else {
		cmd.Printf("  snapshot: (not seen)\n")
	}
	if s.MultiWave != nil {
		cmd.Printf("  multi-wave seq=%d clock=%d\n    %s\n", s.MultiWave.Seq, s.MultiWave.Clock, s.MultiWave.Text)
	} else {
		cmd.Printf("  multi-wave: (not seen)\n")
	}
	if s.Contact != nil {
		cmd.Printf("  contact seq=%d clock=%d\n    %s\n", s.Contact.Seq, s.Contact.Clock, s.Contact.Text)
	} else {
		cmd.Printf("  contact: (not seen)\n")
	}
	if s.Fog != nil && s.Fog.GoneSeq != 0 {
		cmd.Printf("  fog %s visible seq=%d front=%.2f -> gone seq=%d age=%.1fs",
			s.Fog.Lane, s.Fog.VisibleSeq, s.Fog.VisibleFront, s.Fog.GoneSeq, s.Fog.GoneAge)
		if s.Fog.StillSeq != 0 {
			cmd.Printf(" -> still seq=%d age=%.1fs", s.Fog.StillSeq, s.Fog.StillAge)
		}
		if s.Fog.ReappearSeq != 0 {
			cmd.Printf(" -> reappear seq=%d front=%.2f", s.Fog.ReappearSeq, s.Fog.ReappearFront)
		}
		cmd.Printf("\n")
	} else {
		cmd.Printf("  fog transition: (not seen)\n")
	}
}

func replayMapState(res replay.Result, geo *worldmap.Geometry) mapstate.State {
	return mapstate.Build(res.State, geo)
}
