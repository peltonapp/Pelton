package desktop

import (
	"fmt"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestInsightsOnEmptyMailbox(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	newStatsAccount(t, db, ctx)

	st, err := a.InsightsStorage()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Accounts) != 1 || st.Accounts[0].Count != 0 || len(st.Accounts[0].Folders) != 0 || len(st.Senders) != 0 || len(st.Attachments) != 0 {
		t.Errorf("storage = %+v", st)
	}
	vol, err := a.InsightsVolume(30)
	if err != nil {
		t.Fatal(err)
	}
	if len(vol.Days) != 0 || len(vol.Hours) != 0 || !vol.HasSent || vol.Since == "" {
		t.Errorf("volume = %+v", vol)
	}
	people, err := a.InsightsPeople(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(people.WritesTo) != 0 || len(people.WritesYou) != 0 || people.MyReplies.Available || people.TheirReplies.Available {
		t.Errorf("people = %+v", people)
	}
}

func TestInsightsSections(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	acc := newStatsAccount(t, db, ctx)
	now := time.Now()
	acc.add(t, db, ctx, storage.Message{FromAddress: "Ada <ada@example.com>", SizeBytes: 500, Date: now.Add(-time.Hour)})
	acc.add(t, db, ctx, storage.Message{FolderID: acc.trash.ID, FromAddress: "ada@example.com", SizeBytes: 300, Date: now.Add(-time.Hour)})
	// the Gmail copy takes space but is not a second message.
	acc.add(t, db, ctx, storage.Message{FolderID: acc.allMail.ID, FromAddress: "ada@example.com", SizeBytes: 500, Date: now.Add(-time.Hour)})
	acc.add(t, db, ctx, storage.Message{FolderID: acc.sent.ID, FromAddress: "me@example.com", ToAddresses: "Ada <ada@example.com>, me@example.com", SizeBytes: 50, Date: now.Add(-30 * time.Minute)})

	st, err := a.InsightsStorage()
	if err != nil {
		t.Fatal(err)
	}
	if st.Accounts[0].Count != 4 || st.Accounts[0].Bytes != 1350 || len(st.Accounts[0].Folders) != 4 {
		t.Errorf("account usage = %+v", st.Accounts[0])
	}
	if st.Accounts[0].Folders[0].Bytes != 500 {
		t.Errorf("folders are not largest first: %+v", st.Accounts[0].Folders)
	}
	if len(st.Senders) == 0 || st.Senders[0].Email != "ada@example.com" || st.Senders[0].Name != "Ada" || st.Senders[0].Bytes != 1300 {
		t.Errorf("senders = %+v", st.Senders)
	}

	vol, err := a.InsightsVolume(30)
	if err != nil {
		t.Fatal(err)
	}
	received, sent := 0, 0
	for _, d := range vol.Days {
		received += d.Received
		sent += d.Sent
	}
	if received != 1 || sent != 1 {
		t.Errorf("volume received %d sent %d, want 1 and 1: %+v", received, sent, vol.Days)
	}

	people, err := a.InsightsPeople(30)
	if err != nil {
		t.Fatal(err)
	}
	if len(people.WritesYou) != 1 || people.WritesYou[0].Email != "ada@example.com" || people.WritesYou[0].Count != 1 {
		t.Errorf("writes you = %+v", people.WritesYou)
	}
	if len(people.WritesTo) != 1 || people.WritesTo[0].Email != "ada@example.com" || people.WritesTo[0].Name != "Ada" {
		t.Errorf("writes to = %+v", people.WritesTo)
	}
	// one reply is no median.
	if people.MyReplies.Available || people.TheirReplies.Available {
		t.Errorf("replies shown on too little data: %+v / %+v", people.MyReplies, people.TheirReplies)
	}
}

func TestTopRecipients(t *testing.T) {
	groups := []storage.RecipientGroup{
		{To: `"Doe, Jane" <jane@example.com>, bob@example.com`, Cc: "me@example.com", Count: 3},
		{To: "Bob <BOB@example.com>", Count: 2},
		{To: "broken <<cy@example.com", Count: 1},
	}
	got := topRecipients(groups, []string{"me@example.com"}, 10)
	want := "[{bob@example.com Bob 5 0} {jane@example.com Doe, Jane 3 0} {cy@example.com  1 0}]"
	if fmt.Sprint(got) != want {
		t.Errorf("top recipients = %v, want %v", got, want)
	}
}

func TestReplyDTOHidesSmallSamples(t *testing.T) {
	if d := replyDTO(storage.ReplyMedian{N: 4, Median: time.Hour}); d.Available || d.MedianSeconds != 0 {
		t.Errorf("four replies = %+v", d)
	}
	if d := replyDTO(storage.ReplyMedian{N: 5, Median: time.Hour}); !d.Available || d.MedianSeconds != 3600 {
		t.Errorf("five replies = %+v", d)
	}
}
