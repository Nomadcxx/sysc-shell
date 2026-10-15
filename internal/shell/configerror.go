package shell

import (
	"errors"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

const genericConfigReloadError = "configuration reload was rejected"

func configReloadMessage(err error) string {
	if err == nil {
		return ""
	}
	var diagnostic *config.Diagnostic
	if errors.As(err, &diagnostic) && diagnostic != nil {
		return diagnostic.Error()
	}
	return genericConfigReloadError
}

// RecordConfigReload stores the latest safe reload outcome for Settings and
// IPC. It runs on the reload relay, away from the Wayland owner.
func (r *Registry) RecordConfigReload(err error) {
	if r == nil {
		return
	}
	message := configReloadMessage(err)
	r.mu.Lock()
	if message == r.configReloadError {
		r.mu.Unlock()
		return
	}
	r.configReloadError = message
	if message == "" {
		r.configReloadErrorAt = ""
	} else {
		r.configReloadErrorAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	r.mu.Unlock()
}
