package syncsched

import (
	"sync"
	"time"
)

// Batch defaults, in one place. Sources: core-split/RESEARCH-batches.md
// (Thunderbird, K-9, Evolution, Geary, Mailspring).
const (
	// DefaultBodyCount is the starting message count of one body task;
	// MaxBodyCount caps its growth.
	DefaultBodyCount = 5
	MaxBodyCount     = 50
	// DefaultBodyBytes is the starting byte budget of one body task.
	DefaultBodyBytes = 20 << 20
	// MaxBodyBytes and MinBodyBytes bound the adaptive body budget.
	MaxBodyBytes = 64 << 20
	MinBodyBytes = 256 << 10
	// DefaultChunkBytes is the starting size of one partial fetch of a
	// message too big for the budget (BODY.PEEK[]<offset.len>).
	DefaultChunkBytes = 5 << 20
	// MinChunkBytes is the floor of the adaptive chunk size.
	MinChunkBytes = 256 << 10
	// FirstListPage is the first page of a folder's stub list, so the list
	// shows quickly; ListPage is every later page.
	FirstListPage = 50
	ListPage      = 500

	// A batch that ends within growBelow grows the budget; one that takes
	// longer than shrinkAbove halves it.
	growBelow   = 2 * time.Second
	shrinkAbove = 4 * time.Second
	growFactor  = 1.5
)

// Tunables are a provider's batch sizes. Zero fields take the defaults
// above. JMAP sets MaxBodyCount from the server's maxObjectsInGet.
type Tunables struct {
	BodyCount     int
	MaxBodyCount  int
	BodyBytes     int64
	ChunkBytes    int64
	FirstListPage int
	ListPage      int
	// MaxBodyWidth caps the copies of one width entry (a folder's body
	// generator, see Task.Width) below what N allows. 0 is no cap. It is an
	// internal kill switch, not a user setting.
	MaxBodyWidth int
}

func (t Tunables) withDefaults() Tunables {
	if t.BodyCount <= 0 {
		t.BodyCount = DefaultBodyCount
	}
	if t.MaxBodyCount <= 0 {
		t.MaxBodyCount = MaxBodyCount
	}
	t.BodyCount = min(t.BodyCount, t.MaxBodyCount)
	if t.BodyBytes <= 0 {
		t.BodyBytes = DefaultBodyBytes
	}
	if t.ChunkBytes <= 0 {
		t.ChunkBytes = DefaultChunkBytes
	}
	if t.FirstListPage <= 0 {
		t.FirstListPage = FirstListPage
	}
	if t.ListPage <= 0 {
		t.ListPage = ListPage
	}
	return t
}

// Adaptive is a byte size that follows how long the last batch took, like
// Thunderbird's AdjustChunkSize: within 2 s it grows by half, past 4 s it
// halves, in between it holds. It only grows after a batch that used the
// whole size, since a small batch that was quick says nothing about the
// link. It lives in memory per account.
type Adaptive struct {
	mu            sync.Mutex
	cur, min, max int64
}

// NewAdaptive starts at start, kept within [min, max].
func NewAdaptive(start, min, max int64) *Adaptive {
	return &Adaptive{cur: clamp64(start, min, max), min: min, max: max}
}

// Get is the current size.
func (a *Adaptive) Get() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cur
}

// Observe adjusts the size after a batch that took d. full is whether the
// batch used the whole size (a whole chunk, a batch cut by the budget); only
// a full batch can grow it. A slow batch halves it either way.
func (a *Adaptive) Observe(d time.Duration, full bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case d <= growBelow && full:
		a.cur = clamp64(int64(float64(a.cur)*growFactor), a.min, a.max)
	case d > shrinkAbove:
		a.cur = clamp64(a.cur/2, a.min, a.max)
	}
}

func clamp64(v, lo, hi int64) int64 { return min(max(v, lo), hi) }

// BodyLimits are one account's adaptive body batch: a message count and a
// byte budget, both following the last batch's duration (see Adaptive). The
// count grows by half rounded up, to at most the provider's maximum, and
// halves to at least 1. A batch ends at whichever limit it hits first.
type BodyLimits struct {
	mu       sync.Mutex
	count    int
	maxCount int
	bytes    *Adaptive
}

// NewBodyLimits starts at t's count and byte budget.
func NewBodyLimits(t Tunables) *BodyLimits {
	t = t.withDefaults()
	return &BodyLimits{count: t.BodyCount, maxCount: t.MaxBodyCount, bytes: NewAdaptive(t.BodyBytes, MinBodyBytes, MaxBodyBytes)}
}

// Get is the current count and byte budget.
func (l *BodyLimits) Get() (count int, bytes int64) {
	l.mu.Lock()
	count = l.count
	l.mu.Unlock()
	return count, l.bytes.Get()
}

// Observe adjusts both limits after a batch that took d. hitLimit is
// whether the batch was cut by the count or the byte budget (PlanBatchFull's
// full): only such a batch grows them, so a run of quick one-message batches
// (new mail) does not raise them to the maximum. A slow batch halves them
// either way.
func (l *BodyLimits) Observe(d time.Duration, hitLimit bool) {
	l.mu.Lock()
	switch {
	case d <= growBelow && hitLimit:
		l.count = min((l.count*3+1)/2, l.maxCount)
	case d > shrinkAbove:
		l.count = max(l.count/2, 1)
	}
	l.mu.Unlock()
	l.bytes.Observe(d, hitLimit)
}

// PlanBatch takes ids newest first up to maxCount messages or budget bytes,
// whichever comes first. A first message larger than the budget goes alone.
// size is asked only for the ids it looks at; an unknown size is 0.
func PlanBatch(ids []string, size func(id string) int64, maxCount int, budget int64) []string {
	batch, _ := PlanBatchFull(ids, size, maxCount, budget)
	return batch
}

// PlanBatchFull is PlanBatch that also reports whether the batch was cut by
// maxCount or budget, rather than by running out of ids. Pass full to
// BodyLimits.Observe.
func PlanBatchFull(ids []string, size func(id string) int64, maxCount int, budget int64) (batch []string, full bool) {
	var total int64
	for _, id := range ids {
		if len(batch) >= maxCount {
			return batch, true
		}
		n := size(id)
		if len(batch) > 0 && total+n > budget {
			return batch, true
		}
		batch = append(batch, id)
		total += n
		if total >= budget {
			return batch, true
		}
	}
	return batch, false
}
