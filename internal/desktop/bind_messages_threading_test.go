package desktop

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/mail"
	"strconv"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
)

func TestGetMessageThreadingMetadata(t *testing.T) {
	for _, tt := range []struct {
		name, stored, want            string
		inReplyTo, references         []string
		wantInReplyTo, wantReferences string
		rawAncestry                   string
	}{
		{name: "bare", stored: "Parent@Example.com", want: "Parent@Example.com"},
		{name: "bracketed", stored: "<Parent@Example.com>", want: "Parent@Example.com"},
		{name: "invalid", stored: "<<Parent@Example.com>>"},
		{name: "missing"},
		{name: "IDN parent", stored: "<parent@bücher.de>"},
		{name: "trailing dot parent", stored: "<parent@example.com.>"},
		{name: "legacy nil", stored: "parent@example.com", want: "parent@example.com"},
		{name: "known empty", stored: "parent@example.com", want: "parent@example.com", inReplyTo: []string{}, references: []string{}},
		{name: "corrupt JSON", stored: "parent@example.com", want: "parent@example.com", rawAncestry: "["},
		{name: "JSON null", stored: "parent@example.com", want: "parent@example.com", rawAncestry: "null"},
		{name: "non-array JSON", stored: "parent@example.com", want: "parent@example.com", rawAncestry: "{}"},
		{name: "non-string JSON element", stored: "parent@example.com", want: "parent@example.com", rawAncestry: `["root@example.com",1]`},
		{
			name: "ordered mixed bracketed and bare ancestry", stored: "<parent@example.com>", want: "parent@example.com",
			inReplyTo: []string{"<A@Example.com>", "b@example.com"}, references: []string{"<Root@Example.com>", "a@example.com", "<parent@example.com>", "a@example.com"},
			wantInReplyTo: `["A@Example.com","b@example.com"]`, wantReferences: `["Root@Example.com","a@example.com","parent@example.com","a@example.com"]`,
		},
		{
			name: "invalid IRT clears entire list", stored: "parent@example.com", want: "parent@example.com",
			inReplyTo: []string{"a@example.com", "<<bad@example.com>>", "b@example.com"},
		},
		{
			name: "duplicate IRT stays duplicated", stored: "parent@example.com", want: "parent@example.com",
			inReplyTo: []string{"<a@example.com>", "a@example.com"}, wantInReplyTo: `["a@example.com","a@example.com"]`,
		},
		{
			name: "invalid References elements are filtered", stored: "parent@example.com", want: "parent@example.com",
			references:     []string{"<<bad@example.com>>", "<root@example.com>", "bad\r\nBcc: attacker@example.com", "a@example.com", "trailing@example.com."},
			wantReferences: `["root@example.com","a@example.com"]`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app, db, accountID := pgpTestApp(t, nil, nil)
			folder := addFolder(t, app, accountID, "Inbox")
			id, err := db.InsertMessage(app.ctx, &storage.Message{
				AccountID: accountID, FolderID: folder.ID, UID: 1,
				MessageID: tt.stored, InReplyTo: tt.inReplyTo, References: tt.references, FromAddress: "sender@example.com",
				Subject: "parent", BodyPlain: "readable body", Date: time.Now(),
			})
			if err != nil {
				t.Fatalf("insert message: %v", err)
			}
			if tt.rawAncestry != "" {
				rawDB, err := sql.Open("sqlite", db.Path())
				if err != nil {
					t.Fatalf("open raw storage: %v", err)
				}
				defer rawDB.Close()
				if _, err := rawDB.ExecContext(app.ctx, `UPDATE messages SET in_reply_to = ?, "references" = ? WHERE id = ?`, tt.rawAncestry, tt.rawAncestry, id); err != nil {
					t.Fatalf("write raw ancestry: %v", err)
				}
			}
			detail, err := app.GetMessage(id)
			if err != nil {
				t.Fatalf("GetMessage: %v", err)
			}
			if detail.BodyPlain != "readable body" {
				t.Errorf("detail body = %q, want readable body", detail.BodyPlain)
			}
			if tt.wantInReplyTo == "" {
				tt.wantInReplyTo = "[]"
			}
			if tt.wantReferences == "" {
				tt.wantReferences = "[]"
			}
			if tt.want == "" {
				msg, err := app.buildMessage(ComposeRequest{
					AccountID: accountID, To: addr("recipient@example.com"), Text: "reply",
					InReplyTo: detail.MessageID, References: []string{"root@example.com"},
				})
				if err != nil {
					t.Fatalf("build reply: %v", err)
				}
				raw, _, err := smtp.BuildDraft(msg)
				if err != nil {
					t.Fatalf("serialize reply: %v", err)
				}
				parsed, err := mail.ReadMessage(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("parse reply: %v", err)
				}
				header := parsed.Header
				if header.Get("In-Reply-To") != "" || header.Get("References") != "" {
					t.Fatalf("reply to invalid parent has threading headers: %v", header)
				}
			}
			data, err := json.Marshal(detail)
			if err != nil {
				t.Fatalf("marshal detail: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatalf("decode detail: %v", err)
			}
			for key, want := range map[string]string{
				"messageId": strconv.Quote(tt.want), "accountId": strconv.FormatInt(accountID, 10),
				"inReplyTo": tt.wantInReplyTo, "references": tt.wantReferences,
			} {
				if got := string(fields[key]); got != want {
					t.Errorf("detail.%s = %q, want %q", key, got, want)
				}
			}
			list, err := app.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: folder.ID, Limit: 10})
			if err != nil || len(list.Messages) != 1 {
				t.Fatalf("ListMessages = %+v, %v, want one summary", list, err)
			}
			for name, dto := range map[string]any{
				"list": list, "search": SearchResultDTO{Messages: list.Messages, Total: 1},
			} {
				data, err := json.Marshal(dto)
				if err != nil {
					t.Fatalf("marshal %s: %v", name, err)
				}
				var page struct {
					Messages []map[string]json.RawMessage `json:"messages"`
				}
				if err := json.Unmarshal(data, &page); err != nil {
					t.Fatalf("decode %s: %v", name, err)
				}
				for _, key := range []string{"messageId", "inReplyTo", "references"} {
					if _, found := page.Messages[0][key]; found {
						t.Errorf("%s summary contains detail-only key %q", name, key)
					}
				}
			}
		})
	}
}
