package desktop

import (
	"context"
	"testing"
	"time"
)

// backgroundWorkGrace is how long a test waits for the app's goroutines after
// cancelling them. Generous: it is only ever reached when something is wedged,
// and the alternative is a test that passes while a goroutine keeps writing.
const backgroundWorkGrace = 10 * time.Second

// testContext returns the context a test's App should run on, together with the
// cleanup that stops the goroutines started under it.
//
// Bindings hand work to goSafe and return, so a test that calls one returns
// while that work is still running. Cleanups run in reverse, which put the
// store's Close and the temp directory's removal ahead of goroutines still
// using both: the database was closed under them, and sqlite recreated its
// -wal and -shm sidecars inside a directory the test framework was in the
// middle of deleting, which surfaced as a "directory not empty" failure on
// whichever test happened to lose the race.
//
// Register the returned cleanup last, after the store's own, so it runs first
// and the store is still open when the goroutines finish with it:
//
//	ctx, stopBackground := testContext(t)
//	store, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
//	t.Cleanup(func() { _ = store.Close() })
//	t.Cleanup(stopBackground)
func testContext(t *testing.T) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	return ctx, func() {
		cancel()
		if !waitForBackgroundWork(backgroundWorkGrace) {
			t.Error("background goroutines were still running after the test ended")
		}
	}
}
