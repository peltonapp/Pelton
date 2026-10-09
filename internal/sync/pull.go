package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/logging"
	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/storage"
)

// fetchBatch pulls a run of messages through the adapter and stores each,
// returning the ids it stored. Fetch may return both messages and an error;
// every returned message is stored before the error is recorded.
func (e *Engine) fetchBatch(ctx context.Context, folder storage.Folder, remoteIDs []string) ([]int64, error) {
	fetched, fetchErr := e.adapter.Fetch(ctx, folder.RemoteID, remoteIDs)
	ids := make([]int64, 0, len(fetched))
	for _, msg := range fetched {
		if err := ctx.Err(); err != nil {
			return ids, err
		}
		id, err := e.storeMessage(ctx, folder, msg)
		if err != nil {
			e.log.Error("store fetched message failed", "remote_id", msg.RemoteID, "err", err)
			if fetchErr == nil {
				fetchErr = err
			}
			continue
		}
		if id == 0 {
			// Already stored. Not a new row and not a batch failure.
			continue
		}
		ids = append(ids, id)
	}
	if fetchErr != nil {
		return ids, fmt.Errorf("sync: fetch batch in %q: %w", folder.IMAPPath, fetchErr)
	}
	return ids, nil
}

// storeMessage writes one fetched message and its attachments to the cache.
func (e *Engine) storeMessage(ctx context.Context, folder storage.Folder, msg Fetched) (int64, error) {
	return StoreFetched(ctx, e.store, e.log, folder, msg, true)
}

// StoreFetched writes one fetched message to the cache the way sync does: a
// stub row is filled in place, a row that already has its body is left alone
// and 0 is returned, and the raw source of PGP mail is kept for decryption.
// withAttachments false stores the message without attachment files.
// LegacyUID is copied from the adapter as-is; a UID is never inferred by
// parsing RemoteID.
func StoreFetched(ctx context.Context, store *storage.DB, log *slog.Logger, folder storage.Folder, msg Fetched, withAttachments bool) (int64, error) {
	stored := &storage.Message{
		AccountID:    folder.AccountID,
		FolderID:     folder.ID,
		UID:          msg.LegacyUID,
		RemoteID:     msg.RemoteID,
		MessageID:    msg.MessageID,
		Subject:      msg.Subject,
		FromAddress:  msg.From,
		FromName:     firstAddressName(msg.From),
		ToAddresses:  msg.To,
		CcAddresses:  msg.Cc,
		Date:         msg.Date,
		Flags:        msg.Flags,
		BodyPlain:    msg.Text,
		BodyHTML:     msg.HTML,
		SizeBytes:    msg.Size,
		CharsetGuess: msg.CharsetGuess,

		ListUnsubscribe:     msg.ListUnsubscribe,
		ListUnsubscribePost: msg.ListUnsubscribePost,

		ReplyTo: msg.ReplyTo,
		Auth:    storedAuth(msg.AuthResults),

		SMIME: verifySignature(msg.Raw, msg.From),
	}

	var atts []storage.IncomingAttachment
	if withAttachments {
		atts = make([]storage.IncomingAttachment, 0, len(msg.Attachments))
		for _, a := range msg.Attachments {
			atts = append(atts, storage.IncomingAttachment{
				Filename:    a.Filename,
				ContentType: a.ContentType,
				ContentID:   a.ContentID,
				Content:     bytes.NewReader(a.Content),
			})
		}
	}

	id, err := store.InsertMessageWithAttachments(ctx, stored, atts)
	if errors.Is(err, storage.ErrMessageBodyComplete) {
		// A retry can list an id whose body landed on an earlier attempt.
		// Skipping it lets the rest of the batch continue.
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("sync: store message %q: %w", msg.RemoteID, err)
	}

	if crypto.IsProtected(msg.Raw) {
		if err := store.SetMessagePGPSource(ctx, id, msg.Raw); err != nil {
			log.Error("store pgp source", "remote_id", msg.RemoteID, "err", err)
		}
	}

	if logging.MessageMetadata() {
		log.Debug("message stored",
			"folder", folder.IMAPPath, "remote_id", msg.RemoteID, "id", id,
			"from", msg.From, "subject", msg.Subject, "date", msg.Date)
	}
	return id, nil
}

// firstAddressName returns the display name of the first address in a stored
// From list, or "" when the list has no name or cannot be parsed.
func firstAddressName(from string) string {
	addrs, err := mail.ParseAddressList(from)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0].Name
}

// deleteLocal removes a cached message that the server no longer has, including
// its attachment files via staged cache deletion.
func (e *Engine) deleteLocal(ctx context.Context, folder storage.Folder, state storage.MessageState) error {
	if err := e.store.DeleteCachedMessage(ctx, folder.AccountID, state.ID); err != nil {
		return fmt.Errorf("sync: delete local message %q: %w", state.RemoteID, err)
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
func verifySignature(raw []byte, from string) storage.SMIMESignature {
	if len(raw) == 0 {
		return storage.SMIMESignature{}
	}
	sig := crypto.VerifySMIME(raw, from)
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
// server with no pending local change. The plan was built from a snapshot, so
// the row may have gained a pending change since; the store then keeps it and
// this reports false, leaving the change for the next push.
func (e *Engine) adoptServerFlags(ctx context.Context, state storage.MessageState, flags storage.Flag) (bool, error) {
	applied, err := e.store.AdoptServerFlags(ctx, state.ID, flags)
	if err != nil {
		return false, fmt.Errorf("sync: adopt server flags for %q: %w", state.RemoteID, err)
	}
	return applied, nil
}
