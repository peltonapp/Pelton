package autocrypt

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

var keyBytes = []byte("not really a key, the rules here never look inside one")

func keydata() string { return base64.StdEncoding.EncodeToString(keyBytes) }

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		ok      bool
		addr    string
		prefer  string
		comment string
	}{
		{name: "minimal", value: "addr=a@example.org; keydata=" + keydata(), ok: true, addr: "a@example.org", prefer: PreferNoPreference},
		{name: "mutual", value: "addr=a@example.org; prefer-encrypt=mutual; keydata=" + keydata(), ok: true, addr: "a@example.org", prefer: PreferMutual},
		{name: "unknown prefer value reads as no preference", value: "addr=a@example.org; prefer-encrypt=always; keydata=" + keydata(), ok: true, addr: "a@example.org", prefer: PreferNoPreference},
		{name: "address is lowercased", value: "addr=Anna@Example.ORG; keydata=" + keydata(), ok: true, addr: "anna@example.org", prefer: PreferNoPreference},
		{name: "underscore attributes are ignored", value: "addr=a@example.org; _extra=1; keydata=" + keydata(), ok: true, addr: "a@example.org", prefer: PreferNoPreference},
		{name: "folded keydata", value: "addr=a@example.org; keydata=\r\n " + keydata()[:10] + "\r\n " + keydata()[10:], ok: true, addr: "a@example.org", prefer: PreferNoPreference},
		{name: "unknown critical attribute", value: "addr=a@example.org; type=1; keydata=" + keydata()},
		{name: "missing addr", value: "keydata=" + keydata()},
		{name: "missing keydata", value: "addr=a@example.org"},
		{name: "keydata not base64", value: "addr=a@example.org; keydata=%%%"},
		{name: "empty keydata", value: "addr=a@example.org; keydata="},
		{name: "attribute without value", value: "addr=a@example.org; mutual; keydata=" + keydata()},
		{name: "addr twice", value: "addr=a@example.org; addr=b@example.org; keydata=" + keydata()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := Parse(tt.value)
			if tt.ok != (err == nil) {
				t.Fatalf("Parse ok = %v, want %v (err %v)", err == nil, tt.ok, err)
			}
			if !tt.ok {
				return
			}
			if h.Addr != tt.addr || h.PreferEncrypt != tt.prefer || !bytes.Equal(h.KeyData, keyBytes) {
				t.Errorf("Parse = %+v", h)
			}
		})
	}
}

func TestParseGossipRejectsPreferEncrypt(t *testing.T) {
	if _, err := ParseGossip("addr=a@example.org; prefer-encrypt=mutual; keydata=" + keydata()); err == nil {
		t.Error("a gossip header with prefer-encrypt was accepted")
	}
	h, err := ParseGossip("addr=a@example.org; keydata=" + keydata())
	if err != nil || h.Addr != "a@example.org" {
		t.Errorf("ParseGossip = %+v, %v", h, err)
	}
}

func TestFormatRoundTripAndFolding(t *testing.T) {
	long := bytes.Repeat([]byte{0x99, 0x01}, 400)
	h := Header{Addr: "me@example.org", PreferEncrypt: PreferMutual, KeyData: long}
	value := Format(h)
	for _, line := range strings.Split("Autocrypt: "+value, "\r\n") {
		if len(line) > 78 {
			t.Fatalf("line of %d characters: %q", len(line), line)
		}
	}
	back, err := Parse(value)
	if err != nil {
		t.Fatalf("Parse(Format) = %v", err)
	}
	if back.Addr != h.Addr || back.PreferEncrypt != PreferMutual || !bytes.Equal(back.KeyData, long) {
		t.Errorf("round trip = %+v", back)
	}
	if strings.Contains(Format(Header{Addr: "x@y", PreferEncrypt: PreferNoPreference, KeyData: long}), "prefer-encrypt") {
		t.Error("prefer-encrypt written for no preference")
	}
	if strings.Contains(FormatGossip(h), "prefer-encrypt") {
		t.Error("prefer-encrypt written into a gossip header")
	}
}

func TestSelectHeader(t *testing.T) {
	good := "addr=a@example.org; keydata=" + keydata()
	other := "addr=b@example.org; keydata=" + keydata()
	bad := "addr=a@example.org; bogus=1; keydata=" + keydata()
	tests := []struct {
		name   string
		values []string
		ok     bool
	}{
		{"single matching header", []string{good}, true},
		{"address must match From", []string{other}, false},
		{"two valid headers for the sender cancel out", []string{good, good}, false},
		{"an invalid header does not count", []string{good, bad}, true},
		{"another address alongside is ignored", []string{good, other}, true},
		{"none", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := SelectHeader(tt.values, "A@example.org")
			if ok != tt.ok {
				t.Errorf("SelectHeader ok = %v, want %v", ok, tt.ok)
			}
		})
	}
}

func TestEffectiveDate(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	if got := EffectiveDate(now.Add(48*time.Hour), now); !got.Equal(now) {
		t.Errorf("future date = %v, want now", got)
	}
	if got := EffectiveDate(time.Time{}, now); !got.Equal(now) {
		t.Errorf("missing date = %v, want now", got)
	}
	past := now.Add(-time.Hour)
	if got := EffectiveDate(past, now); !got.Equal(past) {
		t.Errorf("past date = %v", got)
	}
}

func TestPeerUpdate(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	h := &Header{Addr: "a@example.org", PreferEncrypt: PreferMutual, KeyData: []byte("k1")}
	h2 := &Header{Addr: "a@example.org", PreferEncrypt: PreferNoPreference, KeyData: []byte("k2")}

	var p Peer
	if !p.Update(day(10), h) {
		t.Fatal("first header changed nothing")
	}
	if !p.AutocryptTimestamp.Equal(day(10)) || !p.LastSeen.Equal(day(10)) || string(p.PublicKey) != "k1" || p.PreferEncrypt != PreferMutual {
		t.Fatalf("after first header: %+v", p)
	}

	// an older message, with or without a header, changes nothing.
	if p.Update(day(5), h2) || string(p.PublicKey) != "k1" {
		t.Errorf("older message replaced the key: %+v", p)
	}
	if p.Update(day(5), nil) {
		t.Error("older headerless message changed state")
	}

	// a newer message without a header only moves last_seen.
	if !p.Update(day(20), nil) {
		t.Error("newer headerless message changed nothing")
	}
	if !p.LastSeen.Equal(day(20)) || !p.AutocryptTimestamp.Equal(day(10)) || string(p.PublicKey) != "k1" {
		t.Errorf("after headerless message: %+v", p)
	}

	// a message between the header and last_seen still updates the key, but
	// leaves last_seen where it was.
	if !p.Update(day(15), h2) {
		t.Error("newer header changed nothing")
	}
	if !p.LastSeen.Equal(day(20)) || !p.AutocryptTimestamp.Equal(day(15)) || string(p.PublicKey) != "k2" || p.PreferEncrypt != PreferNoPreference {
		t.Errorf("after second header: %+v", p)
	}
}

func TestPeerUpdateGossip(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	var p Peer
	if !p.UpdateGossip(day(3), []byte("g1")) {
		t.Fatal("first gossip ignored")
	}
	if p.UpdateGossip(day(2), []byte("g0")) || string(p.GossipKey) != "g1" {
		t.Error("older gossip replaced newer")
	}
	if p.UpdateGossip(day(3), []byte("g2")) {
		t.Error("gossip at the same time replaced the earlier one")
	}
	if !p.UpdateGossip(day(4), []byte("g3")) || string(p.GossipKey) != "g3" {
		t.Error("newer gossip ignored")
	}
}

func TestPreliminary(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	usable := func(k []byte) bool { return string(k) != "expired" }
	tests := []struct {
		name string
		peer Peer
		want Recommendation
		key  string
	}{
		{"no state", Peer{}, Disable, ""},
		{"fresh key", Peer{PublicKey: []byte("k"), AutocryptTimestamp: now, LastSeen: now}, Available, "k"},
		{"unusable key", Peer{PublicKey: []byte("expired"), AutocryptTimestamp: now, LastSeen: now}, Disable, ""},
		{"unusable key does not fall back to gossip", Peer{PublicKey: []byte("expired"), GossipKey: []byte("g"), AutocryptTimestamp: now, LastSeen: now}, Disable, ""},
		{"gossip only", Peer{GossipKey: []byte("g")}, Discourage, "g"},
		{"stale header", Peer{PublicKey: []byte("k"), AutocryptTimestamp: now.Add(-36 * 24 * time.Hour), LastSeen: now}, Discourage, "k"},
		{"just within 35 days", Peer{PublicKey: []byte("k"), AutocryptTimestamp: now.Add(-35 * 24 * time.Hour), LastSeen: now}, Available, "k"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, key := Preliminary(tt.peer, usable)
			if got != tt.want || string(key) != tt.key {
				t.Errorf("Preliminary = %v, %q; want %v, %q", got, key, tt.want, tt.key)
			}
		})
	}
}

func TestFinal(t *testing.T) {
	tests := []struct {
		name     string
		prelim   Recommendation
		peer     string
		own      string
		reply    bool
		expected Recommendation
	}{
		{"both mutual", Available, PreferMutual, PreferMutual, false, Encrypt},
		{"only peer mutual", Available, PreferMutual, PreferNoPreference, false, Available},
		{"only me mutual", Available, PreferNoPreference, PreferMutual, false, Available},
		{"discouraged with both mutual stays discouraged", Discourage, PreferMutual, PreferMutual, false, Discourage},
		{"reply to encrypted", Available, PreferNoPreference, PreferNoPreference, true, Encrypt},
		{"reply to encrypted lifts discourage", Discourage, PreferNoPreference, PreferNoPreference, true, Encrypt},
		{"reply cannot enable the impossible", Disable, PreferMutual, PreferMutual, true, Disable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Final(tt.prelim, tt.peer, tt.own, tt.reply); got != tt.expected {
				t.Errorf("Final = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCombine(t *testing.T) {
	tests := []struct {
		name string
		in   []Recommendation
		want Recommendation
	}{
		{"nobody", nil, Disable},
		{"all encrypt", []Recommendation{Encrypt, Encrypt}, Encrypt},
		{"one disable", []Recommendation{Encrypt, Disable}, Disable},
		{"one discourage", []Recommendation{Encrypt, Discourage, Available}, Discourage},
		{"one available", []Recommendation{Encrypt, Available}, Available},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Combine(tt.in); got != tt.want {
				t.Errorf("Combine = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadIncoming(t *testing.T) {
	raw := "From: Anna <Anna@Example.org>\r\n" +
		"Date: Mon, 02 Feb 2026 10:00:00 +0000\r\n" +
		"Autocrypt: addr=anna@example.org; keydata=\r\n " + keydata() + "\r\n" +
		"Subject: hi\r\n\r\nbody\r\n"
	in, ok := ReadIncoming([]byte(raw))
	if !ok {
		t.Fatal("ordinary message ignored")
	}
	if in.From != "anna@example.org" || in.Date.IsZero() || len(in.Headers) != 1 {
		t.Fatalf("ReadIncoming = %+v", in)
	}
	if _, ok := SelectHeader(in.Headers, in.From); !ok {
		t.Error("folded header did not parse")
	}

	if _, ok := ReadIncoming([]byte("From: a@example.org, b@example.org\r\n\r\nx")); ok {
		t.Error("message with two From addresses was used")
	}
	if _, ok := ReadIncoming([]byte("From: a@example.org\r\nContent-Type: multipart/report; report-type=delivery-status; boundary=x\r\n\r\nx")); ok {
		t.Error("delivery report was used")
	}
	if _, ok := ReadIncoming(nil); ok {
		t.Error("empty message was used")
	}
}

func TestGossipHeaders(t *testing.T) {
	entity := "Content-Type: text/plain; protected-headers=\"v1\"\r\n" +
		"Autocrypt-Gossip: addr=b@example.org; keydata=" + keydata() + "\r\n" +
		"Autocrypt-Gossip: addr=stranger@example.org; keydata=" + keydata() + "\r\n" +
		"Autocrypt-Gossip: addr=c@example.org; prefer-encrypt=mutual; keydata=" + keydata() + "\r\n" +
		"\r\nhello\r\n"
	got := GossipHeaders([]byte(entity), []string{"B@example.org", "c@example.org"})
	if len(got) != 1 || got[0].Addr != "b@example.org" {
		t.Errorf("GossipHeaders = %+v, want only b@example.org", got)
	}
}

func TestAddressesIn(t *testing.T) {
	got := AddressesIn("Anna <ANNA@example.org>, bob@example.org")
	if len(got) != 2 || got[0] != "anna@example.org" || got[1] != "bob@example.org" {
		t.Errorf("AddressesIn = %v", got)
	}
	if AddressesIn("") != nil {
		t.Error("empty list gave addresses")
	}
}
