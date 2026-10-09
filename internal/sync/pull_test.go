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
