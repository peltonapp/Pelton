package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/autocrypt"
	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/keylookup"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Lookups ask the network for a recipient's key. They happen only for the
// sources the user allowed, only when the composer asks about addresses it
// was given, never for mail being read and never over the whole address book.
// Every request is written to the lookup log, and every answer is cached so
// the same question is not asked again for a while.

const (
	// lookupFreshFound and lookupFreshMissing are how long an answer is
	// trusted before the source is asked again.
	lookupFreshFound   = 7 * 24 * time.Hour
	lookupFreshMissing = 24 * time.Hour
	// lookupMaxAddresses caps one composer request.
	lookupMaxAddresses = 10
	// lookupParallel is how many addresses are looked up at once.
	lookupParallel = 3
	// lookupTimeout bounds a whole request.
	lookupTimeout = 30 * time.Second
	// lookupLogLimit is how many log entries the settings page gets.
	lookupLogLimit = 200
)

// Lookup log outcomes.
const (
	lookupFound    = "found"
	lookupNone     = "none"
	lookupUnusable = "unusable"
	lookupFailed   = "failed"
)

func (a *App) lookupClient() *keylookup.Client {
	if a.autoEnc.client != nil {
		return a.autoEnc.client
	}
	return keylookup.New(keylookup.Config{Dial: a.scriptDial})
}

// LookupRecipientKeys asks the allowed sources for the keys of recipients
// Pelton knows none for, and reports whether it found any. With the feature
// off or no network source allowed it makes no request at all.
func (a *App) LookupRecipientKeys(accountID int64, emails []string) (bool, error) {
	if err := a.ready(); err != nil {
		return false, err
	}
	if !a.autoEncryptOn() {
		return false, nil
	}
	sources := a.keySources()
	order := a.lookupSourceOrder(sources)
	if len(order) == 0 {
		return false, nil
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return false, err
	}
	own := autocrypt.NormalizeAddr(account.Email)
	keys, _ := a.keyStore()

	var todo []string
	seen := map[string]bool{}
	for _, e := range emails {
		addr := autocrypt.NormalizeAddr(e)
		if addr == "" || addr == own || seen[addr] || !strings.Contains(addr, "@") {
			continue
		}
		seen[addr] = true
		r := a.resolveRecipient(keys, sources, addr, false, autocrypt.PreferNoPreference)
		if r.key != nil && (r.source == keySourceManual || r.source == keySourceAutocrypt) {
			continue
		}
		todo = append(todo, addr)
		if len(todo) == lookupMaxAddresses {
			break
		}
	}
	if len(todo) == 0 {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(a.ctx, lookupTimeout)
	defer cancel()
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		found bool
		sem   = make(chan struct{}, lookupParallel)
	)
	for _, addr := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if a.lookupAddress(ctx, addr, order) {
				mu.Lock()
				found = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if found {
		a.emit(EventAutoEncryptChanged, nil)
	}
	return found, nil
}

// lookupAddress asks each source in turn until one has a usable key, skipping
// sources whose cached answer is still fresh. It reports whether a new key
// was stored. Two lookups for the same address never run at once.
func (a *App) lookupAddress(ctx context.Context, addr string, order []string) bool {
	st := &a.autoEnc
	st.lookupMu.Lock()
	if wait, busy := st.inflight[addr]; busy {
		st.lookupMu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
		}
		return false
	}
	if st.inflight == nil {
		st.inflight = map[string]chan struct{}{}
	}
	done := make(chan struct{})
	st.inflight[addr] = done
	st.lookupMu.Unlock()
	defer func() {
		st.lookupMu.Lock()
		delete(st.inflight, addr)
		st.lookupMu.Unlock()
		close(done)
	}()

	now := a.autoNow()
	for _, source := range order {
		if ctx.Err() != nil {
			return false
		}
		if cached, ok, err := a.store.CachedKeyFor(a.ctx, addr, source); err == nil && ok {
			if cached.Key != nil && now.Sub(cached.FetchedAt) < lookupFreshFound {
				if crypto.KeyUsable(cached.Key, now) {
					return false
				}
				continue
			}
			if cached.Key == nil && now.Sub(cached.FetchedAt) < lookupFreshMissing {
				continue
			}
		}
		body, err := a.fetchKey(ctx, source, addr)
		switch {
		case err == nil:
			key, info, serr := crypto.SelectKeyFor(body, addr, now)
			if serr != nil {
				a.logLookup(source, addr, lookupUnusable)
				a.cacheLookup(addr, source, nil, now)
				continue
			}
			a.logLookup(source, addr, lookupFound)
			a.cacheLookup(addr, source, key, now)
			if err := a.store.RecordKeySeen(a.ctx, addr, crypto.NormalizeFingerprint(info.Fingerprint), source, now); err != nil {
				a.log.Error("record found key", "err", err)
			}
			return true
		case errors.Is(err, keylookup.ErrNotFound):
			a.logLookup(source, addr, lookupNone)
			a.cacheLookup(addr, source, nil, now)
		default:
			a.log.Debug("key lookup failed", "source", source, "err", err)
			a.logLookup(source, addr, lookupFailed)
		}
	}
	return false
}

func (a *App) fetchKey(ctx context.Context, source, addr string) ([]byte, error) {
	client := a.lookupClient()
	switch {
	case source == keySourceWKD:
		return client.WKD(ctx, addr)
	case source == keySourceKOO:
		return client.VKS(ctx, keylookup.KeysOpenPGPOrg, addr)
	case strings.HasPrefix(source, keySourceServerPrefix):
		return client.KeyServer(ctx, strings.TrimPrefix(source, keySourceServerPrefix), addr)
	}
	return nil, keylookup.ErrRefused
}

func (a *App) cacheLookup(addr, source string, key []byte, at time.Time) {
	if err := a.store.SaveCachedKey(a.ctx, storage.CachedKey{Addr: addr, Source: source, Key: key, FetchedAt: at}); err != nil {
		a.log.Error("cache key lookup", "err", err)
	}
}

func (a *App) logLookup(source, addr, outcome string) {
	if err := a.store.AddKeyLookupLog(a.ctx, storage.KeyLookupLogEntry{At: a.autoNow(), Source: source, Addr: addr, Outcome: outcome}); err != nil {
		a.log.Error("log key lookup", "err", err)
	}
}

// KeyLookupLogDTO is one request a lookup made.
type KeyLookupLogDTO struct {
	// At is RFC 3339.
	At string `json:"at"`
	// Source is wkd, keys.openpgp.org or server:<url>.
	Source string `json:"source"`
	Email  string `json:"email"`
	// Outcome is found, none, unusable or failed.
	Outcome string `json:"outcome"`
}

// ListKeyLookupLog returns the most recent lookup requests, newest first.
func (a *App) ListKeyLookupLog() ([]KeyLookupLogDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	entries, err := a.store.ListKeyLookupLog(a.ctx, lookupLogLimit)
	if err != nil {
		return nil, err
	}
	out := make([]KeyLookupLogDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, KeyLookupLogDTO{At: e.At.UTC().Format(time.RFC3339), Source: e.Source, Email: e.Addr, Outcome: e.Outcome})
	}
	return out, nil
}

// ClearKeyLookupLog empties the lookup log.
func (a *App) ClearKeyLookupLog() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.ClearKeyLookupLog(a.ctx)
}
