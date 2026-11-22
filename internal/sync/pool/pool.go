// Package pool holds one account's connection cap and its logged-in sessions.
//
// Configured N is the saved parallelism (sync_max_parallel), clamped to 1..10.
// The ui offers 2..10; the settings accept 1..10, and the throttle may take
// the effective size down to 1.
// Effective N is the runtime cap and is never higher than configured N, so
// the adaptive throttle can shrink the pool without rewriting the setting.
//
// Sessions keeps up to effective N sessions open and reuses them across
// jobs. The scheduler (desktop/syncsched) runs at most effective N workers,
// each holding one session. At a constant N the open connections hold at N;
// after a shrink the sessions already checked out stay until their tasks
// end, so the new N is reached again as tasks end, and no new session is
// handed out above it meanwhile. Sending (SMTP) is another server and stays
// outside the pool.
package pool

import (
	"sync"
)

const (
	minParallel = 1
	maxParallel = 10
)

// Pool is the per-account connection cap.
type Pool struct {
	mu         sync.Mutex
	configured int
	effective  int
	watchers   map[int]func()
	nextW      int
}

// New returns a pool whose configured and effective size are n, clamped to 1..10.
func New(n int) *Pool {
	n = clampParallel(n)
	return &Pool{configured: n, effective: n}
}

// SetConfigured stores the saved parallelism after clamping it to 1..10.
// Effective N shrinks when it would exceed the new ceiling. Raising the saved
// setting does not undo a throttle; call SetEffective to grow the pool again.
func (p *Pool) SetConfigured(n int) {
	p.mu.Lock()
	p.configured = clampParallel(n)
	if p.effective > p.configured {
		p.effective = p.configured
	}
	p.mu.Unlock()
	p.notifyResize()
}

// SetEffective sets the throttled cap. Values below 1 become 1, and values
// above the configured size are capped at it.
func (p *Pool) SetEffective(n int) {
	p.mu.Lock()
	p.effective = min(max(n, minParallel), p.configured)
	p.mu.Unlock()
	p.notifyResize()
}

// Watch adds fn to run after every SetConfigured and SetEffective. fn runs
// on the setter's goroutine, without the pool's lock, and may call back
// into it. fn must not block: the setter may hold its own locks. Sessions'
// trim only hands sessions to a closer goroutine. The returned func removes
// fn.
func (p *Pool) Watch(fn func()) (stop func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.watchers == nil {
		p.watchers = make(map[int]func())
	}
	id := p.nextW
	p.nextW++
	p.watchers[id] = fn
	return func() {
		p.mu.Lock()
		delete(p.watchers, id)
		p.mu.Unlock()
	}
}

func (p *Pool) notifyResize() {
	p.mu.Lock()
	fns := make([]func(), 0, len(p.watchers))
	for _, w := range p.watchers {
		fns = append(fns, w)
	}
	p.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Effective returns the runtime connection cap (may be below configured).
func (p *Pool) Effective() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.effective
}

// Configured returns the saved parallelism ceiling, clamped to 1..10.
func (p *Pool) Configured() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.configured
}

func clampParallel(n int) int {
	return min(max(n, minParallel), maxParallel)
}
