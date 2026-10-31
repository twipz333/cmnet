package web

import (
	"fmt"
	"net/http"
	"strings"

	"cmnet/internal/gsi"
)

// routeGSI registers install/status endpoints used by the native Settings GSI tab.
func (s *Server) routeGSI(mux *http.ServeMux) {
	mux.HandleFunc("/api/gsi/status", s.handleGSIStatus)
	mux.HandleFunc("/api/gsi/install", s.mutate(s.doGSIInstall))
}

type gsiStatusResponse struct {
	CfgDir       string   `json:"cfgDir"`
	CfgPath      string   `json:"cfgPath"`
	CfgFileName  string   `json:"cfgFileName"`
	Detected     bool     `json:"detected"`
	Installed    bool     `json:"installed"`
	DetectError  string   `json:"detectError,omitempty"`
	Candidates   []string `json:"candidates,omitempty"`
	GSIEnabled   bool     `json:"gsiEnabled"` // shell started with GSI pump
	GSIMode      bool     `json:"gsiMode"`    // session is read-only GSI board
	GSISeen      bool     `json:"gsiSeen"`    // at least one live event arrived
	DefaultAddr  string   `json:"defaultAddr"`
	Note         string   `json:"note"`
}

func (s *Server) handleGSIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfgDir := strings.TrimSpace(r.URL.Query().Get("cfgDir"))
	dir, path, detected, installed, detectErr := gsi.Inspect(cfgDir)
	resp := gsiStatusResponse{
		CfgDir:      dir,
		CfgPath:     path,
		CfgFileName: gsi.CfgFileName,
		Detected:    detected,
		Installed:   installed,
		Candidates:  gsi.CandidateCfgDirs(),
		GSIEnabled:  s.shell.GSI,
		DefaultAddr: "127.0.0.1:54001",
	}
	if detectErr != nil {
		resp.DetectError = detectErr.Error()
		resp.Detected = false
	}
	if sess := s.sess; sess != nil {
		resp.GSIMode = sess.GSIMode()
		resp.GSISeen = sess.GSISeen()
	}
	resp.Note = gsiStatusNote(resp)
	writeJSON(w, http.StatusOK, resp)
}

func gsiStatusNote(st gsiStatusResponse) string {
	switch {
	case !st.Installed && st.DetectError != "":
		return "Папка Dota не найдена автоматически. Укажи путь к gamestate_integration вручную и нажми «Установить»."
	case !st.Installed:
		return "Конфиг GSI ещё не записан. Установка кладёт файл в папку Dota; после этого перезапусти Dota."
	case !st.GSIEnabled:
		return "Конфиг на месте, но ядро запущено без GSI. Включи gsi: true в Настройках и перезапусти приложение."
	case st.GSIMode && !st.GSISeen:
		return "GSI включён, доска только для чтения. Ждём первое событие от Dota (запусти драфт / матч)."
	case st.GSIMode && st.GSISeen:
		return "Живой GSI: доска следует за Dota, ручной ввод отключён."
	default:
		return "Конфиг установлен. Ручной режим активен, пока GSI не включён в ядре."
	}
}

type gsiInstallRequest struct {
	CfgDir string `json:"cfgDir"`
	Addr   string `json:"addr"`
	Token  string `json:"token"`
}

func (s *Server) doGSIInstall(r *http.Request) (string, error) {
	var req gsiInstallRequest
	if err := decode(r, &req); err != nil {
		// Empty body is fine — autodect + defaults.
		req = gsiInstallRequest{}
	}
	addr := strings.TrimSpace(req.Addr)
	if addr == "" {
		addr = "127.0.0.1:54001"
	}
	path, err := gsi.Install(strings.TrimSpace(req.CfgDir), addr, strings.TrimSpace(req.Token))
	if err != nil {
		return "", fmt.Errorf("GSI install: %w", err)
	}
	note := fmt.Sprintf("GSI config written: %s — RESTART Dota if it was already running", path)
	if !s.shell.GSI {
		note += ". Enable gsi: true in Settings and restart CMNET to listen for live events"
	}
	return note, nil
}
