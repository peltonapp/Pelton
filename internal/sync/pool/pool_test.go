package pool

import (
	"testing"
)

func TestPoolSizes(t *testing.T) {
	tests := []struct {
		name          string
		n             int
		configure     int // 0 = leave
		effective     int // 0 = leave
		wantConf      int
		wantEffective int
	}{
		{"default", 3, 0, 0, 3, 3},
		{"clamp low", 0, 0, 0, 1, 1},
		{"clamp high", 11, 0, 0, 10, 10},
		{"ten is allowed", 10, 0, 0, 10, 10},
		{"throttle", 3, 0, 1, 3, 1},
		{"effective capped by configured", 2, 0, 5, 2, 2},
		{"lower configured shrinks effective", 4, 2, 0, 2, 2},
		{"raise configured keeps throttle", 2, 5, 1, 5, 1},
		{"raise configured to the cap", 2, 12, 0, 10, 2},
		{"effective below 1", 3, 0, -2, 3, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.n)
			if tt.configure != 0 {
				p.SetConfigured(tt.configure)
			}
			if tt.effective != 0 {
				p.SetEffective(tt.effective)
			}
			if got := p.Configured(); got != tt.wantConf {
				t.Errorf("Configured = %d, want %d", got, tt.wantConf)
			}
			if got := p.Effective(); got != tt.wantEffective {
				t.Errorf("Effective = %d, want %d", got, tt.wantEffective)
			}
		})
	}
}

func TestPoolWatch(t *testing.T) {
	p := New(3)
	var a, b int
	stopA := p.Watch(func() { a++ })
	p.Watch(func() { b++; _ = p.Effective() }) // may call back into the pool
	p.SetEffective(2)
	p.SetConfigured(4)
	stopA()
	p.SetEffective(1)
	if a != 2 || b != 3 {
		t.Fatalf("watch calls a=%d b=%d, want 2 and 3", a, b)
	}
}
