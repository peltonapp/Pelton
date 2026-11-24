package mailimport

import (
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
)

func testRegistry(t *testing.T) *scripting.Registry {
	t.Helper()
	r := scripting.NewRegistry()
	if err := scripting.RegisterBuiltins(r); err != nil {
		t.Fatal(err)
	}
	return r
}

func filterFixture(t *testing.T) []Filter {
	t.Helper()
	text := strings.Join([]string{
		`version="9"`,
		`name="Receipts"`,
		`enabled="yes"`,
		`type="17"`,
		`action="Move to folder"`,
		`actionValue="imap://wren%40example.org@imap.example.org/INBOX/Receipts"`,
		`action="Mark read"`,
		`action="Stop execution"`,
		`condition="AND (from,contains,shop@example.com) AND (subject,doesn't contain,\"reminder, weekly\")"`,
		`name="Everything"`,
		`type="16"`,
		`action="Mark flagged"`,
		`condition="ALL"`,
		`name="Mixed"`,
		`type="1"`,
		`action="Delete"`,
		`condition="AND (from,is,a@example.com) OR (to,is,b@example.com)"`,
		`name="Tag only"`,
		`type="1"`,
		`action="AddTag"`,
		`actionValue="$label1"`,
		`condition="AND (subject,contains,x)"`,
		`name="To local"`,
		`type="1"`,
		`action="Move to folder"`,
		`actionValue="mailbox://nobody@Local%20Folders/Archive"`,
		`action="Mark unread"`,
		`condition="OR (\"X-Mailer\",begins with,Bulk) OR (size,is greater than,500) OR (has attachment status,isn't,true)"`,
		`name="Outgoing"`,
		`type="64"`,
		`action="Mark read"`,
		`condition="ALL"`,
	}, "\n")
	filters, err := parseFilterRules(strings.NewReader(text), "rules", "wren@example.org")
	if err != nil {
		t.Fatal(err)
	}
	return filters
}

func TestParseFilterRules(t *testing.T) {
	filters := filterFixture(t)
	if len(filters) != 6 {
		t.Fatalf("%d filters", len(filters))
	}
	f := filters[0]
	if f.Name != "Receipts" || !f.Enabled || f.Type != 17 || len(f.Actions) != 3 ||
		f.Actions[0].Value != "imap://wren%40example.org@imap.example.org/INBOX/Receipts" {
		t.Fatalf("filter = %+v", f)
	}
	if !strings.Contains(f.Condition, `"reminder, weekly"`) {
		t.Fatalf("condition lost its escaped quotes: %q", f.Condition)
	}
	if filters[1].Enabled {
		t.Fatal("a filter with no enabled line came back enabled")
	}
}

func TestMapFiltersCompile(t *testing.T) {
	reg := testRegistry(t)
	filters := filterFixture(t)
	results := map[string]FilterMapping{}
	for _, f := range filters {
		results[f.Name] = MapFilter(f, 7)
	}

	receipts := results["Receipts"]
	if receipts.Unmappable || receipts.Trigger != scripting.TriggerMailArrived || len(receipts.Dropped) != 0 {
		t.Fatalf("receipts = %+v", receipts)
	}
	compiled := scripting.Compile(reg, receipts.Tree, receipts.Trigger, false)
	if len(compiled.Errors) > 0 {
		t.Fatalf("receipts does not compile: %+v", compiled.Errors)
	}
	for _, want := range []string{`"INBOX/Receipts"`, `"shop@example.com"`, `"reminder, weekly"`, "not (", "== 7"} {
		if !strings.Contains(compiled.Lua, want) {
			t.Errorf("receipts Lua lacks %s:\n%s", want, compiled.Lua)
		}
	}

	all := results["Everything"]
	if all.Trigger != scripting.TriggerManual || len(all.Tree.Blocks) != 1 || all.Tree.Blocks[0].Type != "mail.flag" {
		t.Fatalf("a manual filter matching all mail = %+v", all)
	}
	if c := scripting.Compile(reg, all.Tree, all.Trigger, false); len(c.Errors) > 0 {
		t.Fatalf("everything does not compile: %+v", c.Errors)
	}

	if m := results["Mixed"]; !m.Unmappable || !strings.Contains(m.Dropped[0], "AND and OR") {
		t.Fatalf("mixed = %+v", m)
	}
	if m := results["Tag only"]; !m.Unmappable || m.Dropped[0] != "AddTag: $label1" {
		t.Fatalf("tag only = %+v", m)
	}
	local := results["To local"]
	if local.Unmappable || len(local.Dropped) != 1 || !strings.HasPrefix(local.Dropped[0], "Move to folder") {
		t.Fatalf("to local = %+v", local)
	}
	if c := scripting.Compile(reg, local.Tree, local.Trigger, false); len(c.Errors) > 0 {
		t.Fatalf("to local does not compile: %+v", c.Errors)
	} else if !strings.Contains(c.Lua, `"X-Mailer"`) || !strings.Contains(c.Lua, "500") {
		t.Fatalf("to local Lua:\n%s", c.Lua)
	}
	if m := results["Outgoing"]; !m.Unmappable {
		t.Fatalf("a filter run on sending was mapped: %+v", m)
	}
}
