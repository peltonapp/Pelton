package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
)

type grants map[int64]string

func (g grants) Folder(_ context.Context, id int64) (string, error) { return g[id], nil }

func setup(t *testing.T) (*scripting.Registry, string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "granted")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "in.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := scriptingtest.Registry(t, func(r *scripting.Registry) error { return Register(r, grants{1: root}) })
	return r, root, outside
}

func run(t *testing.T, r *scripting.Registry, src string, perms ...string) *scripting.Run {
	t.Helper()
	return scriptingtest.Run(t, r, scriptingtest.Script(1, src, perms...), false)
}

func TestLocalized(t *testing.T) {
	r, _, _ := setup(t)
	scriptingtest.Localized(t, r)
}

func TestReadWriteAppendList(t *testing.T) {
	r, root, _ := setup(t)
	got := run(t, r, `
print(files.read("in.txt"))
files.write("out/log.txt", "a")
files.append("out/log.txt", "b")
print(files.read("out/log.txt"), files.exists("out/log.txt"), files.exists("nope"))
for _, e in ipairs(files.list()) do print(e.name, e.dir) end`, PermRead, PermWrite)
	if got.Status != scripting.StatusOK {
		t.Fatalf("%s %s", got.Status, got.Error)
	}
	want := "inside\nab true false\nin.txt false\nout true"
	if s := strings.Join(scriptingtest.Logged(got), "\n"); s != want {
		t.Fatalf("logged %q", s)
	}
	info, err := os.Stat(filepath.Join(root, "out", "log.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 != 0 {
		t.Fatalf("mode %v", info.Mode())
	}
}

func TestPathsCannotLeaveTheFolder(t *testing.T) {
	r, root, outside := setup(t)
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret.txt")); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"../outside/secret.txt", "a/../../outside/secret.txt", filepath.Join(outside, "secret.txt"), "/etc/passwd", "..\\outside\\secret.txt"} {
		got := run(t, r, fmt.Sprintf(`print(files.read(%q))`, p), PermRead)
		if got.Status != scripting.StatusError {
			t.Fatalf("read %q: %s %v", p, got.Status, scriptingtest.Logged(got))
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	for _, src := range []string{
		`local t = files.read("link/secret.txt") print(t)`,
		`local t = files.read("secret.txt") print(t)`,
		`local l = files.list("link") print(l)`,
	} {
		got := run(t, r, src, PermRead)
		if logged := scriptingtest.Logged(got); got.Status != scripting.StatusOK || len(logged) != 1 || logged[0] != "nil" {
			t.Fatalf("%s escaped: %s %v", src, got.Status, logged)
		}
	}
	for _, src := range []string{`files.write("link/new.txt", "x")`, `files.append("secret.txt", "x")`} {
		if got := run(t, r, src, PermWrite); got.Status != scripting.StatusError {
			t.Fatalf("%s: %s", src, got.Status)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); err == nil {
		t.Fatal("wrote through a link")
	}
	if data, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(data) != "secret" {
		t.Fatalf("changed through a link: %q", data)
	}
}

func TestPermissionsAndGrant(t *testing.T) {
	r, root, _ := setup(t)
	if got := run(t, r, `files.write("x.txt", "x")`, PermRead); got.Status != scripting.StatusDenied {
		t.Fatalf("write with read only: %s", got.Status)
	}
	if got := run(t, r, `files.read("in.txt")`); got.Status != scripting.StatusDenied {
		t.Fatalf("read without permission: %s", got.Status)
	}
	other := scriptingtest.Run(t, r, scriptingtest.Script(2, `files.read("in.txt")`, PermRead), false)
	if other.Status != scripting.StatusError || !strings.Contains(other.Error, "no folder") {
		t.Fatalf("no grant: %s %s", other.Status, other.Error)
	}
	sc := scriptingtest.Script(1, `print(files.read("in.txt")) files.write("dry.txt", "x")`, PermRead, PermWrite)
	dry := scriptingtest.Run(t, r, sc, true)
	if dry.Status != scripting.StatusOK || dry.Changes() != 1 {
		t.Fatalf("dry: %s %s %+v", dry.Status, dry.Error, dry.Steps)
	}
	if _, err := os.Stat(filepath.Join(root, "dry.txt")); err == nil {
		t.Fatal("a dry run wrote a file")
	}
}
