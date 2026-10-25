package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cmnet/internal/bundle"
	dotastate "cmnet/internal/dota/state"
	"cmnet/internal/dota/worldmap"
	"cmnet/internal/gsi/belief"
	"cmnet/internal/gsi/lanestate"
	"cmnet/internal/gsi/mapstate"
	"cmnet/internal/gsi/track"
)

type dotaStateReplayExporter struct {
	geo      *worldmap.Geometry
	laneGeo  *worldmap.LaneGeometry
	laneTr   *lanestate.Tracker
	graph    *worldmap.RegionGraph
	cfg      belief.Config
	interval int

	stateFile  *os.File
	vectorFile *os.File

	rows           int
	vectorRows     int
	lastBucket     *int
	vectorFailures int
	counts         map[string]int
}

type dotaStateExportRow struct {
	SchemaVersion       string          `json:"schema_version"`
	MatchID             string          `json:"match_id,omitempty"`
	Seq                 uint64          `json:"seq"`
	GameTime            int             `json:"game_time"`
	MapGeometryVersion  string          `json:"map_geometry_version,omitempty"`
	LaneGeometryVersion string          `json:"lane_geometry_version,omitempty"`
	RegionGraphVersion  string          `json:"region_graph_version,omitempty"`
	BeliefModelVersion  string          `json:"belief_model_version,omitempty"`
	AdvisorVersion      string          `json:"advisor_version,omitempty"`
	State               dotastate.State `json:"state"`
}

type dotaVectorExportRow struct {
	SchemaVersion       string                         `json:"schema_version"`
	MatchID             string                         `json:"match_id,omitempty"`
	Seq                 uint64                         `json:"seq"`
	GameTime            int                            `json:"game_time"`
	MapGeometryVersion  string                         `json:"map_geometry_version,omitempty"`
	LaneGeometryVersion string                         `json:"lane_geometry_version,omitempty"`
	RegionGraphVersion  string                         `json:"region_graph_version,omitempty"`
	BeliefModelVersion  string                         `json:"belief_model_version,omitempty"`
	AdvisorVersion      string                         `json:"advisor_version,omitempty"`
	Vector              dotastate.NumericFeatureVector `json:"vector"`
}

func newDotaStateReplayExporter(geo *worldmap.Geometry, laneGeo *worldmap.LaneGeometry, graph *worldmap.RegionGraph, cfg belief.Config, statePath, vectorPath string, interval int) (*dotaStateReplayExporter, error) {
	if interval <= 0 {
		interval = 1
	}
	ex := &dotaStateReplayExporter{geo: geo, laneGeo: laneGeo, graph: graph, cfg: cfg, interval: interval, counts: map[string]int{}}
	if laneGeo != nil {
		ex.laneTr = lanestate.New(laneGeo)
	}
	if statePath != "" {
		if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
			return nil, err
		}
		f, err := os.Create(statePath)
		if err != nil {
			return nil, err
		}
		ex.stateFile = f
	}
	if vectorPath != "" {
		if err := os.MkdirAll(filepath.Dir(vectorPath), 0o755); err != nil {
			return nil, err
		}
		f, err := os.Create(vectorPath)
		if err != nil {
			if ex.stateFile != nil {
				ex.stateFile.Close()
			}
			return nil, err
		}
		ex.vectorFile = f
	}
	return ex, nil
}

func (e *dotaStateReplayExporter) Close() error {
	var errs []string
	if e == nil {
		return nil
	}
	if e.stateFile != nil {
		if err := e.stateFile.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if e.vectorFile != nil {
		if err := e.vectorFile.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (e *dotaStateReplayExporter) OnTick(seq uint64, st track.State) {
	if e == nil || e.geo == nil || e.graph == nil || st.Clock.GameTime == nil {
		return
	}
	bucket := *st.Clock.GameTime / e.interval
	if e.lastBucket != nil && *e.lastBucket == bucket {
		return
	}
	e.lastBucket = &bucket

	ms := mapstate.Build(st, e.geo)
	var ls *lanestate.State
	if e.laneGeo != nil && e.laneTr != nil {
		obs := lanestate.Build(st, e.laneGeo)
		e.laneTr.Apply(obs)
		snap := e.laneTr.Snapshot()
		ls = &snap
	}
	bs, err := belief.Build(ms, e.graph, e.cfg)
	if err != nil {
		return
	}
	ds, err := dotastate.Build(st, &ms, ls, &bs)
	if err != nil {
		return
	}
	e.rows++
	e.noteAvailability(ds)
	if e.stateFile != nil {
		row := dotaStateExportRow{
			SchemaVersion:       ds.Meta.SchemaVersion,
			MatchID:             ds.Meta.MatchID.Value,
			Seq:                 seq,
			GameTime:            *st.Clock.GameTime,
			MapGeometryVersion:  ds.Meta.MapGeometryVersion,
			LaneGeometryVersion: ds.Meta.LaneGeometryVersion,
			RegionGraphVersion:  ds.Meta.RegionGraphVersion,
			BeliefModelVersion:  ds.Meta.BeliefModelVersion,
			AdvisorVersion:      bundle.CodeVersion,
			State:               ds,
		}
		_ = json.NewEncoder(e.stateFile).Encode(row)
	}
	if e.vectorFile != nil {
		vec, err := dotastate.Encode(ds)
		if err != nil {
			e.vectorFailures++
			return
		}
		e.vectorRows++
		if e.counts["__numeric_feature_count"] == 0 {
			e.counts["__numeric_feature_count"] = len(vec.Values)
		}
		row := dotaVectorExportRow{
			SchemaVersion:       ds.Meta.SchemaVersion,
			MatchID:             ds.Meta.MatchID.Value,
			Seq:                 seq,
			GameTime:            *st.Clock.GameTime,
			MapGeometryVersion:  ds.Meta.MapGeometryVersion,
			LaneGeometryVersion: ds.Meta.LaneGeometryVersion,
			RegionGraphVersion:  ds.Meta.RegionGraphVersion,
			BeliefModelVersion:  ds.Meta.BeliefModelVersion,
			AdvisorVersion:      bundle.CodeVersion,
			Vector:              vec,
		}
		_ = json.NewEncoder(e.vectorFile).Encode(row)
	}
}

func (e *dotaStateReplayExporter) noteAvailability(st dotastate.State) {
	e.bump("self.region", st.Self.Region.Available)
	e.bump("team.grouped_ratio", st.Team.GroupedRatio.Available)
	e.bump("enemy.last_seen_age", anyEnemy(st.Enemies, func(en dotastate.EnemyHero) bool { return en.LastSeenAge.Available }))
	e.bump("lane.top.our_front", st.Lanes.Top.OurFrontTeamProgress.Available)
	e.bump("lane.top.enemy_visible_front", st.Lanes.Top.VisibleEnemyFrontProgress.Available)
	e.bump("lane.top.enemy_last_seen_front", st.Lanes.Top.EnemyLastSeenFrontProgress.Available)
	e.bump("belief.localized_or_observed", anyEnemy(st.Enemies, func(en dotastate.EnemyHero) bool { return en.BeliefStatus != "UNLOCALIZED" && en.BeliefStatus != "" }))
	e.bump("belief.unlocalized", anyEnemy(st.Enemies, func(en dotastate.EnemyHero) bool { return en.BeliefStatus == "UNLOCALIZED" }))
}

func (e *dotaStateReplayExporter) bump(key string, ok bool) {
	if ok {
		e.counts[key]++
	}
}

func anyEnemy(in []dotastate.EnemyHero, fn func(dotastate.EnemyHero) bool) bool {
	for _, enemy := range in {
		if fn(enemy) {
			return true
		}
	}
	return false
}

func (e *dotaStateReplayExporter) Summary() string {
	if e == nil || e.rows == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nDotaState export:\n")
	fmt.Fprintf(&b, "  rows: %d\n", e.rows)
	fmt.Fprintf(&b, "  vector_rows: %d\n", e.vectorRows)
	fmt.Fprintf(&b, "  vector_failures: %d\n", e.vectorFailures)
	fmt.Fprintf(&b, "  semantic_feature_count: %d\n", len(dotastate.SemanticFeatureRegistry()))

	featureCount := 0
	if e.vectorRows > 0 {
		featureCount = e.counts["__numeric_feature_count"]
	}
	fmt.Fprintf(&b, "  numeric_feature_count: %d\n", featureCount)
	fmt.Fprintf(&b, "\nAvailability:\n")
	keys := make([]string, 0, len(e.counts))
	for k := range e.counts {
		if strings.HasPrefix(k, "__") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s: %.1f%%\n", k, 100*float64(e.counts[k])/float64(e.rows))
	}
	return b.String()
}
