package discordbot

import (
	"log"
	"sync"
)

// Speaker turns due alerts into voice clips via NTTS + cache.
type Speaker struct {
	mu       sync.Mutex
	live     *LiveConfig
	cache    *AudioCache
	client   *NTTSClient
	voice    *Voice
	belledAt map[string]int // kind -> nextAt
}

func NewSpeaker(live *LiveConfig, cache *AudioCache, voice *Voice) *Speaker {
	cfg := live.Get()
	return &Speaker{
		live:     live,
		cache:    cache,
		client:   NewNTTSClient(cfg.NTTS.BaseURL, cfg.NTTS.Token),
		voice:    voice,
		belledAt: map[string]int{},
	}
}

// OnConfigChange refreshes the NTTS client and clears the audio cache.
func (s *Speaker) OnConfigChange() {
	cfg := s.live.Get()
	s.mu.Lock()
	s.client = NewNTTSClient(cfg.NTTS.BaseURL, cfg.NTTS.Token)
	s.mu.Unlock()
	if err := s.cache.Clear(); err != nil {
		log.Printf("discordbot: cache clear: %v", err)
	} else {
		log.Printf("discordbot: tts cache cleared after config change")
	}
}

// HandleState speaks newly-due alerts when connected to voice.
func (s *Speaker) HandleState(st *State) {
	if st == nil || !s.voice.Connected() {
		return
	}
	cfg := s.live.Get()
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	for _, a := range st.Alerts {
		if !a.Due {
			continue
		}
		s.mu.Lock()
		prev, ok := s.belledAt[a.Kind]
		if ok && prev == a.NextAt {
			s.mu.Unlock()
			continue
		}
		s.belledAt[a.Kind] = a.NextAt
		s.mu.Unlock()

		phrase := AlertPhrase{Kind: a.Kind, Label: a.Label, NextIn: a.NextIn, NextAt: a.NextAt}
		tmpl := a.TTS
		if tmpl == "" {
			tmpl = TemplateFor(cfg.NTTS.Templates, a.Kind)
		}
		text := RenderTemplate(tmpl, phrase)
		path, err := s.cache.Resolve(&cfg.NTTS, client, text)
		if err != nil {
			log.Printf("discordbot: tts %q: %v", text, err)
			continue
		}
		s.voice.Enqueue(path)
	}
}
