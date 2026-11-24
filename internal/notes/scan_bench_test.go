package notes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// BenchmarkScanUnchanged is the periodic rescan of a 2k-note vault in which
// nothing changed, which is what an idle app pays on every scan tick.
func BenchmarkScanUnchanged(b *testing.B) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if err := db.RunMigrations(ctx); err != nil {
		b.Fatal(err)
	}
	vault := b.TempDir()
	for i := 0; i < 2000; i++ {
		full := filepath.Join(vault, fmt.Sprintf("folder%d", i%20), fmt.Sprintf("note%d.md", i))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			b.Fatal(err)
		}
		body := fmt.Sprintf("# Note %d\n\nLinks to [[note%d]] and #tag%d\n\n%s", i, (i+7)%2000, i%15, strings.Repeat("Some text for the note. ", 40))
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	svc := NewService(db, filepath.Join(b.TempDir(), "search"), nil, func(Change) {})
	if err := svc.Open(ctx, vault); err != nil {
		b.Fatal(err)
	}
	defer svc.Close()
	if err := svc.Scan(ctx); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := svc.Scan(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
