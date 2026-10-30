package desktop

import (
	"bytes"
	"net/mail"
	"slices"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/messageid"
	"github.com/peltonapp/Pelton/internal/smtp"
)

func TestGeneratedDraftMessageIDRoundTrip(t *testing.T) {
	for _, tt := range []struct{ email, domain string }{
		{"me@bücher.de", "xn--bcher-kva.de"},
		{"me@exämple.com", "xn--exmple-cua.com"},
		{"me@example.com.", "localhost"},
		{"me@example.com", "example.com"},
	} {
		t.Run(tt.email, func(t *testing.T) {
			generated, err := smtp.NewMessageID(tt.email)
			if err != nil {
				t.Fatalf("NewMessageID: %v", err)
			}
			if _, ok := messageid.Normalize(generated); !ok {
				t.Errorf("NewMessageID = %q, want strict reply token", generated)
			}
			app, accountID, client := draftTestApp(t)
			account, err := app.store.GetAccount(app.ctx, accountID)
			if err != nil {
				t.Fatalf("GetAccount: %v", err)
			}
			account.Email = tt.email
			if err := app.store.UpdateAccount(app.ctx, account); err != nil {
				t.Fatalf("UpdateAccount: %v", err)
			}
			req := ComposeRequest{AccountID: accountID, Subject: "first", Text: "body", Bcc: addr("hidden@example.com")}
			id := saveDraft(t, app, 0, req)
			uploaded, err := app.UploadDraft(id)
			settle(t)
			if err != nil {
				t.Fatalf("UploadDraft: %v", err)
			}
			if _, ok := messageid.Normalize(uploaded); !ok || !strings.HasSuffix(uploaded, "@"+tt.domain+">") {
				t.Errorf("uploaded ID = %q, want strict token with domain %q", uploaded, tt.domain)
			}
			if len(client.drafts) != 1 || client.drafts[0].messageID != uploaded {
				t.Fatalf("uploads = %+v, want one linked to %q", client.drafts, uploaded)
			}
			parsed, err := mail.ReadMessage(bytes.NewReader(client.drafts[0].raw))
			if err != nil || parsed.Header.Get("Message-ID") != uploaded {
				t.Fatalf("uploaded MIME = %v, %v, want Message-ID %q", parsed, err, uploaded)
			}
			req.DraftMessageID = uploaded
			req.Subject = "edited"
			saveDraft(t, app, id, req)
			cached := draftsOnServer(t, app, accountID, uploaded)
			opened, err := app.OpenDraft(cached[0])
			if err != nil || opened.ID != id || opened.Request.Subject != "edited" || opened.Request.DraftMessageID != uploaded || !slices.Equal(opened.Request.Bcc, req.Bcc) {
				t.Fatalf("OpenDraft = %+v, %v, want edited draft %d with original ID and Bcc", opened, err, id)
			}
		})
	}
}

func TestLegacyStoredDraftMessageIDs(t *testing.T) {
	for _, messageID := range []string{"<draft@bücher.de>", "<draft@exämple.com>", "<draft@example.com.>"} {
		for _, operation := range []string{"resave and reopen", "delete", "seal with omitted ID"} {
			t.Run(messageID+"/"+operation, func(t *testing.T) {
				var own []string
				if operation == "seal with omitted ID" {
					own = []string{"me@example.com"}
				}
				app, _, accountID := pgpTestApp(t, own, nil)
				client := &fakeIMAP{}
				useFake(t, app, accountID, client)
				addFolder(t, app, accountID, "Drafts", `\Drafts`)
				// Seed the on-disk format directly: these identities predate the
				// current generator and must never be encoded or replaced.
				req := ComposeRequest{AccountID: accountID, Subject: "legacy", Text: "plain", DraftMessageID: messageID}
				const id = 1
				if err := app.store.SetJSON(app.ctx, draftsKey, []storedDraft{{ID: id, AccountID: accountID, Request: req}}); err != nil {
					t.Fatalf("seed legacy draft: %v", err)
				}
				switch operation {
				case "resave and reopen":
					req.Subject = "legacy edited"
					saveDraft(t, app, id, req)
					cached := draftsOnServer(t, app, accountID, messageID)
					opened, err := app.OpenDraft(cached[0])
					if err != nil || opened.ID != id || opened.Request.Subject != req.Subject || opened.Request.DraftMessageID != messageID {
						t.Fatalf("OpenDraft = %+v, %v, want edited legacy draft with ID %q", opened, err, messageID)
					}
				case "delete":
					if err := app.DeleteDraft(id); err != nil {
						t.Fatalf("DeleteDraft: %v", err)
					}
					if drafts := listDrafts(t, app); len(drafts) != 0 {
						t.Fatalf("drafts after delete = %+v, want empty", drafts)
					}
				case "seal with omitted ID":
					req.DraftMessageID = ""
					req.Protection = protectionEncrypt
					saveDraft(t, app, id, req)
					var stored []storedDraft
					if err := app.store.GetJSON(app.ctx, draftsKey, &stored); err != nil {
						t.Fatalf("read sealed draft: %v", err)
					}
					if len(stored) != 1 || stored[0].Sealed == "" || stored[0].Request.DraftMessageID != messageID || stored[0].Request.Text != "" {
						t.Fatalf("stored = %+v, want sealed draft retaining legacy ID %q", stored, messageID)
					}
				}
				settle(t)
				if operation != "resave and reopen" && !slices.Equal(client.removedDrafts, []string{messageID}) {
					t.Fatalf("removed = %q, want exactly one removal of %q", client.removedDrafts, messageID)
				}
			})
		}
	}
}
