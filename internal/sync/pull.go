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
// every returned message is stored before the error is recorded. Only ids in
// insertable may become new rows; any other body fills its existing stub or
// is dropped when the stub is gone. Each body is pinned to the folder's
// generation and to the row its id had when the batch started: a local reset
// while the FETCH was in flight drops the body (see storeFetched).
func (e *Engine) fetchBatch(ctx context.Context, folder storage.Folder, remoteIDs []string, insertable map[string]struct{}) ([]int64, error) {
	rows, err := e.store.MessageIDsByRemoteID(ctx, folder.ID, remoteIDs)
	if err != nil {
		return nil, fmt.Errorf("sync: fetch batch in %q: %w", folder.IMAPPath, err)
	}
	// The ids are from the folder's stored generation; a changed UIDVALIDITY
	// fails the batch with ErrGenerationChanged, which stops the campaign.
	fetched, fetchErr := e.adapter.Fetch(folderContext(ctx, folder), folder.RemoteID, remoteIDs)
	var storeErr error
	ids := make([]int64, 0, len(fetched))
	for _, msg := range fetched {
		if err := ctx.Err(); err != nil {
			return ids, err
		}
		_, insert := insertable[msg.RemoteID]
		id, err := e.storeMessage(ctx, folder, msg, insert, rows[msg.RemoteID])
		if err != nil {
			e.log.Error("store fetched message failed", "remote_id", msg.RemoteID, "err", err)
			if storeErr == nil {
				storeErr = err
			}
			continue
		}
		if id == 0 {
			// Already stored, or the stub is gone. Not a new row and not a
			// batch failure.
			continue
		}
		ids = append(ids, id)
	}
	// A failed store outranks unreadable messages: it is not about one
	// message, so the campaign must stop.
	err = fetchErr
	if storeErr != nil && (err == nil || isMessageError(err)) {
		err = storeErr
	}
	if err != nil {
		return ids, fmt.Errorf("sync: fetch batch in %q: %w", folder.IMAPPath, err)
	}
	return ids, nil
}

// isMessageError reports whether err concerns single messages only (see
// MessageError), so the body fetch can go on.
func isMessageError(err error) bool {
	var me *MessageError
	return errors.As(err, &me)
}

// storeMessage writes one fetched message and its attachments (none with
// WithoutAttachments) to the cache. insert false only fills an existing stub.
// The body was fetched under folder's generation for row rowID (0: whichever
// row has its id). When the folder was reset or the id names another row
// since, the body is dropped: 0 and no error, like a stub that is gone.
func (e *Engine) storeMessage(ctx context.Context, folder storage.Folder, msg Fetched, insert bool, rowID int64) (int64, error) {
	want := storage.BodyTarget{UIDValidity: folder.UIDValidity, MessageID: rowID}
	id, err := storeFetched(ctx, e.store, e.log, folder, msg, !e.WithoutAttachments, insert, want)
	if errors.Is(err, storage.ErrBodyTargetChanged) {
		e.log.Warn("fetched body dropped: its message was replaced meanwhile",
			"folder", folder.IMAPPath, "remote_id", msg.RemoteID, "err", err)
		return 0, nil
	}
	return id, err
}

// StoreFetched writes one fetched message to the cache the way sync does: a
// stub row is filled in place, a row that already has its body is left alone
// and 0 is returned, and the raw source of PGP mail is kept for decryption.
// A message with no row is inserted, which is what an explicit download of
// mail outside the sync window needs; sync's own body fills never insert.
// withAttachments false stores the message without attachment files.
// LegacyUID is copied from the adapter as-is; a UID is never inferred by
// parsing RemoteID. msg must have been fetched under folder's generation
// (folder.UIDValidity): when the folder was reset since, nothing is stored
// and the error is storage.ErrBodyTargetChanged.
func StoreFetched(ctx context.Context, store *storage.DB, log *slog.Logger, folder storage.Folder, msg Fetched, withAttachments bool) (int64, error) {
	return storeFetched(ctx, store, log, folder, msg, withAttachments, true, storage.BodyTarget{UIDValidity: folder.UIDValidity})
}

// storeFetched is StoreFetched; insert false only fills an existing stub and
// returns 0 when the row is gone, so a body that lands after the user moved
// or archived the message does not bring it back. want is checked in the
// write transaction; a mismatch returns storage.ErrBodyTargetChanged.
func storeFetched(ctx context.Context, store *storage.DB, log *slog.Logger, folder storage.Folder, msg Fetched, withAttachments, insert bool, want storage.BodyTarget) (int64, error) {
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

	var (
		id  int64
		err error
	)
	if insert {
		id, err = store.InsertFetchedMessage(ctx, want, stored, atts)
	} else {
		id, err = store.FillMessageBody(ctx, want, stored, atts)
	}
	if errors.Is(err, storage.ErrMessageBodyComplete) || errors.Is(err, storage.ErrMessageNotFound) {
		// A retry can list an id whose body landed on an earlier attempt, and
		// a stub can be deleted while its body is in flight. Skipping either
		// lets the rest of the batch continue.
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
