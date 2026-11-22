package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func (f *vaultFixture) listStub(t *testing.T, folder int64, uid uint32, suffix string) int64 {
	t.Helper()
	id, err := f.db.UpsertMessageListMeta(context.Background(), &Message{
		AccountID: f.account, FolderID: folder, UID: uid,
		MessageID: "<" + suffix + markMsgID + ">", Subject: markSubject + suffix,
		FromAddress: markFrom, FromName: "Marker Name", ToAddresses: "me@example.com",
		Date: time.Date(2026, 11, 1, 10, 0, int(uid), 0, time.UTC), BodyPlain: markBody + suffix, SizeBytes: 1234,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *vaultFixture) fill(ctx context.Context, id int64, folder int64, uid uint32, suffix string) error {
	m := &Message{
		AccountID: f.account, FolderID: folder, UID: uid,
		MessageID: "<" + suffix + markMsgID + ">", Subject: markSubject + suffix,
		FromAddress: markFrom, FromName: "Marker Name", ToAddresses: "me@example.com",
		Date: time.Date(2026, 11, 1, 10, 0, int(uid), 0, time.UTC), BodyPlain: markBody + suffix + " full",
		BodyHTML: "<p>" + markBody + "</p>", SizeBytes: 1234, PGPSource: []byte(markSource),
	}
	_, err := f.db.FillMessageBody(ctx, BodyTarget{MessageID: id}, m, []IncomingAttachment{
		{Filename: markFile, ContentType: "application/pdf", Content: strings.NewReader(markContent + suffix)},
	})
	return err
}

// checkSealedRow fails when the row of id keeps any content in clear.
func (f *vaultFixture) checkSealedRow(t *testing.T, id int64, wantComplete bool) {
	t.Helper()
	var subject, from, plain, html, mid string
	var vaultID int64
	var complete bool
	var sealed int
	if err := f.db.sql.QueryRow(`SELECT subject, from_address, body_plain, body_html, message_id, vault_id, body_complete,
		(SELECT count(*) FROM mail_vault_sealed WHERE message_id = messages.id) FROM messages WHERE id = ?`, id).
		Scan(&subject, &from, &plain, &html, &mid, &vaultID, &complete, &sealed); err != nil {
		t.Fatal(err)
	}
	if subject != "" || from != "" || plain != "" || html != "" || strings.Contains(mid, "markermsgid") {
		t.Fatalf("row %d keeps content in clear: %q %q %q %q %q", id, subject, from, plain, html, mid)
	}
	if vaultID != f.vault || sealed != 1 || complete != wantComplete {
		t.Fatalf("row %d: vault %d sealed rows %d complete %v, want vault %d, one sealed row, complete %v",
			id, vaultID, sealed, complete, f.vault, wantComplete)
	}
	if n := upInt(t, f.db.sql, `SELECT count(*) FROM message_pgp WHERE message_id = ?`, id); n != 0 {
		t.Fatalf("row %d has its pgp source in clear", id)
	}
}

// A stub listed into a vaulted folder is sealed, a refresh keeps it so, and
// the body fill seals the whole message: nothing of it is ever in clear.
func TestVaultStubAndFillStaySealed(t *testing.T) {
	ctx := context.Background()
	f := newVaultFixture(t)
	f.makeVault(t)

	id := f.listStub(t, f.secret, 1, "a")
	f.checkSealedRow(t, id, false)
	if _, err := f.db.GetMessage(userCtx(), id); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("locked vault stub readable: %v", err)
	}
	if again := f.listStub(t, f.secret, 1, "a"); again != id {
		t.Fatalf("refresh made row %d, want %d", again, id)
	}
	f.checkSealedRow(t, id, false)

	f.gate.set(true)
	stub, err := f.db.GetMessage(userCtx(), id)
	if err != nil || stub.Subject != markSubject+"a" || stub.BodyPlain != markBody+"a" || stub.BodyComplete {
		t.Fatalf("opened stub = %+v %v", stub, err)
	}
	f.gate.set(false)

	if err := f.fill(ctx, id, f.secret, 1, "a"); err != nil {
		t.Fatalf("fill while locked: %v", err)
	}
	f.checkSealedRow(t, id, true)
	if msgs, err := f.db.ListMessagesForReindex(ctx, 0, id, 10); err != nil || len(msgs) != 0 {
		t.Fatalf("vault mail offered to the search index: %d rows, %v", len(msgs), err)
	}

	f.gate.set(true)
	got, err := f.db.GetMessage(userCtx(), id)
	if err != nil || got.BodyPlain != markBody+"a full" || got.FromAddress != markFrom || !got.BodyComplete {
		t.Fatalf("opened filled message = %+v %v", got, err)
	}
	atts, err := f.db.ListAttachments(userCtx(), id)
	if err != nil || len(atts) != 1 || atts[0].Filename != markFile {
		t.Fatalf("attachments = %+v %v", atts, err)
	}
	rc, err := f.db.OpenAttachment(userCtx(), atts[0])
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(rc)
	rc.Close()
	if string(content) != markContent+"a" {
		t.Fatalf("attachment bytes = %q", content)
	}
	if src, err := f.db.MessagePGPSource(userCtx(), id); err != nil || string(src) != markSource {
		t.Fatalf("pgp source = %q %v", src, err)
	}
	f.gate.set(false)

	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, hit := range findMarkers(t, f.dir) {
		t.Errorf("in clear on disk: %s", hit)
	}
}

// The folder joins a vault, and the sealing job seals and wipes the stub,
// while its body is in flight: the fill lands sealed, never in clear.
func TestVaultSealWhileFilling(t *testing.T) {
	ctx := context.Background()
	f := newVaultFixture(t)
	id := f.listStub(t, f.secret, 1, "a")

	f.db.bodyFillLookupHook = func() {
		f.db.bodyFillLookupHook = nil
		f.makeVault(t)
		for {
			n, err := f.db.SealPending(ctx, f.vault, 10)
			if err != nil {
				t.Errorf("seal during the fill: %v", err)
				return
			}
			if n == 0 {
				return
			}
		}
	}
	if err := f.fill(ctx, id, f.secret, 1, "a"); err != nil {
		t.Fatalf("fill: %v", err)
	}
	f.checkSealedRow(t, id, true)

	f.gate.set(true)
	got, err := f.db.GetMessage(userCtx(), id)
	if err != nil || got.BodyPlain != markBody+"a full" || got.Subject != markSubject+"a" {
		t.Fatalf("opened message = %+v %v", got, err)
	}
	f.gate.set(false)

	if _, err := f.db.ScrubAfterSealing(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, hit := range findMarkers(t, f.dir) {
		t.Errorf("in clear on disk after the race: %s", hit)
	}
}

// A fill that commits before the folder joins the vault is plain mail the
// sealing job then seals; nothing stays behind in clear either way.
func TestVaultSealAfterFill(t *testing.T) {
	ctx := context.Background()
	f := newVaultFixture(t)
	id := f.listStub(t, f.secret, 1, "a")
	if err := f.fill(ctx, id, f.secret, 1, "a"); err != nil {
		t.Fatal(err)
	}
	f.makeVault(t)
	for {
		n, err := f.db.SealPending(ctx, f.vault, 10)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	f.checkSealedRow(t, id, true)
	if _, err := f.db.ScrubAfterSealing(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, hit := range findMarkers(t, f.dir) {
		t.Errorf("in clear on disk: %s", hit)
	}
}
