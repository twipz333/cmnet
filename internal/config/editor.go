package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"

	"cmnet/internal/atomicfile"
)

// View is the schema-driven config editor payload. Fields come from the live
// cobra flag tree, so a newly added flag appears here without a UI change.
type View struct {
	Path     string    `json:"path"`
	Exists   bool      `json:"exists"`
	Sections []Section `json:"sections"`
	Note     string    `json:"note"`
}

// Section is either the top-level block (Name=="") or one command section.
type Section struct {
	Name   string  `json:"name"`
	Title  string  `json:"title"`
	Fields []Field `json:"fields"`
}

// Field is one editable setting. Key is the flag name (and YAML key).
type Field struct {
	Key     string `json:"key"`
	Type    string `json:"type"` // bool|int|float|string|duration|stringSlice
	Default string `json:"default"`
	Help    string `json:"help"`
	Secret  bool   `json:"secret"`
	Set     bool   `json:"set"` // present in the file
	Value   any    `json:"value"`
}

// SaveRequest is what the UI posts back. Only keys that exist in the schema are
// written; unknown keys already in the file are preserved.
type SaveRequest struct {
	Sections []SaveSection `json:"sections"`
}

// SaveSection mirrors a YAML block.
type SaveSection struct {
	Name   string      `json:"name"`
	Fields []SaveField `json:"fields"`
}

// SaveField is one key. Set=false removes it from the file (fall back to default).
type SaveField struct {
	Key   string `json:"key"`
	Set   bool   `json:"set"`
	Value any    `json:"value"`
}

var skipFlagNames = map[string]bool{
	"help":    true,
	"version": true,
	"config":  true, // path of this file — editing it into itself is confusing
}

var skipCommandNames = map[string]bool{
	"help":       true,
	"completion": true,
}

// BuildView introspects root and merges current file values.
func BuildView(root *cobra.Command, path string) (View, error) {
	if root == nil {
		return View{}, fmt.Errorf("nil command root")
	}
	if path == "" {
		path = DefaultName
	}
	f, err := Load(path)
	if err != nil {
		return View{}, err
	}
	_, statErr := os.Stat(path)
	exists := statErr == nil

	v := View{
		Path:   path,
		Exists: exists,
		Note:   "Изменения пишутся в config.yaml. Большинство настроек применяется после перезапуска advisor.",
	}
	v.Sections = append(v.Sections, buildSection("", "Общие", collectFlags(root, true), f, true, root.Name()))
	subs := append([]*cobra.Command(nil), root.Commands()...)
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name() < subs[j].Name() })
	for _, sub := range subs {
		if skipCommandNames[sub.Name()] || sub.Hidden {
			continue
		}
		fields := collectFlags(sub, false)
		if len(fields) == 0 {
			continue
		}
		title := sub.Name()
		if s := strings.TrimSpace(sub.Short); s != "" {
			title = sub.Name() + " — " + s
		}
		v.Sections = append(v.Sections, buildSection(sub.Name(), title, fields, f, false, root.Name()))
	}
	return v, nil
}

type flagMeta struct {
	Name    string
	Type    string
	Default string
	Help    string
	Secret  bool
}

func collectFlags(cmd *cobra.Command, includePersistent bool) []flagMeta {
	seen := map[string]bool{}
	var out []flagMeta
	add := func(fs *pflag.FlagSet) {
		if fs == nil {
			return
		}
		fs.VisitAll(func(fl *pflag.Flag) {
			if fl.Hidden || skipFlagNames[fl.Name] || seen[fl.Name] {
				return
			}
			seen[fl.Name] = true
			out = append(out, flagMeta{
				Name:    fl.Name,
				Type:    flagType(fl),
				Default: fl.DefValue,
				Help:    fl.Usage,
				Secret:  isSecret(fl.Name),
			})
		})
	}
	add(cmd.LocalFlags())
	if includePersistent {
		add(cmd.PersistentFlags())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func flagType(fl *pflag.Flag) string {
	switch fl.Value.Type() {
	case "bool":
		return "bool"
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "count":
		return "int"
	case "float32", "float64":
		return "float"
	case "duration":
		return "duration"
	case "stringSlice", "stringArray", "intSlice", "float64Slice", "durationSlice":
		return "stringSlice"
	default:
		return "string"
	}
}

func isSecret(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "token") || strings.Contains(n, "password") || strings.Contains(n, "secret")
}

func buildSection(name, title string, flags []flagMeta, f *File, global bool, rootName string) Section {
	sec := Section{Name: name, Title: title}
	var fileMap map[string]any
	if f != nil {
		if global {
			// Root-command settings may live at the top level or under the root
			// command section (see config.Example's "advisor:" block). Section
			// values win on Apply, so they win on display too.
			fileMap = map[string]any{}
			for k, v := range f.Global {
				fileMap[k] = v
			}
			if rootName != "" && f.Commands != nil {
				for k, v := range f.Commands[rootName] {
					fileMap[k] = v
				}
			}
		} else if f.Commands != nil {
			fileMap = f.Commands[name]
		}
	}
	for _, fl := range flags {
		field := Field{
			Key:     fl.Name,
			Type:    fl.Type,
			Default: fl.Default,
			Help:    fl.Help,
			Secret:  fl.Secret,
			Value:   coerceDisplay(fl.Type, fl.Default),
		}
		if fileMap != nil {
			if raw, ok := fileMap[fl.Name]; ok {
				field.Set = true
				field.Value = normalizeStored(fl.Type, raw)
			}
		}
		sec.Fields = append(sec.Fields, field)
	}
	return sec
}

func coerceDisplay(typ, def string) any {
	switch typ {
	case "bool":
		b, _ := strconv.ParseBool(def)
		return b
	case "int":
		n, err := strconv.ParseInt(def, 10, 64)
		if err != nil {
			return def
		}
		return n
	case "float":
		n, err := strconv.ParseFloat(def, 64)
		if err != nil {
			return def
		}
		return n
	case "stringSlice":
		if def == "" || def == "[]" {
			return ""
		}
		return strings.Trim(def, "[]")
	default:
		return def
	}
}

func normalizeStored(typ string, raw any) any {
	switch typ {
	case "bool":
		switch t := raw.(type) {
		case bool:
			return t
		case string:
			b, _ := strconv.ParseBool(t)
			return b
		default:
			s, _ := toFlagValue(raw)
			b, _ := strconv.ParseBool(s)
			return b
		}
	case "int":
		switch t := raw.(type) {
		case int:
			return int64(t)
		case int64:
			return t
		case float64:
			return int64(t)
		default:
			s, _ := toFlagValue(raw)
			n, _ := strconv.ParseInt(s, 10, 64)
			return n
		}
	case "float":
		switch t := raw.(type) {
		case float64:
			return t
		case int:
			return float64(t)
		case int64:
			return float64(t)
		default:
			s, _ := toFlagValue(raw)
			n, _ := strconv.ParseFloat(s, 64)
			return n
		}
	case "stringSlice":
		switch t := raw.(type) {
		case []any:
			parts := make([]string, 0, len(t))
			for _, item := range t {
				s, _ := toFlagValue(item)
				parts = append(parts, s)
			}
			return strings.Join(parts, ",")
		case string:
			return t
		default:
			s, _ := toFlagValue(raw)
			return s
		}
	default:
		s, err := toFlagValue(raw)
		if err != nil {
			return fmt.Sprint(raw)
		}
		return s
	}
}

// SaveView writes the editor payload back to path. Schema-unknown keys already in
// the file are kept; Set=false drops a known key so the flag default applies.
func SaveView(root *cobra.Command, path string, req SaveRequest) error {
	if root == nil {
		return fmt.Errorf("nil command root")
	}
	if path == "" {
		path = DefaultName
	}
	allowed := schemaIndex(root)
	old, err := Load(path)
	if err != nil {
		return err
	}

	global := map[string]any{}
	commands := map[string]map[string]any{}
	if old != nil {
		for k, v := range old.Global {
			if !allowed["\x00"+k] {
				global[k] = v
			}
		}
		for sec, m := range old.Commands {
			keep := map[string]any{}
			for k, v := range m {
				if !allowed[sec+"\x00"+k] {
					keep[k] = v
				}
			}
			if len(keep) > 0 {
				commands[sec] = keep
			}
		}
	}

	for _, sec := range req.Sections {
		name := sec.Name
		for _, field := range sec.Fields {
			id := "\x00" + field.Key
			if name != "" {
				id = name + "\x00" + field.Key
			}
			if !allowed[id] {
				continue
			}
			if !field.Set {
				continue
			}
			val, err := coerceSave(allowedType(root, name, field.Key), field.Value)
			if err != nil {
				where := "top level"
				if name != "" {
					where = "section " + name
				}
				return fmt.Errorf("%s: %s: %w", where, field.Key, err)
			}
			if name == "" {
				global[field.Key] = val
				continue
			}
			if commands[name] == nil {
				commands[name] = map[string]any{}
			}
			commands[name][field.Key] = val
		}
	}

	if rootName := root.Name(); rootName != "" {
		if sec := commands[rootName]; sec != nil {
			for k := range sec {
				if allowed["\x00"+k] {
					delete(sec, k)
				}
			}
			if len(sec) == 0 {
				delete(commands, rootName)
			}
		}
	}

	return writeYAML(path, global, commands)
}

func schemaIndex(root *cobra.Command) map[string]bool {
	out := map[string]bool{}
	for _, fl := range collectFlags(root, true) {
		out["\x00"+fl.Name] = true
	}
	for _, sub := range root.Commands() {
		if skipCommandNames[sub.Name()] || sub.Hidden {
			continue
		}
		for _, fl := range collectFlags(sub, false) {
			out[sub.Name()+"\x00"+fl.Name] = true
		}
	}
	return out
}

func allowedType(root *cobra.Command, section, key string) string {
	var cmd *cobra.Command
	if section == "" {
		cmd = root
	} else {
		for _, sub := range root.Commands() {
			if sub.Name() == section {
				cmd = sub
				break
			}
		}
	}
	if cmd == nil {
		return "string"
	}
	fl := lookup(cmd, key)
	if fl == nil {
		return "string"
	}
	return flagType(fl)
}

func coerceSave(typ string, raw any) (any, error) {
	if raw == nil {
		return nil, fmt.Errorf("empty value")
	}
	switch typ {
	case "bool":
		switch t := raw.(type) {
		case bool:
			return t, nil
		case string:
			return strconv.ParseBool(t)
		default:
			s, err := toFlagValue(raw)
			if err != nil {
				return nil, err
			}
			return strconv.ParseBool(s)
		}
	case "int":
		switch t := raw.(type) {
		case int:
			return t, nil
		case int64:
			return int(t), nil
		case float64:
			if t != float64(int64(t)) {
				return nil, fmt.Errorf("want integer, got %v", t)
			}
			return int(t), nil
		case string:
			return strconv.Atoi(strings.TrimSpace(t))
		default:
			s, err := toFlagValue(raw)
			if err != nil {
				return nil, err
			}
			return strconv.Atoi(s)
		}
	case "float":
		switch t := raw.(type) {
		case float64:
			return t, nil
		case int:
			return float64(t), nil
		case int64:
			return float64(t), nil
		case string:
			return strconv.ParseFloat(strings.TrimSpace(t), 64)
		default:
			s, err := toFlagValue(raw)
			if err != nil {
				return nil, err
			}
			return strconv.ParseFloat(s, 64)
		}
	case "stringSlice":
		switch t := raw.(type) {
		case []any:
			parts := make([]any, 0, len(t))
			for _, item := range t {
				s, err := toFlagValue(item)
				if err != nil {
					return nil, err
				}
				if s = strings.TrimSpace(s); s != "" {
					parts = append(parts, s)
				}
			}
			return parts, nil
		case string:
			parts := splitCSV(t)
			out := make([]any, len(parts))
			for i, p := range parts {
				out[i] = p
			}
			return out, nil
		default:
			s, err := toFlagValue(raw)
			if err != nil {
				return nil, err
			}
			parts := splitCSV(s)
			out := make([]any, len(parts))
			for i, p := range parts {
				out[i] = p
			}
			return out, nil
		}
	default:
		switch t := raw.(type) {
		case string:
			return t, nil
		default:
			return toFlagValue(t)
		}
	}
}

func splitCSV(s string) []string {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func writeYAML(path string, global map[string]any, commands map[string]map[string]any) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	doc := map[string]any{}
	keys := make([]string, 0, len(global))
	for k := range global {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		doc[k] = global[k]
	}
	secs := make([]string, 0, len(commands))
	for k := range commands {
		secs = append(secs, k)
	}
	sort.Strings(secs)
	for _, name := range secs {
		sec := commands[name]
		inner := map[string]any{}
		ikeys := make([]string, 0, len(sec))
		for k := range sec {
			ikeys = append(ikeys, k)
		}
		sort.Strings(ikeys)
		for _, k := range ikeys {
			inner[k] = sec[k]
		}
		doc[name] = inner
	}

	var body strings.Builder
	body.WriteString("# advisor configuration (edited from the web UI).\n")
	body.WriteString("# Keys are flag names. CLI flags still win over this file.\n\n")
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	body.Write(data)
	return atomicfile.WriteWithBackup(path, []byte(body.String()), 0o644)
}