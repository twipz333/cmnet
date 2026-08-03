// Package rules is a tiny, flexible advice DSL (#10). Each line maps a condition
// over the enemy draft to a piece of advice (e.g. counter-item suggestions):
//
//	axe, lifestealer | ursa => buy Eul, Lotus
//	# comment, blank lines ignored
//	else => standard itemization
//
// Condition grammar: comma = AND within a group, "|" = OR between groups. So
// "a, b | c" means (a AND b) OR c. The left side "else" or "*" is the fallback,
// shown only when no other rule matched. Hero tokens are resolved to ids via a
// caller-supplied name lookup; unknown tokens are dropped (and the group with
// them can never match, which is reported at parse time).
package rules

import (
	"sort"
	"strings"
)

// Rule is one parsed advice rule.
type Rule struct {
	Groups  [][]int // OR of AND-groups of hero ids; empty for else rules
	Advice  string
	IsElse  bool
	Unknown []string // hero tokens that didn't resolve (for editor feedback)
}

// Parse turns raw editor lines into rules. nameToID resolves a hero token (name or
// numeric id, case-insensitive) to a dense/Valve hero id; it returns ok=false for
// unknown tokens. The mapping is the caller's (it owns the hero name table).
func Parse(lines []string, nameToID func(token string) (int, bool)) []Rule {
	var out []Rule
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cond, advice, ok := splitArrow(line)
		if !ok {
			continue
		}
		advice = strings.TrimSpace(advice)
		if advice == "" {
			continue
		}
		lc := strings.ToLower(strings.TrimSpace(cond))
		if lc == "else" || lc == "*" || lc == "" {
			out = append(out, Rule{Advice: advice, IsElse: true})
			continue
		}
		r := Rule{Advice: advice}
		for _, grp := range strings.Split(cond, "|") {
			var ids []int
			for _, tok := range strings.Split(grp, ",") {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				if id, ok := nameToID(tok); ok {
					ids = append(ids, id)
				} else {
					r.Unknown = append(r.Unknown, tok)
				}
			}
			if len(ids) > 0 {
				r.Groups = append(r.Groups, ids)
			}
		}
		if len(r.Groups) > 0 || len(r.Unknown) > 0 {
			out = append(out, r)
		}
	}
	return out
}

func splitArrow(line string) (cond, advice string, ok bool) {
	i := strings.Index(line, "=>")
	if i < 0 {
		return "", "", false
	}
	return line[:i], line[i+2:], true
}

// Eval returns the advice for the given enemy hero ids: every non-else rule with a
// fully-present AND-group fires; if none fire, the else rules' advice is returned.
// Output order is rule order; deterministic.
func Eval(rs []Rule, enemyIDs []int) []string {
	present := make(map[int]bool, len(enemyIDs))
	for _, id := range enemyIDs {
		present[id] = true
	}
	var fired []string
	matched := false
	for _, r := range rs {
		if r.IsElse {
			continue
		}
		if groupMatches(r.Groups, present) {
			fired = append(fired, r.Advice)
			matched = true
		}
	}
	if !matched {
		for _, r := range rs {
			if r.IsElse {
				fired = append(fired, r.Advice)
			}
		}
	}
	return fired
}

// groupMatches reports whether any AND-group is fully present in the enemy set.
func groupMatches(groups [][]int, present map[int]bool) bool {
	for _, g := range groups {
		all := true
		for _, id := range g {
			if !present[id] {
				all = false
				break
			}
		}
		if all && len(g) > 0 {
			return true
		}
	}
	return false
}

// UnknownTokens collects all unresolved tokens across rules, sorted+deduped, for
// editor feedback.
func UnknownTokens(rs []Rule) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rs {
		for _, u := range r.Unknown {
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	sort.Strings(out)
	return out
}
