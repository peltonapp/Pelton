package desktop

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// loginCap refuses a login while limit sessions are open, the way
// Gmail or Dovecot refuse one connection too many.
type loginCap struct {
	mu                   sync.Mutex
	limit, open, refused int
}

// cappedConn is one connection to a loginCap, on the mailbox of a
// wideIMAP, with IDLE.
type cappedConn struct {
	*wideIMAP
	srv  *loginCap
	once sync.Once
}

func (s *loginCap) dial(box *wideIMAP) (mailClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open >= s.limit {
		s.refused++
		return nil, errors.New("imap: login: NO [UNAVAILABLE] Too many simultaneous connections. (Failure)")
	}
	s.open++
	return &cappedConn{wideIMAP: box, srv: s}, nil
}

func (c *cappedConn) release() {
	c.once.Do(func() {
		c.srv.mu.Lock()
		c.srv.open--
		c.srv.mu.Unlock()
	})
}

func (c *cappedConn) Logout() error      { c.release(); return nil }
func (c *cappedConn) Close() error       { c.release(); return nil }
func (c *cappedConn) SupportsIdle() bool { return true }
func (c *cappedConn) IdleUntil(ctx context.Context) (bool, error) {
	<-ctx.Done()
	return false, nil
}

func (s *loginCap) refusals() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

// At N=4 with IDLE against a server that takes 2 connections, the refused
// logins halve N to 2, after which the folder's bodies run one unit at a
// time beside IDLE; the refusals stay few and the folder still fills.
func TestThrottledServerRunsOneBodyUnitBesideIdle(t *testing.T) {
	box := newWideIMAP(20 * time.Millisecond)
	srv := &loginCap{limit: 2}
	a, account, inbox, rt := wideInbox(t, box, 40, 4)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return srv.dial(box) }
	runIdle(t, a, &account)
	waitUntil(t, 5*time.Second, "IDLE never parked", func() bool { return rt.sched.Stats().Idle })

	var units, overlaps atomic.Int32
	step := func(ctx context.Context, c mailClient, folder storage.Folder, kind syncsched.Kind) error {
		if kind != syncsched.KindBodies {
			return nil
		}
		if n := units.Add(1); n > 1 && rt.pool.Effective() <= 2 {
			overlaps.Add(1)
		}
		defer units.Add(-1)
		return a.imapBodyBatch(withTaskSession(ctx, account.ID, c), account, folder)
	}
	done := make(chan error, 1)
	enqueueSyncPlan(a.ctx, rt.sched, []storage.Folder{inbox}, syncPlan{step: step, bodyWidth: rt.bodyWidth, done: done})
	if err := recv(t, done, 20*time.Second, "the plan never ended"); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := rt.pool.Effective(); got > 2 {
		t.Fatalf("effective N %d after the refusals, want at most 2", got)
	}
	if n := overlaps.Load(); n != 0 {
		t.Fatalf("%d body units started beside another at N=2 with IDLE", n)
	}
	if r := srv.refusals(); r == 0 || r > 4 {
		t.Fatalf("%d refused logins, want a few that taught the throttle", r)
	}
	if n := missingBodies(t, a, inbox.ID); n != 0 {
		t.Fatalf("%d bodies missing", n)
	}
}
