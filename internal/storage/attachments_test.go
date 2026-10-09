package storage

import "testing"

func TestSanitizeFilenameKeepsTheLastPathSegment(t *testing.T) {
	tests := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"a/b/report.pdf", "report.pdf"},
		{`a\b\report.pdf`, "report.pdf"},
		{`a/b\report.pdf`, "report.pdf"},
		{"/report.pdf", "report.pdf"},
		{"dir/", fallbackFilename},
		{"", fallbackFilename},
		{"../../etc/passwd", "passwd"},
		{"a/CON.txt", fallbackFilename},
	}
	for _, tt := range tests {
		if got := sanitizeFilename(tt.in); got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
