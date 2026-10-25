package discordbot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cmnet/internal/atomicfile"
	"gopkg.in/yaml.v3"
)

// EditorView is the schema-driven discord.yaml editor payload (same JSON shape
// as config.View so the web UI can reuse the form renderer).
type EditorView struct {
	Path     string          `json:"path"`
	Exists   bool            `json:"exists"`
	Sections []EditorSection `json:"sections"`
	Note     string          `json:"note"`
}

// EditorSection groups fields.
type EditorSection struct {
	Name   string        `json:"name"`
	Title  string        `json:"title"`
	Fields []EditorField `json:"fields"`
}

// EditorField is one editable key.
type EditorField struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Default string `json:"default"`
	Help    string `json:"help"`
	Secret  bool   `json:"secret"`
	Set     bool   `json:"set"`
	Value   any    `json:"value"`
}

// EditorSaveRequest is posted back from the UI.
type EditorSaveRequest struct {
	Sections []EditorSaveSection `json:"sections"`
}

// EditorSaveSection mirrors one YAML block / logical group.
type EditorSaveSection struct {
	Name   string            `json:"name"`
	Fields []EditorSaveField `json:"fields"`
}

// EditorSaveField is one value to write.
type EditorSaveField struct {
	Key   string `json:"key"`
	Set   bool   `json:"set"`
	Value any    `json:"value"`
}

// FindConfig resolves which discord.yaml to edit and returns an absolute path.
// Order: explicit flag → ./discord.yaml → next to the executable → create ./discord.yaml.
func FindConfig(explicit string) string {
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
	const name = "discord.yaml"
	if _, err := os.Stat(name); err == nil {
		return resolve(name)
	}
	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(beside); err == nil {
			return resolve(beside)
		}
	}
	// Prefer creating in the working directory (where the user usually keeps the repo),
	// not silently next to the .exe.
	return resolve(name)
}

// LoadLoose reads discord.yaml without requiring tokens to be filled in
// (so the web editor can open a half-filled template).
func LoadLoose(path string) (*Config, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		c := &Config{}
		c.applyDefaults()
		return c, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	c.applyDefaults()
	return &c, true, nil
}

type fieldSpec struct {
	section string
	key     string
	typ     string
	def     string
	help    string
	secret  bool
	get     func(*Config) string
	set     func(*Config, string)
}

func editorSpecs() []fieldSpec {
	return []fieldSpec{
		{"", "token", "string", "", "Discord bot token (без префикса Bot)", true,
			func(c *Config) string { return c.Token },
			func(c *Config, v string) { c.Token = v }},
		{"", "guild-id", "string", "", "ID Discord-сервера", false,
			func(c *Config) string { return c.GuildID },
			func(c *Config, v string) { c.GuildID = v }},
		{"", "role-id", "string", "", "ID роли, которой разрешены slash-команды", false,
			func(c *Config) string { return c.RoleID },
			func(c *Config, v string) { c.RoleID = v }},
		{"", "advisor-url", "string", "http://127.0.0.1:54000", "URL локального advisor", false,
			func(c *Config) string { return c.AdvisorURL },
			func(c *Config, v string) { c.AdvisorURL = v }},
		{"", "advisor-token", "string", "", "Общий секрет с advisor remote-token (любая строка, НЕ токен STRATZ/NTTS/Discord)", true,
			func(c *Config) string { return c.AdvisorToken },
			func(c *Config, v string) { c.AdvisorToken = v }},

		{"ntts", "base-url", "string", "https://ntts.fdev.team", "Базовый URL NTTS API", false,
			func(c *Config) string { return c.NTTS.BaseURL },
			func(c *Config, v string) { c.NTTS.BaseURL = v }},
		{"ntts", "token", "string", "", "Bearer-токен NTTS", true,
			func(c *Config) string { return c.NTTS.Token },
			func(c *Config, v string) { c.NTTS.Token = v }},
		{"ntts", "speaker", "string", "father_grigori", "ID голоса из /api/v1/tts/speakers", false,
			func(c *Config) string { return c.NTTS.Speaker },
			func(c *Config, v string) { c.NTTS.Speaker = v }},
		{"ntts", "ext", "string", "ogg", "NTTS format: ogg (Vorbis) or wav; bot re-encodes to Opus for Discord", false,
			func(c *Config) string { return c.NTTS.Ext },
			func(c *Config, v string) { c.NTTS.Ext = v }},
		{"ntts", "effect", "string", "", "Опциональный эффект NTTS", false,
			func(c *Config) string { return c.NTTS.Effect },
			func(c *Config, v string) { c.NTTS.Effect = v }},
		{"ntts", "cache-dir", "string", "tts-cache", "Локальный кэш озвучки", false,
			func(c *Config) string { return c.NTTS.CacheDir },
			func(c *Config, v string) { c.NTTS.CacheDir = v }},

		{"templates", "default", "string", "{{label}} через {{nextIn}} секунд", "Шаблон по умолчанию. Плейсхолдеры: {{kind}} {{label}} {{nextIn}} {{nextAt}}", false,
			func(c *Config) string { return c.NTTS.Templates["default"] },
			func(c *Config, v string) { setTemplate(c, "default", v) }},
		{"templates", "power", "string", "Силовые руны через {{nextIn}}", "Озвучка power runes", false,
			func(c *Config) string { return c.NTTS.Templates["power"] },
			func(c *Config, v string) { setTemplate(c, "power", v) }},
		{"templates", "bounty", "string", "Баунти через {{nextIn}}", "Озвучка bounty runes", false,
			func(c *Config) string { return c.NTTS.Templates["bounty"] },
			func(c *Config, v string) { setTemplate(c, "bounty", v) }},
		{"templates", "wisdom", "string", "Wisdom через {{nextIn}}", "Озвучка wisdom runes", false,
			func(c *Config) string { return c.NTTS.Templates["wisdom"] },
			func(c *Config, v string) { setTemplate(c, "wisdom", v) }},
		{"templates", "lotus", "string", "Лотусы через {{nextIn}}", "Озвучка lotus pools", false,
			func(c *Config) string { return c.NTTS.Templates["lotus"] },
			func(c *Config, v string) { setTemplate(c, "lotus", v) }},
		{"templates", "tormentor", "string", "Торментор через {{nextIn}}", "Озвучка tormentor", false,
			func(c *Config) string { return c.NTTS.Templates["tormentor"] },
			func(c *Config, v string) { setTemplate(c, "tormentor", v) }},
		{"templates", "daynight", "string", "Смена дня и ночи через {{nextIn}}", "Озвучка day/night", false,
			func(c *Config) string { return c.NTTS.Templates["daynight"] },
			func(c *Config, v string) { setTemplate(c, "daynight", v) }},
	}
}

func setTemplate(c *Config, key, v string) {
	if c.NTTS.Templates == nil {
		c.NTTS.Templates = map[string]string{}
	}
	if v == "" {
		delete(c.NTTS.Templates, key)
		return
	}
	c.NTTS.Templates[key] = v
}

// BuildEditorView builds the form model for path.
func BuildEditorView(path string) (EditorView, error) {
	if path == "" {
		path = FindConfig("")
	}
	cfg, exists, err := LoadLoose(path)
	if err != nil {
		return EditorView{}, err
	}
	v := EditorView{
		Path:   path,
		Exists: exists,
		Note:   "Сохранение пишет discord.yaml. advisor-token — общий секрет с remote-control (если remote-token в advisor пуст, берётся отсюда). Бот подхватывает голос/шаблоны без перезапуска.",
	}
	bySec := map[string]*EditorSection{}
	title := map[string]string{
		"":          "Discord",
		"ntts":      "NTTS",
		"templates": "Шаблоны озвучки",
	}
	order := []string{"", "ntts", "templates"}
	for _, name := range order {
		bySec[name] = &EditorSection{Name: name, Title: title[name]}
	}
	for _, sp := range editorSpecs() {
		val := sp.get(cfg)
		f := EditorField{
			Key:     sp.key,
			Type:    sp.typ,
			Default: sp.def,
			Help:    sp.help,
			Secret:  sp.secret,
			Set:     true,
			Value:   val,
		}
		bySec[sp.section].Fields = append(bySec[sp.section].Fields, f)
	}
	for _, name := range order {
		v.Sections = append(v.Sections, *bySec[name])
	}
	return v, nil
}

// SaveEditor writes the editor payload to path (creates the file if needed).
func SaveEditor(path string, req EditorSaveRequest) error {
	if path == "" {
		path = FindConfig("")
	}
	cfg, _, err := LoadLoose(path)
	if err != nil {
		return err
	}
	specs := editorSpecs()
	index := map[string]fieldSpec{}
	for _, sp := range specs {
		index[sp.section+"\x00"+sp.key] = sp
	}
	for _, sec := range req.Sections {
		for _, f := range sec.Fields {
			sp, ok := index[sec.Name+"\x00"+f.Key]
			if !ok {
				continue
			}
			if !f.Set {
				sp.set(cfg, sp.def)
				continue
			}
			sp.set(cfg, fmt.Sprint(f.Value))
		}
	}
	cfg.applyDefaults()
	return writeConfigFile(path, cfg)
}

func writeConfigFile(path string, c *Config) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// Stable template key order in YAML.
	type tmplKV struct{ K, V string }
	var tmpls []tmplKV
	for k, v := range c.NTTS.Templates {
		tmpls = append(tmpls, tmplKV{k, v})
	}
	sort.Slice(tmpls, func(i, j int) bool { return tmpls[i].K < tmpls[j].K })
	tmplMap := map[string]string{}
	for _, kv := range tmpls {
		tmplMap[kv.K] = kv.V
	}
	c.NTTS.Templates = tmplMap

	doc := map[string]any{
		"token":          c.Token,
		"guild-id":       c.GuildID,
		"role-id":        c.RoleID,
		"advisor-url":    c.AdvisorURL,
		"advisor-token":  c.AdvisorToken,
		"ntts": map[string]any{
			"base-url":  c.NTTS.BaseURL,
			"token":     c.NTTS.Token,
			"speaker":   c.NTTS.Speaker,
			"ext":       c.NTTS.Ext,
			"effect":    c.NTTS.Effect,
			"cache-dir": c.NTTS.CacheDir,
			"templates": c.NTTS.Templates,
		},
	}
	var body []byte
	body = append(body, []byte("# discord.yaml — edited from advisor web UI\n")...)
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	body = append(body, raw...)
	return atomicfile.WriteWithBackup(path, body, 0o600)
}
// AdvisorRemoteToken returns discord.yaml advisor-token for use as advisor
// --remote-token when the flag/config value is empty. Empty string if unset.
func AdvisorRemoteToken(discordConfigPath string) string {
	path := FindConfig(discordConfigPath)
	cfg, _, err := LoadLoose(path)
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.AdvisorToken)
}
