package storage

import (
	"context"
	"slices"
	"testing"
)

// An account nobody widened trust for must read back with none, which is what
// keeps every existing mailbox on the standard verification.
func TestCertTrustDefaultsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	got, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if len(got.TrustedCerts) != 0 || got.CAPEM != "" {
		t.Errorf("new account trusts %v and %q, want nothing", got.TrustedCerts, got.CAPEM)
	}
}

func TestCreateAccountStoresCertTrust(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := newAccountTestDB(t)
	pins := []string{"aa11", "bb22"}

	id, err := db.CreateAccount(ctx, &Account{Email: "bridge@proton.example", TrustedCerts: pins, CAPEM: "PEM"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	got, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if !slices.Equal(got.TrustedCerts, pins) || got.CAPEM != "PEM" {
		t.Errorf("stored trust = %v, %q, want %v, %q", got.TrustedCerts, got.CAPEM, pins, "PEM")
	}
}

// Trust is set on its own, so a regular edit of the account's servers must not
// wipe the certificates the user accepted.
func TestSetAccountCertTrustSurvivesAnUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	if err := db.SetAccountCertTrust(ctx, id, []string{"cc33"}, "CA"); err != nil {
		t.Fatalf("set trust: %v", err)
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
	if !slices.Equal(got.TrustedCerts, []string{"cc33"}) || got.CAPEM != "CA" {
		t.Errorf("trust after update = %v, %q, want it kept", got.TrustedCerts, got.CAPEM)
	}

	if err := db.SetAccountCertTrust(ctx, id, nil, ""); err != nil {
		t.Fatalf("clear trust: %v", err)
	}
	got, err = db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if len(got.TrustedCerts) != 0 || got.CAPEM != "" {
		t.Errorf("trust after clearing = %v, %q, want none", got.TrustedCerts, got.CAPEM)
	}
}

func TestSetAccountCertTrustUnknownAccount(t *testing.T) {
	t.Parallel()
	db, _ := newAccountTestDB(t)
	if err := db.SetAccountCertTrust(context.Background(), 9999, nil, ""); err != ErrAccountNotFound {
		t.Errorf("SetAccountCertTrust(unknown) error = %v, want ErrAccountNotFound", err)
	}
}
