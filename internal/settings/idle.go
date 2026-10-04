package settings

import "time"

const DefaultIdleDelay = 5 * time.Minute

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
