package desktop

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Unsubscribe support (#71). Preference order per the List-Unsubscribe headers
// stored at sync time:
//   - RFC 8058 one-click (https target + List-Unsubscribe-Post): a background
//     POST, fired here, only ever on explicit user action.
//   - mailto target: an unsubscribe mail sent through the account's own SMTP
//     via the outbox.
//   - plain http(s) target: the frontend opens it in the system browser
//     (auto-POSTing arbitrary URLs without RFC 8058 would be unsafe).

// unsubscribe kinds as exposed to the frontend.
const (
	unsubKindOneClick = "oneclick"
	unsubKindMailto   = "mailto"
	unsubKindLink     = "link"
)

// settingUnsubscribed holds a json map of sender address -> RFC3339 time of a
// carried-out unsubscribe, so the button can show "already unsubscribed".
const settingUnsubscribed = "unsubscribed_senders"

// UnsubscribeDTO describes the unsubscribe mechanism a message advertises.
type UnsubscribeDTO struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	// Done is true when this sender was already unsubscribed from.
	Done bool `json:"done"`
	// Unverified marks a one-click header that authentication does not vouch
	// for, offered as a web page to open instead (new design only).
	Unverified bool `json:"unverified"`
}

// newDesign reports whether the new design is on. The classic design keeps
// its unsubscribe behaviour as it was.
func (a *App) newDesign() bool {
	return a.stringSetting(settingDesign, "new") != "classic"
}

// oneClickVerified reports whether a message's one-click unsubscribe can be
// trusted, as RFC 8058 section 4 asks: the user's own server recorded a
// passing DKIM signature for a domain whose signature covers both
// List-Unsubscribe and List-Unsubscribe-Post.
func (a *App) oneClickVerified(id int64) bool {
	trust, err := a.store.GetMessageTrust(a.ctx, id)
	if err != nil || trust.AuthState != phishing.AuthTrusted {
		return false
	}
	for _, d := range trust.DKIMDomains {
		if slices.Contains(trust.UnsubSigners, d) {
			return true
		}
	}
	return false
}

// unsubEntry matches one <...> entry of a List-Unsubscribe header.
var unsubEntry = regexp.MustCompile(`<([^>]+)>`)

// parseListUnsubscribe picks the preferred mechanism from a raw header value.
// It returns an empty kind when the header offers nothing usable.
func parseListUnsubscribe(header string, oneClickPost bool) (kind, target string) {
	var mailto, httpsURL, httpURL string
	for _, m := range unsubEntry.FindAllStringSubmatch(header, -1) {
		entry := strings.TrimSpace(m[1])
		switch {
		case strings.HasPrefix(strings.ToLower(entry), "mailto:") && mailto == "":
			mailto = entry
		case strings.HasPrefix(strings.ToLower(entry), "https://") && httpsURL == "":
			httpsURL = entry
		case strings.HasPrefix(strings.ToLower(entry), "http://") && httpURL == "":
			httpURL = entry
		}
	}
	// one-click requires https per RFC 8058.
	if oneClickPost && httpsURL != "" {
		return unsubKindOneClick, httpsURL
	}
	if mailto != "" {
		return unsubKindMailto, mailto
	}
	if httpsURL != "" {
		return unsubKindLink, httpsURL
	}
	if httpURL != "" {
		return unsubKindLink, httpURL
	}
	return "", ""
}

// unsubscribeInfo builds the DTO for a message, or nil when it advertises no
// unsubscribe mechanism (including rows cached before the header column
// existed; the frontend still falls back to scanning the body for a link).
func (a *App) unsubscribeInfo(m *storage.Message) *UnsubscribeDTO {
	kind, target := parseListUnsubscribe(m.ListUnsubscribe, m.ListUnsubscribePost)
	if kind == "" {
		return nil
	}
	dto := &UnsubscribeDTO{Kind: kind, Target: target, Done: a.unsubscribedAt(m.FromAddress) != ""}
	if kind == unsubKindOneClick && a.newDesign() && !a.oneClickVerified(m.ID) {
		dto.Kind, dto.Unverified = unsubKindLink, true
	}
	return dto
}

// Unsubscribe carries out a message's unsubscribe mechanism: the one-click
// POST or the mailto send. Plain-link targets are opened by the frontend in
// the system browser instead, so calling this for one is an error. On success
// the sender is remembered so the button relabels.
func (a *App) Unsubscribe(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	kind, target := parseListUnsubscribe(m.ListUnsubscribe, m.ListUnsubscribePost)
	switch kind {
	case unsubKindOneClick:
		if a.newDesign() && !a.oneClickVerified(m.ID) {
			return errUnverifiedUnsubscribe
		}
		if err := postOneClick(a.oneClickClient(), target); err != nil {
			return err
		}
	case unsubKindMailto:
		if err := a.sendUnsubscribeMail(m.AccountID, target); err != nil {
			return err
		}
	default:
		return fmt.Errorf("pelton: message offers no unsubscribe action")
	}
	a.markUnsubscribed(m.FromAddress)
	a.recordUnsubscribe(m, kind)
	return nil
}

// errUnverifiedUnsubscribe refuses a one-click request authentication does
// not vouch for.
var errUnverifiedUnsubscribe = errors.New("pelton: this unsubscribe header could not be verified, so it is opened as a web page instead")

// NoteUnsubscribeLink records that the user opened a message's unsubscribe
// web page, so mail that keeps coming afterwards is noticed like after any
// other unsubscribe. Whether the page worked is not known here.
func (a *App) NoteUnsubscribeLink(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	a.recordUnsubscribe(m, unsubKindLink)
	return nil
}

// recordUnsubscribe starts watching for mail that keeps arriving.
func (a *App) recordUnsubscribe(m *storage.Message, method string) {
	listID, err := a.store.MessageListID(a.ctx, m.ID)
	if err != nil {
		a.log.Error("record unsubscribe: read list id", "err", err)
	}
	u := storage.Unsubscribe{Sender: bareAddress(m.FromAddress), ListID: listID, AccountID: m.AccountID, Method: method, At: time.Now()}
	if err := a.store.RecordUnsubscribe(a.ctx, u); err != nil {
		a.log.Error("record unsubscribe", "err", err)
		return
	}
	a.emit(EventPrivacyChanged, nil)
}

// unsubscribeTransport, when set, carries the one-click request in place of
// the app's own transport. Tests point it at a local server, or at one that
// fails the test when anything is sent.
var unsubscribeTransport http.RoundTripper

// maxUnsubscribeRedirects bounds the redirects a one-click request follows.
const maxUnsubscribeRedirects = 5

// oneClickClient is the app's own client, through the proxy setting, with
// redirects limited to the host the header named.
func (a *App) oneClickClient() *http.Client {
	c := a.httpClient(15 * time.Second)
	if unsubscribeTransport != nil {
		c.Transport = unsubscribeTransport
	}
	c.CheckRedirect = sameHostRedirect
	return c
}

// sameHostRedirect lets a one-click request follow a redirect only to the same
// host over https: the header named one address, and a redirect elsewhere
// would send the request somewhere the user never saw.
func sameHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxUnsubscribeRedirects {
		return fmt.Errorf("pelton: unsubscribe redirected too often")
	}
	if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return fmt.Errorf("pelton: unsubscribe redirected to another address (%s), which was refused", req.URL.Host)
	}
	return nil
}

// postOneClick fires the RFC 8058 one-click POST. The fixed form body is the
// whole protocol; a 2xx counts as done.
func postOneClick(client *http.Client, target string) error {
	if u, err := url.Parse(target); err != nil || u.Scheme != "https" {
		return fmt.Errorf("pelton: a one-click unsubscribe needs an https address")
	}
	resp, err := client.Post(target, "application/x-www-form-urlencoded",
		strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		return fmt.Errorf("pelton: unsubscribe request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("pelton: unsubscribe endpoint answered %s", resp.Status)
	}
	return nil
}

// sendUnsubscribeMail queues the unsubscribe mail through the account's own
// smtp via the outbox, so it gets the same durability and retries as any send.
func (a *App) sendUnsubscribeMail(accountID int64, target string) error {
	u, err := url.Parse(target)
	if err != nil || u.Opaque == "" {
		return fmt.Errorf("pelton: invalid unsubscribe mailto target")
	}
	acc, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}
	q := u.Query()
	subject := q.Get("subject")
	if subject == "" {
		subject = "unsubscribe"
	}
	body := q.Get("body")
	if body == "" {
		body = "unsubscribe"
	}
	msg := &smtp.Message{
		From:    smtp.Address{Name: acc.DisplayName, Email: acc.Email},
		To:      []smtp.Address{{Email: u.Opaque}},
		Subject: subject,
		Text:    body,
	}
	_, err = smtp.Enqueue(a.ctx, a.queue, accountID, msg, nil, crypto.ModeNone, crypto.Options{}, time.Time{})
	return err
}

// unsubscribedAt returns when the sender was unsubscribed from, or "".
func (a *App) unsubscribedAt(sender string) string {
	var done map[string]string
	if err := a.store.GetJSON(a.ctx, settingUnsubscribed, &done); err != nil {
		return ""
	}
	return done[strings.ToLower(sender)]
}

// markUnsubscribed records a carried-out unsubscribe for the sender. Failures
// only cost the "already unsubscribed" label, so they are logged, not fatal.
func (a *App) markUnsubscribed(sender string) {
	var done map[string]string
	if err := a.store.GetJSON(a.ctx, settingUnsubscribed, &done); err != nil || done == nil {
		done = map[string]string{}
	}
	done[strings.ToLower(sender)] = time.Now().UTC().Format(time.RFC3339)
	if err := a.store.SetJSON(a.ctx, settingUnsubscribed, done); err != nil {
		a.log.Error("record unsubscribe", "sender", sender, "err", err)
	}
}
