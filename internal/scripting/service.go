package scripting

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"
)

// Dispatcher limits.
const (
	// MaxDepth is how many scripts may set each other off in a chain: a run
	// started by an event a run caused is one level deeper. Deeper events
	// are dropped, which ends a loop of scripts triggering each other.
	MaxDepth = 3
	// maxRunsPerMinute caps the event started runs of one script.
	maxRunsPerMinute = 60
	// queueSize is how many runs may wait for one script.
	queueSize = 256
	// tickEvery is how often schedules are checked.
	tickEvery = 30 * time.Second
	// catchUpSettle is how long the dispatcher waits after the last mail of a
	// catch-up before running over it, so the first sync can finish.
	catchUpSettle = 5 * time.Second
	// keepRuns is how many runs of each script the log keeps at most.
	keepRuns = 500
	// DefaultRetentionDays is how long runs are kept unless set otherwise.
	DefaultRetentionDays = 30
)

// Event is something that happened in the app that scripts may react to.
type Event struct {
	// Trigger is the trigger id, e.g. "mail.arrived".
	Trigger string
	// Data is the raw event, e.g. {"message_id": int64(5)}. The trigger's
	// Prepare turns it into the event table.
	Data map[string]any
	// Depth counts how many runs led to this event; see EventDepth.
	Depth int
	// CatchUp marks mail the first sync after start brought in, which may
	// have arrived while the app was closed.
	CatchUp bool
	// Date is when the mail was sent, for catch-up order and age.
	Date time.Time
	// Source is the script whose run caused the event, 0 for none. That
	// script is not started by it again.
	Source int64
}

// EventDepth returns the depth an event caused from ctx gets: one more than
// the run ctx belongs to, or 0 outside a run.
func EventDepth(ctx context.Context) int {
	if info, ok := RunInfoFrom(ctx); ok {
		return info.Depth + 1
	}
	return 0
}

// EventFrom builds an event caused from ctx, with the depth and the source
// script of the run ctx belongs to, if any.
func EventFrom(ctx context.Context, trigger string, data map[string]any) Event {
	ev := Event{Trigger: trigger, Data: data, Depth: EventDepth(ctx)}
	if info, ok := RunInfoFrom(ctx); ok {
		ev.Source = info.ScriptID
	}
	return ev
}

// Config sets up a Service.
type Config struct {
	DB     *sql.DB
	Hosts  *Hosts
	Limits Limits
	Log    *slog.Logger
	// OnRun is told about every finished run that was logged, and every
	// test run.
	OnRun func(s *Script, r *Run)
	// OnChange is told when scripts were added, changed or removed.
	OnChange func()
	// OnCatchUp asks the user whether a script in ask mode should run over
	// count messages that arrived while the app was closed. The answer comes
	// back through ResolveCatchUp.
	OnCatchUp func(s *Script, count int)
	// RetentionDays returns how many days of runs to keep.
	RetentionDays func() int
	// AskTimeout returns how long a question of a run nobody started waits.
	AskTimeout func() time.Duration
	// OnProgress is told about a run that takes a moment: when it shows up,
	// as it goes and when it is done.
	OnProgress func(LiveRun)
}

// LiveRun is a run in progress, for the progress indicator and its cancel
// button. ID is not the run log id; a test run has one too.
type LiveRun struct {
	ID         int64  `json:"id"`
	ScriptID   int64  `json:"scriptId"`
	ScriptName string `json:"scriptName"`
	Dry        bool   `json:"dry"`
	Attended   bool   `json:"attended"`
	// Steps counts what the run did so far and Last says what it did last.
	Steps int    `json:"steps"`
	Last  string `json:"last"`
	// Waiting is set while the run waits for the user.
	Waiting bool `json:"waiting"`
	Done    bool `json:"done"`
}

// progressAfter is how long a run goes before it shows up as in progress,
// so quick runs never flash an indicator, and progressEvery how often it is
// updated after that.
const (
	progressAfter = 300 * time.Millisecond
	progressEvery = 200 * time.Millisecond
)

type liveRun struct {
	mu     sync.Mutex
	info   LiveRun
	shown  bool
	last   time.Time
	cancel context.CancelCauseFunc
}

// Service is the scripting system: the registry, the store, the engine and
// the dispatcher that starts runs.
type Service struct {
	cfg    Config
	reg    *Registry
	store  *Store
	engine *Engine
	log    *slog.Logger

	mu         sync.Mutex
	scripts    map[int64]*Script
	workers    map[int64]*worker
	lastFired  map[int64]time.Time
	watermarks map[int64]time.Time
	rate       map[int64]*rateWindow
	pending    map[int64]*catchUp
	done       chan struct{}
	wg         sync.WaitGroup
	running    bool
	lastPrune  time.Time
	live       map[int64]*liveRun
	nextLive   int64
}

type worker struct {
	mu   sync.Mutex
	jobs chan Event
}

type rateWindow struct {
	start   time.Time
	count   int
	skipped bool
}

type catchUp struct {
	events []Event
	timer  *time.Timer
	asked  bool
}

// NewService builds the service and registers the built-ins.
func NewService(cfg Config) (*Service, error) {
	reg := NewRegistry()
	if err := RegisterBuiltins(reg); err != nil {
		return nil, err
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Hosts == nil {
		cfg.Hosts = &Hosts{}
	}
	if cfg.Limits.Timeout <= 0 {
		cfg.Limits = DefaultLimits()
	}
	s := &Service{
		cfg:        cfg,
		reg:        reg,
		store:      NewStore(cfg.DB),
		log:        cfg.Log,
		scripts:    map[int64]*Script{},
		workers:    map[int64]*worker{},
		lastFired:  map[int64]time.Time{},
		watermarks: map[int64]time.Time{},
		rate:       map[int64]*rateWindow{},
		pending:    map[int64]*catchUp{},
		live:       map[int64]*liveRun{},
	}
	s.engine = &Engine{Registry: reg, Hosts: cfg.Hosts, KV: s.store, Limits: cfg.Limits, OnAllow: s.allowAlways, AskTimeout: cfg.AskTimeout, Libraries: s.store}
	return s, nil
}

// Cancel cancels a run in progress by its live id. It reports whether the
// run was still going.
func (s *Service) Cancel(liveID int64) bool {
	s.mu.Lock()
	lr := s.live[liveID]
	s.mu.Unlock()
	if lr == nil {
		return false
	}
	lr.cancel(ErrRunCancelled)
	return true
}

// Live lists the runs in progress that have shown up.
func (s *Service) Live() []LiveRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []LiveRun{}
	for _, lr := range s.live {
		lr.mu.Lock()
		if lr.shown {
			out = append(out, lr.info)
		}
		lr.mu.Unlock()
	}
	slices.SortFunc(out, func(a, b LiveRun) int { return int(a.ID - b.ID) })
	return out
}

// track registers a run in progress, wiring its steps and waits into
// progress reports, and returns the context that cancels it and the
// function that ends the tracking.
func (s *Service) track(ctx context.Context, req *RunRequest) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	s.mu.Lock()
	s.nextLive++
	lr := &liveRun{cancel: cancel, info: LiveRun{
		ID: s.nextLive, ScriptID: req.Script.ID, ScriptName: req.Script.Name, Dry: req.Dry, Attended: req.Attended,
	}}
	s.live[lr.info.ID] = lr
	s.mu.Unlock()
	report := func(force bool) {
		lr.mu.Lock()
		if !lr.shown || (!force && time.Since(lr.last) < progressEvery) {
			lr.mu.Unlock()
			return
		}
		lr.last = time.Now()
		info := lr.info
		lr.mu.Unlock()
		if s.cfg.OnProgress != nil {
			s.cfg.OnProgress(info)
		}
	}
	show := time.AfterFunc(progressAfter, func() {
		lr.mu.Lock()
		lr.shown = true
		lr.mu.Unlock()
		report(true)
	})
	req.OnStep = func(st Step) {
		lr.mu.Lock()
		lr.info.Steps++
		if st.Kind != StepCheck {
			lr.info.Last = st.Summary
		}
		lr.mu.Unlock()
		report(false)
	}
	req.OnWait = func(waiting bool) {
		lr.mu.Lock()
		lr.info.Waiting = waiting
		if waiting {
			lr.shown = true
		}
		lr.mu.Unlock()
		report(true)
	}
	return ctx, func() {
		show.Stop()
		cancel(nil)
		s.mu.Lock()
		delete(s.live, lr.info.ID)
		s.mu.Unlock()
		lr.mu.Lock()
		lr.info.Done = true
		lr.info.Waiting = false
		lr.mu.Unlock()
		report(true)
	}
}

// Registry returns the registry, for other packages to register entries in
// at startup.
func (s *Service) Registry() *Registry { return s.reg }

// Store returns the store.
func (s *Service) Store() *Store { return s.store }

func (s *Service) now() time.Time { return s.cfg.Hosts.now() }

// load reads every script into the cache.
func (s *Service) load(ctx context.Context) error {
	list, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts = map[int64]*Script{}
	for _, sc := range list {
		s.scripts[sc.ID] = sc
		if _, ok := s.lastFired[sc.ID]; !ok {
			s.lastFired[sc.ID] = sc.LastRunAt
		}
	}
	return nil
}

// Start loads the scripts, remembers until when each one already saw mail,
// fires the app start trigger and starts the schedule loop.
func (s *Service) Start(ctx context.Context) error {
	if err := s.load(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.done = make(chan struct{})
	s.workers = map[int64]*worker{}
	for id, sc := range s.scripts {
		s.watermarks[id] = laterOf(sc.EnabledAt, sc.AliveAt)
	}
	s.mu.Unlock()

	now := s.now()
	if err := s.store.MarkAlive(ctx, now); err != nil {
		s.log.Error("scripting: mark alive", "err", err)
	}
	s.prune(ctx, now)
	s.Emit(Event{Trigger: TriggerAppStart})

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-t.C:
				s.tick(ctx, s.now())
			}
		}
	}()
	return nil
}

// Stop ends the schedule loop and the workers once their current run is
// done, and stamps the scripts alive for the next start's catch-up.
func (s *Service) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.done)
	for _, w := range s.workers {
		close(w.jobs)
	}
	s.workers = map[int64]*worker{}
	for _, p := range s.pending {
		if p.timer != nil {
			p.timer.Stop()
		}
	}
	s.mu.Unlock()
	s.wg.Wait()
	if err := s.store.MarkAlive(context.Background(), s.now()); err != nil {
		s.log.Error("scripting: mark alive", "err", err)
	}
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (s *Service) tick(ctx context.Context, now time.Time) {
	if err := s.store.MarkAlive(ctx, now); err != nil {
		s.log.Error("scripting: mark alive", "err", err)
	}
	if now.Sub(s.lastPrune) > time.Hour {
		s.prune(ctx, now)
	}
	s.mu.Lock()
	due := map[int64]string{}
	for id, sc := range s.scripts {
		if !sc.Enabled {
			continue
		}
		t, ok := s.reg.Trigger(sc.Trigger.Type)
		if !ok || t.Due == nil {
			continue
		}
		if t.Due(sc.Trigger.Params, s.lastFired[id], sc.EnabledAt, now) {
			s.lastFired[id] = now
			due[id] = sc.Trigger.Type
		}
	}
	s.mu.Unlock()
	for id, trigger := range due {
		s.enqueue(id, Event{Trigger: trigger})
	}
}

func (s *Service) prune(ctx context.Context, now time.Time) {
	s.lastPrune = now
	days := DefaultRetentionDays
	if s.cfg.RetentionDays != nil {
		days = s.cfg.RetentionDays()
	}
	if days < 1 {
		days = 1
	}
	if err := s.store.PruneRuns(ctx, now.AddDate(0, 0, -days), keepRuns); err != nil {
		s.log.Error("scripting: prune runs", "err", err)
	}
}

// Wants reports whether an enabled script is started by trigger, so a hook can
// skip building an event nobody listens to.
func (s *Service) Wants(trigger string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.scripts {
		if sc.Enabled && sc.Trigger.Type == trigger {
			return true
		}
	}
	return false
}

// Listening returns the trigger parameters of every enabled script started by
// trigger, for a hook that works out itself which events are due, such as
// "an event starts in N minutes".
func (s *Service) Listening(trigger string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, sc := range s.scripts {
		if sc.Enabled && sc.Trigger.Type == trigger {
			out = append(out, sc.Trigger.Params)
		}
	}
	return out
}

// listeners returns the enabled scripts ev starts, leaving out the script
// whose run caused it, in id order. A trigger marked missing starts none.
func (s *Service) listeners(ev Event) []*Script {
	if t, ok := s.reg.Trigger(ev.Trigger); ok && t.Missing != "" {
		return nil
	}
	s.mu.Lock()
	var targets []*Script
	for _, sc := range s.scripts {
		if sc.Enabled && sc.Trigger.Type == ev.Trigger && (ev.Source == 0 || sc.ID != ev.Source) {
			targets = append(targets, sc)
		}
	}
	s.mu.Unlock()
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets
}

// Emit hands an event to every enabled script started by its trigger, but not
// to the script that caused it. It returns at once; the runs happen on the
// scripts' own queues.
func (s *Service) Emit(ev Event) {
	for _, sc := range s.listeners(ev) {
		if ev.CatchUp {
			s.catchUp(sc, ev)
			continue
		}
		s.enqueue(sc.ID, ev)
	}
}

// guard applies the loop and rate limits, logging a skipped run the first
// time a script is held back.
func (s *Service) guard(sc *Script, ev Event) bool {
	reason := ""
	s.mu.Lock()
	switch {
	case ev.Depth > MaxDepth:
		reason = fmt.Sprintf("stopped a chain of %d scripts setting each other off", ev.Depth)
	default:
		now := s.now()
		w := s.rate[sc.ID]
		if w == nil || now.Sub(w.start) >= time.Minute {
			w = &rateWindow{start: now}
			s.rate[sc.ID] = w
		}
		w.count++
		if w.count > maxRunsPerMinute {
			reason = fmt.Sprintf("ran more than %d times in a minute", maxRunsPerMinute)
			if w.skipped {
				s.mu.Unlock()
				return false
			}
			w.skipped = true
		}
	}
	s.mu.Unlock()
	if reason == "" {
		return true
	}
	run := &Run{ScriptID: sc.ID, Trigger: ev.Trigger, Depth: ev.Depth, Started: s.now(), Status: StatusSkipped, Error: reason}
	if err := s.store.FinishRun(context.Background(), run); err != nil {
		s.log.Error("scripting: log skipped run", "err", err)
	}
	if s.cfg.OnRun != nil {
		s.cfg.OnRun(sc, run)
	}
	return false
}

func (s *Service) enqueue(id int64, ev Event) {
	s.mu.Lock()
	sc := s.scripts[id]
	s.mu.Unlock()
	if sc == nil {
		return
	}
	if ev.Trigger != TriggerAppStart && !s.guard(sc, ev) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.scripts[id] == nil {
		return
	}
	w := s.workers[id]
	if w == nil {
		w = &worker{jobs: make(chan Event, queueSize)}
		s.workers[id] = w
		s.wg.Add(1)
		go s.work(id, w)
	}
	select {
	case w.jobs <- ev:
	default:
		s.log.Warn("scripting: queue full, dropped a run", "script", id, "trigger", ev.Trigger)
	}
}

func (s *Service) work(id int64, w *worker) {
	defer s.wg.Done()
	for ev := range w.jobs {
		w.mu.Lock()
		s.mu.Lock()
		sc := s.scripts[id]
		s.mu.Unlock()
		if sc != nil && sc.Enabled {
			if _, err := s.dispatch(context.Background(), sc, ev); err != nil {
				s.log.Error("scripting: run", "script", id, "err", err)
			}
		}
		w.mu.Unlock()
	}
}

// dispatch prepares the event for one script, checks it concerns the script
// and runs it. It returns nil without running when the event does not match.
func (s *Service) dispatch(ctx context.Context, sc *Script, ev Event) (*Run, error) {
	t, ok := s.reg.Trigger(sc.Trigger.Type)
	if !ok {
		return nil, fmt.Errorf("scripting: unknown trigger %q", sc.Trigger.Type)
	}
	event, err := s.prepare(ctx, t, ev.Data)
	if err != nil {
		return nil, err
	}
	if t.Match != nil && !t.Match(sc.Trigger.Params, event) {
		return nil, nil
	}
	return s.run(ctx, sc, ev.Trigger, event, ev.Depth, false, false)
}

func (s *Service) prepare(ctx context.Context, t Trigger, data map[string]any) (map[string]any, error) {
	if data == nil {
		data = map[string]any{}
	}
	if t.Prepare == nil {
		return data, nil
	}
	return t.Prepare(&PrepareContext{Ctx: ctx, Hosts: s.cfg.Hosts}, data)
}

// run executes one run, logging it unless it is a dry run. attended says the
// user started it and is there to answer its questions.
func (s *Service) run(ctx context.Context, sc *Script, trigger string, event map[string]any, depth int, dry, attended bool) (*Run, error) {
	req := RunRequest{Script: sc, Trigger: trigger, Event: event, Dry: dry, Depth: depth, Attended: attended}
	if !dry {
		if t, ok := s.reg.Trigger(trigger); ok && !sc.granted(t.Permission, false) {
			run := &Run{ScriptID: sc.ID, Trigger: trigger, Depth: depth, Started: s.now(), Status: StatusDenied,
				Error: fmt.Sprintf("the trigger needs the permission %q", t.Permission)}
			return run, s.finish(ctx, sc, run)
		}
		id, err := s.store.StartRun(ctx, &Run{ScriptID: sc.ID, Trigger: trigger, Depth: depth, Started: s.now()})
		if err != nil {
			return nil, err
		}
		req.RunID = id
	}
	runCtx, untrack := s.track(ctx, &req)
	run := s.engine.Run(runCtx, req)
	untrack()
	if dry {
		if s.cfg.OnRun != nil {
			s.cfg.OnRun(sc, run)
		}
		return run, nil
	}
	return run, s.finish(ctx, sc, run)
}

func (s *Service) finish(ctx context.Context, sc *Script, run *Run) error {
	err := s.store.FinishRun(ctx, run)
	s.mu.Lock()
	if cached := s.scripts[sc.ID]; cached != nil {
		cached.LastRunAt = run.Started
	}
	s.mu.Unlock()
	if s.cfg.OnRun != nil {
		s.cfg.OnRun(sc, run)
	}
	return err
}

// RunSync runs every enabled script started by ev's trigger right away, one
// after the other, and returns their runs. It is for triggers whose caller
// waits on the outcome, such as a send that a script may stop or change:
// each script sees the event as the previous one left it, and the caller
// reads the last event back from the final run.
func (s *Service) RunSync(ctx context.Context, ev Event) []*Run {
	targets := s.listeners(ev)
	var runs []*Run
	data := ev.Data
	for _, sc := range targets {
		if !s.guard(sc, ev) {
			continue
		}
		w := s.workerFor(sc.ID)
		w.mu.Lock()
		run, err := s.dispatch(ctx, sc, Event{Trigger: ev.Trigger, Data: data, Depth: ev.Depth, Source: ev.Source})
		w.mu.Unlock()
		if err != nil {
			s.log.Error("scripting: run", "script", sc.ID, "err", err)
			continue
		}
		if run == nil {
			continue
		}
		runs = append(runs, run)
		if run.Event != nil {
			data = run.Event
		}
		if run.Cancelled {
			break
		}
	}
	return runs
}

func (s *Service) workerFor(id int64) *worker {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.workers[id]
	if w == nil {
		w = &worker{jobs: make(chan Event, queueSize)}
		s.workers[id] = w
		if s.running {
			s.wg.Add(1)
			go s.work(id, w)
		}
	}
	return w
}

// RunNow runs a script at once, as its manual trigger or from the run
// button, and waits for the result. data is the raw event, e.g. the message
// the user picked.
func (s *Service) RunNow(ctx context.Context, id int64, data map[string]any) (*Run, error) {
	return s.RunNowWithSettings(ctx, id, data, nil)
}

// RunNowWithSettings is RunNow with some of the script's settings replaced
// for this one run, checked against its schema. Nothing is stored.
func (s *Service) RunNowWithSettings(ctx context.Context, id int64, data, settings map[string]any) (*Run, error) {
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(settings) > 0 {
		merged := maps.Clone(sc.Settings)
		if merged == nil {
			merged = map[string]any{}
		}
		maps.Copy(merged, settings)
		if sc.Settings, err = CheckSettings(sc.SettingsSchema, merged); err != nil {
			return nil, err
		}
	}
	if !sc.Enabled {
		return nil, fmt.Errorf("scripting: %q is switched off", sc.Name)
	}
	t, ok := s.reg.Trigger(sc.Trigger.Type)
	if !ok {
		return nil, fmt.Errorf("scripting: unknown trigger %q", sc.Trigger.Type)
	}
	event, err := s.prepare(ctx, t, data)
	if err != nil {
		return nil, err
	}
	w := s.workerFor(id)
	w.mu.Lock()
	defer w.mu.Unlock()
	return s.run(ctx, sc, sc.Trigger.Type, event, EventDepth(ctx), false, true)
}

// TestRun runs a script, saved or not, as a dry run: reads work, changes are
// recorded as planned steps and nothing is changed. data is the raw event;
// nil uses a sample event for the script's trigger.
func (s *Service) TestRun(ctx context.Context, sc *Script, data map[string]any) (*Run, error) {
	t, ok := s.reg.Trigger(sc.Trigger.Type)
	if !ok {
		return nil, fmt.Errorf("scripting: unknown trigger %q", sc.Trigger.Type)
	}
	if data == nil {
		return s.run(ctx, sc, sc.Trigger.Type, SampleEvent(s.reg, t, s.now()), 0, true, true)
	}
	event, err := s.prepare(ctx, t, data)
	if err != nil {
		return nil, err
	}
	return s.run(ctx, sc, sc.Trigger.Type, event, 0, true, true)
}

// SampleEvent builds an event for a trigger from its declared fields, for a
// test run with nothing picked. A field of a value type with a Sample in r
// gets that sample.
func SampleEvent(r *Registry, t Trigger, now time.Time) map[string]any {
	out := map[string]any{}
	for _, f := range t.Event {
		switch f.Type {
		case "message":
			out[f.Name] = MessageObject(SampleMessage(now))
		case "draft":
			out[f.Name] = map[string]any{
				"account_id": 0, "to": []any{"someone@example.com"}, "cc": []any{}, "bcc": []any{},
				"subject": "Quarterly report", "body": "Hello,\n\nthe report is attached.\n",
			}
		case "bool":
			out[f.Name] = true
		case "folder":
			out[f.Name] = "Archive"
		case "number":
			out[f.Name] = 0
		default:
			out[f.Name] = ""
			if r == nil {
				continue
			}
			if vt, ok := r.ValueType(f.Type); ok && vt.Sample != nil {
				out[f.Name] = Object{Type: vt.ID, Fields: vt.Sample(now)}
			}
		}
	}
	return out
}

func (s *Service) catchUp(sc *Script, ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mark := s.watermarks[sc.ID]
	if mark.IsZero() {
		mark = sc.EnabledAt
	}
	if sc.CatchUp == CatchUpLive || ev.Date.Before(mark) {
		return
	}
	p := s.pending[sc.ID]
	if p == nil {
		p = &catchUp{}
		s.pending[sc.ID] = p
	}
	if p.asked {
		return
	}
	p.events = append(p.events, ev)
	id := sc.ID
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(catchUpSettle, func() { s.flushCatchUp(id) })
}

func (s *Service) flushCatchUp(id int64) {
	s.mu.Lock()
	p := s.pending[id]
	sc := s.scripts[id]
	if p == nil || sc == nil || !s.running {
		s.mu.Unlock()
		return
	}
	events := p.events
	sort.SliceStable(events, func(i, j int) bool { return events[i].Date.Before(events[j].Date) })
	limit := sc.CatchUpCap
	if limit <= 0 {
		limit = DefaultCatchUpCap
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	p.events = events
	if sc.CatchUp == CatchUpAsk {
		p.asked = true
		s.mu.Unlock()
		if s.cfg.OnCatchUp != nil {
			s.cfg.OnCatchUp(sc, len(events))
		}
		return
	}
	delete(s.pending, id)
	s.mu.Unlock()
	for _, ev := range events {
		ev.CatchUp = false
		s.enqueue(id, ev)
	}
}

// ResolveCatchUp answers a catch-up question: run over the waiting mail, or
// drop it.
func (s *Service) ResolveCatchUp(id int64, run bool) {
	s.mu.Lock()
	p := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if p == nil || !run {
		return
	}
	for _, ev := range p.events {
		s.enqueue(id, ev)
	}
}

// PendingCatchUps lists the scripts waiting for a catch-up answer, with how
// many messages each would run over.
func (s *Service) PendingCatchUps() map[int64]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int64]int{}
	for id, p := range s.pending {
		if p.asked {
			out[id] = len(p.events)
		}
	}
	return out
}

func (s *Service) allowAlways(id int64, function string) {
	if id == 0 {
		return
	}
	ctx := context.Background()
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return
	}
	if !slices.Contains(sc.AllowUnasked, function) {
		sc.AllowUnasked = append(sc.AllowUnasked, function)
		if err := s.save(ctx, sc); err != nil {
			s.log.Error("scripting: remember allow", "err", err)
		}
	}
}
