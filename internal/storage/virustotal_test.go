package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCachedVerdictRoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	want := Verdict{Status: "flagged", Malicious: 3, Suspicious: 1, Total: 70, Permalink: "https://example.test/report"}
	if err := db.CacheVerdict(ctx, VerdictKindURL, "https://example.com/a", want); err != nil {
		t.Fatalf("CacheVerdict: %v", err)
	}

	got, err := db.CachedVerdict(ctx, VerdictKindURL, "https://example.com/a", time.Hour)
	if err != nil {
		t.Fatalf("CachedVerdict: %v", err)
	}
	if got.Status != want.Status || got.Malicious != want.Malicious ||
		got.Suspicious != want.Suspicious || got.Total != want.Total || got.Permalink != want.Permalink {
		t.Errorf("got %+v, want %+v", *got, want)
	}
	if got.CheckedAt.IsZero() {
		t.Error("CheckedAt was not filled in")
	}
}

// A url and a file digest live in one table, so the kind has to be part of the
// key: a lookup for one must never return the other's verdict.
func TestCachedVerdictSeparatesKinds(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	const target = "same-string"
	if err := db.CacheVerdict(ctx, VerdictKindURL, target, Verdict{Status: "clean", Total: 70}); err != nil {
		t.Fatalf("CacheVerdict url: %v", err)
	}
	if err := db.CacheVerdict(ctx, VerdictKindFile, target, Verdict{Status: "flagged", Malicious: 9, Total: 70}); err != nil {
		t.Fatalf("CacheVerdict file: %v", err)
	}

	url, err := db.CachedVerdict(ctx, VerdictKindURL, target, time.Hour)
	if err != nil {
		t.Fatalf("CachedVerdict url: %v", err)
	}
	if url.Status != "clean" {
		t.Errorf("url status = %q, want clean", url.Status)
	}
	file, err := db.CachedVerdict(ctx, VerdictKindFile, target, time.Hour)
	if err != nil {
		t.Fatalf("CachedVerdict file: %v", err)
	}
	if file.Status != "flagged" {
		t.Errorf("file status = %q, want flagged", file.Status)
	}
}

func TestCacheVerdictReplacesTheEarlierResult(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.CacheVerdict(ctx, VerdictKindFile, "abc", Verdict{Status: "unknown"}); err != nil {
		t.Fatalf("CacheVerdict: %v", err)
	}
	if err := db.CacheVerdict(ctx, VerdictKindFile, "abc", Verdict{Status: "flagged", Malicious: 2, Total: 70}); err != nil {
		t.Fatalf("CacheVerdict again: %v", err)
	}

	got, err := db.CachedVerdict(ctx, VerdictKindFile, "abc", time.Hour)
	if err != nil {
		t.Fatalf("CachedVerdict: %v", err)
	}
	if got.Status != "flagged" || got.Malicious != 2 {
		t.Errorf("got %+v, want the newer flagged verdict", *got)
	}
}

func TestCachedVerdictExpiry(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	stale := Verdict{Status: "clean", Total: 70, CheckedAt: time.Now().UTC().Add(-48 * time.Hour)}
	if err := db.CacheVerdict(ctx, VerdictKindURL, "https://example.com/", stale); err != nil {
		t.Fatalf("CacheVerdict: %v", err)
	}

	if _, err := db.CachedVerdict(ctx, VerdictKindURL, "https://example.com/", 24*time.Hour); !errors.Is(err, ErrVerdictNotCached) {
		t.Errorf("an entry older than maxAge was returned: %v", err)
	}
	if _, err := db.CachedVerdict(ctx, VerdictKindURL, "https://example.com/", 72*time.Hour); err != nil {
		t.Errorf("an entry within maxAge was not returned: %v", err)
	}
	// a rescan passes no age budget, which must bypass the cache entirely
	// rather than being read as "any age will do".
	if _, err := db.CachedVerdict(ctx, VerdictKindURL, "https://example.com/", 0); !errors.Is(err, ErrVerdictNotCached) {
		t.Errorf("maxAge of 0 should skip the cache: %v", err)
	}
}

func TestCachedVerdictMissing(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if _, err := db.CachedVerdict(context.Background(), VerdictKindURL, "never-scanned", time.Hour); !errors.Is(err, ErrVerdictNotCached) {
		t.Errorf("err = %v, want ErrVerdictNotCached", err)
	}
}

func TestClearVerdicts(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.CacheVerdict(ctx, VerdictKindURL, "https://example.com/", Verdict{Status: "clean", Total: 70}); err != nil {
		t.Fatalf("CacheVerdict: %v", err)
	}
	if err := db.ClearVerdicts(ctx); err != nil {
		t.Fatalf("ClearVerdicts: %v", err)
	}
	if _, err := db.CachedVerdict(ctx, VerdictKindURL, "https://example.com/", time.Hour); !errors.Is(err, ErrVerdictNotCached) {
		t.Errorf("a verdict survived ClearVerdicts: %v", err)
	}
}
