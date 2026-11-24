// Command smtptest exercises the sending side end-to-end: MIME building, crypto,
// the outbox worker, and append-to-Sent/Drafts over a real account.
//
// Config and secrets come from the environment, the same convention as the imap
// layer:
//
//	SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASSWORD   submission server + creds
//	SMTP_OAUTH2_TOKEN                                 bearer token, selects XOAUTH2
//	SMTP_INSECURE=1                                   skip tls verification (debug)
//	MAIL_FROM, MAIL_TO                                envelope/header addresses
//	IMAP_HOST, IMAP_PORT, IMAP_USER, IMAP_PASSWORD    for append-to-Sent/Drafts
//	PGP_KEYDIR                                        dir holding pubring.asc/secring.asc
//	PGP_PASSPHRASE                                    unlocks the sender private key
//	REPLY_MESSAGE_ID, REPLY_REFERENCES               threading inputs for reply mode
//
// Modes (first argument): plain, reply, draft, outbox, pgp, smime.
//
// Real side effects: plain/outbox/pgp actually transmit mail and append to Sent;
// draft appends to Drafts. Use a test recipient you control.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	pcrypto "github.com/peltonapp/Pelton/internal/crypto"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/outbox"
	psmtp "github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"

	"log/slog"
)

const (
	modePlain  = "plain"
	modeReply  = "reply"
	modeDraft  = "draft"
	modeOutbox = "outbox"
	modePGP    = "pgp"
	modeSMIME  = "smime"

	demoAccountID = 1
	pollInterval  = 1 * time.Second
	drainTimeout  = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < 2 {
		return fmt.Errorf("usage: smtptest <%s|%s|%s|%s|%s|%s>",
			modePlain, modeReply, modeDraft, modeOutbox, modePGP, modeSMIME)
	}
	mode := os.Args[1]

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	switch mode {
	case modePlain:
		return runPlain(ctx, log)
	case modeReply:
		return runReply(ctx, log)
	case modeDraft:
		return runDraft(ctx, log)
	case modeOutbox:
		return runOutbox(ctx, log)
	case modePGP:
		return runPGP(ctx, log)
	case modeSMIME:
		return runSMIME(ctx, log)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}

// sampleMessage builds a multipart/alternative message with one attachment and
// one inline image, the realistic shape for the plain and outbox modes.
func sampleMessage() (*psmtp.Message, error) {
	from, to := os.Getenv("MAIL_FROM"), os.Getenv("MAIL_TO")
	if from == "" || to == "" {
		return nil, fmt.Errorf("MAIL_FROM and MAIL_TO must be set")
	}

	// a 1x1 transparent png so the inline cid reference resolves to real bytes.
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	const cid = "logo@pelton"

	return &psmtp.Message{
		From:    psmtp.Address{Name: "Pelton Test", Email: from},
		To:      []psmtp.Address{{Email: to}},
		Subject: "Pelton smtptest message",
		Text:    "This is the plain text alternative.\n",
		HTML:    `<html><body><p>This is the <b>html</b> body.</p><img src="cid:` + cid + `" alt="logo"></body></html>`,
		Attachments: []psmtp.Attachment{
			{Filename: "note.txt", ContentType: "text/plain", Content: []byte("a regular attachment\n")},
			{Filename: "logo.png", ContentType: "image/png", Content: png, Inline: true, ContentID: cid},
		},
	}, nil
}

func runPlain(ctx context.Context, log *slog.Logger) error {
	msg, err := sampleMessage()
	if err != nil {
		return err
	}
	raw, err := psmtp.BuildRaw(msg, nil, pcrypto.ModeNone, pcrypto.Options{})
	if err != nil {
		return err
	}
	fmt.Printf("built message, %d bytes\n", len(raw))

	sender, cleanup, err := newSender(log)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Println("transmitting ...")
	if err := sender.Transmit(ctx, toOutbox(msg, raw)); err != nil {
		return err
	}
	fmt.Println("sent and appended to Sent")
	return nil
}

func runReply(ctx context.Context, log *slog.Logger) error {
	msg, err := sampleMessage()
	if err != nil {
		return err
	}
	parent := os.Getenv("REPLY_MESSAGE_ID")
	if parent == "" {
		return fmt.Errorf("REPLY_MESSAGE_ID must be set for reply mode")
	}
	msg.Subject = "Re: " + msg.Subject
	msg.InReplyTo = parent
	if refs := os.Getenv("REPLY_REFERENCES"); refs != "" {
		msg.References = strings.Fields(refs)
	}

	raw, err := psmtp.BuildRaw(msg, nil, pcrypto.ModeNone, pcrypto.Options{})
	if err != nil {
		return err
	}
	// print the headers so threading can be eyeballed without sending.
	fmt.Println("--- reply headers ---")
	fmt.Println(headerBlock(raw))

	if os.Getenv("REPLY_SEND") != "1" {
		fmt.Println("set REPLY_SEND=1 to actually transmit this reply")
		return nil
	}
	sender, cleanup, err := newSender(log)
	if err != nil {
		return err
	}
	defer cleanup()
	return sender.Transmit(ctx, toOutbox(msg, raw))
}

func runDraft(ctx context.Context, log *slog.Logger) error {
	msg, err := sampleMessage()
	if err != nil {
		return err
	}
	msg.Subject = "[draft] " + msg.Subject
	raw, err := psmtp.BuildRaw(msg, nil, pcrypto.ModeNone, pcrypto.Options{})
	if err != nil {
		return err
	}

	client, err := connectIMAP()
	if err != nil {
		return err
	}
	defer client.Close()
	defer client.Logout()

	folder, err := client.AppendToDrafts(raw)
	if err != nil {
		return err
	}
	fmt.Printf("draft saved to %q\n", folder)
	return nil
}

func runOutbox(ctx context.Context, log *slog.Logger) error {
	store, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	queue := outbox.NewQueue(store)
	msg, err := sampleMessage()
	if err != nil {
		return err
	}

	id, err := psmtp.Enqueue(ctx, queue, demoAccountID, msg, nil, pcrypto.ModeNone, pcrypto.Options{}, time.Time{})
	if err != nil {
		return err
	}
	fmt.Printf("enqueued message id %d\n", id)

	// a deliberately bad host to demonstrate retry and backoff before the real run.
	if os.Getenv("OUTBOX_SIMULATE_FAILURE") == "1" {
		bad := psmtp.NewSender(psmtp.Config{Host: "smtp.invalid.example", Port: psmtp.PortStartTLS}, psmtp.WithLogger(log))
		worker := outbox.NewWorker(queue, bad, outbox.WithLogger(log), outbox.WithInterval(pollInterval))
		fmt.Println("draining once against a bad host to show a failed attempt ...")
		if err := worker.DrainOnce(ctx); err != nil {
			return err
		}
		printOutbox(ctx, queue)
		fmt.Println("the message is requeued with backoff; the real worker below would retry it once due")
		return nil
	}

	sender, cleanup, err := newSender(log)
	if err != nil {
		return err
	}
	defer cleanup()

	worker := outbox.NewWorker(queue, sender, outbox.WithLogger(log), outbox.WithInterval(pollInterval))
	drainCtx, cancel := context.WithTimeout(ctx, drainTimeout)
	defer cancel()

	fmt.Println("draining outbox ...")
	if err := worker.DrainOnce(drainCtx); err != nil {
		return err
	}
	printOutbox(ctx, queue)
	return nil
}

func runPGP(ctx context.Context, log *slog.Logger) error {
	keydir := os.Getenv("PGP_KEYDIR")
	if keydir == "" {
		return fmt.Errorf("PGP_KEYDIR must point at a dir with pubring.asc and secring.asc")
	}
	engine := pcrypto.NewPGP(pcrypto.NewPGPKeyStore(keydir))

	base, err := sampleMessage()
	if err != nil {
		return err
	}
	opts := pcrypto.Options{
		SenderEmail: base.From.Email,
		Recipients:  []string{base.To[0].Email},
		Passphrase:  []byte(os.Getenv("PGP_PASSPHRASE")),
	}

	// build each variant locally to prove the structure without sending.
	for _, m := range []struct {
		name string
		mode pcrypto.Mode
	}{
		{"sign-only", pcrypto.ModeSign},
		{"encrypt-only", pcrypto.ModeEncrypt},
		{"sign+encrypt", pcrypto.ModeSignEncrypt},
	} {
		raw, err := psmtp.BuildRaw(base, engine, m.mode, opts)
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
		fmt.Printf("%-13s built, %d bytes, %s\n", m.name, len(raw), contentTypeOf(raw))
	}

	// prove a missing recipient key is a hard failure with no output.
	missing := pcrypto.Options{SenderEmail: base.From.Email, Recipients: []string{"nobody-" + base.To[0].Email}}
	if _, err := psmtp.BuildRaw(base, engine, pcrypto.ModeEncrypt, missing); err != nil {
		fmt.Printf("missing-key encrypt correctly refused: %v\n", err)
	} else {
		return fmt.Errorf("SAFETY VIOLATION: encrypt to a missing key did not fail")
	}

	if os.Getenv("PGP_SEND") != "1" {
		fmt.Println("set PGP_SEND=1 to transmit the sign+encrypt variant")
		return nil
	}
	raw, err := psmtp.BuildRaw(base, engine, pcrypto.ModeSignEncrypt, opts)
	if err != nil {
		return err
	}
	sender, cleanup, err := newSender(log)
	if err != nil {
		return err
	}
	defer cleanup()
	return sender.Transmit(ctx, toOutbox(base, raw))
}

func runSMIME(ctx context.Context, log *slog.Logger) error {
	_ = ctx
	_ = log
	engine := pcrypto.NewSMIME()
	base, err := sampleMessage()
	if err != nil {
		return err
	}
	opts := pcrypto.Options{SenderEmail: base.From.Email, Recipients: []string{base.To[0].Email}}
	_, err = psmtp.BuildRaw(base, engine, pcrypto.ModeEncrypt, opts)
	fmt.Println("s/mime status: not supported in this build (PGP is the supported path)")
	fmt.Printf("encrypt attempt returned: %v\n", err)
	return nil
}

// newSender builds an smtp sender wired to append to Sent over imap. The cleanup
// closes the imap connection. The imap connection is optional: without imap env
// vars the sender still transmits, just without appending to Sent.
func newSender(log *slog.Logger) (*psmtp.Sender, func(), error) {
	cfg, err := smtpConfig()
	if err != nil {
		return nil, nil, err
	}

	opts := []psmtp.SenderOption{psmtp.WithLogger(log)}
	cleanup := func() {}

	if os.Getenv("IMAP_HOST") != "" {
		client, err := connectIMAP()
		if err != nil {
			return nil, nil, err
		}
		cleanup = func() {
			_ = client.Logout()
			_ = client.Close()
		}
		opts = append(opts, psmtp.WithSentAppender(func(raw []byte) (string, error) {
			return client.AppendToSent(raw)
		}))
	} else {
		log.Warn("no IMAP_HOST set, will not append to Sent")
	}

	return psmtp.NewSender(cfg, opts...), cleanup, nil
}

func smtpConfig() (psmtp.Config, error) {
	cfg := psmtp.Config{
		Host:               os.Getenv("SMTP_HOST"),
		Username:           os.Getenv("SMTP_USER"),
		Password:           os.Getenv("SMTP_PASSWORD"),
		OAuth2Token:        os.Getenv("SMTP_OAUTH2_TOKEN"),
		InsecureSkipVerify: os.Getenv("SMTP_INSECURE") == "1",
	}
	if cfg.Host == "" {
		return cfg, fmt.Errorf("SMTP_HOST must be set")
	}
	if p := os.Getenv("SMTP_PORT"); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return cfg, fmt.Errorf("invalid SMTP_PORT %q: %w", p, err)
		}
		cfg.Port = port
	}
	return cfg, nil
}

func connectIMAP() (*pimap.Client, error) {
	cfg := pimap.Config{
		Host:               os.Getenv("IMAP_HOST"),
		Username:           os.Getenv("IMAP_USER"),
		Password:           os.Getenv("IMAP_PASSWORD"),
		InsecureSkipVerify: os.Getenv("IMAP_INSECURE") == "1",
	}
	if cfg.Host == "" || cfg.Username == "" || cfg.Password == "" {
		return nil, fmt.Errorf("IMAP_HOST, IMAP_USER and IMAP_PASSWORD must be set for append")
	}
	if p := os.Getenv("IMAP_PORT"); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid IMAP_PORT %q: %w", p, err)
		}
		cfg.Port = port
	}
	client, err := pimap.Connect(cfg)
	if err != nil {
		return nil, err
	}
	if err := client.Login(); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func openStore(ctx context.Context) (*storage.DB, error) {
	path, err := storage.DefaultPath()
	if err != nil {
		return nil, err
	}
	store, err := storage.Open(path)
	if err != nil {
		return nil, err
	}
	if err := store.RunMigrations(ctx); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func toOutbox(msg *psmtp.Message, raw []byte) outbox.Message {
	return outbox.Message{
		AccountID:    demoAccountID,
		EnvelopeFrom: msg.From.Email,
		Recipients:   msg.Recipients(),
		Raw:          raw,
	}
}

func printOutbox(ctx context.Context, queue *outbox.Queue) {
	rows, err := queue.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list outbox: %v\n", err)
		return
	}
	fmt.Println("--- outbox ---")
	for _, r := range rows {
		fmt.Printf("id=%d state=%-7s attempts=%d next=%s err=%q\n",
			r.ID, r.State, r.Attempts, r.NextAttemptAt.Format(time.RFC3339), r.LastError)
	}
}

// headerBlock returns the header section of a raw message, up to the first blank
// line, for eyeballing threading headers.
func headerBlock(raw []byte) string {
	s := string(raw)
	if before, _, ok := strings.Cut(s, "\r\n\r\n"); ok {
		return before
	}
	return s
}

// contentTypeOf returns the message's top-level Content-Type line for display.
func contentTypeOf(raw []byte) string {
	for line := range strings.SplitSeq(headerBlock(raw), "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			return strings.TrimSpace(line)
		}
	}
	return "content-type: unknown"
}
