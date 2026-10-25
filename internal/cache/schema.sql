-- Local match cache. Pure-Go SQLite (modernc.org/sqlite); no CGO.

CREATE TABLE IF NOT EXISTS heroes (
    id        INTEGER PRIMARY KEY,
    name      TEXT NOT NULL,
    localized TEXT NOT NULL DEFAULT ''
);

-- One row per cached match.
--
-- The provenance columns (game_mode / rank_tier / source) exist because without
-- them the training pipeline cannot tell a pro Captains Mode draft from a public
-- one, or an Archon pub from an Immortal game — and those are different
-- populations that must not be pooled blindly. "Has a ban row" was the old proxy
-- for Captains Mode; it is a guess, these are facts.
CREATE TABLE IF NOT EXISTS matches (
    match_id     INTEGER PRIMARY KEY,
    start_time   INTEGER NOT NULL,   -- unix seconds (match start)
    radiant_win  INTEGER NOT NULL,   -- 0/1
    avg_mmr      INTEGER,            -- average match MMR if known, else NULL
    patch        TEXT NOT NULL DEFAULT '',
    fetched_at   INTEGER NOT NULL,   -- unix seconds when we cached it
    game_mode    INTEGER NOT NULL DEFAULT 0,  -- Valve game_mode (2 = Captains Mode); 0 unknown
    rank_tier    INTEGER,            -- avg rank tier (40 Archon … 80 Immortal), NULL unknown
    source       TEXT NOT NULL DEFAULT ''     -- 'pro', 'public-cm', 'bulk', '' unknown
);

CREATE INDEX IF NOT EXISTS idx_matches_start_time ON matches(start_time);

-- Pick/ban entries for a match, in draft order.
CREATE TABLE IF NOT EXISTS pick_bans (
    match_id  INTEGER NOT NULL,
    ord       INTEGER NOT NULL,   -- order within the match draft
    hero_id   INTEGER NOT NULL,
    is_pick   INTEGER NOT NULL,   -- 0 ban / 1 pick
    team      INTEGER NOT NULL,   -- 0 radiant / 1 dire
    PRIMARY KEY (match_id, ord),
    FOREIGN KEY (match_id) REFERENCES matches(match_id) ON DELETE CASCADE
);

-- Participants of a match: who played which hero, on which side, in which lane
-- position.
--
-- Without this table the models can only ever reason about a bag of heroes. Roles
-- and players are the two things a human drafter actually reasons with ("their
-- carry is on this, their support pool is thin"), and neither is derivable from
-- pick_bans: the draft rows say which heroes went to which side, never who ends up
-- on them or where they line up.
--
-- Keyed on player_slot, Valve's own per-match slot (0-4 Radiant, 128-132 Dire),
-- because that is the participant's identity in the source data. hero_id is UNIQUE
-- per match as an integrity check rather than a key: Dota forbids duplicate heroes
-- in a match, so a violation means the parser mangled the response, and that is
-- worth failing on rather than storing.
--
-- account_id is NULL for anonymous profiles — never 0 or a sentinel, so that
-- "player unknown" cannot be mistaken for a real account by a JOIN.
CREATE TABLE IF NOT EXISTS match_players (
    match_id    INTEGER NOT NULL,
    player_slot INTEGER NOT NULL,   -- Valve slot: 0-4 Radiant, 128-132 Dire
    account_id  INTEGER,            -- NULL = anonymous / hidden profile
    hero_id     INTEGER NOT NULL,
    is_radiant  INTEGER NOT NULL,   -- 0/1
    position    INTEGER,            -- 1..5 lane position, NULL if unknown
    pos_source  TEXT NOT NULL DEFAULT '',  -- 'stratz' | '' (unknown)
    PRIMARY KEY (match_id, player_slot),
    UNIQUE (match_id, hero_id),
    FOREIGN KEY (match_id) REFERENCES matches(match_id) ON DELETE CASCADE
);

-- Lookups run "all rows for these matches" (training) and "all matches for this
-- account" (scouting); the primary key serves the first, this index the second.
CREATE INDEX IF NOT EXISTS idx_match_players_account ON match_players(account_id);

-- Key/value metadata: sync watermarks, etc.
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Scouted players' display names. Purely cosmetic, like heroes.localized: the
-- persona name never reaches the model, so it lives in the cache and not in the
-- snapshot — rewriting a shared snapshot to add a nickname would change its hash
-- and split the file teammates are supposed to blend on identically (§6/§9).
CREATE TABLE IF NOT EXISTS players (
    account_id INTEGER PRIMARY KEY,
    persona    TEXT NOT NULL DEFAULT '',
    fetched_at INTEGER NOT NULL          -- unix seconds; lets a stale name be refreshed
);
