package scripting

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Kind says how a script is written.
type Kind string

// The script kinds. A block script compiles to Lua; a Lua script cannot turn
// back into blocks.
const (
	KindLua    Kind = "lua"
	KindBlocks Kind = "blocks"
)

// Origin says where a script came from. Imported and assistant scripts start
// disabled and say so.
type Origin string

// The origins.
const (
	OriginUser      Origin = "user"
	OriginImport    Origin = "import"
	OriginAssistant Origin = "assistant"
)

// CatchUpMode says what happens to mail that arrived while the app was
// closed, for a script started by mail arriving.
type CatchUpMode string

// The catch-up modes.
const (
	// CatchUpRun runs the script over that mail on start, oldest first, up
	// to the script's cap.
	CatchUpRun CatchUpMode = "run"
	// CatchUpLive ignores it.
	CatchUpLive CatchUpMode = "live"
	// CatchUpAsk asks the user on start.
	CatchUpAsk CatchUpMode = "ask"
)

// DefaultCatchUpCap is how many messages a catch-up runs over at most.
const DefaultCatchUpCap = 50

// TriggerConfig is the trigger a script is started by, with its parameters.
type TriggerConfig struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// Script is one automation.
type Script struct {
	ID          int64
	Name        string
	Description string
	Kind        Kind
	// Source is the Lua of a Lua script. A block script's Lua is compiled
	// from Blocks whenever it runs.
	Source  string
	Blocks  *Tree
	Trigger TriggerConfig
	// Permissions are those the script declares; Approved the subset the
	// user approved. Only approved ones work in a real run.
	Permissions []string
	Approved    []string
	Enabled     bool
	Origin      Origin
	// SettingsSchema declares the script's settings and Settings holds the
	// user's values for them.
	SettingsSchema []Param
	Settings       map[string]any
	CatchUp        CatchUpMode
	CatchUpCap     int
	// AllowUnasked lists the risky functions, by full Lua name, the user
	// lets this script call without a confirmation.
	AllowUnasked []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// EnabledAt is when the script was last switched on, and AliveAt the
	// last time the app was running with it on. Mail older than both is
	// never caught up on.
	EnabledAt time.Time
	AliveAt   time.Time
	LastRunAt time.Time
}

// Validate checks a script against the registry: a name, a known trigger, a
// kind with its code, declared permissions that exist, and a settings schema
// that is well formed.
func (s *Script) Validate(r *Registry) error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("scripting: a script needs a name")
	}
	t, ok := r.Trigger(s.Trigger.Type)
	if !ok {
		return fmt.Errorf("scripting: unknown trigger %q", s.Trigger.Type)
	}
	if t.Permission != "" && !slices.Contains(s.Permissions, t.Permission) {
		return fmt.Errorf("scripting: the trigger needs the permission %q", t.Permission)
	}
	switch s.Kind {
	case KindLua:
		if len(s.Source) > maxSourceBytes {
			return fmt.Errorf("scripting: the script is longer than %d bytes", maxSourceBytes)
		}
	case KindBlocks:
		if s.Blocks == nil {
			return fmt.Errorf("scripting: a block script needs blocks")
		}
	default:
		return fmt.Errorf("scripting: unknown script kind %q", s.Kind)
	}
	for _, p := range s.Permissions {
		if !r.ValidPermission(p) {
			return fmt.Errorf("scripting: unknown permission %q", p)
		}
	}
	switch s.CatchUp {
	case CatchUpRun, CatchUpLive, CatchUpAsk:
	default:
		return fmt.Errorf("scripting: unknown catch-up mode %q", s.CatchUp)
	}
	return r.checkParams("settings", s.SettingsSchema)
}

// granted reports whether perm works in this run: declared always, and
// approved too unless the run is a dry run. A base permission such as "net"
// is met by any targeted form of it, e.g. "net:example.org"; the function
// then checks its own target with Call.Require.
func (s *Script) granted(perm string, dry bool) bool {
	if perm == "" {
		return true
	}
	if !permIn(s.Permissions, perm) {
		return false
	}
	return dry || permIn(s.Approved, perm)
}

func permIn(list []string, perm string) bool {
	targeted := strings.Contains(perm, ":")
	return slices.ContainsFunc(list, func(p string) bool {
		if strings.EqualFold(p, perm) {
			return true
		}
		return !targeted && len(p) > len(perm) && p[len(perm)] == ':' && p[:len(perm)] == perm
	})
}

// Unapproved returns the declared permissions the user has not approved.
func (s *Script) Unapproved() []string {
	var out []string
	for _, p := range s.Permissions {
		if !slices.Contains(s.Approved, p) {
			out = append(out, p)
		}
	}
	return out
}

// setting returns a script setting, falling back to its declared default.
func (s *Script) setting(name string) (any, bool) {
	if v, ok := s.Settings[name]; ok {
		return v, true
	}
	for _, p := range s.SettingsSchema {
		if p.Name == name {
			return p.Default, p.Default != nil
		}
	}
	return nil, false
}

// Lua returns the code a run executes: the source of a Lua script, or the
// compiled blocks.
func (s *Script) Lua(r *Registry) (string, error) {
	if s.Kind == KindLua {
		return s.Source, nil
	}
	if s.Blocks == nil {
		return "", fmt.Errorf("scripting: a block script needs blocks")
	}
	res := Compile(r, *s.Blocks, s.Trigger.Type, true)
	if len(res.Errors) > 0 {
		return "", res.Errors[0]
	}
	return res.Lua, nil
}
