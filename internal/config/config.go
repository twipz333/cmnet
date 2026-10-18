// Package config loads an optional YAML file whose keys are FLAG NAMES, and
// applies it as the default value of every flag the user did not pass explicitly.
//
// The design constraint that shapes everything here: a config value must never
// silently win over something typed on the command line. So the file is not read
// into a struct that the commands then consult — it is pushed into cobra's own
// flag set, and only into flags whose Changed bit is false. Precedence therefore
// falls out of one rule instead of a per-flag convention:
//
//	explicit flag  >  config file  >  environment default  >  flag default
//
// Keys are flag names verbatim (`window-days`, not `WindowDays`), because the
// help output is then also the config reference, and a rename cannot leave the
// two silently disagreeing.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// DefaultName is the file looked for when --config is not given.
const DefaultName = "config.yaml"

// File is a parsed config: top-level settings that apply to any command that has
// such a flag, plus per-command sections that override them.
type File struct {
	Path     string
	Global   map[string]any
	Commands map[string]map[string]any
}

// Load reads and parses a config file. A missing file is not an error — it
// returns nil, so callers can treat "no config" and "empty config" alike.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	f := &File{
		Path:     path,
		Global:   map[string]any{},
		Commands: map[string]map[string]any{},
	}
	for k, v := range raw {
		if section, ok := v.(map[string]any); ok {
			f.Commands[k] = section
			continue
		}
		f.Global[k] = v
	}
	return f, nil
}

// Find resolves which config path to use: the explicit one when given, otherwise
// DefaultName in the working directory, otherwise next to the executable. The
// executable fallback matters because the tool is normally launched by
// double-clicking or from a shortcut, where the working directory is not the
// folder the binary lives in.
func Find(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if _, err := os.Stat(DefaultName); err == nil {
		return DefaultName
	}
	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), DefaultName)
		if _, err := os.Stat(beside); err == nil {
			return beside
		}
	}
	return DefaultName // may not exist; Load treats that as "no config"
}

// Apply pushes config values into cmd's flags, skipping any flag the user set on
// the command line.
//
// Unknown keys are handled differently depending on where they appear, because
// the two cases mean different things:
//
//   - Inside a command's section the user is addressing that command, so a key it
//     has no flag for is a mistake and is reported. A typo would otherwise be
//     invisible: the file is edited, nothing changes, and nothing says why.
//   - At the top level the key is meant for whichever commands have such a flag —
//     `token` is useless to `eval` and that is not an error. It is only reported
//     when NO command in the tree has a flag by that name, which still catches
//     typos without punishing sharing.
func (f *File) Apply(cmd *cobra.Command) error {
	if f == nil {
		return nil
	}

	// Section values win over the global block for the running command.
	section := f.Commands[cmd.Name()]
	if err := f.applyKeys(cmd, section, true); err != nil {
		return err
	}
	global := map[string]any{}
	for k, v := range f.Global {
		if _, overridden := section[k]; !overridden {
			global[k] = v
		}
	}
	return f.applyKeys(cmd, global, false)
}

// applyKeys sets one group of values. strict marks the command-section case,
// where a key with no matching flag is an error.
func (f *File) applyKeys(cmd *cobra.Command, values map[string]any, strict bool) error {
	// Deterministic order so an error message does not depend on map iteration.
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var unknown []string
	for _, key := range keys {
		flag := lookup(cmd, key)
		if flag == nil {
			if strict || !knownAnywhere(cmd.Root(), key) {
				unknown = append(unknown, key)
			}
			continue
		}
		if flag.Changed {
			continue // typed on the command line: leave it alone
		}
		s, err := toFlagValue(values[key])
		if err != nil {
			return fmt.Errorf("%s: %s: %w", f.Path, key, err)
		}
		if err := flag.Value.Set(s); err != nil {
			return fmt.Errorf("%s: %s: %w", f.Path, key, err)
		}
	}
	if len(unknown) > 0 {
		where := "at the top level"
		if strict {
			where = fmt.Sprintf("in the %q section", cmd.Name())
		}
		return fmt.Errorf("%s: unknown setting(s) %s: %s (config keys are flag names; see --help)",
			f.Path, where, strings.Join(unknown, ", "))
	}
	return nil
}

// knownAnywhere reports whether any command in the tree defines a flag by this
// name, so a shared top-level key is not mistaken for a typo.
func knownAnywhere(root *cobra.Command, name string) bool {
	if root == nil {
		return false
	}
	if lookup(root, name) != nil {
		return true
	}
	for _, sub := range root.Commands() {
		if lookup(sub, name) != nil {
			return true
		}
	}
	return false
}

// lookup finds a flag by name on the command or among its inherited flags.
func lookup(cmd *cobra.Command, name string) *pflag.Flag {
	if fl := cmd.Flags().Lookup(name); fl != nil {
		return fl
	}
	return cmd.InheritedFlags().Lookup(name)
}

// toFlagValue renders a YAML scalar (or list) the way pflag parses it.
func toFlagValue(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", nil
	case string:
		return t, nil
	case bool:
		return strconv.FormatBool(t), nil
	case int:
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case []any:
		// pflag's slice flags take a comma-separated list.
		parts := make([]string, 0, len(t))
		for _, item := range t {
			s, err := toFlagValue(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), nil
	default:
		return "", fmt.Errorf("unsupported value %v (%T)", v, v)
	}
}

// Example is the starter config written by `advisor config-init`.
const Example = `# advisor configuration.
#
# Every key is a FLAG NAME — whatever you would type after "--". Anything you do
# pass on the command line still wins over this file.
#
# Top-level keys apply to any command that has that flag; a section applies to
# one command only.

# STRATZ API token. Needed for per-position meta and, importantly, for
# --public-cm: without it the per-match detail comes from OpenDota, which times
# out on most requests and wastes ~90% of the run.
token: ""

db: cmnet.db
bundle: bundle.json
snapshot: snapshot.json

# Rank bracket used for data collection (40=Archon, 50=Legend, 60=Ancient,
# 70=Divine, 80=Immortal).
min-rank-tier: 40
max-rank-tier: 85

sync:
  window-days: 31
  max-matches: 2500
  cm-matches: 500
  # Public Captains Mode drafts — the population this tool is used in.
  # One request per match, so this is the slow part; STRATZ allows ~10k/day.
  public-cm: 0

train:
  # 90 days, not the 31 of the original spec: the enemy predictor only learns from
  # Captains Mode drafts, and there are ~490 of those in a 31-day window versus
  # ~3000 in 90 days. Recency is handled by half-life below, not by throwing the
  # older drafts away entirely.
  window-days: 90

  # Recency half-life, measured over three rolling windows
  # ('advisor eval --sweep-half-life 7,14,30,-1 --folds 3'). The predictor cannot
  # tell 7d, 14d and 30d apart (the winner changes with the window), but win-prob on
  # CM never gets worse as this GROWS across 7/14/30 — 7d was the worst of the three
  # in all three windows (+0.018/+0.024/+0.004 going to 30d). Dropping the weighting
  # entirely scores even higher on CM in two windows, but is the worst setting for
  # the predictor everywhere, so keep some. Point estimates only, no per-point CIs:
  # treat neighbouring values as a tie.
  half-life: 30

  # Weight of pro Captains Mode drafts relative to public games. 1 = no
  # down-weighting. Treat this as an UNMEASURED default: the comparison that used
  # to justify it ran while the weights never reached the predictor, so it proved
  # nothing about that head. Now that 'sync --public-cm' has collected public CM
  # drafts to weigh pro games against, ~0.25 is worth re-measuring.
  pro-weight: 1

  # Latent rank of the factorization machine; 0 skips it. Untested on this data.
  fm-rank: 0

eval:
  bootstrap: 300
  folds: 1

serve:
  side: radiant
  first-pick: radiant
  # Read the live draft from Dota's Game State Integration instead of typing it
  # in. Run 'advisor gsi-install' once (or start the TUI home, which installs it
  # automatically) and RESTART Dota, otherwise the game never posts anything.
  gsi: false
  gsi-addr: 127.0.0.1:54001

# Settings for the TUI home — what you get by running 'advisor' with no
# subcommand. The section is named after the command itself.
advisor:
  gsi: false
  gsi-addr: 127.0.0.1:54001
  side: radiant
  first-pick: radiant

  # Seconds before a spawn to ring / flag Due. Per-kind: 0 = use alert-lead, -1 = disable.
  alert-lead: 15
  # alert-lead-daynight: -1
  # alert-lead-bounty: 10
  # alert-lead-power: 20
  # alert-lead-wisdom: 30
  # alert-lead-lotus: 15
  # alert-lead-tormentor: 45
  # alert-lead-daynight: 10

  # Active game-clock windows for alerts (seconds). -1 from = no lower limit;
  # 0 until = no upper limit. Example: wisdom only until 35:00.
  # alert-from-wisdom: 0
  # alert-until-wisdom: 2100

  # Shared secret for Discord remote-control (/api/remote/*).
  # remote-token: "change-me"

  # Start advisor-discord.exe as a sibling process with the web UI.
  # discord: true

  # Background collection. While the app is open it keeps syncing, and retrains
  # when enough new data has arrived — the corpus takes days to fill and a few
  # hundred drafts a day to stay current, which is not something to run by hand.
  #
  # Passes are small on purpose: the pass boundary is where the watermarks are
  # written, so an interrupted pass costs only what it had not stored yet, and the
  # next run resumes from there. Killing the app at any moment is safe.
  auto: true
  # Per background pass. public-cm needs a token (without one the per-match detail
  # comes from OpenDota and times out for ~9 requests in 10, so it is skipped).
  #
  # Weighted towards public CM on purpose: it is the only stream that matches what
  # the advisor is used for (Captains Mode at pub rank) and the scarce one. Bulk is
  # all-pick with no bans and no draft order — it feeds final compositions and the
  # synergy/counter matrices, which stop being empty around 45k matches, a mark the
  # cache has already passed. Pro CM is real Captains Mode but a different
  # population: coordinated teams where execution, not the draft, decides games.
  auto-public-cm: 400
  auto-max-matches: 100
  auto-cm-matches: 50
  # A retrain needs BOTH: this much time since the last one AND this many new
  # matches. Time alone would retrain nightly on fifty new games; volume alone
  # would retrain hourly during the first days of collection.
  auto-train-every: 24h
  auto-train-min-new: 500
  # Pause between passes. It doubles up to auto-max-idle while passes come back
  # empty (the window is collected and only new games trickle in), and drops back
  # to the floor as soon as one collects something.
  auto-idle: 1m
  auto-max-idle: 30m
`
