package storage

import (
	"context"
	"errors"
	"testing"
)

// SetJSONsIn writes settings of several profiles at once, and when one cannot
// be written none of them is, so two values that must agree never disagree.
// GetJSONIn reads one profile's setting whichever profile the store is in.
func TestSetJSONsInWritesAllOrNothing(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	other := &Profile{Name: "Work"}
	if _, err := db.CreateProfile(ctx, other); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	here := db.SettingsScope()

	if err := db.SetJSONsIn(ctx, map[ScopedSetting]any{{here, "a"}: []int{1}, {other.ID, "b"}: "two"}); err != nil {
		t.Fatalf("SetJSONsIn: %v", err)
	}
	var a []int
	var b string
	if err := db.GetJSON(ctx, "a", &a); err != nil || len(a) != 1 || a[0] != 1 {
		t.Fatalf("a = %v, %v", a, err)
	}
	if err := db.GetJSON(ctx, "b", &b); !errors.Is(err, ErrSettingNotFound) {
		t.Fatalf("b read in this profile = %q, %v; want it only in the other", b, err)
	}
	if err := db.GetJSONIn(ctx, other.ID, "b", &b); err != nil || b != "two" {
		t.Fatalf("b = %q, %v", b, err)
	}

	if err := db.SetJSONsIn(ctx, map[ScopedSetting]any{{here, "a"}: []int{2}, {here, "c"}: make(chan int)}); err == nil {
		t.Fatal("SetJSONsIn of a value json cannot encode succeeded")
	}
	if err := db.GetJSON(ctx, "a", &a); err != nil || a[0] != 1 {
		t.Fatalf("a = %v, %v after a failed SetJSONsIn, want it unchanged", a, err)
	}
	if err := db.GetJSON(ctx, "c", &b); !errors.Is(err, ErrSettingNotFound) {
		t.Fatalf("c = %v, want it never written", err)
	}
}
