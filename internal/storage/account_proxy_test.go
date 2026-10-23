package storage

import (
	"context"
	"testing"
)

// Every existing mailbox has to keep following the app-wide proxy, with its
// contacts and sign-in on the same route as its mail.
func TestAccountProxyDefaultsToGlobal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, id := newAccountTestDB(t)

	got, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.Proxy != (AccountProxy{}) {
		t.Errorf("new account proxy = %+v, want the zero value", got.Proxy)
	}
}

func TestCreateAccountStoresProxy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := newAccountTestDB(t)
	want := AccountProxy{Mode: AccountProxyManual, Scheme: "socks5", Host: "10.0.0.1", Port: 1080, Username: "me", OAuthUseGlobal: true}

	id, err := db.CreateAccount(ctx, &Account{Email: "routed@example.com", Proxy: want})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	got, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.Proxy != want {
		t.Errorf("stored proxy = %+v, want %+v", got.Proxy, want)
	}
}

// The route is set on its own, so a regular edit of the account's servers must
// not reset it.
func TestSetAccountProxySurvivesAnUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, id := newAccountTestDB(t)
	want := AccountProxy{Mode: AccountProxyDirect, ContactsUseGlobal: true}

	if err := db.SetAccountProxy(ctx, id, want); err != nil {
		t.Fatalf("set proxy: %v", err)
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
	if got.Proxy != want {
		t.Errorf("proxy after update = %+v, want %+v", got.Proxy, want)
	}
}

func TestSetAccountProxyUnknownAccount(t *testing.T) {
	t.Parallel()
	db, _ := newAccountTestDB(t)
	if err := db.SetAccountProxy(context.Background(), 9999, AccountProxy{}); err != ErrAccountNotFound {
		t.Errorf("SetAccountProxy(unknown) error = %v, want ErrAccountNotFound", err)
	}
}
