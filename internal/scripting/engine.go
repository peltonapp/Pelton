package scripting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// maxSourceBytes caps the Lua a script may hold.
const maxSourceBytes = 256 << 10

// Limits bound one run.
type Limits struct {
	// Timeout is the wall clock time a run may take, waiting on the user's
	// confirmation included.
	Timeout time.Duration
	// CallStack is the deepest Lua call nesting, and Registry and
	// RegistryMax the start and the cap of the Lua value stack.
	CallStack   int
	Registry    int
	RegistryMax int
	// LogBytes caps the text a run logs; later lines are dropped.
	LogBytes int
	// Steps caps the recorded steps, host calls included; a run that needs
	// more is stopped.
	Steps int
	// Memory is how far the heap may grow during a run before it is
	// stopped. The heap is shared with the app, so this is a backstop
	// against a runaway script rather than an exact budget. 0 turns it off.
	Memory uint64
}

// DefaultLimits are the limits of a normal run.
func DefaultLimits() Limits {
	return Limits{
		Timeout:     10 * time.Second,
		CallStack:   200,
		Registry:    4096,
		RegistryMax: 64 * 1024,
		LogBytes:    64 << 10,
		Steps:       1000,
		Memory:      256 << 20,
	}
}

// Status is how a run ended.
type Status string

// The run statuses.
const (
	StatusRunning Status = "running"
	StatusOK      Status = "ok"
	// StatusStopped means the script called stop().
	StatusStopped Status = "stopped"
	StatusError   Status = "error"
	// StatusDenied means a call needed a permission the script lacks, or the
	// user said no to a risky call.
	StatusDenied  Status = "denied"
	StatusTimeout Status = "timeout"
	// StatusLimit means the run hit a step or memory limit.
	StatusLimit Status = "limit"
	// StatusSkipped means the dispatcher did not start the run, e.g. to break
	// a loop of scripts triggering each other.
	StatusSkipped Status = "skipped"
	// StatusCancelled means the user cancelled the run or one of its
	// questions.
	StatusCancelled Status = "cancelled"
)

// StepKind says what a recorded step is.
type StepKind string

// The step kinds.
const (
	// StepCall is a host call that changed something.
	StepCall StepKind = "call"
	// StepPlanned is a change a dry run would have made.
	StepPlanned StepKind = "planned"
	// StepRead is a host call that read app data.
	StepRead StepKind = "read"
	// StepCheck is a block condition and its outcome.
	StepCheck StepKind = "check"
	StepLog   StepKind = "log"
	// StepDenied is a call refused for a missing permission, StepDeclined
	// one the user said no to.
	StepDenied   StepKind = "denied"
	StepDeclined StepKind = "declined"
	// StepAsk is a question put to the user and the answer.
	StepAsk StepKind = "ask"
)

// Step is one thing a run did, in order.
type Step struct {
	Kind StepKind `json:"kind"`
	// Block is the block that was running, for a block script.
	Block    string `json:"block,omitempty"`
	Function string `json:"function,omitempty"`
	Summary  string `json:"summary"`
	Level    string `json:"level,omitempty"`
	Error    string `json:"error,omitempty"`
	// Result is a check's outcome, and Branch the index of the if block's
	// branch whose condition it was.
	Result bool `json:"result,omitempty"`
	Branch *int `json:"branch,omitempty"`
	// AtMs is the time since the run started.
	AtMs int64 `json:"atMs"`
}

// Run is the record of one run.
type Run struct {
	ID       int64
	ScriptID int64
	Trigger  string
	Dry      bool
	Depth    int
	Started  time.Time
	Duration time.Duration
	Status   Status
	Error    string
	Steps    []Step
	// LogTruncated is set when the run logged more than the limit.
	LogTruncated bool
	// Event is the event table as the script left it, for a trigger whose
	// event the caller reads back.
	Event map[string]any
	// Cancelled is set when a script stopped the action that started it,
	// e.g. a send, with CancelReason.
	Cancelled    bool
	CancelReason string
	// ErrorLine is the line of a Lua script the run failed on, 0 when not
	// known, and ErrorBlock the block of a block script that was running.
	// ErrorLibrary names the library the failing line is in, when it is in
	// one; ErrorLine then counts in that library.
	ErrorLine    int
	ErrorBlock   string
	ErrorLibrary string
	// NotReached lists, for a block script, the blocks the run never got
	// to, in the order they stand.
	NotReached []string
}

// Changes counts the steps that changed something, planned ones included.
func (r *Run) Changes() int {
	n := 0
	for _, s := range r.Steps {
		if s.Kind == StepCall || s.Kind == StepPlanned {
			n++
		}
	}
	return n
}

// RunRequest asks the engine for one run.
type RunRequest struct {
	Script *Script
	// RunID is the run log id, 0 for a run that is not logged.
	RunID   int64
	Trigger string
	Event   map[string]any
	Dry     bool
	Depth   int
	// Attended is set when the user started the run, so its questions wait
	// for them rather than time out.
	Attended bool
	// OnStep, when set, is told about every step as it is recorded, and
	// OnWait when the run starts or stops waiting for the user.
	OnStep func(Step)
	OnWait func(waiting bool)
}

// RunInfo describes the run a host call belongs to. Hosts read it from the
// call's context, to group changes for undo or to carry the loop depth into
// events the call causes.
type RunInfo struct {
	RunID      int64
	ScriptID   int64
	ScriptName string
	Trigger    string
	Dry        bool
	Depth      int
}

type runInfoKey struct{}

// WithRunInfo returns ctx carrying info.
func WithRunInfo(ctx context.Context, info RunInfo) context.Context {
	return context.WithValue(ctx, runInfoKey{}, info)
}

// RunInfoFrom returns the run a context belongs to.
func RunInfoFrom(ctx context.Context) (RunInfo, bool) {
	info, ok := ctx.Value(runInfoKey{}).(RunInfo)
	return info, ok
}

// KVStore is the per script key value storage behind the store module.
type KVStore interface {
	KVGet(ctx context.Context, scriptID int64, key string) (string, bool, error)
	KVSet(ctx context.Context, scriptID int64, key, value string) error
	KVDelete(ctx context.Context, scriptID int64, key string) error
	KVKeys(ctx context.Context, scriptID int64) ([]string, error)
}

// Engine runs scripts. Every run gets a fresh Lua state with only the safe
// parts of the standard library and the registered modules.
type Engine struct {
	Registry *Registry
	Hosts    *Hosts
	KV       KVStore
	Limits   Limits
	// OnAllow is called when the user answers a confirmation with "allow
	// without asking", to remember that for the script.
	OnAllow func(scriptID int64, function string)
	// AskTimeout returns how long a question of a run nobody started waits;
	// DefaultAskTimeout when nil or 0. A script may ask for another per
	// question.
	AskTimeout func() time.Duration
	// Libraries is where require finds the user's libraries; with none,
	// require finds nothing.
	Libraries LibrarySource
}

// sandboxGlobals are the standard globals a run keeps. Everything else the
// base library defines (load, dofile, require, getfenv and so on) is removed.
var sandboxGlobals = []string{
	"assert", "error", "ipairs", "next", "pairs", "pcall", "rawequal", "rawget", "rawset",
	"select", "setmetatable", "getmetatable", "tonumber", "tostring", "type", "unpack", "xpcall",
	"_G", "_VERSION", "string", "table", "math",
}

// fatal is an error that ends the run whatever pcall says.
type fatal struct {
	status Status
	msg    string
}

func (f *fatal) Error() string { return f.msg }

type runState struct {
	e      *Engine
	req    RunRequest
	ctx    context.Context
	cancel context.CancelFunc
	start  time.Time
	limits Limits
	// deadline ends the run when its time budget runs out. The clock stops
	// while the user is asked to confirm a call: remaining is what is left
	// and clockFrom when the clock last started.
	deadline  *time.Timer
	remaining time.Duration
	clockFrom time.Time

	mu       sync.Mutex
	run      *Run
	fatal    *fatal
	logBytes int
	block    string
	reached  map[string]bool
	allowed  []string
	metas    map[string]*lua.LTable
	regexps  map[string]*regexpEntry
	// libs holds the libraries this run loaded, by name, and loading the
	// ones being loaded, innermost last.
	libs    map[string]lua.LValue
	loading []string
	// requirable holds the libraries the run may load: those named in the
	// script's code and in the libraries it loaded, which are the ones the
	// user approved with it.
	requirable map[string]bool
	counts     map[string]int
}

// Run executes one run and returns its record. It never panics and never
// outlives its timeout by more than a short grace period, even if a host
// call hangs.
func (e *Engine) Run(ctx context.Context, req RunRequest) *Run {
	limits := e.Limits
	if limits.Timeout <= 0 {
		limits = DefaultLimits()
	}
	run := &Run{
		ID:       req.RunID,
		ScriptID: req.Script.ID,
		Trigger:  req.Trigger,
		Dry:      req.Dry,
		Depth:    req.Depth,
		Started:  e.Hosts.now(),
		Status:   StatusRunning,
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx = WithRunInfo(ctx, RunInfo{
		RunID:      req.RunID,
		ScriptID:   req.Script.ID,
		ScriptName: req.Script.Name,
		Trigger:    req.Trigger,
		Dry:        req.Dry,
		Depth:      req.Depth,
	})
	st := &runState{
		e:       e,
		req:     req,
		ctx:     ctx,
		cancel:  cancel,
		start:   time.Now(),
		run:     run,
		allowed: slices.Clone(req.Script.AllowUnasked),
		regexps: map[string]*regexpEntry{},
		reached: map[string]bool{},
		libs:    map[string]lua.LValue{},
	}
	st.limits = limits
	st.remaining = limits.Timeout
	st.clockFrom = time.Now()
	st.deadline = time.AfterFunc(limits.Timeout, func() {
		st.setFatal(StatusTimeout, "the script took too long")
		cancel()
	})
	defer st.deadline.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if p := recover(); p != nil {
				st.setFatal(StatusError, fmt.Sprintf("internal error: %v", p))
			}
		}()
		st.execute()
	}()
	if limits.Memory > 0 {
		go st.watchMemory(done, limits.Memory)
	}

	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			st.ctxFatal()
		}
	}
	return st.finish()
}

// finish settles the status and returns a copy the caller may keep while a
// stuck run goroutine is still winding down. A failed run names the Lua line
// or the block it failed in; a block script's run lists the blocks it never
// got to.
func (st *runState) finish() *Run {
	st.mu.Lock()
	defer st.mu.Unlock()
	run := *st.run
	run.Steps = slices.Clone(st.run.Steps)
	run.Duration = time.Since(st.start)
	if st.fatal != nil {
		run.Status = st.fatal.status
		run.Error = st.fatal.msg
		if run.Status == StatusStopped || run.Status == StatusCancelled {
			run.Error = ""
		}
	} else if run.Status == StatusRunning {
		run.Status = StatusOK
	}
	sc := st.req.Script
	if run.Error != "" {
		if m := libraryErrorRE.FindStringSubmatch(run.Error); m != nil {
			run.ErrorLibrary = m[1]
			run.ErrorLine, _ = strconv.Atoi(m[2])
		} else if sc.Kind == KindBlocks {
			run.ErrorBlock = st.block
		} else {
			run.ErrorLine = errorLine(run.Error)
		}
	}
	if sc.Kind == KindBlocks && sc.Blocks != nil {
		run.NotReached = []string{}
		walkBlocks(sc.Blocks.Blocks, func(b Block) {
			if !st.reached[b.ID] {
				run.NotReached = append(run.NotReached, b.ID)
			}
		})
	}
	return &run
}

var errorLineRE = regexp.MustCompile(`^<string>:(\d+):`)

// errorLine reads the line a Lua error message starts with, 0 for none.
func errorLine(msg string) int {
	m := errorLineRE.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// ctxFatal ends the run for its context: cancelled by the user, or out of
// time.
func (st *runState) ctxFatal() *fatal {
	if errors.Is(context.Cause(st.ctx), ErrRunCancelled) {
		return st.setFatal(StatusCancelled, "cancelled")
	}
	return st.setFatal(StatusTimeout, "the script took too long")
}

func (st *runState) setWaiting(waiting bool) {
	if st.req.OnWait != nil {
		st.req.OnWait(waiting)
	}
}

func (st *runState) setFatal(status Status, msg string) *fatal {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.fatal == nil {
		st.fatal = &fatal{status: status, msg: msg}
	}
	return st.fatal
}

func (st *runState) failed() *fatal {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.fatal
}

// unwinding reports whether the run must end, so pcall cannot swallow it.
func (st *runState) unwinding() bool {
	if st.failed() != nil {
		return true
	}
	if err := st.ctx.Err(); err != nil {
		st.ctxFatal()
		return true
	}
	return false
}

func (st *runState) watchMemory(done <-chan struct{}, limit uint64) {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	read := func() uint64 {
		metrics.Read(sample)
		if sample[0].Value.Kind() != metrics.KindUint64 {
			return 0
		}
		return sample[0].Value.Uint64()
	}
	base := read()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			if now := read(); now > base && now-base > limit {
				st.setFatal(StatusLimit, "the script used too much memory")
				st.cancel()
				return
			}
		}
	}
}

func (st *runState) addStep(s Step) {
	st.mu.Lock()
	if len(st.run.Steps) >= st.limits.Steps {
		if st.fatal == nil {
			st.fatal = &fatal{status: StatusLimit, msg: fmt.Sprintf("the script took more than %d steps", st.limits.Steps)}
		}
		st.mu.Unlock()
		return
	}
	if s.Block == "" {
		s.Block = st.block
	}
	s.AtMs = time.Since(st.start).Milliseconds()
	st.run.Steps = append(st.run.Steps, s)
	st.mu.Unlock()
	if st.req.OnStep != nil {
		st.req.OnStep(s)
	}
}

// log records a line of the script's own output, within the log limit.
func (st *runState) log(level, text string) {
	st.mu.Lock()
	if st.logBytes+len(text) > st.limits.LogBytes {
		st.run.LogTruncated = true
		st.mu.Unlock()
		return
	}
	st.logBytes += len(text)
	st.mu.Unlock()
	st.addStep(Step{Kind: StepLog, Level: level, Summary: text})
}

func (st *runState) execute() {
	limits := st.limits
	code, err := st.req.Script.Lua(st.e.Registry)
	if err != nil {
		st.setFatal(StatusError, err.Error())
		return
	}
	if len(code) > maxSourceBytes {
		st.setFatal(StatusError, fmt.Sprintf("the script is longer than %d bytes", maxSourceBytes))
		return
	}
	st.requirable = map[string]bool{}
	for _, n := range RequiredLibraries(code) {
		st.requirable[n] = true
	}
	L := lua.NewState(lua.Options{
		SkipOpenLibs:        true,
		CallStackSize:       limits.CallStack,
		RegistrySize:        limits.Registry,
		RegistryMaxSize:     limits.RegistryMax,
		MinimizeStackMemory: true,
	})
	defer L.Close()
	L.SetContext(st.ctx)

	st.openSandbox(L)
	st.installModules(L)
	st.installGlobals(L)

	fn, err := L.LoadString(code)
	if err != nil {
		st.setFatal(StatusError, cleanLuaError(err))
		return
	}
	L.Push(fn)
	err = L.PCall(0, 0, nil)
	if f := st.failed(); f != nil {
		return
	}
	if err != nil {
		if st.ctx.Err() != nil {
			st.ctxFatal()
			return
		}
		st.setFatal(StatusError, cleanLuaError(err))
		return
	}
	if t, ok := st.e.Registry.Trigger(st.req.Trigger); ok && t.Mutable {
		if ev, ok := fromLua(L.GetGlobal("event")).(map[string]any); ok {
			kind := StepCall
			if st.req.Dry {
				kind = StepPlanned
			}
			for _, c := range eventChanges(st.req.Event, ev) {
				st.addStep(Step{Kind: kind, Function: "event." + c.path, Summary: c.summary()})
			}
			st.mu.Lock()
			st.run.Event = ev
			st.mu.Unlock()
		}
	}
}

// eventChange is one field a script changed in the event it was handed.
type eventChange struct {
	path          string
	before, after any
}

func (c eventChange) summary() string {
	return fmt.Sprintf("changed %s from %s to %s", c.path, short(c.before), short(c.after))
}

// eventChanges lists the fields of a mutable event a run changed, one level
// into each table, e.g. "draft.to", in name order.
func eventChanges(before, after map[string]any) []eventChange {
	same := func(a, b any) bool {
		if emptyValue(a) && emptyValue(b) {
			return true
		}
		x, _ := json.Marshal(plainValue(a))
		y, _ := json.Marshal(plainValue(b))
		return string(x) == string(y)
	}
	var out []eventChange
	for _, k := range sortedKeys(after) {
		was, _ := plainValue(before[k]).(map[string]any)
		now, ok := after[k].(map[string]any)
		if !ok || was == nil {
			if !same(before[k], after[k]) {
				out = append(out, eventChange{k, before[k], after[k]})
			}
			continue
		}
		keys := sortedKeys(now)
		for _, f := range sortedKeys(was) {
			if _, ok := now[f]; !ok {
				keys = append(keys, f)
			}
		}
		for _, f := range keys {
			if !same(was[f], now[f]) {
				out = append(out, eventChange{k + "." + f, was[f], now[f]})
			}
		}
	}
	return out
}

// emptyValue reports nil and empty lists and tables, which Lua does not tell
// apart.
func emptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		return len(x) == 0
	case []string:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// cleanLuaError keeps the message of a Lua error and drops the Go stack
// trace gopher-lua appends.
func cleanLuaError(err error) string {
	var apiErr *lua.ApiError
	msg := err.Error()
	if errors.As(err, &apiErr) && apiErr.Object != nil {
		msg = apiErr.Object.String()
	}
	if i := strings.Index(msg, "\nstack traceback:"); i >= 0 {
		msg = msg[:i]
	}
	return msg
}

func (st *runState) openSandbox(L *lua.LState) {
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.open))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	globals := L.Get(lua.GlobalsIndex).(*lua.LTable)
	var drop []lua.LValue
	globals.ForEach(func(k, _ lua.LValue) {
		if s, ok := k.(lua.LString); !ok || !slices.Contains(sandboxGlobals, string(s)) {
			drop = append(drop, k)
		}
	})
	for _, k := range drop {
		globals.RawSet(k, lua.LNil)
	}
	L.SetGlobal("pcall", L.NewFunction(st.pcall))
	L.SetGlobal("xpcall", L.NewFunction(st.xpcall))

	str := L.GetGlobal("string").(*lua.LTable)
	str.RawSetString("dump", lua.LNil)
	str.RawSetString("rep", L.NewFunction(strRep))
	str.RawSetString("format", L.NewFunction(strFormat(str.RawGetString("format"))))
	plainFind := str.RawGetString("find")
	str.RawSetString("find", L.NewFunction(func(L *lua.LState) int {
		if !L.ToBool(4) {
			L.RaiseError("string.find only searches plain text here (pass true as the 4th argument); use text.find for patterns")
		}
		top := L.GetTop()
		L.Push(plainFind)
		for i := 1; i <= top; i++ {
			L.Push(L.Get(i))
		}
		L.Call(top, lua.MultRet)
		return L.GetTop() - top
	}))
	for _, name := range []string{"match", "gmatch", "gfind", "gsub"} {
		alt := map[string]string{"match": "text.match", "gmatch": "text.find_all", "gfind": "text.find_all", "gsub": "text.replace"}[name]
		msg := fmt.Sprintf("string.%s uses Lua patterns, which scripts do not have; use %s, which takes a regular expression", name, alt)
		str.RawSetString(name, L.NewFunction(func(L *lua.LState) int {
			L.RaiseError("%s", msg)
			return 0
		}))
	}
}

// maxBuiltString caps strings built by string.rep and string.format.
const maxBuiltString = 1 << 20

func strRep(L *lua.LState) int {
	s := L.CheckString(1)
	n := L.CheckInt(2)
	sep := L.OptString(3, "")
	unit := len(s) + len(sep)
	if n <= 0 || unit == 0 {
		L.Push(lua.LString(""))
		return 1
	}
	if n > maxBuiltString/unit {
		L.RaiseError("string.rep would build more than %d bytes", maxBuiltString)
	}
	var b strings.Builder
	b.Grow(unit * n)
	for i := range n {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s)
	}
	L.Push(lua.LString(b.String()))
	return 1
}

func strFormat(orig lua.LValue) lua.LGFunction {
	return func(L *lua.LState) int {
		f := L.CheckString(1)
		size := len(f)
		for i := 0; i < len(f); i++ {
			if f[i] != '%' {
				continue
			}
			j := i + 1
			for j < len(f) && strings.IndexByte("-+ #0", f[j]) >= 0 {
				j++
			}
			digits, num := 0, 0
			for j < len(f) && (f[j] >= '0' && f[j] <= '9' || f[j] == '.') {
				if f[j] == '.' {
					size += num
					num = 0
				} else {
					digits++
					num = num*10 + int(f[j]-'0')
				}
				j++
			}
			if digits > 4 {
				L.RaiseError("string.format width or precision too large")
			}
			size += num + 32
			i = j
		}
		top := L.GetTop()
		for i := 2; i <= top; i++ {
			if s, ok := L.Get(i).(lua.LString); ok {
				size += len(s)
			}
		}
		if size > maxBuiltString {
			L.RaiseError("string.format would build more than %d bytes", maxBuiltString)
		}
		L.Push(orig)
		for i := 1; i <= top; i++ {
			L.Push(L.Get(i))
		}
		L.Call(top, 1)
		if out := L.Get(-1); len(lua.LVAsString(out)) > maxBuiltString {
			L.RaiseError("string.format would build more than %d bytes", maxBuiltString)
		}
		return 1
	}
}

// pcall is the standard pcall, except that it lets a fatal error (stop, a
// denied permission, a timeout or a limit) go on ending the run.
func (st *runState) pcall(L *lua.LState) int {
	n := L.GetTop()
	if n == 0 {
		L.ArgError(1, "value expected")
	}
	L.Push(L.Get(1))
	for i := 2; i <= n; i++ {
		L.Push(L.Get(i))
	}
	err := L.PCall(n-1, lua.MultRet, nil)
	if st.unwinding() {
		L.RaiseError("%s", st.failed().msg)
	}
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(errorValue(err))
		return 2
	}
	L.Insert(lua.LTrue, n+1)
	return L.GetTop() - n
}

func (st *runState) xpcall(L *lua.LState) int {
	fn := L.CheckFunction(1)
	handler := L.CheckFunction(2)
	top := L.GetTop()
	L.Push(fn)
	err := L.PCall(0, lua.MultRet, nil)
	if st.unwinding() {
		L.RaiseError("%s", st.failed().msg)
	}
	if err != nil {
		L.Push(handler)
		L.Push(errorValue(err))
		L.Call(1, 1)
		res := L.Get(-1)
		L.Push(lua.LFalse)
		L.Push(res)
		return 2
	}
	L.Insert(lua.LTrue, top+1)
	return L.GetTop() - top
}

func errorValue(err error) lua.LValue {
	var apiErr *lua.ApiError
	if errors.As(err, &apiErr) && apiErr.Object != nil {
		return apiErr.Object
	}
	return lua.LString(err.Error())
}

func (st *runState) installGlobals(L *lua.LState) {
	L.SetGlobal("event", toLua(L, st.req.Event, st.metatable(L)))
	info := L.NewTable()
	info.RawSetString("id", lua.LNumber(st.req.Script.ID))
	info.RawSetString("name", lua.LString(st.req.Script.Name))
	info.RawSetString("trigger", lua.LString(st.req.Trigger))
	info.RawSetString("dry_run", lua.LBool(st.req.Dry))
	L.SetGlobal("script", info)
	L.SetGlobal("stop", L.NewFunction(func(L *lua.LState) int {
		st.setFatal(StatusStopped, "stopped")
		L.RaiseError("stopped")
		return 0
	}))
	L.SetGlobal("require", L.NewFunction(st.require))
	L.SetGlobal("print", L.NewFunction(func(L *lua.LState) int {
		st.log("info", joinArgs(L))
		return 0
	}))
	L.SetGlobal("__block", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		st.mu.Lock()
		st.block = id
		st.reached[id] = true
		st.mu.Unlock()
		return 0
	}))
	L.SetGlobal("__check", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		branch := L.CheckInt(2)
		result := lua.LVAsBool(L.Get(3))
		st.addStep(Step{Kind: StepCheck, Block: id, Summary: id, Result: result, Branch: &branch})
		L.Push(lua.LBool(result))
		return 1
	}))
}

func joinArgs(L *lua.LState) string {
	parts := make([]string, 0, L.GetTop())
	for i := 1; i <= L.GetTop(); i++ {
		parts = append(parts, L.ToStringMeta(L.Get(i)).String())
	}
	return strings.Join(parts, " ")
}

// metatable returns the lookup of value type metatables, building each once
// per run. A type with lazy fields fetches them through their host function
// on first read, so the permission check applies there too.
func (st *runState) metatable(L *lua.LState) func(string) *lua.LTable {
	return func(typ string) *lua.LTable {
		if mt, ok := st.metas[typ]; ok {
			return mt
		}
		if st.metas == nil {
			st.metas = map[string]*lua.LTable{}
		}
		vt, ok := st.e.Registry.ValueType(typ)
		if !ok || len(vt.Lazy) == 0 {
			st.metas[typ] = nil
			return nil
		}
		mt := L.NewTable()
		mt.RawSetString("__index", L.NewFunction(func(L *lua.LState) int {
			t := L.CheckTable(1)
			key := L.CheckString(2)
			full, ok := vt.Lazy[key]
			if !ok {
				L.Push(lua.LNil)
				return 1
			}
			f, ok := st.e.Registry.Function(full)
			if !ok {
				L.Push(lua.LNil)
				return 1
			}
			res, err := st.invoke(false, full, f, []any{fromLua(t)})
			if err != nil {
				L.RaiseError("%s", err.Error())
			}
			var v lua.LValue = lua.LNil
			if len(res) > 0 {
				v = toLua(L, res[0], st.metatable(L))
			}
			t.RawSetString(key, v)
			L.Push(v)
			return 1
		}))
		st.metas[typ] = mt
		return mt
	}
}

func (st *runState) installModules(L *lua.LState) {
	for _, m := range st.e.Registry.Modules() {
		t := L.NewTable()
		for _, f := range m.Functions {
			t.RawSetString(f.Name, L.NewFunction(st.hostFunc(m.ID+"."+f.Name, f)))
		}
		L.SetGlobal(m.ID, t)
	}
}

func (st *runState) hostFunc(full string, f Function) lua.LGFunction {
	return func(L *lua.LState) int {
		args := make([]any, L.GetTop())
		for i := range args {
			args[i] = fromLua(L.Get(i + 1))
		}
		res, err := st.invoke(false, full, f, args)
		if err != nil {
			L.RaiseError("%s", err.Error())
		}
		meta := st.metatable(L)
		for _, v := range res {
			L.Push(toLua(L, v, meta))
		}
		return len(res)
	}
}

// invoke runs one host call through the permission check, the dry run
// switch and the confirmation, and records it as a step. A call a model
// chose asks the user every time it is risky, whatever the script is
// allowed.
func (st *runState) invoke(model bool, full string, f Function, args []any) ([]any, error) {
	if st.unwinding() {
		return nil, st.failed()
	}
	script := st.req.Script
	summary := summarize(full, args)
	if f.Summary != nil {
		summary = full + ": " + f.Summary(args)
	}
	for _, perm := range permList(f.Permission, f.Permissions) {
		if !script.granted(perm, st.req.Dry) {
			st.addStep(Step{Kind: StepDenied, Function: full, Summary: summary, Error: "needs " + perm})
			return nil, st.setFatal(StatusDenied, fmt.Sprintf("%s needs the permission %q, which the script does not have", full, perm))
		}
	}
	call := &Call{Ctx: st.ctx, Args: args, Dry: st.req.Dry, Hosts: st.e.Hosts, Function: full, st: st}
	if f.Effect && st.req.Dry {
		st.addStep(Step{Kind: StepPlanned, Function: full, Summary: summary})
		if f.Dry == nil {
			return nil, st.failedErr()
		}
		res, err := safeCall(f.Dry, call)
		if err != nil {
			return nil, err
		}
		return res, st.failedErr()
	}
	if f.Risky && (model || !st.isAllowed(full)) {
		if err := st.confirm(full, summary, model); err != nil {
			return nil, err
		}
	}
	res, err := safeCall(f.Handler, call)
	switch {
	case f.Effect:
		step := Step{Kind: StepCall, Function: full, Summary: summary}
		if err != nil {
			step.Error = err.Error()
		}
		st.addStep(step)
	case f.Permission != "" || len(f.Permissions) > 0:
		step := Step{Kind: StepRead, Function: full, Summary: summary}
		if err != nil {
			step.Error = err.Error()
		}
		st.addStep(step)
	}
	if err != nil {
		return nil, err
	}
	return res, st.failedErr()
}

func (st *runState) failedErr() error {
	if f := st.failed(); f != nil {
		return f
	}
	return nil
}

func (st *runState) isAllowed(full string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return slices.Contains(st.allowed, full)
}

// pauseClock stops the run's time budget while the user is asked, so reading
// a confirmation does not use it up. It reports false when the budget had
// already run out.
func (st *runState) pauseClock() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.deadline.Stop() {
		return false
	}
	st.remaining -= time.Since(st.clockFrom)
	return true
}

func (st *runState) resumeClock() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.clockFrom = time.Now()
	st.deadline.Reset(max(st.remaining, time.Millisecond))
}

func (st *runState) confirm(full, summary string, model bool) error {
	hosts := st.e.Hosts
	if hosts == nil || hosts.Confirm == nil {
		st.addStep(Step{Kind: StepDeclined, Function: full, Summary: summary, Error: "nobody to ask"})
		return st.setFatal(StatusDenied, full+" needs your confirmation, and there was no way to ask")
	}
	stopped := st.pauseClock()
	st.setWaiting(true)
	defer st.setWaiting(false)
	ans, err := hosts.Confirm.Confirm(st.ctx, ConfirmRequest{
		ScriptID:   st.req.Script.ID,
		ScriptName: st.req.Script.Name,
		Function:   full,
		Summary:    summary,
		EveryTime:  model,
	})
	if stopped {
		st.resumeClock()
	}
	if err != nil || !ans.Allow {
		st.addStep(Step{Kind: StepDeclined, Function: full, Summary: summary})
		if st.ctx.Err() != nil {
			return st.ctxFatal()
		}
		return st.setFatal(StatusDenied, "you declined "+full)
	}
	if ans.Always && !model {
		st.mu.Lock()
		st.allowed = append(st.allowed, full)
		st.mu.Unlock()
		if st.e.OnAllow != nil {
			st.e.OnAllow(st.req.Script.ID, full)
		}
	}
	return nil
}

func safeCall(h Handler, c *Call) (res []any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%s failed: %v", c.Function, p)
		}
	}()
	return h(c)
}

// summarize renders a call for the run log: its name and arguments, with
// messages shown by subject and long values cut short.
func summarize(full string, args []any) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, short(a))
	}
	return full + "(" + strings.Join(parts, ", ") + ")"
}

func short(v any) string {
	if m, ok := v.(map[string]any); ok {
		if subject, ok := m["subject"].(string); ok {
			return fmt.Sprintf("message %q", clip(subject, 60))
		}
	}
	b, err := json.Marshal(plainValue(v))
	if err != nil {
		return "?"
	}
	return clip(string(b), 120)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && cut < len(s) && s[cut]&0xc0 == 0x80 {
		cut--
	}
	return s[:cut] + "..."
}
