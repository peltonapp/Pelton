// Seed fills alice and bob inboxes over IMAP APPEND.
// Each inbox receives messages from the other account. The corpus is not committed.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const (
	// imapAddr is Stalwart's IMAPS port. Its certificate is issued for
	// 127.0.0.1 by the e2e CA in certs/.
	imapAddr = "127.0.0.1:993"
	fiveMB   = 5 * 1024 * 1024
	// appendWindow is how many APPENDs are in flight at once.
	appendWindow = 32
)

// perInbox is 6,400 for the Playwright suite. E2E_SEED_PER_INBOX lowers it
// for manual.sh, where a smaller inbox is quicker to seed.
var perInbox = seedCount()

var (
	start = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end   = time.Date(2026, 9, 24, 23, 59, 59, 0, time.UTC)
)

type account struct {
	name   string
	email  string
	secret string
}

type manifest struct {
	AliceCount         int    `json:"aliceCount"`
	BobCount           int    `json:"bobCount"`
	AliceShortSubject  string `json:"aliceShortSubject"`
	AliceLargeSubject  string `json:"aliceLargeSubject"`
	AliceSearchSubject string `json:"aliceSearchSubject"`
	ShortBody          string `json:"shortBody"`
	LargePrefix        string `json:"largePrefix"`
}

func main() {
	alice := account{name: "alice", email: "alice@example.org", secret: "alice-e2e"}
	bob := account{name: "bob", email: "bob@example.org", secret: "bob-e2e"}

	aliceC := login(alice)
	defer aliceC.Close()
	bobC := login(bob)
	defer bobC.Close()

	rng := rand.New(rand.NewSource(1))
	man := manifest{
		AliceCount:  perInbox,
		BobCount:    perInbox,
		ShortBody:   "x",
		LargePrefix: "LARGE-BODY-MARKER",
	}

	fmt.Println("seeding alice inbox from bob")
	man.AliceShortSubject, man.AliceLargeSubject, man.AliceSearchSubject = seedInbox(aliceC, alice, bob.email, rng)
	fmt.Println("seeding bob inbox from alice")
	seedInbox(bobC, bob, alice.email, rng)

	out := filepath.Join(moduleE2E(), "manifest.json")
	raw, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(out, append(raw, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("wrote", out)
}

// login opens an IMAPS session as a. The e2e CA is added to the system roots,
// so seeding works before the CA is trusted system-wide too.
func login(a account) *imapclient.Client {
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if pem, err := os.ReadFile(filepath.Join(moduleE2E(), "certs", "ca.crt")); err == nil {
		roots.AppendCertsFromPEM(pem)
	}
	c, err := imapclient.DialTLS(imapAddr, &imapclient.Options{
		TLSConfig: &tls.Config{RootCAs: roots, ServerName: "127.0.0.1"},
	})
	if err != nil {
		fatal(fmt.Errorf("%s connect: %w", a.name, err))
	}
	if err := c.Login(a.email, a.secret).Wait(); err != nil {
		fatal(fmt.Errorf("%s login: %w", a.name, err))
	}
	return c
}

// seedInbox appends perInbox messages to owner's INBOX, sent as fromEmail.
func seedInbox(c *imapclient.Client, owner account, fromEmail string, rng *rand.Rand) (shortSub, largeSub, searchSub string) {
	var inFlight []*imapclient.AppendCommand
	wait := func(keep int) {
		for len(inFlight) > keep {
			if _, err := inFlight[0].Wait(); err != nil {
				fatal(fmt.Errorf("append: %w", err))
			}
			inFlight = inFlight[1:]
		}
	}

	for i := range perInbox {
		size := bodySize(i)
		subject := randomSubject(rng)
		switch i {
		case 0:
			shortSub = subject
		case 1:
			largeSub = subject
		case 100:
			searchSub = subject
		}
		when := randomWhen(rng)
		raw := message(i, owner, fromEmail, subject, when, bodyText(i, size))
		cmd := c.Append("INBOX", int64(len(raw)), &imap.AppendOptions{Time: when})
		if _, err := cmd.Write(raw); err != nil {
			fatal(fmt.Errorf("append write: %w", err))
		}
		if err := cmd.Close(); err != nil {
			fatal(fmt.Errorf("append close: %w", err))
		}
		inFlight = append(inFlight, cmd)
		wait(appendWindow)
		if (i+1)%200 == 0 {
			fmt.Printf("  %d/%d\n", i+1, perInbox)
		}
	}
	wait(0)
	return shortSub, largeSub, searchSub
}

// message is one plain-text RFC 5322 message from fromEmail to owner.
func message(i int, owner account, fromEmail, subject string, when time.Time, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: <%s>\r\n", fromEmail)
	fmt.Fprintf(&b, "To: <%s>\r\n", owner.email)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", when.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <seed-%s-%d@example.org>\r\n", owner.name, i)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=us-ascii\r\n")
	b.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

func bodySize(i int) int {
	rng := rand.New(rand.NewSource(int64(i) + 17))
	switch {
	case i == 0:
		return 1
	case i == 1:
		return fiveMB
	case i < 42:
		return logUniform(rng, 1<<20, 4<<20)
	case i%25 == 0:
		return logUniform(rng, 100*1024, 800*1024)
	default:
		return logUniform(rng, 1024, 40*1024)
	}
}

func bodyText(i, size int) string {
	if size == 1 {
		return "x"
	}
	prefix := ""
	if i == 1 {
		prefix = "LARGE-BODY-MARKER\r\n"
	}
	if len(prefix) > size {
		return prefix[:size]
	}
	unit := []byte("abcdefghijklmnopqrstuvwxyz\r\n")
	buf := make([]byte, size)
	copy(buf, prefix)
	for n := len(prefix); n < size; {
		c := copy(buf[n:], unit)
		n += c
	}
	// a cut can leave a lone CR; end on a letter instead
	if buf[size-1] == '\r' {
		buf[size-1] = 'z'
	}
	return string(buf)
}

func logUniform(rng *rand.Rand, min, max int) int {
	ln := math.Log(float64(min)) + rng.Float64()*(math.Log(float64(max))-math.Log(float64(min)))
	n := int(math.Exp(ln))
	if n < min {
		n = min
	}
	if n > max {
		n = max
	}
	return n
}

func randomSubject(rng *rand.Rand) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	n := 3 + rng.Intn(33)
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(b)
}

func randomWhen(rng *rand.Rand) time.Time {
	span := end.Sub(start)
	return start.Add(time.Duration(rng.Int63n(int64(span))))
}

func moduleE2E() string {
	wd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	if filepath.Base(wd) == "seed" {
		return filepath.Dir(wd)
	}
	if filepath.Base(wd) == "e2e" {
		return wd
	}
	return filepath.Join(wd, "e2e")
}

func seedCount() int {
	raw := os.Getenv("E2E_SEED_PER_INBOX")
	if raw == "" {
		return 6400
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		fatal(fmt.Errorf("E2E_SEED_PER_INBOX must be a non-negative number, got %q", raw))
	}
	return n
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
