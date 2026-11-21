package desktop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestSplitJoinAddrs(t *testing.T) {
	got := splitAddrs("alice@x.test\n  bob@y.test \n\n")
	want := []string{"alice@x.test", "bob@y.test"}
	if len(got) != len(want) {
		t.Fatalf("splitAddrs len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitAddrs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if joinAddrs([]string{" a@x ", "", "b@y"}) != "a@x\nb@y" {
		t.Errorf("joinAddrs did not trim and drop blanks: %q", joinAddrs([]string{" a@x ", "", "b@y"}))
	}
}

func TestViewToDTOEmptyAddrsAreArrays(t *testing.T) {
	b, err := json.Marshal(viewToDTO(storage.View{Name: "x"}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"queryFrom":[]`) || !strings.Contains(s, `"queryTo":[]`) {
		t.Errorf("empty address lists must encode as [], got %s", s)
	}
}

func TestViewFilterMultiAddress(t *testing.T) {
	v := storage.View{QueryFrom: "alice@x.test\nbob@y.test"}
	f, err := buildViewFilter(v)
	if err != nil {
		t.Fatalf("buildViewFilter: %v", err)
	}
	if !f.matches(storage.Message{FromAddress: "bob@y.test"}) {
		t.Error("expected match on second chip (OR within field)")
	}
	if !f.matches(storage.Message{FromAddress: "ALICE@X.TEST"}) {
		t.Error("plain match should be case-insensitive")
	}
	if f.matches(storage.Message{FromAddress: "carol@z.test"}) {
		t.Error("unexpected match on non-listed sender")
	}
}

func TestViewFilterRegex(t *testing.T) {
	v := storage.View{QueryText: "invoice|receipt", UseRegex: true}
	f, err := buildViewFilter(v)
	if err != nil {
		t.Fatalf("buildViewFilter: %v", err)
	}
	if !f.matches(storage.Message{Subject: "Your receipt is ready"}) {
		t.Error("expected regex alternation to match body/subject content")
	}
	if f.matches(storage.Message{Subject: "hello there"}) {
		t.Error("unexpected match")
	}
}

func TestViewFilterRegexInvalid(t *testing.T) {
	if _, err := buildViewFilter(storage.View{QueryText: "([", UseRegex: true}); err == nil {
		t.Error("expected error for an invalid regular expression")
	}
}

func TestViewFilterAndsFields(t *testing.T) {
	v := storage.View{QueryFrom: "alice@x.test", QuerySubject: "report"}
	f, err := buildViewFilter(v)
	if err != nil {
		t.Fatalf("buildViewFilter: %v", err)
	}
	if f.matches(storage.Message{FromAddress: "alice@x.test", Subject: "lunch"}) {
		t.Error("from matched but subject did not; fields must AND")
	}
	if !f.matches(storage.Message{FromAddress: "alice@x.test", Subject: "weekly report"}) {
		t.Error("both fields present should match")
	}
}
