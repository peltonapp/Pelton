package storage

import (
	"context"
	"testing"
)

func TestSetAccountColorSurvivesAnUpdate(t *testing.T) {
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	if err := db.SetAccountColor(ctx, id, "teal"); err != nil {
		t.Fatalf("set color: %v", err)
	}
	acct, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	acct.IMAPPort = 1143
	if err := db.UpdateAccount(ctx, acct); err != nil {
		t.Fatalf("update account: %v", err)
	}
	got, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.Color != "teal" {
		t.Errorf("color = %q, want teal", got.Color)
	}
	if err := db.SetAccountColor(ctx, 9999, "red"); err != ErrAccountNotFound {
		t.Errorf("SetAccountColor(unknown) error = %v, want ErrAccountNotFound", err)
	}
}
