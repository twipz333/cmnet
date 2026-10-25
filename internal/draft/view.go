package draft

import "slices"

// View is the read-only draft information the recommender needs. Both the
// mutable *DraftState (manual input) and the GSI-fed *Snapshot satisfy it, so the
// core works identically regardless of the input source (§7/§8).
type View interface {
	// Picks returns the hero ids picked by team.
	Picks(team Team) []int
	// Bans returns the hero ids banned by team.
	Bans(team Team) []int
	// Current returns the step that must be filled next; ok is false when complete.
	Current() (Step, bool)
	// IsTaken reports whether a hero already appears as any pick or ban.
	IsTaken(heroID int) bool
	// FirstPickTeam is the side that drafts first. The CM schedule is not
	// symmetric between the two seats, so the next-action model needs to know
	// which seat the acting team occupies.
	FirstPickTeam() Team
}

// PicksPerTeam is how many heroes each side picks in Captains Mode.
const PicksPerTeam = 5

// FirstPickTeam implements View. GSI does not label the first-pick side, so a
// snapshot reports its FirstPick field, defaulting to Radiant.
func (s *Snapshot) FirstPickTeam() Team { return s.FirstPick }

// Snapshot is a point-in-time draft view built from a GSI event. Unlike
// DraftState it carries no step sequence: GSI reports the current acting team and
// pick/ban flag directly, plus the heroes chosen so far per team.
type Snapshot struct {
	RadiantPicks []int
	DirePicks    []int
	RadiantBans  []int
	DireBans     []int

	// CurrentTeam / CurrentIsPick describe the step in progress. Done overrides
	// them when the draft is finished.
	CurrentTeam   Team
	CurrentIsPick bool
	Done          bool

	// FirstPick is the side that drafted first. Radiant (the zero value) when the
	// source could not tell.
	FirstPick Team

	// HasDraft reports whether the source GSI event actually carried a draft block.
	// Map-only in-game events (post-draft / All Pick) leave it false so the server
	// can retain the last known picks instead of wiping them (M6).
	HasDraft bool

	// Live carries in-game state (clock, game phase) from GSI's map block. Nil until
	// a map-bearing event arrives. Present in both the draft and post-draft phases.
	Live *Live
}

// Live is the in-game state read from GSI's map block (M6). It drives the objective
// timer/alert panel and the post-draft strength readout.
// The json tags are what the browser front end reads; without them the fields
// serialise as ClockTime/GameState and the status bar's clock renders as NaN.
type Live struct {
	ClockTime int    `json:"clockTime"` // game clock in seconds; negative before the horn
	GameState string `json:"gameState"` // GSI map.game_state (e.g. DOTA_GAMERULES_STATE_GAME_IN_PROGRESS)
	Daytime   bool   `json:"daytime"`   // GSI map.daytime

	// The rest of what the client actually sends. Display only, always: none of
	// it may reach the scoring path. Two friends watching the same draft have
	// different live telemetry — one is playing, one may not be — and a score that
	// depended on it would differ between them, which is the guarantee §6 exists
	// to hold.
	MatchID      string `json:"matchId,omitempty"`
	RadiantScore int    `json:"radiantScore,omitempty"`
	DireScore    int    `json:"direScore,omitempty"`
	Paused       bool   `json:"paused,omitempty"`

	// Our own player's line, when the client sends one. Absent while spectating
	// and during the draft itself.
	Self *SelfState `json:"self,omitempty"`
}

// SelfState is the local player's own performance, straight from GSI.
//
// "Local player" is the whole scope of it: while playing, Dota reports only our
// own row. The opponents' numbers are not withheld by us, they are not sent.
type SelfState struct {
	HeroID   int    `json:"heroId,omitempty"`
	HeroName string `json:"heroName,omitempty"`
	Level    int    `json:"level,omitempty"`
	Alive    bool   `json:"alive"`
	// RespawnSeconds is meaningful only while dead.
	RespawnSeconds int `json:"respawnSeconds,omitempty"`
	BuybackCost    int `json:"buybackCost,omitempty"`
	BuybackCooldown int `json:"buybackCooldown,omitempty"`

	Kills    int `json:"kills"`
	Deaths   int `json:"deaths"`
	Assists  int `json:"assists"`
	LastHits int `json:"lastHits,omitempty"`
	Denies   int `json:"denies,omitempty"`
	GPM      int `json:"gpm,omitempty"`
	XPM      int `json:"xpm,omitempty"`
	NetWorth int `json:"netWorth,omitempty"`
	Gold     int `json:"gold,omitempty"`
}

// Picks implements View.
func (s *Snapshot) Picks(team Team) []int {
	if team == Radiant {
		return s.RadiantPicks
	}
	return s.DirePicks
}

// Bans implements View.
func (s *Snapshot) Bans(team Team) []int {
	if team == Radiant {
		return s.RadiantBans
	}
	return s.DireBans
}

// Current implements View.
func (s *Snapshot) Current() (Step, bool) {
	if s.Done {
		return Step{}, false
	}
	action := Ban
	if s.CurrentIsPick {
		action = Pick
	}
	return Step{Team: s.CurrentTeam, Action: action}, true
}

// IsTaken implements View.
func (s *Snapshot) IsTaken(heroID int) bool {
	if heroID == NoHero {
		return false
	}
	for _, group := range [][]int{s.RadiantPicks, s.DirePicks, s.RadiantBans, s.DireBans} {
		if slices.Contains(group, heroID) {
			return true
		}
	}
	return false
}

// compile-time checks
var (
	_ View = (*DraftState)(nil)
	_ View = (*Snapshot)(nil)
)
