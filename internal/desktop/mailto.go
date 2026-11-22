package desktop

import (
	"net/url"
	"strings"
	"sync"
)

// MailtoDraft is a compose prefill parsed from a mailto: URL. Address fields are
// comma-joined so they drop straight into the compose panes' raw recipient
// inputs.
type MailtoDraft struct {
	To      string `json:"to"`
	Cc      string `json:"cc"`
	Bcc     string `json:"bcc"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// PendingMailtoDTO is what the frontend polls on startup: a draft plus whether
// the launch actually carried one. Present is false on a normal launch.
type PendingMailtoDTO struct {
	Present bool        `json:"present"`
	Draft   MailtoDraft `json:"draft"`
}

// parseMailto reads a mailto: URL per RFC 6068 into a compose prefill. It never
// fails: a malformed URL yields an empty draft (which opens a blank compose)
// rather than an error. Per RFC 6068 the encoding is plain percent-encoding, so
// "+" is a literal plus, not a space (that is form-query semantics, which
// mailto does not use); spaces arrive as %20.
func parseMailto(raw string) MailtoDraft {
	raw = strings.TrimSpace(raw)
	// tolerate any capitalization of the scheme and a stray leading slash form
	// ("mailto://") some senders emit.
	if len(raw) < len("mailto:") || !strings.EqualFold(raw[:len("mailto:")], "mailto:") {
		return MailtoDraft{}
	}
	rest := strings.TrimPrefix(raw[len("mailto:"):], "//")

	path := rest
	query := ""
	if before, after, ok := strings.Cut(rest, "?"); ok {
		path = before
		query = after
	}

	var draft MailtoDraft
	tos := decodeAddressList(path)

	for pair := range strings.SplitSeq(query, "&") {
		if pair == "" {
			continue
		}
		key, value := pair, ""
		if before, after, ok := strings.Cut(pair, "="); ok {
			key, value = before, after
		}
		// RFC 6068 field names are case-insensitive; the value keeps its own
		// case once decoded.
		decoded := unescape(value)
		switch strings.ToLower(unescape(key)) {
		case "to":
			tos = append(tos, splitAddresses(decoded)...)
		case "cc":
			draft.Cc = joinCSV(draft.Cc, decoded)
		case "bcc":
			draft.Bcc = joinCSV(draft.Bcc, decoded)
		case "subject":
			draft.Subject = decoded
		case "body":
			draft.Body = decoded
		default:
			// unknown headers are ignored rather than treated as an error.
		}
	}

	draft.To = strings.Join(tos, ", ")
	return draft
}

// decodeAddressList decodes the comma-separated to-list from a mailto path.
func decodeAddressList(path string) []string {
	if path == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(path, ",") {
		if a := strings.TrimSpace(unescape(part)); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// splitAddresses splits an already-decoded comma-separated address string.
func splitAddresses(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if a := strings.TrimSpace(part); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// joinCSV appends b to a as a comma-separated list, skipping empties.
func joinCSV(a, b string) string {
	b = strings.TrimSpace(b)
	if b == "" {
		return a
	}
	if a == "" {
		return b
	}
	return a + ", " + b
}

// unescape percent-decodes an RFC 6068 component. PathUnescape (not
// QueryUnescape) is used on purpose so "+" stays a literal plus. A value that
// fails to decode is passed through unchanged rather than dropped.
func unescape(s string) string {
	if decoded, err := url.PathUnescape(s); err == nil {
		return decoded
	}
	return s
}

// firstMailtoArg returns the first mailto: URL in a launch argument list, or ""
// when there is none. It is how a mailto click reaches the app on Linux and
// Windows (the URL arrives as argv).
func firstMailtoArg(args []string) string {
	for _, a := range args {
		if len(a) >= len("mailto:") && strings.EqualFold(a[:len("mailto:")], "mailto:") {
			return a
		}
	}
	return ""
}

// mailtoState holds the not-yet-consumed launch draft. A mailto that arrives
// before the webview is up (app launched by a mailto click) is stashed here and
// the frontend polls it via ConsumePendingMailto once mounted; a mailto that
// arrives while the app is already running is emitted straight to the frontend.
type mailtoState struct {
	mu      sync.Mutex
	pending *MailtoDraft
}

// deliverMailto routes a freshly received mailto: URL. When the frontend is
// live it emits the compose event immediately; otherwise it stashes the draft
// for the frontend to pick up on startup.
func (a *App) deliverMailto(raw string) {
	draft := parseMailto(raw)
	if a.ctx != nil {
		a.emit(EventMailtoCompose, draft)
		return
	}
	a.mailto.mu.Lock()
	a.mailto.pending = &draft
	a.mailto.mu.Unlock()
}

// setPendingMailto stashes a launch draft before wails starts (from os.Args),
// so the very first webview mount can open it.
func (a *App) setPendingMailto(draft MailtoDraft) {
	a.mailto.mu.Lock()
	a.mailto.pending = &draft
	a.mailto.mu.Unlock()
}

// ConsumePendingMailto returns the draft the app was launched with (if any) and
// clears it, so a later reload does not reopen the same compose.
func (a *App) ConsumePendingMailto() PendingMailtoDTO {
	a.mailto.mu.Lock()
	defer a.mailto.mu.Unlock()
	if a.mailto.pending == nil {
		return PendingMailtoDTO{}
	}
	draft := *a.mailto.pending
	a.mailto.pending = nil
	return PendingMailtoDTO{Present: true, Draft: draft}
}
