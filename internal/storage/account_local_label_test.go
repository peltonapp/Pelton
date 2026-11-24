package storage

import (
	"context"
	"testing"
)

func TestAccountLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		account Account
		want    string
	}{
		{
			name:    "no label falls back to the from name",
			account: Account{DisplayName: "Arne Kock"},
			want:    "Arne Kock",
		},
		{
			name:    "label in use wins",
			account: Account{DisplayName: "Arne Kock", LocalLabel: "work junk", UseLocalLabel: true},
			want:    "work junk",
		},
		{
			name:    "label stored but switched off is not used",
			account: Account{DisplayName: "Arne Kock", LocalLabel: "work junk"},
			want:    "Arne Kock",
		},
		{
			name:    "empty label switched on is not a name",
			account: Account{DisplayName: "Arne Kock", UseLocalLabel: true},
			want:    "Arne Kock",
		},
		{
			name:    "nothing set leaves the caller to fall back to the address",
			account: Account{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.account.Label(); got != tt.want {
				t.Errorf("Label() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLocalLabelDefaultsOff is the migration guard: accounts that existed
// before the column keep showing the name they always did.
func TestLocalLabelDefaultsOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	got, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.UseLocalLabel || got.LocalLabel != "" {
		t.Errorf("new account has label %q in use %v, want off and empty", got.LocalLabel, got.UseLocalLabel)
	}
}

// TestUpdateAccountKeepsLabelWhenSwitchedOff covers the reason the toggle is a
// column of its own: turning it off has to leave the name where it was.
func TestUpdateAccountKeepsLabelWhenSwitchedOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	account, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	account.DisplayName = "Arne Kock"
	account.LocalLabel = "work junk"
	account.UseLocalLabel = true
	if err := db.UpdateAccount(ctx, account); err != nil {
		t.Fatalf("update account: %v", err)
	}

	stored, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if stored.Label() != "work junk" {
		t.Fatalf("Label() = %q, want %q", stored.Label(), "work junk")
	}

	stored.UseLocalLabel = false
	if err := db.UpdateAccount(ctx, stored); err != nil {
		t.Fatalf("update account: %v", err)
	}
	off, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if off.LocalLabel != "work junk" {
		t.Errorf("LocalLabel = %q after switching off, want it kept", off.LocalLabel)
	}
	if off.Label() != "Arne Kock" {
		t.Errorf("Label() = %q, want the from name back", off.Label())
	}
}
