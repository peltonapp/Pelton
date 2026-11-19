package sync

import (
	"context"
	"testing"

	"github.com/emersion/go-imap/v2"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/rfc822"
)

type trustClient struct {
	fakeClient
	raw []byte
}

func (c *trustClient) FetchMessage(uid imap.UID) (*pimap.Message, error) {
	parsed, err := rfc822.ParseBody(c.raw)
	if err != nil {
		return nil, err
	}
	return &pimap.Message{
		UID: uid, MessageID: "trust@example.com", Subject: "trust", From: "PayPal <service@paypal.com>",
		Text: parsed.Text, Size: parsed.Size, Raw: c.raw,
		ReplyTo: parsed.ReplyTo, AuthResults: parsed.AuthResults, Claims: parsed.Claims,
		InReplyTo: parsed.InReplyTo, References: parsed.References,
	}, nil
}

func (c *trustClient) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	for _, uid := range uids {
		msg, err := c.FetchMessage(uid)
		if err := fn(uid, msg, err); err != nil {
			return err
		}
	}
	return nil
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
	engine := NewEngine(&trustClient{fakeClient: fakeClient{uids: []uint32{3}}, raw: []byte(raw)}, db, nil)
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
	res, err := NewEngine(&trustClient{fakeClient: fakeClient{uids: []uint32{4}}, raw: []byte(raw)}, db, nil).SyncFolder(ctx, folder)
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
