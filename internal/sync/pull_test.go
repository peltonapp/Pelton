package sync

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// pgpMIMERaw is a minimal PGP/MIME encrypted message: enough structure for the
// cache to recognise it as protected mail whose source has to be kept.
const pgpMIMERaw = "Message-ID: <enc@example.com>\r\n" +
	"Subject: secret\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=b\r\n" +
	"\r\n" +
	"--b\r\n" +
	"Content-Type: application/pgp-encrypted\r\n" +
	"\r\n" +
	"Version: 1\r\n" +
	"--b\r\n" +
	"Content-Type: application/octet-stream\r\n" +
	"\r\n" +
	"-----BEGIN PGP MESSAGE-----\r\n" +
	"hQEMA\r\n" +
	"-----END PGP MESSAGE-----\r\n" +
	"--b--\r\n"

// The bulk offline download stores through StoreFetched: it has to fill the
// list stub in place, keep the PGP source so the message can be decrypted
// offline, and leave attachment files out when the user asked for that.
func TestStoreFetchedWithoutAttachmentsFillsStubAndKeepsPGPSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	stubID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: "E1",
		Subject: "secret", Date: time.Now(),
	})
	if err != nil {
		t.Fatalf("stub: %v", err)
	}

	id, err := StoreFetched(ctx, db, slog.New(slog.DiscardHandler), folder, Fetched{
		RemoteID: "E1",
		Raw:      []byte(pgpMIMERaw),
		Subject:  "secret",
		Date:     time.Now(),
		Attachments: []Attachment{{
			Filename: "encrypted.asc", ContentType: "application/octet-stream", Content: []byte("x"),
		}},
	}, false)
	if err != nil {
		t.Fatalf("StoreFetched: %v", err)
	}
	if id != stubID {
		t.Fatalf("stored id %d, want the stub row %d filled in place", id, stubID)
	}
	m, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !m.BodyComplete {
		t.Error("the stub was not marked body-complete")
	}
	atts, err := db.ListAttachments(ctx, id)
	if err != nil {
		t.Fatalf("attachments: %v", err)
	}
	if len(atts) != 0 {
		t.Errorf("stored %d attachments, want none", len(atts))
	}
	src, err := db.MessagePGPSource(ctx, id)
	if err != nil {
		t.Fatalf("pgp source: %v", err)
	}
	if !bytes.Equal(src, []byte(pgpMIMERaw)) {
		t.Errorf("pgp source %q, want the raw message", src)
	}
}

// The From header is stored whole in FromAddress; the name shown in the list
// has to come from its first address, or filling a stub leaves no sender name.
func TestStoreFetchedSetsFromNameFromFromHeader(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, from, wantName string
	}{
		{"named", "Jane Doe <jane@x.org>", "Jane Doe"},
		{"quoted with comma", `"Doe, Jane" <jane@x.org>, bob@y.org`, "Doe, Jane"},
		{"bare address", "jane@x.org", ""},
		{"unparseable", "not an address", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{
				AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: "R1", Date: time.Now(),
			}); err != nil {
				t.Fatalf("stub: %v", err)
			}
			id, err := StoreFetched(ctx, db, slog.New(slog.DiscardHandler), folder, Fetched{
				RemoteID: "R1", From: tc.from, Date: time.Now(), Text: "hi",
			}, true)
			if err != nil {
				t.Fatalf("StoreFetched: %v", err)
			}
			m, err := db.GetMessage(ctx, id)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if m.FromName != tc.wantName {
				t.Errorf("FromName %q, want %q", m.FromName, tc.wantName)
			}
			if m.FromAddress != tc.from {
				t.Errorf("FromAddress %q, want the whole From header %q", m.FromAddress, tc.from)
			}
		})
	}
}

// A body fill that lands after the user moved or archived the message must
// not bring it back: the stub is gone, so nothing is stored or counted as new.
func TestFetchBodiesDoesNotReviveDeletedStub(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		during bool // delete while the body is in flight rather than before
	}{
		{"deleted before fetch", false},
		{"deleted during fetch", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			adapter := &fakeAdapter{ids: []string{"2", "1"}, listMeta: true}
			engine := NewEngine(adapter, db, nil)
			stubs, err := engine.SyncFolderStubs(ctx, folder, 0)
			if err != nil {
				t.Fatal(err)
			}
			archive := func() {
				m, err := messageByRemote(t, db, folder.ID, "2")
				if err != nil {
					t.Fatal(err)
				}
				if err := db.DeleteCachedMessage(ctx, folder.AccountID, m.ID); err != nil {
					t.Fatal(err)
				}
			}
			if tc.during {
				adapter.onFetch = func(id string) {
					if id == "2" {
						archive()
					}
				}
			} else {
				archive()
			}

			res, err := engine.FetchBodies(ctx, folder, stubs.ToFetch)
			if err != nil {
				t.Fatalf("fetch bodies: %v", err)
			}
			if res.New != 1 || len(res.NewIDs) != 1 {
				t.Fatalf("new = %d %v, want only 1", res.New, res.NewIDs)
			}
			if has := remoteIDsPresent(t, db, folder.ID); has["2"] || !has["1"] {
				t.Fatalf("cached %v, want 1 only", has)
			}
		})
	}
}

// attAdapter serves every fetched message with one attachment.
type attAdapter struct{ *fakeAdapter }

func (a *attAdapter) Fetch(ctx context.Context, box string, remoteIDs []string) ([]Fetched, error) {
	out, err := a.fakeAdapter.Fetch(ctx, box, remoteIDs)
	for i := range out {
		out[i].Attachments = []Attachment{{Filename: "a.pdf", ContentType: "application/pdf", Content: []byte("x")}}
	}
	return out, err
}

// An engine set WithoutAttachments fills a stub body-complete with no
// attachment files; by default the files are stored.
func TestFetchBodiesWithoutAttachments(t *testing.T) {
	t.Parallel()
	for _, without := range []bool{false, true} {
		ctx := context.Background()
		db, folder := newSyncTestFolder(t)
		seedLocalStub(t, db, folder, "7")
		engine := NewEngine(&attAdapter{&fakeAdapter{ids: fakeIDs(7)}}, db, nil)
		engine.WithoutAttachments = without
		if _, err := engine.FetchBodies(ctx, folder, []string{"7"}); err != nil {
			t.Fatalf("FetchBodies: %v", err)
		}
		id, err := db.MessageIDByRemoteID(ctx, folder.ID, "7")
		if err != nil {
			t.Fatal(err)
		}
		m, err := db.GetMessage(ctx, id)
		if err != nil || !m.BodyComplete {
			t.Fatalf("without=%v: body complete %v (%v), want the stub filled", without, m != nil && m.BodyComplete, err)
		}
		atts, err := db.ListAttachments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{false: 1, true: 0}[without]; len(atts) != want {
			t.Fatalf("without=%v: %d attachments, want %d", without, len(atts), want)
		}
	}
}
