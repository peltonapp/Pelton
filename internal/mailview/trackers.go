package mailview

import (
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

// strong reports whether one signal is enough on its own. Hidden is not:
// responsive newsletters hide the desktop or the mobile copy of an image with
// display:none and show it again from a media query, so a hidden image is very
// often an ordinary one.
func (s TrackerSignal) strong() bool {
	switch s {
	case SignalTiny, SignalKnownHost, SignalRecipient:
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
//
// An opaque id on the only image from a host is not enough together either:
// that is a newsletter's one hero picture on its cdn, under a hashed file name,
// at least as often as it is a pixel.
func (img TrackedImage) LooksLikeTracker() bool {
	weak := 0
	opaqueAndLone := 0
	for _, s := range img.Signals {
		if s.strong() {
			return true
		}
		weak++
		if s == SignalOpaqueID || s == SignalLoneImage {
			opaqueAndLone++
		}
	}
	return weak >= 2 && weak > opaqueAndLone
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
var emailInURLPattern = regexp.MustCompile(`(?i)[a-z0-9._%+-]+(@|%40)[a-z0-9.-]+\.([a-z]{2,})`)

// fileExtensions end what looks like an address but is a file name: retina
// images are named logo@2x.png, and that is not anybody's mailbox.
var fileExtensions = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "svg": true,
	"avif": true, "bmp": true, "ico": true, "tif": true, "tiff": true, "heic": true,
}

// hasAddress reports whether a url carries an email address.
func hasAddress(src string) bool {
	for _, m := range emailInURLPattern.FindAllStringSubmatch(src, -1) {
		if !fileExtensions[strings.ToLower(m[2])] {
			return true
		}
	}
	return false
}

// opaqueIDPattern matches a long run of identifier characters with no word
// breaks, which is what a campaign or recipient token looks like.
var opaqueIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{` + strconv.Itoa(opaqueIDLen) + `,}$`)

// hiddenStylePattern matches inline styles that load an image without showing
// it: display:none, visibility:hidden or no opacity. Each has to be a whole
// declaration, so neither "max-height:0" in a responsive rule nor a property
// that merely ends in one of these names counts.
var hiddenStylePattern = regexp.MustCompile(`(?i)(?:^|;)\s*(?:display\s*:\s*none|visibility\s*:\s*hidden|opacity\s*:\s*0(?:\.0+)?)\s*(?:!important\s*)?(?:;|$)`)

// styleSizePattern reads a width or height declaration from an inline style,
// as a whole declaration: line-height:1 and border-width:0 are not sizes.
var styleSizePattern = regexp.MustCompile(`(?i)(?:^|;)\s*(width|height)\s*:\s*([^;]*)`)

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
	// the declared sides, by name; the inline style wins over the attribute,
	// as it does when the page is drawn.
	sides := map[string]string{}
	for {
		key, val, more := z.TagAttr()
		switch k := strings.ToLower(string(key)); k {
		case "src":
			// the tokenizer has decoded entities already; decoding again read
			// "&amp;amp;" as "&" here but as "&amp;" when stripping, so the
			// tracker found was never the tracker removed.
			src = imageSource(string(val))
		case "style":
			style = string(val)
		case "width", "height":
			if _, set := sides[k]; !set {
				sides[k] = string(val)
			}
		}
		if !more {
			break
		}
	}
	for _, m := range styleSizePattern.FindAllStringSubmatch(style, -1) {
		sides[strings.ToLower(m[1])] = m[2]
	}
	tiny := isTiny(sides)

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
	if hasAddress(src) {
		img.Signals = append(img.Signals, SignalRecipient)
	} else if hasOpaqueID(parsed) {
		// only when there is no address: an address is the same observation,
		// stated more plainly, and reporting both would double-count it.
		img.Signals = append(img.Signals, SignalOpaqueID)
	}
	return img, true
}

// isTiny reports whether an image is too small to be meant to be seen: a side
// of a pixel or less, and no side declared larger. A divider is 600 by 1 and is
// plainly visible, so one thin side is not enough when the other is wide.
func isTiny(sides map[string]string) bool {
	tiny := false
	for _, v := range sides {
		small, known := isTinyDimension(v)
		if !known {
			continue
		}
		if !small {
			return false
		}
		tiny = true
	}
	return tiny
}

// isTinyDimension reads one declared side. known is false for something that
// says nothing about the size, like "auto"; a percentage is known and never
// tiny, since "100%" is a full-width banner.
func isTinyDimension(value string) (small, known bool) {
	v := strings.TrimSpace(strings.ToLower(value))
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(v, "!important")), "px"))
	if v == "" {
		return false, false
	}
	if strings.HasSuffix(v, "%") {
		return false, true
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return false, false
	}
	return n <= tinyThreshold, true
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
		if dot := strings.LastIndexByte(segment, '.'); dot > 0 {
			segment = segment[:dot]
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
//
// A domain that also serves a newsletter's ordinary pictures does not belong
// here, because a match is enough on its own and would take every one of them:
// mailchimp.com (gallery.mailchimp.com), constantcontact.com
// (files.constantcontact.com) and iterable.com (library.iterable.com) are left
// out for that reason. Their pixels are still caught by size, or on the
// separate tracking domains listed here.
var trackerDomains = []string{
	"actv.io",
	"bananatag.com",
	"braze.com",
	"braze.eu",
	"convertkit-mail.com",
	"customeriomail.com",
	"doubleclick.net",
	"emltrk.com",
	"exct.net",
	"google-analytics.com",
	"googletagmanager.com",
	"hs-analytics.net",
	"hubspotemail.net",
	"klaviyomail.com",
	"list-manage.com",
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

// StripTrackers removes the img elements whose src is in urls, so remote
// content can be shown without confirming the open. urls comes from
// ScanRemoteImages over the same html, and both read a src the same way
// (imageSource), so a tracker found is a tracker removed.
//
// Removal happens before sanitizing, not after: the sanitizer's job is to
// decide what is safe markup, and it has no opinion about which of two equally
// well-formed images the reader wants. See rewrite.go for how it is done.
func StripTrackers(html string, urls map[string]bool) string {
	if html == "" || len(urls) == 0 {
		return html
	}
	return rewrite(html, filter{dropImage: func(src string) bool { return urls[src] }})
}

// TrackerURLs is the set of image sources in a scan that look like tracking,
// ready to hand to StripTrackers.
func (s RemoteScan) TrackerURLs() map[string]bool {
	out := make(map[string]bool, len(s.Trackers))
	for _, t := range s.Trackers {
		out[t.URL] = true
	}
	return out
}
