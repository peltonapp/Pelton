package desktop

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func newTrackerTestApp(t *testing.T) *App {
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
	return &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler)}
}

const trackerBody = `<p>hello</p>
<img src="https://cdn.example.com/hero.jpg" width="600">
<img src="https://track.example.net/open.gif" width="1" height="1">`

// TestLoadingRemoteContentStillBlocksTrackers is the point of #205: saying yes
// to a newsletter's pictures must not also confirm the open.
func TestLoadingRemoteContentStillBlocksTrackers(t *testing.T) {
	a := newTrackerTestApp(t)
	if err := a.store.Set(a.ctx, settingBlockTrackers, "true"); err != nil {
		t.Fatalf("set: %v", err)
	}

	out := a.renderHTML(trackerBody, nil, true)
	if strings.Contains(out, "track.example.net") {
		t.Errorf("tracking pixel survived a remote load:\n%s", out)
	}
	if !strings.Contains(out, "hero.jpg") {
		t.Errorf("ordinary image was dropped along with it:\n%s", out)
	}
}

// TestDetectionIsOffByDefault pins the opt-in: a fresh install renders exactly
// as it did before this feature existed.
func TestDetectionIsOffByDefault(t *testing.T) {
	a := newTrackerTestApp(t)

	out := a.renderHTML(trackerBody, nil, true)
	if !strings.Contains(out, "track.example.net") {
		t.Errorf("pixel still blocked with detection off:\n%s", out)
	}
	if got := a.trackerDTOs(trackerBody); got != nil {
		t.Errorf("trackerDTOs() = %v with detection off, want none: nothing is being blocked to report", got)
	}
}

// TestLoadAnywayIgnoresTheSetting covers the per-button override: the reader
// looked at what was detected, decided it was wrong, and asked for the image.
func TestLoadAnywayIgnoresTheSetting(t *testing.T) {
	a := newTrackerTestApp(t)
	if err := a.store.Set(a.ctx, settingBlockTrackers, "true"); err != nil {
		t.Fatalf("set: %v", err)
	}

	out := a.renderHTMLWithTrackers(trackerBody, nil)
	if !strings.Contains(out, "track.example.net") {
		t.Errorf("load anyway still withheld the pixel:\n%s", out)
	}
}

// TestBlockedViewDropsAllRemoteImages covers the ordinary first view, where the
// tracking-pixel setting makes no difference: nothing remote is loaded at all.
func TestBlockedViewDropsAllRemoteImages(t *testing.T) {
	a := newTrackerTestApp(t)

	out := a.renderHTML(trackerBody, nil, false)
	if strings.Contains(out, "track.example.net") || strings.Contains(out, "hero.jpg") {
		t.Errorf("remote image survived the blocked view:\n%s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("body text went with them:\n%s", out)
	}
}

func TestTrackerDTOsReportHostAndReasons(t *testing.T) {
	a := newTrackerTestApp(t)
	if err := a.store.Set(a.ctx, settingBlockTrackers, "true"); err != nil {
		t.Fatalf("set: %v", err)
	}

	got := a.trackerDTOs(trackerBody)
	if len(got) != 1 {
		t.Fatalf("trackerDTOs() = %d entries, want 1: %v", len(got), got)
	}
	if got[0].Host != "track.example.net" {
		t.Errorf("host = %q, want track.example.net", got[0].Host)
	}
	if len(got[0].Reasons) == 0 {
		t.Error("no reasons reported, so the ui cannot say why")
	}
	if got[0].Reasons[0] != "tiny" {
		t.Errorf("first reason = %q, want tiny", got[0].Reasons[0])
	}
}
