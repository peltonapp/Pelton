package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// prefsWithTwoImapAndOnePop3 is a minimal Thunderbird prefs.js: two imap
// accounts that can be imported and a pop3 one that cannot.
const prefsWithTwoImapAndOnePop3 = `// Mozilla User Preferences
user_pref("mail.accountmanager.accounts", "account1,account2,account3");

user_pref("mail.account.account1.server", "server1");
user_pref("mail.account.account1.identities", "id1");
user_pref("mail.server.server1.type", "imap");
user_pref("mail.server.server1.hostname", "imap.one.test");
user_pref("mail.server.server1.port", 993);
user_pref("mail.server.server1.userName", "one-login");
user_pref("mail.identity.id1.useremail", "one@example.test");
user_pref("mail.identity.id1.fullName", "Account One");
user_pref("mail.identity.id1.smtpServer", "smtp1");
user_pref("mail.smtpserver.smtp1.hostname", "smtp.one.test");
user_pref("mail.smtpserver.smtp1.port", 587);

user_pref("mail.account.account2.server", "server2");
user_pref("mail.account.account2.identities", "id2");
user_pref("mail.server.server2.type", "imap");
user_pref("mail.server.server2.hostname", "imap.two.test");
user_pref("mail.server.server2.port", 993);
user_pref("mail.identity.id2.useremail", "two@example.test");
user_pref("mail.identity.id2.fullName", "Account Two");

user_pref("mail.account.account3.server", "server3");
user_pref("mail.account.account3.identities", "id3");
user_pref("mail.server.server3.type", "pop3");
user_pref("mail.server.server3.hostname", "pop.three.test");
user_pref("mail.server.server3.port", 995);
user_pref("mail.identity.id3.useremail", "three@example.test");
`

// importTestApp is an app with a real store and a stubbed account start, so an
// import can be run without opening a connection or touching the os keyring.
func importTestApp(t *testing.T) (*App, *[]int64) {
	t.Helper()
	a := newAccountTestApp(t)
	var started []int64
	a.startAccount = func(id int64) error {
		started = append(started, id)
		return nil
	}
	return a, &started
}

// thunderbirdProfile writes a profile directory holding the given prefs.js.
func thunderbirdProfile(t *testing.T, prefs string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatalf("write prefs.js: %v", err)
	}
	return dir
}

func accountIDsByEmail(t *testing.T, a *App) map[string]int64 {
	t.Helper()
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	out := make(map[string]int64, len(accounts))
	for _, acc := range accounts {
		out[acc.Email] = acc.ID
	}
	return out
}

// The bug this guards: the import created the account rows and stopped there.
// Nothing synced them and nothing parked them on idle, so they never recorded
// that they could not log in, carried no warning marker, and received no mail
// while looking perfectly healthy.
func TestImportStartsEveryAccountItCreates(t *testing.T) {
	a, started := importTestApp(t)
	profile := thunderbirdProfile(t, prefsWithTwoImapAndOnePop3)

	created, err := a.ImportThunderbirdAccounts(profile, []string{"one@example.test", "two@example.test"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if created != 2 {
		t.Fatalf("created = %d, want 2", created)
	}

	ids := accountIDsByEmail(t, a)
	want := []int64{ids["one@example.test"], ids["two@example.test"]}
	slices.Sort(want)
	got := append([]int64(nil), *started...)
	slices.Sort(got)

	if len(got) != len(want) {
		t.Fatalf("started %v, want the %d imported accounts %v", got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("started %v, want %v", got, want)
		}
	}
}

// A pop3 account cannot be imported, so it must not be created and must not be
// started either.
func TestImportSkipsPop3(t *testing.T) {
	a, started := importTestApp(t)
	profile := thunderbirdProfile(t, prefsWithTwoImapAndOnePop3)

	created, err := a.ImportThunderbirdAccounts(profile, []string{"three@example.test"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if created != 0 {
		t.Fatalf("created = %d, want 0 for a pop3 account", created)
	}
	if len(*started) != 0 {
		t.Fatalf("started %v, want nothing", *started)
	}
}

// Importing the same profile twice must not start the accounts a second time:
// they are already running, and a duplicate start would open a second login for
// a mailbox that already has one.
func TestImportDoesNotRestartAccountsItAlreadyHas(t *testing.T) {
	a, started := importTestApp(t)
	profile := thunderbirdProfile(t, prefsWithTwoImapAndOnePop3)
	emails := []string{"one@example.test", "two@example.test"}

	if _, err := a.ImportThunderbirdAccounts(profile, emails); err != nil {
		t.Fatalf("first import: %v", err)
	}
	first := len(*started)

	created, err := a.ImportThunderbirdAccounts(profile, emails)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if created != 0 {
		t.Fatalf("second import created = %d, want 0", created)
	}
	if len(*started) != first {
		t.Fatalf("second import started %d more accounts, want 0", len(*started)-first)
	}
}

// The server settings have to survive the import, since not retyping six
// servers is the point of it.
func TestImportKeepsTheServerSettings(t *testing.T) {
	a, _ := importTestApp(t)
	profile := thunderbirdProfile(t, prefsWithTwoImapAndOnePop3)

	if _, err := a.ImportThunderbirdAccounts(profile, []string{"one@example.test"}); err != nil {
		t.Fatalf("import: %v", err)
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("got %d accounts, want 1", len(accounts))
	}
	acc := accounts[0]
	if acc.IMAPHost != "imap.one.test" || acc.IMAPPort != 993 {
		t.Errorf("imap = %s:%d, want imap.one.test:993", acc.IMAPHost, acc.IMAPPort)
	}
	if acc.SMTPHost != "smtp.one.test" || acc.SMTPPort != 587 {
		t.Errorf("smtp = %s:%d, want smtp.one.test:587", acc.SMTPHost, acc.SMTPPort)
	}
	// the login name differs from the address here, and logging in as the
	// address would fail.
	if acc.Username != "one-login" {
		t.Errorf("username = %q, want one-login", acc.Username)
	}
}
