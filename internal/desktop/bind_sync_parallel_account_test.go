package desktop

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// parallelFixture is a hold test app with the global parallel setting at
// global and two accounts without an override.
func parallelFixture(t *testing.T, global int) (*App, int64, int64) {
	t.Helper()
	a := newHoldTestApp(t)
	setParallel(t, a, global)
	var ids [2]int64
	for i, email := range []string{"a@example.com", "b@example.com"} {
		id, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: email})
		if err != nil {
			t.Fatalf("create %s: %v", email, err)
		}
		ids[i] = id
	}
	return a, ids[0], ids[1]
}

// An account's override wins over the global setting, both on read and for
// the pool of the runtime it starts.
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
	if got := mustAccountSync(t, a, withOverride).pool.Configured(); got != 2 {
		t.Errorf("override pool = %d, want 2", got)
	}
	if got := mustAccountSync(t, a, without).pool.Configured(); got != 4 {
		t.Errorf("default pool = %d, want 4", got)
	}
	// a stored value outside the range is clamped on read as well.
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, withOverride, new(99)); err != nil {
		t.Fatal(err)
	}
	if got := a.accountSyncMaxParallel(withOverride); got != maxSyncMaxParallel {
		t.Errorf("out-of-range override = %d, want %d", got, maxSyncMaxParallel)
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

// The request carries the whole editor state, so a nil SyncMaxParallel is the
// editor's "use default": it clears the stored override, not keeps it, and
// the running pool follows both changes.
func TestUpdateAccountReconfiguresRunningPool(t *testing.T) {
	a, id, _ := parallelFixture(t, 3)
	p := mustAccountSync(t, a, id).pool
	if p.Configured() != 3 {
		t.Fatalf("start = %d, want 3", p.Configured())
	}
	set, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: new(0)})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Configured(); got != 1 || set.SyncMaxParallel == nil {
		t.Errorf("after override 0 (clamped) = %d (override %v), want 1", got, set.SyncMaxParallel)
	}
	updated, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: nil})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Configured(); got != 3 {
		t.Errorf("after clearing override = %d, want global 3", got)
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

// The ui sends the editor's whole state, so syncMaxParallel null clears the
// override. A request without the field at all (a ui that does not know
// it) leaves the override as it is, so saving an account cannot lose it.
func TestUpdateAccountWithoutSyncMaxParallelKeepsTheOverride(t *testing.T) {
	a, id, _ := parallelFixture(t, 3)
	if _, err := a.UpdateAccount(UpdateAccountRequest{ID: id, SyncMaxParallel: new(2)}); err != nil {
		t.Fatal(err)
	}
	decode := func(payload string) UpdateAccountRequest {
		t.Helper()
		var req UpdateAccountRequest
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			t.Fatal(err)
		}
		return req
	}
	override := func() *int {
		t.Helper()
		acc, err := a.store.GetAccount(a.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return acc.SyncMaxParallel
	}
	if _, err := a.UpdateAccount(decode(fmt.Sprintf(`{"id": %d, "displayName": "Work"}`, id))); err != nil {
		t.Fatal(err)
	}
	if got := override(); got == nil || *got != 2 {
		t.Fatalf("override after a save without the field = %v, want 2 kept", got)
	}
	if _, err := a.UpdateAccount(decode(fmt.Sprintf(`{"id": %d, "syncMaxParallel": null}`, id))); err != nil {
		t.Fatal(err)
	}
	if got := override(); got != nil {
		t.Fatalf("override after null = %d, want cleared", *got)
	}
}

// The ui offers 2..10, but the backend still accepts 1: a stored 1, global
// or per account, stays 1 and is not rewritten. 10 is the top.
func TestStoredParallelOneStaysOne(t *testing.T) {
	a, withOverride, without := parallelFixture(t, 1)
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, withOverride, new(1)); err != nil {
		t.Fatal(err)
	}
	if got := a.syncMaxParallel(); got != 1 {
		t.Errorf("global = %d, want 1", got)
	}
	for _, id := range []int64{withOverride, without} {
		if got := a.accountSyncMaxParallel(id); got != 1 {
			t.Errorf("account %d = %d, want 1", id, got)
		}
		if got := mustAccountSync(t, a, id).pool.Configured(); got != 1 {
			t.Errorf("account %d pool = %d, want 1", id, got)
		}
	}
	if err := a.SetSetting(settingSyncMaxParallel, "11"); err != nil {
		t.Fatal(err)
	}
	if got := mustAccountSync(t, a, without).pool.Configured(); got != 10 {
		t.Errorf("pool after global 11 = %d, want 10", got)
	}
	if _, err := a.UpdateAccount(UpdateAccountRequest{ID: withOverride, SyncMaxParallel: new(0)}); err != nil {
		t.Fatal(err)
	}
	if got := a.accountSyncMaxParallel(withOverride); got != 1 {
		t.Errorf("override 0 = %d, want 1", got)
	}
}
