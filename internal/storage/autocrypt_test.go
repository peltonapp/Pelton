package storage

import (
	"context"
	"testing"
	"time"
)

func TestAutocryptPeerRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if _, ok, err := db.AutocryptPeerFor(ctx, "a@example.org"); ok || err != nil {
		t.Fatalf("missing peer = %v, %v", ok, err)
	}
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	p := AutocryptPeer{Addr: "a@example.org", LastSeen: now, AutocryptTimestamp: now, PublicKey: []byte{1, 2}, PreferEncrypt: "mutual"}
	if err := db.SaveAutocryptPeer(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.GossipKey, p.GossipTimestamp = []byte{3}, now.Add(time.Hour)
	if err := db.SaveAutocryptPeer(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := db.AutocryptPeerFor(ctx, "a@example.org")
	if err != nil || !ok || !got.LastSeen.Equal(now) || string(got.PublicKey) != "\x01\x02" || got.PreferEncrypt != "mutual" || !got.GossipTimestamp.Equal(now.Add(time.Hour)) {
		t.Errorf("peer = %+v, %v", got, err)
	}
	// a peer with only last_seen is not listed.
	if err := db.SaveAutocryptPeer(ctx, AutocryptPeer{Addr: "b@example.org", LastSeen: now, PreferEncrypt: "nopreference"}); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListAutocryptPeers(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("ListAutocryptPeers = %d, %v", len(list), err)
	}
}

func TestKeyLookupCacheAndHistory(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	at := time.Unix(1_800_000_000, 0).UTC()
	if err := db.SaveCachedKey(ctx, CachedKey{Addr: "a@x", Source: "wkd", FetchedAt: at}); err != nil {
		t.Fatal(err)
	}
	c, ok, err := db.CachedKeyFor(ctx, "a@x", "wkd")
	if err != nil || !ok || c.Key != nil || !c.FetchedAt.Equal(at) {
		t.Errorf("negative cache = %+v, %v, %v", c, ok, err)
	}
	if err := db.SaveCachedKey(ctx, CachedKey{Addr: "a@x", Source: "wkd", Key: []byte("k"), FetchedAt: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	found, _ := db.ListFoundKeys(ctx)
	if len(found) != 1 || string(found[0].Key) != "k" {
		t.Errorf("ListFoundKeys = %+v", found)
	}

	if err := db.RecordKeySeen(ctx, "a@x", "FP", "wkd", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordKeySeen(ctx, "a@x", "FP", "autocrypt", at); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordKeySeen(ctx, "a@x", "FP", "gossip", at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	s, ok, err := db.KeySightingFor(ctx, "a@x", "FP")
	if err != nil || !ok || !s.FirstSeen.Equal(at) || s.Source != "autocrypt" {
		t.Errorf("sighting = %+v, %v, %v; want the earliest", s, ok, err)
	}

	if err := db.SaveAcceptedKey(ctx, AcceptedKey{Addr: "a@x", Fingerprint: "FP", Source: "wkd", AcceptedAt: at, Verified: true, Preference: "always"}); err != nil {
		t.Fatal(err)
	}
	ck, ok, err := db.AcceptedKeyFor(ctx, "a@x")
	if err != nil || !ok || !ck.Verified || ck.Preference != "always" {
		t.Errorf("contact key = %+v", ck)
	}

	if err := db.ForgetContactKeys(ctx, "a@x"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.AcceptedKeyFor(ctx, "a@x"); ok {
		t.Error("contact key survived forgetting")
	}
	if _, ok, _ := db.CachedKeyFor(ctx, "a@x", "wkd"); ok {
		t.Error("cache survived forgetting")
	}
	if _, ok, _ := db.KeySightingFor(ctx, "a@x", "FP"); ok {
		t.Error("history survived forgetting")
	}
}

func TestKeyLookupLog(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for i := range keyLookupLogKeep + 5 {
		if err := db.AddKeyLookupLog(ctx, KeyLookupLogEntry{At: time.Unix(int64(1000+i), 0), Source: "wkd", Addr: "a@x", Outcome: "found"}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := db.ListKeyLookupLog(ctx, 5000)
	if err != nil || len(all) != keyLookupLogKeep {
		t.Fatalf("log has %d entries, %v", len(all), err)
	}
	if all[0].At.Unix() != int64(1000+keyLookupLogKeep+4) {
		t.Error("newest entry is not first")
	}
	if err := db.ClearKeyLookupLog(ctx); err != nil {
		t.Fatal(err)
	}
	if all, _ := db.ListKeyLookupLog(ctx, 10); len(all) != 0 {
		t.Error("log not cleared")
	}
}
