package main

import (
	"github.com/spf13/pflag"

	"cmnet/internal/objectives"
)

// alertLeadFlags are per-kind pre-alert leads and active clock windows for
// rune/lotus/etc. notifications. Lead override: 0 = use --alert-lead, -1 = disable kind.
// Window: from -1 = no lower bound; until 0 = no upper bound (seconds of game clock).
type alertLeadFlags struct {
	defaultSec int
	bounty     int
	power      int
	wisdom     int
	lotus      int
	tormentor  int
	daynight   int

	fromBounty, untilBounty       int
	fromPower, untilPower         int
	fromWisdom, untilWisdom       int
	fromLotus, untilLotus         int
	fromTormentor, untilTormentor int
	fromDaynight, untilDaynight   int
}

func (a *alertLeadFlags) bind(f *pflag.FlagSet) {
	f.IntVar(&a.defaultSec, "alert-lead", objectives.DefaultLead,
		"seconds before a spawn to flag Due / ring; -1 disables all kinds without a positive per-kind override")
	f.IntVar(&a.bounty, "alert-lead-bounty", 0, "bounty lead; 0 = use --alert-lead, -1 = disable")
	f.IntVar(&a.power, "alert-lead-power", 0, "power lead; 0 = use --alert-lead, -1 = disable")
	f.IntVar(&a.wisdom, "alert-lead-wisdom", 0, "wisdom lead; 0 = use --alert-lead, -1 = disable")
	f.IntVar(&a.lotus, "alert-lead-lotus", 0, "lotus lead; 0 = use --alert-lead, -1 = disable")
	f.IntVar(&a.tormentor, "alert-lead-tormentor", 0, "tormentor lead; 0 = use --alert-lead, -1 = disable")
	f.IntVar(&a.daynight, "alert-lead-daynight", 0, "day/night lead; 0 = use --alert-lead, -1 = disable")

	bindWindow := func(kind string, from, until *int) {
		f.IntVar(from, "alert-from-"+kind, objectives.NoLowerBound,
			"earliest game-clock second to arm "+kind+" alerts (-1 = no lower limit; 0 = from horn)")
		f.IntVar(until, "alert-until-"+kind, 0,
			"latest game-clock second to arm "+kind+" alerts (0 = no upper limit; 35:00 = 2100)")
	}
	bindWindow("bounty", &a.fromBounty, &a.untilBounty)
	bindWindow("power", &a.fromPower, &a.untilPower)
	bindWindow("wisdom", &a.fromWisdom, &a.untilWisdom)
	bindWindow("lotus", &a.fromLotus, &a.untilLotus)
	bindWindow("tormentor", &a.fromTormentor, &a.untilTormentor)
	bindWindow("daynight", &a.fromDaynight, &a.untilDaynight)
}

func (a alertLeadFlags) leads() objectives.Leads {
	l := objectives.Leads{Default: a.defaultSec}
	m := map[objectives.Kind]int{}
	put := func(k objectives.Kind, v int) {
		if v != 0 { // 0 = inherit default; -1 disable; >0 override
			m[k] = v
		}
	}
	put(objectives.Bounty, a.bounty)
	put(objectives.Power, a.power)
	put(objectives.Wisdom, a.wisdom)
	put(objectives.Lotus, a.lotus)
	put(objectives.Tormentor, a.tormentor)
	put(objectives.DayNight, a.daynight)
	if len(m) > 0 {
		l.ByKind = m
	}
	return l
}

func (a alertLeadFlags) windows() objectives.Windows {
	type pair struct {
		kind        objectives.Kind
		from, until int
	}
	pairs := []pair{
		{objectives.Bounty, a.fromBounty, a.untilBounty},
		{objectives.Power, a.fromPower, a.untilPower},
		{objectives.Wisdom, a.fromWisdom, a.untilWisdom},
		{objectives.Lotus, a.fromLotus, a.untilLotus},
		{objectives.Tormentor, a.fromTormentor, a.untilTormentor},
		{objectives.DayNight, a.fromDaynight, a.untilDaynight},
	}
	m := map[objectives.Kind]objectives.Window{}
	for _, p := range pairs {
		if p.from == objectives.NoLowerBound && p.until == 0 {
			continue // unlimited — omit
		}
		m[p.kind] = objectives.Window{From: p.from, Until: p.until}
	}
	if len(m) == 0 {
		return objectives.Windows{}
	}
	return objectives.Windows{ByKind: m}
}
