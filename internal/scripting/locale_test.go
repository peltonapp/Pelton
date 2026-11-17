package scripting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuiltinsAreLocalized checks that the English catalog has a string for
// every key the built-in registry uses, so no picker shows a raw key.
func TestBuiltinsAreLocalized(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "locales", "en.ts"))
	if err != nil {
		t.Skip("frontend catalog not found:", err)
	}
	text := string(data)
	var missing []string
	for _, k := range testRegistry(t).Catalog().LocaleKeys() {
		if !strings.Contains(text, "'"+k+"':") {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d keys missing from en.ts:\n%s", len(missing), strings.Join(missing, "\n"))
	}
}
