package macrolabel

import (
	"sort"

	"cmnet/internal/dota/action"
	"cmnet/internal/dota/worldmap"
)

func detectCandidatesV0(cfg Config, space ActionSpaceProvider, frames []BehaviorFrame, at int, smokeTimes []int) []Candidate {
	if at < 0 || at >= len(frames) {
		return nil
	}
	ctx := splitTemporal(cfg, frames, at)
	anchor := ctx.Current
	pastCurrent := append(append([]BehaviorFrame(nil), ctx.Past...), ctx.Current)
	currentFuture := append([]BehaviorFrame{ctx.Current}, ctx.Future...)
	start := temporalStart(ctx)
	end := temporalEnd(ctx)

	hasSmoke := smokeActiveAt(ctx.AnchorTime, smokeTimes, cfg.SmokeFollowSeconds)
	towerKillAfter := eventInFuture(ctx, "TOWER_KILL")
	barracksKillAfter := eventInFuture(ctx, "BARRACKS_KILL")
	heroKillAfter := eventInFuture(ctx, "HERO_KILL")

	roshanDead := false
	roshanVisible := false
	maxRoshanAllies := 0
	for _, f := range pastCurrent {
		if f.Objectives.RoshanKnownDeadAt != nil {
			roshanDead = true
		}
		if f.Objectives.RoshanVisible {
			roshanVisible = true
		}
		if f.Objectives.RoshanAlliesNear > maxRoshanAllies {
			maxRoshanAllies = f.Objectives.RoshanAlliesNear
		}
	}

	siegeFrame := ctx.Current
	if len(pastCurrent) > 0 {
		siegeFrame = pastCurrent[len(pastCurrent)-1]
	}

	groupDelta := end.Team.GroupedRatio - start.Team.GroupedRatio
	spreadDelta := end.Team.Spread - start.Team.Spread

	laneOrder := []worldmap.LaneID{worldmap.LaneTop, worldmap.LaneMid, worldmap.LaneBot}
	type laneDelta struct {
		lane  worldmap.LaneID
		delta float64
	}
	var laneDeltas []laneDelta
	anyPushEvidence := false
	for _, ln := range laneOrder {
		d0, ok0 := laneOurFrontProgress(start, ln)
		d1, ok1 := laneOurFrontProgress(end, ln)
		if !ok0 || !ok1 {
			continue
		}
		d := d1 - d0
		if d >= cfg.MinLaneFrontDeltaPush {
			anyPushEvidence = true
		}
		laneDeltas = append(laneDeltas, laneDelta{lane: ln, delta: d})
	}

	cands := make([]Candidate, 0, 12)

	if roshanDead {
		if a, err := space.New(action.KindTakeObjective, action.TargetRoshan); err == nil {
			cands = append(cands, Candidate{Action: a, EvidenceScore: 0.95, Confidence: 0.92, Evidence: tagEvidence([]Evidence{{Type: "roshan_dead"}}, TemporalRolePast)})
		}
	}
	if roshanVisible {
		cur := ctx.Current
		if cur.Objectives.RoshanAlliesNear >= 3 && cur.Team.GroupedRatio >= 0.45 && maxRoshanAllies >= 3 {
			if a, err := space.New(action.KindControlObjective, action.TargetRoshan); err == nil {
				s := clamp01(0.35 + 0.2*float64(cur.Objectives.RoshanAlliesNear)/5.0 + 0.45*cur.Team.GroupedRatio)
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "roshan_control"}}, TemporalRoleCurrent)})
			}
		}
	}

	if hasSmoke {
		cands = append(cands, smokeCandidatesV02(cfg, space, anchor.Side, ctx)...)
	}

	if siegeFrame.Objectives.EnemyBaseAlliesNear >= 3 && siegeFrame.Team.GroupedRatio >= 0.45 {
		deep := false
		for _, ln := range siegeFrame.Lanes {
			if ln.OurFrontAvailable && (ln.OurFrontDepth == "ENEMY_SIDE" || ln.OurFrontDepth == "ENEMY_BASE") {
				deep = true
				break
			}
		}
		if deep {
			if a, err := space.New(action.KindSiege, action.TargetEnemyBase); err == nil {
				s := clamp01(0.45 + 0.25*float64(siegeFrame.Objectives.EnemyBaseAlliesNear)/5.0 + 0.3*siegeFrame.Team.GroupedRatio)
				evRole := TemporalRoleCurrent
				if towerKillAfter || barracksKillAfter {
					s = clamp01(s + 0.08)
				}
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "enemy_base_pressure"}}, evRole)})
			}
		}
	}

	for _, d := range laneDeltas {
		if d.delta < cfg.MinLaneFrontDeltaPush {
			continue
		}
		if heroCountInLane(end, d.lane) < 3 {
			continue
		}
		var tgt action.ActionTarget
		switch d.lane {
		case worldmap.LaneTop:
			tgt = action.TargetTop
		case worldmap.LaneMid:
			tgt = action.TargetMid
		case worldmap.LaneBot:
			tgt = action.TargetBot
		default:
			continue
		}
		if a, err := space.New(action.KindPush, tgt); err == nil {
			s := clamp01(d.delta / 0.35)
			if towerKillAfter || barracksKillAfter {
				s = clamp01(s + 0.06)
			}
			ev := tagEvidence([]Evidence{{Type: "lane_front_delta", Value: ptrFloat(d.delta)}}, TemporalRolePast)
			if towerKillAfter {
				ev = mergeEvidence(ev, tagEvidence([]Evidence{{Type: "tower_kill_future"}}, TemporalRoleFuture))
			}
			cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: clamp01(0.45 + 0.55*s), Evidence: ev})
		}
	}

	startCounts := heroAreaCounts(start)
	endCounts := heroAreaCounts(end)

	if end.Team.GroupedRatio >= 0.3 && end.Team.Spread <= start.Team.Spread {
		if endCounts.EnemyJungle >= cfg.MinHeroesInAreaForFarm && (endCounts.EnemyJungle-startCounts.EnemyJungle) >= 1 {
			if a, err := space.New(action.KindInvade, action.TargetEnemyJungle); err == nil {
				s := clamp01(0.45 + 0.35*float64(endCounts.EnemyJungle)/5.0)
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "enemy_entry_transition"}}, TemporalRoleCurrent)})
			}
		}
		if endCounts.EnemyTriangle >= cfg.MinHeroesInAreaForFarm && (endCounts.EnemyTriangle-startCounts.EnemyTriangle) >= 1 {
			if a, err := space.New(action.KindInvade, action.TargetEnemyTriangle); err == nil {
				s := clamp01(0.45 + 0.35*float64(endCounts.EnemyTriangle)/5.0)
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "enemy_entry_transition"}}, TemporalRoleCurrent)})
			}
		}
	}

	if end.Team.GroupedRatio <= 0.55 {
		if endCounts.OurJungle >= cfg.MinHeroesInAreaForFarm {
			if a, err := space.New(action.KindFarm, action.TargetOurJungle); err == nil {
				s := clamp01(0.35 + 0.35*float64(endCounts.OurJungle)/5.0 + 0.15*(1.0-end.Team.GroupedRatio))
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "farm_our_jungle"}}, TemporalRoleCurrent)})
			}
		}
		if endCounts.OurTriangle >= cfg.MinHeroesInAreaForFarm {
			if a, err := space.New(action.KindFarm, action.TargetOurTriangle); err == nil {
				s := clamp01(0.35 + 0.35*float64(endCounts.OurTriangle)/5.0 + 0.15*(1.0-end.Team.GroupedRatio))
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "farm_our_triangle"}}, TemporalRoleCurrent)})
			}
		}
		if (endCounts.EnemyJungle-startCounts.EnemyJungle) < 1 && endCounts.EnemyJungle >= cfg.MinHeroesInAreaForFarm {
			if a, err := space.New(action.KindFarm, action.TargetEnemyJungle); err == nil {
				s := clamp01(0.25 + 0.35*float64(endCounts.EnemyJungle)/5.0 + 0.1*(1.0-end.Team.GroupedRatio))
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "farm_enemy_jungle"}}, TemporalRoleCurrent)})
			}
		}
		if (endCounts.EnemyTriangle-startCounts.EnemyTriangle) < 1 && endCounts.EnemyTriangle >= cfg.MinHeroesInAreaForFarm {
			if a, err := space.New(action.KindFarm, action.TargetEnemyTriangle); err == nil {
				s := clamp01(0.25 + 0.35*float64(endCounts.EnemyTriangle)/5.0 + 0.1*(1.0-end.Team.GroupedRatio))
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "farm_enemy_triangle"}}, TemporalRoleCurrent)})
			}
		}
	}

	for _, ln := range laneOrder {
		lb := laneByID(end, ln)
		if lb == nil || !lb.VisibleEnemyFrontAvailable {
			continue
		}
		if lb.VisibleEnemyFrontDepth != "OWN_SIDE" && lb.VisibleEnemyFrontDepth != "OWN_BASE" {
			continue
		}
		if heroCountInLane(end, ln) < 2 {
			continue
		}
		var tgt action.ActionTarget
		switch ln {
		case worldmap.LaneTop:
			tgt = action.TargetTop
		case worldmap.LaneMid:
			tgt = action.TargetMid
		case worldmap.LaneBot:
			tgt = action.TargetBot
		}
		if a, err := space.New(action.KindDefend, tgt); err == nil {
			s := clamp01(0.35 + 0.25*end.Team.GroupedRatio)
			cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "enemy_pressure_defend"}}, TemporalRoleCurrent)})
		}
	}

	if end.Team.Spread > start.Team.Spread && groupDelta < 0 {
		if countDistinctPrimaryRegions(end) >= 3 {
			if a, err := space.New(action.KindSplitMap, action.TargetNone); err == nil {
				s := clamp01(0.35 + 0.35*clamp01(spreadDelta/200.0) + 0.3*clamp01(-groupDelta/0.3))
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "split_map"}}, TemporalRoleCurrent)})
			}
		}
	}

	type wardTarget struct {
		tgt     action.ActionTarget
		regions []worldmap.RegionID
	}
	wTargets := []wardTarget{
		{tgt: action.TargetTop, regions: laneTopRegions()},
		{tgt: action.TargetMid, regions: laneMidRegions()},
		{tgt: action.TargetBot, regions: laneBotRegions()},
		{tgt: action.TargetEnemyJungle, regions: enemyJungleRegions(anchor.Side)},
		{tgt: action.TargetEnemyTriangle, regions: enemyTriangleRegions(anchor.Side)},
	}
	for _, wt := range wTargets {
		startW := countWardsInRegionSet(start, wt.regions)
		endW := countWardsInRegionSet(end, wt.regions)
		if endW-startW < 1 || endW < 1 {
			continue
		}
		if heroCountInRegionSet(end, wt.regions) < 1 {
			continue
		}
		if a, err := space.New(action.KindSetVision, wt.tgt); err == nil {
			delta := float64(endW - startW)
			s := clamp01(0.35 + 0.35*clamp01(delta/3.0) + 0.25*clamp01(float64(endW)/5.0))
			cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "ward_delta"}}, TemporalRolePast)})
		}
	}

	retreatFrame := ctx.Current
	if len(currentFuture) > 0 {
		retreatFrame = currentFuture[len(currentFuture)-1]
	}
	siegeNow := siegeFrame.Objectives.EnemyBaseAlliesNear >= 3 && siegeFrame.Team.GroupedRatio >= 0.45
	if inOurSafeRegion(retreatFrame.Team.MainGroupRegion) && !siegeNow {
		if towerKillAfter || barracksKillAfter || heroKillAfter {
			if a, err := space.New(action.KindRetreat, action.TargetNone); err == nil {
				s := clamp01(0.4 + 0.35*clamp01(spreadDelta/150.0) + 0.25*retreatFrame.Team.GroupedRatio)
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "retreat_after_contact"}}, TemporalRoleFuture)})
			}
		} else if anyPushEvidence && spreadDelta >= 0 && retreatFrame.Team.GroupedRatio < 0.55 {
			if a, err := space.New(action.KindReset, action.TargetNone); err == nil {
				s := clamp01(0.25 + 0.35*clamp01(spreadDelta/200.0) + 0.25*(0.55-retreatFrame.Team.GroupedRatio))
				cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "reset_recovery"}}, TemporalRoleFuture)})
			}
		}
	}

	if groupDelta >= cfg.MinGroupedRatioDeltaGroup && end.Team.Spread < start.Team.Spread && !anyStrongPushOrSiege(cands) {
		if a, err := space.New(action.KindGroup, action.TargetNone); err == nil {
			s := clamp01(0.3 + 0.6*clamp01(groupDelta/0.3) + 0.1*end.Team.GroupedRatio)
			cands = append(cands, Candidate{Action: a, EvidenceScore: s, Confidence: s, Evidence: tagEvidence([]Evidence{{Type: "grouped_ratio_delta", Value: ptrFloat(groupDelta)}}, TemporalRolePast)})
		}
	}

	best := 0.0
	for _, c := range cands {
		if c.Confidence > best {
			best = c.Confidence
		}
	}
	if best < cfg.MinConfidenceToLabel {
		window := append(pastCurrent, ctx.Future...)
		if holdEvidence(cfg, window) {
			if a, err := space.New(action.KindHold, action.TargetNone); err == nil {
				cands = append(cands, Candidate{Action: a, EvidenceScore: 0.5, Confidence: 0.6, Evidence: tagEvidence([]Evidence{{Type: "posture_stable"}}, TemporalRoleCurrent)})
			}
		}
	}

	uniq := map[int]Candidate{}
	for _, c := range cands {
		id := c.Action.ID
		if old, ok := uniq[id]; !ok || c.Confidence > old.Confidence {
			uniq[id] = c
		}
	}
	out := make([]Candidate, 0, len(uniq))
	for _, c := range uniq {
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Action.ID < out[j].Action.ID })
	return out
}

func smokeInPastCurrent(ctx TemporalContext) bool {
	frames := append(append([]BehaviorFrame(nil), ctx.Past...), ctx.Current)
	for _, f := range frames {
		for _, ev := range f.Events {
			if ev.Kind == "SMOKE_ACTIVATED" {
				return true
			}
		}
	}
	return false
}

func eventInFuture(ctx TemporalContext, kind string) bool {
	for _, f := range ctx.Future {
		for _, ev := range f.Events {
			if ev.Kind != kind {
				continue
			}
			if ev.GameTime != nil && *ev.GameTime < ctx.AnchorTime {
				continue
			}
			return true
		}
	}
	return false
}

func smokeCandidatesV02(cfg Config, space ActionSpaceProvider, side string, ctx TemporalContext) []Candidate {
	future := ctx.Future
	if len(future) == 0 {
		return nil
	}
	followEnd := float64(ctx.AnchorTime) + cfg.SmokeFollowSeconds
	var follow []BehaviorFrame
	for _, f := range future {
		if float64(f.GameTime) <= followEnd {
			follow = append(follow, f)
		}
	}
	if len(follow) == 0 {
		follow = future
	}
	dest := follow[len(follow)-1]
	return smokeCandidates(cfg, space, side, dest)
}

type heroAreaCountsRow struct {
	OurJungle     int
	OurTriangle   int
	EnemyJungle   int
	EnemyTriangle int
}

func heroAreaCounts(f BehaviorFrame) heroAreaCountsRow {
	row := heroAreaCountsRow{}
	for _, h := range f.Heroes {
		if !h.Observed || h.PrimaryRegion == nil {
			continue
		}
		r := *h.PrimaryRegion
		switch {
		case inAny(r, ourJungleRegions(f.Side)):
			row.OurJungle++
		case inAny(r, ourTriangleRegions(f.Side)):
			row.OurTriangle++
		case inAny(r, enemyJungleRegions(f.Side)):
			row.EnemyJungle++
		case inAny(r, enemyTriangleRegions(f.Side)):
			row.EnemyTriangle++
		}
	}
	return row
}

func laneByID(f BehaviorFrame, ln worldmap.LaneID) *LaneBehavior {
	for i := range f.Lanes {
		if f.Lanes[i].Lane == ln {
			return &f.Lanes[i]
		}
	}
	return nil
}

func laneOurFrontProgress(f BehaviorFrame, ln worldmap.LaneID) (float64, bool) {
	lb := laneByID(f, ln)
	if lb == nil || !lb.OurFrontAvailable {
		return 0, false
	}
	return lb.OurFrontTeamProgress, true
}

func heroCountInLane(f BehaviorFrame, ln worldmap.LaneID) int {
	regions := laneRegionsFor(ln)
	cnt := 0
	for _, h := range f.Heroes {
		if !h.Observed || h.PrimaryRegion == nil {
			continue
		}
		if inAny(*h.PrimaryRegion, regions) {
			cnt++
		}
	}
	return cnt
}

func laneRegionsFor(ln worldmap.LaneID) []worldmap.RegionID {
	switch ln {
	case worldmap.LaneTop:
		return laneTopRegions()
	case worldmap.LaneMid:
		return laneMidRegions()
	case worldmap.LaneBot:
		return laneBotRegions()
	default:
		return nil
	}
}

func laneTopRegions() []worldmap.RegionID {
	return []worldmap.RegionID{worldmap.RegionTopLaneCenter, worldmap.RegionTopLaneRadiant, worldmap.RegionTopLaneDire, worldmap.RegionTopRiver}
}
func laneMidRegions() []worldmap.RegionID {
	return []worldmap.RegionID{worldmap.RegionMidLaneCenter, worldmap.RegionMidLaneRadiant, worldmap.RegionMidLaneDire}
}
func laneBotRegions() []worldmap.RegionID {
	return []worldmap.RegionID{worldmap.RegionBotLaneCenter, worldmap.RegionBotLaneRadiant, worldmap.RegionBotLaneDire, worldmap.RegionBotRiver}
}

func ourJungleRegions(side string) []worldmap.RegionID {
	if side == "radiant" {
		return []worldmap.RegionID{worldmap.RegionRadiantMainJungle, worldmap.RegionRadiantOfflaneJungle}
	}
	return []worldmap.RegionID{worldmap.RegionDireMainJungle, worldmap.RegionDireOfflaneJungle}
}

func ourTriangleRegions(side string) []worldmap.RegionID {
	if side == "radiant" {
		return []worldmap.RegionID{worldmap.RegionRadiantTriangle}
	}
	return []worldmap.RegionID{worldmap.RegionDireTriangle}
}

func enemyJungleRegions(side string) []worldmap.RegionID {
	if side == "radiant" {
		return []worldmap.RegionID{worldmap.RegionDireMainJungle, worldmap.RegionDireOfflaneJungle}
	}
	return []worldmap.RegionID{worldmap.RegionRadiantMainJungle, worldmap.RegionRadiantOfflaneJungle}
}

func enemyTriangleRegions(side string) []worldmap.RegionID {
	if side == "radiant" {
		return []worldmap.RegionID{worldmap.RegionDireTriangle}
	}
	return []worldmap.RegionID{worldmap.RegionRadiantTriangle}
}

func inAny(r worldmap.RegionID, xs []worldmap.RegionID) bool {
	for _, x := range xs {
		if r == x {
			return true
		}
	}
	return false
}

func smokeCandidates(cfg Config, space ActionSpaceProvider, side string, end BehaviorFrame) []Candidate {
	votes := map[action.ActionTarget]int{}
	for _, h := range end.Heroes {
		if !h.Observed || h.PrimaryRegion == nil {
			continue
		}
		t := regionToSmokeTarget(side, *h.PrimaryRegion)
		if t == action.TargetNone {
			continue
		}
		votes[t]++
	}
	if len(votes) == 0 {
		return nil
	}
	type vt struct {
		t action.ActionTarget
		n int
	}
	rows := make([]vt, 0, len(votes))
	for k, v := range votes {
		rows = append(rows, vt{t: k, n: v})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].t < rows[j].t
	})
	best := rows[0]
	bestFrac := float64(best.n) / 5.0
	if bestFrac < cfg.SmokeTargetClearFraction && len(rows) >= 2 {
		second := rows[1]
		var out []Candidate
		for _, r := range []vt{best, second} {
			if a, err := space.New(action.KindSmoke, r.t); err == nil {
				conf := clamp01(0.5 + float64(r.n)/5.0)
				out = append(out, Candidate{Action: a, EvidenceScore: conf, Confidence: conf, Evidence: []Evidence{{Type: "smoke_vote"}}})
			}
		}
		return out
	}
	if a, err := space.New(action.KindSmoke, best.t); err == nil {
		conf := clamp01(0.45 + bestFrac)
		return []Candidate{{Action: a, EvidenceScore: conf, Confidence: conf, Evidence: []Evidence{{Type: "smoke_majority"}}}}
	}
	return nil
}

func regionToSmokeTarget(side string, r worldmap.RegionID) action.ActionTarget {
	if inAny(r, laneTopRegions()) {
		return action.TargetTop
	}
	if inAny(r, laneMidRegions()) {
		return action.TargetMid
	}
	if inAny(r, laneBotRegions()) {
		return action.TargetBot
	}
	if inAny(r, enemyJungleRegions(side)) {
		return action.TargetEnemyJungle
	}
	if inAny(r, enemyTriangleRegions(side)) {
		return action.TargetEnemyTriangle
	}
	return action.TargetNone
}

func holdEvidence(cfg Config, window []BehaviorFrame) bool {
	_ = cfg
	if len(window) < 2 {
		return false
	}
	start := window[0]
	end := window[len(window)-1]
	for _, f := range window {
		if f.Objectives.RoshanAlliesNear >= 3 {
			return false
		}
		for _, ev := range f.Events {
			if ev.Kind == "SMOKE_ACTIVATED" {
				return false
			}
		}
	}
	grpDelta := abs(end.Team.GroupedRatio - start.Team.GroupedRatio)
	spreadDelta := abs(end.Team.Spread - start.Team.Spread)
	if grpDelta > 0.06 || spreadDelta > 200 {
		return false
	}
	for _, ln := range []worldmap.LaneID{worldmap.LaneTop, worldmap.LaneMid, worldmap.LaneBot} {
		d0, ok0 := laneOurFrontProgress(start, ln)
		d1, ok1 := laneOurFrontProgress(end, ln)
		if ok0 && ok1 && abs(d1-d0) > 0.06 {
			return false
		}
	}
	return true
}

func anyStrongPushOrSiege(cands []Candidate) bool {
	for _, c := range cands {
		if c.Action.Kind == action.KindPush && c.Confidence >= 0.7 {
			return true
		}
		if c.Action.Kind == action.KindSiege && c.Confidence >= 0.7 {
			return true
		}
	}
	return false
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func ptrFloat(v float64) *float64 { return &v }

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func inOurSafeRegion(r worldmap.RegionID) bool {
	switch r {
	case worldmap.RegionRadiantBase, worldmap.RegionDireBase,
		worldmap.RegionRadiantHighground, worldmap.RegionDireHighground,
		worldmap.RegionRadiantTriangle, worldmap.RegionDireTriangle,
		worldmap.RegionRadiantMainJungle, worldmap.RegionDireMainJungle,
		worldmap.RegionRadiantOfflaneJungle, worldmap.RegionDireOfflaneJungle:
		return true
	default:
		return false
	}
}

func countDistinctPrimaryRegions(f BehaviorFrame) int {
	set := map[worldmap.RegionID]struct{}{}
	for _, h := range f.Heroes {
		if h.Observed && h.PrimaryRegion != nil {
			set[*h.PrimaryRegion] = struct{}{}
		}
	}
	return len(set)
}

func countWardsInRegionSet(f BehaviorFrame, regions []worldmap.RegionID) int {
	cnt := 0
	for _, r := range f.AlliedWardRegions {
		if inAny(r, regions) {
			cnt++
		}
	}
	return cnt
}

func heroCountInRegionSet(f BehaviorFrame, regions []worldmap.RegionID) int {
	cnt := 0
	for _, h := range f.Heroes {
		if !h.Observed || h.PrimaryRegion == nil {
			continue
		}
		if inAny(*h.PrimaryRegion, regions) {
			cnt++
		}
	}
	return cnt
}
