package scripting

import (
	_ "embed"
	"encoding/json"
	"slices"
	"sync"
)

// englishJSON holds the English text of the localization keys the built-in
// entries of this package and its subpackages use. It mirrors en.ts, which
// TestEnglishMatchesFrontend keeps it in step with, so an entry carries its
// English Label and Description for readers outside the frontend: the API
// reference and the tools an AI step is offered.
//
//go:embed english.json
var englishJSON []byte

var englishText = sync.OnceValue(func() map[string]string {
	out := map[string]string{}
	if err := json.Unmarshal(englishJSON, &out); err != nil {
		panic("scripting: english.json: " + err.Error())
	}
	return out
})

// englishFor returns the English text of a localization key, "" for none.
func englishFor(key string) string {
	if key == "" {
		return ""
	}
	return englishText()[key]
}

func englishMeta(m *Meta) {
	if m.Label == "" {
		m.Label = englishFor(m.LabelKey)
	}
	if m.Description == "" {
		m.Description = englishFor(m.DescKey)
	}
}

func englishParams(ps []Param) []Param {
	if ps == nil {
		return nil
	}
	out := slices.Clone(ps)
	for i := range out {
		p := &out[i]
		if p.Label == "" {
			p.Label = englishFor(p.LabelKey)
		}
		if p.Description == "" {
			p.Description = englishFor(p.DescKey)
		}
		if p.Options != nil {
			p.Options = slices.Clone(p.Options)
			for j := range p.Options {
				if p.Options[j].Label == "" {
					p.Options[j].Label = englishFor(p.Options[j].LabelKey)
				}
			}
		}
	}
	return out
}

func englishFields(fs []Field) []Field {
	if fs == nil {
		return nil
	}
	out := slices.Clone(fs)
	for i := range out {
		if out[i].Label == "" {
			out[i].Label = englishFor(out[i].LabelKey)
		}
	}
	return out
}
