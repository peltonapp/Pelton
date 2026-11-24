package smtp

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// stallingServer accepts a message up to DATA, then stalls before answering
// 354 and reports whether the message was ever finished with its final dot.
func stallingServer(inData chan<- struct{}, finished chan<- bool) func(net.Conn) {
	return func(conn net.Conn) {
		defer conn.Close()
		r := bufio.NewReader(conn)
		reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		reply("220 test ready")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				finished <- false
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				reply("250 test")
			case strings.HasPrefix(cmd, "DATA"):
				close(inData)
				// no 354: the client is left waiting until it gives up.
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						finished <- false
						return
					}
					if line == ".\r\n" {
						finished <- true
						return
					}
				}
			default:
				reply("250 ok")
			}
		}
	}
}

// Cancelling a send before the message is committed must drop the connection
// rather than finish DATA, so the server never takes the message.
func TestSendCancelledBeforeCommitDropsTheMessage(t *testing.T) {
	inData := make(chan struct{})
	finished := make(chan bool, 1)
	port, _ := selfSignedSubmission(t, stallingServer(inData, finished))
	client, err := Dial(Config{Host: "127.0.0.1", Port: port, TLS: TLSImplicit, InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sent := make(chan error, 1)
	go func() {
		sent <- client.Send(ctx, "me@example.com", []string{"you@example.com"}, []byte("Subject: hi\r\n\r\nbody\r\n"))
	}()
	select {
	case <-inData:
	case <-time.After(5 * time.Second):
		t.Fatal("the client never reached DATA")
	}
	cancel()

	select {
	case err := <-sent:
		if err == nil {
			t.Fatal("a cancelled send reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not end the send")
	}
	select {
	case got := <-finished:
		if got {
			t.Fatal("the server received the whole message after the send was cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server never saw the connection end")
	}
}
