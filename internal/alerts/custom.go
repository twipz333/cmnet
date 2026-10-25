package alerts

import (
	"sort"

	"cmnet/internal/objectives"
)

// CustomKindPrefix prefixes user-defined alert kinds in SSE / Discord de-dup.
const CustomKindPrefix = "custom:"

// Upcoming merges built-in objective alerts with enabled custom game-clock alerts.
// TTS templates are attached so Discord can speak without reading alerts.yaml.
func Upcoming(f *File, specs []objectives.Spawn, clock int) []objectives.Alert {
	if f == nil {
		f = DefaultFile()
	}
	leads := f.ToLeads()
	windows := f.ToWindows()
	out := objectives.Upcoming(specs, clock, leads, windows)
	for i := range out {
		out[i].TTS = f.TemplateFor(string(out[i].Kind))
	}
	for _, c := range f.Custom {
		if !c.Enabled || c.At < 0 || c.Label == "" {
			continue
		}
		lead := c.Lead
		if lead == 0 {
			lead = f.DefaultLead
		}
		if lead < 0 {
			continue
		}
		if clock > c.At {
			continue // one-shot past
		}
		in := c.At - clock
		if in < 0 {
			in = 0
		}
		tts := c.TTS
		if tts == "" {
			tts = f.DefaultTTS
		}
		out = append(out, objectives.Alert{
			Kind:   objectives.Kind(CustomKindPrefix + c.ID),
			Label:  c.Label,
			NextAt: c.At,
			NextIn: in,
			Due:    in <= lead,
			TTS:    tts,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NextIn != out[j].NextIn {
			return out[i].NextIn < out[j].NextIn
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
