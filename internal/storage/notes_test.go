package storage

import (
	"context"
	"errors"
	"testing"
)

func TestNotesStoreAndSearch(t *testing.T) {
	f := newThreadFixture(t)
	ctx := context.Background()
	m := f.add(Message{MessageID: "<m@x>", Subject: "contract", FromAddress: "ada@x.test"}, 1)
	bare := f.add(Message{Subject: "no id", FromAddress: "bob@x.test"}, 2)
	thread := f.add(Message{MessageID: "<t@x>", Subject: "trip", FromAddress: "cy@x.test"}, 3)
	reply := f.add(Message{MessageID: "<t2@x>", Subject: "Re: trip", FromAddress: "cy@x.test", InReplyTo: []string{"<t@x>"}}, 4)

	if n, err := f.db.GetNote(ctx, m.ID, false); err != nil || n != nil {
		t.Fatalf("empty note = %+v, %v", n, err)
	}
	saved, err := f.db.SaveNote(ctx, m.ID, false, "Call Ada about clause 4")
	if err != nil || saved == nil || saved.Body != "Call Ada about clause 4" {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	if _, err := f.db.SaveNote(ctx, m.ID, false, "Call Ada about clause 5"); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.db.GetNote(ctx, m.ID, false); n == nil || n.Body != "Call Ada about clause 5" {
		t.Errorf("updated note = %+v", n)
	}
	if _, err := f.db.SaveNote(ctx, bare.ID, false, "no message id here"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SaveNote(ctx, reply.ID, true, "Book the train"); err != nil {
		t.Fatal(err)
	}

	// the note marks the rows it belongs to.
	got, err := f.db.GetMessage(ctx, thread.ID)
	if err != nil || !got.HasNote {
		t.Errorf("thread member has note = %v, %v", got, err)
	}
	got, _ = f.db.GetMessage(ctx, bare.ID)
	if !got.HasNote {
		t.Error("message without a Message-ID lost its note mark")
	}

	ids, err := f.db.NoteMessageIDs(ctx, "TRAIN")
	if err != nil {
		t.Fatal(err)
	}
	sameIDs(t, "note:train", ids, reply.ID, thread.ID)
	ids, _ = f.db.NoteMessageIDs(ctx, "clause")
	sameIDs(t, "note:clause", ids, m.ID)
	ids, _ = f.db.NoteMessageIDs(ctx, "")
	sameIDs(t, "has:note", ids, reply.ID, thread.ID, bare.ID, m.ID)

	// a blank body deletes.
	if n, err := f.db.SaveNote(ctx, m.ID, false, "  "); err != nil || n != nil {
		t.Errorf("blank save = %+v, %v", n, err)
	}
	if n, _ := f.db.GetNote(ctx, m.ID, false); n != nil {
		t.Errorf("note survived a blank save: %+v", n)
	}
	if err := f.db.DeleteNote(ctx, reply.ID, true); err != nil {
		t.Fatal(err)
	}
	ids, _ = f.db.NoteMessageIDs(ctx, "")
	sameIDs(t, "after deletes", ids, bare.ID)

	if _, err := f.db.GetNote(ctx, 9999, false); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("missing message err = %v", err)
	}
}
