package mailview

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestScanRemoteImagesSignals(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		want    []TrackerSignal
		tracker bool
	}{
		{
			name:    "1x1 gif",
			html:    `<img src="https://example.com/a/open.gif" width="1" height="1">`,
			want:    []TrackerSignal{SignalTiny, SignalLoneImage},
			tracker: true,
		},
		{
			name:    "zero sized",
			html:    `<img src="https://example.com/p.png" width="0" height="0">`,
			want:    []TrackerSignal{SignalTiny, SignalLoneImage},
			tracker: true,
		},
		{
			name:    "hidden by style",
			html:    `<img src="https://example.com/p.png" style="display:none">`,
			want:    []TrackerSignal{SignalHidden, SignalLoneImage},
			tracker: true,
		},
		{
			name:    "known tracking host",
			html:    `<img src="https://track.list-manage.com/img.png" width="600">`,
			want:    []TrackerSignal{SignalKnownHost, SignalLoneImage},
			tracker: true,
		},
		{
			name:    "subdomain of a known host",
			html:    `<img src="https://ct.sendgrid.net/wf/open" width="20">`,
			want:    []TrackerSignal{SignalKnownHost, SignalLoneImage},
			tracker: true,
		},
		{
			name:    "recipient address in the url",
			html:    `<img src="https://news.example.com/o?to=arne%40example.org" width="500">`,
			want:    []TrackerSignal{SignalRecipient, SignalLoneImage},
			tracker: true,
		},
		{
			name:    "opaque id alone on a host is two weak signals",
			html:    `<img src="https://cdn.example.com/i?c=Zm9vYmFyYmF6cXV1eDEyMw" width="500">`,
			want:    []TrackerSignal{SignalOpaqueID, SignalLoneImage},
			tracker: true,
		},
		{
			name:    "plain image is nothing",
			html:    `<img src="https://cdn.example.com/logo.png" width="200"><img src="https://cdn.example.com/hero.jpg" width="600">`,
			want:    nil,
			tracker: false,
		},
		{
			name:    "percentage width is not tiny",
			html:    `<img src="https://cdn.example.com/a.png" width="100%"><img src="https://cdn.example.com/b.png" width="100%">`,
			want:    nil,
			tracker: false,
		},
		{
			name:    "inline cid image is not remote",
			html:    `<img src="cid:logo@example" width="1" height="1">`,
			want:    nil,
			tracker: false,
		},
		{
			name:    "data uri is not remote",
			html:    `<img src="data:image/gif;base64,R0lGOD" width="1">`,
			want:    nil,
			tracker: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scan := ScanRemoteImages(tt.html)
			if tt.want == nil {
				if len(scan.Trackers) != 0 {
					t.Fatalf("Trackers = %v, want none", scan.Trackers)
				}
				return
			}
			if len(scan.Images) == 0 {
				t.Fatal("no remote images found")
			}
			got := scan.Images[0].Signals
			if !slices.Equal(got, tt.want) {
				t.Errorf("signals = %v, want %v", got, tt.want)
			}
			if scan.Images[0].LooksLikeTracker() != tt.tracker {
				t.Errorf("LooksLikeTracker() = %t, want %t", scan.Images[0].LooksLikeTracker(), tt.tracker)
			}
		})
	}
}

// TestOneWeakSignalIsNotEnough is the false-positive guard: a signed cdn url
// looks opaque, and a message with several images on that host must not have
// them all called tracking.
func TestOneWeakSignalIsNotEnough(t *testing.T) {
	html := `
<img src="https://cdn.example.com/i/aGVyb2ltYWdlc2lnbmVkMTIz.jpg" width="600">
<img src="https://cdn.example.com/i/Zm9vdGVyaW1hZ2VzaWduZWQ.jpg" width="600">`
	scan := ScanRemoteImages(html)
	if len(scan.Images) != 2 {
		t.Fatalf("found %d images, want 2", len(scan.Images))
	}
	if len(scan.Trackers) != 0 {
		t.Errorf("Trackers = %v, want none: an opaque id on a shared host is one weak signal", scan.Trackers)
	}
	if scan.OtherCount() != 2 {
		t.Errorf("OtherCount() = %d, want 2", scan.OtherCount())
	}
}

func TestScanSeparatesTrackersFromImages(t *testing.T) {
	html := `
<img src="https://cdn.example.com/logo.png" width="200">
<img src="https://cdn.example.com/hero.jpg" width="600">
<img src="https://track.example.net/open.gif" width="1" height="1">
<img src="https://ct.sendgrid.net/wf/open?upn=abc" height="1">`

	scan := ScanRemoteImages(html)
	if len(scan.Images) != 4 {
		t.Fatalf("found %d images, want 4", len(scan.Images))
	}
	if len(scan.Trackers) != 2 {
		t.Fatalf("Trackers = %d, want 2: %v", len(scan.Trackers), scan.Trackers)
	}
	if scan.OtherCount() != 2 {
		t.Errorf("OtherCount() = %d, want 2", scan.OtherCount())
	}
	wantHosts := []string{"track.example.net", "ct.sendgrid.net"}
	for i, want := range wantHosts {
		if scan.Trackers[i].Host != want {
			t.Errorf("tracker %d host = %q, want %q", i, scan.Trackers[i].Host, want)
		}
	}
}

func TestStripTrackersRemovesOnlyTheTrackers(t *testing.T) {
	html := `<p>hi</p>
<img src="https://cdn.example.com/hero.jpg" width="600">
<img src="https://track.example.net/open.gif" width="1" height="1">`

	scan := ScanRemoteImages(html)
	out := StripTrackers(html, scan.TrackerURLs())

	if strings.Contains(out, "track.example.net") {
		t.Errorf("tracker survived:\n%s", out)
	}
	if !strings.Contains(out, "hero.jpg") {
		t.Errorf("ordinary image was removed:\n%s", out)
	}
	if !strings.Contains(out, "<p>hi</p>") {
		t.Errorf("body text was disturbed:\n%s", out)
	}
}

func TestStripTrackersWithNothingToStripIsUnchanged(t *testing.T) {
	html := `<img src="https://cdn.example.com/hero.jpg">`
	if got := StripTrackers(html, nil); got != html {
		t.Errorf("StripTrackers() = %q, want the input back", got)
	}
	if got := StripTrackers(html, map[string]bool{}); got != html {
		t.Errorf("StripTrackers() with an empty set = %q, want the input back", got)
	}
}

func TestScanIsCapped(t *testing.T) {
	var b strings.Builder
	for range maxScannedImages + 50 {
		b.WriteString(`<img src="https://cdn.example.com/a.png" width="10">`)
	}
	if got := len(ScanRemoteImages(b.String()).Images); got != maxScannedImages {
		t.Errorf("scanned %d images, want the cap of %d", got, maxScannedImages)
	}
}

// TestSanitizeBlocksRemoteImageSources covers the sanitizer layer itself. It
// used to pass everything through: the scheme list has no effect until urls are
// parsed, so the blocked variant kept the remote src and only the reading
// iframe's csp stopped it loading.
func TestSanitizeBlocksRemoteImageSources(t *testing.T) {
	const html = `<p>hi</p><img src="https://cdn.example.com/hero.jpg" width="600">`

	blocked := Sanitize(html, false, true)
	if strings.Contains(blocked, "cdn.example.com") {
		t.Errorf("remote src survived the blocking policy:\n%s", blocked)
	}
	if !strings.Contains(blocked, "hi") {
		t.Errorf("body text was lost with it:\n%s", blocked)
	}

	allowed := Sanitize(html, true, true)
	if !strings.Contains(allowed, "cdn.example.com") {
		t.Errorf("remote src dropped by the allowing policy:\n%s", allowed)
	}
}

func TestSanitizeKeepsInlineAndMailtoURLs(t *testing.T) {
	const html = `<img src="cid:logo@example"><img src="data:image/gif;base64,R0lGOD"><a href="mailto:me@example.com">mail</a>`
	got := Sanitize(html, false, true)
	for _, want := range []string{"cid:logo@example", "data:image/gif", "mailto:me@example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("sanitize dropped %q:\n%s", want, got)
		}
	}
}

// TestSanitizeKeepsLinksWhenRemoteIsBlocked is the reason the block is done by
// removing images rather than by withdrawing the http(s) url schemes. Taking
// the schemes away works, but it takes every href with it, and a link is not a
// leak: it goes nowhere until it is clicked.
func TestSanitizeKeepsLinksWhenRemoteIsBlocked(t *testing.T) {
	const html = `<a href="https://example.com/article">read this</a>`
	got := Sanitize(html, false, true)
	if !strings.Contains(got, "https://example.com/article") {
		t.Errorf("link href was stripped before the user loaded anything:\n%s", got)
	}
}

func TestSanitizeStripsRemoteBackgroundAttribute(t *testing.T) {
	const html = `<td background="https://track.example.net/bg.gif">cell</td>`
	got := Sanitize(html, false, true)
	if strings.Contains(got, "track.example.net") {
		t.Errorf("remote background attribute survived:\n%s", got)
	}
	if !strings.Contains(got, "cell") {
		t.Errorf("the cell content went with it:\n%s", got)
	}
}

// newsletterBody is a whole message rather than one img at a time: the
// lone-image signal depends on what else the message loads, so the per-signal
// cases above cannot show what a real newsletter comes out as.
const newsletterBody = `
<img src="https://cdn.example.com/hero.jpg" width="600" height="200" alt="hero">
<img src="https://cdn.example.com/logo.png" width="120" height="40" alt="logo">
<img src="https://signed.example.com/i/aGVyb2ltYWdlc2lnbmVkMTIz.jpg" width="600">
<img src="https://signed.example.com/i/Zm9vdGVyaW1hZ2VzaWduZWQ.jpg" width="600">
<img src="https://track.example.net/open.gif" width="1" height="1">
<img src="https://beacon.example.net/p.png" style="display:none" width="50">
<img src="https://ct.sendgrid.net/wf/open?upn=abc" width="40" height="40">
<img src="https://mail.example.org/o?to=arne%40example.org" width="500">
<img src="https://lonely.example.org/i?c=Zm9vYmFyYmF6cXV1eDEyMw" width="500">`

func TestScanOnARealisticNewsletter(t *testing.T) {
	scan := ScanRemoteImages(newsletterBody)

	if len(scan.Images) != 9 {
		t.Fatalf("found %d images, want 9", len(scan.Images))
	}
	wantTrackers := []string{
		"track.example.net",
		"beacon.example.net",
		"ct.sendgrid.net",
		"mail.example.org",
		"lonely.example.org",
	}
	var got []string
	for _, tr := range scan.Trackers {
		got = append(got, tr.Host)
	}
	if !slices.Equal(got, wantTrackers) {
		t.Errorf("trackers = %v, want %v", got, wantTrackers)
	}
	if scan.OtherCount() != 4 {
		t.Errorf("OtherCount() = %d, want 4", scan.OtherCount())
	}
}

// mail that designs itself with a typeface should read the way it was written,
// which it cannot do if font-family is stripped on the way in.
func TestSanitizeKeepsSenderFontsOnlyWhenAllowed(t *testing.T) {
	const html = `<p style="font-family:Georgia,serif;font-size:15px">hello</p><font face="Courier">code</font>`

	kept := Sanitize(html, false, true)
	for _, want := range []string{"Georgia", "font-size", `face="Courier"`} {
		if !strings.Contains(kept, want) {
			t.Errorf("sender fonts on dropped %q:\n%s", want, kept)
		}
	}

	stripped := Sanitize(html, false, false)
	for _, gone := range []string{"Georgia", "Courier"} {
		if strings.Contains(stripped, gone) {
			t.Errorf("sender fonts off kept %q:\n%s", gone, stripped)
		}
	}
	// only the family goes: size is a layout choice, not a typeface.
	if !strings.Contains(stripped, "font-size") {
		t.Errorf("font-size went with the family:\n%s", stripped)
	}
	if !strings.Contains(stripped, "hello") || !strings.Contains(stripped, "code") {
		t.Errorf("text was lost with the fonts:\n%s", stripped)
	}
}

func TestHasOpaqueIDPathSegment(t *testing.T) {
	id := "abcdef0123456789"
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"bare id", "/" + id, true},
		{"extension is dropped", "/" + id + ".gif", true},
		{"only the last dot is dropped", "/" + id + ".tar.gif", false},
		{"trailing dot is dropped", "/" + id + ".", true},
		{"leading dot is kept", "/." + id, false},
		{"no dot, too short", "/abc", false},
		{"id in a later segment", "/a/b/" + id + ".png", true},
		{"empty path", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasOpaqueID(&url.URL{Path: tt.path}); got != tt.want {
				t.Errorf("hasOpaqueID(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
