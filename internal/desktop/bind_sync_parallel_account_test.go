package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func parallelFixture(t *testing.T, global int) (*App, int64, int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.SetInt(ctx, settingSyncMaxParallel, global); err != nil {
		t.Fatalf("set global: %v", err)
	}
	one, err := db.CreateAccount(ctx, &storage.Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	two, err := db.CreateAccount(ctx, &storage.Account{Email: "b@example.com"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	a := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}
	return a, one, two
}

func TestAccountSyncMaxParallelOverrideWins(t *testing.T) {
	a, withOverride, without := parallelFixture(t, 4)
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, withOverride, new(2)); err != nil {
		t.Fatal(err)
	}
	if got := a.accountSyncMaxParallel(withOverride); got != 2 {
		t.Errorf("override account = %d, want 2", got)
	}
	if got := a.accountSyncMaxParallel(without); got != 4 {
		t.Errorf("default account = %d, want global 4", got)
	}
	// a stored value outside the range is clamped on read as well.
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, withOverride, new(99)); err != nil {
		t.Fatal(err)
	}
	if got := a.accountSyncMaxParallel(withOverride); got != maxSyncMaxParallel {
		t.Errorf("out-of-range override = %d, want %d", got, maxSyncMaxParallel)
	}
}

func TestEnsureAccountSyncUsesOverride(t *testing.T) {
	a, withOverride, without := parallelFixture(t, 4)
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, withOverride, new(1)); err != nil {
		t.Fatal(err)
	}
	if got := mustAccountSync(t, a, withOverride).pool.Configured(); got != 1 {
		t.Errorf("override pool = %d, want 1", got)
	}
	if got := mustAccountSync(t, a, without).pool.Configured(); got != 4 {
		t.Errorf("default pool = %d, want 4", got)
	}
}

func TestGlobalChangeSkipsOverriddenAccounts(t *testing.T) {
	a, withOverride, without := parallelFixture(t, 4)
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, withOverride, new(2)); err != nil {
		t.Fatal(err)
	}
	over := mustAccountSync(t, a, withOverride).pool
	def := mustAccountSync(t, a, without).pool

	if err := a.SetSetting(settingSyncMaxParallel, "1"); err != nil {
		t.Fatal(err)
	}
	if got := over.Configured(); got != 2 {
		t.Errorf("overridden pool = %d after global change, want 2", got)
	}
	if got := def.Configured(); got != 1 {
		t.Errorf("default pool = %d after global change, want 1", got)
	}
}

func TestUpdateAccountReconfiguresRunningPool(t *testing.T) {
	a, id, _ := parallelFixture(t, 3)
	p := mustAccountSync(t, a, id).pool
	if p.Configured() != 3 {
		t.Fatalf("start = %d, want 3", p.Configured())
	}
	if _, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: new(0)}); err != nil {
		t.Fatal(err)
	}
	if got := p.Configured(); got != 1 {
		t.Errorf("after override 0 (clamped) = %d, want 1", got)
	}
	if _, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: nil}); err != nil {
		t.Fatal(err)
	}
	if got := p.Configured(); got != 3 {
		t.Errorf("after clearing override = %d, want global 3", got)
	}
}

// Every pause and live-slot decision reads the pool's effective size, and
// SetConfigured only ever lowers it. Raising the setting has to raise the
// running pool at once, or the account keeps behaving as N=1 until the
// throttle's success streak grows it back.
func TestRaisingParallelRaisesEffectiveAtOnce(t *testing.T) {
	t.Run("per-account override", func(t *testing.T) {
		a, id, _ := parallelFixture(t, 1)
		p := mustAccountSync(t, a, id).pool
		if got := p.Effective(); got != 1 {
			t.Fatalf("start effective = %d, want 1", got)
		}
		if _, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: new(3)}); err != nil {
			t.Fatal(err)
		}
		if got := p.Effective(); got != 3 {
			t.Errorf("effective after raising to 3 = %d, want 3", got)
		}
		if _, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: new(2)}); err != nil {
			t.Fatal(err)
		}
		if got := p.Effective(); got != 2 {
			t.Errorf("effective after lowering to 2 = %d, want 2", got)
		}
	})
	t.Run("global setting", func(t *testing.T) {
		a, id, _ := parallelFixture(t, 1)
		p := mustAccountSync(t, a, id).pool
		if err := a.SetSetting(settingSyncMaxParallel, "3"); err != nil {
			t.Fatal(err)
		}
		if got := p.Effective(); got != 3 {
			t.Errorf("effective after global 3 = %d, want 3", got)
		}
		if err := a.SetSetting(settingSyncMaxParallel, "1"); err != nil {
			t.Fatal(err)
		}
		if got := p.Effective(); got != 1 {
			t.Errorf("effective after global 1 = %d, want 1", got)
		}
	})
	t.Run("unchanged setting keeps a throttle", func(t *testing.T) {
		a, id, _ := parallelFixture(t, 4)
		rt := mustAccountSync(t, a, id)
		rt.pool.SetEffective(2) // the adaptive throttle halved it
		if _, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: nil}); err != nil {
			t.Fatal(err)
		}
		if got := rt.pool.Effective(); got != 2 {
			t.Errorf("effective after a save that left N at 4 = %d, want throttled 2", got)
		}
	})
}

// mustAccountSync is the account's runtime, failing the test if it is held.
func mustAccountSync(t *testing.T, a *App, accountID int64) *accountSync {
	t.Helper()
	rt, err := a.ensureAccountSync(a.ctx, accountID)
	if err != nil {
		t.Fatalf("ensureAccountSync(%d): %v", accountID, err)
	}
	return rt
}

// the request carries the whole editor state, so a nil SyncMaxParallel is the
// editor's "use default" and must clear a stored override, not keep it.
func TestUpdateAccountNilSyncMaxParallelClearsOverride(t *testing.T) {
	a, id, _ := parallelFixture(t, 3)
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, id, new(2)); err != nil {
		t.Fatal(err)
	}
	updated, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: nil})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SyncMaxParallel != nil {
		t.Errorf("returned override = %d, want nil", *updated.SyncMaxParallel)
	}
	stored, err := a.store.GetAccount(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SyncMaxParallel != nil {
		t.Errorf("stored override = %d, want nil", *stored.SyncMaxParallel)
	}
}
