package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

func emlTestMessages(t *testing.T, a *App, subjects ...string) []*storage.Message {
	t.Helper()
	accountID := seedAccount(t, a)
	folder := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := a.store.CreateFolder(a.ctx, folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	var out []*storage.Message
	for i, subject := range subjects {
		id, err := a.store.InsertMessage(a.ctx, &storage.Message{
			AccountID: accountID,
			FolderID:  folder.ID,
			UID:       uint32(i + 1),
			Subject:   subject,
			Date:      time.Date(2026, 3, 7, 14, 5, 9, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("insert message: %v", err)
		}
		m, err := a.store.GetMessage(a.ctx, id)
		if err != nil {
			t.Fatalf("get message: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func TestExportEMLWritesOneFilePerMessage(t *testing.T) {
	a := newTrashTestApp(t)
	msgs := emlTestMessages(t, a, "Invoice", "Invoice", "Hello")
	dir := t.TempDir()

	result := a.exportEMLTo(dir, msgs, func(ms []*storage.Message) []emlSource {
		out := make([]emlSource, len(ms))
		for i, m := range ms {
			out[i].raw = []byte("Subject: " + m.Subject + "\r\n\r\nbody")
		}
		return out
	})

	if result.Exported != 3 || result.Failed != 0 || result.Dir != dir {
		t.Fatalf("result = %+v, want 3 exported into %s", result, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("wrote %d files, want 3 (same subject must not overwrite)", len(entries))
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".eml" {
			t.Errorf("file %q is not an .eml", e.Name())
		}
	}
}

func TestExportEMLCountsFailuresAndKeepsGoing(t *testing.T) {
	a := newTrashTestApp(t)
	msgs := emlTestMessages(t, a, "One", "Two")
	dir := t.TempDir()

	result := a.exportEMLTo(dir, msgs, func(ms []*storage.Message) []emlSource {
		return []emlSource{{err: errors.New("fetch failed")}, {raw: []byte("Subject: Two\r\n\r\nx")}}
	})

	if result.Exported != 1 || result.Failed != 1 || result.FirstError != "fetch failed" {
		t.Fatalf("result = %+v, want 1 exported, 1 failed, the fetch error", result)
	}
}

func TestFetchEMLSourcesRefusesLocalFolders(t *testing.T) {
	a := newTrashTestApp(t)
	account, err := a.store.EnsureLocalAccount(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := a.store.EnsureLocalFolder(a.ctx, account.ID, "Imported")
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.store.InsertMessage(a.ctx, &storage.Message{AccountID: account.ID, FolderID: folder.ID, UID: 1, Subject: "x", Date: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got := a.fetchEMLSources([]*storage.Message{m})
	if got[0].err == nil {
		t.Error("a Local Folders message produced a source, want an error")
	}
}

func TestExportMessagesEMLRejectsEmptySelection(t *testing.T) {
	a := newTrashTestApp(t)
	if _, err := a.ExportMessagesEML(nil); err == nil {
		t.Error("an empty selection was accepted")
	}
}
