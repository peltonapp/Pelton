package desktop

import (
	"errors"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Reading protected mail (#193). The ciphertext is kept by the sync layer and
// decrypted here, in memory, every time a message is opened. Plaintext is never
// written back to the database: caching it would undo the encryption it was
// sent with.
//
// Verification runs on every open rather than once at sync, because the keyring
// is local and changes. Import a correspondent's key today and yesterday's mail
// from them starts verifying immediately, with nothing to re-sync.

// protectedState describes why a protected message is not being shown as
// plaintext, so the reading pane can offer the right next step instead of a
// generic error.
const (
	// pgpStateOpen means the message was decrypted and is being shown.
	pgpStateOpen = "open"
	// pgpStateLocked means a key that can open it is present but locked, so the
	// ui should ask for the passphrase.
	pgpStateLocked = "locked"
	// pgpStateNoKey means no imported private key can open it.
	pgpStateNoKey = "nokey"
	// pgpStateFailed means the pgp data could not be read at all.
	pgpStateFailed = "failed"
)

// openProtected decrypts and verifies a stored protected message. It returns
// the zero value for mail that is not protected, which is nearly all of it, so
// the caller can apply it unconditionally.
func (a *App) openProtected(m storage.Message) (body string, isHTML bool, state string, sig crypto.Signature) {
	raw, err := a.store.MessagePGPSource(a.ctx, m.ID)
	if err != nil {
		// not protected, or cached before the source was kept.
		return "", false, "", crypto.Signature{}
	}
	keys, err := a.keyStore()
	if err != nil {
		return "", false, pgpStateFailed, crypto.Signature{}
	}

	engine := crypto.NewPGP(keys)
	opened, err := engine.Open(raw, a.anyPassphrase())
	if err != nil {
		return "", false, openFailureState(err), crypto.Signature{}
	}

	plain, html := splitOpenedBody(opened.Body)
	if html != "" {
		return html, true, pgpStateOpen, opened.Signature
	}
	return plain, false, pgpStateOpen, opened.Signature
}

// openFailureState maps a decryption failure to the state the ui acts on.
func openFailureState(err error) string {
	switch {
	case errors.Is(err, crypto.ErrPassphraseRequired), errors.Is(err, crypto.ErrWrongPassphrase):
		return pgpStateLocked
	case errors.Is(err, crypto.ErrNoDecryptionKey), errors.Is(err, crypto.ErrSenderKeyNotFound):
		return pgpStateNoKey
	case errors.Is(err, crypto.ErrNotProtected):
		return ""
	}
	return pgpStateFailed
}

// anyPassphrase returns a remembered passphrase to try. A message names the key
// it was encrypted to by key id rather than by address, so there is nothing to
// look up by: whichever keys the user has unlocked this session are the
// candidates, and go-crypto stops at the first that works.
//
// Returning one passphrase rather than all of them is deliberate. Trying every
// remembered passphrase against every key would turn a locked message into a
// silent search across the user's keys, and the common case is a single key.
func (a *App) anyPassphrase() []byte {
	passphraseMu.Lock()
	defer passphraseMu.Unlock()
	for _, p := range passphrases {
		return p
	}
	return nil
}

// splitOpenedBody separates the decrypted MIME entity into its text and html
// parts. The decrypted payload is a MIME entity in its own right, so it is
// parsed the same way any body would be rather than shown as raw source.
func splitOpenedBody(entity []byte) (plain, html string) {
	msg, err := mailview.ParseEntity(entity)
	if errors.Is(err, mailview.ErrNoReadableBody) {
		// a MIME entity with nothing to read: an empty body, not its source.
		return "", ""
	}
	if err != nil {
		// not a MIME entity: a clearsigned block is plain text as it stands.
		return string(entity), ""
	}
	return msg.Text, msg.HTML
}

// sessionPassphraseKey labels a passphrase entered to read a message. A message
// names the key it was encrypted to by key id, not by fingerprint, so there is
// no fingerprint to file it under; this slot keeps it usable for the rest of
// the session without pretending to know which key it belongs to.
const sessionPassphraseKey = "reading-pane-session"

// UnlockMessage tries a passphrase against a protected message and, if it
// opens, holds the passphrase for the rest of the session so the reader is not
// asked again for every message.
//
// It deliberately does not offer to store the passphrase in the os keyring.
// Doing that needs a specific key to file it under, which the Encryption pane
// has and a message does not, and silently persisting a passphrase in answer to
// a prompt about reading one message would be a larger commitment than the
// prompt asked for.
func (a *App) UnlockMessage(messageID int64, passphrase string) error {
	if err := a.ready(); err != nil {
		return err
	}
	raw, err := a.store.MessagePGPSource(a.ctx, messageID)
	if err != nil {
		return err
	}
	keys, err := a.keyStore()
	if err != nil {
		return err
	}
	if _, err := crypto.NewPGP(keys).Open(raw, []byte(passphrase)); err != nil {
		return err
	}
	passphraseMu.Lock()
	passphrases[sessionPassphraseKey] = []byte(passphrase)
	passphraseMu.Unlock()
	return nil
}
