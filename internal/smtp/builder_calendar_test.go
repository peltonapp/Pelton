package smtp

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/crypto"
)

// An invitation reply has to arrive as a text/calendar alternative with its
// method, or the organizer's calendar will not apply it.
func TestBuildCalendarReplyPart(t *testing.T) {
	msg := baseMessage()
	ics := "BEGIN:VCALENDAR\r\nMETHOD:REPLY\r\nEND:VCALENDAR\r\n"
	msg.Calendar = &CalendarPart{Method: "reply", Data: []byte(ics)}
	raw, err := BuildRaw(msg, nil, crypto.ModeNone, crypto.Options{})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("top content type = %q, %v", mediaType, err)
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	var types []string
	var calendar string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, part.Header.Get("Content-Type"))
		if strings.HasPrefix(part.Header.Get("Content-Type"), "text/calendar") {
			body, _ := io.ReadAll(part)
			decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(body), "\r\n", ""))
			if err != nil {
				t.Fatal(err)
			}
			calendar = string(decoded)
		}
	}
	if len(types) != 2 || types[1] != "text/calendar; charset=utf-8; method=REPLY" {
		t.Errorf("parts = %v", types)
	}
	if calendar != ics {
		t.Errorf("calendar = %q", calendar)
	}
}
