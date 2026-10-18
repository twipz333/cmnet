package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/macrolabel"
	"cmnet/internal/dota/macrolabel/audit"
	"cmnet/internal/dota/macrolabel/behavioraudit"
	"cmnet/internal/dota/minimap"
	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/replay"
	"cmnet/internal/gsi/track"
)

func newMacroLabelReviewCmd() *cobra.Command {
	var (
		inputPath       string
		outputPath      string
		sessionDir      string
		rosterPath      string
		sampleSize      int
		seed            int
		serveAddr       string
		includeExcluded bool
		reportPath      string
		mapCache        string
		dotaPath        string
	)

	cmd := &cobra.Command{
		Use:   "macro-label-review",
		Short: "Behavior Audit Reviewer v0.1 — human review UI for macro-label audit cases",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Report-only mode: --report [path] reading from --output reviewed NDJSON.
			if serveAddr == "" && reportPath != "" && inputPath == "" {
				src := outputPath
				if src == "" {
					src = reportPath
					reportPath = ""
				}
				return runBehaviorReport(cmd, src, reportPath)
			}

			if inputPath == "" {
				return fmt.Errorf("--input is required (audit NDJSON from macro-label --export-label-audit)")
			}
			if outputPath == "" {
				return fmt.Errorf("--output is required")
			}

			f, err := os.Open(inputPath)
			if err != nil {
				return err
			}
			defer f.Close()
			items, err := audit.LoadAuditNDJSON(f)
			if err != nil {
				return err
			}
			segs := make([]macrolabel.MacroActionSegment, 0, len(items))
			for _, it := range items {
				segs = append(segs, it.Segment)
			}
			if len(segs) == 0 {
				return fmt.Errorf("no segments in %s", inputPath)
			}

			sampled := behavioraudit.StratifiedSample(segs, behavioraudit.SampleConfig{
				Size:            sampleSize,
				Seed:            int64(seed),
				IncludeExcluded: includeExcluded,
			})
			cmd.Printf("sampled %d episodes from %d input items (seed=%d)\n", len(sampled), len(items), seed)

			matCfg := behavioraudit.MaterializeConfig{Seed: int64(seed)}
			if geo, err := worldmap.LoadCurrent(); err == nil {
				matCfg.Geometry = geo
			}
			if sessionDir != "" && rosterPath != "" {
				tracks, err := loadTrackSamples(sessionDir, rosterPath)
				if err != nil {
					cmd.Printf("warning: session trajectories unavailable: %v (continuing without)\n", err)
				} else {
					matCfg.TrackByTime = tracks
					cmd.Printf("loaded trajectory samples at %d distinct game times\n", len(tracks))
				}
			}

			cases := behavioraudit.MaterializeCases(sampled, matCfg)

			existing, err := behavioraudit.ResumeFromOutput(outputPath)
			if err != nil {
				return err
			}
			if len(existing) > 0 {
				cases = behavioraudit.MergeReviews(cases, existing)
				cmd.Printf("resumed %d existing reviews from %s\n", countReviews(cases), outputPath)
			}

			if err := behavioraudit.SaveReviewedNDJSON(outputPath, behavioraudit.CasesToReviewed(cases)); err != nil {
				return err
			}

			if serveAddr == "" && reportPath != "" {
				return writeBehaviorReport(cmd, cases, reportPath)
			}

			if serveAddr == "" {
				serveAddr = "127.0.0.1:32124"
			}

			srv := behavioraudit.NewReviewServerOpts(cases, outputPath, serveAddr, behavioraudit.ReviewServerOptions{
				MapCache: mapCache,
				DotaPath: dotaPath,
			})

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				cmd.Printf("\nStopped. Print report with:\n  advisor macro-label-review --output %s --report %s\n",
					outputPath, filepath.Join(filepath.Dir(outputPath), "behavior-report.json"))
				os.Exit(0)
			}()

			return srv.ListenAndServe()
		},
	}

	cmd.Flags().StringVar(&inputPath, "input", "", "audit NDJSON (from macro-label --export-label-audit)")
	cmd.Flags().StringVar(&outputPath, "output", "", "reviewed NDJSON output (resume if exists)")
	cmd.Flags().StringVar(&sessionDir, "session", "", "optional gsi-probe session for trajectories")
	cmd.Flags().StringVar(&rosterPath, "roster", "", "optional roster JSON (with --session)")
	cmd.Flags().IntVar(&sampleSize, "sample-size", 120, "stratified sample size")
	cmd.Flags().IntVar(&seed, "seed", 42, "deterministic sample seed")
	cmd.Flags().StringVar(&serveAddr, "serve", "", "listen addr (localhost only), e.g. 127.0.0.1:32124")
	cmd.Flags().BoolVar(&includeExcluded, "include-excluded", false, "include EXCLUDED segments in sample")
	cmd.Flags().StringVar(&reportPath, "report", "", "if set without starting long serve intent, write BehaviorReport JSON")
	cmd.Flags().StringVar(&mapCache, "map-cache", minimap.DefaultCacheRel, "local Dota minimap cache dir")
	cmd.Flags().StringVar(&dotaPath, "dota-path", "", "optional Dota install path (status only)")

	return cmd
}

func runBehaviorReport(cmd *cobra.Command, reviewedPath, outPath string) error {
	f, err := os.Open(reviewedPath)
	if err != nil {
		return err
	}
	defer f.Close()
	items, err := behavioraudit.LoadAuditItems(f)
	if err != nil {
		return err
	}
	cases := make([]behavioraudit.AuditCase, 0, len(items))
	for _, it := range items {
		c := it.Case
		if it.Review != nil {
			c.Review = it.Review
		}
		cases = append(cases, c)
	}
	return writeBehaviorReport(cmd, cases, outPath)
}

func writeBehaviorReport(cmd *cobra.Command, cases []behavioraudit.AuditCase, outPath string) error {
	rep := behavioraudit.BuildReport(cases)
	cmd.Printf("behavior report: reviewed=%d/%d precision=%.3f (CONFIRMED=%d REJECTED=%d UNCLEAR=%d excluded from denom)\n",
		rep.ReviewedCount, rep.TotalCases, rep.BehaviorPrecision, rep.Confirmed, rep.Rejected, rep.Unclear)
	cmd.Printf("confidence-weighted=%.3f unlabeled_recovery=%.3f rejected_top1=%.3f top3=%.3f\n",
		rep.ConfidenceWeightedPrecision, rep.UnlabeledRecoveryRate, rep.RejectedTop1Recovery, rep.RejectedTop3Recovery)
	if len(rep.RecommendedNextInvestigation) > 0 {
		cmd.Printf("next:\n")
		for _, s := range rep.RecommendedNextInvestigation {
			cmd.Printf("  - %s\n", s)
		}
	}
	if outPath != "" {
		dir := filepath.Dir(outPath)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(outPath, b, 0o644); err != nil {
			return err
		}
		cmd.Printf("report written to %s\n", outPath)
	}
	return nil
}

func countReviews(cases []behavioraudit.AuditCase) int {
	n := 0
	for _, c := range cases {
		if c.Review != nil && c.Review.IsReviewed() {
			n++
		}
	}
	return n
}

func loadTrackSamples(sessionDir, rosterPath string) (map[int]behavioraudit.TrackSample, error) {
	roster, err := track.LoadRosterFile(rosterPath)
	if err != nil {
		return nil, err
	}
	if !roster.Valid() {
		return nil, fmt.Errorf("invalid roster")
	}
	geo, err := worldmap.LoadCurrent()
	if err != nil {
		return nil, err
	}
	laneGeo, err := worldmap.LoadLanesCurrent()
	if err != nil {
		return nil, err
	}

	sess, err := replay.Open(sessionDir)
	if err != nil {
		return nil, err
	}

	out := map[int]behavioraudit.TrackSample{}
	lastSec := -1_000_000
	_, err = replay.Run(sess, replay.Options{
		Roster:   roster,
		Geometry: geo,
		Lanes:    laneGeo,
		OnTick: func(seq uint64, st track.State) {
			t, ok := st.Clock.Instant()
			if !ok {
				return
			}
			if t == lastSec {
				return
			}
			lastSec = t
			sample := behavioraudit.TrackSample{GameTime: t}
			for _, a := range st.Allies {
				if len(a.PositionCandidates) == 0 {
					continue
				}
				p := a.PositionCandidates[0]
				sample.Allies = append(sample.Allies, behavioraudit.TrajectoryPoint{
					Hero: a.Hero, X: float64(p.X), Y: float64(p.Y), GameTime: t, Observed: a.CurrentlyObserved,
				})
			}
			for _, e := range st.Enemies {
				if e.VisibleNow && len(e.VisiblePositions) > 0 {
					p := e.VisiblePositions[0]
					sample.Enemies = append(sample.Enemies, behavioraudit.TrajectoryPoint{
						Hero: e.Hero, X: float64(p.X), Y: float64(p.Y), GameTime: t, Observed: true,
					})
				} else if len(e.LastSeenPositions) > 0 {
					p := e.LastSeenPositions[0]
					sample.Enemies = append(sample.Enemies, behavioraudit.TrajectoryPoint{
						Hero: e.Hero, X: float64(p.X), Y: float64(p.Y), GameTime: t, Observed: false,
					})
				}
			}
			out[t] = sample
		},
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
