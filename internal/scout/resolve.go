// Package scout resolves opponent identities and (in M4) freezes opponent
// snapshots for the reweighting layers. For now it provides manual account_id
// resolution so a user who has found a player's profile by hand (e.g. on
// OpenDota) can paste the link or id directly.
package scout

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// steamID64Base is the offset between a 64-bit SteamID and a Dota account_id
// (a.k.a. SteamID32): account_id = steamID64 - steamID64Base.
const steamID64Base = 76561197960265728

var (
	// trailing numeric path segment, e.g. /players/123456789 or /players/123456789/matches
	pathID = regexp.MustCompile(`(\d{2,})`)
	// SteamID3 form: [U:1:123456789] or U:1:123456789
	steamID3 = regexp.MustCompile(`^\[?U:1:(\d+)\]?$`)
)

// ParseAccountID extracts a Dota account_id (SteamID32) from a raw id, a SteamID3,
// a SteamID64, or a profile URL (OpenDota, STRATZ, Dotabuff, or a Steam community
// profile). Parsing a URL string is not scraping — no request is made.
func ParseAccountID(input string) (int64, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return 0, fmt.Errorf("empty player reference")
	}

	// SteamID3: [U:1:N]
	if m := steamID3.FindStringSubmatch(s); m != nil {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return id, nil
	}

	// Bare number: account_id, or a SteamID64 we convert.
	if id, err := strconv.ParseInt(s, 10, 64); err == nil {
		return normalizeNumeric(id)
	}

	// Otherwise treat it as a URL and pull the last numeric path segment.
	u, err := url.Parse(s)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q as id or URL: %w", input, err)
	}
	host := strings.ToLower(u.Host)
	num, ok := lastNumericSegment(u.Path)
	if !ok {
		return 0, fmt.Errorf("no numeric id found in %q", input)
	}
	// Steam community profile paths carry a SteamID64.
	if strings.Contains(host, "steamcommunity.com") && strings.Contains(u.Path, "/profiles/") {
		return normalizeNumeric(num)
	}
	// OpenDota / STRATZ / Dotabuff player URLs carry the account_id directly.
	return normalizeNumeric(num)
}

// normalizeNumeric converts a SteamID64 to account_id; smaller values are assumed
// to already be account_ids.
func normalizeNumeric(n int64) (int64, error) {
	if n <= 0 {
		return 0, fmt.Errorf("invalid id %d", n)
	}
	if n >= steamID64Base {
		return n - steamID64Base, nil
	}
	return n, nil
}

// lastNumericSegment returns the last numeric path segment (handles trailing
// segments like /matches).
func lastNumericSegment(path string) (int64, bool) {
	segs := strings.Split(path, "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if m := pathID.FindString(segs[i]); m != "" && m == segs[i] {
			if n, err := strconv.ParseInt(m, 10, 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// ParsePlayers resolves a list of player references, returning the resolved
// account_ids and a slice of per-input errors (nil entries when resolved).
func ParsePlayers(inputs []string) (ids []int64, errs []error) {
	for _, in := range inputs {
		id, err := ParseAccountID(in)
		ids = append(ids, id)
		errs = append(errs, err)
	}
	return ids, errs
}
