package scripting

import "sort"

// LocaleKeys lists every localization key the catalog uses, sorted, so a
// package that registers entries can check its strings exist.
func (c Catalog) LocaleKeys() []string {
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
			add(p.LabelKey, p.DescKey)
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
