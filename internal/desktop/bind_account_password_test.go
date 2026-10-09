package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

func newAccountTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler)}
}

// needsPassword is tested rather than AccountsNeedingPassword because the
// latter reads the real OS keyring, where account ids collide with whatever the
// developer running the tests actually has installed.
func TestNeedsPassword(t *testing.T) {
	a := newAccountTestApp(t)
	acct := storage.Account{ID: 1, Email: "imported@example.test", IMAPHost: "imap.example.test", IMAPPort: 993}

	for _, tt := range []struct {
		name      string
		secretErr error
		want      bool
	}{
		// an account imported from another mail client: the import creates the
		// row but cannot take the password with it.
		{"nothing stored", credentials.ErrNotFound, true},
		{"a secret exists", nil, false},
		// a locked or broken keyring is not the same as no password, and
		// prompting would ask for one the user already gave.
		{"keyring unreadable", errors.New("keyring: access denied"), false},
		{"wrapped not found", fmt.Errorf("load: %w", credentials.ErrNotFound), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.needsPassword(acct, tt.secretErr); got != tt.want {
				t.Errorf("needsPassword(%v) = %v, want %v", tt.secretErr, got, tt.want)
			}
		})
	}
}

// The legacy cli account keeps its password in the environment, so it is not
// missing one even with an empty keyring.
func TestEnvBackedAccountDoesNotNeedAPassword(t *testing.T) {
	a := newAccountTestApp(t)
	acct := storage.Account{ID: 1, Email: "cli@example.test"}
	t.Setenv("IMAP_USER", "cli@example.test")
	t.Setenv("IMAP_PASSWORD", "from-the-environment")

	if a.needsPassword(acct, credentials.ErrNotFound) {
		t.Error("an env-backed account was reported as needing a password")
	}
}

// Local Folders holds imported mail and has no server, so an empty keyring is
// its normal state rather than a missing password.
func TestLocalAccountDoesNotNeedAPassword(t *testing.T) {
	a := newAccountTestApp(t)
	acct := storage.Account{ID: 1, Email: storage.LocalAccountEmail, Local: true}

	if a.needsPassword(acct, credentials.ErrNotFound) {
		t.Error("the Local Folders account was reported as needing a password")
	}
}

// A stored password the server refuses looks fine to the keyring, so the only
// way to know is what the last login said.
func TestNoteLoginResult(t *testing.T) {
	a := newAccountTestApp(t)

	if a.loginRejected(1) {
		t.Error("an account with no login attempt yet is reported as rejected")
	}

	a.noteLoginResult(1, fmt.Errorf("login as %q: %w", "me", imap.ErrAuthFailed))
	if !a.loginRejected(1) {
		t.Error("a refused login was not remembered")
	}

	// a server that is down, or a dropped connection, says nothing about the
	// password: the mark has to survive it rather than being cleared.
	a.noteLoginResult(1, errors.New("dial tcp: connection refused"))
	if !a.loginRejected(1) {
		t.Error("a network error cleared the refused-login mark")
	}

	a.noteLoginResult(1, nil)
	if a.loginRejected(1) {
		t.Error("a successful login did not clear the mark")
	}
}

// A network error on an account that was fine must not mark it.
func TestNoteLoginResultIgnoresNetworkErrors(t *testing.T) {
	a := newAccountTestApp(t)

	a.noteLoginResult(2, errors.New("dial tcp: no route to host"))
	if a.loginRejected(2) {
		t.Error("a network error was reported as a refused login")
	}
}

func TestEmptyPasswordIsRefused(t *testing.T) {
	a := newAccountTestApp(t)
	if err := a.SetAccountPassword(1, ""); !errors.Is(err, errEmptyPassword) {
		t.Errorf("SetAccountPassword(empty) = %v, want errEmptyPassword", err)
	}
}

// imapFromEnv is what the env check leans on; pin its contract so a change
// there cannot silently start prompting for every account.
func TestImapFromEnvNeedsAMatchingUser(t *testing.T) {
	a := newAccountTestApp(t)
	t.Setenv("IMAP_USER", "someone@example.test")
	t.Setenv("IMAP_PASSWORD", "secret")

	if _, err := a.imapFromEnv(imap.Config{Username: "other@example.test"}); err == nil {
		t.Error("a mismatched IMAP_USER was accepted")
	}
	if _, err := a.imapFromEnv(imap.Config{Username: "someone@example.test"}); err != nil {
		t.Errorf("a matching IMAP_USER was rejected: %v", err)
	}
}

// the reachable paths of CheckAccountPassword need a server and a keyring, so
// what is pinned here is that the two arguments it can reject on its own are
// rejected before either is touched.
func TestCheckAccountPasswordRejectsBadArguments(t *testing.T) {
	a := newAccountTestApp(t)

	if _, err := a.CheckAccountPassword(1, ""); !errors.Is(err, errEmptyPassword) {
		t.Errorf("CheckAccountPassword(empty) = %v, want errEmptyPassword", err)
	}
	if _, err := a.CheckAccountPassword(404, "secret"); err == nil {
		t.Error("CheckAccountPassword accepted an account that does not exist")
	}
}

// the sidebar mark and the sentence under it both come from this classification,
// so a network blip must never read as a rejected password.
func TestSyncFailureReason(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"nothing stored", errNoCredentials, syncFailCredentials},
		{"wrapped no credentials", fmt.Errorf("resolve: %w", errNoCredentials), syncFailCredentials},
		{"server refused the login", fmt.Errorf("login as %q: %w", "me", imap.ErrAuthFailed), syncFailAuth},
		{"dial failed", errors.New("dial tcp 1.2.3.4:993: connection refused"), syncFailNetwork},
		{"dns failed", errors.New("lookup imap.example.test: no such host"), syncFailNetwork},
		{"anything else", errors.New("SELECT INBOX: unexpected response"), syncFailOther},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := syncFailureReason(tt.err); got != tt.want {
				t.Errorf("syncFailureReason(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// a recorded failure has to survive the run it happened in, and a later success
// has to clear it: that is the whole difference from the log line it replaces.
func TestNoteSyncOutcomeRecordsAndClears(t *testing.T) {
	a := newAccountTestApp(t)
	id, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@example.test"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	a.noteSyncOutcome(id, fmt.Errorf("login as %q: %w", "me", imap.ErrAuthFailed))
	states, err := a.AccountSyncStates()
	if err != nil {
		t.Fatalf("read states: %v", err)
	}
	if len(states) != 1 || states[0].FailedAt == "" || states[0].Reason != syncFailAuth {
		t.Fatalf("states = %+v, want one auth failure", states)
	}
	if states[0].LastOK != "" {
		t.Errorf("LastOK = %q, want empty for an account that never synced", states[0].LastOK)
	}

	a.noteSyncOutcome(id, nil)
	states, _ = a.AccountSyncStates()
	if len(states) != 1 || states[0].FailedAt != "" || states[0].Reason != "" {
		t.Errorf("states = %+v, want the failure cleared", states)
	}
}

// stop, switch and shutdown cancel a running sync on purpose; that must not be
// recorded as the account failing, while a real network error still is.
func TestNoteSyncOutcomeIgnoresIntentionalCancel(t *testing.T) {
	a := newAccountTestApp(t)
	id, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@example.test"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	a.noteSyncOutcome(id, fmt.Errorf("sync: %w", context.Canceled))
	states, _ := a.AccountSyncStates()
	for _, s := range states {
		if s.FailedAt != "" {
			t.Fatalf("cancel recorded as failure: %+v", s)
		}
	}

	a.noteSyncOutcome(id, errors.New("dial tcp 1.2.3.4:993: connection refused"))
	states, _ = a.AccountSyncStates()
	if len(states) != 1 || states[0].FailedAt == "" || states[0].Reason != syncFailNetwork {
		t.Fatalf("states = %+v, want a network failure", states)
	}
}
