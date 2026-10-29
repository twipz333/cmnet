package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cmnet/internal/share"
)

// shareDTO is the sharing panel: the host's open code, or the guest's last
// receive. Guest reports which app this is, so the page shows the right one.
type shareDTO struct {
	Guest     bool              `json:"guest"`
	CanHost   bool              `json:"canHost"`
	Host      *share.HostStatus `json:"host,omitempty"`
	Receiving bool              `json:"receiving"`
	LastCode  string            `json:"lastCode,omitempty"`
	Received  *receivedDTO      `json:"received,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type receivedDTO struct {
	At   time.Time  `json:"at"`
	Via  string     `json:"via"`
	Meta share.Meta `json:"meta"`
}

// guestFile remembers the last code and what came with it, so "get the fresh
// scouting" between rounds is one click.
const guestFile = "guest.json"

type guestMemo struct {
	Code     string       `json:"code"`
	Received *receivedDTO `json:"received,omitempty"`
}

type shareState struct {
	mu        sync.Mutex
	host      *share.Host
	receiving bool
	err       string
	memo      guestMemo
	loaded    bool
}

func (s *Server) shareView() shareDTO {
	st := &s.shareSt
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.loaded {
		st.loaded = true
		if data, err := os.ReadFile(guestFile); err == nil {
			_ = json.Unmarshal(data, &st.memo)
		}
	}
	out := shareDTO{Guest: s.shell.Guest, CanHost: s.shell.SharePayload != nil,
		Receiving: st.receiving, Error: st.err}
	if s.shell.Guest {
		out.LastCode = st.memo.Code
		out.Received = st.memo.Received
	}
	if st.host != nil {
		hs := st.host.Status()
		out.Host = &hs
	}
	return out
}

func (s *Server) routeShare(mux *http.ServeMux) {
	mux.HandleFunc("/api/share/start", s.mutate(s.shareStart))
	mux.HandleFunc("/api/share/stop", s.mutate(s.shareStop))
	mux.HandleFunc("/api/share/receive", s.mutate(s.shareReceive))
}

func (s *Server) shareStart(*http.Request) (string, error) {
	if s.shell.SharePayload == nil {
		return "", errors.New("в гостевой версии раздавать нельзя")
	}
	if _, err := s.shell.SharePayload(); err != nil {
		return "", err // say so now rather than leave a code that serves nothing
	}
	st := &s.shareSt
	st.mu.Lock()
	if st.host == nil {
		st.host = &share.Host{
			Transports: share.DefaultTransports(),
			Build:      s.shell.SharePayload,
			Changed:    func() { s.broadcast(false) },
		}
	}
	h := st.host
	st.mu.Unlock()
	s.mu.Lock()
	ctx := s.baseCtx
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	hs := h.Start(ctx)
	return "код для друга: " + hs.Code, nil
}

func (s *Server) shareStop(*http.Request) (string, error) {
	st := &s.shareSt
	st.mu.Lock()
	h := st.host
	st.mu.Unlock()
	if h != nil {
		h.Stop()
	}
	return "раздача закрыта", nil
}

// shareRefresh tells an open offer that the model or scouting changed.
func (s *Server) shareRefresh() {
	st := &s.shareSt
	st.mu.Lock()
	h := st.host
	st.mu.Unlock()
	if h != nil {
		h.Refresh()
	}
}

const receiveTimeout = 2 * time.Minute

func (s *Server) shareReceive(r *http.Request) (string, error) {
	if s.shell.ApplyShare == nil {
		return "", errors.New("получать от хоста умеет только гостевая версия")
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", errors.New("нужен код")
	}
	code := strings.TrimSpace(req.Code)
	st := &s.shareSt
	st.mu.Lock()
	if code == "" {
		code = st.memo.Code
	}
	if st.receiving {
		st.mu.Unlock()
		return "уже получаю", nil
	}
	if _, err := share.Normalize(code); err != nil {
		st.mu.Unlock()
		return "", err
	}
	st.receiving, st.err = true, ""
	st.mu.Unlock()

	s.mu.Lock()
	ctx := s.baseCtx
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	go s.receive(ctx, code)
	return "подключаюсь к хосту…", nil
}

func (s *Server) receive(ctx context.Context, code string) {
	p, via, err := share.Receive(ctx, share.DefaultTransports(), code, receiveTimeout)
	msg := ""
	if err == nil {
		msg, err = s.shell.ApplyShare(p)
	}
	if err == nil {
		// A rebuilt session starts from an empty board; that is the point (new
		// model, new opponent), and the guest pressed the button.
		if rerr := s.reloadSession(); rerr != nil {
			err = rerr
		}
	}
	st := &s.shareSt
	st.mu.Lock()
	st.receiving = false
	if err != nil {
		st.err = err.Error()
	} else {
		st.err = ""
		st.memo = guestMemo{Code: share.Format(code), Received: &receivedDTO{At: time.Now(), Via: via, Meta: p.Meta}}
		if data, jerr := json.MarshalIndent(st.memo, "", "  "); jerr == nil {
			_ = os.WriteFile(guestFile, data, 0o644)
		}
	}
	st.mu.Unlock()
	if err != nil {
		s.setStatus(err.Error())
	} else {
		s.setStatus(msg)
	}
	s.broadcast(false)
}
