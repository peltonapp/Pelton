package sync

import (
	"context"
	"strconv"
	"testing"

	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/rfc822"
)

// trustAdapter serves raw for every message it is asked for, parsed the way
// the IMAP adapter parses a fetched body.
type trustAdapter struct {
	fakeAdapter
	raw []byte
}

func (a *trustAdapter) Fetch(_ context.Context, _ string, remoteIDs []string) ([]Fetched, error) {
	parsed, err := rfc822.ParseBody(a.raw)
	if err != nil {
		return nil, err
	}
	out := make([]Fetched, 0, len(remoteIDs))
	for _, id := range remoteIDs {
		a.fetched = append(a.fetched, id)
		uid, _ := strconv.ParseUint(id, 10, 32)
		out = append(out, Fetched{
			RemoteID: id, LegacyUID: uint32(uid), MessageID: "trust@example.com", Subject: "trust",
			From: "PayPal <service@paypal.com>", Text: parsed.Text, Size: parsed.Size, Raw: a.raw,
			ReplyTo: parsed.ReplyTo, AuthResults: parsed.AuthResults, Claims: parsed.Claims,
			InReplyTo: parsed.InReplyTo, References: parsed.References,
		})
	}
	return out, nil
}

func newTrustAdapter(raw string, uids ...uint32) *trustAdapter {
	return &trustAdapter{fakeAdapter: fakeAdapter{ids: fakeIDs(uids...)}, raw: []byte(raw)}
}

// TestSyncStoresTheTrustedRecord: the record is written at sync from the
// provider's own header, and a forged header below it is not read.
func TestSyncStoresTheTrustedRecord(t *testing.T) {
	raw := "Authentication-Results: mx.google.com; dmarc=fail (p=REJECT) header.from=paypal.com; spf=fail smtp.mailfrom=evil.test\r\n" +
		"Return-Path: <bounce@evil.test>\r\n" +
		"Disposition-Notification-To: <track@evil.test>\r\n" +
		"Authentication-Results: mx.google.com; dkim=pass header.d=paypal.com; dmarc=pass header.from=paypal.com\r\n" +
		"From: PayPal <service@paypal.com>\r\n" +
		"Content-Type: text/plain\r\n\r\nhello\r\n"
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	engine := NewEngine(newTrustAdapter(raw, 3), db, nil)
	engine.AuthServ = phishing.ServerFor("me@gmail.com", "imap.gmail.com", "smtp.gmail.com")
	res, err := engine.SyncFolder(ctx, folder)
	if err != nil || len(res.NewIDs) != 1 {
		t.Fatalf("SyncFolder = %+v, %v", res, err)
	}
	got, err := db.GetMessageTrust(ctx, res.NewIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthState != phishing.AuthTrusted || got.DMARC != "fail" || got.DMARCPolicy != "reject" || got.SPF != "fail" {
		t.Errorf("record = %+v, want the provider's failing header", got)
	}
	if got.DKIM != "pass" || len(got.DKIMDomains) != 1 {
		t.Errorf("record = %+v, want the provider's second header to add its signature", got)
	}
	if got.ReturnPath != "bounce@evil.test" || got.ReadReceiptTo != "track@evil.test" {
		t.Errorf("claims = %+v", got)
	}
}

// TestSyncWithoutAuthServTrustsNothing: an engine that does not know the
// account's servers stores what it saw as untrusted rather than guessing.
func TestSyncWithoutAuthServTrustsNothing(t *testing.T) {
	raw := "Authentication-Results: mx.google.com; dmarc=pass header.from=paypal.com\r\nFrom: a@paypal.com\r\n\r\nhi\r\n"
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	res, err := NewEngine(newTrustAdapter(raw, 4), db, nil).SyncFolder(ctx, folder)
	if err != nil || len(res.NewIDs) != 1 {
		t.Fatalf("SyncFolder = %+v, %v", res, err)
	}
	got, err := db.GetMessageTrust(ctx, res.NewIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthState != phishing.AuthUntrusted || got.DMARC != "" {
		t.Errorf("record = %+v, want untrusted and empty", got)
	}
}
