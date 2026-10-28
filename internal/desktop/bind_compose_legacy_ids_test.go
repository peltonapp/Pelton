package desktop

import (
	"slices"
	"testing"
)

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
