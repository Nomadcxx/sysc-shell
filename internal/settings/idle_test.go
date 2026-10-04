package settings

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

type fakeWalls struct {
	enabled bool
	timeout string
	fail    bool
}

func (f *fakeWalls) SetEnabled(on bool) bool {
	if f.fail {
		return false
	}
	f.enabled = on
	return true
}

func (f *fakeWalls) SetTimeout(d time.Duration) bool {
	if f.fail {
		return false
	}
	f.timeout = d.String()
	return true
}

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

func TestApplyWhenIdle(t *testing.T) {
	cfg := config.Config{Idle: config.Idle{Lock: time.Hour}, Session: config.Session{Locker: "sysc-lock"}}
	w := &fakeWalls{enabled: true, timeout: "5m"}

	if err := ApplyWhenIdle("lock", 10*time.Minute, &cfg, w); err != nil {
		t.Fatal(err)
	}
	if cfg.Idle.Lock != 10*time.Minute || w.enabled {
		t.Fatalf("lock apply: lock=%v enabled=%v", cfg.Idle.Lock, w.enabled)
	}
	if w.timeout != "5m" {
		t.Fatalf("must not rewrite walls timeout, got %q", w.timeout)
	}

	if err := ApplyWhenIdle("screensaver", 3*time.Minute, &cfg, w); err != nil {
		t.Fatal(err)
	}
	if cfg.Idle.Lock != 0 || !w.enabled || w.timeout != (3*time.Minute).String() {
		t.Fatalf("screensaver apply: %+v enabled=%v timeout=%q", cfg.Idle, w.enabled, w.timeout)
	}

	if err := ApplyWhenIdle("nothing", 0, &cfg, w); err != nil {
		t.Fatal(err)
	}
	if cfg.Idle.Lock != 0 || w.enabled {
		t.Fatalf("nothing apply: lock=%v enabled=%v", cfg.Idle.Lock, w.enabled)
	}
	if w.timeout != (3*time.Minute).String() {
		t.Fatalf("nothing must not rewrite timeout, got %q", w.timeout)
	}
}

func TestApplyWhenIdleRejectsLockWithoutLocker(t *testing.T) {
	cfg := config.Config{}
	if err := ApplyWhenIdle("lock", time.Minute, &cfg, &fakeWalls{}); err == nil {
		t.Fatal("want error when locker is empty")
	}
}

func TestApplyWhenIdleNilWalls(t *testing.T) {
	cfg := config.Config{Idle: config.Idle{Lock: time.Hour}, Session: config.Session{Locker: "sysc-lock"}}
	if err := ApplyWhenIdle("screensaver", time.Minute, &cfg, nil); err == nil {
		t.Fatal("want error when screensaver has no walls")
	}
	if cfg.Idle.Lock != time.Hour {
		t.Fatalf("screensaver nil walls mutated lock to %v", cfg.Idle.Lock)
	}

	if err := ApplyWhenIdle("lock", 2*time.Minute, &cfg, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Idle.Lock != 2*time.Minute {
		t.Fatalf("lock nil walls: lock=%v", cfg.Idle.Lock)
	}

	if err := ApplyWhenIdle("nothing", 0, &cfg, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Idle.Lock != 0 {
		t.Fatalf("nothing nil walls: lock=%v", cfg.Idle.Lock)
	}
}

func TestApplyWhenIdleRejectsNonPositiveDelay(t *testing.T) {
	cfg := config.Config{Session: config.Session{Locker: "sysc-lock"}}
	w := &fakeWalls{enabled: true, timeout: "5m"}
	if err := ApplyWhenIdle("lock", 0, &cfg, w); err == nil {
		t.Fatal("want error when lock delay is not positive")
	}
	if err := ApplyWhenIdle("screensaver", 0, &cfg, w); err == nil {
		t.Fatal("want error when screensaver delay is not positive")
	}
}

func TestApplyWhenIdleRejectsUnknownMode(t *testing.T) {
	cfg := config.Config{}
	if err := ApplyWhenIdle("blank", time.Minute, &cfg, &fakeWalls{}); err == nil {
		t.Fatal("want error for unknown mode")
	}
}

func TestApplyWhenIdleUnitReject(t *testing.T) {
	cfg := config.Config{Idle: config.Idle{Lock: time.Hour}, Session: config.Session{Locker: "sysc-lock"}}
	w := &fakeWalls{fail: true, enabled: true, timeout: "5m"}
	if err := ApplyWhenIdle("lock", time.Minute, &cfg, w); err == nil {
		t.Fatal("want error when disable fails")
	}
	if cfg.Idle.Lock != time.Hour {
		t.Fatalf("failed lock left Idle.Lock=%v", cfg.Idle.Lock)
	}
	if err := ApplyWhenIdle("screensaver", time.Minute, &cfg, w); err == nil {
		t.Fatal("want error when timeout fails")
	}
	if cfg.Idle.Lock != time.Hour || w.enabled != true || w.timeout != "5m" {
		t.Fatalf("failed screensaver mutated state: lock=%v enabled=%v timeout=%q", cfg.Idle.Lock, w.enabled, w.timeout)
	}
}
