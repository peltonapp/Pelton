package pool

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestBackgroundCannotTakeLastSlotWhenNIs3(t *testing.T) {
	p := New(3)
	ctx := context.Background()
	if err := p.Acquire(ctx, Background); err != nil {
		t.Fatal(err)
	}
	if err := p.Acquire(ctx, Background); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := p.Acquire(ctx2, Background); err == nil {
		t.Fatal("third background must not take reserved live slot")
	}
	if err := p.Acquire(ctx, Live); err != nil {
		t.Fatal("live must get reserved slot:", err)
	}
}

func TestThrottleLowersEffective(t *testing.T) {
	p := New(3)
	ctx := context.Background()
	if err := p.Acquire(ctx, Background); err != nil {
		t.Fatal(err)
	}
	p.SetEffective(1)
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := p.Acquire(ctx2, Background); err == nil {
		t.Fatal("SetEffective(1) must block a second background acquire")
	}
}

func TestBackgroundTakesTheOnlySlotWhenNIs1(t *testing.T) {
	p := New(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Acquire(ctx, Background); err != nil {
		t.Fatal("background must take the only slot when N is 1:", err)
	}
	ctx2, cancel2 := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel2()
	if err := p.Acquire(ctx2, Live); err == nil {
		t.Fatal("live must wait while the only slot is held")
	}
	p.Release(Background)
	if err := p.Acquire(ctx, Live); err != nil {
		t.Fatal("live must run after background releases the only slot:", err)
	}
}

func TestLiveMayUseEverySlot(t *testing.T) {
	p := New(3)
	ctx := context.Background()
	for i := range 3 {
		if err := p.Acquire(ctx, Live); err != nil {
			t.Fatalf("live %d: %v", i, err)
		}
	}
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := p.Acquire(ctx2, Background); err == nil {
		t.Fatal("background must not exceed effective N")
	}
	p.Release(Live)
	if err := p.Acquire(ctx, Background); err != nil {
		t.Fatal("background must take a slot live released:", err)
	}
}

func TestSetConfiguredShrinksEffective(t *testing.T) {
	p := New(3)
	p.SetConfigured(1)
	ctx := context.Background()
	if err := p.Acquire(ctx, Background); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := p.Acquire(ctx2, Background); err == nil {
		t.Fatal("shrinking configured N to 1 must block a second background")
	}
}

func TestSetConfiguredDoesNotUndoThrottle(t *testing.T) {
	p := New(3)
	p.SetEffective(1)
	p.SetConfigured(5)
	ctx := context.Background()
	if err := p.Acquire(ctx, Background); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := p.Acquire(ctx2, Background); err == nil {
		t.Fatal("raising configured N must not undo an effective throttle")
	}
}

func TestSetConfiguredClampsAndCapsEffective(t *testing.T) {
	p := New(1)
	p.SetConfigured(100)
	p.SetEffective(100)
	ctx := context.Background()
	for i := range 4 {
		if err := p.Acquire(ctx, Background); err != nil {
			t.Fatalf("background %d: %v", i, err)
		}
	}
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := p.Acquire(ctx2, Background); err == nil {
		t.Fatal("parallelism must clamp to 5, reserving the last slot for live")
	}
	if err := p.Acquire(ctx, Live); err != nil {
		t.Fatal("live must take the reserved slot at the clamped ceiling:", err)
	}
}

func TestCancelledAcquireDoesNotTakeReleasedSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Hold the only slot, block in Acquire, cancel, then Release. Repeating
		// covers the wake where that slot is already free: a cancelled waiter
		// must not check it out.
		const rounds = 200
		for i := range rounds {
			p := New(1)
			if err := p.Acquire(context.Background(), Background); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			errCh := make(chan error, 1)
			go func() {
				errCh <- p.Acquire(ctx, Background)
			}()
			// Acquire is parked in cond.Wait once the bubble is idle.
			synctest.Wait()

			cancel()
			p.Release(Background)

			select {
			case err := <-errCh:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("round %d: err=%v, want context canceled", i, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("round %d: Acquire did not return after cancel", i)
			}

			p.mu.Lock()
			bg, live := p.bgInUse, p.liveInUse
			p.mu.Unlock()
			if bg != 0 || live != 0 {
				t.Fatalf("round %d: bgInUse=%d liveInUse=%d, cancelled Acquire checked out a slot", i, bg, live)
			}
		}
	})
}
