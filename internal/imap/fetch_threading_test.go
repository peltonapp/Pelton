package imap

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestFetchThreadingMetadata(t *testing.T) {
	for _, tt := range []struct {
		name       string
		headers    string
		inReplyTo  []string
		references []string
	}{
		{
			name: "folded references and multiple parents",
			headers: "In-Reply-To: <Parent@example.com> <other@example.com> <Parent@example.com>\r\n" +
				"References: <root@example.com>\r\n\t<older@example.com> <root@example.com>\r\n",
			inReplyTo:  []string{"Parent@example.com", "other@example.com", "Parent@example.com"},
			references: []string{"root@example.com", "older@example.com", "root@example.com"},
		},
		{
			name:       "absent ancestry is known empty",
			inReplyTo:  []string{},
			references: []string{},
		},
		{
			name: "bad ancestry leaves readable content",
			headers: "In-Reply-To: <valid@example.com> <invalid>\r\n" +
				"References: <invalid>\r\n",
			inReplyTo:  []string{},
			references: []string{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, wire := newTestServer(t, testOptions{})
			raw := []byte("From: Sender <sender@example.com>\r\n" +
				"To: receiver@another-account.example\r\n" +
				"Subject: Thread ancestry\r\n" +
				"Message-ID: <Message@independent.example>\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n" +
				tt.headers + "\r\nReadable body.\r\n")
			appendCmd := c.raw.Append("INBOX", int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagFlagged}})
			if _, err := appendCmd.Write(raw); err != nil {
				t.Fatalf("write APPEND: %v", err)
			}
			if err := appendCmd.Close(); err != nil {
				t.Fatalf("close APPEND: %v", err)
			}
			if _, err := appendCmd.Wait(); err != nil {
				t.Fatalf("APPEND: %v", err)
			}
			if _, err := c.Select("INBOX"); err != nil {
				t.Fatalf("select: %v", err)
			}
			initial, err := c.FetchAllFlags()
			if err != nil || len(initial) != 1 {
				t.Fatalf("initial flags = %v, %v; want one message", initial, err)
			}
			uid := initial[0].UID
			if !slices.Equal(initial[0].Flags, []imap.Flag{imap.FlagFlagged}) {
				t.Fatalf("initial flags = %v, want [\\Flagged]", initial[0].Flags)
			}
			for _, method := range []string{"single", "batch"} {
				t.Run(method, func(t *testing.T) {
					wire.reset()
					var msg *Message
					if method == "single" {
						msg, err = c.FetchMessage(uid)
					} else {
						calls := 0
						err = c.FetchMessages([]imap.UID{uid}, func(gotUID imap.UID, got *Message, parseErr error) error {
							calls++
							if gotUID != uid {
								t.Errorf("callback UID = %d, want %d", gotUID, uid)
							}
							msg = got
							return parseErr
						})
						if calls != 1 {
							t.Errorf("batch callbacks = %d, want 1", calls)
						}
					}
					if err != nil || msg == nil {
						t.Fatalf("fetch = %v, %v; want readable message", msg, err)
					}
					if msg.InReplyTo == nil || !slices.Equal(msg.InReplyTo, tt.inReplyTo) {
						t.Errorf("InReplyTo = %#v, want %#v", msg.InReplyTo, tt.inReplyTo)
					}
					if msg.References == nil || !slices.Equal(msg.References, tt.references) {
						t.Errorf("References = %#v, want %#v", msg.References, tt.references)
					}
					if msg.UID != uid || msg.MessageID != "Message@independent.example" || msg.Subject != "Thread ancestry" {
						t.Errorf("envelope changed: UID=%d MessageID=%q Subject=%q", msg.UID, msg.MessageID, msg.Subject)
					}
					if msg.Text != "Readable body.\r\n" || msg.HTML != "" {
						t.Errorf("body = %q, HTML = %q", msg.Text, msg.HTML)
					}
					if !bytes.Equal(msg.Raw, raw) || msg.Size != int64(len(raw)) {
						t.Error("fetch changed the raw source or its size")
					}
					if !slices.Equal(msg.Flags, initial[0].Flags) {
						t.Errorf("message flags = %v, want %v", msg.Flags, initial[0].Flags)
					}
					assertThreadingFetchCommand(t, wire)
					after, err := c.FetchAllFlags()
					if err != nil || len(after) != 1 {
						t.Fatalf("flags after fetch = %v, %v", after, err)
					}
					if !slices.Equal(after[0].Flags, initial[0].Flags) {
						t.Errorf("server flags after fetch = %v, want %v", after[0].Flags, initial[0].Flags)
					}
				})
			}
		})
	}
}

func assertThreadingFetchCommand(t *testing.T, wire *wireLog) {
	t.Helper()
	var fetches []string
	for _, cmd := range wire.commands() {
		if strings.HasPrefix(cmd, "UID FETCH ") || strings.HasPrefix(cmd, "FETCH ") {
			fetches = append(fetches, cmd)
		}
		if strings.HasPrefix(cmd, "UID STORE ") || strings.HasPrefix(cmd, "STORE ") {
			t.Errorf("fetch sent a STORE command: %s", cmd)
		}
	}
	if len(fetches) != 1 || !strings.HasPrefix(fetches[0], "UID FETCH ") {
		t.Fatalf("FETCH commands = %v, want exactly one UID FETCH\n%s", fetches, wire.String())
	}
	cmd := fetches[0]
	items := strings.Fields(strings.NewReplacer("(", " ", ")", " ").Replace(cmd))
	if !slices.Contains(items, "ENVELOPE") || !slices.Contains(items, "FLAGS") || strings.Count(strings.Join(items, " "), "UID") != 2 {
		t.Errorf("FETCH must request ENVELOPE, FLAGS and UID: %s", cmd)
	}
	if strings.Count(cmd, "BODY.PEEK[]") != 1 || strings.Count(cmd, "BODY") != 1 || strings.Contains(cmd, "HEADER.FIELDS") {
		t.Errorf("FETCH must request only one full BODY.PEEK[] section: %s", cmd)
	}
}

func TestBodyFetchOptionsUsesExistingPeek(t *testing.T) {
	section, options := bodyFetchOptions()
	if !options.Envelope || !options.Flags || !options.UID {
		t.Errorf("missing envelope, flags or UID: %+v", options)
	}
	if len(options.BodySection) != 1 || options.BodySection[0] != section {
		t.Fatalf("body sections = %v, want the single returned section", options.BodySection)
	}
	if !section.Peek || section.Specifier != "" || len(section.Part) != 0 || section.Partial != nil || len(section.HeaderFields) != 0 || len(section.HeaderFieldsNot) != 0 {
		t.Errorf("want full BODY.PEEK[] without header selection: %+v", section)
	}
}
