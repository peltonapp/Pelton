package desktop

import (
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
)

// colorKeywords maps a color index (1..8) to the Thunderbird-style imap keyword
// used when the user turns on color syncing, so colors show in other clients.
var colorKeywords = []imap.Flag{
	"$Label1", "$Label2", "$Label3", "$Label4",
	"$Label5", "$Label6", "$Label7", "$Label8",
}

// SetFlagColor sets a message's color label (0 clears, 1..8 pick a palette
// color). It is authoritative locally; when the flag_color_sync setting is on it
// also pushes the matching imap keyword in the background, best effort.
func (a *App) SetFlagColor(id int64, color int) error {
	if err := a.ready(); err != nil {
		return err
	}
	if color < 0 || color > len(colorKeywords) {
		return fmt.Errorf("pelton: invalid flag color %d", color)
	}
	if err := a.store.SetFlagColor(a.ctx, id, color); err != nil {
		return err
	}
	if a.boolSetting(settingFlagColorSync, false) {
		goSafe("syncing a flag color", func() { a.pushColorKeyword(id, color) })
	}
	return nil
}

// pushColorKeyword reflects a color change onto the server as an imap keyword.
// It removes every label keyword then adds the chosen one, so switching or
// clearing a color leaves a single (or no) label. Failures are logged only; the
// local color is already saved and authoritative.
func (a *App) pushColorKeyword(id int64, color int) {
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		a.log.Error("color sync: load message", "id", id, "err", err)
		return
	}
	folder, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		a.log.Error("color sync: load folder", "id", id, "err", err)
		return
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		a.log.Error("color sync: load account", "id", id, "err", err)
		return
	}
	a.protocolFor(*account).setColor(*m, *folder, *account, color)
}

// DownloadMessageOffline keeps a message available offline. A message that so
// far has only its list entry gets its body fetched first; pinning a row with
// no body would show it as downloaded while there is nothing to read.
func (a *App) DownloadMessageOffline(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	if !m.BodyComplete {
		if err := a.fetchMessageBodyOnDemand(m); err != nil {
			return offlineOrErr(err)
		}
		if err := a.bodyStored(id); err != nil {
			return err
		}
	}
	return a.store.SetOffline(a.ctx, id, true)
}

// RemoveOffline unpins a message from offline.
func (a *App) RemoveOffline(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetOffline(a.ctx, id, false)
}

// SnoozeMessage schedules a message to resurface at untilRFC3339. hideNow also
// hides it from the list until then; when false it stays visible and is only
// marked unread when the timer fires.
func (a *App) SnoozeMessage(id int64, untilRFC3339 string, hideNow bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	t, err := time.Parse(time.RFC3339, untilRFC3339)
	if err != nil {
		return fmt.Errorf("pelton: invalid snooze time %q: %w", untilRFC3339, err)
	}
	stored := t.UTC().Format(time.RFC3339)
	return a.store.SetSnooze(a.ctx, id, stored, hideNow)
}

// UnsnoozeMessage cancels a snooze without reviving the message.
func (a *App) UnsnoozeMessage(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.ClearSnooze(a.ctx, id)
}

// runSnoozePoller wakes periodically to revive messages whose snooze time has
// passed, marking them unread and unhiding them, then nudges the ui to refresh.
func (a *App) runSnoozePoller() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	a.reviveDueSnoozes() // catch any that came due while the app was closed
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.reviveDueSnoozes()
		}
	}
}

// reviveDueSnoozes revives every message whose snooze has elapsed.
func (a *App) reviveDueSnoozes() {
	now := time.Now().UTC().Format(time.RFC3339)
	ids, err := a.store.DueSnoozes(a.ctx, now)
	if err != nil {
		a.log.Error("snooze poller: query due", "err", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		if err := a.store.ReviveSnoozed(a.ctx, id); err != nil {
			a.log.Error("snooze poller: revive", "id", id, "err", err)
		}
	}
	// a refresh is enough; the list re-reads and the revived rows are back, unread.
	a.emit(EventMailNew, MailNewEvent{Count: len(ids)})
}
