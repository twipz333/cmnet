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

	"cmnet/internal/draft"
	"cmnet/internal/share"
)

// shareDTO is the sharing panel: the host's open code, or the guest's last
// receive. Guest reports which app this is, so the page shows the right one.
type shareDTO struct {
	LocalOnly bool `json:"localOnly"`
	Guest     bool              `json:"guest"`
	CanHost   bool              `json:"canHost"`
	Host      *share.HostStatus `json:"host,omitempty"`
	Receiving bool              `json:"receiving"`
	LastCode  string            `json:"lastCode,omitempty"`
	Received  *receivedDTO      `json:"received,omitempty"`
	Error     string            `json:"error,omitempty"`

	// Host: whether the live draft goes out with the code, and how many guests
	// are following it right now.
	Live      bool `json:"live"`
	Followers int  `json:"followers"`

	// Guest: whether the board follows the host's live draft, and how that is
	// going: off | connecting | connected | waiting (host not streaming / gone).
	Follow      bool   `json:"follow"`
	FollowState string `json:"followState,omitempty"`
	FollowVia   string `json:"followVia,omitempty"`
}

type receivedDTO struct {
	At   time.Time  `json:"at"`
	Via  string     `json:"via"`
	Meta share.Meta `json:"meta"`
}

// guestFile remembers the last code and what came with it, so "get the fresh
// scouting" between rounds is one click, and the live draft resumes on start.
const guestFile = "guest.json"

// hostFile remembers whether the host shares the live draft.
const hostFile = "share-host.json"

type guestMemo struct {
	Code     string       `json:"code"`
	Received *receivedDTO `json:"received,omitempty"`
	// NoFollow: the guest switched off following the host's draft.
	NoFollow bool `json:"noFollow,omitempty"`
}

type hostMemo struct {
	Live bool `json:"live"`
}

type shareState struct {
	mu        sync.Mutex
	host      *share.Host
	receiving bool
	err       string
	memo      guestMemo
	hostPrefs hostMemo
	loaded    bool

	// host side of the live draft
	live       *share.LiveHost
	liveCancel context.CancelFunc

	// guest side of the live draft
	followCancel context.CancelFunc
	followState  string
	followVia    string
	lastFrame    *draft.Snapshot
}

func (st *shareState) loadLocked() {
	if st.loaded {
		return
	}
	st.loaded = true
	if data, err := os.ReadFile(guestFile); err == nil {
		_ = json.Unmarshal(data, &st.memo)
	}
	if data, err := os.ReadFile(hostFile); err == nil {
		_ = json.Unmarshal(data, &st.hostPrefs)
	}
}

func saveJSON(path string, v any) {
	if data, err := json.MarshalIndent(v, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

func (s *Server) shareView() shareDTO {
	st := &s.shareSt
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loadLocked()
	out := shareDTO{LocalOnly: s.localOnly, Guest: s.shell.Guest, CanHost: s.shell.SharePayload != nil && !s.localOnly,
		Receiving: st.receiving, Error: st.err, Live: st.hostPrefs.Live}
	if s.shell.Guest {
		out.LastCode = st.memo.Code
		out.Received = st.memo.Received
		out.Follow = st.followCancel != nil
		out.FollowState = st.followState
		out.FollowVia = st.followVia
		out.Live = st.memo.Received != nil && st.memo.Received.Meta.Live
	}
	if st.host != nil {
		hs := st.host.Status()
		out.Host = &hs
	}
	if st.live != nil {
		out.Followers = st.live.Guests()
	}
	return out
}

func (s *Server) routeShare(mux *http.ServeMux) {
	mux.HandleFunc("/api/share/start", s.mutate(s.shareStart))
	mux.HandleFunc("/api/share/stop", s.mutate(s.shareStop))
	mux.HandleFunc("/api/share/mode", s.mutate(s.shareMode))
	mux.HandleFunc("/api/share/receive", s.mutate(s.shareReceive))
	mux.HandleFunc("/api/share/follow", s.mutate(s.shareFollow))
}

func (s *Server) lifetime() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.baseCtx == nil {
		return context.Background()
	}
	return s.baseCtx
}

// --- host -------------------------------------------------------------------

func (s *Server) sharePayload() (share.Payload, error) {
	p, err := s.shell.SharePayload()
	if err != nil {
		return p, err
	}
	st := &s.shareSt
	st.mu.Lock()
	p.Meta.Live = st.hostPrefs.Live
	st.mu.Unlock()
	return p, nil
}

func (s *Server) shareStart(r *http.Request) (string, error) {
	if s.localOnly {
		return "", errors.New("public-relay sharing disabled in local-only mode")
	}
	if s.shell.SharePayload == nil {
		return "", errors.New("в гостевой версии раздавать нельзя")
	}
	var req struct {
		Live *bool `json:"live"`
	}
	_ = decode(r, &req)
	if _, err := s.shell.SharePayload(); err != nil {
		return "", err // say so now rather than leave a code that serves nothing
	}
	st := &s.shareSt
	st.mu.Lock()
	st.loadLocked()
	if req.Live != nil {
		st.hostPrefs.Live = *req.Live
		saveJSON(hostFile, st.hostPrefs)
	}
	if st.host == nil {
		st.host = &share.Host{
			Transports: share.DefaultTransports(),
			Build:      s.sharePayload,
			Changed:    func() { s.broadcast(false) },
		}
	}
	h := st.host
	live := st.hostPrefs.Live
	st.mu.Unlock()
	hs := h.Start(s.lifetime())
	if live {
		s.startLive(hs.Code)
	}
	return "код для друга: " + hs.Code, nil
}

// shareMode turns the live draft on or off, also while a code is open.
func (s *Server) shareMode(r *http.Request) (string, error) {
	if s.localOnly { return "", errors.New("public-relay live mode disabled in local-only mode") }
	var req struct {
		Live bool `json:"live"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	st := &s.shareSt
	st.mu.Lock()
	st.loadLocked()
	st.hostPrefs.Live = req.Live
	saveJSON(hostFile, st.hostPrefs)
	h := st.host
	st.mu.Unlock()
	var code string
	if h != nil {
		if hs := h.Status(); hs.Active {
			code = hs.Code
		}
	}
	if req.Live && code != "" {
		s.startLive(code)
	}
	if !req.Live {
		s.stopLive()
	}
	s.shareRefresh() // guests who come next learn whether there is a live draft
	if req.Live {
		return "друзья увидят твой драфт вживую", nil
	}
	return "раздаются только модель и разведка", nil
}

func (s *Server) startLive(code string) {
	st := &s.shareSt
	st.mu.Lock()
	if st.live != nil {
		st.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.lifetime())
	lh := &share.LiveHost{Streamers: share.DefaultStreamers(), Changed: func() { s.broadcast(false) }}
	st.live, st.liveCancel = lh, cancel
	st.mu.Unlock()
	s.publishLive()
	go func() { _ = lh.Run(ctx, code) }()
}

func (s *Server) stopLive() {
	st := &s.shareSt
	st.mu.Lock()
	if st.liveCancel != nil {
		st.liveCancel()
	}
	st.live, st.liveCancel = nil, nil
	st.mu.Unlock()
}

// publishLive sends the current board to following guests. It runs on every
// broadcast; identical boards are dropped by the live host.
func (s *Server) publishLive() {
	st := &s.shareSt
	st.mu.Lock()
	lh := st.live
	st.mu.Unlock()
	if lh == nil {
		return
	}
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		return
	}
	snap := sess.BoardSnapshot()
	if data, err := json.Marshal(snap); err == nil {
		lh.Publish(data)
	}
}

func (s *Server) shareStop(*http.Request) (string, error) {
	st := &s.shareSt
	st.mu.Lock()
	h := st.host
	st.mu.Unlock()
	if h != nil {
		h.Stop()
	}
	s.stopLive()
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

// --- guest ------------------------------------------------------------------

const receiveTimeout = 2 * time.Minute

func (s *Server) shareReceive(r *http.Request) (string, error) {
	if s.localOnly {
		return "", errors.New("public-relay receiving disabled in local-only mode")
	}
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
	st.loadLocked()
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
	go s.receive(s.lifetime(), code)
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
	follow := false
	if err != nil {
		st.err = err.Error()
	} else {
		st.err = ""
		prevCode := st.memo.Code
		st.memo = guestMemo{Code: share.Format(code), Received: &receivedDTO{At: time.Now(), Via: via, Meta: p.Meta},
			NoFollow: st.memo.NoFollow && prevCode == share.Format(code)}
		saveJSON(guestFile, st.memo)
		follow = p.Meta.Live && !st.memo.NoFollow
	}
	st.mu.Unlock()
	if err != nil {
		s.setStatus(err.Error())
	} else {
		s.setStatus(msg)
		if follow {
			s.startFollow(code)
		} else if !p.Meta.Live {
			s.stopFollow()
		}
	}
	s.broadcast(false)
}

// shareFollow switches following the host's live draft on or off.
func (s *Server) shareFollow(r *http.Request) (string, error) {
	var req struct {
		On bool `json:"on"`
	}
	if err := decode(r, &req); err != nil {
		return "", err
	}
	st := &s.shareSt
	st.mu.Lock()
	st.loadLocked()
	code := st.memo.Code
	st.memo.NoFollow = !req.On
	saveJSON(guestFile, st.memo)
	st.mu.Unlock()
	if !req.On {
		s.stopFollow()
		return "драфт снова вводится вручную", nil
	}
	if code == "" {
		return "", errors.New("сначала получите модель от хоста")
	}
	s.startFollow(code)
	return "слежу за драфтом хоста", nil
}

// resumeFollow restarts following on app start when the guest left it on.
func (s *Server) resumeFollow() {
	if s.localOnly || !s.shell.Guest {
		return
	}
	st := &s.shareSt
	st.mu.Lock()
	st.loadLocked()
	m := st.memo
	st.mu.Unlock()
	if m.Code != "" && m.Received != nil && m.Received.Meta.Live && !m.NoFollow {
		s.startFollow(m.Code)
	}
}

func (s *Server) setFollowState(state, via string) {
	st := &s.shareSt
	st.mu.Lock()
	changed := st.followState != state || st.followVia != via
	st.followState, st.followVia = state, via
	st.mu.Unlock()
	if changed {
		s.broadcast(false)
	}
}

func (s *Server) startFollow(code string) {
	if s.localOnly { return } // prevents automatic relay reconnection
	st := &s.shareSt
	st.mu.Lock()
	if st.followCancel != nil {
		st.followCancel()
	}
	ctx, cancel := context.WithCancel(s.lifetime())
	st.followCancel = cancel
	st.followState = "connecting"
	st.mu.Unlock()

	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess != nil {
		sess.SetFollowing(true)
	}
	go func() {
		for ctx.Err() == nil {
			s.setFollowState("connecting", "")
			err := share.Follow(ctx, share.DefaultStreamers(), code,
				func(via string) { s.setFollowState("connected", via) },
				s.applyHostFrame)
			if ctx.Err() != nil {
				return
			}
			_ = err
			// The host closed cmnet, switched the live draft off, or the line
			// dropped: keep the last board and keep trying.
			s.setFollowState("waiting", "")
			sleep(ctx, 3*time.Second)
		}
	}()
	s.broadcast(false)
}

func (s *Server) stopFollow() {
	st := &s.shareSt
	st.mu.Lock()
	if st.followCancel != nil {
		st.followCancel()
	}
	st.followCancel = nil
	st.followState, st.followVia = "", ""
	st.lastFrame = nil
	st.mu.Unlock()
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess != nil {
		sess.SetFollowing(false)
	}
	s.broadcast(false)
}

// applyHostFrame puts one board from the host on the guest's screen.
func (s *Server) applyHostFrame(data []byte) {
	var snap draft.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	st := &s.shareSt
	st.mu.Lock()
	st.lastFrame = &snap
	st.mu.Unlock()
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		return
	}
	if !sess.Following() {
		sess.SetFollowing(true)
	}
	ring := sess.ApplyGSI(&snap)
	s.broadcast(ring)
}

// followIntoSession carries following over to a rebuilt session (a new model
// or new scouting arrived while the guest follows the host).
func (s *Server) followIntoSession() {
	st := &s.shareSt
	st.mu.Lock()
	on := st.followCancel != nil
	last := st.lastFrame
	st.mu.Unlock()
	if !on {
		return
	}
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		return
	}
	sess.SetFollowing(true)
	if last != nil {
		cp := *last
		sess.ApplyGSI(&cp)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
