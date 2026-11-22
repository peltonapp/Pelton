// bind_profiles.go is the profiles feature (#270): one install keeping work and
// private life apart without two copies of the app.
//
// A profile is a scope, not a second installation. It owns which accounts it
// shows, its settings, its signatures and its saved views; the mail itself
// belongs to the install, so an account visible in two profiles is cached and
// synced once. Passwords are keyed by account for the same reason: the same
// mailbox in two profiles is one login.
//
// Switching happens in place. The profile session (idle loops, background sync)
// is cancelled, the store is re-scoped, and the frontend is told to reload. No
// relaunch, and no idle loop left watching a mailbox the new profile cannot see.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/peltonapp/Pelton/internal/storage"
)

// how a new profile starts out in one area.
const (
	// startShare links the area to the main profile: a change in either is a
	// change in both.
	startShare = "share"
	// startCopy duplicates main's rows once. Independent from then on.
	startCopy = "copy"
	// startFresh leaves the area empty, so it falls back to the defaults.
	startFresh = "fresh"
)

var (
	errProfileNameRequired = errors.New("a profile needs a name")
	errMainProfileDelete   = errors.New("the main profile cannot be deleted")
)

// maxProfileIcon caps the icon at a couple of glyphs. It is a label, not a
// place to paste a paragraph.
const maxProfileIcon = 8

// ProfileDTO is one profile as the ui sees it.
type ProfileDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
	// Main marks the profile the install started as: renamable, never deletable.
	Main bool `json:"main"`
	// Active marks the profile the app is currently in.
	Active bool `json:"active"`
	// ShareSettings, ShareSignatures and ShareViews are the live links to the
	// main profile. A copied area reads false here: it was a one-off duplicate.
	ShareSettings   bool `json:"shareSettings"`
	ShareSignatures bool `json:"shareSignatures"`
	ShareViews      bool `json:"shareViews"`
	// ShareLayout links the sidebar layout to main: pinned folders and the order
	// of folders and account sections.
	ShareLayout bool `json:"shareLayout"`
	// AccountIDs are the accounts this profile shows.
	AccountIDs []int64 `json:"accountIds"`
}

// ProfileRequest creates or updates a profile. On create, the three Start
// fields say where each area comes from ("share", "copy" or "fresh"); on
// update only the sharing part of that is still meaningful, since copying is a
// one-off that already happened.
type ProfileRequest struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Icon       string  `json:"icon"`
	AccountIDs []int64 `json:"accountIds"`
	// StartSettings, StartSignatures and StartViews are one of the start
	// constants. An unknown value is treated as "fresh", which is the choice
	// that copies nothing and links nothing.
	StartSettings   string `json:"startSettings"`
	StartSignatures string `json:"startSignatures"`
	StartViews      string `json:"startViews"`
	StartLayout     string `json:"startLayout"`
}

// ListProfiles returns every profile on the install, main first.
func (a *App) ListProfiles() ([]ProfileDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	profiles, err := a.store.ListProfiles(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProfileDTO, 0, len(profiles))
	for _, p := range profiles {
		dto, err := a.toProfileDTO(p)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// ActiveProfile returns the profile the app is in.
func (a *App) ActiveProfile() (ProfileDTO, error) {
	if err := a.ready(); err != nil {
		return ProfileDTO{}, err
	}
	active, err := a.store.ActiveProfile(a.ctx)
	if err != nil {
		return ProfileDTO{}, err
	}
	return a.toProfileDTO(*active)
}

// CreateProfile adds a profile and returns it. Areas set to "copy" are
// duplicated from main at this point, which is what makes a copy different from
// sharing: it never changes again on its own.
func (a *App) CreateProfile(req ProfileRequest) (ProfileDTO, error) {
	if err := a.ready(); err != nil {
		return ProfileDTO{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ProfileDTO{}, errProfileNameRequired
	}

	main, err := a.store.MainProfile(a.ctx)
	if err != nil {
		return ProfileDTO{}, err
	}

	profile := storage.Profile{
		Name:            name,
		Icon:            trimIcon(req.Icon),
		ShareSettings:   req.StartSettings == startShare,
		ShareSignatures: req.StartSignatures == startShare,
		ShareViews:      req.StartViews == startShare,
		ShareLayout:     req.StartLayout == startShare,
	}
	if _, err := a.store.CreateProfile(a.ctx, &profile); err != nil {
		return ProfileDTO{}, err
	}
	if err := a.store.SetProfileAccounts(a.ctx, profile.ID, req.AccountIDs); err != nil {
		return ProfileDTO{}, err
	}
	if err := a.copyStartingRows(main.ID, profile.ID, req); err != nil {
		return ProfileDTO{}, err
	}
	return a.toProfileDTO(profile)
}

// copyStartingRows duplicates main's rows for every area the request asked to
// copy. A failure part way through leaves the profile with some areas copied,
// which is recoverable by hand and better than refusing to create it at all,
// so it is reported rather than rolled back.
func (a *App) copyStartingRows(mainID, profileID int64, req ProfileRequest) error {
	copies := []struct {
		mode string
		copy func(context.Context, int64, int64) error
	}{
		{req.StartSettings, a.store.CopyProfileSettings},
		{req.StartSignatures, a.store.CopyProfileSignatures},
		{req.StartViews, a.store.CopyProfileViews},
		{req.StartLayout, a.store.CopyProfileLayout},
	}
	for _, c := range copies {
		if c.mode != startCopy {
			continue
		}
		if err := c.copy(a.ctx, mainID, profileID); err != nil {
			return err
		}
	}
	return nil
}

// UpdateProfile saves a profile's name, icon, sharing switches and account
// visibility. Turning sharing on for an area means the profile reads main's
// rows from now on; its own rows stay in the database untouched, so turning it
// back off brings them back rather than losing them.
func (a *App) UpdateProfile(req ProfileRequest) (ProfileDTO, error) {
	if err := a.ready(); err != nil {
		return ProfileDTO{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ProfileDTO{}, errProfileNameRequired
	}
	profile, err := a.store.GetProfile(a.ctx, req.ID)
	if err != nil {
		return ProfileDTO{}, err
	}

	profile.Name = name
	profile.Icon = trimIcon(req.Icon)
	// main shares with nobody: it is what the others share from.
	if !profile.Main {
		profile.ShareSettings = req.StartSettings == startShare
		profile.ShareSignatures = req.StartSignatures == startShare
		profile.ShareViews = req.StartViews == startShare
		profile.ShareLayout = req.StartLayout == startShare
	}
	if err := a.store.UpdateProfile(a.ctx, *profile); err != nil {
		return ProfileDTO{}, err
	}
	if err := a.store.SetProfileAccounts(a.ctx, profile.ID, req.AccountIDs); err != nil {
		return ProfileDTO{}, err
	}

	// editing the profile you are in changes what it can see and where its
	// settings come from, so the scope has to follow immediately.
	if profile.ID == a.store.ScopedProfileID() {
		main, err := a.store.MainProfile(a.ctx)
		if err != nil {
			return ProfileDTO{}, err
		}
		a.store.UseProfile(*profile, main.ID)
		a.emit(EventProfileChanged, nil)
	}
	return a.toProfileDTO(*profile)
}

// DeleteProfile removes a profile and everything it owned. Its accounts and
// their mail belong to the install and stay. Deleting the profile you are in
// switches to main first, since the app has to be somewhere.
func (a *App) DeleteProfile(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	profile, err := a.store.GetProfile(a.ctx, id)
	if err != nil {
		return err
	}
	if profile.Main {
		return errMainProfileDelete
	}

	if id == a.store.ScopedProfileID() {
		main, err := a.store.MainProfile(a.ctx)
		if err != nil {
			return err
		}
		if err := a.SwitchProfile(main.ID); err != nil {
			return err
		}
	}
	return a.store.DeleteProfile(a.ctx, id)
}

// SwitchProfile moves the app to another profile: the current profile's
// background work stops, the store is re-scoped, and the frontend reloads.
//
// Sync is cancelled rather than waited for. A sync is resumable by design (it
// picks up from the stored uid watermark) and waiting would mean the switch
// hangs for as long as a mailbox takes.
func (a *App) SwitchProfile(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	profile, err := a.store.GetProfile(a.ctx, id)
	if err != nil {
		return err
	}
	main, err := a.store.MainProfile(a.ctx)
	if err != nil {
		return err
	}

	a.endProfileSession()

	if err := a.store.SetActiveProfile(a.ctx, id); err != nil {
		return err
	}
	a.store.UseProfile(*profile, main.ID)

	// the new profile's settings decide the log level, the menu language and
	// everything else the backend reads at startup, so they are applied again
	// here rather than left on the old profile's values.
	a.applyLogSettings()
	a.RebuildMenu()

	a.emit(EventProfileChanged, nil)
	goSafe("starting the profile's mail sync", a.runInitialSyncAndIdle)
	return nil
}

// sessionCtx is the context for work that belongs to the profile the app is in:
// idle loops and background sync. It is cancelled on a switch, so nothing keeps
// watching a mailbox the new profile cannot see.
func (a *App) sessionCtx() context.Context {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if a.session == nil {
		ctx, cancel := context.WithCancel(a.ctx)
		a.session, a.sessionStop = ctx, cancel
	}
	return a.session
}

// endProfileSession cancels the current session, joins every account worker, and
// clears the session so the next sessionCtx starts a fresh one.
func (a *App) endProfileSession() {
	a.sessionMu.Lock()
	stop := a.sessionStop
	a.session, a.sessionStop = nil, nil
	a.sessionMu.Unlock()

	if stop != nil {
		stop()
	}
	a.joinAllAccountWorkers()
}

// toProfileDTO flattens a profile and looks up the accounts it shows.
func (a *App) toProfileDTO(p storage.Profile) (ProfileDTO, error) {
	accounts, err := a.store.ProfileAccountIDs(a.ctx, p.ID)
	if err != nil {
		return ProfileDTO{}, fmt.Errorf("desktop: profile %d accounts: %w", p.ID, err)
	}
	return ProfileDTO{
		ID:              p.ID,
		Name:            p.Name,
		Icon:            p.Icon,
		Main:            p.Main,
		Active:          p.Active,
		ShareSettings:   p.ShareSettings,
		ShareSignatures: p.ShareSignatures,
		ShareViews:      p.ShareViews,
		ShareLayout:     p.ShareLayout,
		AccountIDs:      accounts,
	}, nil
}

// trimIcon keeps the icon to a glyph or two. Counted in runes, so an emoji is
// one character rather than four bytes.
func trimIcon(icon string) string {
	runes := []rune(strings.TrimSpace(icon))
	if len(runes) > maxProfileIcon {
		return string(runes[:maxProfileIcon])
	}
	return string(runes)
}
