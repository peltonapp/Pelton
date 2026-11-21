package sync

import (
	"bytes"
	"context"
	"fmt"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/crypto"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/logging"
	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/storage"
)

// imapFlagsToStorage maps the imap flag list to the storage bitmask, keeping
// only the flags Pelton models. unknown server flags (\Answered, \Draft, custom
// keywords) are intentionally ignored here.
func imapFlagsToStorage(flags []imap.Flag) storage.Flag {
	var out storage.Flag
	for _, f := range flags {
		switch f {
		case imap.FlagSeen:
			out |= storage.FlagSeen
		case imap.FlagFlagged:
			out |= storage.FlagFlagged
		case imap.FlagDeleted:
			out |= storage.FlagDeleted
		}
	}
	return out
}

// storageFlagsToImap maps the storage bitmask back to imap flags.
func storageFlagsToImap(f storage.Flag) []imap.Flag {
	var out []imap.Flag
	if f.Has(storage.FlagSeen) {
		out = append(out, imap.FlagSeen)
	}
	if f.Has(storage.FlagFlagged) {
		out = append(out, imap.FlagFlagged)
	}
	if f.Has(storage.FlagDeleted) {
		out = append(out, imap.FlagDeleted)
	}
	return out
}

// fetchAndStore pulls a full message by uid and inserts it with its
// attachments. body and attachments are fetched with BODY.PEEK so caching does
// not set \Seen on the server.
func (e *Engine) fetchAndStore(ctx context.Context, folder storage.Folder, uid uint32) (int64, error) {
	msg, err := e.client.FetchMessage(imap.UID(uid))
	if err != nil {
		return 0, fmt.Errorf("sync: fetch message uid %d: %w", uid, err)
	}
	return e.storeMessage(ctx, folder, msg)
}

// fetchBatch pulls a run of messages in one command and stores each as it
// arrives, returning the ids it stored. A message that cannot be fetched or
// parsed is logged and skipped: the rest of the batch is already on its way
// down the connection and the next sync asks for the skipped one again.
func (e *Engine) fetchBatch(ctx context.Context, folder storage.Folder, uids []uint32) ([]int64, error) {
	ids := make([]int64, 0, len(uids))
	set := make([]imap.UID, 0, len(uids))
	for _, uid := range uids {
		set = append(set, imap.UID(uid))
	}

	err := e.client.FetchMessages(set, func(uid imap.UID, msg *pimap.Message, parseErr error) error {
		if parseErr != nil {
			e.log.Error("parse fetched message failed", "uid", uint32(uid), "err", parseErr)
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		id, err := e.storeMessage(ctx, folder, msg)
		if err != nil {
			e.log.Error("store fetched message failed", "uid", uint32(uid), "err", err)
			return nil
		}
		ids = append(ids, id)
		return nil
	})
	if err != nil {
		// whatever was stored before the failure stays stored; the ids go back so
		// the caller can count and announce them.
		return ids, fmt.Errorf("sync: fetch batch in %q: %w", folder.IMAPPath, err)
	}
	return ids, nil
}

// storeMessage writes one fetched message and its attachments to the cache.
func (e *Engine) storeMessage(ctx context.Context, folder storage.Folder, msg *pimap.Message) (int64, error) {
	uid := uint32(msg.UID)
	stored := &storage.Message{
		AccountID:  folder.AccountID,
		FolderID:   folder.ID,
		UID:        uint32(msg.UID),
		MessageID:  msg.MessageID,
		InReplyTo:  msg.InReplyTo,
		References: msg.References,
		Subject:    msg.Subject,
		// from/to are kept as formatted strings here. splitting name from address
		// is a later refinement, the storage columns already allow it.
		FromAddress: msg.From,
		ToAddresses: msg.To,
		CcAddresses: msg.Cc,
		Date:        msg.Date,
		Flags:       imapFlagsToStorage(msg.Flags),
		BodyPlain:   msg.Text,
		BodyHTML:    msg.HTML,
		SizeBytes:   msg.Size,
		// set only when the message was wrong about its own encoding and the
		// text had to be guessed at, which the reader is told about.
		CharsetGuess: msg.CharsetGuess,

		ListUnsubscribe:     msg.ListUnsubscribe,
		ListUnsubscribePost: msg.ListUnsubscribePost,

		// parsed here for the same reason as the signature below: the headers
		// are gone once the message is stored, so a later check would have to
		// refetch and would report nothing offline.
		ReplyTo: msg.ReplyTo,
		Auth:    storedAuth(msg.AuthResults),

		// verified here because this is the only place the raw bytes exist: they
		// are not cached, so a later check would have to refetch the message and
		// would report nothing at all offline.
		SMIME: verifySignature(msg),
	}

	atts := make([]storage.IncomingAttachment, 0, len(msg.Attachments))
	for _, a := range msg.Attachments {
		atts = append(atts, storage.IncomingAttachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			ContentID:   a.ContentID,
			Content:     bytes.NewReader(a.Content),
		})
	}

	id, err := e.store.InsertMessageWithAttachments(ctx, stored, atts)
	if err != nil {
		return 0, fmt.Errorf("sync: store message uid %d: %w", uid, err)
	}

	// pgp mail keeps its source, since decrypting and verifying need the exact
	// bytes and nothing else caches them. What is kept is the sender's
	// ciphertext, so this stores nothing the message did not already carry. A
	// failure here costs the ability to open that one message, not the sync.
	if crypto.IsProtected(msg.Raw) {
		if err := e.store.SetMessagePGPSource(ctx, id, msg.Raw); err != nil {
			e.log.Error("store pgp source", "uid", uid, "err", err)
		}
	}

	// this is the only point in a sync that holds a message's sender and
	// subject, which is what tracking down "the wrong mail arrived" or "one
	// message never showed up" needs. It writes something about the user's
	// mail to disk, so it is behind its own opt-in and off even when file
	// logging is on. Without it a sync log has uids and counts.
	if logging.MessageMetadata() {
		e.log.Debug("message stored",
			"folder", folder.IMAPPath, "uid", uid, "id", id,
			"from", msg.From, "subject", msg.Subject, "date", msg.Date)
	}
	return id, nil
}

// deleteLocal removes a cached message that the server no longer has, including
// its attachment files.
func (e *Engine) deleteLocal(ctx context.Context, folder storage.Folder, state storage.MessageState) error {
	if err := e.store.DeleteMessage(ctx, state.ID); err != nil {
		return fmt.Errorf("sync: delete local message uid %d: %w", state.UID, err)
	}
	if err := e.store.DeleteAttachmentFilesForMessage(folder.AccountID, state.ID); err != nil {
		return fmt.Errorf("sync: remove attachment files for uid %d: %w", state.UID, err)
	}
	return nil
}

// storedAuth folds the message's Authentication-Results headers into the
// columns. Nothing is inferred: a header that says nothing about a method
// leaves that field empty, which reads as unknown rather than as a failure.
func storedAuth(headers []string) storage.MessageAuth {
	auth := phishing.ParseAuth(headers)
	return storage.MessageAuth{
		SPF:        auth.SPF,
		DKIM:       auth.DKIM,
		DMARC:      auth.DMARC,
		SPFDomain:  auth.SPFDomain,
		DKIMDomain: auth.DKIMDomain,
	}
}

// verifySignature checks a freshly fetched message's s/mime signature. Mail
// that carries none, which is nearly all of it, produces a zero value and costs
// only the header scan that establishes there is nothing to check.
func verifySignature(msg *pimap.Message) storage.SMIMESignature {
	if len(msg.Raw) == 0 {
		return storage.SMIMESignature{}
	}
	sig := crypto.VerifySMIME(msg.Raw, msg.From)
	if sig.Status == crypto.SigNone {
		return storage.SMIMESignature{}
	}
	return storage.SMIMESignature{
		Status:      string(sig.Status),
		Signer:      sig.SignerName,
		Email:       sig.SignerEmail,
		Issuer:      sig.Issuer,
		Detail:      sig.Detail,
		Certs:       storage.EncodeCerts(sig.Certs),
		Fingerprint: crypto.CertFingerprint(sig.Certs),
	}
}

// adoptServerFlags stores the server's flags for a message that changed on the
// server with no pending local change.
func (e *Engine) adoptServerFlags(ctx context.Context, state storage.MessageState, flags storage.Flag) error {
	if err := e.store.SetMessageFlags(ctx, state.ID, flags); err != nil {
		return fmt.Errorf("sync: adopt server flags for uid %d: %w", state.UID, err)
	}
	return nil
}

// compile-time check that the real imap client satisfies the interface sync
// depends on, so the public surface stays sufficient.
var _ mailClient = (*pimap.Client)(nil)
