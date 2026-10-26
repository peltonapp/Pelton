package storage

import (
	"context"
	"testing"
)

func TestAccountSyncStateRecordsFailureAndRecovery(t *testing.T) {
	t.Parallel()
	db, id := newAccountTestDB(t)
	ctx := context.Background()

	// nothing recorded yet is absent rather than failing: an account that has
	// never synced is not a broken one.
	states, err := db.AccountSyncStates(ctx)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("states = %v, want none before any sync", states)
	}

	if err := db.RecordSyncOK(ctx, id); err != nil {
		t.Fatalf("record ok: %v", err)
	}
	states, err = db.AccountSyncStates(ctx)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	if len(states) != 1 || states[0].Failing() {
		t.Fatalf("states = %+v, want one that is not failing", states)
	}
	firstOK := states[0].LastOK
	if firstOK.IsZero() {
		t.Error("a successful sync recorded no time")
	}

	if err := db.RecordSyncFailure(ctx, id, "auth", "LOGIN failed"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	states, _ = db.AccountSyncStates(ctx)
	if len(states) != 1 || !states[0].Failing() {
		t.Fatalf("states = %+v, want one that is failing", states)
	}
	// the last success is what says how long it has been broken, so a failure
	// must not take it with it.
	if !states[0].LastOK.Equal(firstOK) {
		t.Errorf("LastOK = %v, want the earlier %v kept", states[0].LastOK, firstOK)
	}
	if states[0].Reason != "auth" || states[0].Detail != "LOGIN failed" {
		t.Errorf("reason/detail = %q/%q, want auth/LOGIN failed", states[0].Reason, states[0].Detail)
	}

	if err := db.RecordSyncOK(ctx, id); err != nil {
		t.Fatalf("record ok again: %v", err)
	}
	states, _ = db.AccountSyncStates(ctx)
	if states[0].Failing() || states[0].Reason != "" || states[0].Detail != "" {
		t.Errorf("a working sync left the failure behind: %+v", states[0])
	}
}

// deleting an account must not leave its sync state behind to be reported
// against whatever id sqlite hands out next.
func TestAccountSyncStateGoesWithTheAccount(t *testing.T) {
	t.Parallel()
	db, id := newAccountTestDB(t)
	ctx := context.Background()

	if err := db.RecordSyncFailure(ctx, id, "network", "dial tcp: timeout"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	if err := db.DeleteAccount(ctx, id); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	states, err := db.AccountSyncStates(ctx)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("states = %+v, want none after the account was deleted", states)
	}
}
