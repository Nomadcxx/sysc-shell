package settings

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

const DefaultIdleDelay = 5 * time.Minute

// IdleWalls is the walls unit seam for ApplyWhenIdle. Do not import internal/walls here.
type IdleWalls interface {
	ConfigureIdle(enabled bool, delay time.Duration) error
}

func ApplyWhenIdle(mode string, delay time.Duration, cfg *config.Config, walls IdleWalls) error {
	lock := time.Duration(0)
	switch mode {
	case "lock":
		if delay <= 0 {
			return fmt.Errorf("settings: idle.after: delay %v is not positive", delay)
		}
		if strings.TrimSpace(cfg.Session.Locker) == "" {
			return fmt.Errorf("settings: idle.after: set a locker command first")
		}
		lock = delay
	case "screensaver":
		if delay < time.Second || delay > 24*time.Hour || delay%time.Second != 0 {
			return fmt.Errorf("settings: idle.after: screensaver delay must be whole seconds from 1s to 24h")
		}
		if walls == nil {
			return fmt.Errorf("settings: idle.after: screensaver is not available")
		}
	case "nothing":
	default:
		return fmt.Errorf("settings: idle.after: %q is not a valid option", mode)
	}
	if walls != nil {
		if err := walls.ConfigureIdle(mode == "screensaver", delay); err != nil {
			return fmt.Errorf("settings: idle.after: %w", err)
		}
	}
	cfg.Idle.Lock = lock
	return nil
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
