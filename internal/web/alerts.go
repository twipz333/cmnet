package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"cmnet/internal/alerts"
	"cmnet/internal/discordbot"
)

func (s *Server) routeAlerts(mux *http.ServeMux) {
	mux.HandleFunc("/api/alerts", s.handleAlerts)
	mux.HandleFunc("/api/alerts/save", s.handleAlertsSave)
	mux.HandleFunc("/api/tts-cache", s.handleTTSCache)
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.shell.GetAlerts == nil {
		http.Error(w, "alerts editor is not available", http.StatusNotFound)
		return
	}
	view, err := s.shell.GetAlerts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var cache []discordbot.CacheEntry
	if s.shell.ListTTSCache != nil {
		cache, _ = s.shell.ListTTSCache()
	}
	var live any
	live = []alerts.Custom{} // placeholder type-stable empty
	if sess, err := s.session(); err == nil && sess != nil {
		live = sess.Alerts()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"alerts": view,
		"cache":  cache,
		"live":   live,
	})
}

func (s *Server) handleAlertsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.shell.SaveAlerts == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "alerts editor is not available"})
		return
	}
	var req alerts.SaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	msg, file, err := s.shell.SaveAlerts(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if file != nil {
		if sess, err := s.session(); err == nil && sess != nil {
			sess.SetAlertFile(file)
		}
	}
	s.setStatus(msg)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": msg, "state": s.snapshot(false)})
}

func (s *Server) handleTTSCache(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.shell.ListTTSCache == nil {
			http.Error(w, "tts cache is not available", http.StatusNotFound)
			return
		}
		entries, err := s.shell.ListTTSCache()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"cache": entries})
	case http.MethodDelete:
		key := strings.TrimSpace(r.URL.Query().Get("key"))
		if key == "" || key == "*" {
			if s.shell.ClearTTSCache == nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "clear not available"})
				return
			}
			msg, err := s.shell.ClearTTSCache()
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": msg})
			return
		}
		if s.shell.DeleteTTSCache == nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "delete not available"})
			return
		}
		msg, err := s.shell.DeleteTTSCache(key)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": msg})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
