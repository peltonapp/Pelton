package desktop

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/certtrust"
	"github.com/peltonapp/Pelton/internal/credentials"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

// submissionServer is enough of a submission server to carry one message:
// greeting, EHLO, AUTH, MAIL, RCPT, DATA, QUIT. It runs on implicit TLS with
// httptest's self-signed certificate, and returns its port and the certificate
// fingerprint so the account under test can pin it.
//
// The whole point of these tests is the step that happens after a send
// succeeds, so the send has to actually succeed.
func submissionServer(t *testing.T) (port int, fingerprint string) {
	t.Helper()
	certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certServer.Close()
	tlsConfig := certServer.TLS.Clone()
	tlsConfig.NextProtos = nil

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go speakSubmission(conn)
		}
	}()

	_, p, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(p)
	return n, certtrust.Fingerprint(certServer.Certificate())
}

func speakSubmission(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }

	write("220 localhost ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			write("250-localhost")
			write("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			write("235 2.7.0 authenticated")
		case strings.HasPrefix(cmd, "DATA"):
			write("354 go ahead")
			// read to the end-of-data marker and accept whatever arrived.
			for {
				body, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(body) == "." {
					break
				}
			}
			write("250 2.0.0 queued")
		case strings.HasPrefix(cmd, "QUIT"):
			write("221 bye")
			return
		default:
			write("250 2.0.0 ok")
		}
	}
}

// sendingAccount builds an app and an account whose smtp points at a live test
// server, with the server's certificate pinned and a password stored, and
// points the imap side at client.
func sendingAccount(t *testing.T, client *fakeIMAP) (*App, storage.Account) {
	t.Helper()
	ctx, stopBackground := testContext(t)
	db, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	t.Cleanup(stopBackground)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	port, fingerprint := submissionServer(t)
	account := storage.Account{
		Email:        "me@example.com",
		Username:     "me@example.com",
		SMTPHost:     "127.0.0.1",
		SMTPPort:     port,
		SMTPTLS:      "ssl",
		IMAPHost:     "127.0.0.1",
		IMAPPort:     993,
		IMAPTLS:      "ssl",
		TrustedCerts: []string{fingerprint},
	}
	id, err := db.CreateAccount(ctx, &account)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	account.ID = id

	if err := credentials.Store(id, credentials.Secret{
		Method:   credentials.MethodPassword,
		Password: "hunter2",
	}); err != nil {
		t.Fatalf("store credentials: %v", err)
	}
	t.Cleanup(func() { _ = credentials.Delete(id) })

	app := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}
	app.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	return app, account
}

// The bug this exists for (#451): the append-to-Sent step was wired through an
// option the one place that builds the sender never passed, so every message
// sent correctly and no copy was ever written. Nothing failed, because the
// missing appender is a debug log and a send that returns nil.
//
// So the assertion is on the transmit path as the outbox worker drives it, not
// on the helper underneath. A test of the helper alone would still have passed
// the whole time the feature was dead.
func TestTransmitAppendsACopyToSent(t *testing.T) {
	client := &fakeIMAP{}
	app, account := sendingAccount(t, client)

	raw := []byte("From: me@example.com\r\nTo: you@example.com\r\nSubject: hi\r\n\r\nbody\r\n")
	transmitter := &accountTransmitter{app: app}
	err := transmitter.Transmit(context.Background(), outbox.Message{
		AccountID:    account.ID,
		EnvelopeFrom: "me@example.com",
		Recipients:   []string{"you@example.com"},
		Raw:          raw,
	})
	if err != nil {
		t.Fatalf("Transmit: %v", err)
	}

	if len(client.appended) != 1 {
		t.Fatalf("appended %d messages to Sent, want 1", len(client.appended))
	}
	if string(client.appended[0]) != string(raw) {
		t.Errorf("appended %q, want the message that was sent", client.appended[0])
	}
	// The append ran on a pooled session; stopping the runtime logs it out.
	app.stopAccountScheduler(account.ID, false)
	app.accountSessionsFor(account.ID, 0).sessions.WaitClosed(context.Background()) // closes run in the background
	if !client.loggedOut.Load() {
		t.Error("the pooled imap session was left open after the runtime stopped")
	}
}

// The message has already left the building by the time the append runs, so a
// server that refuses it must not turn a delivered message into a failed one.
// The copy is lost, which is worth a log, but reporting failure would have the
// outbox retry and send it twice.
func TestTransmitSucceedsWhenTheSentCopyFails(t *testing.T) {
	client := &fakeIMAP{failAppend: errors.New("permission denied")}
	app, account := sendingAccount(t, client)

	transmitter := &accountTransmitter{app: app}
	err := transmitter.Transmit(context.Background(), outbox.Message{
		AccountID:    account.ID,
		EnvelopeFrom: "me@example.com",
		Recipients:   []string{"you@example.com"},
		Raw:          []byte("Subject: hi\r\n\r\nbody\r\n"),
	})
	if err != nil {
		t.Errorf("Transmit reported failure for a message that was sent: %v", err)
	}
}
