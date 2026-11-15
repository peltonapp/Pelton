package desktop

import (
	"strings"
	"testing"
)

// The print view opens in the system browser, outside the reading pane's
// frame, so it has to carry its own policy. Without one a css background url
// the sanitizer keeps loaded there even with remote content blocked.
func TestPrintDocumentBlocksRemoteContentUnlessAllowed(t *testing.T) {
	body := `<div style="background:url(https://tracker.example/p.png)">hi</div>`

	blocked := buildPrintDocument(printMeta{Subject: "s"}, body, false)
	policy := printPolicy(t, blocked)
	if !strings.Contains(policy, "img-src data:;") || strings.Contains(policy, "http") {
		t.Errorf("blocked print view allows remote content: %s", policy)
	}
	if !strings.Contains(policy, "script-src 'none'") {
		t.Errorf("print view allows scripts: %s", policy)
	}

	allowed := printPolicy(t, buildPrintDocument(printMeta{Subject: "s"}, body, true))
	if !strings.Contains(allowed, "img-src data: https: http:;") {
		t.Errorf("print view of a trusted sender blocks its images: %s", allowed)
	}
}

// printPolicy returns the content security policy of a print document, which
// has to come before anything in it that could load.
func printPolicy(t *testing.T, doc string) string {
	t.Helper()
	const marker = `<meta http-equiv="Content-Security-Policy" content="`
	at := strings.Index(doc, marker)
	if at < 0 || at > strings.Index(doc, "<title>") {
		t.Fatalf("no content security policy at the top of the print view:\n%s", doc)
	}
	rest := doc[at+len(marker):]
	return rest[:strings.IndexByte(rest, '"')]
}
