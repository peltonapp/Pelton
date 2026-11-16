package contactmerge

import (
	"sort"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Kind is what two contacts have in common.
type Kind string

const (
	// KindEmail is a shared email address.
	KindEmail Kind = "email"
	// KindPhone is a shared phone number, in national or international form.
	KindPhone Kind = "phone"
	// KindName is the same name together with another overlapping detail.
	KindName Kind = "name"
)

// kindRank orders reasons of equal score, the most telling kind first.
var kindRank = map[Kind]int{KindEmail: 0, KindPhone: 1, KindName: 2}

// Scores of each reason. A pair's score is their sum, capped at MaxScore.
const (
	scoreEmail   = 100
	scorePhone   = 80
	scoreName    = 40
	scoreOverlap = 20
	// MaxScore is the score of the surest match.
	MaxScore = 100
	// MinScore is the lowest score a pair needs to be offered.
	MinScore = 60
	// maxBucket is the largest number of contacts that share one value before
	// the value is taken as a role address or a switchboard number and skipped.
	maxBucket = 20
)

// Overlap names a detail two contacts with the same name also share.
type Overlap string

const (
	// OverlapOrganization is the same organization.
	OverlapOrganization Overlap = "organization"
	// OverlapDomain is an email address at the same company domain.
	OverlapDomain Overlap = "domain"
	// OverlapEmpty is one of the two having no address or number at all.
	OverlapEmpty Overlap = "empty"
)

// Reason is one thing a group has in common.
type Reason struct {
	Kind Kind
	// Value is the shared address, number or name, as the first contact wrote it.
	Value string
	// Overlap lists what backs up a shared name; empty for the other kinds.
	Overlap []Overlap
	// Score is what the reason is worth.
	Score int
}

// Group is a set of contacts that look like one person.
type Group struct {
	// IDs are the contacts, in the order they were given.
	IDs []int64
	// Score is the highest score of any pair in the group, up to MaxScore.
	Score int
	// Reasons say why, strongest first.
	Reasons []Reason
}

type pairInfo struct {
	score   int
	reasons []Reason
}

// FindDuplicates groups the contacts that are probably the same person. Two
// contacts match on a shared email address (100), a shared phone number (80),
// or the same name when something else overlaps (40 plus 20 for each of the
// same organization, an address at the same company domain, or one of them
// having no address or number). Pairs below MinScore are dropped, as are pairs
// the user dismissed; the rest are joined into groups, so A matching B and B
// matching C is one group of three. A value shared by more than maxBucket
// contacts is skipped as a role address.
func FindDuplicates(contacts []storage.Contact, dismissed map[storage.ContactPair]bool) []Group {
	pairs := map[[2]int]*pairInfo{}
	add := func(i, j int, r Reason) {
		if i > j {
			i, j = j, i
		}
		p := pairs[[2]int{i, j}]
		if p == nil {
			p = &pairInfo{}
			pairs[[2]int{i, j}] = p
		}
		p.score += r.Score
		p.reasons = append(p.reasons, r)
	}

	emails := map[string][]int{}
	phones := map[string][]int{}
	names := map[string][]int{}
	for i, c := range contacts {
		seenEmail := map[string]bool{}
		for _, e := range c.Emails {
			if key := normalizeEmail(e.Value); key != "" && !seenEmail[key] {
				seenEmail[key] = true
				emails[key] = append(emails[key], i)
			}
		}
		seenPhone := map[string]bool{}
		for _, p := range c.Phones {
			if key := PhoneKey(p.Value); key != "" && !seenPhone[phoneBucket(key)] {
				seenPhone[phoneBucket(key)] = true
				phones[phoneBucket(key)] = append(phones[phoneBucket(key)], i)
			}
		}
		if key := normalizeName(c.FullName); key != "" {
			names[key] = append(names[key], i)
		}
	}

	for key, members := range emails {
		forPairs(members, func(i, j int) {
			add(i, j, Reason{Kind: KindEmail, Value: valueOf(contacts[i].Emails, func(v string) bool { return normalizeEmail(v) == key }), Score: scoreEmail})
		})
	}
	for _, members := range phones {
		forPairs(members, func(i, j int) {
			if shared, ok := sharedPhone(contacts[i], contacts[j]); ok {
				add(i, j, Reason{Kind: KindPhone, Value: shared, Score: scorePhone})
			}
		})
	}
	for _, members := range names {
		forPairs(members, func(i, j int) {
			overlap := nameOverlap(contacts[i], contacts[j])
			if len(overlap) > 0 {
				add(i, j, Reason{Kind: KindName, Value: contacts[i].FullName, Overlap: overlap, Score: scoreName + scoreOverlap*len(overlap)})
			}
		})
	}

	parent := make([]int, len(contacts))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	type kept struct {
		pair [2]int
		info *pairInfo
	}
	var keep []kept
	for pair, info := range pairs {
		if info.score < MinScore {
			continue
		}
		a, b := contacts[pair[0]], contacts[pair[1]]
		if dismissed[storage.NewContactPair(storage.ContactKey(a), storage.ContactKey(b))] {
			continue
		}
		keep = append(keep, kept{pair, info})
		parent[find(pair[0])] = find(pair[1])
	}

	byRoot := map[int]*Group{}
	var order []int
	seen := map[int]map[string]bool{}
	for _, k := range keep {
		root := find(k.pair[0])
		g := byRoot[root]
		if g == nil {
			g = &Group{}
			byRoot[root] = g
			order = append(order, root)
			seen[root] = map[string]bool{}
		}
		score := min(k.info.score, MaxScore)
		if score > g.Score {
			g.Score = score
		}
		for _, r := range k.info.reasons {
			id := string(r.Kind) + "|" + normalizeReasonValue(r)
			if !seen[root][id] {
				seen[root][id] = true
				g.Reasons = append(g.Reasons, r)
			}
		}
	}
	members := map[int][]int{}
	for i := range contacts {
		if root := find(i); byRoot[root] != nil {
			members[root] = append(members[root], i)
		}
	}
	groups := make([]Group, 0, len(order))
	for _, root := range order {
		g := byRoot[root]
		for _, i := range members[root] {
			g.IDs = append(g.IDs, contacts[i].ID)
		}
		sort.SliceStable(g.Reasons, func(a, b int) bool {
			if g.Reasons[a].Score != g.Reasons[b].Score {
				return g.Reasons[a].Score > g.Reasons[b].Score
			}
			if g.Reasons[a].Kind != g.Reasons[b].Kind {
				return kindRank[g.Reasons[a].Kind] < kindRank[g.Reasons[b].Kind]
			}
			return g.Reasons[a].Value < g.Reasons[b].Value
		})
		groups = append(groups, *g)
	}
	first := map[int64]int{}
	for i, c := range contacts {
		first[c.ID] = i
	}
	sort.SliceStable(groups, func(a, b int) bool {
		if groups[a].Score != groups[b].Score {
			return groups[a].Score > groups[b].Score
		}
		return first[groups[a].IDs[0]] < first[groups[b].IDs[0]]
	})
	return groups
}

// forPairs calls fn for every pair in a bucket, unless the bucket is too big to
// mean anything.
func forPairs(members []int, fn func(i, j int)) {
	if len(members) < 2 || len(members) > maxBucket {
		return
	}
	for a := 0; a < len(members); a++ {
		for b := a + 1; b < len(members); b++ {
			fn(members[a], members[b])
		}
	}
}

func valueOf(values []storage.ContactValue, match func(string) bool) string {
	for _, v := range values {
		if match(v.Value) {
			return v.Value
		}
	}
	return ""
}

func normalizeReasonValue(r Reason) string {
	switch r.Kind {
	case KindEmail:
		return normalizeEmail(r.Value)
	case KindPhone:
		return PhoneKey(r.Value)
	default:
		return normalizeName(r.Value)
	}
}

// sharedPhone returns a's number that b also has.
func sharedPhone(a, b storage.Contact) (string, bool) {
	for _, pa := range a.Phones {
		for _, pb := range b.Phones {
			if PhonesMatch(pa.Value, pb.Value) {
				return pa.Value, true
			}
		}
	}
	return "", false
}

// nameOverlap lists what two contacts with the same name also share.
func nameOverlap(a, b storage.Contact) []Overlap {
	var out []Overlap
	if oa := normalizeOrg(a.Organization); oa != "" && oa == normalizeOrg(b.Organization) {
		out = append(out, OverlapOrganization)
	}
	if sharesDomain(a, b) {
		out = append(out, OverlapDomain)
	}
	if bare(a) || bare(b) {
		out = append(out, OverlapEmpty)
	}
	return out
}

func sharesDomain(a, b storage.Contact) bool {
	have := map[string]bool{}
	for _, e := range a.Emails {
		if d := emailDomain(normalizeEmail(e.Value)); d != "" && !freeMail[d] {
			have[d] = true
		}
	}
	for _, e := range b.Emails {
		if have[emailDomain(normalizeEmail(e.Value))] {
			return true
		}
	}
	return false
}

// bare says a contact has no address and no number to tell it from another.
func bare(c storage.Contact) bool {
	return len(c.Emails) == 0 && len(c.Phones) == 0
}
