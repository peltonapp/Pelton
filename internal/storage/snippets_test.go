package storage

import (
	"context"
	"errors"
	"testing"
)

func snippetNames(t *testing.T, db *DB) []string {
	t.Helper()
	list, err := db.ListSnippets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}

func TestSnippetCRUD(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	thanks := Snippet{Name: "Thanks", Body: "Thanks, {first name}!", Trigger: "thanks", Subject: "Re", To: "a@x", Cc: "b@x"}
	if _, err := db.CreateSnippet(ctx, &thanks); err != nil {
		t.Fatal(err)
	}
	intro := Snippet{Name: "Intro", Body: "Hello"}
	if _, err := db.CreateSnippet(ctx, &intro); err != nil {
		t.Fatal(err)
	}
	if thanks.Position != 0 || intro.Position != 1 {
		t.Fatalf("positions %d, %d, want 0 and 1", thanks.Position, intro.Position)
	}

	list, err := db.ListSnippets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Trigger != "thanks" || list[0].Subject != "Re" || list[0].To != "a@x" || list[0].Cc != "b@x" {
		t.Fatalf("list = %+v", list)
	}

	thanks.Body = "Many thanks"
	thanks.Trigger = ""
	if err := db.UpdateSnippet(ctx, &thanks); err != nil {
		t.Fatal(err)
	}
	list, _ = db.ListSnippets(ctx)
	if list[0].Body != "Many thanks" || list[0].Trigger != "" || list[0].Position != 0 {
		t.Fatalf("after update = %+v", list[0])
	}

	if err := db.DeleteSnippet(ctx, intro.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSnippet(ctx, intro.ID); !errors.Is(err, ErrSnippetNotFound) {
		t.Fatalf("second delete = %v", err)
	}
	missing := Snippet{ID: intro.ID, Name: "x"}
	if err := db.UpdateSnippet(ctx, &missing); !errors.Is(err, ErrSnippetNotFound) {
		t.Fatalf("update of a deleted snippet = %v", err)
	}
}

func TestReorderSnippets(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	ids := map[string]int64{}
	for _, name := range []string{"a", "b", "c"} {
		s := Snippet{Name: name}
		if _, err := db.CreateSnippet(ctx, &s); err != nil {
			t.Fatal(err)
		}
		ids[name] = s.ID
	}

	if err := db.ReorderSnippets(ctx, []int64{ids["c"], ids["a"], ids["b"]}); err != nil {
		t.Fatal(err)
	}
	if got := snippetNames(t, db); got[0] != "c" || got[1] != "a" || got[2] != "b" {
		t.Fatalf("order = %v", got)
	}

	if err := db.ReorderSnippets(ctx, []int64{ids["b"]}); err != nil {
		t.Fatal(err)
	}
	if got := snippetNames(t, db); got[0] != "b" || got[1] != "c" || got[2] != "a" {
		t.Fatalf("order after a partial reorder = %v", got)
	}

	d := Snippet{Name: "d"}
	if _, err := db.CreateSnippet(ctx, &d); err != nil {
		t.Fatal(err)
	}
	if got := snippetNames(t, db); got[3] != "d" {
		t.Fatalf("a new snippet is not last: %v", got)
	}
}
