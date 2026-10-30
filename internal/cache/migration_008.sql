-- Additive migration 8. No existing Electron table or column is dropped.
-- Old clients can continue reading their existing tables throughout migration.
CREATE TABLE IF NOT EXISTS source_observations (
    account_id INTEGER NOT NULL CHECK (account_id > 0),
    provider TEXT NOT NULL CHECK (length(provider) BETWEEN 1 AND 64),
    observed_at INTEGER NOT NULL CHECK (observed_at > 0),
    status TEXT NOT NULL CHECK (status IN (
        'fresh','stale','loading','partial','private',
        'rate_limited','unavailable','error'
    )),
    evidence_json TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (account_id, provider, observed_at)
);
CREATE INDEX IF NOT EXISTS idx_source_observations_latest
    ON source_observations (account_id, provider, observed_at DESC);

CREATE TABLE IF NOT EXISTS team_snapshots (
    snapshot_id TEXT PRIMARY KEY,
    frozen_at INTEGER NOT NULL,
    our_accounts_json TEXT NOT NULL,
    enemy_accounts_json TEXT NOT NULL,
    evidence_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_team_snapshots_frozen
    ON team_snapshots (frozen_at DESC);

CREATE TABLE IF NOT EXISTS model_registry (
    model_hash TEXT PRIMARY KEY,
    bundle_version TEXT NOT NULL,
    patch_tag TEXT NOT NULL,
    evaluated_at INTEGER,
    evaluation_json TEXT,
    accepted INTEGER NOT NULL DEFAULT 0 CHECK (accepted IN (0, 1))
);

CREATE TABLE IF NOT EXISTS draft_sessions (
    draft_id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    model_version TEXT NOT NULL,
    team_snapshot_id TEXT REFERENCES team_snapshots(snapshot_id) ON DELETE SET NULL,
    state_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_draft_sessions_recent
    ON draft_sessions (updated_at DESC);

CREATE TABLE IF NOT EXISTS decision_events (
    draft_id TEXT NOT NULL REFERENCES draft_sessions(draft_id) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK (revision >= 0),
    event_type TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    PRIMARY KEY (draft_id, revision)
);
