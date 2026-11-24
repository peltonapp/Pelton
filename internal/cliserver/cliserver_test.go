package cliserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/peltonapp/Pelton/internal/cliproto"
)

type fakeBackend struct {
	mu      sync.Mutex
	state   State
	handled []string
	entries []Entry
}

func (f *fakeBackend) State() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeBackend) Handle(_ context.Context, command string, _ []byte) (Response, error) {
	f.mu.Lock()
	f.handled = append(f.handled, command)
	f.mu.Unlock()
	if command == cliproto.CmdShow {
		return Response{Touched: "message 9"}, cliproto.Errorf(cliproto.CodeNotFound, "no message 9")
	}
	return Response{Data: []string{"done"}, Touched: "everything"}, nil
}

func (f *fakeBackend) Record(e Entry) {
	f.mu.Lock()
	f.entries = append(f.entries, e)
	f.mu.Unlock()
}

// shortDir is a temporary folder with a path short enough for a socket on
// macOS, where t.TempDir() can pass the 104 byte limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func allCaps() map[string]bool {
	caps := map[string]bool{}
	for _, c := range cliproto.Capabilities {
		caps[c] = true
	}
	return caps
}

func startServer(t *testing.T, st State) (*Server, *fakeBackend, cliproto.Paths) {
	t.Helper()
	paths := cliproto.PathsIn(shortDir(t))
	b := &fakeBackend{state: st}
	s := New(paths, b, nil)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s, b, paths
}

func call(t *testing.T, paths cliproto.Paths, token, command string, version int) (int, cliproto.Reply) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", paths.Socket)
	}}}
	req, _ := http.NewRequest(http.MethodPost, "http://pelton/v1/"+command, bytes.NewReader([]byte("{}")))
	req.Header.Set(cliproto.VersionHeader, strconv.Itoa(version))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reply cliproto.Reply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, reply
}

func TestFilesAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are access lists on Windows")
	}
	s, _, paths := startServer(t, State{Access: true})
	if err := s.SetToken("secret"); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{paths.Dir: 0o700, paths.Socket: 0o600, paths.Token: 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode %v, want %v", filepath.Base(path), got, want)
		}
	}
	data, _ := os.ReadFile(paths.Token)
	if string(data) != "secret" {
		t.Fatalf("token file holds %q", data)
	}
}

func TestLoosenedFolderIsTightenedAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are access lists on Windows")
	}
	paths := cliproto.PathsIn(shortDir(t))
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.Dir, 0o777); err != nil {
		t.Fatal(err)
	}
	s := New(paths, &fakeBackend{}, nil)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	info, _ := os.Stat(paths.Dir)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", info.Mode().Perm())
	}
}

func TestTokenIsChecked(t *testing.T) {
	s, b, paths := startServer(t, State{Access: true, Caps: allCaps()})
	if err := s.SetToken("right"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "wrong", "righ", "right2"} {
		code, reply := call(t, paths, token, cliproto.CmdList, cliproto.Version)
		if code != http.StatusUnauthorized || reply.Error == nil || reply.Error.Code != cliproto.CodeUnauthorized {
			t.Fatalf("token %q: %d %+v", token, code, reply)
		}
	}
	if len(b.handled) != 0 {
		t.Fatalf("handled %v without the token", b.handled)
	}
	if code, reply := call(t, paths, "right", cliproto.CmdList, cliproto.Version); code != http.StatusOK || !reply.OK {
		t.Fatalf("right token: %d %+v", code, reply)
	}
}

func TestNoTokenRefusesEverything(t *testing.T) {
	_, _, paths := startServer(t, State{Access: true, Caps: allCaps()})
	if _, reply := call(t, paths, "", cliproto.CmdStatus, cliproto.Version); reply.Error == nil || reply.Error.Code != cliproto.CodeUnauthorized {
		t.Fatalf("reply %+v", reply)
	}
}

func TestAccessOff(t *testing.T) {
	s, b, paths := startServer(t, State{Access: false, Caps: allCaps()})
	_ = s.SetToken("tok")
	for _, command := range []string{cliproto.CmdStatus, cliproto.CmdList, cliproto.CmdSend} {
		_, reply := call(t, paths, "tok", command, cliproto.Version)
		if reply.Error == nil || reply.Error.Code != cliproto.CodeAccessOff {
			t.Fatalf("%s: %+v", command, reply)
		}
	}
	if len(b.handled) != 0 {
		t.Fatalf("handled %v while off", b.handled)
	}
}

func TestEachSwitchRefusesItsCommands(t *testing.T) {
	for _, capability := range cliproto.Capabilities {
		caps := allCaps()
		caps[capability] = false
		s, b, paths := startServer(t, State{Access: true, Caps: caps})
		_ = s.SetToken("tok")
		for command, need := range cliproto.Needs {
			_, reply := call(t, paths, "tok", command, cliproto.Version)
			refused := reply.Error != nil && reply.Error.Code == cliproto.CodePermissionOff
			if refused != (need.Cap == capability) {
				t.Fatalf("switch %s off, command %s: %+v", capability, command, reply)
			}
			if refused && reply.Error.Switch != capability {
				t.Fatalf("names switch %q, want %q", reply.Error.Switch, capability)
			}
		}
		for _, h := range b.handled {
			if cliproto.Needs[h].Cap == capability {
				t.Fatalf("handled %s with %s off", h, capability)
			}
		}
	}
}

func TestLockedRefusesAllButStatus(t *testing.T) {
	s, b, paths := startServer(t, State{Access: true, Locked: true, Caps: allCaps()})
	_ = s.SetToken("tok")
	for command := range cliproto.Needs {
		_, reply := call(t, paths, "tok", command, cliproto.Version)
		if command == cliproto.CmdStatus {
			var st cliproto.Status
			raw, _ := json.Marshal(reply.Data)
			_ = json.Unmarshal(raw, &st)
			if !reply.OK || !st.Locked {
				t.Fatalf("status while locked: %+v", reply)
			}
			continue
		}
		if reply.Error == nil || reply.Error.Code != cliproto.CodeLocked {
			t.Fatalf("%s while locked: %+v", command, reply)
		}
	}
	if len(b.handled) != 0 {
		t.Fatalf("handled %v while locked", b.handled)
	}
}

func TestUnknownCommandAndOldProtocol(t *testing.T) {
	s, _, paths := startServer(t, State{Access: true, Caps: allCaps()})
	_ = s.SetToken("tok")
	if _, reply := call(t, paths, "tok", "rm-rf", cliproto.Version); reply.Error == nil || reply.Error.Code != cliproto.CodeBadRequest {
		t.Fatalf("unknown: %+v", reply)
	}
	if _, reply := call(t, paths, "tok", cliproto.CmdList, cliproto.Version+1); reply.Error == nil || reply.Error.Code != cliproto.CodeBadRequest {
		t.Fatalf("version: %+v", reply)
	}
}

func TestEveryRequestIsLogged(t *testing.T) {
	s, b, paths := startServer(t, State{Access: true, Caps: map[string]bool{cliproto.CapRead: true}})
	_ = s.SetToken("tok")
	call(t, paths, "tok", cliproto.CmdList, cliproto.Version)
	call(t, paths, "tok", cliproto.CmdShow, cliproto.Version)
	call(t, paths, "tok", cliproto.CmdSend, cliproto.Version)
	call(t, paths, "bad", cliproto.CmdList, cliproto.Version)
	want := []Entry{
		{Command: cliproto.CmdList, Touched: "everything", Result: "ok"},
		{Command: cliproto.CmdShow, Touched: "message 9", Result: string(cliproto.CodeNotFound)},
		{Command: cliproto.CmdSend, Result: string(cliproto.CodePermissionOff), Switch: cliproto.CapSend},
		{Command: cliproto.CmdList, Result: string(cliproto.CodeUnauthorized)},
	}
	if len(b.entries) != len(want) {
		t.Fatalf("entries %+v", b.entries)
	}
	for i := range want {
		if b.entries[i] != want[i] {
			t.Fatalf("entry %d: %+v, want %+v", i, b.entries[i], want[i])
		}
	}
}

func TestStaleSocketIsReplacedAndALiveOneIsNot(t *testing.T) {
	s, _, paths := startServer(t, State{Access: true})
	second := New(paths, &fakeBackend{}, nil)
	if err := second.Start(); err == nil {
		t.Fatal("took over a socket another server answers on")
	}
	_ = s.Stop(context.Background())
	ln, err := net.Listen("unix", paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	ln.Close()
	if _, err := os.Lstat(paths.Socket); err != nil {
		t.Fatalf("no stale socket to test with: %v", err)
	}
	third := New(paths, &fakeBackend{}, nil)
	if err := third.Start(); err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	_ = third.Stop(context.Background())
}

func TestStopRemovesTheTokenAndSocket(t *testing.T) {
	s, _, paths := startServer(t, State{Access: true})
	_ = s.SetToken("tok")
	_ = s.Stop(context.Background())
	for _, path := range []string{paths.Token, paths.Socket} {
		if _, err := os.Lstat(path); err == nil {
			t.Fatalf("%s left behind", path)
		}
	}
}

func TestRegularFileInTheWayIsKept(t *testing.T) {
	paths := cliproto.PathsIn(shortDir(t))
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Socket, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := New(paths, &fakeBackend{}, nil).Start(); err == nil {
		t.Fatal("started over a regular file")
	}
	if data, _ := os.ReadFile(paths.Socket); string(data) != "mine" {
		t.Fatal("removed a file that was not a socket")
	}
}
