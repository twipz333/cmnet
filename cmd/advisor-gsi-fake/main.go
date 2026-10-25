package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmnet/internal/objectives"
)

func main() {
	uri := flag.String("uri", "http://127.0.0.1:54001/", "advisor GSI endpoint (POST)")
	token := flag.String("token", "", "auth.token; must match advisor --gsi-token when set")
	hz := flag.Float64("hz", 2, "POST rate while playing (updates per second)")
	onceClock := flag.String("clock", "", "with --once: set clock and POST once (sec or mm:ss)")
	once := flag.Bool("once", false, "POST current/default state once and exit")
	scenario := flag.String("scenario", "", "non-interactive: alerts | draft")
	flag.Parse()

	s := newSim(*uri, *token, *hz)
	s.resetGame(0)

	switch {
	case *scenario != "":
		if err := s.runScenario(*scenario); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case *once:
		if *onceClock != "" {
			sec, err := parseClock(*onceClock)
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			s.setClock(sec)
		}
		if err := s.send(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Println(s.statusLine())
	default:
		if err := s.repl(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
}

type teamDraft struct {
	Picks []int
	Bans  []int
}

type sim struct {
	uri   string
	token string
	hz    float64
	http  *http.Client

	mu       sync.Mutex
	clock    int
	paused   bool
	playing  bool
	speed    float64
	state    string
	team     string
	hasDraft bool
	radiant  teamDraft
	dire     teamDraft
	matchID  string
	active   int
	isPick   bool

	stopTick chan struct{}
}

func newSim(uri, token string, hz float64) *sim {
	if hz <= 0 {
		hz = 2
	}
	return &sim{
		uri:      uri,
		token:    token,
		hz:       hz,
		http:     &http.Client{Timeout: 3 * time.Second},
		speed:    1,
		state:    "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS",
		team:     "radiant",
		matchID:  "9000000001",
		active:   2,
		isPick:   true,
		stopTick: make(chan struct{}),
	}
}

func (s *sim) resetGame(clock int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = clock
	s.paused = false
	s.state = "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS"
	s.hasDraft = false
	s.radiant = teamDraft{}
	s.dire = teamDraft{}
}

func (s *sim) setClock(sec int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = sec
}

func (s *sim) getClock() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clock
}

func (s *sim) repl() error {
	fmt.Println("advisor-gsi-fake — fake Dota GSI POSTs to local advisor")
	fmt.Println("Type help. Target:", s.uri)
	_ = s.send()
	fmt.Println(s.statusLine())

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("gsi> ")
		if !in.Scan() {
			s.stopPlay()
			return in.Err()
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if err := s.exec(line); err != nil {
			fmt.Println("!", err)
			continue
		}
		if line == "quit" || line == "exit" || line == "q" {
			return nil
		}
	}
}

func (s *sim) exec(line string) error {
	parts := strings.Fields(line)
	cmd := strings.ToLower(parts[0])
	arg := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}

	switch cmd {
	case "help", "h", "?":
		fmt.Print(helpText)
		return nil
	case "status", "st":
		fmt.Println(s.statusLine())
		return nil
	case "send":
		if err := s.send(); err != nil {
			return err
		}
		fmt.Println("posted", s.statusLine())
		return nil
	case "quit", "exit", "q":
		s.stopPlay()
		return nil
	case "clock", "jump":
		sec, err := parseClock(arg(1))
		if err != nil {
			return err
		}
		s.setClock(sec)
		return s.sendPrint()
	case "+":
		n, err := strconv.Atoi(arg(1))
		if err != nil {
			return fmt.Errorf("usage: + SECONDS")
		}
		s.mu.Lock()
		s.clock += n
		s.mu.Unlock()
		return s.sendPrint()
	case "-":
		n, err := strconv.Atoi(arg(1))
		if err != nil {
			return fmt.Errorf("usage: - SECONDS")
		}
		s.mu.Lock()
		s.clock -= n
		s.mu.Unlock()
		return s.sendPrint()
	case "play":
		speed := s.speed
		if arg(1) != "" {
			v, err := strconv.ParseFloat(strings.TrimSuffix(arg(1), "x"), 64)
			if err != nil || v <= 0 {
				return fmt.Errorf("usage: play [speed]")
			}
			speed = v
		}
		s.startPlay(speed)
		fmt.Printf("playing at %.1fx from %s\n", speed, fmtClock(s.getClock()))
		return nil
	case "pause":
		s.stopPlay()
		fmt.Println("play paused (local ticker)")
		return nil
	case "speed":
		v, err := strconv.ParseFloat(strings.TrimSuffix(arg(1), "x"), 64)
		if err != nil || v <= 0 {
			return fmt.Errorf("usage: speed N")
		}
		s.mu.Lock()
		s.speed = v
		s.mu.Unlock()
		fmt.Printf("speed=%.1fx\n", v)
		return nil
	case "state":
		switch strings.ToLower(arg(1)) {
		case "draft", "hero", "hero_selection":
			s.mu.Lock()
			s.state = "DOTA_GAMERULES_STATE_HERO_SELECTION"
			s.hasDraft = true
			s.mu.Unlock()
		case "game", "ingame":
			s.mu.Lock()
			s.state = "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS"
			s.mu.Unlock()
		case "pregame":
			s.mu.Lock()
			s.state = "DOTA_GAMERULES_STATE_PRE_GAME"
			s.mu.Unlock()
		case "postgame", "post":
			s.mu.Lock()
			s.state = "DOTA_GAMERULES_STATE_POST_GAME"
			s.mu.Unlock()
		default:
			return fmt.Errorf("usage: state draft|game|pregame|postgame")
		}
		return s.sendPrint()
	case "team":
		t := strings.ToLower(arg(1))
		if t != "radiant" && t != "dire" {
			return fmt.Errorf("usage: team radiant|dire")
		}
		s.mu.Lock()
		s.team = t
		s.mu.Unlock()
		return s.sendPrint()
	case "map-pause":
		s.mu.Lock()
		s.paused = true
		s.mu.Unlock()
		return s.sendPrint()
	case "map-unpause":
		s.mu.Lock()
		s.paused = false
		s.mu.Unlock()
		return s.sendPrint()
	case "pick", "ban":
		side := strings.ToLower(arg(1))
		id, err := strconv.Atoi(arg(2))
		if err != nil || (side != "radiant" && side != "dire") || id <= 0 {
			return fmt.Errorf("usage: %s radiant|dire HERO_ID", cmd)
		}
		s.mu.Lock()
		s.hasDraft = true
		s.state = "DOTA_GAMERULES_STATE_HERO_SELECTION"
		td := &s.radiant
		if side == "dire" {
			td = &s.dire
		}
		if cmd == "pick" {
			td.Picks = append(td.Picks, id)
			s.isPick = true
		} else {
			td.Bans = append(td.Bans, id)
			s.isPick = false
		}
		if side == "radiant" {
			s.active = 2
		} else {
			s.active = 3
		}
		s.mu.Unlock()
		return s.sendPrint()
	case "clear-draft":
		s.mu.Lock()
		s.radiant = teamDraft{}
		s.dire = teamDraft{}
		s.mu.Unlock()
		return s.sendPrint()
	case "load":
		switch strings.ToLower(arg(1)) {
		case "draft":
			s.loadSampleDraft()
		case "game":
			s.resetGame(0)
		default:
			return fmt.Errorf("usage: load draft|game")
		}
		return s.sendPrint()
	case "scenario":
		return s.runScenario(arg(1))
	case "activeteam":
		switch strings.ToLower(arg(1)) {
		case "radiant", "2":
			s.mu.Lock()
			s.active = 2
			s.mu.Unlock()
		case "dire", "3":
			s.mu.Lock()
			s.active = 3
			s.mu.Unlock()
		default:
			return fmt.Errorf("usage: activeteam radiant|dire")
		}
		return s.sendPrint()
	case "turn":
		switch strings.ToLower(arg(1)) {
		case "pick":
			s.mu.Lock()
			s.isPick = true
			s.mu.Unlock()
		case "ban":
			s.mu.Lock()
			s.isPick = false
			s.mu.Unlock()
		default:
			return fmt.Errorf("usage: turn pick|ban")
		}
		return s.sendPrint()
	default:
		if strings.HasPrefix(cmd, "+") && len(cmd) > 1 {
			n, err := strconv.Atoi(cmd[1:])
			if err != nil {
				return fmt.Errorf("unknown command %q (help)", parts[0])
			}
			s.mu.Lock()
			s.clock += n
			s.mu.Unlock()
			return s.sendPrint()
		}
		if strings.HasPrefix(cmd, "-") && len(cmd) > 1 {
			n, err := strconv.Atoi(cmd[1:])
			if err != nil {
				return fmt.Errorf("unknown command %q (help)", parts[0])
			}
			s.mu.Lock()
			s.clock -= n
			s.mu.Unlock()
			return s.sendPrint()
		}
		return fmt.Errorf("unknown command %q (help)", parts[0])
	}
}

func (s *sim) sendPrint() error {
	if err := s.send(); err != nil {
		return err
	}
	fmt.Println(s.statusLine())
	return nil
}

func (s *sim) startPlay(speed float64) {
	s.stopPlay()
	s.mu.Lock()
	s.speed = speed
	s.playing = true
	s.stopTick = make(chan struct{})
	stop := s.stopTick
	hz := s.hz
	s.mu.Unlock()

	go func() {
		interval := time.Duration(float64(time.Second) / hz)
		if interval < 50*time.Millisecond {
			interval = 50 * time.Millisecond
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		last := time.Now()
		acc := 0.0
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				s.mu.Lock()
				if s.paused {
					last = now
					s.mu.Unlock()
					continue
				}
				dt := now.Sub(last).Seconds() * s.speed
				last = now
				acc += dt
				steps := int(acc)
				if steps > 0 {
					acc -= float64(steps)
					s.clock += steps
					s.mu.Unlock()
					_ = s.send()
					continue
				}
				s.mu.Unlock()
			}
		}
	}()
}

func (s *sim) stopPlay() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.playing {
		return
	}
	s.playing = false
	close(s.stopTick)
}

func (s *sim) loadSampleDraft() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = -75
	s.paused = false
	s.state = "DOTA_GAMERULES_STATE_HERO_SELECTION"
	s.hasDraft = true
	s.radiant = teamDraft{Picks: []int{8}, Bans: []int{74, 41}}
	s.dire = teamDraft{Picks: []int{26}, Bans: []int{86}}
	s.active = 3
	s.isPick = false
	s.team = "dire"
}

func (s *sim) runScenario(name string) error {
	switch strings.ToLower(name) {
	case "alerts":
		fmt.Println("scenario alerts: walk clock through lead windows (default lead 15s)")
		s.stopPlay()
		s.resetGame(0)
		targets := []int{0, 180, 300, 360, 420, 1200}
		seen := map[int]bool{}
		for _, at := range targets {
			if seen[at] {
				continue
			}
			seen[at] = true
			start := at - 16
			if start < -10 {
				start = -10
			}
			fmt.Printf("-- approaching spawn %s\n", fmtClock(at))
			for c := start; c <= at+1; c++ {
				s.setClock(c)
				if err := s.send(); err != nil {
					return err
				}
				time.Sleep(120 * time.Millisecond)
			}
		}
		fmt.Println("scenario alerts done")
		fmt.Println(s.statusLine())
		return nil
	case "draft":
		fmt.Println("scenario draft: sample CM board + a few actions")
		s.stopPlay()
		s.loadSampleDraft()
		if err := s.send(); err != nil {
			return err
		}
		fmt.Println(s.statusLine())
		steps := []string{
			"ban radiant 11",
			"ban dire 1",
			"pick radiant 74",
			"pick dire 2",
			"activeteam radiant",
			"turn pick",
		}
		for _, step := range steps {
			fmt.Println(">>", step)
			if err := s.exec(step); err != nil {
				return err
			}
			time.Sleep(300 * time.Millisecond)
		}
		fmt.Println("scenario draft done")
		return nil
	default:
		return fmt.Errorf("unknown scenario %q (alerts|draft)", name)
	}
}

func (s *sim) statusLine() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	play := "stop"
	if s.playing {
		play = fmt.Sprintf("play %.1fx", s.speed)
	}
	draft := "no-draft"
	if s.hasDraft {
		draft = fmt.Sprintf("R p%v b%v / D p%v b%v active=%d pick=%v",
			s.radiant.Picks, s.radiant.Bans, s.dire.Picks, s.dire.Bans, s.active, s.isPick)
	}
	due := ""
	alerts := objectives.Upcoming(objectives.DefaultObjectives(), s.clock, objectives.Leads{}, objectives.Windows{})
	var parts []string
	for _, a := range alerts {
		if a.Due {
			parts = append(parts, fmt.Sprintf("%s in %ds", a.Kind, a.NextIn))
		}
	}
	if len(parts) > 0 {
		due = " | DUE " + strings.Join(parts, ", ")
	}
	return fmt.Sprintf("clock=%s paused=%v team=%s %s | %s | %s%s",
		fmtClock(s.clock), s.paused, s.team, shortState(s.state), play, draft, due)
}

func (s *sim) send() error {
	body, err := s.buildJSON()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.uri, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", s.uri, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("POST %s: HTTP %s", s.uri, res.Status)
	}
	return nil
}

func (s *sim) buildJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clock := s.clock
	daytime := true
	if clock >= 0 {
		daytime = ((clock / 300) % 2) == 0
	}
	payload := map[string]any{
		"provider": map[string]any{
			"name": "Dota 2", "appid": 570, "version": 47,
			"timestamp": time.Now().Unix(),
		},
		"map": map[string]any{
			"name":          "start",
			"matchid":       s.matchID,
			"game_time":     maxInt(clock, 0),
			"clock_time":    clock,
			"daytime":       daytime,
			"game_state":    s.state,
			"paused":        s.paused,
			"win_team":      "none",
			"radiant_score": 0,
			"dire_score":    0,
		},
		"player": map[string]any{
			"team_name": s.team,
			"name":      "gsi-fake",
			"activity":  "playing",
			"kills":     0, "deaths": 0, "assists": 0,
			"last_hits": 0, "denies": 0,
			"gold": 600, "gpm": 0, "xpm": 0, "net_worth": 600,
		},
		"hero": map[string]any{
			"id": 0, "name": "", "level": 0, "alive": true,
			"respawn_seconds": 0, "buyback_cost": 0, "buyback_cooldown": 0,
		},
	}
	if s.token != "" {
		payload["auth"] = map[string]any{"token": s.token}
	}
	if s.hasDraft {
		payload["draft"] = map[string]any{
			"activeteam":                s.active,
			"pick":                      s.isPick,
			"activeteam_time_remaining": 25,
			"radiant_bonus_time":        130,
			"dire_bonus_time":           130,
			"team2":                     teamBlock(s.radiant),
			"team3":                     teamBlock(s.dire),
		}
	}
	return json.Marshal(payload)
}

func teamBlock(t teamDraft) map[string]any {
	m := map[string]any{"home_team": false}
	for i, id := range t.Picks {
		m[fmt.Sprintf("pick%d_id", i)] = id
	}
	for i, id := range t.Bans {
		m[fmt.Sprintf("ban%d_id", i)] = id
	}
	return m
}

func parseClock(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty clock")
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) != 2 {
			return 0, fmt.Errorf("bad clock %q", s)
		}
		m, err1 := strconv.Atoi(parts[0])
		sec, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || sec < 0 || sec > 59 {
			return 0, fmt.Errorf("bad clock %q", s)
		}
		out := m*60 + sec
		if neg {
			out = -out
		}
		return out, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bad clock %q", s)
	}
	if neg {
		n = -n
	}
	return n, nil
}

func fmtClock(sec int) string {
	sign := ""
	if sec < 0 {
		sign = "-"
		sec = -sec
	}
	return fmt.Sprintf("%s%d:%02d", sign, sec/60, sec%60)
}

func shortState(st string) string {
	st = strings.TrimPrefix(st, "DOTA_GAMERULES_STATE_")
	return strings.ToLower(st)
}

const helpText = `Commands:
  status                 show clock / draft / due alerts
  send                   POST current payload
  clock|jump TIME        set clock (seconds or mm:ss)
  +N / -N                shift clock
  play [speed]           auto-tick clock (e.g. play 10)
  pause                  stop auto-tick
  speed N                change play speed
  state draft|game|...   set map.game_state
  team radiant|dire      local player side
  map-pause|map-unpause  map.paused
  pick|ban SIDE HERO_ID  append draft action
  clear-draft            empty picks/bans
  load draft|game        sample boards
  activeteam radiant|dire
  turn pick|ban
  scenario alerts|draft  scripted sequences for DS/TTS testing
  quit
`

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
