//go:build unix

package files

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// TestOnlyPlainFiles checks that a script cannot hang a run on a named pipe
// in its folder, and cannot rewrite a file that can be run.
func TestOnlyPlainFiles(t *testing.T) {
	r, root, _ := setup(t)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skip("no named pipes here:", err)
	}
	start := time.Now()
	got := run(t, r, `files.read("pipe")`, PermRead)
	if got.Status != scripting.StatusError || !strings.Contains(got.Error, "not a plain file") || time.Since(start) > time.Second {
		t.Fatalf("pipe: %s %q after %s", got.Status, got.Error, time.Since(start))
	}
	tool := filepath.Join(root, "tool.sh")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho hi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{`files.write("tool.sh", "rm -rf ~")`, `files.append("tool.sh", "rm -rf ~")`, `files.write("pipe", "x")`} {
		if got := run(t, r, src, PermWrite); got.Status != scripting.StatusError || !strings.Contains(got.Error, "not a plain file") {
			t.Fatalf("%s: %s %q", src, got.Status, got.Error)
		}
	}
	if data, _ := os.ReadFile(tool); !strings.Contains(string(data), "echo hi") || strings.Contains(string(data), "rm") {
		t.Fatalf("the runnable file changed: %q", data)
	}
}
