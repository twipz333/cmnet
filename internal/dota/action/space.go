package action

import "fmt"

type kindTargetPair struct {
	kind   ActionKind
	target ActionTarget
}

type validKindRow struct {
	kind    ActionKind
	targets []ActionTarget
}

// ActionSpace enumerates schema-valid macro actions with stable IDs.
//
// This is schema validity only (not state availability).
type ActionSpace struct {
	schemaVersion string
	actions       []MacroAction
	byID          map[int]MacroAction
	idByPair      map[kindTargetPair]int
}

var defaultSpaceV0 = newActionSpaceV0()

func newActionSpaceV0() ActionSpace {
	valid := []validKindRow{
		{kind: KindHold, targets: []ActionTarget{TargetNone}},
		{kind: KindFarm, targets: []ActionTarget{TargetOurJungle, TargetOurTriangle, TargetEnemyJungle, TargetEnemyTriangle}},
		{kind: KindPush, targets: []ActionTarget{TargetTop, TargetMid, TargetBot}},
		{kind: KindDefend, targets: []ActionTarget{TargetTop, TargetMid, TargetBot, TargetOurBase}},
		{kind: KindControlObjective, targets: []ActionTarget{TargetRoshan, TargetTormentor}},
		{kind: KindTakeObjective, targets: []ActionTarget{TargetRoshan, TargetTormentor}},
		{kind: KindSmoke, targets: []ActionTarget{TargetTop, TargetMid, TargetBot, TargetEnemyJungle, TargetEnemyTriangle}},
		{kind: KindInvade, targets: []ActionTarget{TargetEnemyJungle, TargetEnemyTriangle}},
		{kind: KindGroup, targets: []ActionTarget{TargetNone}},
		{kind: KindSplitMap, targets: []ActionTarget{TargetNone}},
		{kind: KindSetVision, targets: []ActionTarget{TargetTop, TargetMid, TargetBot, TargetRoshan, TargetEnemyJungle, TargetEnemyTriangle}},
		{kind: KindSiege, targets: []ActionTarget{TargetEnemyBase}},
		{kind: KindReset, targets: []ActionTarget{TargetNone}},
		{kind: KindRetreat, targets: []ActionTarget{TargetNone}},
	}

	actions := make([]MacroAction, 0, 128)
	byID := map[int]MacroAction{}
	idByPair := map[kindTargetPair]int{}

	id := 0
	for _, row := range valid {
		for _, tgt := range row.targets {
			a := MacroAction{SchemaVersion: SchemaVersion, ID: id, Kind: row.kind, Target: tgt}
			actions = append(actions, a)
			byID[id] = a
			idByPair[kindTargetPair{kind: row.kind, target: tgt}] = id
			id++
		}
	}

	return ActionSpace{schemaVersion: SchemaVersion, actions: actions, byID: byID, idByPair: idByPair}
}

// DefaultSpaceV0 returns the schema-valid MacroAction v0.1 action space.
func DefaultSpaceV0() ActionSpace { return defaultSpaceV0 }

func (s ActionSpace) SchemaVersion() string { return s.schemaVersion }

// All returns all schema-valid actions in stable ID order.
func (s ActionSpace) All() []MacroAction {
	out := make([]MacroAction, len(s.actions))
	copy(out, s.actions)
	return out
}

func (s ActionSpace) ByID(id int) (MacroAction, bool) {
	a, ok := s.byID[id]
	return a, ok
}

func (s ActionSpace) IDOf(kind ActionKind, target ActionTarget) (int, bool) {
	id, ok := s.idByPair[kindTargetPair{kind: kind, target: target}]
	return id, ok
}

// Validate checks schema validity of a (kind,target) combination.
//
// It is state-independent: Roshan being dead does not invalidate TAKE_OBJECTIVE.
func (s ActionSpace) Validate(kind ActionKind, target ActionTarget) error {
	if _, ok := s.idByPair[kindTargetPair{kind: kind, target: target}]; !ok {
		return fmt.Errorf("invalid macro action schema combination: kind=%s target=%s", kind, target)
	}
	return nil
}

func (s ActionSpace) New(kind ActionKind, target ActionTarget) (MacroAction, error) {
	if err := s.Validate(kind, target); err != nil {
		return MacroAction{}, err
	}
	id, _ := s.IDOf(kind, target)
	return MacroAction{SchemaVersion: SchemaVersion, ID: id, Kind: kind, Target: target}, nil
}
