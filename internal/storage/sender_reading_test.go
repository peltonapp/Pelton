package storage

import (
	"context"
	"errors"
	"testing"
)

func TestSenderReadingRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.SetSenderColors(ctx, " Ada@Example.com ", SenderColorsOriginal); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSenderFontSize(ctx, "ada@example.com", 18); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSenderFontSize(ctx, "grace@example.com", 12); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListSenderReading(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []SenderReading{
		{Address: "ada@example.com", Colors: SenderColorsOriginal, FontSize: 18},
		{Address: "grace@example.com", FontSize: 12},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// one choice going back to its default keeps the other; both going back
// leaves no row behind.
func TestSenderReadingForgetsDefaults(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.SetSenderColors(ctx, "ada@example.com", SenderColorsAdapt); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSenderFontSize(ctx, "ada@example.com", 16); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSenderColors(ctx, "ada@example.com", SenderColorsDefault); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListSenderReading(ctx)
	if len(got) != 1 || got[0].Colors != "" || got[0].FontSize != 16 {
		t.Fatalf("after clearing colors = %+v", got)
	}
	if err := db.SetSenderFontSize(ctx, "ada@example.com", 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.ListSenderReading(ctx); len(got) != 0 {
		t.Fatalf("after clearing both = %+v", got)
	}
}

func TestSenderReadingRejectsBadInput(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	cases := []struct {
		name string
		err  error
	}{
		{"empty address", db.SetSenderColors(ctx, "  ", SenderColorsAdapt)},
		{"unknown mode", db.SetSenderColors(ctx, "ada@example.com", "invert")},
		{"size too small", db.SetSenderFontSize(ctx, "ada@example.com", 4)},
		{"size too large", db.SetSenderFontSize(ctx, "ada@example.com", 99)},
	}
	for _, c := range cases {
		if !errors.Is(c.err, ErrSenderReading) {
			t.Errorf("%s: err = %v", c.name, c.err)
		}
	}
	if got, _ := db.ListSenderReading(ctx); len(got) != 0 {
		t.Errorf("rejected input left rows: %+v", got)
	}
}

func TestResetSenderReading(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for _, addr := range []string{"a@example.com", "b@example.com"} {
		if err := db.SetSenderFontSize(ctx, addr, 20); err != nil {
			t.Fatal(err)
		}
	}
	n, err := db.ResetSenderReading(ctx)
	if err != nil || n != 2 {
		t.Fatalf("reset = %d, %v", n, err)
	}
	if got, _ := db.ListSenderReading(ctx); len(got) != 0 {
		t.Errorf("rows after reset = %+v", got)
	}
}
