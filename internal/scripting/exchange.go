package scripting

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// FileExtension is the extension of an exported script.
const FileExtension = ".peltonscript"

// FileFormat is the version of the export format this package writes.
const FileFormat = 1

// maxFileBytes caps a script file read for import.
const maxFileBytes = 1 << 20

// File is a script as exported: readable JSON with what the script is and
// does, and nothing about who used it. Account ids, setting values, run
// history, storage and approvals stay behind.
type File struct {
	Format      int           `json:"format"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Kind        Kind          `json:"kind"`
	Trigger     TriggerConfig `json:"trigger"`
	Permissions []string      `json:"permissions"`
	Blocks      *Tree         `json:"blocks,omitempty"`
	Source      string        `json:"source,omitempty"`
	Settings    []Param       `json:"settings,omitempty"`
	CatchUp     CatchUpMode   `json:"catchUp,omitempty"`
	// Placements are where the script is offered in the app; a keyboard
	// shortcut's key stays behind.
	Placements []PlacementConfig `json:"placements,omitempty"`
	// Requires names the libraries the code loads with require, which an
	// import checks for.
	Requires []string `json:"requires,omitempty"`
}

// scriptRequires lists the libraries a script's own code requires: a Lua
// script's source, or the Lua blocks of a block script.
func scriptRequires(sc *Script) []string {
	if sc.Kind != KindBlocks {
		return RequiredLibraries(sc.Source)
	}
	if sc.Blocks == nil {
		return nil
	}
	var code strings.Builder
	walkBlocks(sc.Blocks.Blocks, func(b Block) {
		if b.Kind == BlockLua {
			code.WriteString(b.Code + "\n")
		}
	})
	return RequiredLibraries(code.String())
}

// fileRequires is what a file says it requires, with what its code requires
// in case the list was left out.
func fileRequires(f File) []string {
	out := slices.Clone(f.Requires)
	found := RequiredLibraries(f.Source)
	if f.Blocks != nil {
		found = append(found, scriptRequires(&Script{Kind: KindBlocks, Blocks: f.Blocks})...)
	}
	for _, n := range found {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func readFile(data []byte) (File, error) {
	if len(data) > maxFileBytes {
		return File{}, fmt.Errorf("scripting: the file is larger than %d bytes", maxFileBytes)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("scripting: not a script file: %w", err)
	}
	if f.Format == 0 {
		return File{}, fmt.Errorf("scripting: not a script file: no format version")
	}
	if f.Format > FileFormat {
		return File{}, fmt.Errorf("scripting: the file is format %d, newer than this app reads", f.Format)
	}
	return f, nil
}

// ReadFileInfo reads what a .peltonscript file holds without checking it
// against a registry: its kind (a script's, or KindLibrary) and the
// libraries it requires.
func ReadFileInfo(data []byte) (Kind, []string, error) {
	f, err := readFile(data)
	if err != nil {
		return "", nil, err
	}
	return f.Kind, fileRequires(f), nil
}

// Export renders a script as the text of a .peltonscript file.
func Export(r *Registry, sc *Script) ([]byte, error) {
	f := File{
		Format:      FileFormat,
		Name:        sc.Name,
		Description: sc.Description,
		Kind:        sc.Kind,
		Trigger:     TriggerConfig{Type: sc.Trigger.Type, Params: withoutPersonal(triggerParams(r, sc.Trigger.Type), sc.Trigger.Params)},
		Permissions: nonNil(sc.Permissions),
		Settings:    exportSettings(sc.SettingsSchema),
		CatchUp:     sc.CatchUp,
		Placements:  sc.Placements,
		Requires:    scriptRequires(sc),
	}
	if sc.Kind == KindBlocks && sc.Blocks != nil {
		t := scrubTree(r, *sc.Blocks)
		f.Blocks = &t
	} else {
		f.Source = sc.Source
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("scripting: export: %w", err)
	}
	return append(b, '\n'), nil
}

// ParseFile reads a .peltonscript file into a script that is ready to store:
// switched off, nothing approved, marked as imported. It checks the script
// against the registry but runs nothing.
func ParseFile(r *Registry, data []byte) (*Script, error) {
	f, err := readFile(data)
	if err != nil {
		return nil, err
	}
	if f.Kind == KindLibrary {
		return nil, fmt.Errorf("scripting: the file holds a library, not a script")
	}
	sc := &Script{
		Name:           f.Name,
		Description:    f.Description,
		Kind:           f.Kind,
		Source:         f.Source,
		Blocks:         f.Blocks,
		Trigger:        f.Trigger,
		Permissions:    f.Permissions,
		SettingsSchema: f.Settings,
		CatchUp:        f.CatchUp,
		Placements:     f.Placements,
		Origin:         OriginImport,
	}
	if sc.CatchUp == "" {
		sc.CatchUp = CatchUpLive
	}
	if sc.Blocks != nil && sc.Blocks.Version > TreeVersion {
		return nil, fmt.Errorf("scripting: the blocks are format %d, newer than this app reads", sc.Blocks.Version)
	}
	if err := sc.Validate(r); err != nil {
		return nil, err
	}
	return sc, nil
}

// Import stores a script read by ParseFile, switched off.
func (s *Service) Import(ctx context.Context, data []byte) (*Script, error) {
	sc, err := ParseFile(s.reg, data)
	if err != nil {
		return nil, err
	}
	return s.Create(ctx, sc)
}

// exportSettings is the settings schema with the defaults of account
// settings dropped, since an account id only means something here.
func exportSettings(schema []Param) []Param {
	out := make([]Param, 0, len(schema))
	for _, p := range schema {
		if p.Type == TypeAccount {
			p.Default = nil
		}
		out = append(out, p)
	}
	return out
}

func triggerParams(r *Registry, id string) []Param {
	t, _ := r.Trigger(id)
	return t.Params
}

// withoutPersonal drops the values of account parameters, which only mean
// something in the exporting install.
func withoutPersonal(params []Param, values map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range values {
		if slices.ContainsFunc(params, func(p Param) bool { return p.Name == k && p.Type == TypeAccount }) {
			continue
		}
		out[k] = v
	}
	return out
}

func withoutPersonalArgs(params []Param, args map[string]Arg) map[string]Arg {
	if args == nil {
		return nil
	}
	out := map[string]Arg{}
	for k, v := range args {
		if v.Ref == "" && slices.ContainsFunc(params, func(p Param) bool { return p.Name == k && p.Type == TypeAccount }) {
			continue
		}
		out[k] = v
	}
	return out
}

func scrubTree(r *Registry, t Tree) Tree {
	return Tree{Version: t.Version, Blocks: scrubBlocks(r, t.Blocks)}
}

func scrubBlocks(r *Registry, blocks []Block) []Block {
	if blocks == nil {
		return nil
	}
	out := make([]Block, len(blocks))
	for i, b := range blocks {
		if a, ok := r.Action(b.Type); ok && b.Kind == BlockAction {
			b.Args = withoutPersonalArgs(a.Params, b.Args)
		}
		if b.Branches != nil {
			branches := make([]Branch, len(b.Branches))
			for j, br := range b.Branches {
				branches[j] = Branch{When: scrubCond(r, br.When), Body: scrubBlocks(r, br.Body)}
			}
			b.Branches = branches
		}
		b.Else = scrubBlocks(r, b.Else)
		b.Body = scrubBlocks(r, b.Body)
		out[i] = b
	}
	return out
}

func scrubCond(r *Registry, c Cond) Cond {
	if def, ok := r.Condition(c.Type); ok && c.Kind == CondCondition {
		c.Args = withoutPersonalArgs(def.Params, c.Args)
	}
	if c.Items != nil {
		items := make([]Cond, len(c.Items))
		for i, item := range c.Items {
			items[i] = scrubCond(r, item)
		}
		c.Items = items
	}
	return c
}
