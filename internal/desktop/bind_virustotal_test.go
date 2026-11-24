package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
	"github.com/peltonapp/Pelton/internal/virustotal"
)

func newVTTestApp(t *testing.T) *App {
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

// The integration and both auto-scan targets are opt-in. A fresh profile must
// not scan anything, and must not reach the keyring looking for a key either.
func TestVirusTotalIsOffByDefault(t *testing.T) {
	a := newVTTestApp(t)

	for _, key := range []string{settingVTEnabled, settingVTAutoLinks, settingVTAutoAttachments} {
		if a.boolSetting(key, false) {
			t.Errorf("%s defaults to on, want off", key)
		}
	}
	if _, err := a.vtClient(); !errors.Is(err, errVTDisabled) {
		t.Errorf("vtClient() = %v, want errVTDisabled", err)
	}
}

func TestScanRefusedWhileDisabled(t *testing.T) {
	a := newVTTestApp(t)

	if _, err := a.ScanURL("https://example.com/"); !errors.Is(err, errVTDisabled) {
		t.Errorf("ScanURL = %v, want errVTDisabled", err)
	}
	if _, err := a.ScanAttachment(1, 1); !errors.Is(err, errVTDisabled) {
		t.Errorf("ScanAttachment = %v, want errVTDisabled", err)
	}
	if _, err := a.ScanMessage(1, true, true); !errors.Is(err, errVTDisabled) {
		t.Errorf("ScanMessage = %v, want errVTDisabled", err)
	}
}

// Asking for neither target type is a no-op that must not need a configured
// client, so an auto-scan with both settings off costs nothing.
func TestScanMessageWithNoTargetsNeedsNoClient(t *testing.T) {
	a := newVTTestApp(t)

	got, err := a.ScanMessage(1, false, false)
	if err != nil {
		t.Fatalf("ScanMessage: %v", err)
	}
	if len(got.Links) != 0 || len(got.Attachments) != 0 {
		t.Errorf("got %+v, want an empty result", got)
	}
}

// Turning the integration off has to leave nothing behind: the cached verdicts
// are a record of what was scanned, and the auto-scan toggles must not survive
// to resume scanning the moment it is switched back on.
func TestDisablingClearsCachedVerdictsAndAutoScan(t *testing.T) {
	a := newVTTestApp(t)
	ctx := context.Background()

	if err := a.SetVirusTotalEnabled(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := a.SetVirusTotalAutoScanLinks(true); err != nil {
		t.Fatalf("auto links: %v", err)
	}
	if err := a.SetVirusTotalAutoScanAttachments(true); err != nil {
		t.Fatalf("auto attachments: %v", err)
	}
	if err := a.store.CacheVerdict(ctx, storage.VerdictKindURL, "https://example.com/", storage.Verdict{Status: "clean", Total: 70}); err != nil {
		t.Fatalf("cache verdict: %v", err)
	}

	if err := a.SetVirusTotalEnabled(false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if a.boolSetting(settingVTAutoLinks, false) || a.boolSetting(settingVTAutoAttachments, false) {
		t.Error("auto-scan survived the integration being turned off")
	}
	if _, err := a.store.CachedVerdict(ctx, storage.VerdictKindURL, "https://example.com/", time.Hour); !errors.Is(err, storage.ErrVerdictNotCached) {
		t.Errorf("a cached verdict survived the integration being turned off: %v", err)
	}
}

func TestVTErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"quota", virustotal.ErrRateLimited, "rate_limited"},
		{"bad key", virustotal.ErrUnauthorized, "unauthorized"},
		{"wrapped quota", fmt.Errorf("lookup: %w", virustotal.ErrRateLimited), "rate_limited"},
		{"anything else keeps its text", errors.New("dial tcp: no route to host"), "dial tcp: no route to host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vtErrorMessage(tt.err); got != tt.want {
				t.Errorf("vtErrorMessage(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
