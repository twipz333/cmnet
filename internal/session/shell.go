package session

import (
	"context"

	"cmnet/internal/cache"
	"cmnet/internal/jobs"
)

// CorpusStats is the cache's own breakdown of what it holds, surfaced to the
// front ends for the Data screen. Aliased rather than re-declared: it is the
// storage layer's shape, and copying six structs to keep the import out would
// mean two definitions to keep in step for no gain (session already depends on
// cache transitively).
type CorpusStats = cache.CorpusStats

// ShellConfig wires a front end's home screen to the app's operations via
// callbacks, so neither the tui nor the web package depends on cache/train/scout.
// Any callback may be nil (its entry is then disabled).
//
// It lives here rather than in tui because both front ends drive the same
// operations: the browser UI must offer everything the terminal one does.
type ShellConfig struct {
	GSI   bool
	Token bool // whether a STRATZ token was provided (enables sync)

	// Long operations. Each returns a human summary or an error; they run off the
	// UI goroutine and emit progress lines via the progress callback.
	Sync        func(ctx context.Context, progress func(string)) (string, error)
	Train       func(ctx context.Context, progress func(string)) (string, error)
	Scout       func(ctx context.Context, players []string, progress func(string)) (string, error)
	ScoutAllies func(ctx context.Context, players []string, progress func(string)) (string, error)

	// Stats returns the current snapshot projection for the Statistics screen (#5).
	Stats func() (StatsView, error)
	// Corpus reports what the local match cache holds, for the Data screen: volume,
	// how much of it is the scarce kind (drafts with a ban phase), the rank mix the
	// training filter cuts into, and what has been arriving lately.
	Corpus func() (CorpusStats, error)
	// SetHeroPool parses hero tokens (ids or names) and saves the pool (#4),
	// returning a human summary.
	SetHeroPool func(tokens []string) (string, error)
	// RemovePlayer deletes a scouted player from the snapshot (#2).
	RemovePlayer func(accountID int64, ally bool) (string, error)
	// ClearLayer3 drops the enemy team's event drafts (layer 3) from the snapshot.
	// Needed because layer 3 hangs off a single entry-point enemy that may no longer
	// be in the layer-2 player list (so it isn't deletable via RemovePlayer).
	ClearLayer3 func() (string, error)
	// SetItemRules saves the advice-DSL text and returns a summary (#10).
	SetItemRules func(text string) (string, error)

	// CorpusMeta reports the per-position and per-lane aggregates baked into the
	// bundle from our own corpus — what heroes actually do at each position here,
	// as opposed to the STRATZ ranked all-pick meta shown beside it.
	CorpusMeta func() (CorpusMetaView, error)

	// KnownPlayers lists the accounts this machine has scouted or played with, so
	// picking a teammate is a click instead of pasting a profile URL again. Role is
	// "ally", "enemy" or "" for both. The list lives in the local cache and never in
	// the shared snapshot.
	KnownPlayers func(role string) ([]KnownPlayer, error)
	// ForgetPlayer drops an account from that list along with its cached
	// aggregates. Distinct from RemovePlayer, which only takes them out of the
	// current snapshot and deliberately leaves the cache warm.
	ForgetPlayer func(accountID int64) (string, error)
	// PinPlayer keeps an account out of the automatic pruning — the regular five
	// stay, the long tail of one-off opponents does not.
	PinPlayer func(accountID int64, pinned bool) (string, error)
	// AddPlayer files an account in the book without scouting it, so a stack can
	// be assembled once and reused. ref is an id or a profile URL.
	AddPlayer func(ref string, ally bool) (string, error)

	// BuildScan loads the bundle (+snapshot) and returns the draft-screen config.
	BuildScan func() (Config, error)

	// Auto is the background sync/train worker, or nil when it is disabled. Front
	// ends use it for two things: showing what it is doing, and telling it when the
	// draft screen is open (TouchDraft). Standing it aside is no longer its job —
	// the queue preempts a background pass when a manual request outranks it.
	Auto *Auto

	// AssetsDir is where cached hero portraits and avatars live, or "" when images
	// are unavailable. The web front end serves the directory read-only; the page
	// never reaches out to a CDN itself, so it keeps working with no network.
	AssetsDir string

	// Jobs is the work queue every long operation runs through, or nil in builds
	// and tests that wire the operations directly. Front ends read it to show what
	// is in flight and to page through a job's log; they never execute work
	// themselves.
	Jobs *jobs.Queue
}

// StatsView is a read-only projection of the current snapshot for the Statistics
// screen (#5). A front end asks the app to build it via ShellConfig.Stats.
type StatsView struct {
	EnemyPlayers []StatsPlayer `json:"enemyPlayers"`
	AllyPlayers  []StatsPlayer `json:"allyPlayers"`
	Layer3Drafts int           `json:"layer3Drafts"`
	HeroPool     []StatsHero   `json:"heroPool"`
	ItemRules    string        `json:"itemRules"` // raw advice-DSL text (#10)
	SnapshotPath string        `json:"snapshotPath"`
}

// StatsPlayer is one scouted player and their comfort heroes.
type StatsPlayer struct {
	AccountID int64       `json:"accountId"`
	Nickname  string      `json:"nickname"`
	Resolved  bool        `json:"resolved"`
	TopHeroes []StatsHero `json:"topHeroes"`
}

// CorpusMetaView is the bundle's view of our own corpus, for the Data screen.
type CorpusMetaView struct {
	// PatchTag and BundleVersion say which bundle these numbers came from: they
	// are baked at train time, so they describe that build and not the cache as it
	// stands now.
	PatchTag      string `json:"patchTag,omitempty"`
	BundleVersion string `json:"bundleVersion,omitempty"`

	Positions []CorpusPosRow  `json:"positions"`
	Lanes     []CorpusLaneRow `json:"lanes"`
	Bans      []CorpusBanRow  `json:"bans"`
}

// CorpusPosRow is one hero's record at one position.
type CorpusPosRow struct {
	Position int     `json:"position"`
	HeroID   int     `json:"heroId"`
	Name     string  `json:"name"`
	Matches  int     `json:"matches"`
	WinRate  float64 `json:"winRate"`
	PickRate float64 `json:"pickRate"`
}

// CorpusLaneRow is one lane pairing and how much it is worth over an average one.
type CorpusLaneRow struct {
	Lane    string   `json:"lane"`
	Heroes  []string `json:"heroes"`
	Matches int      `json:"matches"`
	WinRate float64  `json:"winRate"`
	Lift    float64  `json:"lift"`
}

// CorpusBanRow is one hero's ban record. No position: bans happen before lanes
// exist.
type CorpusBanRow struct {
	HeroID  int     `json:"heroId"`
	Name    string  `json:"name"`
	Bans    int     `json:"bans"`
	BanRate float64 `json:"banRate"`
}

// KnownPlayer is one remembered account as a front end sees it.
type KnownPlayer struct {
	AccountID int64  `json:"accountId"`
	Persona   string `json:"persona"`
	Role      string `json:"role"`
	Note      string `json:"note,omitempty"`
	LastSeen  int64  `json:"lastSeen"`
	Pinned    bool   `json:"pinned"`
	// Heroes is how many hero rows are cached for this account: what makes a
	// repeat scout cheap, and worth showing so "cached" is visible rather than
	// merely claimed.
	Heroes int `json:"heroes"`
	// InSnapshot is "ally", "enemy" or "" — whether this player is part of the
	// CURRENT lineup, as opposed to merely being known.
	//
	// The two are deliberately different things. The snapshot is who we are
	// drafting with or against today; the book is everyone we have ever played
	// with. Taking someone out of today's lineup must not make us forget who they
	// are, which is exactly the confusion the first version created.
	InSnapshot string `json:"inSnapshot,omitempty"`
}

// StatsHero is a hero reference with optional per-player stats.
type StatsHero struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Games   int     `json:"games"`
	WinRate float64 `json:"winRate"`
}
