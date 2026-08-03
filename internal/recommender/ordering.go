package recommender

import "sort"

// rankRecommendations sorts recommendations by score descending with a stable
// hero_id ascending tie-break. Scores are assumed already rounded by the model
// layer; the deterministic tie-break neutralizes any remaining equality (§6).
func rankRecommendations(recs []Recommendation) {
	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].Score != recs[j].Score {
			return recs[i].Score > recs[j].Score
		}
		return recs[i].HeroID < recs[j].HeroID
	})
}

// rankEnemyPicks sorts predicted enemy picks by probability descending with a
// hero_id ascending tie-break.
func rankEnemyPicks(picks []EnemyPick) {
	sort.SliceStable(picks, func(i, j int) bool {
		if picks[i].Prob != picks[j].Prob {
			return picks[i].Prob > picks[j].Prob
		}
		return picks[i].HeroID < picks[j].HeroID
	})
}

// sortMetaHeroes sorts position-meta heroes by score descending with a hero_id
// ascending tie-break (deterministic, §6).
func sortMetaHeroes(hs []MetaHero) {
	sort.SliceStable(hs, func(i, j int) bool {
		if hs[i].Score != hs[j].Score {
			return hs[i].Score > hs[j].Score
		}
		return hs[i].HeroID < hs[j].HeroID
	})
}
