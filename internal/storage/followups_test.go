package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func addFollowUp(t *testing.T, f *threadFixture, messageID string, outbox int64) FollowUp {
	t.Helper()
	fu := FollowUp{
		AccountID:  f.account,
		MessageID:  messageID,
		OutboxID:   outbox,
		Subject:    "Quote",
		Recipients: "ada@x",
		SentAt:     f.base,
		DueAt:      f.base.Add(72 * time.Hour),
	}
	if _, err := f.db.AddFollowUp(context.Background(), &fu); err != nil {
		t.Fatal(err)
	}
	return fu
}

func TestFollowUpStoreRoundTrip(t *testing.T) {
	f := newThreadFixture(t)
	ctx := context.Background()
	later := addFollowUp(t, f, "<late@me>", 0)
	early := FollowUp{AccountID: f.account, MessageID: "early@me", OutboxID: 7, SentAt: f.base, DueAt: f.base.Add(time.Hour)}
	if _, err := f.db.AddFollowUp(ctx, &early); err != nil {
		t.Fatal(err)
	}

	list, err := f.db.ListFollowUps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != early.ID || list[1].ID != later.ID {
		t.Fatalf("list = %+v, want the earlier due first", list)
	}
	if list[1].MessageID != "late@me" {
		t.Fatalf("message id stored as %q, want it without brackets", list[1].MessageID)
	}
	if !list[0].FiredAt.IsZero() {
		t.Fatalf("a new reminder has fired at %v", list[0].FiredAt)
	}

	fired := f.base.Add(2 * time.Hour)
	if err := f.db.MarkFollowUpFired(ctx, early.ID, fired); err != nil {
		t.Fatal(err)
	}
	list, _ = f.db.ListFollowUps(ctx)
	if !list[0].FiredAt.Equal(fired) {
		t.Fatalf("fired at %v, want %v", list[0].FiredAt, fired)
	}

	if gone, err := f.db.DeleteFollowUpsForOutbox(ctx, 7); err != nil || !gone {
		t.Fatalf("delete for outbox = %v, %v", gone, err)
	}
	if gone, _ := f.db.DeleteFollowUpsForOutbox(ctx, 7); gone {
		t.Fatal("deleted a second time")
	}
	if err := f.db.DeleteFollowUp(ctx, later.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.DeleteFollowUp(ctx, later.ID); !errors.Is(err, ErrFollowUpNotFound) {
		t.Fatalf("second delete = %v, want ErrFollowUpNotFound", err)
	}
}

func TestFollowUpNeedsMessageID(t *testing.T) {
	f := newThreadFixture(t)
	fu := FollowUp{AccountID: f.account, MessageID: " <> ", SentAt: f.base, DueAt: f.base}
	if _, err := f.db.AddFollowUp(context.Background(), &fu); err == nil {
		t.Fatal("stored a reminder with no message id")
	}
}

func TestFollowUpReplied(t *testing.T) {
	cases := []struct {
		name  string
		reply *Message
		want  bool
	}{
		{name: "no reply yet", want: false},
		{
			name:  "a reply naming it in In-Reply-To",
			reply: &Message{MessageID: "r@x", Subject: "Re: Quote", FromAddress: "Ada <ada@x>", InReplyTo: []string{"<q@me>"}},
			want:  true,
		},
		{
			name: "a reply further down naming it in References",
			reply: &Message{MessageID: "r@x", Subject: "Re: Quote", FromAddress: "bob@x",
				InReplyTo: []string{"other@x"}, References: []string{"q@me", "other@x"}},
			want: true,
		},
		{
			name:  "a nudge the user sent themselves",
			reply: &Message{MessageID: "n@me", Subject: "Re: Quote", FromAddress: "me@example.com", InReplyTo: []string{"q@me"}},
			want:  false,
		},
		{
			name:  "same subject without threading headers",
			reply: &Message{MessageID: "s@x", Subject: "Re: Quote", FromAddress: "ada@x", ToAddresses: "me@example.com"},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newThreadFixture(t)
			f.add(Message{MessageID: "<q@me>", Subject: "Quote", FromAddress: "me@example.com", ToAddresses: "ada@x", FolderID: f.sent}, 0)
			fu := addFollowUp(t, f, "q@me", 0)
			if tc.reply != nil {
				f.add(*tc.reply, 5)
			}
			got, err := f.db.FollowUpReplied(context.Background(), fu)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("replied = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFollowUpRepliedWithoutSentCopy(t *testing.T) {
	f := newThreadFixture(t)
	fu := addFollowUp(t, f, "q@me", 0)
	if got, err := f.db.FollowUpReplied(context.Background(), fu); err != nil || got {
		t.Fatalf("replied = %v, %v before anything arrived", got, err)
	}
	f.add(Message{MessageID: "r@x", Subject: "Re: Quote", FromAddress: "ada@x", InReplyTo: []string{"q@me"}}, 3)
	if got, err := f.db.FollowUpReplied(context.Background(), fu); err != nil || !got {
		t.Fatalf("replied = %v, %v once the reply is in", got, err)
	}
}

func TestFollowUpRepliedStaysInItsAccount(t *testing.T) {
	db := newTestDB(t)
	mine := newThreadFixtureOn(t, db, "me@example.com")
	other := newThreadFixtureOn(t, db, "other@example.com")
	fu := addFollowUp(t, mine, "q@me", 0)
	other.add(Message{MessageID: "r@x", Subject: "Re: Quote", FromAddress: "ada@x", InReplyTo: []string{"q@me"}}, 3)
	if got, err := db.FollowUpReplied(context.Background(), fu); err != nil || got {
		t.Fatalf("replied = %v, %v from a reply in another account", got, err)
	}
}

func TestCopiesOf(t *testing.T) {
	f := newThreadFixture(t)
	sent := f.add(Message{MessageID: "<q@me>", Subject: "Quote", FolderID: f.sent}, 0)
	inbox := f.add(Message{MessageID: "q@me", Subject: "Quote"}, 0)
	copies, err := f.db.CopiesOf(context.Background(), f.account, "q@me")
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 2 || copies[0].ID != sent.ID || copies[1].ID != inbox.ID || copies[0].FolderID != f.sent {
		t.Fatalf("copies = %+v", copies)
	}
	if copies[0].ThreadID == 0 {
		t.Fatal("copy has no thread")
	}
	none, err := f.db.CopiesOf(context.Background(), f.account, "missing@me")
	if err != nil || len(none) != 0 {
		t.Fatalf("copies of a missing message = %v, %v", none, err)
	}
}
