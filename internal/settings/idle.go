package settings

import (
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

const DefaultIdleDelay = 5 * time.Minute

// IdleWalls is the walls unit seam for ApplyWhenIdle. Do not import internal/walls here.
type IdleWalls interface {
	SetEnabled(on bool) bool
	SetTimeout(d time.Duration) bool
}

func ApplyWhenIdle(mode string, delay time.Duration, cfg *config.Config, walls IdleWalls) error {
	switch mode {
	case "lock":
		if delay <= 0 {
			return fmt.Errorf("settings: idle.after: delay %v is not positive", delay)
		}
		if cfg.Session.Locker == "" {
			return fmt.Errorf("settings: idle.after: set a locker command first")
		}
		if walls != nil && !walls.SetEnabled(false) {
			return fmt.Errorf("settings: idle.after: could not disable screensaver")
		}
		cfg.Idle.Lock = delay
		return nil
	case "screensaver":
		if delay <= 0 {
			return fmt.Errorf("settings: idle.after: delay %v is not positive", delay)
		}
		if walls == nil {
			return fmt.Errorf("settings: idle.after: screensaver is not available")
		}
		if !walls.SetTimeout(delay) {
			return fmt.Errorf("settings: idle.after: could not set screensaver delay")
		}
		if !walls.SetEnabled(true) {
			return fmt.Errorf("settings: idle.after: could not enable screensaver")
		}
		cfg.Idle.Lock = 0
		return nil
	case "nothing":
		if walls != nil && !walls.SetEnabled(false) {
			return fmt.Errorf("settings: idle.after: could not disable screensaver")
		}
		cfg.Idle.Lock = 0
		return nil
	default:
		return fmt.Errorf("settings: idle.after: %q is not a valid option", mode)
	}
}

func WhenIdleMode(lock time.Duration, wallsEnabled bool) string {
	if lock > 0 {
		return "lock"
	}
	if wallsEnabled {
		return "screensaver"
	}
	return "nothing"
}

func WhenIdleDelay(lock time.Duration, wallsTimeout string) time.Duration {
	if lock > 0 {
		return lock
	}
	if d, err := time.ParseDuration(wallsTimeout); err == nil && d > 0 {
		return d
	}
	return DefaultIdleDelay
}
