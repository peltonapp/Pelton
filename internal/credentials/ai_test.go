package credentials

import (
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/logging"
)

func TestAIKeyRoundTrip(t *testing.T) {
	if got, err := LoadAIKey("p1"); err != nil || got != "" {
		t.Fatalf("missing key: %q %v", got, err)
	}
	if err := StoreAIKey("p1", "sk-ai-roundtrip-123"); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadAIKey("p1"); err != nil || got != "sk-ai-roundtrip-123" {
		t.Fatalf("got %q %v", got, err)
	}
	if strings.Contains(logging.Redact("key sk-ai-roundtrip-123"), "sk-ai-roundtrip-123") {
		t.Fatal("the key is not redacted from logs")
	}
	if err := StoreAIKey("p1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadAIKey("p1"); got != "" {
		t.Fatalf("an empty key did not remove it: %q", got)
	}
	if err := DeleteAIKey("p1"); err != nil {
		t.Fatal(err)
	}
}
