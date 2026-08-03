package session

import (
	"context"

	"cmnet/internal/cache"
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

	// BuildScan loads the bundle (+snapshot) and returns the draft-screen config.
	BuildScan func() (Config, error)

	// Auto is the background sync/train worker, or nil when it is disabled. Front
	// ends use it for three things: showing what it is doing, telling it when the
	// draft screen is open (SetDraftActive), and standing it aside while a manual
	// sync or train runs (Yield).
	Auto *Auto
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

// StatsHero is a hero reference with optional per-player stats.
type StatsHero struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Games   int     `json:"games"`
	WinRate float64 `json:"winRate"`
}
