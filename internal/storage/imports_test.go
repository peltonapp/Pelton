package storage

import (
	"context"
	"testing"
)

func TestImportJobLifecycle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	job := &ImportJob{SourceKind: "files", SourcePath: "/tmp/x", Plan: `{"a":1}`}
	id, err := db.CreateImportJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetImportPosition(ctx, id, "mail:Inbox", ImportPosition{Position: 42}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetImportPosition(ctx, id, "mail:Inbox", ImportPosition{Position: 99, Done: true}); err != nil {
		t.Fatal(err)
	}
	pos, err := db.ImportPositions(ctx, id)
	if err != nil || pos["mail:Inbox"] != (ImportPosition{Position: 99, Done: true}) {
		t.Fatalf("positions = %+v %v", pos, err)
	}
	n, err := db.InterruptImportJobs(ctx)
	if err != nil || n != 1 {
		t.Fatalf("interrupted %d %v", n, err)
	}
	got, err := db.GetImportJob(ctx, id)
	if err != nil || got.State != ImportPaused || got.Plan != `{"a":1}` {
		t.Fatalf("job = %+v %v", got, err)
	}
	if err := db.SaveImportJobState(ctx, id, ImportDone, `{"p":1}`, `{"r":1}`, ""); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ListImportJobs(ctx, 5)
	if err != nil || len(jobs) != 1 || jobs[0].State != ImportDone || jobs[0].Report != `{"r":1}` {
		t.Fatalf("jobs = %+v %v", jobs, err)
	}
	if err := db.SaveImportJobState(ctx, id+10, ImportDone, "", "", ""); err != ErrImportJobNotFound {
		t.Fatalf("missing job err = %v", err)
	}
}

func TestImportLedgerFollowsTheLocalMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	account, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := db.EnsureLocalFolderPath(ctx, account.ID, []string{"Imported", "Archive", "2019"})
	if err != nil {
		t.Fatal(err)
	}
	msg := &Message{AccountID: account.ID, FolderID: folder.ID, UID: 1, MessageID: "<a@example.org>", Subject: "hi"}
	id, err := db.InsertMessage(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecordImportDelivery(ctx, "local:1", "fp", id); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordImportDelivery(ctx, "local:1", "fp", id); err != nil {
		t.Fatalf("a second record of the same delivery failed: %v", err)
	}
	if ok, err := db.ImportDelivered(ctx, "local:1", "fp"); err != nil || !ok {
		t.Fatalf("delivered = %v %v", ok, err)
	}
	if ok, _ := db.ImportDelivered(ctx, "local:2", "fp"); ok {
		t.Fatal("a delivery counted for another destination")
	}
	if err := db.DeleteMessage(ctx, id); err != nil {
		t.Fatal(err)
	}
	if ok, _ := db.ImportDelivered(ctx, "local:1", "fp"); ok {
		t.Fatal("the ledger kept a message that was deleted")
	}
	if err := db.RecordImportDelivery(ctx, "upload:1:INBOX", "fp", 0); err != nil {
		t.Fatal(err)
	}
	if ok, _ := db.ImportDelivered(ctx, "upload:1:INBOX", "fp"); !ok {
		t.Fatal("an upload delivery was not recorded")
	}
}

func TestEnsureLocalFolderPathNests(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	account, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := db.EnsureLocalFolderPath(ctx, account.ID, []string{"Imported", "a/b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.EnsureLocalFolderPath(ctx, account.ID, []string{"Imported", "a/b", "c"})
	if err != nil || again.ID != leaf.ID {
		t.Fatalf("second call made another folder: %d vs %d %v", again.ID, leaf.ID, err)
	}
	if leaf.Name != "c" || leaf.IMAPPath != "Imported/a∕b/c" || leaf.ParentID == nil {
		t.Fatalf("leaf = %+v", leaf)
	}
	parent, err := db.GetFolder(ctx, *leaf.ParentID)
	if err != nil || parent.Name != "a∕b" || parent.ParentID == nil {
		t.Fatalf("parent = %+v %v", parent, err)
	}
	folders, _ := db.ListFolders(ctx, account.ID)
	if len(folders) != 3 {
		t.Fatalf("%d folders, want 3", len(folders))
	}
}
