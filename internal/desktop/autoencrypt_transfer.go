package desktop

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/autocrypt"
	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The Autocrypt Setup Message (Level 1, section 4.4) moves a mailbox's key to
// another device: Pelton mails it to the user's own address, encrypted with a
// setup code it shows once, and the other device opens it with that code.

const (
	setupMessageType     = "application/autocrypt-setup"
	setupMessageFilename = "autocrypt-setup-message.html"
	setupMessageHeader   = "Autocrypt-Setup-Message"
)

const setupMessageText = `This message carries your encryption key, so another device can read
the encrypted mail you receive.

To use it, open this message in Pelton or another mail program that supports
Autocrypt on the other device, and type in the setup code that was shown on
the device that sent it. Without that code the key cannot be read.
`

var errNotSetupMessage = errors.New("pelton: this message is not a key transfer from one of your devices")

// SendKeyToOtherDevice mails a mailbox's key to itself as an Autocrypt Setup
// Message and returns the setup code, which is shown once and needed to open
// it on the other device.
func (a *App) SendKeyToOtherDevice(accountID int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return "", err
	}
	keys, err := a.keyStore()
	if err != nil {
		return "", err
	}
	fp, current, err := a.ownKey(keys, account.Email)
	if err != nil {
		return "", err
	}
	code, err := crypto.NewSetupCode()
	if err != nil {
		return "", err
	}
	payload, err := keys.SetupPayload(fp, current, code, a.ownPrefer(accountID))
	if err != nil {
		return "", err
	}
	html := "<html><body><p>This is the Autocrypt setup file used to transfer keys between clients.</p><pre>\n" +
		string(payload) + "</pre></body></html>\n"
	me := smtp.Address{Name: account.DisplayName, Email: account.Email}
	msg := &smtp.Message{
		From:    me,
		To:      []smtp.Address{me},
		Subject: "Autocrypt Setup Message",
		Text:    setupMessageText,
		Attachments: []smtp.Attachment{{
			Filename: setupMessageFilename, ContentType: setupMessageType, Content: []byte(html),
		}},
		Headers: [][2]string{{setupMessageHeader, "v1"}},
	}
	id, err := smtp.Enqueue(a.ctx, a.queue, accountID, msg, nil, crypto.ModeNone, crypto.Options{}, time.Time{})
	if err != nil {
		return "", err
	}
	a.activity.Add(outbox.Entry{MessageID: id, Step: outbox.StepQueued, Recipients: msg.Recipients()})
	a.emit(EventOutboxChanged, nil)
	return code, nil
}

// setupAttachment returns a message's setup file when it is a key transfer
// sent from the user's own address to itself.
func (a *App) setupAttachment(m storage.Message) (storage.Attachment, bool) {
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return storage.Attachment{}, false
	}
	from := autocrypt.AddressesIn(m.FromAddress)
	if len(from) != 1 || from[0] != autocrypt.NormalizeAddr(account.Email) {
		return storage.Attachment{}, false
	}
	atts, err := a.store.ListAttachments(a.ctx, m.ID)
	if err != nil {
		return storage.Attachment{}, false
	}
	for _, att := range atts {
		if strings.EqualFold(att.ContentType, setupMessageType) {
			return att, true
		}
	}
	return storage.Attachment{}, false
}

// ImportKeyFromSetupMessage opens a key transfer with its setup code and
// makes the key it carries this mailbox's key.
func (a *App) ImportKeyFromSetupMessage(messageID int64, code string) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return err
	}
	att, ok := a.setupAttachment(*m)
	if !ok {
		return errNotSetupMessage
	}
	if att.DiskPath == "" {
		return fmt.Errorf("pelton: download this message first, its key file is not on this device yet")
	}
	f, err := a.store.OpenAttachment(a.ctx, att)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	f.Close()
	if err != nil {
		return err
	}
	keys, err := a.keyStore()
	if err != nil {
		return err
	}
	pass, err := newKeyPassphrase()
	if err != nil {
		return err
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return err
	}
	previous := ""
	if ent, err := keys.SenderKey(account.Email); err == nil {
		previous = crypto.Fingerprint(ent)
	}
	info, prefer, err := keys.ImportSetupPayload(data, code, pass)
	if errors.Is(err, crypto.ErrWrongSetupCode) {
		return fmt.Errorf("pelton: that setup code does not open this message. Check it against the code the other device showed")
	}
	if err != nil {
		return err
	}
	fp := crypto.NormalizeFingerprint(info.Fingerprint)
	if err := credentials.StorePGPPassphrase(fp, string(pass)); err != nil {
		_ = keys.Delete(fp)
		return fmt.Errorf("pelton: the key's passphrase could not be stored, so the key was not imported: %w", err)
	}
	passphraseMu.Lock()
	passphrases[fp] = pass
	passphraseMu.Unlock()
	// the key from the other device becomes this mailbox's key; the one it
	// replaces stays behind it, to read what was encrypted to it.
	if previous != "" && crypto.NormalizeFingerprint(previous) != fp {
		if err := keys.Demote(previous); err != nil {
			return err
		}
	}

	generated := a.generatedKeys()
	generated = append(generated, fp)
	if err := a.store.SetJSON(a.ctx, settingAutoEncryptOwnKeys, generated); err != nil {
		return err
	}
	mode := accountModeManual
	if prefer == autocrypt.PreferMutual {
		mode = accountModeAuto
	}
	if err := a.SetAutoEncryptAccountMode(m.AccountID, mode); err != nil {
		return err
	}
	a.emit(EventAutoEncryptChanged, nil)
	return nil
}
