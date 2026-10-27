package shell

import (
	"errors"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/i18n"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
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
	var settingsPub *wayland.Invalidation
	if h := r.panelHosts[PanelSettings]; h != nil {
		r.rebuildPanel(h)
		settingsPub = &wayland.Invalidation{Global: h.output, SurfaceID: panelSurfaceID(PanelSettings)}
	}
	allowed := r.configReloadNoticeAllowedLocked()
	noOutput := len(r.bars) == 0
	showOSD := message != "" && allowed && !noOutput && r.osd != nil
	// A refusal that arrives before the first output has no bar to draw the
	// notice on. It waits for one instead of being lost: a configuration
	// rejected at startup is the one refusal the user cannot see anywhere
	// else, because the shell never reached a state where it could say so.
	r.configReloadNotice = message != "" && allowed && noOutput
	osdText := i18n.T(i18n.Resolve(r.cfg.Session.Language), "config.not-applied", "Config not applied")
	r.mu.Unlock()
	if settingsPub != nil {
		r.publishSurface(settingsPub.Global, settingsPub.SurfaceID)
	}
	if showOSD {
		r.osd.Show(OSDView{Kind: osdConfigError, Text: osdText})
	}
}

// showPendingConfigNotice draws a refusal that arrived before there was an
// output to draw it on. The OSD needs a bar, so a startup refusal waits for
// the first one. It runs outside Registry.mu, on the caller's goroutine.
func (r *Registry) showPendingConfigNotice() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.configReloadNotice || !r.configReloadNoticeAllowedLocked() || len(r.bars) == 0 || r.osd == nil {
		r.mu.Unlock()
		return
	}
	r.configReloadNotice = false
	osdText := i18n.T(i18n.Resolve(r.cfg.Session.Language), "config.not-applied", "Config not applied")
	r.mu.Unlock()
	r.osd.Show(OSDView{Kind: osdConfigError, Text: osdText})
}

// Caller holds Registry.mu. A rejected config can be reported safely over IPC
// while the lock is active, but a transient surface must not appear on top of
// a tracked or managed lock screen.
func (r *Registry) configReloadNoticeAllowedLocked() bool {
	if r.lockerRunning || r.lockerAcquired {
		return false
	}
	switch r.currentLockStateLocked().Phase {
	case "", "idle", "failed-before-acquisition", "unavailable":
		return true
	default:
		return false
	}
}
