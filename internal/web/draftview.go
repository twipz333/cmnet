package web

import (
	"net/http"
	"sort"

	"cmnet/internal/draft"
	"cmnet/internal/roles"
	"cmnet/internal/session"
)

// This file serves the pieces the draft screen needs beyond the raw board: the
// whole step sequence (so the UI can draw the draft as one ribbon instead of
// guessing the schedule), and a hero catalog with the positions each hero is
// played in (so the picker can filter by role).

// seqStepDTO is one step of the draft in order, with the hero placed there.
type seqStepDTO struct {
	Team    string `json:"team"`   // "Radiant" | "Dire"
	Action  string `json:"action"` // "Pick" | "Ban"
	HeroID  int    `json:"heroId"` // 0 while empty
	Ours    bool   `json:"ours"`
	Current bool   `json:"current"`
}

// sequenceOf lays the board onto the schedule. The manual board knows its own
// sequence (including any per-step overrides); a GSI board only reports each
// side's picks and bans in order, which are slotted into the schedule's steps for
// that side and action, in order.
func sequenceOf(view draft.View, order draft.Order, ourTeam draft.Team) []seqStepDTO {
	var steps []draft.Step
	var heroes []int
	if ds, ok := view.(*draft.DraftState); ok {
		steps = ds.Seq
		heroes = make([]int, len(ds.Seq))
		for i := 0; i < ds.Cursor && i < len(ds.Selections); i++ {
			heroes[i] = ds.Selections[i]
		}
	} else {
		steps = order.OrDefault().Sequence(view.FirstPickTeam())
		heroes = make([]int, len(steps))
		used := map[[2]int]int{}
		for i, st := range steps {
			var list []int
			if st.Action == draft.Pick {
				list = view.Picks(st.Team)
			} else {
				list = view.Bans(st.Team)
			}
			k := [2]int{int(st.Team), int(st.Action)}
			if used[k] < len(list) {
				heroes[i] = list[used[k]]
				used[k]++
			}
		}
	}
	cur := -1
	if _, ok := view.Current(); ok {
		for i, h := range heroes {
			if h == 0 {
				cur = i
				break
			}
		}
	}
	out := make([]seqStepDTO, len(steps))
	for i, st := range steps {
		out[i] = seqStepDTO{
			Team: st.Team.String(), Action: st.Action.String(),
			HeroID: heroes[i], Ours: st.Team == ourTeam, Current: i == cur,
		}
	}
	return out
}

// catalogHero is one hero with the positions it is played in, most common first.
type catalogHero struct {
	session.Hero
	Roles []int `json:"roles"`
}

// minCatalogRoleShare is how often a hero must be played in a position for the
// picker to list it under that role.
const minCatalogRoleShare = 0.15

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	sess, err := s.session()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"heroes": []catalogHero{}})
		return
	}
	cfg := sess.Config()
	var prior *roles.Prior
	var elig map[int][]int
	if rec := sess.Recommender(); rec != nil {
		prior, elig = rec.RolePrior(), rec.PosByID()
	}
	out := make([]catalogHero, 0, len(cfg.Heroes))
	for _, h := range cfg.Heroes {
		out = append(out, catalogHero{Hero: h, Roles: heroRoles(h.ID, prior, elig)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label() < out[j].Label() })
	writeJSON(w, http.StatusOK, map[string]any{"heroes": out})
}

func heroRoles(id int, prior *roles.Prior, elig map[int][]int) []int {
	if prior != nil {
		if row, ok := prior.Prob[id]; ok {
			type pv struct {
				pos int
				v   float64
			}
			var ps []pv
			for i, v := range row {
				if v >= minCatalogRoleShare {
					ps = append(ps, pv{i + 1, v})
				}
			}
			sort.SliceStable(ps, func(a, b int) bool { return ps[a].v > ps[b].v })
			out := make([]int, len(ps))
			for i, p := range ps {
				out[i] = p.pos
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if ps, ok := elig[id]; ok {
		return append([]int(nil), ps...)
	}
	return []int{}
}

func (s *Server) doNewDraft(*http.Request) (string, error) {
	sess, err := s.session()
	if err != nil {
		return "", err
	}
	if err := sess.NewDraft(); err != nil {
		return "", err
	}
	s.mu.Lock()
	pending := s.pendingCfg
	s.pendingCfg = nil
	s.draftID = newSessionID()
	s.draftRevision = 0
	s.mu.Unlock()
	if pending != nil {
		pending.OurTeam, pending.FirstPick = sess.OurTeam(), sess.FirstPick()
		fresh := session.New(*pending)
		s.applyTeam(fresh)
		s.mu.Lock()
		s.sess = fresh
		s.mu.Unlock()
		s.startGSIPump(fresh)
		s.followIntoSession()
		s.shareRefresh()
		return "new draft · updated model activated", nil
	}
	return "new draft", nil
}
