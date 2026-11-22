package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newProfileTestDB(t *testing.T) *DB {
	t.Helper()
	ctx := context.Background()
	db, err := openMigrated(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// addProfile creates a profile and returns it, so the tests read as what they
// are testing rather than as setup.
func addProfile(t *testing.T, db *DB, name string, share Profile) Profile {
	t.Helper()
	p := Profile{
		Name:            name,
		ShareSettings:   share.ShareSettings,
		ShareSignatures: share.ShareSignatures,
		ShareViews:      share.ShareViews,
	}
	if _, err := db.CreateProfile(context.Background(), &p); err != nil {
		t.Fatalf("create profile %s: %v", name, err)
	}
	return p
}

// use points the store at a profile the way switching does.
func use(t *testing.T, db *DB, p Profile) {
	t.Helper()
	main, err := db.MainProfile(context.Background())
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	db.UseProfile(p, main.ID)
}

// The migration has to leave every existing install as one profile holding
// everything it already had.
func TestMigrationCreatesTheMainProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)

	profiles, err := db.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("got %d profiles, want 1", len(profiles))
	}
	main := profiles[0]
	if !main.Main {
		t.Error("the only profile is not marked main")
	}
	if !main.Active {
		t.Error("the only profile is not active")
	}
	if main.ShareSettings || main.ShareSignatures || main.ShareViews {
		t.Error("main shares with itself")
	}
}

func TestMainProfileCannotBeDeleted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)

	main, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	if err := db.DeleteProfile(ctx, main.ID); !errors.Is(err, ErrMainProfileUndeletable) {
		t.Errorf("DeleteProfile(main) = %v, want ErrMainProfileUndeletable", err)
	}

	// renaming it is fine, though.
	main.Name = "Personal"
	if err := db.UpdateProfile(ctx, *main); err != nil {
		t.Fatalf("rename main: %v", err)
	}
	got, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	if got.Name != "Personal" {
		t.Errorf("name = %q, want Personal", got.Name)
	}
}

// The whole point of profiles: a setting changed in one is not the other's.
func TestSettingsAreSeparatePerProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)
	work := addProfile(t, db, "Work", Profile{})

	if err := db.Set(ctx, "theme", "dark"); err != nil {
		t.Fatalf("set on main: %v", err)
	}

	use(t, db, work)
	if _, err := db.Get(ctx, "theme"); !errors.Is(err, ErrSettingNotFound) {
		t.Errorf("work sees main's theme: err = %v", err)
	}
	if err := db.Set(ctx, "theme", "light"); err != nil {
		t.Fatalf("set on work: %v", err)
	}

	main, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	use(t, db, *main)
	got, err := db.Get(ctx, "theme")
	if err != nil {
		t.Fatalf("get on main: %v", err)
	}
	if got != "dark" {
		t.Errorf("main's theme = %q, want dark: work wrote over it", got)
	}
}

// Sharing is the other half: a shared area reads and writes main's rows, so a
// change shows up in both.
func TestSharedSettingsWriteThroughToMain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)
	work := addProfile(t, db, "Work", Profile{ShareSettings: true})

	if err := db.Set(ctx, "accent", "#ff0000"); err != nil {
		t.Fatalf("set on main: %v", err)
	}

	use(t, db, work)
	got, err := db.Get(ctx, "accent")
	if err != nil {
		t.Fatalf("get on work: %v", err)
	}
	if got != "#ff0000" {
		t.Errorf("accent = %q, want main's #ff0000", got)
	}

	if err := db.Set(ctx, "accent", "#00ff00"); err != nil {
		t.Fatalf("set on work: %v", err)
	}
	main, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	use(t, db, *main)
	if got, _ := db.Get(ctx, "accent"); got != "#00ff00" {
		t.Errorf("main's accent = %q, want the shared write #00ff00", got)
	}
}

func TestAccountsAreScopedToTheProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)

	mainAccount := Account{Email: "me@example.com"}
	if _, err := db.CreateAccount(ctx, &mainAccount); err != nil {
		t.Fatalf("create account: %v", err)
	}

	work := addProfile(t, db, "Work", Profile{})
	use(t, db, work)

	visible, err := db.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("a new profile already shows %d accounts, want none", len(visible))
	}

	// an account created here belongs here, and stays out of main.
	workAccount := Account{Email: "work@example.com"}
	if _, err := db.CreateAccount(ctx, &workAccount); err != nil {
		t.Fatalf("create work account: %v", err)
	}
	visible, err = db.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(visible) != 1 || visible[0].Email != "work@example.com" {
		t.Errorf("work sees %v, want only work@example.com", visible)
	}

	all, err := db.ListAllAccounts(ctx)
	if err != nil {
		t.Fatalf("list all accounts: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListAllAccounts returned %d, want both", len(all))
	}
}

// One account visible in two profiles is the reason everything lives in one
// database: its mail is cached and synced once.
func TestAnAccountCanBeInTwoProfiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)

	shared := Account{Email: "both@example.com"}
	if _, err := db.CreateAccount(ctx, &shared); err != nil {
		t.Fatalf("create account: %v", err)
	}
	work := addProfile(t, db, "Work", Profile{})
	if err := db.SetProfileAccounts(ctx, work.ID, []int64{shared.ID}); err != nil {
		t.Fatalf("set profile accounts: %v", err)
	}

	use(t, db, work)
	visible, err := db.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(visible) != 1 || visible[0].ID != shared.ID {
		t.Errorf("work sees %v, want the shared account", visible)
	}
}

func TestDeleteProfileLeavesTheAccountsAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)

	account := Account{Email: "me@example.com"}
	if _, err := db.CreateAccount(ctx, &account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	work := addProfile(t, db, "Work", Profile{})
	if err := db.SetProfileAccounts(ctx, work.ID, []int64{account.ID}); err != nil {
		t.Fatalf("set profile accounts: %v", err)
	}
	use(t, db, work)
	if err := db.Set(ctx, "theme", "dark"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err := db.DeleteProfile(ctx, work.ID); err != nil {
		t.Fatalf("delete profile: %v", err)
	}

	main, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	use(t, db, *main)
	all, err := db.ListAllAccounts(ctx)
	if err != nil {
		t.Fatalf("list all accounts: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("deleting a profile took %d accounts with it", 1-len(all))
	}
	// its own settings go, though, so recreating a profile starts clean.
	var count int
	if err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM settings WHERE profile_id = ?`, work.ID).Scan(&count); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if count != 0 {
		t.Errorf("%d settings rows outlived their profile", count)
	}
}

func TestOnlyOneProfileIsActive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)
	work := addProfile(t, db, "Work", Profile{})

	if err := db.SetActiveProfile(ctx, work.ID); err != nil {
		t.Fatalf("activate: %v", err)
	}
	active, err := db.ActiveProfile(ctx)
	if err != nil {
		t.Fatalf("active profile: %v", err)
	}
	if active.ID != work.ID {
		t.Errorf("active = %d, want work %d", active.ID, work.ID)
	}

	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles WHERE is_active = 1`).Scan(&count); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if count != 1 {
		t.Errorf("%d profiles are active, want exactly 1", count)
	}
}

// "Copy from" is a snapshot: editing the copy afterwards must not touch the
// original, which is what makes it different from sharing.
func TestCopySettingsIsASnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)
	if err := db.Set(ctx, "density", "compact"); err != nil {
		t.Fatalf("set: %v", err)
	}
	main, err := db.MainProfile(ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}

	work := addProfile(t, db, "Work", Profile{})
	if err := db.CopyProfileSettings(ctx, main.ID, work.ID); err != nil {
		t.Fatalf("copy settings: %v", err)
	}

	use(t, db, work)
	if got, _ := db.Get(ctx, "density"); got != "compact" {
		t.Errorf("copied density = %q, want compact", got)
	}
	if err := db.Set(ctx, "density", "relaxed"); err != nil {
		t.Fatalf("set on work: %v", err)
	}

	use(t, db, *main)
	if got, _ := db.Get(ctx, "density"); got != "compact" {
		t.Errorf("main's density = %q, want compact: the copy wrote back", got)
	}
}

func TestSignaturesAndViewsFollowTheProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)

	sig := Signature{Name: "Work block", Kind: "footer", Format: "markdown", Content: "Regards"}
	if _, err := db.CreateSignature(ctx, &sig); err != nil {
		t.Fatalf("create signature: %v", err)
	}
	view := View{Name: "Unread", Position: 1}
	if _, err := db.CreateView(ctx, &view); err != nil {
		t.Fatalf("create view: %v", err)
	}

	work := addProfile(t, db, "Work", Profile{})
	use(t, db, work)

	sigs, err := db.ListSignatures(ctx)
	if err != nil {
		t.Fatalf("list signatures: %v", err)
	}
	if len(sigs) != 0 {
		t.Errorf("work sees %d of main's signatures", len(sigs))
	}
	views, err := db.ListViews(ctx)
	if err != nil {
		t.Fatalf("list views: %v", err)
	}
	if len(views) != 0 {
		t.Errorf("work sees %d of main's views", len(views))
	}
}

func TestSharedSignaturesAndViewsAreVisible(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newProfileTestDB(t)

	sig := Signature{Name: "Shared", Kind: "footer", Format: "markdown", Content: "Bye"}
	if _, err := db.CreateSignature(ctx, &sig); err != nil {
		t.Fatalf("create signature: %v", err)
	}
	view := View{Name: "Flagged", Position: 1}
	if _, err := db.CreateView(ctx, &view); err != nil {
		t.Fatalf("create view: %v", err)
	}

	work := addProfile(t, db, "Work", Profile{ShareSignatures: true, ShareViews: true})
	use(t, db, work)

	if sigs, _ := db.ListSignatures(ctx); len(sigs) != 1 {
		t.Errorf("shared signatures not visible: got %d", len(sigs))
	}
	if views, _ := db.ListViews(ctx); len(views) != 1 {
		t.Errorf("shared views not visible: got %d", len(views))
	}
}
