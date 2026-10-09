package mailview

import (
	stdhtml "html"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	htmltok "golang.org/x/net/html"
)

// TrackerSignal is one reason an image looks like a tracking pixel. They are
// reported rather than collapsed into a verdict, so the ui can say why and the
// reader can disagree. Detection is a guess, and a guess that will not show its
// working is not worth much.
type TrackerSignal string

const (
	// SignalTiny is a declared width or height of 1 or 0. A remote image that
	// small is not there to be looked at.
	SignalTiny TrackerSignal = "tiny"
	// SignalHidden is an inline style that keeps the image off screen while
	// still loading it.
	SignalHidden TrackerSignal = "hidden"
	// SignalKnownHost is a host on the bundled list below.
	SignalKnownHost TrackerSignal = "known-host"
	// SignalRecipient is an email address in the url. That is the message
	// reporting who opened it, not a coincidence.
	SignalRecipient TrackerSignal = "recipient"
	// SignalOpaqueID is a long meaningless-looking token in the query. Weak on
	// its own: signed image cdns look exactly like this.
	SignalOpaqueID TrackerSignal = "opaque-id"
	// SignalLoneImage is the only remote image loaded from its host. Weak on its
	// own: plenty of ordinary mail has one image on its own domain.
	SignalLoneImage TrackerSignal = "lone-image"
)

// strong reports whether one signal is enough on its own.
func (s TrackerSignal) strong() bool {
	switch s {
	case SignalTiny, SignalHidden, SignalKnownHost, SignalRecipient:
		return true
	default:
		return false
	}
}

// TrackedImage is one remote image in a message, with whatever the scan noticed
// about it.
type TrackedImage struct {
	// URL is the src exactly as the message wrote it, and Host is the host it
	// would reach.
	URL  string
	Host string
	// Signals is why this looks like tracking, in detection order. Empty for an
	// ordinary image.
	Signals []TrackerSignal
}

// LooksLikeTracker reports whether the signals add up. One strong signal is
// enough; the weak ones only count together, because each of them describes
// plenty of ordinary images too.
func (img TrackedImage) LooksLikeTracker() bool {
	weak := 0
	for _, s := range img.Signals {
		if s.strong() {
			return true
		}
		weak++
	}
	return weak >= 2
}

// RemoteScan is what a body's remote images add up to.
type RemoteScan struct {
	// Images is every remote image found, in document order, capped at
	// maxScannedImages.
	Images []TrackedImage
	// Trackers are the subset that look like tracking pixels.
	Trackers []TrackedImage
}

// OtherCount is how many remote images did not look like tracking, which is the
// number the banner calls "images".
func (s RemoteScan) OtherCount() int {
	return len(s.Images) - len(s.Trackers)
}

// maxScannedImages caps the scan so a message built out of thousands of img
// tags cannot turn opening it into a long walk.
const maxScannedImages = 500

// tinyThreshold is the largest declared side length that still counts as too
// small to be meant for looking at.
const tinyThreshold = 1

// opaqueIDLen is how long a query value has to be before it reads as an
// identifier rather than a setting.
const opaqueIDLen = 16

// emailInURLPattern finds an email address in a url, including the common
// percent-encoded and base64-ish-looking forms where the @ survives as %40.
var emailInURLPattern = regexp.MustCompile(`(?i)[a-z0-9._%+-]+(@|%40)[a-z0-9.-]+\.[a-z]{2,}`)

// opaqueIDPattern matches a long run of identifier characters with no word
// breaks, which is what a campaign or recipient token looks like.
var opaqueIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{` + strconv.Itoa(opaqueIDLen) + `,}$`)

// hiddenStylePattern matches inline styles that load an image without showing
// it: display:none, visibility:hidden, or a 1px/0 box.
var hiddenStylePattern = regexp.MustCompile(`(?i)(display\s*:\s*none|visibility\s*:\s*hidden|opacity\s*:\s*0(\.0+)?\b|(width|height)\s*:\s*[01](px)?\s*(;|$))`)

// ScanRemoteImages walks the html and reports every remote image with the
// signals that apply to it.
//
// It parses rather than pattern-matches, because the interesting signals are
// the width, height and style of each img, which only exist per element. The
// existing regex helpers above answer coarser questions and stay as they are.
func ScanRemoteImages(html string) RemoteScan {
	var scan RemoteScan
	if html == "" {
		return scan
	}

	perHost := make(map[string]int)
	z := htmltok.NewTokenizer(strings.NewReader(html))
	for len(scan.Images) < maxScannedImages {
		tt := z.Next()
		if tt == htmltok.ErrorToken {
			break
		}
		if tt != htmltok.StartTagToken && tt != htmltok.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) != "img" || !hasAttr {
			continue
		}
		img, ok := scanImg(z)
		if !ok {
			continue
		}
		perHost[img.Host]++
		scan.Images = append(scan.Images, img)
	}

	// the lone-image signal needs the whole document, so it is applied once the
	// per-host counts are final.
	for i := range scan.Images {
		if perHost[scan.Images[i].Host] == 1 {
			scan.Images[i].Signals = append(scan.Images[i].Signals, SignalLoneImage)
		}
		if scan.Images[i].LooksLikeTracker() {
			scan.Trackers = append(scan.Trackers, scan.Images[i])
		}
	}
	return scan
}

// scanImg reads one img element's attributes and returns it when its source is
// remote. ok is false for inline (cid:, data:) images and for anything without
// a usable http(s) src.
func scanImg(z *htmltok.Tokenizer) (TrackedImage, bool) {
	var src, style string
	var tiny bool
	for {
		key, val, more := z.TagAttr()
		switch strings.ToLower(string(key)) {
		case "src":
			src = strings.TrimSpace(stdhtml.UnescapeString(string(val)))
		case "style":
			style = string(val)
		case "width", "height":
			if isTinyDimension(string(val)) {
				tiny = true
			}
		}
		if !more {
			break
		}
	}

	lower := strings.ToLower(src)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return TrackedImage{}, false
	}
	parsed, err := url.Parse(src)
	if err != nil || parsed.Host == "" {
		return TrackedImage{}, false
	}

	img := TrackedImage{URL: src, Host: strings.ToLower(parsed.Hostname())}
	if tiny {
		img.Signals = append(img.Signals, SignalTiny)
	}
	if style != "" && hiddenStylePattern.MatchString(style) {
		img.Signals = append(img.Signals, SignalHidden)
	}
	if isKnownTrackerHost(img.Host) {
		img.Signals = append(img.Signals, SignalKnownHost)
	}
	if emailInURLPattern.MatchString(src) {
		img.Signals = append(img.Signals, SignalRecipient)
	} else if hasOpaqueID(parsed) {
		// only when there is no address: an address is the same observation,
		// stated more plainly, and reporting both would double-count it.
		img.Signals = append(img.Signals, SignalOpaqueID)
	}
	return img, true
}

// isTinyDimension reports whether a width/height attribute is small enough that
// the image cannot be meant to be seen. A percentage or an unparsable value is
// not tiny: "100%" is a full-width banner.
func isTinyDimension(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	v = strings.TrimSuffix(v, "px")
	if v == "" || strings.HasSuffix(v, "%") {
		return false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return false
	}
	return n <= tinyThreshold
}

// hasOpaqueID reports whether the url carries a long identifier-looking token,
// in a query value or as a path segment.
func hasOpaqueID(u *url.URL) bool {
	for _, values := range u.Query() {
		if slices.ContainsFunc(values, opaqueIDPattern.MatchString) {
			return true
		}
	}
	for segment := range strings.SplitSeq(u.Path, "/") {
		// the file extension is not part of the identifier, and keeping it would
		// stop "abcdef0123456789.gif" from matching.
		if stem, _, ok := strings.CutLast(segment, "."); ok && stem != "" {
			segment = stem
		}
		if opaqueIDPattern.MatchString(segment) {
			return true
		}
	}
	return false
}

// isKnownTrackerHost reports whether host is, or is under, a bundled domain.
func isKnownTrackerHost(host string) bool {
	for _, domain := range trackerDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// trackerDomains are domains whose whole business is telling a sender that a
// message was opened. It ships in the repo and is checked here, in the parse
// path, against the host of an image the message already named. Nothing is
// fetched, nothing is looked up, and no host leaves the machine.
//
// It is deliberately not exhaustive and never will be. Chasing completeness
// here would be a losing race, and it does not have to be won: the structural
// signals above catch a pixel from a host nobody has heard of, and this list
// only adds a name to what they already found. Add an entry when a real message
// makes the case for it, not to pad the list.
var trackerDomains = []string{
	"actv.io",
	"bananatag.com",
	"braze.com",
	"braze.eu",
	"constantcontact.com",
	"convertkit-mail.com",
	"customeriomail.com",
	"doubleclick.net",
	"emltrk.com",
	"exct.net",
	"google-analytics.com",
	"googletagmanager.com",
	"hs-analytics.net",
	"hubspotemail.net",
	"iterable.com",
	"klaviyomail.com",
	"list-manage.com",
	"mailchimp.com",
	"mailerlite.com",
	"mailgun.org",
	"mailtrack.io",
	"mandrillapp.com",
	"omnisend.com",
	"rs6.net",
	"sailthru.com",
	"sendgrid.net",
	"sparkpostmail.com",
	"streak.com",
	"yesware.com",
}

// imgTagPattern matches a whole img element, which is what StripTrackers
// removes. img is void, so there is no closing tag to worry about.
var imgTagPattern = regexp.MustCompile(`(?is)<img\b[^>]*>`)

// srcAttrPattern pulls the src out of one img tag, quoted or bare.
var srcAttrPattern = regexp.MustCompile(`(?is)\bsrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

// StripTrackers removes the img elements whose src is in urls, so remote
// content can be shown without confirming the open.
//
// Removal happens before sanitizing, not after: the sanitizer's job is to
// decide what is safe markup, and it has no opinion about which of two equally
// well-formed images the reader wants.
func StripTrackers(html string, urls map[string]bool) string {
	if html == "" || len(urls) == 0 {
		return html
	}
	return imgTagPattern.ReplaceAllStringFunc(html, func(tag string) string {
		m := srcAttrPattern.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		src := strings.TrimSpace(stdhtml.UnescapeString(m[1] + m[2] + m[3]))
		if urls[src] {
			return ""
		}
		return tag
	})
}

// stripRemoteImages removes every img element with an http(s) source, and the
// legacy background attribute that loads one the same way. It backs the blocked
// view; see Sanitize for why the policy cannot do this itself.
func stripRemoteImages(html string) string {
	if html == "" {
		return html
	}
	out := imgTagPattern.ReplaceAllStringFunc(html, func(tag string) string {
		m := srcAttrPattern.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		src := strings.ToLower(strings.TrimSpace(stdhtml.UnescapeString(m[1] + m[2] + m[3])))
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			return ""
		}
		return tag
	})
	return remoteBackgroundAttrPattern.ReplaceAllString(out, "")
}

// mediaElementPattern matches a whole video or audio element, its inner source
// elements and all. Unlike img these have a closing tag, so the contents go
// with them rather than being left loose in the body.
var mediaElementPattern = regexp.MustCompile(`(?is)<video\b[^>]*>.*?</\s*video\s*>|<audio\b[^>]*>.*?</\s*audio\s*>|<(?:video|audio)\b[^>]*/?>`)

// stripRemoteMedia removes a video or audio element that would stream from a
// remote server, for the same reason stripRemoteImages removes a remote image:
// until the reader has asked for remote content, nothing in a message may
// reach out. The whole element goes rather than just its source, since a
// player with nothing to play is worse than no player.
//
// A source inside the element counts as well as the element's own: mail
// offering the same clip in several formats carries the url on those instead.
func stripRemoteMedia(html string) string {
	if html == "" {
		return html
	}
	return mediaElementPattern.ReplaceAllStringFunc(html, func(tag string) string {
		for _, m := range srcAttrPattern.FindAllStringSubmatch(tag, -1) {
			src := strings.ToLower(strings.TrimSpace(stdhtml.UnescapeString(m[1] + m[2] + m[3])))
			if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
				return ""
			}
		}
		return tag
	})
}

// remoteBackgroundAttrPattern matches a background="http(s)://..." attribute,
// the old table-cell way of loading an image.
var remoteBackgroundAttrPattern = regexp.MustCompile(`(?i)\sbackground\s*=\s*(?:"https?://[^"]*"|'https?://[^']*'|https?://[^\s>]+)`)

// TrackerURLs is the set of image sources in a scan that look like tracking,
// ready to hand to StripTrackers.
func (s RemoteScan) TrackerURLs() map[string]bool {
	out := make(map[string]bool, len(s.Trackers))
	for _, t := range s.Trackers {
		out[t.URL] = true
	}
	return out
}
