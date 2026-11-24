package storage

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func (f browserFixture) mail(t *testing.T, folder int64, subject, from, to, cc string, date time.Time) int64 {
	t.Helper()
	fixtureUID++
	id, err := f.db.InsertMessage(context.Background(), &Message{
		AccountID: f.account, FolderID: folder, UID: fixtureUID, MessageID: subject + "@x",
		Subject: subject, FromAddress: from, ToAddresses: to, CcAddresses: cc, Date: date,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func subjects(list []Message) []string {
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, m.Subject)
	}
	return out
}

func TestMessagesInvolvingMatchesSenderAndRecipients(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	f.mail(t, f.inbox, "from ada", "Ada Lovelace <ada@x.example>", "me@x.example", "", day.Add(1*time.Hour))
	f.mail(t, f.inbox, "to bob", "me@x.example", "Bob <bob@x.example>, cy@x.example", "", day.Add(2*time.Hour))
	f.mail(t, f.inbox, "cc bob", "dee@y.example", "me@x.example", "BOB@x.example", day.Add(3*time.Hour))
	f.mail(t, f.inbox, "unrelated", "dee@y.example", "me@x.example", "", day.Add(4*time.Hour))
	f.mail(t, f.inbox, "lookalike", "dee@y.example", "bigbob@x.example", "", day.Add(5*time.Hour))
	f.mail(t, f.inbox, "too old", "ada@x.example", "me@x.example", "", day.AddDate(0, -3, 0))
	f.mail(t, f.trash, "in trash", "ada@x.example", "me@x.example", "", day.Add(6*time.Hour))

	got, err := f.db.MessagesInvolving(ctx, []string{"ada@x.example", "Bob@x.example"}, []int64{f.inbox, f.archive}, day.AddDate(0, -1, 0), 50)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cc bob", "to bob", "from ada"}
	if !reflect.DeepEqual(subjects(got), want) {
		t.Errorf("messages = %v, want %v (newest first, no look-alike address, nothing old or in trash)", subjects(got), want)
	}
}

func TestMessagesInvolvingLimitAndEmptyInputs(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		f.mail(t, f.inbox, string(rune('a'+i)), "ada@x.example", "me@x.example", "", day.Add(time.Duration(i)*time.Hour))
	}
	got, err := f.db.MessagesInvolving(ctx, []string{"ada@x.example"}, []int64{f.inbox}, day.AddDate(0, -1, 0), 3)
	if err != nil || len(got) != 3 {
		t.Fatalf("limit: %v, %v", subjects(got), err)
	}
	if got, _ := f.db.MessagesInvolving(ctx, nil, []int64{f.inbox}, day, 10); len(got) != 0 {
		t.Errorf("no addresses matched %v", subjects(got))
	}
	if got, _ := f.db.MessagesInvolving(ctx, []string{"ada@x.example"}, nil, day, 10); len(got) != 0 {
		t.Errorf("no folders matched %v", subjects(got))
	}
}

func TestMessagesWithSubject(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	f.mail(t, f.inbox, "Roadmap review notes", "a@x.example", "me@x.example", "", day)
	f.mail(t, f.inbox, "Re: ROADMAP REVIEW", "b@x.example", "me@x.example", "", day.Add(time.Hour))
	f.mail(t, f.inbox, "Lunch", "c@x.example", "me@x.example", "", day.Add(2*time.Hour))
	f.mail(t, f.inbox, "100% roadmap", "c@x.example", "me@x.example", "", day.Add(3*time.Hour))

	got, err := f.db.MessagesWithSubject(ctx, "roadmap review", []int64{f.inbox}, day.AddDate(0, -1, 0), 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Re: ROADMAP REVIEW", "Roadmap review notes"}; !reflect.DeepEqual(subjects(got), want) {
		t.Errorf("subjects = %v, want %v", subjects(got), want)
	}
	if got, _ := f.db.MessagesWithSubject(ctx, "100%", []int64{f.inbox}, day.AddDate(0, -1, 0), 10); len(got) != 1 {
		t.Errorf("a percent sign must match literally, got %v", subjects(got))
	}
	if got, _ := f.db.MessagesWithSubject(ctx, "  ", []int64{f.inbox}, day, 10); got != nil {
		t.Errorf("empty text matched %v", subjects(got))
	}
}

func TestAttachmentsOfMessagesFollowTheBrowsersRules(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	older := f.add(t, f.inbox, "ada@x.example", "m1", day, map[string]int{"plan.pdf": 5000})
	newer := f.add(t, f.inbox, "ada@x.example", "m2", day.Add(time.Hour), map[string]int{"budget.xlsx": 4000, "pixel.png": 100, "cid:logo.png": 5000})
	f.add(t, f.inbox, "ada@x.example", "m3", day.Add(2*time.Hour), map[string]int{"elsewhere.pdf": 5000})

	got, err := f.db.AttachmentsOfMessages(ctx, []int64{older, newer})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"budget.xlsx", "plan.pdf"}; !reflect.DeepEqual(names(got), want) {
		t.Errorf("attachments = %v, want %v (newest message first, no tiny or inline images, no other message)", names(got), want)
	}
	if got[0].Kind != KindSpreadsheet || got[0].Subject == "" {
		t.Errorf("entry = %+v", got[0])
	}
	if got, _ := f.db.AttachmentsOfMessages(ctx, nil); got != nil {
		t.Errorf("no messages gave %v", names(got))
	}
}

func TestAddressList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Ada <ada@x.example>, BOB@x.example", []string{"ada@x.example", "bob@x.example"}},
		{`"Doe, Jane" <jane@x.example>`, []string{"jane@x.example"}},
		{"undisclosed-recipients:;", nil},
		{"broken <ada@x.example, bob@x.example", []string{"ada@x.example", "bob@x.example"}},
	}
	for _, tt := range tests {
		if got := AddressList(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("AddressList(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
