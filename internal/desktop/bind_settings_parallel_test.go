package desktop

import "testing"

func TestClampSyncMaxParallel(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 1}, {1, 1}, {3, 3}, {5, 5}, {6, 5}, {-1, 1},
	}
	for _, c := range cases {
		if got := clampSyncMaxParallel(c.in); got != c.want {
			t.Fatalf("clamp(%d)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestDefaultSyncMessageLimitIs100(t *testing.T) {
	if defaultSyncMessageLimit != 100 {
		t.Fatalf("defaultSyncMessageLimit=%d want 100", defaultSyncMessageLimit)
	}
}

func TestSyncMessageLimitExpanded(t *testing.T) {
	cases := []struct {
		from, to int
		want     bool
	}{
		{100, 250, true},
		{100, 100, false},
		{100, 50, false},
		{100, 0, true},
		{0, 100, false},
		{0, 0, false},
		{50, 0, true},
	}
	for _, c := range cases {
		if got := syncMessageLimitExpanded(c.from, c.to); got != c.want {
			t.Fatalf("expanded(%d,%d)=%v want %v", c.from, c.to, got, c.want)
		}
	}
}
