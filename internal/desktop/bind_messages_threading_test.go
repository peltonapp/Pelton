package desktop

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestGetMessageThreadingMetadata(t *testing.T) {
	for _, tt := range []struct {
		name, stored, want string
	}{
		{"bare", "Parent@Example.com", "Parent@Example.com"},
		{"bracketed", "<Parent@Example.com>", "Parent@Example.com"},
		{"invalid", "<<Parent@Example.com>>", ""},
		{"missing", "", ""},
		{"IDN parent", "<parent@bücher.de>", ""},
		{"trailing dot parent", "<parent@example.com.>", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app, db, accountID := pgpTestApp(t, nil, nil)
			folder := addFolder(t, app, accountID, "Inbox")
			id, err := db.InsertMessage(app.ctx, &storage.Message{
				AccountID: accountID, FolderID: folder.ID, UID: 1,
				MessageID: tt.stored, FromAddress: "sender@example.com",
				Subject: "parent", BodyPlain: "readable body", Date: time.Now(),
			})
			if err != nil {
				t.Fatalf("insert message: %v", err)
			}
			detail, err := app.GetMessage(id)
			if err != nil {
				t.Fatalf("GetMessage: %v", err)
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
				"inReplyTo": "[]", "references": "[]",
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
