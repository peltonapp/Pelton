package scripting

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// catalogKeys lists every localization key the built-in registry uses.
func catalogKeys(c Catalog) []string {
	seen := map[string]bool{}
	add := func(keys ...string) {
		for _, k := range keys {
			if k != "" {
				seen[k] = true
			}
		}
	}
	params := func(ps []Param) {
		for _, p := range ps {
			add(p.LabelKey)
			for _, o := range p.Options {
				add(o.LabelKey)
			}
		}
	}
	fields := func(fs []Field) {
		for _, f := range fs {
			add(f.LabelKey)
		}
	}
	for _, t := range c.Triggers {
		add(t.LabelKey, t.DescKey)
		params(t.Params)
		fields(t.Event)
	}
	for _, x := range c.Conditions {
		add(x.LabelKey, x.DescKey)
		params(x.Params)
	}
	for _, a := range c.Actions {
		add(a.LabelKey, a.DescKey)
		params(a.Params)
		fields(a.Outputs)
	}
	for _, m := range c.Modules {
		add(m.LabelKey, m.DescKey)
		for _, f := range m.Functions {
			add(f.DescKey)
			params(f.Params)
			fields(f.Returns)
		}
	}
	for _, v := range c.ValueTypes {
		add(v.LabelKey, v.DescKey)
		fields(v.Fields)
	}
	for _, p := range c.Permissions {
		add(p.LabelKey, p.DescKey)
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestBuiltinsAreLocalized checks that the English catalog has a string for
// every key the built-in registry uses, so no picker shows a raw key.
func TestBuiltinsAreLocalized(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "locales", "en.ts"))
	if err != nil {
		t.Skip("frontend catalog not found:", err)
	}
	text := string(data)
	var missing []string
	for _, k := range catalogKeys(testRegistry(t).Catalog()) {
		if !strings.Contains(text, "'"+k+"':") {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d keys missing from en.ts:\n%s", len(missing), strings.Join(missing, "\n"))
	}
}
