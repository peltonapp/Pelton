package desktop

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
)

// sender photos. avatars are colored initials by default; the user can opt to
// show the sender's real picture sourced either from BIMI (the sender domain's
// own verified logo, published in dns) or from Gravatar (keyed off the email).
// resolving a photo is a network operation, so it only happens when the user has
// chosen a photo source. bimi lookups are cached per domain since most messages
// come from a handful of domains. the setting key and default live in
// bind_settings.go alongside the other preferences.

// bimiCache memoizes domain -> logo url ("" means none) for the process lifetime.
var bimiCache sync.Map

// SenderPhotos returns the ordered list of remote photo candidates for a sender
// under the configured source preference. The frontend tries each <img> in turn
// and, when they all fail (or the list is empty), draws a generated placeholder
// ("pfp"). The order encodes the fallback chain:
//   - bimi_gravatar: sender logo first, then Gravatar
//   - gravatar_bimi: Gravatar first, then sender logo
//   - pfp:           no network; always the generated placeholder
func (a *App) SenderPhotos(email string) ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	source := a.stringSetting(settingAvatarSource, defaultAvatarSource)
	email = strings.ToLower(strings.TrimSpace(email))
	switch source {
	case "pfp":
		return nil, nil
	case "bimi":
		return a.orderedPhotos(email, "bimi"), nil
	case "gravatar_bimi":
		return a.orderedPhotos(email, "gravatar", "bimi"), nil
	default: // bimi_gravatar
		return a.orderedPhotos(email, "bimi", "gravatar"), nil
	}
}

// orderedPhotos builds the candidate list in the requested order, skipping any
// source that yields no url (e.g. a domain with no BIMI record).
func (a *App) orderedPhotos(email string, order ...string) []string {
	out := make([]string, 0, len(order))
	for _, src := range order {
		var url string
		switch src {
		case "gravatar":
			url = gravatarURL(email)
		case "bimi":
			url = a.bimiURL(emailDomain(email))
		}
		if url != "" {
			out = append(out, url)
		}
	}
	return out
}

// gravatarURL returns the Gravatar url for an email, or empty when there is no
// email. d=404 makes Gravatar 404 when it has no image, so the ui falls through.
func gravatarURL(email string) string {
	if email == "" {
		return ""
	}
	sum := md5.Sum([]byte(email))
	return fmt.Sprintf("https://www.gravatar.com/avatar/%s?d=404&s=160", hex.EncodeToString(sum[:]))
}

// bimiURL resolves and caches the BIMI logo url for a domain.
func (a *App) bimiURL(domain string) string {
	if domain == "" {
		return ""
	}
	if v, ok := bimiCache.Load(domain); ok {
		return v.(string)
	}
	url := lookupBIMI(domain)
	bimiCache.Store(domain, url)
	return url
}

// lookupBIMI reads the default BIMI dns record for a domain and returns the
// logo location (the l= tag), or empty when there is no usable record.
func lookupBIMI(domain string) string {
	records, err := net.LookupTXT("default._bimi." + domain)
	if err != nil {
		return ""
	}
	for _, rec := range records {
		for part := range strings.SplitSeq(rec, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(strings.ToLower(part), "l=") {
				return strings.TrimSpace(part[2:])
			}
		}
	}
	return ""
}
