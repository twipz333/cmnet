package macrolabel

// TemporalContext splits a trajectory around an anchor frame into directional windows.
type TemporalContext struct {
	Past        []BehaviorFrame
	Current     BehaviorFrame
	Future      []BehaviorFrame
	AnchorIndex int
	AnchorTime  int
}

func splitTemporal(cfg Config, frames []BehaviorFrame, at int) TemporalContext {
	if at < 0 || at >= len(frames) {
		return TemporalContext{}
	}
	anchor := frames[at]
	loT := float64(anchor.GameTime) - cfg.LookbackSeconds
	hiT := float64(anchor.GameTime) + cfg.LookaheadSeconds
	ctx := TemporalContext{
		Current:     anchor,
		AnchorIndex: at,
		AnchorTime:  anchor.GameTime,
	}
	for i, f := range frames {
		gt := float64(f.GameTime)
		if gt < loT || gt > hiT {
			continue
		}
		if i < at {
			ctx.Past = append(ctx.Past, f)
		} else if i > at {
			ctx.Future = append(ctx.Future, f)
		}
	}
	return ctx
}

func temporalStart(ctx TemporalContext) BehaviorFrame {
	if len(ctx.Past) > 0 {
		return ctx.Past[0]
	}
	return ctx.Current
}

func temporalEnd(ctx TemporalContext) BehaviorFrame {
	if len(ctx.Future) > 0 {
		return ctx.Future[len(ctx.Future)-1]
	}
	return ctx.Current
}

func tagEvidence(ev []Evidence, role TemporalRole) []Evidence {
	if len(ev) == 0 {
		return nil
	}
	out := make([]Evidence, len(ev))
	for i, e := range ev {
		out[i] = e
		out[i].TemporalRole = role
	}
	return out
}

func mergeEvidence(parts ...[]Evidence) []Evidence {
	var out []Evidence
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
