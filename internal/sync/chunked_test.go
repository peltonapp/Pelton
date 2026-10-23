package sync

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// chunkAdapter serves one large source by range.
type chunkAdapter struct {
	*fakeAdapter
	src    map[string][]byte
	ranges [][2]int64
	gens   []string // the generation each call expected
	cap    int64    // a server limit on one partial fetch; 0 = none
}

func (a *chunkAdapter) FetchChunk(ctx context.Context, mailboxID, remoteID string, offset, length int64) (Fetched, error) {
	a.ranges = append(a.ranges, [2]int64{offset, length})
	a.gens = append(a.gens, ExpectedGeneration(ctx, mailboxID))
	src, ok := a.src[remoteID]
	if !ok {
		return Fetched{}, ErrNotOnServer
	}
	if a.cap > 0 {
		length = min(length, a.cap)
	}
	start := min(offset, int64(len(src)))
	return Fetched{RemoteID: remoteID, Subject: "big", Flags: storage.FlagSeen, Raw: src[start:min(start+length, int64(len(src)))]}, nil
}

func (a *chunkAdapter) ParseChunked(f Fetched) (Fetched, error) {
	f.Text = string(f.Raw[:4])
	f.Size = int64(len(f.Raw))
	return f, nil
}

func needsBody(t *testing.T, db *storage.DB, folderID int64, remoteID string) bool {
	t.Helper()
	need, err := db.RemoteIDsNeedingBody(context.Background(), folderID, []string{remoteID})
	if err != nil {
		t.Fatal(err)
	}
	return len(need) == 1
}

// A 30 MB message comes in 5 MB chunks, one per call, and fills its stub
// only after the last one. A restart (a new PartialBody) starts again from
// byte 0 and the stub stays incomplete until then.
func TestFetchBodyChunk(t *testing.T) {
	t.Parallel()
	const mb = 1 << 20
	src := bytes.Repeat([]byte("abcdefgh"), 30*mb/8)
	for _, tc := range []struct {
		name      string
		restartAt int // calls before a restart; 0 = none
		wantCalls int
	}{
		{"straight", 0, 7}, // six full chunks, then an empty one ends it
		{"restart after three chunks", 3, 3 + 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			seedLocalStub(t, db, folder, "9")
			a := &chunkAdapter{fakeAdapter: &fakeAdapter{}, src: map[string][]byte{"9": src}}
			engine := NewEngine(a, db, nil)
			var announced []int64
			engine.OnStored = func(_ storage.Folder, ids []int64) { announced = append(announced, ids...) }

			p := NewPartialBody("9")
			restarted := false
			for calls := 1; ; calls++ {
				done, res, err := engine.FetchBodyChunk(ctx, folder, p, 5*mb)
				if err != nil {
					t.Fatal(err)
				}
				if done {
					if calls != tc.wantCalls || res.New != 1 {
						t.Fatalf("done after %d calls (new %d), want %d", calls, res.New, tc.wantCalls)
					}
					break
				}
				if !needsBody(t, db, folder.ID, "9") {
					t.Fatalf("stub filled after %d of %d bytes", p.Fetched(), len(src))
				}
				if calls == tc.restartAt && !restarted {
					restarted = true
					p = NewPartialBody("9") // the in-memory part is lost
				}
			}
			if needsBody(t, db, folder.ID, "9") || len(announced) != 1 {
				t.Fatalf("stub still incomplete or announced %v", announced)
			}
			if a.ranges[len(a.ranges)-7][0] != 0 {
				t.Fatal("the last run did not start at byte 0")
			}
			m, err := db.MessageIDByRemoteID(ctx, folder.ID, "9")
			if err != nil {
				t.Fatal(err)
			}
			got, err := db.GetMessage(ctx, m)
			if err != nil || got.SizeBytes != int64(len(src)) || got.BodyPlain != "abcd" {
				t.Fatalf("stored %+v, %v", got, err)
			}
		})
	}
}

// A message gone from the server ends the fetch with nothing stored.
func TestFetchBodyChunkGone(t *testing.T) {
	t.Parallel()
	db, folder := newSyncTestFolder(t)
	seedLocalStub(t, db, folder, "9")
	engine := NewEngine(&chunkAdapter{fakeAdapter: &fakeAdapter{}}, db, nil)
	done, res, err := engine.FetchBodyChunk(context.Background(), folder, NewPartialBody("9"), 1024)
	if err != nil || !done || res.New != 0 {
		t.Fatalf("done=%v new=%d err=%v", done, res.New, err)
	}
	if !needsBody(t, db, folder.ID, "9") {
		t.Fatal("a vanished message must stay a stub")
	}
}

// A folder whose generation changed between chunks drops the partial body
// instead of joining two messages, and nothing is stored; every chunk call
// carries the folder's generation for the adapter to check.
func TestFetchBodyChunkGenerationChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedLocalStub(t, db, folder, "9")
	folder.UIDValidity = 7
	a := &chunkAdapter{fakeAdapter: &fakeAdapter{}, src: map[string][]byte{"9": bytes.Repeat([]byte("x"), 100)}}
	engine := NewEngine(a, db, nil)
	p := NewPartialBody("9")
	if done, _, err := engine.FetchBodyChunk(ctx, folder, p, 40); done || err != nil {
		t.Fatalf("first chunk: done=%v err=%v", done, err)
	}
	folder.UIDValidity = 8
	done, res, err := engine.FetchBodyChunk(ctx, folder, p, 40)
	if !done || !errors.Is(err, ErrGenerationChanged) || res.New != 0 {
		t.Fatalf("done=%v new=%d err=%v, want done with ErrGenerationChanged", done, res.New, err)
	}
	if !needsBody(t, db, folder.ID, "9") {
		t.Fatal("stub filled across a generation change")
	}
	if len(a.gens) != 1 || a.gens[0] != "7" {
		t.Fatalf("expected generations %v, want [7]", a.gens)
	}
}

// A server that caps each partial fetch below the asked size returns short
// chunks before the end. With the stub's size known the fetch goes on until
// it has every byte; a size the server never delivers ends on an empty chunk.
func TestFetchBodyChunkServerCap(t *testing.T) {
	t.Parallel()
	src := bytes.Repeat([]byte("abcdefgh"), 25) // 200 bytes
	for _, tc := range []struct {
		name      string
		size      int64
		wantBytes int64
	}{
		{"size known", 200, 200},
		{"size larger than the source", 250, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
				RemoteID: "9", Subject: "big", SizeBytes: tc.size}); err != nil {
				t.Fatal(err)
			}
			a := &chunkAdapter{fakeAdapter: &fakeAdapter{}, src: map[string][]byte{"9": src}, cap: 30}
			engine := NewEngine(a, db, nil)
			p := NewPartialBody("9")
			for calls := 1; ; calls++ {
				if calls > 20 {
					t.Fatal("no end")
				}
				done, _, err := engine.FetchBodyChunk(ctx, folder, p, 100)
				if err != nil {
					t.Fatal(err)
				}
				if done {
					break
				}
			}
			if p.Fetched() != tc.wantBytes || needsBody(t, db, folder.ID, "9") {
				t.Fatalf("fetched %d bytes, stub incomplete %v; want %d stored", p.Fetched(), needsBody(t, db, folder.ID, "9"), tc.wantBytes)
			}
		})
	}
}
