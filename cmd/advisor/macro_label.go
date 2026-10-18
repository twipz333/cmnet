package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
	"cmnet/internal/dota/macrolabel/adapter/current_capture"
	"cmnet/internal/dota/macrolabel/audit"
	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/observe"
	"cmnet/internal/gsi/replay"
	"cmnet/internal/gsi/track"
)

func newMacroLabelCmd() *cobra.Command {
	var (
		sessionDir       string
		rosterPath       string
		exportPath       string
		exportLabelAudit string
		exportReport     string
		lookback         float64
		lookahead        float64
		sampleN          int
		auditSampleN     int
		sampleStatus     string
		traceTime        int
		seed             int
	)

	cmd := &cobra.Command{
		Use:   "macro-label",
		Short: "Replay MacroAction labeler v0.2 over a gsi-probe capture (offline weak supervision + audit)",
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
				return fmt.Errorf("invalid roster: ally_side must be radiant or dire")
			}

			geo, err := worldmap.LoadCurrent()
			if err != nil {
				return err
			}
			lanes, err := worldmap.LoadLanesCurrent()
			if err != nil {
				return err
			}

			sideStr := "radiant"
			if roster.AllySide == observe.TeamDire {
				sideStr = "dire"
			}

			ad := current_capture.NewAdapter(geo, lanes, sideStr)

			replaySess, err := replay.Open(sessionDir)
			if err != nil {
				return err
			}

			cfg := macrolabel.DefaultConfig()
			if lookback > 0 {
				cfg.LookbackSeconds = lookback
			}
			if lookahead > 0 {
				cfg.LookaheadSeconds = lookahead
			}

			space := action.DefaultSpaceV0()
			labeler := macrolabel.NewV0(cfg, space)

			frames := make([]macrolabel.BehaviorFrame, 0, 1024)
			_, err = replay.Run(replaySess, replay.Options{
				Roster:   roster,
				Geometry: geo,
				Lanes:    lanes,
				OnTick: func(seq uint64, st track.State) {
					f, ok, err := ad.Frame(seq, st)
					if err != nil || !ok {
						return
					}
					frames = append(frames, f)
				},
			})
			if err != nil {
				return err
			}

			segs, rep, err := labeler.Label(frames)
			if err != nil {
				return err
			}

			cmd.Printf("frames=%d segments=%d labeler=%s config=%s\n",
				len(frames), rep.SegmentCount, macrolabel.LabelerVersion, cfg.ConfigVersion)
			cmd.Printf("accounting: total=%.1fs eligible=%.1fs labeled=%.1fs (%.1f%% eligible) ambiguous=%.1fs unlabeled=%.1fs excluded=%.1fs delta=%.3fs\n",
				rep.TotalDurationSeconds, rep.EligibleDurationSeconds,
				rep.LabeledDurationSeconds, rep.EligibleLabelCoveragePct,
				rep.AmbiguousDurationSeconds, rep.UnlabeledDurationSeconds,
				rep.ExcludedDurationSeconds, rep.AccountingDeltaSeconds,
			)

			if len(rep.StatusSegmentCounts) > 0 {
				cmd.Printf("status segments: labeled=%d ambiguous=%d unlabeled=%d excluded=%d\n",
					rep.StatusSegmentCounts[macrolabel.LabelStatusLabeled],
					rep.StatusSegmentCounts[macrolabel.LabelStatusAmbiguous],
					rep.StatusSegmentCounts[macrolabel.LabelStatusUnlabeled],
					rep.StatusSegmentCounts[macrolabel.LabelStatusExcluded],
				)
			}

			if traceTime >= 0 {
				for _, s := range segs {
					if traceTime >= s.StartGameTime && traceTime <= s.EndGameTime {
						cmd.Printf("\nTrace @ t=%d: status=%s %s ambiguity=%s exclusion=%s rejected=%d\n",
							traceTime, s.Status, segmentActionStr(space, s), s.AmbiguityReason, s.ExclusionReason, len(s.RejectedCandidates))
						if s.ResolverDiagnostics != nil {
							d := s.ResolverDiagnostics
							cmd.Printf("  resolver: resolution=%s candidates=%d top=%.3f second=%.3f margin=%.3f\n",
								d.Resolution, d.CandidateCount, d.TopConfidence, d.SecondConfidence, d.Margin)
						}
						break
					}
				}
			}

			// Diagnostics (offline).
			statusCounts := map[macrolabel.LabelStatus]int{}

			kindCounts := map[action.ActionKind]int{}
			actionCounts := map[int]int{}
			confidenceBuckets := map[string]int{}

			kindDurations := map[action.ActionKind][]float64{}
			kindConfs := map[action.ActionKind][]float64{}

			actionDurations := map[int][]float64{}
			actionConfs := map[int][]float64{}

			expKinds := []action.ActionKind{action.KindHold, action.KindReset, action.KindRetreat}
			expKindSet := map[action.ActionKind]struct{}{}
			for _, k := range expKinds {
				expKindSet[k] = struct{}{}
			}
			expEvidenceTypes := map[action.ActionKind]map[string]int{}

			var labeledConf []float64
			for _, s := range segs {
				statusCounts[s.Status]++

				if s.Status != macrolabel.LabelStatusLabeled || s.Action == nil {
					continue
				}

				kind := s.Action.Kind
				id := s.Action.ID

				kindCounts[kind]++
				actionCounts[id]++

				kindDurations[kind] = append(kindDurations[kind], s.DurationSeconds)
				actionDurations[id] = append(actionDurations[id], s.DurationSeconds)

				if s.Confidence != nil {
					c := *s.Confidence
					labeledConf = append(labeledConf, c)
					b := confBucket(c)
					confidenceBuckets[b]++

					kindConfs[kind] = append(kindConfs[kind], c)
					actionConfs[id] = append(actionConfs[id], c)
				}

				if _, ok := expKindSet[kind]; ok {
					if expEvidenceTypes[kind] == nil {
						expEvidenceTypes[kind] = map[string]int{}
					}
					for _, ev := range s.Evidence {
						if ev.Type == "" {
							continue
						}
						expEvidenceTypes[kind][ev.Type]++
					}
				}
			}

			coveragePct := 0.0
			if rep.TotalDurationSeconds > 0 {
				coveragePct = 100.0 * rep.LabeledDurationSeconds / rep.TotalDurationSeconds
			}
			cmd.Printf("\nLabel coverage (durations): labeled=%.1fs (%.1f%%), ambiguous=%.1fs, unlabeled=%.1fs\n",
				rep.LabeledDurationSeconds, coveragePct, rep.AmbiguousDurationSeconds, rep.UnlabeledDurationSeconds)
			cmd.Printf("Segments by status: labeled=%d ambiguous=%d unlabeled=%d (confidence_median=%.3f)\n",
				statusCounts[macrolabel.LabelStatusLabeled], statusCounts[macrolabel.LabelStatusAmbiguous], statusCounts[macrolabel.LabelStatusUnlabeled], rep.ConfidenceMedian)

			cmd.Printf("\nDistribution by Kind (labeled):\n")
			for _, k := range action.KindVocab() {
				durs := kindDurations[k]
				if len(durs) == 0 {
					continue
				}
				total := sumFloat64(durs)
				medianDur := medianFloat64(durs)
				meanConf := meanFloat64(kindConfs[k])
				pctLabeled := 0.0
				if rep.LabeledDurationSeconds > 0 {
					pctLabeled = 100.0 * total / rep.LabeledDurationSeconds
				}
				cmd.Printf("  %-18s seg=%d total=%.1fs (%.1f%%) median=%.2fs meanConf=%.3f\n",
					k, len(durs), total, pctLabeled, medianDur, meanConf)
			}

			ids := make([]int, 0, len(actionCounts))
			for id := range actionCounts {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			if len(ids) > 0 {
				cmd.Printf("\nDistribution by ActionID (labeled):\n")
				for _, id := range ids {
					durs := actionDurations[id]
					if len(durs) == 0 {
						continue
					}
					total := sumFloat64(durs)
					medianDur := medianFloat64(durs)
					meanConf := meanFloat64(actionConfs[id])
					pctLabeled := 0.0
					if rep.LabeledDurationSeconds > 0 {
						pctLabeled = 100.0 * total / rep.LabeledDurationSeconds
					}
					a, _ := space.ByID(id)
					cmd.Printf("  %02d %-16s seg=%d total=%.1fs (%.1f%%) median=%.2fs meanConf=%.3f\n",
						id, a.String(), len(durs), total, pctLabeled, medianDur, meanConf)
				}
			}

			cmd.Printf("\nConfidence buckets (labeled):\n")
			for _, b := range []string{"[0.9,1]", "[0.8,0.9)", "[0.7,0.8)", "[0.6,0.7)", "[0,0.6)"} {
				if confidenceBuckets[b] != 0 {
					cmd.Printf("  %-10s %d\n", b, confidenceBuckets[b])
				}
			}
			// Candidate overlaps for AMBIGUOUS segments (competing candidates).
			overlap := map[[2]int]int{}
			for _, s := range segs {
				if s.Status != macrolabel.LabelStatusAmbiguous {
					continue
				}

				idsSet := map[int]struct{}{}
				for _, c := range s.Competing {
					idsSet[c.ActionID] = struct{}{}
				}
				ids := make([]int, 0, len(idsSet))
				for id := range idsSet {
					ids = append(ids, id)
				}
				sort.Ints(ids)
				for i := 0; i < len(ids); i++ {
					for j := i + 1; j < len(ids); j++ {
						overlap[[2]int{ids[i], ids[j]}]++
					}
				}
			}
			if len(overlap) > 0 {
				pairs := make([][2]int, 0, len(overlap))
				for p := range overlap {
					pairs = append(pairs, p)
				}
				sort.SliceStable(pairs, func(i, j int) bool {
					if overlap[pairs[i]] == overlap[pairs[j]] {
						if pairs[i][0] == pairs[j][0] {
							return pairs[i][1] < pairs[j][1]
						}
						return pairs[i][0] < pairs[j][0]
					}
					return overlap[pairs[i]] > overlap[pairs[j]]
				})
				cmd.Printf("\nTop ambiguous overlap pairs (AMBIGUOUS competing candidates):\n")
				for i := 0; i < min(10, len(pairs)); i++ {
					p := pairs[i]
					a1, _ := space.ByID(p[0])
					a2, _ := space.ByID(p[1])
					cmd.Printf("  (%02d %-12s) ↔ (%02d %-12s)  x%d\n", p[0], a1.String(), p[1], a2.String(), overlap[p])
				}
			}

			ambSegs := make([]macrolabel.MacroActionSegment, 0, len(segs))
			for _, s := range segs {
				if s.Status == macrolabel.LabelStatusAmbiguous {
					ambSegs = append(ambSegs, s)
				}
			}
			sort.SliceStable(ambSegs, func(i, j int) bool {
				best1I, best1J := -1.0, -1.0
				for _, c := range ambSegs[i].Competing {
					if c.Confidence > best1I {
						best1I = c.Confidence
					}
				}
				for _, c := range ambSegs[j].Competing {
					if c.Confidence > best1J {
						best1J = c.Confidence
					}
				}
				if best1I == best1J {
					return ambSegs[i].StartGameTime < ambSegs[j].StartGameTime
				}
				return best1I > best1J
			})

			if len(ambSegs) > 0 {
				cmd.Printf("\nAmbiguous examples (top by competing confidence):\n")
				for i := 0; i < min(5, len(ambSegs)); i++ {
					s := ambSegs[i]
					best1, best2 := -1.0, -1.0
					for _, c := range s.Competing {
						if c.Confidence > best1 {
							best2 = best1
							best1 = c.Confidence
						} else if c.Confidence > best2 {
							best2 = c.Confidence
						}
					}
					if best1 < 0 {
						best1, best2 = 0, 0
					}
					cmd.Printf("  t=%d..%d competing=%s best=%.3f second=%.3f\n",
						s.StartGameTime, s.EndGameTime, competingStr(space, s), best1, best2)
				}
			}
			// Hold/Reset/Retreat are experimental — keep them separate with evidence-type hints.
			cmd.Printf("\nHold/Reset/Retreat audit (counts + typical evidence types):\n")
			for _, k := range expKinds {
				cmd.Printf("  %-18s %d\n", k, kindCounts[k])
				evd := expEvidenceTypes[k]
				if len(evd) == 0 {
					continue
				}
				type kv struct {
					t string
					c int
				}
				pairs := make([]kv, 0, len(evd))
				for t, c := range evd {
					pairs = append(pairs, kv{t: t, c: c})
				}
				sort.SliceStable(pairs, func(i, j int) bool {
					if pairs[i].c == pairs[j].c {
						return pairs[i].t < pairs[j].t
					}
					return pairs[i].c > pairs[j].c
				})
				limit := min(5, len(pairs))
				out := make([]string, 0, limit)
				for i := 0; i < limit; i++ {
					out = append(out, fmt.Sprintf("%s(%d)", pairs[i].t, pairs[i].c))
				}
				cmd.Printf("    evidence_types=%v\n", out)
			}
			// Optional random sample for manual audit.
			if sampleN > 0 {
				r := rand.New(rand.NewSource(int64(seed)))
				idxs := r.Perm(len(segs))
				n := min(sampleN, len(segs))
				cmd.Printf("\nManual audit sample (%d segments, seed=%d):\n", n, seed)
				for i := 0; i < n; i++ {
					s := segs[idxs[i]]
					cmd.Printf("  t=%d..%d status=%s %s\n", s.StartGameTime, s.EndGameTime, s.Status, segmentActionStr(space, s))
					if s.Status == macrolabel.LabelStatusAmbiguous {
						cmd.Printf("    competing=%s\n", competingStr(space, s))
						evTypes := make([]string, 0, 4)
						for _, ev := range s.Evidence {
							if ev.Type == "" {
								continue
							}
							evTypes = append(evTypes, ev.Type)
							if len(evTypes) >= 4 {
								break
							}
						}
						cmd.Printf("    evidence_types=%v\n", evTypes)
					}
				}
			}

			if exportPath != "" {
				if err := os.MkdirAll(filepath.Dir(exportPath), 0o755); err != nil {
					return err
				}
				f, err := os.Create(exportPath)
				if err != nil {
					return err
				}
				defer f.Close()

				enc := json.NewEncoder(f)
				for _, s := range segs {
					if err := enc.Encode(s); err != nil {
						return err
					}
				}
				cmd.Printf("\nexported %d segments to %s\n", len(segs), exportPath)
			}

			anchors := macrolabel.ListSmokeAnchors(frames)
			cmd.Printf("\nSmoke source events (deduped anchors): %d\n", len(anchors))
			for i, a := range anchors {
				if i >= 10 {
					break
				}
				cmd.Printf("  event t=%d seq=%d anchor_id=%s frame=%d\n", a.GameTime, a.Seq, a.ID, a.FrameIndex)
			}

			if exportReport != "" {
				if err := os.MkdirAll(filepath.Dir(exportReport), 0o755); err != nil {
					return err
				}
				rf, err := os.Create(exportReport)
				if err != nil {
					return err
				}
				defer rf.Close()
				enc := json.NewEncoder(rf)
				enc.SetIndent("", "  ")
				if err := enc.Encode(rep); err != nil {
					return err
				}
				cmd.Printf("\nexported report to %s\n", exportReport)
			}

			// Export audit NDJSON for behavior reviewer.
			// If --export-label-audit is set: export ALL segments (reviewer does stratified sample).
			// Optional --audit-sample N still exports a reduced set for quick diagnostics.
			if exportLabelAudit != "" {
				filtered := segs
				if sampleStatus != "" {
					want := macrolabel.LabelStatus(sampleStatus)
					tmp := make([]macrolabel.MacroActionSegment, 0, len(segs))
					for _, s := range segs {
						if s.Status == want {
							tmp = append(tmp, s)
						}
					}
					filtered = tmp
				}
				var items []audit.AuditItem
				if auditSampleN > 0 {
					items = audit.BuildAuditSample(filtered, auditSampleN, int64(seed), space)
				} else {
					items = make([]audit.AuditItem, 0, len(filtered))
					for _, s := range filtered {
						items = append(items, audit.AuditItem{
							AuditID:   audit.AuditID(s),
							MatchID:   s.MatchID,
							Side:      s.Side,
							StartTime: s.StartGameTime,
							EndTime:   s.EndGameTime,
							Status:    s.Status,
							Segment:   s,
						})
					}
				}
				if err := audit.ExportAuditNDJSON(exportLabelAudit, items); err != nil {
					return err
				}
				cmd.Printf("exported %d audit items to %s\n", len(items), exportLabelAudit)
			}

			// Smoke: print all segments that mention SMOKE candidates (labeled or ambiguous).
			smokeSegs := make([]macrolabel.MacroActionSegment, 0, len(segs))
			for _, s := range segs {
				// Labeled SMOKE.
				if s.Status == macrolabel.LabelStatusLabeled && s.Action != nil && s.Action.Kind == action.KindSmoke {
					smokeSegs = append(smokeSegs, s)
					continue
				}
				// Ambiguous SMOKE in competing candidates.
				if s.Status == macrolabel.LabelStatusAmbiguous {
					for _, c := range s.Competing {
						a, ok := space.ByID(c.ActionID)
						if !ok {
							continue
						}
						if a.Kind == action.KindSmoke {
							smokeSegs = append(smokeSegs, s)
							break
						}
					}
				}
			}
			cmd.Printf("\nSmoke segments (SMOKE kind in labeled/competing): %d\n", len(smokeSegs))
			for _, s := range smokeSegs {
				confStr := "n/a"
				if s.Confidence != nil {
					confStr = fmt.Sprintf("%.3f", *s.Confidence)
				}
				smokeNames := make([]string, 0, 4)
				if s.Status == macrolabel.LabelStatusLabeled && s.Action != nil && s.Action.Kind == action.KindSmoke {
					smokeNames = append(smokeNames, s.Action.String())
				} else {
					for _, c := range s.Competing {
						a, ok := space.ByID(c.ActionID)
						if !ok {
							continue
						}
						if a.Kind != action.KindSmoke {
							continue
						}
						smokeNames = append(smokeNames, fmt.Sprintf("%s@%.3f", a.String(), c.Confidence))
					}
				}
				evTypes := make([]string, 0, 4)
				for _, ev := range s.Evidence {
					if ev.Type == "" {
						continue
					}
					evTypes = append(evTypes, ev.Type)
					if len(evTypes) >= 4 {
						break
					}
				}
				cmd.Printf("  t=%d..%d status=%s conf=%s smoke=%v evidence_types=%v\n",
					s.StartGameTime, s.EndGameTime, s.Status, confStr, smokeNames, evTypes)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionDir, "session", "", "gsi-probe capture directory")
	cmd.Flags().StringVar(&rosterPath, "roster", "", "roster JSON")
	cmd.Flags().StringVar(&exportPath, "export", "", "export segments NDJSON")
	cmd.Flags().Float64Var(&lookback, "lookback-seconds", 0, "lookback seconds (bootstrap tuning)")
	cmd.Flags().Float64Var(&lookahead, "lookahead-seconds", 0, "lookahead seconds")
	cmd.Flags().IntVar(&sampleN, "sample", 0, "print N deterministic audit samples to stdout")
	cmd.Flags().IntVar(&auditSampleN, "audit-sample", 0, "export N stratified audit items (overrides --sample for export)")
	cmd.Flags().StringVar(&sampleStatus, "sample-status", "", "filter audit export/sample by status (LABELED|AMBIGUOUS|UNLABELED|EXCLUDED)")
	cmd.Flags().StringVar(&exportLabelAudit, "export-label-audit", "", "export audit NDJSON for human review")
	cmd.Flags().StringVar(&exportReport, "export-report", "", "export label report JSON")
	cmd.Flags().IntVar(&traceTime, "trace-time", -1, "print resolver trace for segment covering this game time (-1=off)")
	cmd.Flags().IntVar(&seed, "seed", int(time.Now().Unix()), "sampling seed")
	return cmd
}

func confBucket(c float64) string {
	if c >= 0.9 {
		return "[0.9,1]"
	}
	if c >= 0.8 {
		return "[0.8,0.9)"
	}
	if c >= 0.7 {
		return "[0.7,0.8)"
	}
	if c >= 0.6 {
		return "[0.6,0.7)"
	}
	return "[0,0.6)"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sumFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum
}

func meanFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	return sumFloat64(vals) / float64(len(vals))
}

func medianFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := make([]float64, len(vals))
	copy(cp, vals)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func segmentActionStr(space action.ActionSpace, s macrolabel.MacroActionSegment) string {
	if s.Status != macrolabel.LabelStatusLabeled || s.Action == nil {
		return ""
	}
	return s.Action.String()
}

func competingStr(space action.ActionSpace, s macrolabel.MacroActionSegment) string {
	if s.Status != macrolabel.LabelStatusAmbiguous {
		return ""
	}
	ids := make([]int, 0, len(s.Competing))
	for _, c := range s.Competing {
		ids = append(ids, c.ActionID)
	}
	sort.Ints(ids)
	out := ""
	for i, id := range ids {
		a, _ := space.ByID(id)
		if i > 0 {
			out += ", "
		}
		out += a.String()
	}
	return out
}
