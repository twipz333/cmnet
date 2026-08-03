package main

import (
	"strconv"

	"cmnet/internal/bundle"
	"cmnet/internal/cache"
	"cmnet/internal/heroagg"
	"cmnet/internal/session"
)

// topPerPosition is how many heroes each position's panel shows. A per-position
// list is a shortlist, not a census: past a dozen the tail is heroes with barely
// enough games to have passed the floor.
const topPerPosition = 12

// topLanes / topBans bound the other two lists for the same reason.
const (
	topLanes = 15
	topBans  = 15
)

// opCorpusMeta reads the aggregates baked into the bundle and names the heroes.
//
// From the bundle rather than recomputed from the cache on the spot: these are
// what the model was built alongside, so showing anything else would explain a
// bundle with numbers that do not belong to it. It also keeps the screen cheap —
// the aggregates are a full pass over the corpus, and this is a file read.
func opCorpusMeta(bundlePath, dbPath string) (session.CorpusMetaView, error) {
	b, err := bundle.Load(bundlePath)
	if err != nil {
		return session.CorpusMetaView{}, err
	}
	out := session.CorpusMetaView{
		PatchTag:      b.PatchTag,
		BundleVersion: b.BundleVersion,
	}

	names := heroNames(dbPath)
	name := func(id int) string {
		if n := names[id]; n != "" {
			return n
		}
		return "Hero " + strconv.Itoa(id)
	}

	// The rows arrive sorted by position then popularity, so the top of each
	// position is a prefix — no re-sorting here, which would risk diverging from
	// the order the bundle recorded.
	perPos := map[int]int{}
	for _, r := range b.CorpusPositions {
		if perPos[r.Position] >= topPerPosition {
			continue
		}
		perPos[r.Position]++
		out.Positions = append(out.Positions, session.CorpusPosRow{
			Position: r.Position, HeroID: r.HeroID, Name: name(r.HeroID),
			Matches: r.Matches, WinRate: r.WinRate, PickRate: r.PickRate,
		})
	}

	for i, r := range b.CorpusLanes {
		if i >= topLanes {
			break
		}
		row := session.CorpusLaneRow{
			Lane: laneLabel(heroagg.Lane(r.Lane)), Matches: r.Matches,
			WinRate: r.WinRate, Lift: r.Lift,
		}
		row.Heroes = append(row.Heroes, name(r.Heroes[0]))
		if r.Heroes[1] != 0 {
			row.Heroes = append(row.Heroes, name(r.Heroes[1]))
		}
		out.Lanes = append(out.Lanes, row)
	}

	for i, r := range b.CorpusBans {
		if i >= topBans {
			break
		}
		out.Bans = append(out.Bans, session.CorpusBanRow{
			HeroID: r.HeroID, Name: name(r.HeroID), Bans: r.Bans, BanRate: r.BanRate,
		})
	}
	return out, nil
}

// laneLabel names a lane for the UI.
func laneLabel(l heroagg.Lane) string {
	switch l {
	case heroagg.LaneSafe:
		return "лёгкая (1+5)"
	case heroagg.LaneMid:
		return "мид (2)"
	case heroagg.LaneOff:
		return "сложная (3+4)"
	}
	return string(l)
}

// heroNames reads the localized hero names, or an empty map when the cache cannot
// be opened — the panel then shows ids, which is worse but not broken.
func heroNames(dbPath string) map[int]string {
	store, err := cache.Open(dbPath)
	if err != nil {
		return nil
	}
	defer store.Close()
	heroes, err := store.Heroes()
	if err != nil {
		return nil
	}
	out := make(map[int]string, len(heroes))
	for _, h := range heroes {
		out[h.ID] = h.Localized
	}
	return out
}
