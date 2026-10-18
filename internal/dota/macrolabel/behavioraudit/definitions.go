package behavioraudit

import (
	"fmt"
	"sort"

	"cmnet/internal/dota/action"
)

// ActionDefinition is a plain-language operational description of one MacroAction.
// Descriptions describe observable team behavior, not correctness or optimality.
type ActionDefinition struct {
	ActionID   int               `json:"action_id"`
	Kind       action.ActionKind `json:"kind"`
	Target     action.ActionTarget `json:"target"`
	Label      string            `json:"label"`
	Definition string            `json:"definition"`
}

// AllDefinitions returns HumanDefinition for every action in DefaultSpaceV0().
func AllDefinitions() []ActionDefinition {
	space := action.DefaultSpaceV0()
	all := space.All()
	out := make([]ActionDefinition, 0, len(all))
	for _, a := range all {
		out = append(out, ActionDefinition{
			ActionID:   a.ID,
			Kind:       a.Kind,
			Target:     a.Target,
			Label:      fmt.Sprintf("%s / %s", a.Kind, a.Target),
			Definition: HumanDefinition(a.Kind, a.Target),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ActionID < out[j].ActionID })
	return out
}

// DefinitionByActionID looks up a definition by stable action ID.
func DefinitionByActionID(id int) (ActionDefinition, bool) {
	for _, d := range AllDefinitions() {
		if d.ActionID == id {
			return d, true
		}
	}
	return ActionDefinition{}, false
}

// HumanDefinition returns a Russian operational behavior description.
func HumanDefinition(kind action.ActionKind, target action.ActionTarget) string {
	switch kind {
	case action.KindHold:
		return "Команда сохраняет текущую макро-позу: без явного нового направления давления, удержания или перегруппировки."
	case action.KindFarm:
		switch target {
		case action.TargetOurJungle:
			return "Основная активность союзников — фарм нейтралов/ресурсов в своём лесу."
		case action.TargetOurTriangle:
			return "Основная активность союзников — фарм ресурсов в своём треугольнике."
		case action.TargetEnemyJungle:
			return "Союзники заходят на сторону врага и фармят нейтралов в чужом лесу без явной осады."
		case action.TargetEnemyTriangle:
			return "Союзники фармят ресурсы во вражеском треугольнике без явной осады базы."
		}
	case action.KindPush:
		switch target {
		case action.TargetTop:
			return "Команда продвигает верхнюю линию: герои и/или крипы двигаются вперёд по TOP."
		case action.TargetMid:
			return "Команда продвигает центральную линию: давление идёт по MID."
		case action.TargetBot:
			return "Команда продвигает нижнюю линию: давление идёт по BOT."
		}
	case action.KindDefend:
		switch target {
		case action.TargetTop:
			return "Союзники смещаются защищать верхнюю линию/башню от вражеского давления."
		case action.TargetMid:
			return "Союзники смещаются защищать центральную линию/башню от вражеского давления."
		case action.TargetBot:
			return "Союзники смещаются защищать нижнюю линию/башню от вражеского давления."
		case action.TargetOurBase:
			return "Союзники стягиваются защищать свою базу/высокий грунт."
		}
	case action.KindControlObjective:
		switch target {
		case action.TargetRoshan:
			return "Команда занимает и удерживает пространство вокруг Рошана, не обязательно начиная килл прямо сейчас."
		case action.TargetTormentor:
			return "Команда занимает и удерживает пространство вокруг Торментора, не обязательно начиная килл прямо сейчас."
		}
	case action.KindTakeObjective:
		switch target {
		case action.TargetRoshan:
			return "Команда начинает или завершает убийство Рошана."
		case action.TargetTormentor:
			return "Команда начинает или завершает убийство Торментора."
		}
	case action.KindSmoke:
		switch target {
		case action.TargetTop:
			return "Команда использует смок и движется искать контакт в верхней макро-зоне."
		case action.TargetMid:
			return "Команда использует смок и движется искать контакт в центральной макро-зоне."
		case action.TargetBot:
			return "Команда использует смок и движется искать контакт в нижней макро-зоне."
		case action.TargetEnemyJungle:
			return "Команда использует смок, чтобы зайти во вражеский лес и создать контакт."
		case action.TargetEnemyTriangle:
			return "Команда использует смок, чтобы зайти во вражеский треугольник и создать контакт."
		}
	case action.KindInvade:
		switch target {
		case action.TargetEnemyJungle:
			return "Союзники активно входят и оспаривают пространство вражеского леса."
		case action.TargetEnemyTriangle:
			return "Союзники активно входят и оспаривают пространство вражеского треугольника."
		}
	case action.KindGroup:
		return "Союзники сжимаются в более плотную основную группу без явной цели-объектива."
	case action.KindSplitMap:
		return "Команда намеренно распределяет героев по карте вместо одной основной группы."
	case action.KindSetVision:
		switch target {
		case action.TargetTop:
			return "Приоритет — постановка/обновление зрения в верхней макро-зоне."
		case action.TargetMid:
			return "Приоритет — постановка/обновление зрения в центральной макро-зоне."
		case action.TargetBot:
			return "Приоритет — постановка/обновление зрения в нижней макро-зоне."
		case action.TargetRoshan:
			return "Приоритет — постановка/обновление зрения вокруг ямы Рошана."
		case action.TargetEnemyJungle:
			return "Приоритет — постановка/обновление зрения во вражеском лесу."
		case action.TargetEnemyTriangle:
			return "Приоритет — постановка/обновление зрения во вражеском треугольнике."
		}
	case action.KindSiege:
		return "Команда организует согласованную осаду высокого грунта / вражеской базы."
	case action.KindReset:
		return "Команда прекращает текущую макро-активность и пересобирается (контролируемый отход/перезаход)."
	case action.KindRetreat:
		return "Команда срочно увеличивает дистанцию от угрозы и снимает небезопасное макро-обязательство."
	}
	return fmt.Sprintf("Описание отсутствует для %s / %s", kind, target)
}