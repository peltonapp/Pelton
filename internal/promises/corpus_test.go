package promises

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type labelled struct {
	label string
	text  string
}

func readCorpus(t *testing.T, name string) []labelled {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []labelled
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		label, text, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("corpus line without a tab: %q", line)
		}
		switch label {
		case "commit", "request", "none":
		default:
			t.Fatalf("unknown label %q", label)
		}
		out = append(out, labelled{label, text})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func detectedLabel(text, lang string) string {
	clock := Clock{Ref: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC), Location: time.UTC, WeekStart: time.Monday}
	found := Detect(Input{Text: text, Clock: clock, LangHint: lang})
	if len(found) == 0 {
		return "none"
	}
	if found[0].Kind == Commitment {
		return "commit"
	}
	return "request"
}

type score struct {
	n, negatives, tp, fp, fn int
}

func (s score) precision() float64 { return float64(s.tp) / float64(max(s.tp+s.fp, 1)) }
func (s score) recall() float64    { return float64(s.tp) / float64(max(s.tp+s.fn, 1)) }

// evaluate scores the detector on a corpus. A detection of the wrong kind
// counts against both precision and recall.
func evaluate(t *testing.T, corpus []labelled, lang string) score {
	var s score
	for _, c := range corpus {
		s.n++
		got := detectedLabel(c.text, lang)
		if c.label == "none" {
			s.negatives++
		}
		switch {
		case got == c.label && got != "none":
			s.tp++
		case got != c.label:
			if got != "none" {
				s.fp++
				t.Logf("false positive (%s, labelled %s): %s", got, c.label, c.text)
			}
			if c.label != "none" {
				s.fn++
				t.Logf("missed (%s, read as %s): %s", c.label, got, c.text)
			}
		}
	}
	t.Logf("%s: %d sentences (%d negatives), precision %.3f, recall %.3f (tp %d, fp %d, fn %d)",
		lang, s.n, s.negatives, s.precision(), s.recall(), s.tp, s.fp, s.fn)
	return s
}

// TestCorpus measures precision and recall on the labelled sentences of each
// language. The rules were written with these sentences in view, so the
// numbers are in-sample; TestHoldout is the honest measure.
func TestCorpus(t *testing.T) {
	for _, lang := range Languages() {
		t.Run(lang, func(t *testing.T) {
			corpus := readCorpus(t, "corpus_"+lang+".tsv")
			if len(corpus) < 150 {
				t.Fatalf("%d sentences, want at least 150", len(corpus))
			}
			if s := evaluate(t, corpus, lang); s.precision() < 0.95 || s.recall() < 0.9 {
				t.Errorf("precision %.3f recall %.3f, want at least 0.95 and 0.9", s.precision(), s.recall())
			}
		})
	}
}

// TestHoldout scores sentences that were written after the rules and never
// used to change them.
func TestHoldout(t *testing.T) {
	for _, lang := range Languages() {
		t.Run(lang, func(t *testing.T) {
			corpus := readCorpus(t, "holdout_"+lang+".tsv")
			if s := evaluate(t, corpus, lang); s.precision() < 0.85 || s.recall() < 0.4 {
				t.Errorf("precision %.3f recall %.3f, want at least 0.85 and 0.4", s.precision(), s.recall())
			}
		})
	}
}
