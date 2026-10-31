package web

import (
	"net/http"
	"os"
	"sort"
	"strconv"

	"cmnet/internal/declog"
)

// routeDeclogHistory registers read-only after-match history endpoints.
// They load the local decisions.jsonl beside the cache; they never invent
// confidence, win rates, or reasons that were not recorded.
func (s *Server) routeDeclogHistory(mux *http.ServeMux) {
	mux.HandleFunc("/api/declog/history", s.handleDeclogHistory)
}

type historyListResponse struct {
	Path     string                `json:"path"`
	Exists   bool                  `json:"exists"`
	Logging  bool                  `json:"logging"`
	Count    int                   `json:"count"`
	Drafts   []historyDraftSummary `json:"drafts"`
	Note     string                `json:"note"`
	Evidence string                `json:"evidence"` // empty|ready|partial|unavailable
}

type historyDraftSummary struct {
	ID            string          `json:"id"`
	TS            int64           `json:"ts"`
	OurTeam       string          `json:"ourTeam,omitempty"`
	FirstPick     string          `json:"firstPick,omitempty"`
	Source        string          `json:"source,omitempty"`
	BundleVersion string          `json:"bundleVersion,omitempty"`
	PatchTag      string          `json:"patchTag,omitempty"`
	SnapshotHash  string          `json:"snapshotHash,omitempty"`
	EnemyCount    int             `json:"enemyCount"`
	AllyCount     int             `json:"allyCount"`
	DecisionCount int             `json:"decisionCount"`
	TookTop       int             `json:"tookTop"`
	OffRec        int             `json:"offRec"`
	WithReason    int             `json:"withReason"`
	FinalPresent  bool            `json:"finalPresent"`
	Outcome       *historyOutcome `json:"outcome,omitempty"`
	ProvenanceGap []string        `json:"provenanceGap,omitempty"`
}

type historyOutcome struct {
	Win     bool   `json:"win"`
	MatchID string `json:"matchId,omitempty"`
	Note    string `json:"note,omitempty"`
}

type historyDetailResponse struct {
	Path     string              `json:"path"`
	Logging  bool                `json:"logging"`
	Found    bool                `json:"found"`
	Draft    *historyDraftDetail `json:"draft,omitempty"`
	Note     string              `json:"note"`
	Evidence string              `json:"evidence"`
}

type historyDraftDetail struct {
	historyDraftSummary
	Decisions []historyDecision `json:"decisions"`
	Final     *historyBoard     `json:"final,omitempty"`
}

type historyDecision struct {
	Seq        int           `json:"seq"`
	Action     string        `json:"action"`
	Chosen     int           `json:"chosen"`
	ChosenName string        `json:"chosenName,omitempty"`
	ChosenRank int           `json:"chosenRank"`
	Accepted   bool          `json:"accepted"`
	Reason     string        `json:"reason,omitempty"`
	Strength   *float64      `json:"strength,omitempty"`
	TopHero    int           `json:"topHero,omitempty"`
	TopName    string        `json:"topName,omitempty"`
	TopWhy     string        `json:"topWhy,omitempty"`
	TopScore   *float64      `json:"topScore,omitempty"`
	Recs       []historyRec  `json:"recs,omitempty"`
	EnemyPred  []historyPred `json:"enemyPred,omitempty"`
	Board      historyBoard  `json:"board"`
}

type historyRec struct {
	HeroID int     `json:"heroId"`
	Name   string  `json:"name,omitempty"`
	Score  float64 `json:"score"`
	Role   int     `json:"role,omitempty"`
	Player string  `json:"player,omitempty"`
	Why    string  `json:"why,omitempty"`
}

type historyPred struct {
	HeroID int     `json:"heroId"`
	Name   string  `json:"name,omitempty"`
	Prob   float64 `json:"prob"`
	Source string  `json:"source,omitempty"`
}

type historyBoard struct {
	RadiantPicks []int `json:"radiantPicks,omitempty"`
	DirePicks    []int `json:"direPicks,omitempty"`
	RadiantBans  []int `json:"radiantBans,omitempty"`
	DireBans     []int `json:"direBans,omitempty"`
}

func (s *Server) handleDeclogHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, logging := s.declogPath()
	id := r.URL.Query().Get("id")
	if id != "" {
		s.writeDeclogDetail(w, path, logging, id)
		return
	}
	s.writeDeclogList(w, path, logging, r)
}

func (s *Server) declogPath() (path string, logging bool) {
	if sess := s.sess; sess != nil {
		logging = sess.Logging()
		if p := sess.LogPath(); p != "" {
			return p, logging
		}
	}
	if s.shell.CachePath != "" {
		return declog.PathFor(s.shell.CachePath), logging
	}
	return "", logging
}

func (s *Server) writeDeclogList(w http.ResponseWriter, path string, logging bool, r *http.Request) {
	resp := historyListResponse{
		Path:    path,
		Logging: logging,
		Drafts:  []historyDraftSummary{},
	}
	if path == "" {
		resp.Evidence = "unavailable"
		resp.Note = "Путь к журналу решений неизвестен (нет cache path). История недоступна в этом режиме."
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			resp.Evidence = "empty"
			resp.Note = "Журнал решений ещё пуст — сыграй драфт с включённым decision log, или файл появится после первой записи."
			writeJSON(w, http.StatusOK, resp)
			return
		}
		resp.Evidence = "unavailable"
		resp.Note = "Не удалось прочитать журнал: " + err.Error()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Exists = true
	drafts, err := declog.Load(path)
	if err != nil {
		resp.Evidence = "unavailable"
		resp.Note = "Ошибка разбора журнала: " + err.Error()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	last := 0
	if v := r.URL.Query().Get("last"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			last = n
		}
	}
	if last > 0 && len(drafts) > last {
		drafts = drafts[len(drafts)-last:]
	}
	// Newest first for captain browsing.
	sort.SliceStable(drafts, func(i, j int) bool {
		if drafts[i].TS != drafts[j].TS {
			return drafts[i].TS > drafts[j].TS
		}
		return drafts[i].ID > drafts[j].ID
	})
	for _, d := range drafts {
		resp.Drafts = append(resp.Drafts, summarizeDraft(d))
	}
	resp.Count = len(resp.Drafts)
	switch {
	case resp.Count == 0:
		resp.Evidence = "empty"
		resp.Note = "В журнале нет собранных драфтов."
	case anyPartialHistory(resp.Drafts):
		resp.Evidence = "partial"
		resp.Note = "Есть драфты без исхода или без provenance (бандл/патч/снапшот). Показано только записанное."
	default:
		resp.Evidence = "ready"
		resp.Note = "Локальный журнал решений. Цифры — только из decisions.jsonl, без выдуманной статистики."
	}
	if !logging {
		resp.Note += " Запись новых решений сейчас выключена (--decision-log=false); просмотр старых записей всё ещё возможен."
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeDeclogDetail(w http.ResponseWriter, path string, logging bool, id string) {
	resp := historyDetailResponse{Path: path, Logging: logging}
	if path == "" {
		resp.Evidence = "unavailable"
		resp.Note = "Путь к журналу неизвестен."
		writeJSON(w, http.StatusOK, resp)
		return
	}
	drafts, err := declog.Load(path)
	if err != nil {
		resp.Evidence = "unavailable"
		resp.Note = "Ошибка разбора журнала: " + err.Error()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	label := s.heroLabelFn()
	for _, d := range drafts {
		if d.ID != id {
			continue
		}
		sum := summarizeDraft(d)
		detail := &historyDraftDetail{historyDraftSummary: sum}
		for _, dec := range d.Decisions {
			hd := historyDecision{
				Seq:        dec.Seq,
				Action:     dec.Action,
				Chosen:     dec.Chosen,
				ChosenName: label(dec.Chosen),
				ChosenRank: dec.ChosenRank,
				Accepted:   dec.Accepted(),
				Reason:     dec.Reason,
				Strength:   dec.Strength,
				Board:      historyBoardOf(dec.Board),
			}
			for _, rec := range dec.Recs {
				hd.Recs = append(hd.Recs, historyRec{
					HeroID: rec.HeroID,
					Name:   label(rec.HeroID),
					Score:  rec.Score,
					Role:   rec.Role,
					Player: rec.Player,
					Why:    rec.Why,
				})
			}
			if len(dec.Recs) > 0 {
				hd.TopHero = dec.Recs[0].HeroID
				hd.TopName = label(dec.Recs[0].HeroID)
				hd.TopWhy = dec.Recs[0].Why
				sc := dec.Recs[0].Score
				hd.TopScore = &sc
			}
			for _, p := range dec.EnemyPred {
				hd.EnemyPred = append(hd.EnemyPred, historyPred{
					HeroID: p.HeroID,
					Name:   label(p.HeroID),
					Prob:   p.Prob,
					Source: p.Source,
				})
			}
			detail.Decisions = append(detail.Decisions, hd)
		}
		if d.Final != nil {
			b := historyBoardOf(*d.Final)
			detail.Final = &b
		}
		resp.Found = true
		resp.Draft = detail
		if len(sum.ProvenanceGap) > 0 || sum.Outcome == nil {
			resp.Evidence = "partial"
			resp.Note = "Драфт найден. Отсутствующие поля provenance/исхода не заполняются заглушками."
		} else {
			resp.Evidence = "ready"
			resp.Note = "Драфт из локального журнала."
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Evidence = "empty"
	resp.Note = "Драфт с таким id не найден в журнале."
	writeJSON(w, http.StatusOK, resp)
}

func summarizeDraft(d declog.Draft) historyDraftSummary {
	sum := historyDraftSummary{
		ID:            d.ID,
		TS:            d.TS,
		OurTeam:       d.Header.OurTeam,
		FirstPick:     d.Header.FirstPick,
		Source:        d.Header.Source,
		BundleVersion: d.Header.BundleVersion,
		PatchTag:      d.Header.PatchTag,
		SnapshotHash:  d.Header.SnapshotHash,
		EnemyCount:    len(d.Header.Enemies),
		AllyCount:     len(d.Header.Allies),
		DecisionCount: len(d.Decisions),
		FinalPresent:  d.Final != nil,
	}
	for _, dec := range d.Decisions {
		switch dec.ChosenRank {
		case 1:
			sum.TookTop++
		case 0:
			sum.OffRec++
		}
		if dec.Reason != "" {
			sum.WithReason++
		}
	}
	if d.Outcome != nil {
		sum.Outcome = &historyOutcome{
			Win:     d.Outcome.Win,
			MatchID: d.Outcome.MatchID,
			Note:    d.Outcome.Note,
		}
	}
	var gaps []string
	if d.Header.BundleVersion == "" {
		gaps = append(gaps, "bundleVersion")
	}
	if d.Header.PatchTag == "" {
		gaps = append(gaps, "patchTag")
	}
	if d.Header.SnapshotHash == "" {
		gaps = append(gaps, "snapshotHash")
	}
	if d.Outcome == nil {
		gaps = append(gaps, "outcome")
	}
	sum.ProvenanceGap = gaps
	return sum
}

func anyPartialHistory(drafts []historyDraftSummary) bool {
	for _, d := range drafts {
		if len(d.ProvenanceGap) > 0 {
			return true
		}
	}
	return false
}

func historyBoardOf(b declog.Board) historyBoard {
	return historyBoard{
		RadiantPicks: append([]int(nil), b.RadiantPicks...),
		DirePicks:    append([]int(nil), b.DirePicks...),
		RadiantBans:  append([]int(nil), b.RadiantBans...),
		DireBans:     append([]int(nil), b.DireBans...),
	}
}

func (s *Server) heroLabelFn() func(int) string {
	if sess := s.sess; sess != nil {
		heroes := sess.Config().Heroes
		return func(id int) string {
			for _, h := range heroes {
				if h.ID == id {
					return h.Label()
				}
			}
			return strconv.Itoa(id)
		}
	}
	return func(id int) string { return strconv.Itoa(id) }
}
