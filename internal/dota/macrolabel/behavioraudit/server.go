package behavioraudit

import (
	"encoding/json"
	"fmt"
	"log"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/macrolabel"
	"cmnet/internal/dota/minimap"
	"cmnet/internal/dota/worldmap"
)

type ReviewServer struct {
	mu         sync.Mutex
	cases      []AuditCase
	outputPath string
	addr       string
	startedAt  time.Time
	mapCache   string
	dotaPath   string // optional; status only
}

type ReviewServerOptions struct {
	MapCache string
	DotaPath string
}

func NewReviewServer(cases []AuditCase, outputPath, addr string) *ReviewServer {
	return NewReviewServerOpts(cases, outputPath, addr, ReviewServerOptions{})
}

func NewReviewServerOpts(cases []AuditCase, outputPath, addr string, opts ReviewServerOptions) *ReviewServer {
	if addr == "" {
		addr = "127.0.0.1:32124"
	}
	cache := opts.MapCache
	if cache == "" {
		cache = minimap.DefaultCacheRel
	}
	if abs, err := filepath.Abs(cache); err == nil {
		cache = abs
	}
	return &ReviewServer{
		cases:      cases,
		outputPath: outputPath,
		addr:       addr,
		startedAt:  time.Now().UTC(),
		mapCache:   cache,
		dotaPath:   opts.DotaPath,
	}
}

// ListenAndServe binds localhost only and serves the review UI.
func (s *ReviewServer) ListenAndServe() error {
	if err := ValidateCasesMacroSchema(s.cases); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(s.addr)
	if err != nil {
		return fmt.Errorf("invalid --serve addr: %w", err)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("behavior audit server must bind localhost only, got %q", host)
	}
	_ = port

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/session", s.handleSession)
	mux.HandleFunc("/api/cases", s.handleCases)
	mux.HandleFunc("/api/cases/", s.handleCase)
	mux.HandleFunc("/api/definitions", s.handleDefinitions)
	mux.HandleFunc("/api/actions", s.handleActions)
	mux.HandleFunc("/api/map", s.handleMap)
	mux.HandleFunc("/api/map/image", s.handleMapImage)
	mux.HandleFunc("/api/map/calibration", s.handleMapCalibration)

	sub, err := fs.Sub(staticContent, "static")
	if err != nil {
		return err
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return err
	}
	fmt.Printf("behavior audit UI: http://127.0.0.1:%s/\n", port)
	fmt.Printf("After review (Ctrl+C): advisor macro-label-review --report %s\n", s.outputPath)
	return http.Serve(ln, mux)
}

func (s *ReviewServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := staticContent.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *ReviewServer) mapStatus() map[string]any {
	meta, _, tf, err := minimap.LoadCache(s.mapCache)
	out := map[string]any{
		"available":        err == nil,
		"cache_dir":        s.mapCache,
		"geometry_version": worldmap.CurrentVersion,
		"dota_path":        s.dotaPath,
	}
	if err != nil {
		out["warning"] = "Current Dota minimap background unavailable — run: advisor dota-map-install"
		out["error"] = err.Error()
		return out
	}
	out["meta"] = meta
	out["transform"] = map[string]any{
		"pos_x":  tf.PosX,
		"pos_y":  tf.PosY,
		"scale":  tf.Scale,
		"width":  tf.ImageWidth,
		"height": tf.ImageHeight,
	}
	if meta.MapGeometryVersion != "" && meta.MapGeometryVersion != worldmap.CurrentVersion {
		out["warning"] = fmt.Sprintf("cache geometry %s differs from binary %s — consider re-install",
			meta.MapGeometryVersion, worldmap.CurrentVersion)
	}
	return out
}

func (s *ReviewServer) handleMap(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mapStatus())
}

func (s *ReviewServer) handleMapImage(w http.ResponseWriter, r *http.Request) {
	_, pngPath, _, err := minimap.LoadCache(s.mapCache)
	if err != nil {
		http.Error(w, "minimap unavailable", http.StatusNotFound)
		return
	}
	// Serve only the fixed cache file (no user path / traversal).
	clean := filepath.Clean(pngPath)
	expected := filepath.Clean(filepath.Join(s.mapCache, "minimap.png"))
	if !strings.EqualFold(clean, expected) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	f, err := os.Open(clean)
	if err != nil {
		http.Error(w, "minimap unavailable", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "minimap.png", time.Time{}, f)
}

func (s *ReviewServer) handleMapCalibration(w http.ResponseWriter, r *http.Request) {
	_, _, tf, err := minimap.LoadCache(s.mapCache)
	if err != nil {
		writeJSON(w, map[string]any{"available": false, "error": err.Error()})
		return
	}
	rep, err := minimap.Calibrate(tf)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Optional region polygons for debug overlay.
	geo, gerr := worldmap.LoadCurrent()
	regions := []map[string]any{}
	if gerr == nil {
		for _, id := range geo.RegionIDs() {
			poly, ok := geo.RegionPolygon(id)
			if !ok || len(poly) == 0 {
				continue
			}
			pts := make([]map[string]float64, 0, len(poly))
			for _, p := range poly {
				pix := tf.WorldToPixel(p)
				pts = append(pts, map[string]float64{"x": pix.X, "y": pix.Y, "wx": p.X, "wy": p.Y})
			}
			regions = append(regions, map[string]any{"id": string(id), "pixels": pts})
		}
	}
	writeJSON(w, map[string]any{
		"available": true,
		"report":    rep,
		"regions":   regions,
	})
}

func (s *ReviewServer) handleSession(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reviewed := 0
	for _, c := range s.cases {
		if c.Review != nil && c.Review.IsReviewed() {
			reviewed++
		}
	}
	ms := s.mapStatus()
	schemaOK := true
	schemaWant := action.SchemaVersion
	var schemaWarn string
	for _, c := range s.cases {
		if c.MacroActionSchemaVersion == "" {
			schemaWarn = "some cases missing macro_action_schema_version; assuming " + schemaWant
			continue
		}
		if c.MacroActionSchemaVersion != schemaWant {
			schemaOK = false
			schemaWarn = fmt.Sprintf("macro schema mismatch: dataset %q vs reviewer %q", c.MacroActionSchemaVersion, schemaWant)
			break
		}
	}
	writeJSON(w, map[string]any{
		"total":                         len(s.cases),
		"reviewed":                      reviewed,
		"pending":                       len(s.cases) - reviewed,
		"behavior_audit_schema_version": BehaviorAuditSchemaVersion,
		"behavior_reviewer_version":     BehaviorReviewerVersion,
		"macro_action_schema_version":   schemaWant,
		"macro_schema_ok":               schemaOK,
		"macro_schema_warning":          schemaWarn,
		"started_at":                    s.startedAt.Format(time.RFC3339),
		"output":                        s.outputPath,
		"badge":                         "Player-visible GSI only",
		"map":                           ms,
		"geometry_version":              worldmap.CurrentVersion,
	})
}

func (s *ReviewServer) handleCases(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type row struct {
		ID          string       `json:"id"`
		Status      string       `json:"auto_status"`
		Review      ReviewStatus `json:"review_status,omitempty"`
		Start       int          `json:"start_game_time"`
		End         int          `json:"end_game_time"`
		ActionLabel string       `json:"action_label,omitempty"`
	}
	out := make([]row, 0, len(s.cases))
	for _, c := range s.cases {
		rr := row{ID: c.ID, Status: string(c.AutoStatus), Start: c.StartGameTime, End: c.EndGameTime}
		if c.Review != nil {
			rr.Review = c.Review.Status
		}
		if c.AutoAction != nil {
			rr.ActionLabel = c.AutoAction.String()
		}
		out = append(out, rr)
	}
	writeJSON(w, out)
}

func (s *ReviewServer) handleCase(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/cases/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) >= 2 && parts[1] == "review" && r.Method == http.MethodPost {
		s.handleReviewPOST(w, r, id)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	reveal := r.URL.Query().Get("reveal") == "1"

	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.find(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	view := *c
	view.RevealAlts = false
	hideRejected := view.AutoStatus == macrolabel.LabelStatusLabeled
	if hideRejected {
		if reveal {
			view.RevealAlts = true
		} else if view.Review != nil && view.Review.Status == ReviewRejected {
			view.RevealAlts = true
		}
		if !view.RevealAlts {
			view.RejectedCandidates = nil
		}
	}
	writeJSON(w, caseAPIPayload(view))
}

func (s *ReviewServer) handleReviewPOST(w http.ResponseWriter, r *http.Request, id string) {
	var rev HumanReview
	if err := json.NewDecoder(r.Body).Decode(&rev); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if rev.ReviewedAt == "" {
		rev.ReviewedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := UpsertReview(s.cases, id, rev)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	s.cases = updated
	if err := SaveReviewedNDJSON(s.outputPath, CasesToReviewed(s.cases)); err != nil {
		http.Error(w, "persist: "+err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "review": rev})
}

func (s *ReviewServer) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, AllDefinitions())
}

func (s *ReviewServer) handleActions(w http.ResponseWriter, r *http.Request) {
	space := action.DefaultSpaceV0()
	type row struct {
		ID          int    `json:"id"`
		Kind        string `json:"kind"`
		Target      string `json:"target"`
		Label       string `json:"label"`
		ActionName  string `json:"action_name"`
		Definition  string `json:"definition"`
		Description string `json:"action_description"`
	}
	all := space.All()
	out := make([]row, 0, len(all))
	for _, a := range all {
		name := a.String()
		def := HumanDefinition(a.Kind, a.Target)
		out = append(out, row{
			ID: a.ID, Kind: string(a.Kind), Target: string(a.Target),
			Label: name, ActionName: name,
			Definition: def, Description: def,
		})
	}
	writeJSON(w, out)
}

// caseAPIPayload enriches AuditCase with human-readable MacroAction fields for the UI.
func caseAPIPayload(c AuditCase) map[string]any {
	space := action.DefaultSpaceV0()
	ok, want := CheckMacroSchema(c.MacroActionSchemaVersion)
	if c.MacroActionSchemaVersion == "" {
		ok = true
		log.Printf("behavioraudit: case %s missing macro_action_schema_version; using %s", c.ID, want)
	} else if !ok {
		log.Printf("behavioraudit: case %s macro schema %q != %q", c.ID, c.MacroActionSchemaVersion, want)
	}

	competing := EnrichCompeting(space, c.Competing)
	rejected := EnrichRejected(space, c.RejectedCandidates)

	payload := map[string]any{
		"id":                            c.ID,
		"match_id":                      c.MatchID,
		"side":                          c.Side,
		"start_game_time":               c.StartGameTime,
		"end_game_time":                 c.EndGameTime,
		"duration_seconds":              c.DurationSeconds,
		"auto_status":                   c.AutoStatus,
		"auto_action":                   c.AutoAction,
		"auto_confidence":               c.AutoConfidence,
		"ambiguity_reason":              c.AmbiguityReason,
		"competing":                     competing,
		"rejected_candidates":           rejected,
		"evidence_summary":              c.EvidenceSummary,
		"timeline_markers":              c.Timeline,
		"trajectories":                  c.Trajectories,
		"enemy_markers":                 c.EnemyMarkers,
		"grouping_summary":              c.Grouping,
		"lane_summary":                  c.Lane,
		"map_regions":                   c.MapRegions,
		"definition":                    c.Definition,
		"review":                        c.Review,
		"behavior_audit_schema_version": c.BehaviorAuditSchemaVersion,
		"behavior_reviewer_version":     c.BehaviorReviewerVersion,
		"labeler_version":               c.LabelerVersion,
		"macro_action_schema_version":   c.MacroActionSchemaVersion,
		"macro_schema_ok":               ok,
		"macro_schema_want":             want,
		"sample_seed":                   c.SampleSeed,
		"source_mode":                   c.SourceMode,
		"smoke_anchor_id":               c.SmokeAnchorID,
		"reveal_alts":                   c.RevealAlts,
	}
	if c.AutoAction != nil {
		v := ResolveActionView(space, c.AutoAction.ID)
		payload["auto_action_name"] = v.ActionName
		payload["auto_action_description"] = v.Description
		if c.Definition == "" && v.Description != "" {
			payload["definition"] = v.Description
		}
	}
	if !ok && c.MacroActionSchemaVersion != "" {
		payload["macro_schema_warning"] = fmt.Sprintf(
			"MacroAction schema mismatch: dataset=%s reviewer=%s", c.MacroActionSchemaVersion, want)
	}
	return payload
}

func (s *ReviewServer) find(id string) (*AuditCase, bool) {
	for i := range s.cases {
		if s.cases[i].ID == id {
			return &s.cases[i], true
		}
	}
	return nil, false
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// ProgressString returns "reviewed/total".
func ProgressString(cases []AuditCase) string {
	rev := 0
	for _, c := range cases {
		if c.Review != nil && c.Review.IsReviewed() {
			rev++
		}
	}
	return strconv.Itoa(rev) + "/" + strconv.Itoa(len(cases))
}
