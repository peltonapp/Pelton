package chat

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneMediaCacheDueRemovesOldDownloadsOncePerDay(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	if err := os.MkdirAll(media, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, age time.Duration) string {
		t.Helper()
		path := filepath.Join(media, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		mod := time.Now().Add(-age)
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
		return path
	}
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	s := &session{m: &Manager{opts: Options{Dir: dir}}}
	old := write("old", cacheAge+time.Hour)
	fresh := write("fresh", time.Hour)

	start := time.Now()
	s.pruneMediaCacheDue(start)
	if exists(old) {
		t.Error("the first call kept a download older than cacheAge")
	}
	if !exists(fresh) {
		t.Error("the first call removed a fresh download")
	}

	later := write("later", cacheAge+time.Hour)
	s.pruneMediaCacheDue(start.Add(pruneEvery - time.Minute))
	if !exists(later) {
		t.Error("pruned again before pruneEvery had passed")
	}

	s.pruneMediaCacheDue(start.Add(pruneEvery))
	if exists(later) {
		t.Error("did not prune once pruneEvery had passed")
	}
	if !exists(fresh) {
		t.Error("removed a fresh download")
	}
}
