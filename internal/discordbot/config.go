package discordbot

import (
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the Discord bot process config (separate from advisor config.yaml).
type Config struct {
	Token        string     `yaml:"token"`
	GuildID      string     `yaml:"guild-id"`
	RoleID       string     `yaml:"role-id"`
	AdvisorURL   string     `yaml:"advisor-url"`
	AdvisorToken string     `yaml:"advisor-token"`
	NTTS         NTTSConfig `yaml:"ntts"`
}

// NTTSConfig drives speech synthesis and local cache.
type NTTSConfig struct {
	BaseURL  string            `yaml:"base-url"`
	Token    string            `yaml:"token"`
	Speaker  string            `yaml:"speaker"`
	Ext      string            `yaml:"ext"`
	Effect   string            `yaml:"effect"`
	CacheDir string            `yaml:"cache-dir"`
	Templates map[string]string `yaml:"templates"`
}

// LoadConfig reads path. Missing optional fields get defaults.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.AdvisorURL == "" {
		c.AdvisorURL = "http://127.0.0.1:54000"
	}
	if c.NTTS.BaseURL == "" {
		c.NTTS.BaseURL = "https://ntts.fdev.team"
	}
	if c.NTTS.Ext == "" {
		c.NTTS.Ext = "ogg"
	}
	if c.NTTS.CacheDir == "" {
		c.NTTS.CacheDir = "tts-cache"
	}
	if c.NTTS.Templates == nil {
		c.NTTS.Templates = map[string]string{}
	}
	if c.NTTS.Templates["default"] == "" {
		c.NTTS.Templates["default"] = "{{label}} через {{nextIn}} секунд"
	}
}

func (c *Config) validate() error {
	if c.Token == "" {
		return fmt.Errorf("token is required")
	}
	if c.GuildID == "" {
		return fmt.Errorf("guild-id is required")
	}
	if c.RoleID == "" {
		return fmt.Errorf("role-id is required")
	}
	if c.AdvisorToken == "" {
		return fmt.Errorf("advisor-token is required")
	}
	if c.NTTS.Token == "" {
		return fmt.Errorf("ntts.token is required")
	}
	if c.NTTS.Speaker == "" {
		return fmt.Errorf("ntts.speaker is required")
	}
	return nil
}

// LiveConfig is a hot-reloaded snapshot of Config.
type LiveConfig struct {
	mu   sync.RWMutex
	cfg  Config
	path string
	mod  time.Time
}

// NewLiveConfig watches path and reloads when mtime changes.
func NewLiveConfig(path string) (*LiveConfig, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &LiveConfig{cfg: *cfg, path: path, mod: st.ModTime()}, nil
}

// Get returns a copy of the current config.
func (l *LiveConfig) Get() Config {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cfg
}

// Poll reloads the file when its mtime advances. changed is true when NTTS
// voice/templates/effect/ext/token/base-url changed (cache should invalidate).
func (l *LiveConfig) Poll() (changed bool, err error) {
	st, err := os.Stat(l.path)
	if err != nil {
		return false, err
	}
	l.mu.RLock()
	same := !st.ModTime().After(l.mod)
	old := l.cfg
	l.mu.RUnlock()
	if same {
		return false, nil
	}
	cfg, err := LoadConfig(l.path)
	if err != nil {
		return false, err
	}
	l.mu.Lock()
	l.cfg = *cfg
	l.mod = st.ModTime()
	l.mu.Unlock()
	return nttsIdentity(old.NTTS) != nttsIdentity(cfg.NTTS), nil
}

func nttsIdentity(n NTTSConfig) string {
	return n.BaseURL + "\x00" + n.Token + "\x00" + n.Speaker + "\x00" + n.Ext + "\x00" + n.Effect + "\x00" + fmt.Sprint(n.Templates)
}