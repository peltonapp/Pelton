package desktop

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/logging"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func newProfileApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// switching re-applies the log settings of the profile it lands in, so the
	// app under test needs the real writer rather than a bare logger.
	w := logging.NewWriter()
	return &App{ctx: ctx, store: store, log: w.Logger(), logWriter: w}
}

// A fresh install is one profile holding what it always had.
func TestListProfilesStartsWithMain(t *testing.T) {
	a := newProfileApp(t)

	profiles, err := a.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("got %d profiles, want 1", len(profiles))
	}
	if !profiles[0].Main || !profiles[0].Active {
		t.Errorf("the only profile is %+v, want main and active", profiles[0])
	}
}

func TestCreateProfileNeedsAName(t *testing.T) {
	a := newProfileApp(t)

	if _, err := a.CreateProfile(ProfileRequest{Name: "   "}); !errors.Is(err, errProfileNameRequired) {
		t.Errorf("CreateProfile(blank) = %v, want errProfileNameRequired", err)
	}
}

// Copy and share look the same on the day you make them and differ from then
// on: a copy stops tracking, a share does not.
func TestCreateProfileCopiesAndShares(t *testing.T) {
	a := newProfileApp(t)
	if err := a.store.Set(a.ctx, "theme", "dark"); err != nil {
		t.Fatalf("set: %v", err)
	}

	copied, err := a.CreateProfile(ProfileRequest{
		Name:            "Work",
		StartSettings:   startCopy,
		StartSignatures: startFresh,
		StartViews:      startFresh,
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if copied.ShareSettings {
		t.Error("a copied area is marked as shared")
	}

	shared, err := a.CreateProfile(ProfileRequest{Name: "Family", StartSettings: startShare})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if !shared.ShareSettings {
		t.Error("a shared area is not marked as shared")
	}

	// the copy has main's value of the day, without the link.
	if err := a.SwitchProfile(copied.ID); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if got, err := a.store.Get(a.ctx, "theme"); err != nil || got != "dark" {
		t.Errorf("copied theme = %q (%v), want dark", got, err)
	}
}

func TestSwitchProfileChangesWhatIsVisible(t *testing.T) {
	a := newProfileApp(t)

	mainAccount := storage.Account{Email: "me@example.com"}
	if _, err := a.store.CreateAccount(a.ctx, &mainAccount); err != nil {
		t.Fatalf("create account: %v", err)
	}

	work, err := a.CreateProfile(ProfileRequest{Name: "Work", StartSettings: startFresh})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if err := a.SwitchProfile(work.ID); err != nil {
		t.Fatalf("SwitchProfile: %v", err)
	}

	active, err := a.ActiveProfile()
	if err != nil {
		t.Fatalf("ActiveProfile: %v", err)
	}
	if active.ID != work.ID {
		t.Errorf("active = %d, want work %d", active.ID, work.ID)
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("work shows %d of main's accounts", len(accounts))
	}
}

// Switching has to stop the profile's background work, or the app keeps
// watching mailboxes the new profile cannot see.
func TestSwitchProfileEndsTheSession(t *testing.T) {
	a := newProfileApp(t)
	before := a.sessionCtx()

	work, err := a.CreateProfile(ProfileRequest{Name: "Work"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if err := a.SwitchProfile(work.ID); err != nil {
		t.Fatalf("SwitchProfile: %v", err)
	}

	if before.Err() == nil {
		t.Error("the previous profile's session is still running")
	}
	if a.sessionCtx().Err() != nil {
		t.Error("the new profile has no live session")
	}
}

func TestDeleteMainProfileIsRefused(t *testing.T) {
	a := newProfileApp(t)
	main, err := a.store.MainProfile(a.ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}

	if err := a.DeleteProfile(main.ID); !errors.Is(err, errMainProfileDelete) {
		t.Errorf("DeleteProfile(main) = %v, want errMainProfileDelete", err)
	}
}

// Deleting the profile you are in has to leave the app somewhere.
func TestDeleteActiveProfileFallsBackToMain(t *testing.T) {
	a := newProfileApp(t)
	main, err := a.store.MainProfile(a.ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}
	work, err := a.CreateProfile(ProfileRequest{Name: "Work"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if err := a.SwitchProfile(work.ID); err != nil {
		t.Fatalf("SwitchProfile: %v", err)
	}

	if err := a.DeleteProfile(work.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}

	active, err := a.ActiveProfile()
	if err != nil {
		t.Fatalf("ActiveProfile: %v", err)
	}
	if active.ID != main.ID {
		t.Errorf("active = %d after deleting the profile we were in, want main %d", active.ID, main.ID)
	}
}

func TestUpdateProfileRenamesMainWithoutSharing(t *testing.T) {
	a := newProfileApp(t)
	main, err := a.store.MainProfile(a.ctx)
	if err != nil {
		t.Fatalf("main profile: %v", err)
	}

	got, err := a.UpdateProfile(ProfileRequest{
		ID:   main.ID,
		Name: "Personal",
		// main shares from nobody, so this must be ignored rather than making
		// it point at itself.
		StartSettings: startShare,
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if got.Name != "Personal" {
		t.Errorf("name = %q, want Personal", got.Name)
	}
	if got.ShareSettings {
		t.Error("main was marked as sharing with itself")
	}
}

func TestTrimIconKeepsAGlyphOrTwo(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"💼", "💼"},
		{" 🏠 ", "🏠"},
		{"", ""},
		{"abcdefghijkl", "abcdefgh"},
	} {
		if got := trimIcon(tt.in); got != tt.want {
			t.Errorf("trimIcon(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
