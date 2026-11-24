package storage

import (
	"context"
	"testing"
	"time"
)

func TestAccountSieveDefaultsAndUpdates(t *testing.T) {
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	got, err := db.GetAccountSieve(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != (AccountSieve{AccountID: id}) {
		t.Fatalf("an account without a row = %+v, want the zero value", got)
	}

	want := AccountSieve{
		AccountID: id, Host: "sieve.example.org", Port: 2000, TLS: "ssl",
		Unsupported: true, CheckedAt: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC),
		PreviousActive: "roundcube", HasPrevious: true,
	}
	if err := db.SetAccountSieve(ctx, want); err != nil {
		t.Fatalf("set: %v", err)
	}
	want.Unsupported = false
	if err := db.SetAccountSieve(ctx, want); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = db.GetAccountSieve(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.CheckedAt.Equal(want.CheckedAt) {
		t.Errorf("checked at = %v, want %v", got.CheckedAt, want.CheckedAt)
	}
	got.CheckedAt = want.CheckedAt
	if got != want {
		t.Errorf("stored = %+v, want %+v", got, want)
	}

	if err := db.SetAccountSieve(ctx, AccountSieve{AccountID: 9999}); err != ErrAccountNotFound {
		t.Errorf("set for an unknown account = %v, want ErrAccountNotFound", err)
	}
	if err := db.DeleteAccount(ctx, id); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if got, _ := db.GetAccountSieve(ctx, id); got.Host != "" {
		t.Error("the sieve row outlived its account")
	}
}
