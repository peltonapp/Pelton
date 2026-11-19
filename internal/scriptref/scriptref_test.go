package scriptref_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
	"github.com/peltonapp/Pelton/internal/scriptref"
)

// registerWeather stands in for a package outside scripting that adds its
// own entries, the way the spaces and web packages do.
func registerWeather(r *scripting.Registry) error {
	if err := r.RegisterPermission(scripting.Permission{
		Meta: scripting.Meta{ID: "weather", Label: "Weather", Description: "Look up the forecast."},
		Risk: "low",
	}); err != nil {
		return err
	}
	city := scripting.Required(scripting.NewParam("city", scripting.TypeString))
	city.Label = "City"
	return r.RegisterModule(scripting.Module{
		Meta: scripting.Meta{ID: "weather", Label: "Weather"},
		Functions: []scripting.Function{{
			Name:        "forecast",
			Description: "Returns tomorrow's forecast for a city.",
			Params:      []scripting.Param{city},
			Returns:     []scripting.Field{{Name: "summary", Type: "string", Label: "Summary"}},
			Permission:  "weather",
			Handler:     func(*scripting.Call) ([]any, error) { return []any{"sunny"}, nil },
		}},
	})
}

func build(t *testing.T) *scriptref.Reference {
	t.Helper()
	return scriptref.Build(scriptingtest.Registry(t, registerWeather))
}

func TestReferenceListsOutsideEntries(t *testing.T) {
	ref := build(t)
	mods, ok := ref.Section("modules")
	if !ok {
		t.Fatal("no modules section")
	}
	for _, want := range []string{"weather.forecast(city)", "Returns tomorrow's forecast for a city.", "Needs permission weather"} {
		if !strings.Contains(mods.Body, want) {
			t.Errorf("modules section lacks %q", want)
		}
	}
	perms, _ := ref.Section("permissions")
	if !strings.Contains(perms.Body, "### weather (Weather)") || !strings.Contains(perms.Body, "Risk: low.") {
		t.Errorf("permissions section lacks the outside permission:\n%s", perms.Body)
	}
	hits := ref.Search("forecast", 5)
	if len(hits) == 0 || hits[0].ID != "weather.forecast" {
		t.Fatalf("search did not rank the function first: %+v", hits)
	}
}

func TestReferenceCoversEverySection(t *testing.T) {
	ref := build(t)
	want := []string{"overview", "lua", "modules", "triggers", "conditions", "actions", "values", "permissions", "blocks", "settings", "ui", "placements", "limits", "format"}
	got := ref.Sections()
	if len(got) != len(want) {
		t.Fatalf("got %d sections, want %d", len(got), len(want))
	}
	for i, s := range got {
		if s.ID != want[i] || s.Title == "" || s.Body != "" {
			t.Errorf("section %d: %+v", i, s)
		}
	}
	text := ref.Text()
	for _, want := range []string{
		"### mail.arrived", "Event fields:", "- message: message",
		"#### mail.move(message, folder)", "Needs permission mail.modify",
		"### mail.from", "one of contains, equals, matches, starts, ends",
		"Risk: high.", "SETTINGS = {", "RE2", ".peltonscript", "question kind choice", "place palette",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("reference lacks %q", want)
		}
	}
	if _, ok := ref.Section("nope"); ok {
		t.Error("an unknown section was found")
	}
}

// The worked example is what an assistant copies, so it has to compile.
func TestBlockExampleCompiles(t *testing.T) {
	r := scriptingtest.Registry(t)
	tree, err := scripting.ParseTree([]byte(scriptref.BlockExample))
	if err != nil {
		t.Fatal(err)
	}
	res := scripting.Compile(r, tree, scripting.TriggerMailArrived, false)
	if len(res.Errors) > 0 {
		t.Fatalf("example does not compile: %v", res.Errors)
	}
}

func TestSearchNeedsEveryWord(t *testing.T) {
	ref := build(t)
	if hits := ref.Search("mail move", 0); len(hits) == 0 || hits[0].ID != "mail.move" {
		t.Fatalf("hits %+v", hits)
	}
	if hits := ref.Search("forecast zebra", 0); len(hits) != 0 {
		t.Fatalf("matched a word that is nowhere: %+v", hits)
	}
	if hits := ref.Search("  ", 0); hits != nil {
		t.Fatal("an empty query matched")
	}
}

// The built-ins carry their English text from registration, which is the
// same text the frontend's catalog has.
func TestReferenceHasTheBuiltinsEnglish(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "locales", "en.ts"))
	if err != nil {
		t.Skip("frontend catalog not found:", err)
	}
	f, _ := scriptingtest.Registry(t).Function("mail.move")
	if f.Description == "" || !strings.Contains(string(data), f.Description) {
		t.Fatalf("mail.move has no description matching en.ts: %q", f.Description)
	}
	mods, _ := scriptref.Build(scriptingtest.Registry(t)).Section("modules")
	if !strings.Contains(mods.Body, f.Description) {
		t.Fatal("the reference leaves out mail.move's description")
	}
}
