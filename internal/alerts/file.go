// Package alerts holds local alert preferences: built-in objective leads/windows,
// TTS templates, and user-defined game-clock alerts. The file is hot-reloaded so
// the web Alerts tab applies without restarting advisor.
package alerts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cmnet/internal/atomicfile"
	"cmnet/internal/objectives"

	"gopkg.in/yaml.v3"
)

// DefaultName is the alerts settings file next to the binary / working dir.
const DefaultName = "alerts.yaml"

// KindSettings is per-objective preference (lead, active window, TTS template).
type KindSettings struct {
	Lead  int    `yaml:"lead" json:"lead"`   // 0 = inherit default; -1 = off; >0 override
	From  int    `yaml:"from" json:"from"`   // -1 = no lower bound
	Until int    `yaml:"until" json:"until"` // 0 = no upper bound
	TTS   string `yaml:"tts" json:"tts"`     // optional template; empty → default-tts
}

// Custom is a one-shot (or repeating once per match) game-clock alert.
type Custom struct {
	ID      string `yaml:"id" json:"id"`
	At      int    `yaml:"at" json:"at"` // game-clock seconds of the event
	Label   string `yaml:"label" json:"label"`
	TTS     string `yaml:"tts" json:"tts"`   // template; {{nextIn}} {{label}} {{nextAt}} supported
	Lead    int    `yaml:"lead" json:"lead"` // 0 = inherit DefaultLead
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// File is the on-disk alerts.yaml shape.
type File struct {
	DefaultLead int                     `yaml:"default-lead" json:"defaultLead"`
	DefaultTTS  string                  `yaml:"default-tts" json:"defaultTts"`
	Kinds       map[string]KindSettings `yaml:"kinds" json:"kinds"`
	Custom      []Custom                `yaml:"custom" json:"custom"`
}

// View is what the web Alerts tab edits and displays.
type View struct {
	Path        string     `json:"path"`
	DefaultLead int        `json:"defaultLead"`
	DefaultTTS  string     `json:"defaultTts"`
	Kinds       []KindView `json:"kinds"`
	Custom      []Custom   `json:"custom"`
}

// KindView is one built-in objective row for the editor.
type KindView struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Lead  int    `json:"lead"`
	From  int    `json:"from"`
	Until int    `json:"until"`
	TTS   string `json:"tts"`
	First int    `json:"firstSpawn"`
	Every int    `json:"intervalSec"`
}

// SaveRequest is the PUT /api/alerts body.
type SaveRequest struct {
	DefaultLead int                     `json:"defaultLead"`
	DefaultTTS  string                  `json:"defaultTts"`
	Kinds       map[string]KindSettings `json:"kinds"`
	Custom      []Custom                `json:"custom"`
}

// Find resolves alerts.yaml: explicit → ./alerts.yaml → next to executable → ./alerts.yaml.
func Find(explicit string) string {
	resolve := func(p string) string {
		if p == "" {
			return p
		}
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	if explicit != "" {
		return resolve(explicit)
	}
	if _, err := os.Stat(DefaultName); err == nil {
		return resolve(DefaultName)
	}
	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), DefaultName)
		if _, err := os.Stat(beside); err == nil {
			return resolve(beside)
		}
	}
	return resolve(DefaultName)
}

// Load reads path. Missing file returns defaults seeded from the spawn table.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		f := DefaultFile()
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	f.applyDefaults()
	return &f, nil
}

// DefaultFile returns sensible empty settings (all kinds inherit default lead).
func DefaultFile() *File {
	f := &File{
		DefaultLead: objectives.DefaultLead,
		DefaultTTS:  "{{label}} через {{nextIn}} секунд",
		Kinds:       map[string]KindSettings{},
		Custom:      []Custom{},
	}
	for _, s := range objectives.DefaultObjectives() {
		f.Kinds[string(s.Kind)] = KindSettings{
			Lead:  0,
			From:  objectives.NoLowerBound,
			Until: 0,
		}
	}
	return f
}

func (f *File) applyDefaults() {
	if f.DefaultLead == 0 {
		f.DefaultLead = objectives.DefaultLead
	}
	if f.DefaultTTS == "" {
		f.DefaultTTS = "{{label}} через {{nextIn}} секунд"
	}
	if f.Kinds == nil {
		f.Kinds = map[string]KindSettings{}
	}
	if f.Custom == nil {
		f.Custom = []Custom{}
	}
	for _, s := range objectives.DefaultObjectives() {
		k := string(s.Kind)
		if _, ok := f.Kinds[k]; !ok {
			f.Kinds[k] = KindSettings{Lead: 0, From: objectives.NoLowerBound, Until: 0}
		}
	}
}

// SeedFromFlags overlays CLI/config.yaml lead+window values onto a default file.
func SeedFromFlags(leads objectives.Leads, windows objectives.Windows) *File {
	f := DefaultFile()
	if leads.Default != 0 {
		f.DefaultLead = leads.Default
	}
	if leads.ByKind != nil {
		for k, v := range leads.ByKind {
			ks := f.Kinds[string(k)]
			ks.Lead = v
			f.Kinds[string(k)] = ks
		}
	}
	if windows.ByKind != nil {
		for k, w := range windows.ByKind {
			ks := f.Kinds[string(k)]
			ks.From = w.From
			ks.Until = w.Until
			f.Kinds[string(k)] = ks
		}
	}
	return f
}

// ToLeads converts file settings into objectives.Leads.
func (f *File) ToLeads() objectives.Leads {
	l := objectives.Leads{Default: f.DefaultLead}
	m := map[objectives.Kind]int{}
	for k, ks := range f.Kinds {
		if ks.Lead != 0 {
			m[objectives.Kind(k)] = ks.Lead
		}
	}
	if len(m) > 0 {
		l.ByKind = m
	}
	return l
}

// ToWindows converts file settings into objectives.Windows.
func (f *File) ToWindows() objectives.Windows {
	m := map[objectives.Kind]objectives.Window{}
	for k, ks := range f.Kinds {
		if ks.From == objectives.NoLowerBound && ks.Until == 0 {
			continue
		}
		m[objectives.Kind(k)] = objectives.Window{From: ks.From, Until: ks.Until}
	}
	if len(m) == 0 {
		return objectives.Windows{}
	}
	return objectives.Windows{ByKind: m}
}

// TemplateFor returns the TTS template for a built-in kind.
func (f *File) TemplateFor(kind string) string {
	if ks, ok := f.Kinds[kind]; ok && strings.TrimSpace(ks.TTS) != "" {
		return ks.TTS
	}
	return f.DefaultTTS
}

// BuildView projects the file for the web editor.
func BuildView(path string, f *File) View {
	f.applyDefaults()
	kinds := make([]KindView, 0, len(objectives.DefaultObjectives()))
	for _, s := range objectives.DefaultObjectives() {
		ks := f.Kinds[string(s.Kind)]
		kinds = append(kinds, KindView{
			Kind:  string(s.Kind),
			Label: s.Label,
			Lead:  ks.Lead,
			From:  ks.From,
			Until: ks.Until,
			TTS:   ks.TTS,
			First: s.FirstSpawn,
			Every: s.IntervalSec,
		})
	}
	customs := append([]Custom(nil), f.Custom...)
	return View{
		Path:        path,
		DefaultLead: f.DefaultLead,
		DefaultTTS:  f.DefaultTTS,
		Kinds:       kinds,
		Custom:      customs,
	}
}

// ApplySave builds a File from a SaveRequest.
func ApplySave(req SaveRequest) *File {
	f := &File{
		DefaultLead: req.DefaultLead,
		DefaultTTS:  strings.TrimSpace(req.DefaultTTS),
		Kinds:       map[string]KindSettings{},
		Custom:      nil,
	}
	if req.Kinds != nil {
		for k, v := range req.Kinds {
			f.Kinds[k] = v
		}
	}
	for _, c := range req.Custom {
		c.ID = slugID(c.ID, c.Label)
		c.Label = strings.TrimSpace(c.Label)
		c.TTS = strings.TrimSpace(c.TTS)
		if c.Label == "" || c.At < 0 {
			continue
		}
		if c.TTS == "" {
			c.TTS = "{{label}} через {{nextIn}} секунд"
		}
		f.Custom = append(f.Custom, c)
	}
	f.applyDefaults()
	return f
}

func slugID(id, label string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	if id != "" {
		return sanitizeID(id)
	}
	return sanitizeID(label)
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "alert"
	}
	return out
}

// Save writes f to path atomically.
func Save(path string, f *File) error {
	f.applyDefaults()
	body, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	header := []byte("# alerts.yaml — objective leads, TTS phrases, custom game-clock alerts.\n" +
		"# Edited from the Alerts tab; hot-reloaded without restarting advisor.\n")
	return atomicfile.WriteWithBackup(path, append(header, body...), 0o600)
}

// EnsureFile loads path or seeds+writes from flags when missing.
func EnsureFile(path string, leads objectives.Leads, windows objectives.Windows) (*File, error) {
	if _, err := os.Stat(path); err == nil {
		return Load(path)
	}
	f := SeedFromFlags(leads, windows)
	if err := Save(path, f); err != nil {
		return f, err // still return usable defaults
	}
	return f, nil
}
