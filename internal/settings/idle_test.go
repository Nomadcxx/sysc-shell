package settings

import (
	"testing"
	"time"
)

func TestWhenIdleMode(t *testing.T) {
	cases := []struct {
		lock    time.Duration
		wallsOn bool
		want    string
	}{
		{0, false, "nothing"},
		{0, true, "screensaver"},
		{10 * time.Minute, false, "lock"},
		{10 * time.Minute, true, "lock"}, // lock wins if a hand edit armed two timers
	}
	for _, c := range cases {
		if got := WhenIdleMode(c.lock, c.wallsOn); got != c.want {
			t.Fatalf("lock=%v walls=%v: %q, want %q", c.lock, c.wallsOn, got, c.want)
		}
	}
}

func TestWhenIdleDelay(t *testing.T) {
	if d := WhenIdleDelay(10*time.Minute, "5m"); d != 10*time.Minute {
		t.Fatalf("lock delay = %v", d)
	}
	if d := WhenIdleDelay(0, "5m"); d != 5*time.Minute {
		t.Fatalf("screensaver delay = %v", d)
	}
	if d := WhenIdleDelay(0, ""); d != 5*time.Minute {
		t.Fatalf("default delay = %v", d)
	}
}
