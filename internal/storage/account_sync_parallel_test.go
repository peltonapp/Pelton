package storage

import (
	"context"
	"testing"
)

// TestAccountSyncMaxParallelDefaultsToNull is the migration guard: an account
// with no override reads back nil, meaning "use the global setting".
func TestAccountSyncMaxParallelDefaultsToNull(t *testing.T) {
	db, id := newAccountTestDB(t)
	got, err := db.GetAccount(context.Background(), id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.SyncMaxParallel != nil {
		t.Errorf("SyncMaxParallel = %d, want nil", *got.SyncMaxParallel)
	}
}

func TestSetAccountSyncMaxParallel(t *testing.T) {
	ctx := context.Background()
	db, id := newAccountTestDB(t)
	two := 2

	for _, c := range []struct {
		name string
		in   *int
		want *int
	}{
		{"set", &two, &two},
		{"cleared", nil, nil},
	} {
		if err := db.SetAccountSyncMaxParallel(ctx, id, c.in); err != nil {
			t.Fatalf("%s: set: %v", c.name, err)
		}
		got, err := db.GetAccount(ctx, id)
		if err != nil {
			t.Fatalf("%s: get: %v", c.name, err)
		}
		switch {
		case c.want == nil && got.SyncMaxParallel != nil:
			t.Errorf("%s: got %d, want nil", c.name, *got.SyncMaxParallel)
		case c.want != nil && (got.SyncMaxParallel == nil || *got.SyncMaxParallel != *c.want):
			t.Errorf("%s: got %v, want %d", c.name, got.SyncMaxParallel, *c.want)
		}
	}
}
