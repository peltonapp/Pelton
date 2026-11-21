package sync

import (
	"context"
	"reflect"
	"testing"

	"github.com/emersion/go-imap/v2"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/rfc822"
	"github.com/peltonapp/Pelton/internal/storage"
)

const threadingBody = "MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=threading\r\n\r\n" +
	"--threading\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nreadable body\r\n" +
	"--threading\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>readable body</p>\r\n" +
	"--threading\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=note.txt\r\n\r\nattachment bytes\r\n" +
	"--threading--\r\n"

func threadingCases() []struct {
	name       string
	headers    string
	inReplyTo  []string
	references []string
} {
	return []struct {
		name       string
		headers    string
		inReplyTo  []string
		references []string
	}{
		{"single parent and folded references", "In-Reply-To: <parent@example.com>\r\nReferences: <root@example.com>\r\n <root@example.com> <parent@example.com>\r\n", []string{"parent@example.com"}, []string{"root@example.com", "root@example.com", "parent@example.com"}},
		{"multiple parents preserve duplicates", "In-Reply-To: <one@example.com> <two@example.com> <one@example.com>\r\n", []string{"one@example.com", "two@example.com", "one@example.com"}, []string{}},
		{"absent headers are known empty", "", []string{}, []string{}},
		{"invalid parent does not block body", "In-Reply-To: <parent@example.com> invalid\r\nReferences: <root@example.com>\r\n", []string{}, []string{"root@example.com"}},
		{"invalid headers are known empty", "In-Reply-To: invalid\r\nReferences: invalid\r\n", []string{}, []string{}},
	}
}

type threadingClient struct {
	fakeClient
	raw []byte
}

func (c *threadingClient) FetchMessage(uid imap.UID) (*pimap.Message, error) {
	c.fetched = append(c.fetched, uint32(uid))
	parsed, err := rfc822.ParseBody(c.raw)
	if err != nil {
		return nil, err
	}
	return &pimap.Message{
		UID: uid, MessageID: "message@example.com", Subject: "threading",
		From: "alice@example.com", To: "bob@example.com",
		Text: parsed.Text, HTML: parsed.HTML, Size: parsed.Size, Raw: c.raw,
		Attachments: parsed.Attachments, CharsetGuess: parsed.CharsetGuess,
		ListUnsubscribe: parsed.ListUnsubscribe, ListUnsubscribePost: parsed.ListUnsubscribePost,
		ReplyTo: parsed.ReplyTo, AuthResults: parsed.AuthResults,
		InReplyTo: parsed.InReplyTo, References: parsed.References,
	}, nil
}

func (c *threadingClient) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.commands++
	for _, uid := range uids {
		msg, err := c.FetchMessage(uid)
		if err := fn(uid, msg, err); err != nil {
			return err
		}
	}
	return nil
}

func TestSyncStoresThreadingMetadata(t *testing.T) {
	for _, tt := range threadingCases() {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			uids := []uint32{7, 19}
			client := &threadingClient{fakeClient: fakeClient{uids: uids}, raw: []byte(tt.headers + threadingBody)}
			engine := NewEngine(client, db, nil)
			res, err := engine.SyncFolder(ctx, folder)
			if err != nil || res.New != len(uids) || len(res.NewIDs) != len(uids) {
				t.Fatalf("SyncFolder = %+v, %v", res, err)
			}
			if want := (len(uids) + fetchBatch - 1) / fetchBatch; client.commands != want || len(client.fetched) != len(uids) {
				t.Fatalf("fetch commands = %d, bodies = %v; want %d commands, %d bodies", client.commands, client.fetched, want, len(uids))
			}
			for _, id := range res.NewIDs {
				m, err := db.GetMessage(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(m.InReplyTo, tt.inReplyTo) || !reflect.DeepEqual(m.References, tt.references) {
					t.Errorf("stored ancestry = %#v / %#v, want %#v / %#v", m.InReplyTo, m.References, tt.inReplyTo, tt.references)
				}
				if m.BodyPlain != "readable body" || m.BodyHTML != "<p>readable body</p>" || m.MessageID != "message@example.com" || (m.UID != 7 && m.UID != 19) {
					t.Errorf("stored message = %+v", m)
				}
				atts, err := db.ListAttachments(ctx, id)
				if err != nil || len(atts) != 1 || atts[0].Filename != "note.txt" {
					t.Fatalf("stored attachments = %+v, %v", atts, err)
				}
			}
			res, err = engine.SyncFolder(ctx, folder)
			if err != nil || res.New != 0 || len(client.fetched) != len(uids) {
				t.Fatalf("resync = %+v, %v; fetched %v", res, err, client.fetched)
			}
		})
	}
}

func TestSyncDoesNotBackfillLegacyThreadingMetadata(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	id, err := db.InsertMessage(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, BodyPlain: "legacy body"})
	if err != nil {
		t.Fatal(err)
	}
	client := &threadingClient{fakeClient: fakeClient{uids: []uint32{7}}, raw: []byte(threadingBody)}
	if _, err := NewEngine(client, db, nil).SyncFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	m, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if m.InReplyTo != nil || m.References != nil || m.BodyPlain != "legacy body" || len(client.fetched) != 0 || client.commands != 0 {
		t.Fatalf("legacy row changed or fetched: message=%+v, fetched=%v, commands=%d", m, client.fetched, client.commands)
	}
}
