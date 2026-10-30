package desktop

import (
	"database/sql"
	"fmt"
	"io"
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
	fakeIMAP
	raw     []byte
	fetched []uint32
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

func TestFetchAndPinPreservesThreadingMetadata(t *testing.T) {
	for _, includeAttachments := range []bool{false, true} {
		for _, tt := range threadingCases() {
			t.Run(fmt.Sprintf("attachments=%t/%s", includeAttachments, tt.name), func(t *testing.T) {
				app, db, accountID := pgpTestApp(t, nil, nil)
				folder := addFolder(t, app, accountID, "INBOX")
				client := &threadingClient{raw: []byte(tt.headers + threadingBody)}
				if err := app.fetchAndPin(client, dlTask{folder: *folder, uid: 73}, includeAttachments); err != nil {
					t.Fatalf("fetchAndPin: %v", err)
				}
				messages, err := db.ListMessages(app.ctx, folder.ID, 0)
				if err != nil || len(messages) != 1 {
					t.Fatalf("messages = %+v, %v", messages, err)
				}
				m, err := db.GetMessage(app.ctx, messages[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(m.InReplyTo, tt.inReplyTo) || !reflect.DeepEqual(m.References, tt.references) {
					t.Errorf("stored ancestry = %#v / %#v, want %#v / %#v", m.InReplyTo, m.References, tt.inReplyTo, tt.references)
				}
				if !m.Offline || m.UID != 73 || m.MessageID != "message@example.com" || m.BodyPlain != "readable body" || m.BodyHTML != "<p>readable body</p>" {
					t.Errorf("stored message = %+v", m)
				}
				if !reflect.DeepEqual(client.fetched, []uint32{73}) {
					t.Errorf("fetched = %v, want only uncached UID 73", client.fetched)
				}
				atts, err := db.ListAttachments(app.ctx, m.ID)
				want := 0
				if includeAttachments {
					want = 1
				}
				if err != nil || len(atts) != want || m.HasAttachments != includeAttachments {
					t.Fatalf("stored attachments = %+v, %v; hasAttachments=%t", atts, err, m.HasAttachments)
				}
				if includeAttachments {
					if atts[0].Filename != "note.txt" {
						t.Fatalf("attachment filename = %q", atts[0].Filename)
					}
					r, err := db.OpenAttachment(atts[0].DiskPath)
					if err != nil {
						t.Fatal(err)
					}
					defer r.Close()
					content, err := io.ReadAll(r)
					if err != nil || string(content) != "attachment bytes" {
						t.Fatalf("attachment content = %q, %v", content, err)
					}
				}
			})
		}
	}
}

func TestGetMessageThreadingFromMIMEAndLegacyNull(t *testing.T) {
	app, db, accountID := pgpTestApp(t, nil, nil)
	folder := addFolder(t, app, accountID, "INBOX")
	client := &threadingClient{raw: []byte(
		"In-Reply-To: <Parent@Example.com>\r\n" +
			"References: <Root@Example.com>\r\n <middle@example.com>\r\n\t<Parent@Example.com>\r\n" +
			threadingBody,
	)}
	// The client parses raw MIME into the IMAP model; fetchAndPin uses the production storage writer.
	if err := app.fetchAndPin(client, dlTask{folder: *folder, uid: 73}, false); err != nil {
		t.Fatalf("fetchAndPin: %v", err)
	}
	messages, err := db.ListMessages(app.ctx, folder.ID, 0)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages = %+v, %v, want one fetched message", messages, err)
	}
	detail, err := app.GetMessage(messages[0].ID)
	if err != nil {
		t.Fatalf("GetMessage fetched message: %v", err)
	}
	wantInReplyTo := []string{"Parent@Example.com"}
	wantReferences := []string{"Root@Example.com", "middle@example.com", "Parent@Example.com"}
	if !reflect.DeepEqual(detail.InReplyTo, wantInReplyTo) || !reflect.DeepEqual(detail.References, wantReferences) {
		t.Errorf("MIME ancestry DTO = %#v / %#v, want %#v / %#v", detail.InReplyTo, detail.References, wantInReplyTo, wantReferences)
	}

	legacyID, err := db.InsertMessage(app.ctx, &storage.Message{
		AccountID: accountID, FolderID: folder.ID, UID: 74,
		MessageID: "legacy@example.com", BodyPlain: "legacy body",
	})
	if err != nil {
		t.Fatalf("insert legacy message: %v", err)
	}
	rawDB, err := sql.Open("sqlite", db.Path())
	if err != nil {
		t.Fatalf("open raw storage: %v", err)
	}
	defer rawDB.Close()
	var inReplyToNull, referencesNull bool
	if err := rawDB.QueryRowContext(app.ctx,
		`SELECT in_reply_to IS NULL, "references" IS NULL FROM messages WHERE id = ?`, legacyID,
	).Scan(&inReplyToNull, &referencesNull); err != nil {
		t.Fatalf("read legacy columns: %v", err)
	}
	if !inReplyToNull || !referencesNull {
		t.Fatalf("legacy SQL NULL columns = %t / %t, want both true", inReplyToNull, referencesNull)
	}
	legacy, err := app.GetMessage(legacyID)
	if err != nil {
		t.Fatalf("GetMessage legacy message: %v", err)
	}
	if !reflect.DeepEqual(legacy.InReplyTo, []string{}) || !reflect.DeepEqual(legacy.References, []string{}) {
		t.Errorf("legacy ancestry DTO = %#v / %#v, want non-nil empty arrays", legacy.InReplyTo, legacy.References)
	}
}
