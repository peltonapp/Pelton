package storage

import (
	"context"
	"testing"
	"time"
)

func TestPrivacyCounters(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	scans := []PrivacyScan{
		{Key: "1:a@x", Sender: "News@Shop.example", SenderName: "Shop", Date: day, Pixels: 2, Links: 5, Companies: []string{"Mailchimp"}},
		{Key: "1:b@x", Sender: "news@shop.example", Date: day.AddDate(0, 0, 20), Pixels: 1, Receipt: true, Companies: []string{"Mailchimp", "Google"}},
		{Key: "1:c@x", Sender: "anna@example.com", Date: day.AddDate(0, 0, 25)},
	}
	for _, s := range scans {
		if err := db.RecordPrivacyScan(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	again := scans[0]
	again.Pixels = 99
	if err := db.RecordPrivacyScan(ctx, again); err != nil {
		t.Fatal(err)
	}

	all, err := db.PrivacySenders(ctx, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	bySender := map[string]PrivacySender{}
	for _, s := range all {
		bySender[s.Sender] = s
	}
	shop := bySender["news@shop.example"]
	if shop.Mails != 2 || shop.Pixels != 3 || shop.Links != 5 || shop.Receipts != 1 || shop.Name != "Shop" {
		t.Errorf("shop = %+v, want two mails counted once each", shop)
	}
	if len(shop.Companies) != 2 || !shop.First.Equal(day) {
		t.Errorf("shop companies = %v, first = %v", shop.Companies, shop.First)
	}
	if bySender["anna@example.com"].Mails != 1 {
		t.Errorf("a mail without trackers still counts as received: %+v", bySender["anna@example.com"])
	}

	recent, err := db.PrivacySenders(ctx, day.AddDate(0, 0, 10))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range recent {
		if s.Sender == "news@shop.example" && (s.Mails != 1 || s.Pixels != 1) {
			t.Errorf("period filter = %+v, want only the later mail", s)
		}
	}

	done, err := db.PrivacyScanned(ctx, []string{"1:a@x", "1:z@x"})
	if err != nil || !done["1:a@x"] || done["1:z@x"] {
		t.Errorf("PrivacyScanned = %v, %v", done, err)
	}
	if err := db.ClearPrivacyScans(ctx); err != nil {
		t.Fatal(err)
	}
	if left, _ := db.PrivacySenders(ctx, time.Time{}); len(left) != 0 {
		t.Errorf("after clearing = %+v", left)
	}
	if key := PrivacyKey(Message{ID: 7, AccountID: 2, MessageID: "<Abc@X>"}); key != "2:abc@x" {
		t.Errorf("PrivacyKey = %q", key)
	}
	if key := PrivacyKey(Message{ID: 7}); key != "row:7" {
		t.Errorf("PrivacyKey without a Message-ID = %q", key)
	}
}

func TestUnsubscribeRecords(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	if err := db.RecordUnsubscribe(ctx, Unsubscribe{Sender: "News@Shop.example", ListID: "<list.shop.example>", AccountID: 1, Method: "oneclick", At: at}); err != nil {
		t.Fatal(err)
	}
	u, ok, err := db.UnsubscribeFor(ctx, "promo@shop.example", "<list.shop.example>")
	if err != nil || !ok || u.Sender != "news@shop.example" || !u.At.Equal(at) {
		t.Fatalf("by list id = %+v, %t, %v", u, ok, err)
	}
	if _, ok, _ := db.UnsubscribeFor(ctx, "someone@else.example", ""); ok {
		t.Error("an unrelated sender matched")
	}

	later := at.AddDate(0, 0, 12)
	for range 2 {
		if err := db.NoteStillSending(ctx, "news@shop.example", later, 42); err != nil {
			t.Fatal(err)
		}
	}
	if u, _, _ = db.UnsubscribeFor(ctx, "news@shop.example", ""); u.StillCount != 1 || u.StillMessageID != 42 {
		t.Errorf("still sending = %+v, want the same mail counted once", u)
	}
	if err := db.DismissStillSending(ctx, "news@shop.example"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUnsubscribeBlock(ctx, "news@shop.example", "junk"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUnsubscribeBlock(ctx, "never@example.com", "junk"); err == nil {
		t.Error("blocking a sender never unsubscribed from was accepted")
	}
	list, err := db.ListUnsubscribes(ctx)
	if err != nil || len(list) != 1 || list[0].Blocked != "junk" || list[0].DismissedAt.IsZero() {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if err := db.RecordUnsubscribe(ctx, Unsubscribe{Sender: "news@shop.example", Method: "mailto", At: later}); err != nil {
		t.Fatal(err)
	}
	if u, _, _ = db.UnsubscribeFor(ctx, "news@shop.example", ""); u.StillCount != 0 || !u.DismissedAt.IsZero() || u.Blocked != "junk" {
		t.Errorf("after a retry = %+v, want the watch restarted and the block kept", u)
	}
}
