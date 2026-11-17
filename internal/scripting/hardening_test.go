package scripting

import (
	"strings"
	"testing"
)

// TestStringBuildingIsBounded checks that the string builders a script can
// reach refuse a result past the cap before they allocate it, including the
// cases where the size check could overflow or be skipped.
func TestStringBuildingIsBounded(t *testing.T) {
	e := testEngine(t, newFakeMail())
	for name, src := range map[string]string{
		"rep overflowing the check": `string.rep("abcd", 2^62)`,
		"rep with a separator":      `string.rep("", 2^30, "x")`,
		"format with many widths": `local t = {} for i = 1, 300 do t[i] = 1 end
			string.format(string.rep("%9999d", 300), unpack(t))`,
		"replace of every position": `text.replace(string.rep("a", 100000), "", string.rep("b", 1000))`,
		"replace with a group":      `text.replace(string.rep("a", 600000), "(a)", "$1$1")`,
	} {
		t.Run(name, func(t *testing.T) {
			run := runLua(t, e, src)
			if run.Status != StatusError {
				t.Fatalf("status %s (%s), want an error", run.Status, run.Error)
			}
			if !strings.Contains(run.Error, "more than") {
				t.Fatalf("error %q, want the size cap", run.Error)
			}
		})
	}
	ok := runLua(t, e, `
		assert(string.rep("", 2^40) == "")
		assert(string.rep("ab", 3, "-") == "ab-ab-ab")
		assert(string.format("%5d|%s", 42, "x") == "   42|x")
		assert(text.replace("a1b22", "[0-9]+", "<$0>") == "a<1>b<22>")
		assert(text.replace("abc", "b", "") == "ac")`)
	if ok.Status != StatusOK {
		t.Fatalf("plain use failed: %s %s", ok.Status, ok.Error)
	}
}
