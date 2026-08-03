package scout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"cmnet/internal/atomicfile"
)

// HeroIndexer maps a Valve hero id to a dense index (e.g. *features.HeroMap).
type HeroIndexer interface {
	Len() int
	Index(id int) (int, bool)
}

// HeroAffinity is one player's comfort with a hero (layer 2). Aff is a normalized
// affinity weight in [0,1] used by the blend.
type HeroAffinity struct {
	HeroID  int     `json:"hero_id"`
	Games   int     `json:"games"`
	WinRate float64 `json:"win_rate"`
	Aff     float64 `json:"aff"`
}

// PlayerAffinity is layer 2 for a single opponent player.
type PlayerAffinity struct {
	AccountID int64          `json:"account_id"`
	Nickname  string         `json:"nickname"`
	Resolved  bool           `json:"resolved"`
	Heroes    []HeroAffinity `json:"heroes"`
}

// TeamDraft is one observed opposing-team draft in the current event (layer 3).
type TeamDraft struct {
	MatchID int64 `json:"match_id"`
	Radiant bool  `json:"radiant"` // which side the opponent team was on
	Picks   []int `json:"picks"`
	Bans    []int `json:"bans"`
}

// Layer2 holds player affinity (window ~month).
type Layer2 struct {
	WindowDays int              `json:"window_days"`
	Players    []PlayerAffinity `json:"players"`
}

// Layer3 holds the opposing team's drafts in the current event (window "today").
type Layer3 struct {
	EventWindow    string      `json:"event_window"`
	DraftsObserved int         `json:"drafts_observed"`
	TeamDrafts     []TeamDraft `json:"team_drafts"`
	EntryAccountID int64       `json:"entry_account_id"` // the resolved id used as entry point
}

// OpponentSnapshot is the frozen, shareable layers 2-3 artifact for one match. It
// is versioned and logically separate from the model bundle (bundle = static
// layer 1; snapshot = live layers 2-3). The blend is a pure function of
// (bundle, snapshot, draft state) — §9.
type OpponentSnapshot struct {
	SnapshotVersion  string `json:"snapshot_version"` // sha256 of content with this field blank
	FrozenAt         string `json:"frozen_at"`        // RFC3339, fixed at freeze time
	BundleVersionRef string `json:"bundle_version_ref"`
	SourceTag        string `json:"source_tag"`

	Layer2 *Layer2 `json:"layer2,omitempty"`
	Layer3 *Layer3 `json:"layer3,omitempty"`

	// Allies holds our own team's per-player hero affinity (same shape as the
	// enemy layer 2). It personalizes OUR pick recommendations toward heroes our
	// players are comfortable on (#3). Optional.
	Allies *Layer2 `json:"allies,omitempty"`
	// HeroPool is our preferred draft pool (Valve hero ids). Recommendations
	// surface these, especially when the enemy is likely to contest them (#4).
	HeroPool []int `json:"hero_pool,omitempty"`
	// ItemRules are raw advice-DSL lines (#10), edited in the Statistics screen and
	// evaluated against the enemy draft to surface counter-item / play advice.
	ItemRules []string `json:"item_rules,omitempty"`
}

// Aff2Vector builds the layer-2 affinity vector over the hero map: for each hero,
// the sum of per-player affinity (players are anonymous w.r.t. role, so we
// aggregate across the squad). Returns nil when layer 2 is absent/empty.
func (s *OpponentSnapshot) Aff2Vector(hm HeroIndexer) []float64 {
	if s == nil {
		return nil
	}
	return affVectorFromLayer2(s.Layer2, hm)
}

// AllyAffVector is the same aggregation over our own team (Allies), used to bias
// our pick recommendations toward ally-comfort heroes (#3). Nil when absent.
func (s *OpponentSnapshot) AllyAffVector(hm HeroIndexer) []float64 {
	if s == nil {
		return nil
	}
	return affVectorFromLayer2(s.Allies, hm)
}

// affVectorFromLayer2 sums per-player affinity over the hero map. Returns nil when
// the layer is absent/empty.
func affVectorFromLayer2(l2 *Layer2, hm HeroIndexer) []float64 {
	if l2 == nil || len(l2.Players) == 0 {
		return nil
	}
	vec := make([]float64, hm.Len())
	any := false
	for _, p := range l2.Players {
		for _, h := range p.Heroes {
			if idx, ok := hm.Index(h.HeroID); ok {
				vec[idx] += h.Aff
				any = true
			}
		}
	}
	if !any {
		return nil
	}
	return vec
}

// Aff3Vector builds the layer-3 vector (hero pick counts across the team's event
// drafts) and returns the number of observed drafts (n for w3_eff). Returns
// (nil, 0) when layer 3 is absent/empty.
func (s *OpponentSnapshot) Aff3Vector(hm HeroIndexer) ([]float64, int) {
	if s == nil || s.Layer3 == nil || len(s.Layer3.TeamDrafts) == 0 {
		return nil, 0
	}
	vec := make([]float64, hm.Len())
	for _, d := range s.Layer3.TeamDrafts {
		for _, hid := range d.Picks {
			if idx, ok := hm.Index(hid); ok {
				vec[idx]++
			}
		}
	}
	n := s.Layer3.DraftsObserved
	if n == 0 {
		n = len(s.Layer3.TeamDrafts)
	}
	return vec, n
}

// computeVersion hashes the snapshot content with the version field blanked.
func computeVersion(s *OpponentSnapshot) (string, error) {
	clone := *s
	clone.SnapshotVersion = ""
	data, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Save finalizes the version hash and writes the snapshot as JSON.
//
// The write is atomic (see atomicfile): an interrupted save leaves the previous
// snapshot untouched instead of a half-written file. That matters more here than
// for most artifacts: the snapshot is hash-verified on load, so a truncated write
// would not merely lose the newest scout — it would make the whole file unreadable.
func (s *OpponentSnapshot) Save(path string) error {
	v, err := computeVersion(s)
	if err != nil {
		return err
	}
	s.SnapshotVersion = v
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	// The version being replaced is kept as <path>.bak: scouting is done minutes
	// before a match with no undo and no version control, so one bad overwrite would
	// otherwise mean re-scouting the whole enemy team under time pressure.
	return atomicfile.WriteWithBackup(path, data, 0o644)
}

// LoadSnapshot reads and verifies a frozen snapshot.
func LoadSnapshot(path string) (*OpponentSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s OpponentSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	want, err := computeVersion(&s)
	if err != nil {
		return nil, err
	}
	if s.SnapshotVersion != want {
		return nil, fmt.Errorf("snapshot hash mismatch: file %q, content %q", s.SnapshotVersion, want)
	}
	return &s, nil
}
