package notes

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func newVault(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	v, err := OpenVault(dir)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return v, v.Root()
}

func TestCleanRel(t *testing.T) {
	tests := []struct {
		in   string
		want string
		err  error
	}{
		{"Note.md", "Note.md", nil},
		{"a/./b/../c.md", "a/c.md", nil},
		{`win\path.md`, "win/path.md", nil},
		{"", "", ErrOutside},
		{"../x.md", "", ErrOutside},
		{"a/../../x.md", "", ErrOutside},
		{"/etc/passwd", "", ErrOutside},
		{"..", "", ErrOutside},
		{".obsidian/app.json", "", ErrProtected},
		{"a/.trash/x.md", "", ErrProtected},
		{"a\x00b", "", ErrOutside},
	}
	for _, tt := range tests {
		got, err := CleanRel(tt.in)
		if !errors.Is(err, tt.err) || got != tt.want {
			t.Errorf("CleanRel(%q) = %q, %v; want %q, %v", tt.in, got, err, tt.want, tt.err)
		}
	}
}

func TestVaultRefusesSymlinksLeavingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	v, root := newVault(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Read("escape/secret.md"); !errors.Is(err, ErrOutside) {
		t.Errorf("read through dir symlink: %v, want ErrOutside", err)
	}
	if _, err := v.Read("link.md"); !errors.Is(err, ErrOutside) {
		t.Errorf("read file symlink: %v, want ErrOutside", err)
	}
	if err := v.Write("escape/new.md", []byte("x")); !errors.Is(err, ErrOutside) {
		t.Errorf("write through dir symlink: %v, want ErrOutside", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.md")); err == nil {
		t.Error("a file was written outside the vault")
	}
	entries, err := v.Walk()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Path, "secret") || e.Path == "link.md" {
			t.Errorf("walk listed %q from outside the vault", e.Path)
		}
	}
}

func TestVaultFollowsSymlinkInsideIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	v, root := newVault(t)
	if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.md"), filepath.Join(root, "alias.md")); err != nil {
		t.Fatal(err)
	}
	if err := v.Write("alias.md", []byte("two")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(filepath.Join(root, "alias.md"))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a file")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "real.md")); string(b) != "two" {
		t.Errorf("target holds %q, want two", b)
	}
}

func TestWriteIsAtomicAndKeepsMode(t *testing.T) {
	v, root := newVault(t)
	p := filepath.Join(root, "n.md")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.Write("n.md", []byte("new\r\ntext")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "new\r\ntext" {
		t.Errorf("content %q", b)
	}
	if fi, _ := os.Stat(p); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	leftovers, _ := filepath.Glob(filepath.Join(root, ".pelton-*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
	if err := v.Write("sub/dir/x.md", []byte("x")); err != nil {
		t.Errorf("write into a missing folder: %v", err)
	}
	if err := v.Create("n.md", []byte("again")); !errors.Is(err, ErrExists) {
		t.Errorf("create over a file: %v, want ErrExists", err)
	}
}

func TestWalkSkipsDotFolders(t *testing.T) {
	v, root := newVault(t)
	for _, f := range []string{".obsidian/app.json", ".trash/old.md", ".git/HEAD", "a/b.md", "c.md", "img/x.png", ".hidden.md"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := v.Walk()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Path)
	}
	sort.Strings(got)
	want := []string{"a", "a/b.md", "c.md", "img", "img/x.png"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("walk = %v, want %v", got, want)
	}
}

func TestTrashAndRestore(t *testing.T) {
	v, root := newVault(t)
	for i := 0; i < 2; i++ {
		if err := v.Write("dir/n.md", []byte("x")); err != nil {
			t.Fatal(err)
		}
		dest, err := v.Trash("dir/n.md")
		if err != nil {
			t.Fatal(err)
		}
		want := ".trash/dir/n.md"
		if i == 1 {
			want = ".trash/dir/n 1.md"
		}
		if dest != want {
			t.Errorf("trash %d went to %q, want %q", i, dest, want)
		}
	}
	if err := v.Restore(".trash/dir/n 1.md", "dir/n.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dir", "n.md")); err != nil {
		t.Error("restore did not bring the note back")
	}
	if err := v.Restore("../outside.md", "x.md"); !errors.Is(err, ErrOutside) {
		t.Errorf("restore from outside the trash: %v", err)
	}
}

func TestRenameRefusesTakenName(t *testing.T) {
	v, _ := newVault(t)
	_ = v.Write("a.md", []byte("a"))
	_ = v.Write("b.md", []byte("b"))
	if err := v.Rename("a.md", "b.md"); !errors.Is(err, ErrExists) {
		t.Errorf("rename onto a taken name: %v", err)
	}
	if err := v.Rename("a.md", "A.md"); err != nil {
		t.Errorf("case-only rename: %v", err)
	}
	if err := v.Mkdir("f"); err != nil {
		t.Fatal(err)
	}
	if err := v.Rename("f", "f/inner"); !errors.Is(err, ErrOutside) {
		t.Errorf("moving a folder into itself: %v", err)
	}
}
