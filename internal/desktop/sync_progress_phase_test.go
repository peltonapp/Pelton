package desktop

import "testing"

func TestSyncProgressStubPhaseOnEmit(t *testing.T) {
	var got SyncProgressEvent
	app := &App{}
	app.syncProgressEmitForTest = func(e SyncProgressEvent) { got = e }

	app.emitSyncProgress(1, "user@example.com", "mail.example", syncCounts{
		Folder:       "INBOX",
		FoldersDone:  0,
		FoldersTotal: 3,
		Phase:        SyncPhaseStubs,
	})
	if got.Phase != SyncPhaseStubs {
		t.Fatalf("phase=%q, want %q", got.Phase, SyncPhaseStubs)
	}
	if got.Folder != "INBOX" || got.FoldersTotal != 3 {
		t.Fatalf("event=%+v, want INBOX folder line with 3 mailboxes", got)
	}
}
