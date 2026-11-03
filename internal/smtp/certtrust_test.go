package smtp

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/certtrust"
)

// selfSignedSubmission starts an implicit-TLS submission server with httptest's
// self-signed certificate. serve handles each connection; serveSubmission
// answers just enough (greeting, EHLO, QUIT) for Dial and Close. It returns
// the port and the certificate's fingerprint.
func selfSignedSubmission(t *testing.T, serve func(net.Conn)) (int, string) {
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
			go serve(conn)
		}
	}()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(port)
	return n, certtrust.Fingerprint(certServer.Certificate())
}

func serveSubmission(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	if _, err := conn.Write([]byte("220 test ready\r\n")); err != nil {
		return
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			_, _ = conn.Write([]byte("250 test\r\n"))
		case strings.HasPrefix(cmd, "QUIT"):
			_, _ = conn.Write([]byte("221 bye\r\n"))
			return
		default:
			_, _ = conn.Write([]byte("502 not here\r\n"))
		}
	}
}

// The dial errors were formatted with %v, which dropped the tls error: the app
// could say "connection failed" but not that it was the certificate, so the
// trust prompt never had anything to show (#446).
func TestDialReportsAnUntrustedCertificate(t *testing.T) {
	port, fp := selfSignedSubmission(t, serveSubmission)

	_, err := Dial(Config{Host: "127.0.0.1", Port: port, TLS: TLSImplicit})
	if !errors.Is(err, ErrConnect) {
		t.Errorf("Dial() error = %v, want ErrConnect", err)
	}
	cert, ok := certtrust.Untrusted(err)
	if !ok {
		t.Fatalf("Dial() error = %v, want an untrusted certificate", err)
	}
	if certtrust.Fingerprint(cert) != fp {
		t.Errorf("reported fingerprint %s, want %s", certtrust.Fingerprint(cert), fp)
	}
}

func TestDialAcceptsAPinnedCertificate(t *testing.T) {
	port, fp := selfSignedSubmission(t, serveSubmission)

	client, err := Dial(Config{Host: "127.0.0.1", Port: port, TLS: TLSImplicit, Trust: certtrust.Trust{Pins: []string{fp}}})
	if err != nil {
		t.Fatalf("Dial() with the pin: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
