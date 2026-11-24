package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/virustotal"
)

// VirusTotal settings keys. Everything here is off unless the user turns it on,
// and the api key itself lives in the keyring, not in these settings.
const (
	settingVTEnabled         = "virustotal_enabled"
	settingVTAutoLinks       = "virustotal_autoscan_links"
	settingVTAutoAttachments = "virustotal_autoscan_attachments"
)

const (
	// vtCacheTTL is how long a verdict is reused before asking again. The free
	// tier allows only a few hundred lookups a day, so re-opening a message
	// must not cost anything.
	vtCacheTTL = 7 * 24 * time.Hour

	// vtTimeout bounds a single lookup.
	vtTimeout = 15 * time.Second

	// vtMaxTargets caps one scan, so a message stuffed with links cannot drain
	// the daily quota in a single open.
	vtMaxTargets = 25
)

// errVTDisabled is returned when a scan is requested while the integration is
// off. The ui does not offer the action in that state, so this is a guard
// rather than something a user should normally see.
var errVTDisabled = errors.New("pelton: virustotal is not enabled")

// vtMu serializes scans. VirusTotal's free tier is rate limited per minute, and
// letting several messages scan at once turns a usable quota into a wall of
// 429s.
var vtMu sync.Mutex

// VirusTotalConfigDTO is the External-settings view of the integration. The api
// key is never sent back to the ui; HasAPIKey only reports whether one is
// stored, so the settings field can show a filled state without the secret
// leaving the keyring.
type VirusTotalConfigDTO struct {
	Enabled             bool `json:"enabled"`
	HasAPIKey           bool `json:"hasApiKey"`
	AutoScanLinks       bool `json:"autoScanLinks"`
	AutoScanAttachments bool `json:"autoScanAttachments"`
}

// VerdictDTO is one scan result for the ui. Status is "clean", "flagged" or
// "unknown"; Error is set instead when that particular lookup failed, so one
// rate-limited target does not discard the results either side of it.
type VerdictDTO struct {
	Status     string `json:"status"`
	Malicious  int    `json:"malicious"`
	Suspicious int    `json:"suspicious"`
	Total      int    `json:"total"`
	Permalink  string `json:"permalink"`
	Error      string `json:"error"`
}

// LinkVerdictDTO pairs a verdict with the link it belongs to.
type LinkVerdictDTO struct {
	URL     string     `json:"url"`
	Verdict VerdictDTO `json:"verdict"`
}

// AttachmentVerdictDTO pairs a verdict with the attachment it belongs to.
type AttachmentVerdictDTO struct {
	AttachmentID int64      `json:"attachmentId"`
	Filename     string     `json:"filename"`
	Verdict      VerdictDTO `json:"verdict"`
}

// MessageScanDTO is the result of scanning one message. Either list is empty
// when that target type was not requested.
type MessageScanDTO struct {
	Links       []LinkVerdictDTO       `json:"links"`
	Attachments []AttachmentVerdictDTO `json:"attachments"`
}

// GetVirusTotalConfig returns the current integration settings for the ui.
func (a *App) GetVirusTotalConfig() (VirusTotalConfigDTO, error) {
	if err := a.ready(); err != nil {
		return VirusTotalConfigDTO{}, err
	}
	key, err := credentials.LoadVirusTotalKey()
	if err != nil {
		return VirusTotalConfigDTO{}, err
	}
	return VirusTotalConfigDTO{
		Enabled:             a.boolSetting(settingVTEnabled, false),
		HasAPIKey:           key != "",
		AutoScanLinks:       a.boolSetting(settingVTAutoLinks, false),
		AutoScanAttachments: a.boolSetting(settingVTAutoAttachments, false),
	}, nil
}

// SetVirusTotalEnabled turns the integration on or off. Turning it off also
// discards every cached verdict, so the record of which links and files were
// looked up does not outlive the feature, and switches both auto-scan toggles
// back off so re-enabling never silently resumes scanning on its own.
func (a *App) SetVirusTotalEnabled(enabled bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingVTEnabled, strconv.FormatBool(enabled)); err != nil {
		return err
	}
	if enabled {
		return nil
	}
	if err := a.store.Set(a.ctx, settingVTAutoLinks, "false"); err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingVTAutoAttachments, "false"); err != nil {
		return err
	}
	return a.store.ClearVerdicts(a.ctx)
}

// SetVirusTotalAPIKey stores the api key in the os keyring, or clears it when
// empty. Clearing it also discards the cached verdicts it produced.
func (a *App) SetVirusTotalAPIKey(apiKey string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := credentials.StoreVirusTotalKey(apiKey); err != nil {
		return err
	}
	if apiKey == "" {
		return a.store.ClearVerdicts(a.ctx)
	}
	return nil
}

// SetVirusTotalAutoScanLinks turns automatic link scanning on or off. It is off
// by default: with it off, links are only ever scanned when the user asks.
func (a *App) SetVirusTotalAutoScanLinks(enabled bool) error {
	return a.setVTBool(settingVTAutoLinks, enabled)
}

// SetVirusTotalAutoScanAttachments turns automatic attachment scanning on or
// off. It is off by default.
func (a *App) SetVirusTotalAutoScanAttachments(enabled bool) error {
	return a.setVTBool(settingVTAutoAttachments, enabled)
}

func (a *App) setVTBool(key string, enabled bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.Set(a.ctx, key, strconv.FormatBool(enabled))
}

// ScanURL looks one link up on VirusTotal, answering from the local cache when
// it can. This is the on-demand path behind the link context menu, and works
// whenever the integration is enabled regardless of the auto-scan settings.
func (a *App) ScanURL(rawURL string) (VerdictDTO, error) {
	client, err := a.vtClient()
	if err != nil {
		return VerdictDTO{}, err
	}
	vtMu.Lock()
	defer vtMu.Unlock()
	return a.scanURL(a.ctx, client, rawURL), nil
}

// ScanAttachment looks one attachment up by the sha-256 of its stored bytes.
// The file itself is never sent anywhere; an attachment VirusTotal has not seen
// before comes back "unknown" rather than being uploaded.
func (a *App) ScanAttachment(messageID, attachmentID int64) (VerdictDTO, error) {
	client, err := a.vtClient()
	if err != nil {
		return VerdictDTO{}, err
	}
	atts, err := a.store.ListAttachments(a.ctx, messageID)
	if err != nil {
		return VerdictDTO{}, err
	}
	for _, att := range atts {
		if att.ID != attachmentID {
			continue
		}
		vtMu.Lock()
		defer vtMu.Unlock()
		return a.scanAttachment(a.ctx, client, att), nil
	}
	return VerdictDTO{}, fmt.Errorf("pelton: attachment %d not found on message %d", attachmentID, messageID)
}

// ScanMessage scans a whole message: every link in its body, every attachment,
// or both. The frontend passes what it wants, which is how the two auto-scan
// settings and the manual "scan this message" action share one path. Results
// come back per target, so a single failure does not lose the rest.
func (a *App) ScanMessage(messageID int64, links, attachments bool) (MessageScanDTO, error) {
	out := MessageScanDTO{Links: []LinkVerdictDTO{}, Attachments: []AttachmentVerdictDTO{}}
	if !links && !attachments {
		return out, nil
	}

	client, err := a.vtClient()
	if err != nil {
		return out, err
	}

	msg, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return out, err
	}

	vtMu.Lock()
	defer vtMu.Unlock()

	budget := vtMaxTargets
	if links {
		for _, url := range mailview.Links(msg.BodyHTML, msg.BodyPlain) {
			if budget == 0 {
				break
			}
			budget--
			out.Links = append(out.Links, LinkVerdictDTO{URL: url, Verdict: a.scanURL(a.ctx, client, url)})
		}
	}
	if attachments {
		atts, attErr := a.store.ListAttachments(a.ctx, messageID)
		if attErr != nil {
			return out, attErr
		}
		for _, att := range atts {
			if budget == 0 {
				break
			}
			budget--
			out.Attachments = append(out.Attachments, AttachmentVerdictDTO{
				AttachmentID: att.ID,
				Filename:     att.Filename,
				Verdict:      a.scanAttachment(a.ctx, client, att),
			})
		}
	}
	return out, nil
}

// vtClient builds a lookup client, refusing when the integration is off or no
// key is stored. Building it per call keeps it in step with a settings change
// without any cached state to invalidate.
func (a *App) vtClient() (*virustotal.Client, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if !a.boolSetting(settingVTEnabled, false) {
		return nil, errVTDisabled
	}
	key, err := credentials.LoadVirusTotalKey()
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, virustotal.ErrNoAPIKey
	}
	return virustotal.New(a.httpClient(vtTimeout), key), nil
}

// scanURL answers from the cache, or looks the url up and caches the result.
func (a *App) scanURL(ctx context.Context, client *virustotal.Client, rawURL string) VerdictDTO {
	if cached, err := a.store.CachedVerdict(ctx, storage.VerdictKindURL, rawURL, vtCacheTTL); err == nil {
		return verdictFromCache(cached)
	}
	verdict, err := client.LookupURL(ctx, rawURL)
	if err != nil {
		return VerdictDTO{Status: string(virustotal.StatusUnknown), Error: vtErrorMessage(err)}
	}
	a.cacheVerdict(ctx, storage.VerdictKindURL, rawURL, verdict)
	return verdictFromLookup(verdict)
}

// scanAttachment hashes the stored file and looks that digest up. A file whose
// bytes cannot be read is reported as an error on that one attachment.
func (a *App) scanAttachment(ctx context.Context, client *virustotal.Client, att storage.Attachment) VerdictDTO {
	digest, err := a.attachmentDigest(att)
	if err != nil {
		return VerdictDTO{Status: string(virustotal.StatusUnknown), Error: err.Error()}
	}
	if cached, cErr := a.store.CachedVerdict(ctx, storage.VerdictKindFile, digest, vtCacheTTL); cErr == nil {
		return verdictFromCache(cached)
	}
	verdict, err := client.LookupFile(ctx, digest)
	if err != nil {
		return VerdictDTO{Status: string(virustotal.StatusUnknown), Error: vtErrorMessage(err)}
	}
	a.cacheVerdict(ctx, storage.VerdictKindFile, digest, verdict)
	return verdictFromLookup(verdict)
}

// attachmentDigest is the sha-256 of an attachment's stored bytes, streamed so
// a large file is never held in memory.
func (a *App) attachmentDigest(att storage.Attachment) (string, error) {
	f, err := a.store.OpenAttachment(a.ctx, att)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", fmt.Errorf("pelton: hash attachment %q: %w", att.Filename, err)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// cacheVerdict stores a fresh lookup. A cache write failure only costs the next
// lookup its shortcut, so it is logged rather than surfaced.
func (a *App) cacheVerdict(ctx context.Context, kind storage.VerdictKind, target string, v virustotal.Verdict) {
	err := a.store.CacheVerdict(ctx, kind, target, storage.Verdict{
		Status:     string(v.Status),
		Malicious:  v.Malicious,
		Suspicious: v.Suspicious,
		Total:      v.Total,
		Permalink:  v.Permalink,
	})
	if err != nil {
		a.log.Warn("cache virustotal verdict", "err", err)
	}
}

func verdictFromLookup(v virustotal.Verdict) VerdictDTO {
	return VerdictDTO{
		Status:     string(v.Status),
		Malicious:  v.Malicious,
		Suspicious: v.Suspicious,
		Total:      v.Total,
		Permalink:  v.Permalink,
	}
}

func verdictFromCache(v *storage.Verdict) VerdictDTO {
	return VerdictDTO{
		Status:     v.Status,
		Malicious:  v.Malicious,
		Suspicious: v.Suspicious,
		Total:      v.Total,
		Permalink:  v.Permalink,
	}
}

// vtErrorMessage reduces the client's sentinel errors to stable codes the ui
// localizes. Anything else passes its own text through, which the ui shows
// verbatim rather than guessing at a translation.
func vtErrorMessage(err error) string {
	switch {
	case errors.Is(err, virustotal.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, virustotal.ErrUnauthorized):
		return "unauthorized"
	default:
		return err.Error()
	}
}
