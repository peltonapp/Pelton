// Package pool limits how many sync sessions or sync HTTP requests one account
// may run at once. Sending stays outside this pool;
// callers simply never Acquire for that work. IMAP IDLE does check out a Live
// slot: it is one of the N sync sessions, not an extra connection.
//
// Configured N is the saved parallelism, clamped to 1..5. Effective N is the
// runtime ceiling and is never higher than configured N, so a throttle can
// shrink the pool without rewriting the saved setting.
//
// When effective N is at least 2, one slot is reserved for Live. Background
// may hold at most N-1 slots. Live may take any free slot, including the
// reserved one. When effective N is 1 there is no reservation.
//
// Release must use the same Kind that Acquire did. A mismatched Release does
// not free the slot that is still held.
package pool

import (
	"context"
	"sync"
)

// Kind is the class of sync work checking a slot out of the pool.
type Kind int

const (
	// Background is list and body campaigns. It cannot take the reserved live slot.
	Background Kind = iota
	// Live is IDLE, on-demand body, manual Sync, and new-mail follow-up.
	Live
)

const (
	minParallel = 1
	maxParallel = 5
)

// Pool is the per-account sync semaphore.
type Pool struct {
	mu         sync.Mutex
	cond       *sync.Cond
	configured int
	effective  int
	bgInUse    int
	liveInUse  int
	onResize   func()
}

// New returns a pool whose configured and effective size are n, clamped to 1..5.
func New(n int) *Pool {
	n = clampParallel(n)
	p := &Pool{configured: n, effective: n}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// SetConfigured stores the saved parallelism after clamping it to 1..5.
// Effective N shrinks when it would exceed the new ceiling. Raising the saved
// setting does not undo a throttle; call SetEffective to grow the pool again.
func (p *Pool) SetConfigured(n int) {
	p.mu.Lock()
	p.configured = clampParallel(n)
	if p.effective > p.configured {
		p.effective = p.configured
	}
	p.cond.Broadcast()
	fn := p.onResize
	p.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// SetEffective sets the throttled ceiling. Values below 1 become 1, and values
// above the configured size are capped at it. Live reservation follows the
// effective size: it applies only while that size is at least 2.
func (p *Pool) SetEffective(n int) {
	p.mu.Lock()
	if n < minParallel {
		n = minParallel
	}
	if n > p.configured {
		n = p.configured
	}
	p.effective = n
	p.cond.Broadcast()
	fn := p.onResize
	p.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// OnResize sets fn to run after every SetConfigured and SetEffective, so a
// dispatcher that waits on the pool's size outside Acquire can look again.
// fn runs without the pool's lock held and may call back into the pool. A
// later call replaces fn; nil removes it.
func (p *Pool) OnResize(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onResize = fn
}

// Effective returns the runtime concurrency ceiling (may be below configured).
func (p *Pool) Effective() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.effective
}

// MaxBackgroundSlots is how many background jobs may hold pool slots at once.
func (p *Pool) MaxBackgroundSlots() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.backgroundLimit()
}

// Configured returns the saved parallelism ceiling, clamped to 1..5.
func (p *Pool) Configured() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.configured
}

// Acquire blocks until a slot is free for k, or until ctx is done.
// A cancelled context does not leave a slot checked out.
func (p *Pool) Acquire(ctx context.Context, k Kind) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	// Wait cannot select on ctx. Wake the waiter when the deadline fires, and
	// stop that wake when Acquire returns so it cannot outlive the call.
	stop := context.AfterFunc(ctx, func() {
		p.mu.Lock()
		p.cond.Broadcast()
		p.mu.Unlock()
	})
	defer stop()

	for {
		// Release wakes this waiter with the slot free. A context that is
		// already done must not check that slot out.
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.allows(k) {
			p.hold(k)
			return nil
		}
		p.cond.Wait()
	}
}

// Release returns a slot of kind k. Callers must pass the same Kind they
// acquired. Releasing a kind that is not held does nothing.
func (p *Pool) Release(k Kind) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch k {
	case Live:
		if p.liveInUse == 0 {
			return
		}
		p.liveInUse--
	default:
		if p.bgInUse == 0 {
			return
		}
		p.bgInUse--
	}
	p.cond.Broadcast()
}

func (p *Pool) allows(k Kind) bool {
	if p.bgInUse+p.liveInUse >= p.effective {
		return false
	}
	if k != Live && p.bgInUse >= p.backgroundLimit() {
		return false
	}
	return true
}

func (p *Pool) backgroundLimit() int {
	if p.effective >= 2 {
		return p.effective - 1
	}
	return p.effective
}

func (p *Pool) hold(k Kind) {
	if k == Live {
		p.liveInUse++
		return
	}
	p.bgInUse++
}

func clampParallel(n int) int {
	if n < minParallel {
		return minParallel
	}
	if n > maxParallel {
		return maxParallel
	}
	return n
}
