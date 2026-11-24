package promises

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// The patterns of each language live in lang/<code>.json, so another
// language is a data file: the code reads whatever is there.
//
//go:embed lang/*.json
var langFS embed.FS

type langFile struct {
	Lang          string              `json:"lang"`
	DateOrder     string              `json:"dateOrder"`
	Stopwords     []string            `json:"stopwords"`
	Abbreviations []string            `json:"abbreviations"`
	Cut           []string            `json:"cut"`
	Signoff       string              `json:"signoff"`
	Lists         map[string][]string `json:"lists"`
	Commitments   []patternSpec       `json:"commitments"`
	Requests      []patternSpec       `json:"requests"`
	Negation      string              `json:"negation"`
	Conditional   string              `json:"conditional"`
	// NotConditional are forms of a conditional word that ask rather than
	// set a condition, such as "check if".
	NotConditional string `json:"notConditional"`
	// OpenQuestion starts a question that asks for information, not action.
	OpenQuestion string `json:"openQuestion"`
	// Habitual marks something done regularly rather than promised once.
	Habitual   string   `json:"habitual"`
	Hedge      string   `json:"hedge"`
	Past       string   `json:"past"`
	Done       string   `json:"done"`
	FutureCue  string   `json:"futureCue"`
	Exclusions string   `json:"exclusions"`
	Dates      dateSpec `json:"dates"`
}

type patternSpec struct {
	Re   string  `json:"re"`
	Conf float64 `json:"conf"`
	// NeedsDue keeps a match only when the sentence names a date.
	NeedsDue bool `json:"needsDue"`
	// NeedsCue keeps a match only when the sentence names a date or a word
	// that points ahead, for present tense that is a promise only then.
	NeedsCue bool `json:"needsCue"`
	// AllowNegation is for a form that holds a negation itself, such as
	// "don't forget to".
	AllowNegation bool `json:"allowNegation"`
}

type dateSpec struct {
	Weekdays map[string]int    `json:"weekdays"`
	Months   map[string]int    `json:"months"`
	Numbers  map[string]int    `json:"numbers"`
	Units    map[string]string `json:"units"`
	Phrases  []phraseSpec      `json:"phrases"`
	Times    []timeSpec        `json:"times"`
}

type phraseSpec struct {
	Re  string `json:"re"`
	Rel string `json:"rel"`
	N   int    `json:"n"`
}

type timeSpec struct {
	Re    string `json:"re"`
	AMPM  bool   `json:"ampm"`
	Fixed string `json:"fixed"`
}

type pattern struct {
	re            *regexp.Regexp
	conf          float64
	needsDue      bool
	needsCue      bool
	allowNegation bool
}

type phrase struct {
	re  *regexp.Regexp
	rel string
	n   int
}

type timePattern struct {
	re    *regexp.Regexp
	ampm  bool
	fixed string
}

// language is one compiled data file.
type language struct {
	code        string
	dateOrder   string
	stop        map[string]bool
	abbrev      map[string]bool
	cut         []*regexp.Regexp
	signoff     *regexp.Regexp
	commitments []pattern
	requests    []pattern
	negation    *regexp.Regexp
	conditional *regexp.Regexp
	notCond     *regexp.Regexp
	openQ       *regexp.Regexp
	habitual    *regexp.Regexp
	hedge       *regexp.Regexp
	past        *regexp.Regexp
	done        *regexp.Regexp
	futureCue   *regexp.Regexp
	exclusions  *regexp.Regexp
	weekdays    map[string]int
	months      map[string]int
	numbers     map[string]int
	units       map[string]string
	phrases     []phrase
	times       []timePattern
}

var languages = sync.OnceValues(loadLanguages)

var placeholderRE = regexp.MustCompile(`\{[A-Z]+\}`)

// Languages returns the codes of the languages there are patterns for, in
// order.
func Languages() []string {
	langs, err := languages()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(langs))
	for code := range langs {
		out = append(out, code)
	}
	slices.Sort(out)
	return out
}

func loadLanguages() (map[string]*language, error) {
	files, err := fs.Glob(langFS, "lang/*.json")
	if err != nil {
		return nil, err
	}
	out := map[string]*language{}
	for _, name := range files {
		raw, err := langFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		var f langFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("promises: %s: %w", name, err)
		}
		l, err := compileLanguage(f)
		if err != nil {
			return nil, fmt.Errorf("promises: %s: %w", name, err)
		}
		out[l.code] = l
	}
	return out, nil
}

// alternation joins words into a non-capturing group, longest first so a
// longer phrase wins over its prefix, with any space matching any run of
// white space.
func alternation(words []string) string {
	list := slices.Clone(words)
	sort.SliceStable(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	parts := make([]string, 0, len(list))
	for _, w := range list {
		parts = append(parts, strings.ReplaceAll(regexp.QuoteMeta(foldString(w)), " ", `\s+`))
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func compileLanguage(f langFile) (*language, error) {
	l := &language{
		code:      f.Lang,
		dateOrder: f.DateOrder,
		stop:      map[string]bool{},
		abbrev:    map[string]bool{},
		weekdays:  foldKeys(f.Dates.Weekdays),
		months:    foldKeys(f.Dates.Months),
		numbers:   foldKeys(f.Dates.Numbers),
		units:     foldKeys(f.Dates.Units),
	}
	if l.code == "" {
		return nil, fmt.Errorf("no language code")
	}
	for _, w := range f.Stopwords {
		l.stop[foldString(w)] = true
	}
	for _, w := range f.Abbreviations {
		l.abbrev[foldString(w)] = true
	}
	subst := map[string]string{
		"{WEEKDAY}": "(?P<weekday>" + alternation(keys(f.Dates.Weekdays)) + ")",
		"{MONTH}":   "(?P<month>" + alternation(keys(f.Dates.Months)) + ")",
		"{NUMBER}":  "(?P<number>\\d{1,3}|" + alternation(keys(f.Dates.Numbers)) + ")",
		"{UNIT}":    "(?P<unit>" + alternation(keys(f.Dates.Units)) + ")",
		"{DAY}":     `(?P<day>\d{1,2})`,
		"{YEAR}":    `(?P<year>\d{4})`,
	}
	for name, words := range f.Lists {
		subst["{"+name+"}"] = alternation(words)
	}
	compile := func(src, flags string) (*regexp.Regexp, error) {
		if src == "" {
			return nil, nil
		}
		src = foldString(src)
		for k, v := range subst {
			src = strings.ReplaceAll(src, k, v)
		}
		if leftover := placeholderRE.FindString(src); leftover != "" {
			return nil, fmt.Errorf("unknown placeholder %s", leftover)
		}
		return regexp.Compile(flags + src)
	}
	var err error
	compileAll := func(specs []patternSpec) ([]pattern, error) {
		out := make([]pattern, 0, len(specs))
		for _, s := range specs {
			re, err := compile(s.Re, "(?i)")
			if err != nil {
				return nil, err
			}
			out = append(out, pattern{re: re, conf: s.Conf, needsDue: s.NeedsDue, needsCue: s.NeedsCue, allowNegation: s.AllowNegation})
		}
		return out, nil
	}
	if l.commitments, err = compileAll(f.Commitments); err != nil {
		return nil, err
	}
	if l.requests, err = compileAll(f.Requests); err != nil {
		return nil, err
	}
	for _, c := range f.Cut {
		re, err := compile(c, "(?im)")
		if err != nil {
			return nil, err
		}
		l.cut = append(l.cut, re)
	}
	for _, x := range []struct {
		dst *(*regexp.Regexp)
		src string
	}{
		{&l.signoff, f.Signoff}, {&l.negation, f.Negation}, {&l.conditional, f.Conditional},
		{&l.notCond, f.NotConditional}, {&l.openQ, f.OpenQuestion}, {&l.habitual, f.Habitual},
		{&l.hedge, f.Hedge}, {&l.past, f.Past}, {&l.done, f.Done}, {&l.futureCue, f.FutureCue},
		{&l.exclusions, f.Exclusions},
	} {
		if *x.dst, err = compile(x.src, "(?i)"); err != nil {
			return nil, err
		}
	}
	for _, p := range f.Dates.Phrases {
		re, err := compile(p.Re, "(?i)")
		if err != nil {
			return nil, err
		}
		l.phrases = append(l.phrases, phrase{re: re, rel: p.Rel, n: p.N})
	}
	for _, t := range f.Dates.Times {
		re, err := compile(t.Re, "(?i)")
		if err != nil {
			return nil, err
		}
		l.times = append(l.times, timePattern{re: re, ampm: t.AMPM, fixed: t.Fixed})
	}
	return l, nil
}

func foldKeys[V any](m map[string]V) map[string]V {
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[strings.ToLower(foldString(k))] = v
	}
	return out
}

// pickLanguage chooses the language of a text by its common words, falling
// back to the hint when the words do not decide, and to English after that.
func pickLanguage(text, hint string) *language {
	langs, err := languages()
	if err != nil || len(langs) == 0 {
		return nil
	}
	hits := map[string]int{}
	n := 0
	for _, w := range strings.FieldsFunc(strings.ToLower(foldString(text)), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r == '\'')
	}) {
		if n++; n > 400 {
			break
		}
		for code, l := range langs {
			if l.stop[w] {
				hits[code]++
			}
		}
	}
	best, bestHits, tie := "", 0, false
	for _, code := range Languages() {
		switch h := hits[code]; {
		case h > bestHits:
			best, bestHits, tie = code, h, false
		case h == bestHits && h > 0:
			tie = true
		}
	}
	if best != "" && !tie {
		return langs[best]
	}
	if l, ok := langs[strings.ToLower(hint)]; ok {
		return l
	}
	if l, ok := langs[best]; ok {
		return l
	}
	return langs["en"]
}
