// Package promises finds commitments in mail: what the author promised ("I
// will send it by Friday") and what the author asked of the reader ("can you
// confirm by Monday?"), with who owes it and when it is due.
//
// Detection runs on this device from per-language pattern files in lang/,
// and leans to precision: a match needs a commitment or request form with an
// owner and an action, and is dropped under a negation, a condition, a hedge,
// a question about the past, or quotation marks. Only what the author newly
// wrote counts; quoted replies, forwarded messages and signatures are cut
// first. Store keeps what was found and what the user made of it.
package promises

import "strings"

// Kind says what form a promise took.
type Kind string

// The kinds.
const (
	// Commitment is the author saying they will do something.
	Commitment Kind = "commitment"
	// Request is the author asking the reader to do something.
	Request Kind = "request"
)

// Owner says who owes a promise, seen from the message.
type Owner string

// The owners.
const (
	OwnerAuthor    Owner = "author"
	OwnerRecipient Owner = "recipient"
)

// MinConfidence is the confidence below which nothing is suggested.
const MinConfidence = 0.5

// maxSentenceBytes skips run-on text, which is rarely a sentence a promise
// can be read from.
const maxSentenceBytes = 400

// maxPerMessage caps the candidates of one message.
const maxPerMessage = 5

// Input is one message to read.
type Input struct {
	// Text is the message's plain text, quotes and signature included.
	Text string
	// Clock is when it was written, in the user's zone and week.
	Clock Clock
	// LangHint is the language to assume when the text does not say, such as
	// the user's interface language.
	LangHint string
}

// Candidate is one promise found in a message.
type Candidate struct {
	Kind  Kind
	Owner Owner
	// Quote is the sentence as written; Start and End are its byte offsets
	// in Input.Text.
	Quote      string
	Start, End int
	Due        Due
	Confidence float64
	Lang       string
}

// AuthorText returns the part of text its author newly wrote, with quoted
// lines blanked and replies, forwards and signatures cut off, and the
// language it was read as.
func AuthorText(text, langHint string) (string, string) {
	l := pickLanguage(text, langHint)
	if l == nil {
		return text, ""
	}
	return authorText(text, l), l.code
}

// Detect returns the promises in a message at or above MinConfidence, in
// the order they are written, at most a handful.
func Detect(in Input) []Candidate {
	l := pickLanguage(in.Text, in.LangHint)
	if l == nil {
		return nil
	}
	text := authorText(in.Text, l)
	var out []Candidate
	seen := map[string]bool{}
	for _, s := range sentences(text, l) {
		if s.end-s.start > maxSentenceBytes {
			continue
		}
		raw := text[s.start:s.end]
		c, ok := readSentence(raw, l, in.Clock)
		if !ok {
			continue
		}
		key := strings.ToLower(strings.Join(strings.Fields(raw), " "))
		if seen[key] {
			continue
		}
		seen[key] = true
		c.Quote, c.Start, c.End, c.Lang = raw, s.start, s.end, l.code
		out = append(out, c)
		if len(out) == maxPerMessage {
			break
		}
	}
	return out
}

// HasCue reports whether the author text of a message holds anything shaped
// like a promise, before the checks that drop a match. It decides whether a
// message is worth a second opinion.
func HasCue(text, langHint string) bool {
	l := pickLanguage(text, langHint)
	if l == nil {
		return false
	}
	folded := foldString(authorText(text, l))
	for _, list := range [][]pattern{l.commitments, l.requests} {
		for _, p := range list {
			if p.re.MatchString(folded) {
				return true
			}
		}
	}
	return false
}

// readSentence reads one sentence, returning the best promise in it.
func readSentence(raw string, l *language, clock Clock) (Candidate, bool) {
	f := foldString(raw)
	trimmed := strings.TrimSpace(f)
	question := strings.HasSuffix(strings.TrimRight(trimmed, `"')`), "?")
	condText := f
	if l.notCond != nil {
		condText = l.notCond.ReplaceAllString(f, " ")
	}
	if l.conditional != nil && l.conditional.MatchString(condText) {
		return Candidate{}, false
	}
	if l.openQ != nil && l.openQ.MatchString(trimmed) {
		return Candidate{}, false
	}
	var due Due
	dueRead := false
	dueOf := func() Due {
		if !dueRead {
			due, dueRead = findDue(f, l, clock), true
		}
		return due
	}
	best := Candidate{}
	try := func(list []pattern, kind Kind, owner Owner) {
		for _, p := range list {
			loc := p.re.FindStringIndex(f)
			if loc == nil || insideQuotes(f, loc[0]) {
				continue
			}
			if kind == Commitment && question {
				continue
			}
			if kind == Request && l.past != nil && l.past.MatchString(trimmed) {
				continue
			}
			clause := clauseOf(f, loc)
			if !p.allowNegation && l.negation != nil && l.negation.MatchString(clause) {
				continue
			}
			if kind == Commitment && l.done != nil && l.done.MatchString(clause) {
				continue
			}
			if kind == Commitment && l.hedge != nil && l.hedge.MatchString(clause) {
				continue
			}
			if l.habitual != nil && l.habitual.MatchString(clause) {
				continue
			}
			d := dueOf()
			if p.needsDue && d.IsZero() {
				continue
			}
			if p.needsCue && d.IsZero() && (l.futureCue == nil || !l.futureCue.MatchString(clause)) {
				continue
			}
			conf := p.conf
			if !d.IsZero() {
				conf += 0.1
			}
			if conf > best.Confidence {
				best = Candidate{Kind: kind, Owner: owner, Due: d, Confidence: conf}
			}
		}
	}
	try(l.commitments, Commitment, OwnerAuthor)
	try(l.requests, Request, OwnerRecipient)
	if best.Confidence < MinConfidence {
		return Candidate{}, false
	}
	best.Confidence = min(best.Confidence, 0.95)
	return best, true
}

// insideQuotes reports whether offset at falls between a pair of double
// quotes, which makes the words someone else's.
func insideQuotes(f string, at int) bool {
	return strings.Count(f[:at], `"`)%2 == 1
}

// clauseOf is the part of the sentence around a match that a negation, a
// hedge or a "done" marker must be in to count: from the clause break before
// the match to the one after it.
func clauseOf(f string, loc []int) string {
	start := 0
	for _, sep := range []string{",", ";", ":", " - ", " but ", " aber ", " doch ", " und dann "} {
		if i := strings.LastIndex(strings.ToLower(f[:loc[0]]), sep); i >= 0 && i+len(sep) > start {
			start = i + len(sep)
		}
	}
	end := len(f)
	rest := strings.ToLower(f[loc[1]:])
	for _, sep := range []string{";", " - ", " but ", " aber ", " doch "} {
		if i := strings.Index(rest, sep); i >= 0 && loc[1]+i < end {
			end = loc[1] + i
		}
	}
	return f[start:end]
}
