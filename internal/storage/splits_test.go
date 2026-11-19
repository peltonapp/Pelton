package storage

import (
	"context"
	"testing"
)

func TestMailKindFor(t *testing.T) {
	tests := []struct {
		name string
		m    Message
		want MailKind
	}{
		{"person", Message{FromAddress: "Ada <ada@example.com>"}, 0},
		{"unsubscribe header", Message{FromAddress: "news@shop.test", ListUnsubscribe: "<mailto:u@shop.test>"}, MailKindList},
		{"list id", Message{FromAddress: "dev@lists.test", ListID: "<dev.lists.test>"}, MailKindList},
		{"noreply", Message{FromAddress: "Shop <noreply@shop.test>"}, MailKindAutomated},
		{"no-reply bare", Message{FromAddress: "no-reply@bank.test"}, MailKindAutomated},
		{"notifications", Message{FromAddress: "notifications@github.test"}, MailKindAutomated},
		{"mailer daemon", Message{FromAddress: "MAILER-DAEMON@mx.test"}, MailKindAutomated},
		{"postmaster", Message{FromAddress: "postmaster@mx.test"}, MailKindAutomated},
		{"name mentioning noreply", Message{FromAddress: "noreply fan <ada@example.com>"}, 0},
		{"both", Message{FromAddress: "noreply@news.test", ListUnsubscribe: "<https://x>"}, MailKindList | MailKindAutomated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mailKindFor(&tt.m); got != tt.want {
				t.Errorf("mailKindFor = %d, want %d", got, tt.want)
			}
		})
	}
}

// splitFixture holds an inbox with one message of each kind.
type splitFixture struct {
	*threadFixture
	vip, contact, news, notify, other, newsNoreply Message
}

func newSplitFixture(t *testing.T) *splitFixture {
	t.Helper()
	f := &splitFixture{threadFixture: newThreadFixture(t)}
	ctx := context.Background()
	f.vip = f.add(Message{MessageID: "<vip@x>", Subject: "lunch", FromAddress: "Boss <boss@work.test>"}, 1)
	f.contact = f.add(Message{MessageID: "<c@x>", Subject: "hello", FromAddress: "friend@home.test", Flags: FlagSeen}, 2)
	f.news = f.add(Message{MessageID: "<n@x>", Subject: "weekly", FromAddress: "news@shop.test", ListUnsubscribe: "<mailto:u@shop.test>"}, 3)
	f.notify = f.add(Message{MessageID: "<a@x>", Subject: "build failed", FromAddress: "notifications@ci.test"}, 4)
	f.other = f.add(Message{MessageID: "<o@x>", Subject: "invoice", FromAddress: "billing@vendor.test"}, 5)
	f.newsNoreply = f.add(Message{MessageID: "<nn@x>", Subject: "digest", FromAddress: "noreply@digest.test", ListID: "<digest>"}, 6)

	book, err := f.db.CreateAddressBook(ctx, &AddressBook{Name: "book", URL: "https://dav.test"})
	if err != nil {
		t.Fatal(err)
	}
	contact := &Contact{BookID: book, Path: "/c.vcf", FullName: "Friend", Emails: []ContactValue{{Value: "Friend@Home.test"}}}
	if err := f.db.SaveContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *splitFixture) ids(q MessageQuery) []int64 {
	f.t.Helper()
	msgs, err := f.db.QueryMessages(context.Background(), q)
	if err != nil {
		f.t.Fatal(err)
	}
	out := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

func sameIDs(t *testing.T, label string, got []int64, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: got %v, want %v", label, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: got %v, want %v", label, got, want)
			return
		}
	}
}

func TestSplitClassifiesFirstMatchInTabOrder(t *testing.T) {
	f := newSplitFixture(t)
	important := SplitRule{Kind: SplitImportant, Senders: []string{"boss@work.test"}}
	news := SplitRule{Kind: SplitNewsletters}
	notify := SplitRule{Kind: SplitNotifications}

	rules := []SplitRule{important, news, notify}
	tab := func(i int) MessageQuery {
		return MessageQuery{FolderIDs: []int64{f.inbox}, Split: &SplitFilter{Rules: rules, Index: i}}
	}
	sameIDs(t, "important", f.ids(tab(0)), f.contact.ID, f.vip.ID)
	sameIDs(t, "newsletters", f.ids(tab(1)), f.newsNoreply.ID, f.news.ID)
	sameIDs(t, "notifications", f.ids(tab(2)), f.notify.ID)
	sameIDs(t, "other", f.ids(tab(3)), f.other.ID)

	// with notifications ahead of newsletters, the noreply digest moves over.
	rules = []SplitRule{important, notify, news}
	sameIDs(t, "notifications first", f.ids(tab(1)), f.newsNoreply.ID, f.notify.ID)
	sameIDs(t, "newsletters second", f.ids(tab(2)), f.news.ID)
}

func TestSplitCountsOneGroupedQuery(t *testing.T) {
	f := newSplitFixture(t)
	rules := []SplitRule{
		{Kind: SplitImportant, Senders: []string{"boss@work.test"}},
		{Kind: SplitNewsletters},
		{Kind: SplitNotifications},
	}
	counts, err := f.db.SplitCounts(context.Background(), MessageQuery{FolderIDs: []int64{f.inbox}}, rules)
	if err != nil {
		t.Fatal(err)
	}
	want := []SplitCount{{Total: 2, Unread: 1}, {Total: 2, Unread: 2}, {Total: 1, Unread: 1}, {Total: 1, Unread: 1}}
	if len(counts) != len(want) {
		t.Fatalf("counts = %v, want %v", counts, want)
	}
	for i := range want {
		if counts[i] != want[i] {
			t.Errorf("tab %d = %+v, want %+v", i, counts[i], want[i])
		}
	}

	// muted mail leaves the counts along with the list.
	if err := f.db.SetThreadMuted(context.Background(), f.notify.ThreadID, true); err != nil {
		t.Fatal(err)
	}
	counts, err = f.db.SplitCounts(context.Background(), MessageQuery{FolderIDs: []int64{f.inbox}, HideMuted: true}, rules)
	if err != nil {
		t.Fatal(err)
	}
	if counts[2] != (SplitCount{}) {
		t.Errorf("muted notifications tab = %+v, want empty", counts[2])
	}
}

func TestSplitSavedViewRule(t *testing.T) {
	f := newSplitFixture(t)
	tests := []struct {
		name string
		view View
		want []int64
	}{
		{"subject text", View{QuerySubject: "INVOICE"}, []int64{f.other.ID}},
		{"from list", View{QueryFrom: "shop.test\nci.test"}, []int64{f.notify.ID, f.news.ID}},
		{"regex", View{QueryText: `^(weekly|digest)$`, UseRegex: true}, []int64{f.newsNoreply.ID, f.news.ID}},
		{"unread with attachment", View{UnreadOnly: true, HasAttachment: true}, nil},
		{"literal percent", View{QueryText: "100%"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := []SplitRule{{Kind: SplitView, View: tt.view}}
			got := f.ids(MessageQuery{FolderIDs: []int64{f.inbox}, Split: &SplitFilter{Rules: rules, Index: 0}})
			sameIDs(t, tt.name, got, tt.want...)
		})
	}
}
