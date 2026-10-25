package discordbot

import (
	"strconv"
	"strings"
)

// AlertPhrase is the data available to TTS templates.
type AlertPhrase struct {
	Kind   string
	Label  string
	NextIn int
	NextAt int
}

// RenderTemplate substitutes {{kind}} {{label}} {{nextIn}} {{nextAt}}.
func RenderTemplate(tmpl string, a AlertPhrase) string {
	if tmpl == "" {
		tmpl = "{{label}} через {{nextIn}} секунд"
	}
	r := strings.NewReplacer(
		"{{kind}}", a.Kind,
		"{{label}}", a.Label,
		"{{nextIn}}", strconv.Itoa(a.NextIn),
		"{{nextAt}}", strconv.Itoa(a.NextAt),
	)
	return r.Replace(tmpl)
}

// TemplateFor picks a per-kind template or default.
func TemplateFor(templates map[string]string, kind string) string {
	if templates == nil {
		return "{{label}} через {{nextIn}} секунд"
	}
	if t := templates[kind]; t != "" {
		return t
	}
	if t := templates["default"]; t != "" {
		return t
	}
	return "{{label}} через {{nextIn}} секунд"
}