package desktop

import (
	"errors"
	"testing"
)

func TestNormalizeTrigger(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr error
	}{
		{raw: "", want: ""},
		{raw: " thanks ", want: "thanks"},
		{raw: ";Thanks", want: "thanks"},
		{raw: "sig-2_b", want: "sig-2_b"},
		{raw: "danke", want: "danke"},
		{raw: "größe", want: "größe"},
		{raw: "two words", wantErr: ErrSnippetTriggerInvalid},
		{raw: "a;b", wantErr: ErrSnippetTriggerInvalid},
	}
	for _, tc := range tests {
		got, err := normalizeTrigger(tc.raw)
		if !errors.Is(err, tc.wantErr) || got != tc.want {
			t.Errorf("normalizeTrigger(%q) = %q, %v; want %q, %v", tc.raw, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestSaveSnippetChecksNameAndTrigger(t *testing.T) {
	app, _, _ := pgpTestApp(t, nil, nil)

	if _, err := app.SaveSnippet(SnippetDTO{Name: "  "}); !errors.Is(err, ErrSnippetNameEmpty) {
		t.Fatalf("empty name = %v", err)
	}
	thanks, err := app.SaveSnippet(SnippetDTO{Name: " Thanks ", Body: "Thanks, {first name}.", Trigger: ";Thanks"})
	if err != nil {
		t.Fatal(err)
	}
	if thanks.ID == 0 || thanks.Name != "Thanks" || thanks.Trigger != "thanks" {
		t.Fatalf("saved %+v", thanks)
	}
	if _, err := app.SaveSnippet(SnippetDTO{Name: "Other", Trigger: "THANKS"}); !errors.Is(err, ErrSnippetTriggerTaken) {
		t.Fatalf("duplicate trigger = %v", err)
	}
	thanks.Body = "Many thanks."
	if _, err := app.SaveSnippet(thanks); err != nil {
		t.Fatalf("saving a snippet again with its own trigger: %v", err)
	}

	other, err := app.SaveSnippet(SnippetDTO{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ReorderSnippets([]int64{other.ID, thanks.ID}); err != nil {
		t.Fatal(err)
	}
	list, err := app.ListSnippets()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != other.ID || list[1].Body != "Many thanks." {
		t.Fatalf("list = %+v", list)
	}
	if err := app.DeleteSnippet(other.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSnippetBackupRoundTrip(t *testing.T) {
	from, _, _ := pgpTestApp(t, nil, nil)
	for _, s := range []SnippetDTO{
		{Name: "Thanks", Body: "Thanks!", Trigger: "thanks", Subject: "Re", To: "a@x"},
		{Name: "Intro", Body: "Hello {first name}"},
	} {
		if _, err := from.SaveSnippet(s); err != nil {
			t.Fatal(err)
		}
	}
	exported, err := from.exportSnippets()
	if err != nil {
		t.Fatal(err)
	}

	to, _, _ := pgpTestApp(t, nil, nil)
	if _, err := to.SaveSnippet(SnippetDTO{Name: "Local", Trigger: "thanks"}); err != nil {
		t.Fatal(err)
	}
	if _, err := to.SaveSnippet(SnippetDTO{Name: "Intro", Body: "mine"}); err != nil {
		t.Fatal(err)
	}
	if err := to.importSnippets(exported); err != nil {
		t.Fatal(err)
	}
	if err := to.importSnippets(exported); err != nil {
		t.Fatal(err)
	}
	list, err := to.ListSnippets()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("list after two imports = %+v", list)
	}
	got := list[2]
	if got.Name != "Thanks" || got.Body != "Thanks!" || got.Subject != "Re" || got.To != "a@x" || got.Trigger != "" {
		t.Fatalf("imported %+v, want the local trigger kept and the copy without it", got)
	}
	if list[1].Body != "mine" {
		t.Fatalf("a local snippet of the same name was replaced: %+v", list[1])
	}
}
