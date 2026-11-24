package contactmerge

import (
	"reflect"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func contact(id int64, name string, emails, phones []string) storage.Contact {
	c := storage.Contact{ID: id, BookID: 1, Path: "/b/" + name + ".vcf", UID: name, FullName: name}
	for _, e := range emails {
		c.Emails = append(c.Emails, storage.ContactValue{Value: e})
	}
	for _, p := range phones {
		c.Phones = append(c.Phones, storage.ContactValue{Value: p})
	}
	return c
}

func ids(groups []Group) [][]int64 {
	out := make([][]int64, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.IDs)
	}
	return out
}

func TestFindDuplicatesByEmail(t *testing.T) {
	list := []storage.Contact{
		contact(1, "Ada Lovelace", []string{"ada@example.com"}, nil),
		contact(2, "A. Lovelace", []string{"ADA@Example.com", "ada@home.example"}, nil),
		contact(3, "Grace Hopper", []string{"grace@example.com"}, nil),
	}
	groups := FindDuplicates(list, nil)
	if got, want := ids(groups), [][]int64{{1, 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %v, want %v", got, want)
	}
	g := groups[0]
	if g.Score != MaxScore {
		t.Errorf("score = %d, want %d", g.Score, MaxScore)
	}
	if len(g.Reasons) != 1 || g.Reasons[0].Kind != KindEmail || g.Reasons[0].Value != "ada@example.com" {
		t.Errorf("reasons = %+v", g.Reasons)
	}
}

func TestFindDuplicatesByPhone(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"same digits, different punctuation", "+31 6 1234 5678", "+31-6-12345678", true},
		{"national and international", "06 1234 5678", "+31 6 1234 5678", true},
		{"double zero prefix", "0031 6 1234 5678", "06 1234 5678", true},
		{"different numbers", "+31 6 1234 5678", "+31 6 1234 9999", false},
		{"too short to compare", "112", "112", false},
		{"extensions are not numbers", "1234", "1234", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := []storage.Contact{
				contact(1, "One Person", nil, []string{tt.a}),
				contact(2, "Another Name", nil, []string{tt.b}),
			}
			groups := FindDuplicates(list, nil)
			if got := len(groups) == 1; got != tt.want {
				t.Fatalf("grouped = %v, want %v (%+v)", got, tt.want, groups)
			}
			if tt.want && groups[0].Reasons[0].Kind != KindPhone {
				t.Errorf("kind = %q", groups[0].Reasons[0].Kind)
			}
		})
	}
}

func TestFindDuplicatesByNameNeedsAnOverlap(t *testing.T) {
	withOrg := func(c storage.Contact, org string) storage.Contact {
		c.Organization = org
		return c
	}
	tests := []struct {
		name string
		list []storage.Contact
		want [][]int64
	}{
		{
			"same name, different people",
			[]storage.Contact{
				contact(1, "John Smith", []string{"john@a.example"}, nil),
				contact(2, "John Smith", []string{"jsmith@b.example"}, nil),
			},
			[][]int64{},
		},
		{
			"same name and organization",
			[]storage.Contact{
				withOrg(contact(1, "John Smith", []string{"john@a.example"}, nil), "Acme GmbH"),
				withOrg(contact(2, "Smith, John", []string{"jsmith@b.example"}, nil), "ACME gmbh"),
			},
			[][]int64{{1, 2}},
		},
		{
			"same name and company domain",
			[]storage.Contact{
				contact(1, "John Smith", []string{"john@acme.example"}, nil),
				contact(2, "John Smith", []string{"j.smith@acme.example"}, nil),
			},
			[][]int64{{1, 2}},
		},
		{
			"a shared mail provider is not an overlap",
			[]storage.Contact{
				contact(1, "John Smith", []string{"john@gmail.com"}, nil),
				contact(2, "John Smith", []string{"js@gmail.com"}, nil),
			},
			[][]int64{},
		},
		{
			"one has nothing but a name",
			[]storage.Contact{
				contact(1, "John Smith", []string{"john@a.example"}, nil),
				contact(2, "John Smith", nil, nil),
			},
			[][]int64{{1, 2}},
		},
		{
			"accents and case are ignored",
			[]storage.Contact{
				contact(1, "Jörg Müller", nil, nil),
				contact(2, "JORG MULLER", []string{"joerg@a.example"}, nil),
			},
			[][]int64{{1, 2}},
		},
		{
			"a name that is an address is not a name",
			[]storage.Contact{
				contact(1, "a@b.example", nil, nil),
				contact(2, "a@b.example", nil, nil),
			},
			[][]int64{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(FindDuplicates(tt.list, nil))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("groups = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindDuplicatesScoresAndReasons(t *testing.T) {
	a := contact(1, "John Smith", []string{"john@acme.example"}, []string{"+31 6 1234 5678"})
	b := contact(2, "John Smith", []string{"john@acme.example"}, []string{"06 1234 5678"})
	a.Organization, b.Organization = "Acme", "Acme"
	groups := FindDuplicates([]storage.Contact{a, b}, nil)
	if len(groups) != 1 {
		t.Fatalf("groups = %+v", groups)
	}
	g := groups[0]
	if g.Score != MaxScore {
		t.Errorf("score = %d", g.Score)
	}
	var kinds []Kind
	for _, r := range g.Reasons {
		kinds = append(kinds, r.Kind)
	}
	if want := []Kind{KindEmail, KindPhone, KindName}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("reasons = %v, want strongest first %v", kinds, want)
	}
	name := g.Reasons[2]
	if want := []Overlap{OverlapOrganization, OverlapDomain}; !reflect.DeepEqual(name.Overlap, want) {
		t.Errorf("overlap = %v, want %v", name.Overlap, want)
	}
}

func TestFindDuplicatesJoinsChains(t *testing.T) {
	list := []storage.Contact{
		contact(1, "Ada One", []string{"ada@a.example"}, nil),
		contact(2, "Ada Two", []string{"ada@a.example", "ada@b.example"}, nil),
		contact(3, "Ada Three", []string{"ada@b.example"}, nil),
		contact(4, "Someone Else", []string{"else@a.example"}, nil),
	}
	got := ids(FindDuplicates(list, nil))
	if want := [][]int64{{1, 2, 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestFindDuplicatesOrdersSurestFirst(t *testing.T) {
	list := []storage.Contact{
		contact(1, "John Smith", []string{"a@acme.example"}, nil),
		contact(2, "John Smith", []string{"b@acme.example"}, nil),
		contact(3, "Ada", []string{"ada@x.example"}, nil),
		contact(4, "Ada L", []string{"ada@x.example"}, nil),
	}
	got := ids(FindDuplicates(list, nil))
	if want := [][]int64{{3, 4}, {1, 2}}; !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestFindDuplicatesHonoursDismissals(t *testing.T) {
	a := contact(1, "Ada", []string{"ada@x.example"}, nil)
	b := contact(2, "Ada L", []string{"ada@x.example"}, nil)
	c := contact(3, "Ada K", []string{"ada@x.example"}, nil)
	dismissed := map[storage.ContactPair]bool{
		storage.NewContactPair(storage.ContactKey(b), storage.ContactKey(a)): true,
	}
	got := ids(FindDuplicates([]storage.Contact{a, b, c}, dismissed))
	// a and b are dismissed as a pair, but both still match c.
	if want := [][]int64{{1, 2, 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
	dismissed[storage.NewContactPair(storage.ContactKey(a), storage.ContactKey(c))] = true
	dismissed[storage.NewContactPair(storage.ContactKey(b), storage.ContactKey(c))] = true
	if got := FindDuplicates([]storage.Contact{a, b, c}, dismissed); len(got) != 0 {
		t.Errorf("all pairs dismissed, got %+v", got)
	}
}

func TestFindDuplicatesSkipsRoleAddresses(t *testing.T) {
	var list []storage.Contact
	for i := int64(1); i <= maxBucket+1; i++ {
		list = append(list, contact(i, "Person "+string(rune('A'+i)), []string{"info@corp.example"}, nil))
	}
	if got := FindDuplicates(list, nil); len(got) != 0 {
		t.Errorf("a shared role address grouped %d groups", len(got))
	}
}

func TestPhoneKeyAndMatch(t *testing.T) {
	if got := PhoneKey("+31 (0) 6-1234 5678"); got != "310612345678" {
		t.Errorf("PhoneKey = %q", got)
	}
	if PhoneKey("12345") != "" {
		t.Error("a short number must not produce a key")
	}
	if !PhonesMatch("0612345678", "+31612345678") {
		t.Error("national form should match international")
	}
	if PhonesMatch("0612345678", "0612345679") {
		t.Error("different numbers matched")
	}
}
