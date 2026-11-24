package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestRevocationRoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.RevocationFor(ctx, "nothing"); !errors.Is(err, ErrRevocationNotCached) {
		t.Fatalf("err = %v, want ErrRevocationNotCached", err)
	}

	revoked := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	checked := time.Now().UTC().Truncate(time.Second)
	next := checked.Add(time.Hour)
	want := RevocationRecord{
		Fingerprint: "abc123",
		Status:      "revoked",
		Detail:      "the issuing authority has withdrawn this certificate",
		RevokedAt:   revoked,
		CheckedAt:   checked,
		NextUpdate:  next,
	}
	if err := db.SaveRevocation(ctx, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := db.RevocationFor(ctx, "abc123")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status != want.Status || got.Detail != want.Detail {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !got.RevokedAt.Equal(revoked) || !got.NextUpdate.Equal(next) {
		t.Errorf("timestamps did not survive: %+v", got)
	}
}

// a second answer for the same certificate replaces the first rather than
// piling up, or the cache would grow without bound and read back the oldest.
func TestSaveRevocationReplaces(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Now()

	for _, status := range []string{"unknown", "good"} {
		if err := db.SaveRevocation(ctx, RevocationRecord{
			Fingerprint: "same", Status: status, CheckedAt: now,
		}); err != nil {
			t.Fatalf("save %s: %v", status, err)
		}
	}
	got, err := db.RevocationFor(ctx, "same")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status != "good" {
		t.Errorf("status = %q, want the newer answer", got.Status)
	}
}

// turning the setting off must leave no record of which authorities were asked
// about whom, which is the whole reason it is off by default.
func TestClearRevocations(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.SaveRevocation(ctx, RevocationRecord{
		Fingerprint: "gone", Status: "good", CheckedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := db.ClearRevocations(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := db.RevocationFor(ctx, "gone"); !errors.Is(err, ErrRevocationNotCached) {
		t.Fatalf("err = %v, want the row to be gone", err)
	}
}

func TestRevocationFreshness(t *testing.T) {
	t.Parallel()
	now := time.Now()
	cases := []struct {
		name string
		rec  RevocationRecord
		want bool
	}{
		{
			name: "within the authority's window",
			rec:  RevocationRecord{CheckedAt: now.Add(-time.Hour), NextUpdate: now.Add(time.Hour)},
			want: true,
		},
		{
			name: "past the authority's window",
			rec:  RevocationRecord{CheckedAt: now.Add(-2 * time.Hour), NextUpdate: now.Add(-time.Hour)},
			want: false,
		},
		{
			name: "no window given, inside the fallback",
			rec:  RevocationRecord{CheckedAt: now.Add(-time.Hour)},
			want: true,
		},
		{
			name: "no window given, past the fallback",
			rec:  RevocationRecord{CheckedAt: now.Add(-48 * time.Hour)},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rec.Fresh(now, 24*time.Hour); got != tc.want {
				t.Errorf("Fresh = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEncodeDecodeCerts(t *testing.T) {
	t.Parallel()
	certs := [][]byte{[]byte("first"), []byte("second and longer")}
	got := DecodeCerts(EncodeCerts(certs))
	if len(got) != 2 {
		t.Fatalf("got %d certificates, want 2", len(got))
	}
	for i := range certs {
		if !bytes.Equal(got[i], certs[i]) {
			t.Errorf("certificate %d came back as %q", i, got[i])
		}
	}
	if DecodeCerts(nil) != nil {
		t.Error("an empty blob must decode to nothing")
	}
	// half a chain cannot be checked, so a truncated blob yields nothing rather
	// than a partial answer built on it.
	if DecodeCerts([]byte{0, 0, 0, 9, 'a', 'b'}) != nil {
		t.Error("a truncated blob must decode to nothing")
	}
}
