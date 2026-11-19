package desktop

import (
	"errors"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestSenderReadingBindings(t *testing.T) {
	a := newVIPTestApp(t)

	if err := a.SetSenderColors("News@Example.com", "original"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSenderFontSize("news@example.com", 18); err != nil {
		t.Fatal(err)
	}
	got, err := a.ListSenderReading()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (SenderReadingDTO{Address: "news@example.com", Colors: "original", FontSize: 18}) {
		t.Fatalf("list = %+v", got)
	}

	if err := a.SetSenderColors("news@example.com", "sepia"); !errors.Is(err, storage.ErrSenderReading) {
		t.Errorf("unknown mode err = %v", err)
	}

	n, err := a.ResetSenderReading()
	if err != nil || n != 1 {
		t.Fatalf("reset = %d, %v", n, err)
	}
	if got, _ := a.ListSenderReading(); len(got) != 0 {
		t.Errorf("after reset = %+v", got)
	}
}

func TestSenderReadingEmptyListIsNotNil(t *testing.T) {
	a := newVIPTestApp(t)
	got, err := a.ListSenderReading()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("an empty list must reach the frontend as [], not null")
	}
}
