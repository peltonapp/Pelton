package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/peltonapp/Pelton/internal/autocrypt"
	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/keylookup"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

// untouchableNet is a lookup client whose every way onto the network fails
// the test.
func untouchableNet(t *testing.T) *keylookup.Client {
	return keylookup.New(keylookup.Config{
		Lookup: func(context.Context, string) ([]netip.Addr, error) {
			t.Error("a key lookup resolved a host name")
			return nil, errors.New("no network in tests")
		},
		Dial: func(context.Context, string, string) (net.Conn, error) {
			t.Error("a key lookup opened a connection")
			return nil, errors.New("no network in tests")
		},
	})
}

// fakeKeyNet serves keys over TLS for any host name and counts requests.
type fakeKeyNet struct {
	srv      *httptest.Server
	requests atomic.Int32
	keys     map[string][]byte
}

func newFakeKeyNet(t *testing.T, keys map[string][]byte) *fakeKeyNet {
	n := &fakeKeyNet{keys: keys}
	n.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.requests.Add(1)
		for local, key := range n.keys {
			if strings.HasSuffix(r.URL.Path, "/hu/"+keylookup.WKDHash(local)) || strings.HasSuffix(r.URL.Path, "/"+local+"@example.com") {
				w.Write(key)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(n.srv.Close)
	return n
}

func (n *fakeKeyNet) client() *keylookup.Client {
	return keylookup.New(keylookup.Config{
		Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			if strings.HasPrefix(host, "openpgpkey.") {
				return nil, errors.New("no such host")
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, n.srv.Listener.Addr().String())
		},
		TLS:       n.srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
		AllowAddr: func(netip.Addr) bool { return true },
	})
}

// autoTestApp is an app with one account, me@example.com, and nothing
// switched on.
func autoTestApp(t *testing.T) (*App, int64) {
	t.Helper()
	app, db, accountID := pgpTestApp(t, nil, nil)
	app.queue = outbox.NewQueue(db)
	app.autoEnc.client = untouchableNet(t)
	return app, accountID
}

// switchOn enables the feature with the given sources and waits for the
// account's key.
func switchOn(t *testing.T, app *App, sources KeySourcesDTO) {
	t.Helper()
	if err := app.EnableAutoEncrypt(sources); err != nil {
		t.Fatalf("EnableAutoEncrypt: %v", err)
	}
	app.ensureOwnKeys()
}

func peerEntity(t *testing.T, email string) *openpgp.Entity {
	t.Helper()
	ent, err := openpgp.NewEntity("Peer", "", email, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ent
}

func pubBytes(t *testing.T, ent *openpgp.Entity) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := ent.Serialize(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// incomingFrom builds the header block of a message from addr carrying an
// Autocrypt header for key.
func incomingFrom(addr string, date time.Time, key []byte, prefer string) []byte {
	value := "addr=" + addr + "; "
	if prefer != "" {
		value += "prefer-encrypt=" + prefer + "; "
	}
	value += "keydata=" + base64.StdEncoding.EncodeToString(key)
	return []byte("From: " + addr + "\r\nTo: me@example.com\r\nDate: " + date.Format(time.RFC1123Z) +
		"\r\nAutocrypt: " + value + "\r\nSubject: hi\r\n\r\nhello\r\n")
}

func receive(t *testing.T, app *App, raw []byte) {
	t.Helper()
	in, ok := autocrypt.ReadIncoming(raw)
	if !ok {
		t.Fatal("test message not readable")
	}
	if err := app.applyIncoming(in); err != nil {
		t.Fatalf("applyIncoming: %v", err)
	}
}

func status(t *testing.T, app *App, accountID int64, q AutoEncryptQuery) AutoEncryptStatusDTO {
	t.Helper()
	q.AccountID = accountID
	s, err := app.AutoEncryptStatus(q)
	if err != nil {
		t.Fatalf("AutoEncryptStatus: %v", err)
	}
	return s
}

func TestFeatureOffDoesNothing(t *testing.T) {
	app, accountID := autoTestApp(t)

	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com"}}); s.Enabled || s.Recommendation != "disable" {
		t.Errorf("status with the feature off = %+v", s)
	}
	if found, err := app.LookupRecipientKeys(accountID, []string{"anna@example.com"}); found || err != nil {
		t.Errorf("lookup with the feature off = %v, %v", found, err)
	}

	app.observeIncoming(storage.Folder{IMAPPath: "INBOX"}, incomingFrom("anna@example.com", time.Now(), pubBytes(t, peerEntity(t, "anna@example.com")), "mutual"))
	if len(app.autoEnc.incoming) != 0 || app.autoEnc.draining {
		t.Error("an Autocrypt header was queued with the feature off")
	}

	if _, err := app.SendMessage(ComposeRequest{AccountID: accountID, To: addr("anna@example.com"), Subject: "s", Text: "t", AutoEncrypt: true}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	queued, _ := app.queue.List(app.ctx)
	if len(queued) != 1 || bytes.Contains(queued[0].Raw, []byte("Autocrypt:")) {
		t.Error("an Autocrypt header went out with the feature off")
	}
	keys, _ := app.keyStore()
	if list, _ := keys.List(); len(list) != 0 {
		t.Errorf("%d keys exist with the feature off", len(list))
	}
}

func TestEnableMakesAProtectedKeyPerMailbox(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})

	settings, err := app.GetAutoEncryptSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Enabled || len(settings.Accounts) != 1 {
		t.Fatalf("settings = %+v", settings)
	}
	own := settings.Accounts[0]
	if own.AccountID != accountID || own.Fingerprint == "" || !own.Generated || !own.Usable || own.Expires == "" || own.Mode != accountModeAuto {
		t.Errorf("own key = %+v", own)
	}
	stored, err := credentials.LoadPGPPassphrase(crypto.NormalizeFingerprint(own.Fingerprint))
	if err != nil || stored == "" {
		t.Errorf("passphrase not kept in the credential store: %v", err)
	}
	keys, _ := app.keyStore()
	ent, _ := keys.SenderKey("me@example.com")
	if !crypto.KeyLocked(ent) {
		t.Error("generated private key is not protected")
	}

	// a second pass makes no second key.
	app.ensureOwnKeys()
	if list, _ := keys.List(); len(list) != 1 {
		t.Errorf("%d keys after a second pass", len(list))
	}
}

func TestOwnKeyIsRenewedBeforeItExpires(t *testing.T) {
	app, _ := autoTestApp(t)
	clock := time.Now()
	app.autoEnc.now = func() time.Time { return clock }
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	before, _ := app.GetAutoEncryptSettings()
	expires, _ := time.Parse(time.RFC3339, before.Accounts[0].Expires)

	clock = expires.Add(-90 * 24 * time.Hour)
	app.ensureOwnKeys()
	after, _ := app.GetAutoEncryptSettings()
	renewed, _ := time.Parse(time.RFC3339, after.Accounts[0].Expires)
	if !renewed.After(expires) || after.Accounts[0].Fingerprint != before.Accounts[0].Fingerprint {
		t.Errorf("expiry %v -> %v", expires, renewed)
	}
}

func TestNoRequestWhenSourcesAreOff(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true, Servers: []string{"keys.example.com"}})
	if found, err := app.LookupRecipientKeys(accountID, []string{"anna@example.com", "bob@example.org"}); found || err != nil {
		t.Errorf("lookup with every network source off = %v, %v", found, err)
	}
	if log, _ := app.ListKeyLookupLog(); len(log) != 0 {
		t.Errorf("lookup log has %d entries", len(log))
	}
}

func TestWKDLookupCachesLogsAndResolves(t *testing.T) {
	app, accountID := autoTestApp(t)
	anna := peerEntity(t, "anna@example.com")
	net := newFakeKeyNet(t, map[string][]byte{"anna": pubBytes(t, anna)})
	app.autoEnc.client = net.client()
	clock := time.Now()
	app.autoEnc.now = func() time.Time { return clock }
	switchOn(t, app, KeySourcesDTO{WKD: true})

	found, err := app.LookupRecipientKeys(accountID, []string{"Anna@example.com", "bob@example.com", "me@example.com"})
	if err != nil || !found {
		t.Fatalf("lookup = %v, %v", found, err)
	}
	// anna: advanced host missing, so the direct method; bob: the same, and a
	// 404. Nothing for the user's own address.
	if got := net.requests.Load(); got != 2 {
		t.Errorf("%d requests, want 2", got)
	}
	log, _ := app.ListKeyLookupLog()
	if len(log) != 2 {
		t.Fatalf("log = %+v", log)
	}
	outcomes := map[string]string{}
	for _, e := range log {
		outcomes[e.Email] = e.Outcome
		if e.Source != keySourceWKD {
			t.Errorf("log source = %q", e.Source)
		}
	}
	if outcomes["anna@example.com"] != lookupFound || outcomes["bob@example.com"] != lookupNone {
		t.Errorf("outcomes = %v", outcomes)
	}

	s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com"}})
	if s.Recommendation != "encrypt" || s.Recipients[0].Source != keySourceWKD || s.Recipients[0].State != recipientReady {
		t.Errorf("status = %+v", s)
	}

	// both answers are cached: asking again sends nothing.
	if _, err := app.LookupRecipientKeys(accountID, []string{"anna@example.com", "bob@example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := net.requests.Load(); got != 2 {
		t.Errorf("cached lookups made %d requests in all", got)
	}
	// a day later the negative answer for bob is asked again, anna's key is not.
	clock = clock.Add(25 * time.Hour)
	if _, err := app.LookupRecipientKeys(accountID, []string{"anna@example.com", "bob@example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := net.requests.Load(); got != 3 {
		t.Errorf("%d requests after a day, want 3", got)
	}

	if err := app.ClearKeyLookupLog(); err != nil {
		t.Fatal(err)
	}
	if log, _ := app.ListKeyLookupLog(); len(log) != 0 {
		t.Error("log not cleared")
	}
}

func TestAutocryptHeaderMakesEncryptionTheDefault(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	anna := peerEntity(t, "anna@example.com")
	bob := peerEntity(t, "bob@example.com")
	receive(t, app, incomingFrom("anna@example.com", time.Now().Add(-time.Hour), pubBytes(t, anna), "mutual"))
	receive(t, app, incomingFrom("bob@example.com", time.Now().Add(-time.Hour), pubBytes(t, bob), ""))

	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com"}}); s.Recommendation != "encrypt" || !s.OwnKey {
		t.Errorf("mutual peer = %+v", s)
	}
	// bob has no preference: encryption is available but not switched on.
	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com", "bob@example.com"}}); s.Recommendation != "available" {
		t.Errorf("mixed = %+v", s)
	}
	// someone with no key at all disables it, and is named.
	s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com"}, Cc: []string{"carl@example.com"}})
	if s.Recommendation != "disable" || s.Recipients[1].State != recipientMissing {
		t.Errorf("missing key = %+v", s)
	}
	// in manual mode nothing turns on by itself.
	if err := app.SetAutoEncryptAccountMode(accountID, accountModeManual); err != nil {
		t.Fatal(err)
	}
	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com"}}); s.Recommendation != "available" {
		t.Errorf("manual mode = %+v", s)
	}
}

func TestExpiredAndRevokedKeysAreNeverUsed(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})

	past := time.Now().Add(-3 * 365 * 24 * time.Hour)
	expired, err := openpgp.NewEntity("Old", "", "old@example.com", &packet.Config{
		Time: func() time.Time { return past }, KeyLifetimeSecs: 86400,
	})
	if err != nil {
		t.Fatal(err)
	}
	revoked := peerEntity(t, "gone@example.com")
	if err := revoked.RevokeKey(packet.KeyRetired, "", nil); err != nil {
		t.Fatal(err)
	}
	receive(t, app, incomingFrom("old@example.com", time.Now().Add(-time.Hour), pubBytes(t, expired), "mutual"))
	receive(t, app, incomingFrom("gone@example.com", time.Now().Add(-time.Hour), pubBytes(t, revoked), "mutual"))

	s := status(t, app, accountID, AutoEncryptQuery{To: []string{"old@example.com", "gone@example.com"}})
	if s.Recommendation != "disable" {
		t.Errorf("recommendation = %s", s.Recommendation)
	}
	for _, r := range s.Recipients {
		if r.State != recipientMissing || r.Problem == "" {
			t.Errorf("%s = %+v, want missing with a reason", r.Email, r)
		}
	}
	if _, err := app.SendMessage(ComposeRequest{AccountID: accountID, To: addr("old@example.com"), Subject: "s", Text: "t", Protection: protectionSignEncrypt, AutoEncrypt: true}); !errors.Is(err, crypto.ErrRecipientKeyNotFound) {
		t.Errorf("send to an expired key = %v", err)
	}
}

// sentRaw returns the only message in the outbox.
func sentRaw(t *testing.T, app *App) []byte {
	t.Helper()
	queued, err := app.queue.List(app.ctx)
	if err != nil || len(queued) == 0 {
		t.Fatalf("outbox = %d, %v", len(queued), err)
	}
	return queued[len(queued)-1].Raw
}

func sessionKeyIDs(t *testing.T, raw []byte) []uint64 {
	t.Helper()
	start := bytes.Index(raw, []byte("-----BEGIN PGP MESSAGE-----"))
	end := bytes.Index(raw, []byte("-----END PGP MESSAGE-----"))
	if start < 0 || end < 0 {
		t.Fatal("no encrypted payload")
	}
	block, err := armor.Decode(bytes.NewReader(raw[start : end+len("-----END PGP MESSAGE-----")]))
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint64
	r := packet.NewReader(block.Body)
	for {
		p, err := r.Next()
		if err != nil {
			break
		}
		ek, ok := p.(*packet.EncryptedKey)
		if !ok {
			break
		}
		ids = append(ids, ek.KeyId)
	}
	return ids
}

func decryptAs(t *testing.T, raw []byte, reader *openpgp.Entity) string {
	t.Helper()
	start := bytes.Index(raw, []byte("-----BEGIN PGP MESSAGE-----"))
	block, err := armor.Decode(bytes.NewReader(raw[start:]))
	if err != nil {
		t.Fatal(err)
	}
	md, err := openpgp.ReadMessage(block.Body, openpgp.EntityList{reader}, nil, nil)
	if err != nil {
		t.Fatalf("%s cannot decrypt: %v", reader.PrimaryIdentity().UserId.Email, err)
	}
	body, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestEncryptedSendHidesBccAndProtectsHeaders(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	anna, bob, bea := peerEntity(t, "anna@example.com"), peerEntity(t, "bob@example.com"), peerEntity(t, "bea@example.com")
	for _, p := range []*openpgp.Entity{anna, bob, bea} {
		email := p.PrimaryIdentity().UserId.Email
		receive(t, app, incomingFrom(email, time.Now().Add(-time.Hour), pubBytes(t, p), "mutual"))
	}

	_, err := app.SendMessage(ComposeRequest{
		AccountID: accountID, To: addr("anna@example.com"), Cc: addr("bob@example.com"), Bcc: addr("bea@example.com"),
		Subject: "The secret plan", Text: "meet at noon", Protection: protectionSignEncrypt, AutoEncrypt: true,
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	raw := sentRaw(t, app)
	outer, _, _ := bytes.Cut(raw, []byte("\r\n\r\n"))
	if bytes.Contains(raw, []byte("meet at noon")) || bytes.Contains(outer, []byte("secret plan")) {
		t.Fatal("plaintext or the subject is visible outside the encryption")
	}
	if !bytes.Contains(outer, []byte("Subject: ...")) || !bytes.Contains(outer, []byte("Autocrypt: addr=me@example.com; prefer-encrypt=mutual;")) {
		t.Errorf("outer headers:\n%s", outer)
	}
	if bytes.Contains(outer, []byte("bea@example.com")) {
		t.Error("the bcc recipient is in the headers")
	}

	beaEnc, _ := bea.EncryptionKey(time.Now())
	var hidden int
	for _, id := range sessionKeyIDs(t, raw) {
		if id == beaEnc.PublicKey.KeyId {
			t.Error("the bcc recipient's key id is on the message")
		}
		if id == 0 {
			hidden++
		}
	}
	if hidden != 1 {
		t.Errorf("%d hidden recipients, want 1", hidden)
	}

	for _, reader := range []*openpgp.Entity{anna, bob, bea} {
		inner := decryptAs(t, raw, reader)
		if !strings.Contains(inner, "meet at noon") || !strings.Contains(inner, "Subject: The secret plan") {
			t.Errorf("inner message for %s:\n%s", reader.PrimaryIdentity().UserId.Email, inner)
		}
		if !strings.Contains(inner, "Autocrypt-Gossip: addr=anna@example.com") || !strings.Contains(inner, "Autocrypt-Gossip: addr=bob@example.com") {
			t.Error("gossip for the visible recipients is missing")
		}
		if strings.Contains(inner, "Autocrypt-Gossip: addr=bea@example.com") {
			t.Error("the bcc recipient's key is gossiped")
		}
	}
	// the sender reads the sent copy.
	keys, _ := app.keyStore()
	fp, pass, _ := app.ownKey(keys, "me@example.com")
	if _, err := crypto.NewPGP(keys).Open(raw, pass); err != nil {
		t.Errorf("sender cannot open the sent copy (%s): %v", fp, err)
	}
	// the keys used are now the ones expected next time.
	if rec, ok, _ := app.store.AcceptedKeyFor(app.ctx, "bea@example.com"); !ok || rec.Fingerprint != crypto.Fingerprint(bea) {
		t.Errorf("bea's key was not pinned: %+v", rec)
	}
}

func TestKeyChangeNeedsAcceptance(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	first, second := peerEntity(t, "anna@example.com"), peerEntity(t, "anna@example.com")
	receive(t, app, incomingFrom("anna@example.com", time.Now().Add(-2*time.Hour), pubBytes(t, first), "mutual"))
	send := func() error {
		_, err := app.SendMessage(ComposeRequest{AccountID: accountID, To: addr("anna@example.com"), Subject: "s", Text: "t", Protection: protectionSignEncrypt, AutoEncrypt: true})
		return err
	}
	if err := send(); err != nil {
		t.Fatalf("first send: %v", err)
	}

	receive(t, app, incomingFrom("anna@example.com", time.Now().Add(-time.Hour), pubBytes(t, second), "mutual"))
	s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com"}})
	r := s.Recipients[0]
	if r.State != recipientChanged || r.Previous != crypto.Fingerprint(first) || r.Fingerprint != crypto.Fingerprint(second) || r.ChangedAt == "" || s.Recommendation != "disable" {
		t.Fatalf("after the change = %+v / %+v", s, r)
	}
	if err := send(); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("send with an unaccepted new key = %v", err)
	}
	if err := app.AcceptContactKey("anna@example.com", crypto.Fingerprint(first)); err == nil {
		t.Error("accepted a key other than the one found")
	}
	if err := app.AcceptContactKey("anna@example.com", crypto.Fingerprint(second)); err != nil {
		t.Fatal(err)
	}
	if err := send(); err != nil {
		t.Fatalf("send after accepting: %v", err)
	}
	decryptAs(t, sentRaw(t, app), second)

	if err := app.VerifyContactKey("anna@example.com", crypto.Fingerprint(second)); err != nil {
		t.Fatal(err)
	}
	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com"}}); s.Recipients[0].State != recipientVerified {
		t.Errorf("after verifying = %+v", s.Recipients[0])
	}
}

func TestReplyToEncryptedMailIsNotSilentlyDowngraded(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	ctx := context.Background()
	folderID, err := app.store.CreateFolder(ctx, &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	parentID, err := app.store.InsertMessage(ctx, &storage.Message{AccountID: accountID, FolderID: folderID, UID: 1, MessageID: "<parent@example.com>", FromAddress: "anna@example.com", Subject: "..."})
	if err != nil {
		t.Fatal(err)
	}
	encrypted := "From: anna@example.com\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=b\r\n\r\n--b\r\n\r\nVersion: 1\r\n--b\r\n\r\n-----BEGIN PGP MESSAGE-----\r\n\r\nxx\r\n-----END PGP MESSAGE-----\r\n--b--\r\n"
	if err := app.store.SetMessagePGPSource(ctx, parentID, []byte(encrypted)); err != nil {
		t.Fatal(err)
	}

	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"anna@example.com"}, InReplyTo: "parent@example.com"}); !s.ReplyToEncrypted {
		t.Error("the reply was not recognised as one to encrypted mail")
	}
	reply := ComposeRequest{AccountID: accountID, To: addr("anna@example.com"), Subject: "Re", Text: "t", InReplyTo: "parent@example.com", AutoEncrypt: true}
	if _, err := app.SendMessage(reply); !errors.Is(err, errReplyDowngrade) {
		t.Fatalf("plaintext reply = %v, want errReplyDowngrade", err)
	}
	reply.PlaintextConfirmed = true
	if _, err := app.SendMessage(reply); err != nil {
		t.Fatalf("confirmed plaintext reply: %v", err)
	}
}

func TestGossipIsLearnedFromDecryptedMail(t *testing.T) {
	app, _ := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	bob := peerEntity(t, "bob@example.com")
	gossip := autocrypt.FormatGossip(autocrypt.Header{Addr: "bob@example.com", KeyData: pubBytes(t, bob)})
	entity := "Content-Type: text/plain; protected-headers=\"v1\"\r\nSubject: Real subject\r\nAutocrypt-Gossip: " + gossip +
		"\r\nAutocrypt-Gossip: addr=stranger@example.com; keydata=" + base64.StdEncoding.EncodeToString(pubBytes(t, bob)) + "\r\n\r\nbody\r\n"
	m := storage.Message{FromAddress: "anna@example.com", ToAddresses: "me@example.com, bob@example.com", Date: time.Now().Add(-time.Minute)}
	app.learnGossip(m, []byte(entity))

	peer, ok, _ := app.store.AutocryptPeerFor(app.ctx, "bob@example.com")
	if !ok || !bytes.Equal(peer.GossipKey, pubBytes(t, bob)) {
		t.Errorf("gossip not stored: %+v", peer)
	}
	if _, ok, _ := app.store.AutocryptPeerFor(app.ctx, "stranger@example.com"); ok {
		t.Error("gossip about someone who was not a recipient was stored")
	}
	if got := protectedSubject([]byte(entity)); got != "Real subject" {
		t.Errorf("protectedSubject = %q", got)
	}
	// gossip alone only discourages: it is offered, not switched on.
	s := status(t, app, 1, AutoEncryptQuery{To: []string{"bob@example.com"}})
	if s.Recipients[0].Source != keySourceGossip || s.Recommendation != "discourage" {
		t.Errorf("gossip only = %+v", s)
	}
}

func TestSignatureTrustWords(t *testing.T) {
	app, _ := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if err := app.store.RecordKeySeen(app.ctx, "anna@example.com", "AAAA", keySourceAutocrypt, day); err != nil {
		t.Fatal(err)
	}
	m := storage.Message{FromAddress: "Anna <anna@example.com>", Date: day.Add(24 * time.Hour)}
	if got := app.signatureTrust(m, "AAAA", false); got != sigKnown {
		t.Errorf("key seen before = %s", got)
	}
	m.Date = day
	if got := app.signatureTrust(m, "AAAA", false); got != sigNew {
		t.Errorf("key first seen with this message = %s", got)
	}
	if got := app.signatureTrust(m, "BBBB", true); got != sigKnown {
		t.Errorf("key in the keyring = %s", got)
	}
	if err := app.store.SaveAcceptedKey(app.ctx, storage.AcceptedKey{Addr: "anna@example.com", Fingerprint: "AAAA", Verified: true}); err != nil {
		t.Fatal(err)
	}
	if got := app.signatureTrust(m, "AAAA", false); got != sigVerified {
		t.Errorf("verified key = %s", got)
	}
}

func TestScriptConfirmationShowsEncryption(t *testing.T) {
	app, accountID := autoTestApp(t)
	if got := app.encryptionSummary(ComposeRequest{AccountID: accountID, Protection: protectionSignEncrypt}); got != "" {
		t.Errorf("summary with the feature off = %q", got)
	}
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	receive(t, app, incomingFrom("anna@example.com", time.Now().Add(-time.Hour), pubBytes(t, peerEntity(t, "anna@example.com")), "mutual"))

	ok := app.encryptionSummary(ComposeRequest{AccountID: accountID, To: addr("anna@example.com"), Protection: protectionSignEncrypt, AutoEncrypt: true})
	if !strings.Contains(ok, "Every recipient can read it") {
		t.Errorf("summary = %q", ok)
	}
	blocked := app.encryptionSummary(ComposeRequest{AccountID: accountID, To: []AddressDTO{{Email: "anna@example.com"}, {Email: "x@example.org"}}, Protection: protectionSignEncrypt, AutoEncrypt: true})
	if !strings.Contains(blocked, "x@example.org") || !strings.Contains(blocked, "will not be sent") {
		t.Errorf("summary = %q", blocked)
	}
	if off := app.encryptionSummary(ComposeRequest{AccountID: accountID, To: addr("anna@example.com")}); !strings.Contains(off, "unencrypted") {
		t.Errorf("summary = %q", off)
	}
}

func TestContactPreferenceAndForget(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	receive(t, app, incomingFrom("bob@example.com", time.Now().Add(-time.Hour), pubBytes(t, peerEntity(t, "bob@example.com")), ""))
	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"bob@example.com"}}); s.Recommendation != "available" {
		t.Fatalf("no preference = %s", s.Recommendation)
	}
	if err := app.SetContactEncryption("bob@example.com", contactPrefAlways); err != nil {
		t.Fatal(err)
	}
	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"bob@example.com"}}); s.Recommendation != "encrypt" || s.Recipients[0].Preference != contactPrefAlways {
		t.Errorf("always = %+v", s)
	}
	list, err := app.ListContactKeys()
	if err != nil || len(list) != 1 || list[0].Source != keySourceAutocrypt || list[0].FirstSeen == "" {
		t.Errorf("ListContactKeys = %+v, %v", list, err)
	}
	if err := app.ForgetContactKey("bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if s := status(t, app, accountID, AutoEncryptQuery{To: []string{"bob@example.com"}}); s.Recipients[0].State != recipientMissing {
		t.Errorf("after forgetting = %+v", s.Recipients[0])
	}
}

func TestSetupMessageMovesTheKey(t *testing.T) {
	app, accountID := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	code, err := app.SendKeyToOtherDevice(accountID)
	if err != nil {
		t.Fatalf("SendKeyToOtherDevice: %v", err)
	}
	raw := sentRaw(t, app)
	for _, want := range []string{"Autocrypt-Setup-Message: v1", "application/autocrypt-setup", "<me@example.com>\r\nSubject: Autocrypt Setup Message"} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("setup message lacks %q", want)
		}
	}
	if bytes.Contains(raw, []byte(code)) {
		t.Fatal("the setup code is in the message")
	}
}

func TestSyncHeadersAreAppliedInTheBackground(t *testing.T) {
	app, _ := autoTestApp(t)
	switchOn(t, app, KeySourcesDTO{Autocrypt: true})
	key := pubBytes(t, peerEntity(t, "anna@example.com"))
	app.observeIncoming(storage.Folder{IMAPPath: "Spam", Attributes: []string{`\Junk`}}, incomingFrom("spam@example.com", time.Now(), key, "mutual"))
	app.observeIncoming(storage.Folder{IMAPPath: "INBOX"}, incomingFrom("me@example.com", time.Now(), key, "mutual"))
	app.observeIncoming(storage.Folder{IMAPPath: "INBOX"}, incomingFrom("anna@example.com", time.Now(), key, "mutual"))
	if !waitForBackgroundWork(5 * time.Second) {
		t.Fatal("header processing did not finish")
	}
	if _, ok, _ := app.store.AutocryptPeerFor(app.ctx, "anna@example.com"); !ok {
		t.Error("header from the inbox was not applied")
	}
	if _, ok, _ := app.store.AutocryptPeerFor(app.ctx, "spam@example.com"); ok {
		t.Error("header from the junk folder was applied")
	}
	if _, ok, _ := app.store.AutocryptPeerFor(app.ctx, "me@example.com"); ok {
		t.Error("a header from the user's own address was applied")
	}
}

// setupFile pulls the setup file out of a sent Autocrypt Setup Message.
func setupFile(t *testing.T, raw []byte) []byte {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	r := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, err := r.NextPart()
		if err != nil {
			t.Fatal("no setup file in the message")
		}
		if strings.HasPrefix(part.Header.Get("Content-Type"), "application/autocrypt-setup") {
			data, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
			return data
		}
	}
}

func TestSetupMessageImportsOnTheOtherDevice(t *testing.T) {
	first, firstAccount := autoTestApp(t)
	switchOn(t, first, KeySourcesDTO{Autocrypt: true})
	code, err := first.SendKeyToOtherDevice(firstAccount)
	if err != nil {
		t.Fatal(err)
	}
	file := setupFile(t, sentRaw(t, first))
	firstKeys, _ := first.keyStore()
	moved, _ := firstKeys.SenderKey("me@example.com")

	second, secondAccount := autoTestApp(t)
	switchOn(t, second, KeySourcesDTO{Autocrypt: true})
	secondKeys, _ := second.keyStore()
	own, _ := secondKeys.SenderKey("me@example.com")

	folder, err := second.store.CreateFolder(second.ctx, &storage.Folder{AccountID: secondAccount, Name: "INBOX", IMAPPath: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := second.store.InsertMessageWithAttachments(second.ctx, &storage.Message{
		AccountID: secondAccount, FolderID: folder, UID: 1, FromAddress: "me@example.com", Subject: "Autocrypt Setup Message",
	}, []storage.IncomingAttachment{{Filename: setupMessageFilename, ContentType: setupMessageType, Content: bytes.NewReader(file)}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := second.GetMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Encryption == nil || !detail.Encryption.SetupMessage {
		t.Fatalf("setup message not recognised: %+v", detail.Encryption)
	}
	if err := second.ImportKeyFromSetupMessage(id, "0000-0000-0000-0000-0000-0000-0000-0000-0000"); err == nil {
		t.Error("a wrong code imported the key")
	}
	if err := second.ImportKeyFromSetupMessage(id, code); err != nil {
		t.Fatalf("import: %v", err)
	}
	now, _ := secondKeys.SenderKey("me@example.com")
	if crypto.Fingerprint(now) != crypto.Fingerprint(moved) {
		t.Error("the key from the other device is not the one used now")
	}
	// the key it replaced stays, to read what was encrypted to it.
	list, _ := secondKeys.List()
	found := false
	for _, k := range list {
		found = found || k.Fingerprint == crypto.Fingerprint(own)
	}
	if !found {
		t.Error("the replaced key was deleted")
	}
	if fp, pass, err := second.ownKey(secondKeys, "me@example.com"); err != nil || pass == nil || fp != crypto.Fingerprint(moved) {
		t.Errorf("imported key cannot be used without asking: %v", err)
	}
}

func TestAutoEncryptBindingsNeedTheStore(t *testing.T) {
	app := &App{}
	if _, err := app.AutoEncryptStatus(AutoEncryptQuery{}); err == nil {
		t.Error("status without a store")
	}
	if _, err := app.LookupRecipientKeys(1, []string{"a@example.com"}); err == nil {
		t.Error("lookup without a store")
	}
}
