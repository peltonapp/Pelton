package mailview

import (
	"slices"
	"testing"
)

func TestScanLinks(t *testing.T) {
	tests := []struct {
		name    string
		href    string
		tracked bool
		dest    string
		reasons []string
	}{
		{
			name: "a wrapper carrying its destination", tracked: true, reasons: []string{"redirect"},
			href: "https://click.tracker.test/c?u=https%3A%2F%2Fshop.example%2Fsale&id=abc",
			dest: "https://shop.example/sale",
		},
		{
			name: "a known click host with the destination", tracked: true, reasons: []string{"redirect", "known-host"},
			href: "https://u123.ct.sendgrid.net/ls/click?upn=x&url=https://news.example/article",
			dest: "https://news.example/article",
		},
		{
			name: "a known click host hiding the destination", tracked: true, reasons: []string{"known-host"},
			href: "https://example.us5.list-manage.com/track/click?u=8f2a&id=4c1d&e=9a7b",
		},
		{
			name: "a per-recipient token on a tracking subdomain", tracked: true, reasons: []string{"token"},
			href: "https://click.shop.example/ls/AbC123dEf456GhI789jKl0",
		},
		{
			name: "wrappers inside wrappers end at the last destination", tracked: true, reasons: []string{"redirect"},
			href: "https://t.tracker.test/r?url=" + "https%3A%2F%2Fclicks.other.test%2Fgo%3Furl%3Dhttps%253A%252F%252Fdest.example%252Fpage",
			dest: "https://dest.example/page",
		},
		{name: "a plain link to the sender's site", href: "https://shop.example/sale?utm_source=newsletter"},
		{name: "a site's own redirect to itself", href: "https://www.example.com/out?url=https://example.com/page"},
		{name: "a sign-in link with a return address", href: "https://accounts.example/login?redirect=https://app.example/home"},
		{name: "an unsubscribe link on a click host", href: "https://example.us5.list-manage.com/unsubscribe?u=1&id=2"},
		{name: "a token on an ordinary subdomain", href: "https://docs.example.com/AbC123dEf456GhI789jKl0"},
		{name: "mailto", href: "mailto:someone@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScanLinks(`<a href="` + tt.href + `">x</a>`)
			if !tt.tracked {
				if len(got) != 0 {
					t.Errorf("ScanLinks = %+v, want nothing", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("ScanLinks = %+v, want one link", got)
			}
			if got[0].Destination != tt.dest || !slices.Equal(got[0].Reasons, tt.reasons) {
				t.Errorf("link = %+v, want destination %q and reasons %v", got[0], tt.dest, tt.reasons)
			}
		})
	}
}

func TestScanLinksCountsEachOnce(t *testing.T) {
	html := `<a href="https://x.list-manage.com/track/click?a=1&amp;b=2">a</a><a href="https://x.list-manage.com/track/click?a=1&b=2">b</a>`
	if got := ScanLinks(html); len(got) != 1 {
		t.Errorf("ScanLinks = %+v, want the same link counted once", got)
	}
}

func TestScanExternal(t *testing.T) {
	html := `<html><head>
<link rel="stylesheet" href="https://cdn.example/style.css">
<link rel="icon" href="https://cdn.example/favicon.ico">
<style>
@import url("https://fonts.example/css?family=X");
@font-face { font-family: X; src: url(https://fonts.example/x.woff2) format("woff2"); }
.hero { background: url('https://img.example/hero.png'); }
</style></head>
<body background="https://img.example/body.png">
<div style="background-image:url(https://img.example/div.png)">hi</div>
<form action="https://forms.example/submit"><input name="a"></form>
<iframe src="https://frames.example/embed"></iframe>
<video src="https://media.example/v.mp4"></video>
<img src="https://img.example/plain.png">
<div style="background:url(data:image/png;base64,AAAA)">inline</div>
</body></html>`
	got := map[string][]string{}
	for _, r := range ScanExternal(html) {
		got[r.Kind] = append(got[r.Kind], r.Host)
	}
	want := map[string][]string{
		"stylesheet": {"cdn.example", "fonts.example"},
		"font":       {"fonts.example"},
		"background": {"img.example"},
		"form":       {"forms.example"},
		"frame":      {"frames.example"},
		"media":      {"media.example"},
	}
	for kind, hosts := range want {
		if !slices.Equal(got[kind], hosts) {
			t.Errorf("%s = %v, want %v", kind, got[kind], hosts)
		}
	}
	if len(got) != len(want) {
		t.Errorf("kinds = %v, want exactly %v", got, want)
	}
	if none := ScanExternal(`<p>plain <a href="https://x.example">link</a> <img src="https://x.example/a.png"></p>`); len(none) != 0 {
		t.Errorf("links and images were reported as other resources: %+v", none)
	}
}

func TestCompany(t *testing.T) {
	for host, want := range map[string]string{
		"u123.ct.sendgrid.net":        "Twilio SendGrid",
		"example.us5.list-manage.com": "Mailchimp",
		"r.us-east-1.awstrack.me":     "Amazon",
		"click.shop.example":          "",
		"":                            "",
	} {
		if got := Company(host); got != want {
			t.Errorf("Company(%q) = %q, want %q", host, got, want)
		}
	}
}
