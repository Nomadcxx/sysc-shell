package shell

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/walls"
)

type wallsController interface {
	Snapshot() walls.Snapshot
	Updates() <-chan walls.Snapshot
	Refresh()
	Apply([]walls.Setting) bool
	SetEnabled(bool) bool
	SetRuntimeRunning(bool) bool
	Preview() bool
	StopPreview() bool
	Close() error
}

type idleWallsAdapter struct{ r *Registry }

func (a idleWallsAdapter) SetEnabled(on bool) bool { return setWallsEnabled(a.r, on) }

func (a idleWallsAdapter) SetTimeout(d time.Duration) bool {
	if a.r == nil || a.r.lockerAcquired || a.r.wallsService == nil ||
		!a.r.wallsSnapshot.CanApply || a.r.wallsSnapshot.ActionPending || a.r.wallsSnapshot.Previewing {
		return false
	}
	return a.r.wallsService.Apply([]walls.Setting{{Key: "timeout", Value: wallsIdleTimeout(d)}})
}

// wallsIdleTimeout is the sysc-walls wire format: one whole number plus s, m, or h.
// time.Duration.String() emits 5m0s, which parseIdleTimeout rejects.
func wallsIdleTimeout(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return fmt.Sprintf("%ds", d/time.Second)
}

func idleWallsFor(r *Registry) settings.IdleWalls {
	if r == nil || r.wallsService == nil {
		return nil
	}
	return idleWallsAdapter{r}
}

var wallsSettingOrder = []string{"effect", "theme", "timeout", "file", "datetime", "datetime-position"}

var wallsDefaults = map[string]string{
	"effect": "matrix-art", "theme": "rama", "timeout": "5m", "file": "",
	"datetime": "false", "datetime-position": "bottom",
}

// SetWallsService attaches the one service that owns sysc-walls discovery and
// commands. The service already queues its initial refresh when constructed.
func (r *Registry) SetWallsService(svc *walls.Service) {
	if svc != nil {
		r.attachWallsService(svc)
	}
}

func (r *Registry) attachWallsService(svc wallsController) {
	if r == nil || svc == nil {
		return
	}
	r.mu.Lock()
	if r.wallsService != nil {
		r.mu.Unlock()
		return
	}
	select {
	case <-r.closed:
		r.mu.Unlock()
		return
	default:
	}
	r.wallsService = svc
	r.wallsSnapshot = svc.Snapshot()
	r.mu.Unlock()
	go r.relayWalls(svc)
}

func (r *Registry) relayWalls(svc wallsController) {
	for {
		select {
		case <-r.closed:
			return
		case snapshot, ok := <-svc.Updates():
			if !ok {
				return
			}
			r.applyWallsSnapshot(svc, snapshot)
		}
	}
}

func (r *Registry) applyWallsSnapshot(svc wallsController, snapshot walls.Snapshot) {
	r.mu.Lock()
	if r.wallsService != svc {
		r.mu.Unlock()
		return
	}
	if snapshot.ActionPending && r.wallsSnapshot.EnabledAtLogin() != snapshot.EnabledAtLogin() {
		snapshot.UnitFileState = r.wallsSnapshot.UnitFileState
		snapshot.UnitKnown = r.wallsSnapshot.UnitKnown
		snapshot.UnitStale = r.wallsSnapshot.UnitStale
	}
	r.wallsSnapshot = snapshot
	var publish []waylandSurface
	if h := r.panelHosts[PanelSettings]; h != nil && (h.section == "Screensaver" || h.section == "Session") {
		if h.section == "Screensaver" {
			syncWallsDraft(h, snapshot)
		}
		r.rebuildPanel(h)
		publish = append(publish, waylandSurface{output: h.output, id: panelSurfaceID(h.id)})
	}
	if h := r.panelHosts[PanelControlCenter]; h != nil && h.section == "home" {
		r.rebuildPanel(h)
		publish = append(publish, waylandSurface{output: h.output, id: panelSurfaceID(h.id)})
	}
	r.mu.Unlock()
	for _, surface := range publish {
		r.publishSurface(surface.output, surface.id)
	}
}

// waylandSurface is the small publish list collected while Registry.mu is
// held; invalidations are sent only after the shared snapshot and trees agree.
type waylandSurface struct {
	output uint32
	id     string
}

func (r *Registry) refreshWallsLocked() {
	if r.wallsService != nil {
		r.wallsService.Refresh()
	}
}

func wallsDraftValues(snapshot walls.Snapshot) map[string]string {
	values := make(map[string]string, len(wallsSettingOrder))
	for _, key := range wallsSettingOrder {
		values[key] = wallsSavedValue(snapshot, key)
	}
	return values
}

func wallsSavedValue(snapshot walls.Snapshot, key string) string {
	switch key {
	case "effect":
		return snapshot.Effect
	case "theme":
		return snapshot.Theme
	case "timeout":
		return snapshot.Timeout
	case "file":
		return snapshot.Artwork
	case "datetime":
		return snapshot.DateTime
	case "datetime-position":
		return snapshot.DateTimePosition
	default:
		return ""
	}
}

func ensureWallsDraft(h *PanelHost, snapshot walls.Snapshot) {
	if h.wallsDraftReady {
		return
	}
	h.wallsDraft = wallsDraftValues(snapshot)
	h.wallsDirty = make(map[string]bool)
	h.wallsDraftReady = true
}

func syncWallsDraft(h *PanelHost, snapshot walls.Snapshot) {
	ensureWallsDraft(h, snapshot)
	for _, key := range wallsSettingOrder {
		saved := wallsSavedValue(snapshot, key)
		if h.wallsDirty[key] && h.wallsDraft[key] == saved && snapshot.ConfigReadable {
			delete(h.wallsDirty, key)
		}
		if !h.wallsDirty[key] {
			h.wallsDraft[key] = saved
			if field := h.fields["walls."+key]; field != nil && field.Text != saved {
				h.fields["walls."+key] = ui.NewField(saved)
			}
		}
	}
}

func wallsDraftIsDirty(h *PanelHost) bool {
	for _, key := range wallsSettingOrder {
		if h.wallsDirty[key] {
			return true
		}
	}
	return false
}

func setWallsDraft(r *Registry, h *PanelHost, key, value string) bool {
	if r == nil || h == nil || r.lockerAcquired || !containsWallsSetting(key) {
		return false
	}
	ensureWallsDraft(h, r.wallsSnapshot)
	h.wallsDraft[key] = value
	if value == wallsSavedValue(r.wallsSnapshot, key) {
		delete(h.wallsDirty, key)
	} else {
		h.wallsDirty[key] = true
	}
	if field := h.fields["walls."+key]; field != nil && field.Text != value {
		h.fields["walls."+key] = ui.NewField(value)
	}
	r.rebuildPanel(h)
	return true
}

func containsWallsSetting(key string) bool {
	for _, candidate := range wallsSettingOrder {
		if key == candidate {
			return true
		}
	}
	return false
}

func resetWallsDraft(r *Registry, h *PanelHost) bool {
	if r == nil || h == nil || r.lockerAcquired {
		return false
	}
	ensureWallsDraft(h, r.wallsSnapshot)
	for _, key := range wallsSettingOrder {
		value := wallsDefaults[key]
		h.wallsDraft[key] = value
		if value == wallsSavedValue(r.wallsSnapshot, key) {
			delete(h.wallsDirty, key)
		} else {
			h.wallsDirty[key] = true
		}
		if field := h.fields["walls."+key]; field != nil {
			h.fields["walls."+key] = ui.NewField(value)
		}
	}
	r.rebuildPanel(h)
	return true
}

func applyWallsDraft(r *Registry, h *PanelHost) bool {
	if r == nil || h == nil || r.lockerAcquired || r.wallsService == nil ||
		!r.wallsSnapshot.CanApply || r.wallsSnapshot.ActionPending || r.wallsSnapshot.Previewing {
		return false
	}
	patch := make([]walls.Setting, 0, len(h.wallsDirty))
	for _, key := range wallsSettingOrder {
		if h.wallsDirty[key] {
			patch = append(patch, walls.Setting{Key: key, Value: h.wallsDraft[key]})
		}
	}
	if len(patch) == 0 {
		return false
	}
	return r.wallsService.Apply(patch)
}

func setWallsEnabled(r *Registry, enabled bool) bool {
	if r == nil || r.lockerAcquired || r.wallsService == nil || r.wallsSnapshot.ActionPending ||
		!r.wallsSnapshot.ServiceAvailable || !r.wallsSnapshot.UnitKnown || r.wallsSnapshot.UnitStale || r.wallsSnapshot.Previewing ||
		(r.wallsSnapshot.UnitFileState != "enabled" && r.wallsSnapshot.UnitFileState != "disabled") {
		return false
	}
	return r.wallsService.SetEnabled(enabled)
}

func activateWallsAction(r *Registry, h *PanelHost, n *ui.Node) bool {
	if r == nil || h == nil || n == nil {
		return false
	}
	if r.lockerAcquired {
		return true
	}
	switch n.Action {
	case "walls:enable":
		ui.Activate(n)
		if !setWallsEnabled(r, n.Value != 0) {
			h.errLabel = "Enablement is unavailable until the service state is known and supported."
			r.rebuildPanel(h)
		}
		return true
	case "walls:apply":
		if !applyWallsDraft(r, h) && wallsDraftIsDirty(h) {
			h.errLabel = "Settings could not be queued. Check the service and configuration status."
			r.rebuildPanel(h)
		}
		return true
	case "walls:reset":
		resetWallsDraft(r, h)
		return true
	case "walls:preview":
		if r.wallsService != nil && r.wallsSnapshot.CanPreview && !r.wallsSnapshot.ActionPending {
			r.wallsService.Preview()
		}
		return true
	case "walls:stop":
		if r.wallsService != nil {
			r.wallsService.StopPreview()
		}
		return true
	default:
		return false
	}
}

func wallsUnitEnabledState(snapshot walls.Snapshot) string {
	if !snapshot.UnitKnown || snapshot.UnitStale {
		return "Enablement unavailable"
	}
	switch snapshot.UnitFileState {
	case "enabled":
		return "Enabled at login"
	case "enabled-runtime":
		return "Enabled this session"
	case "disabled":
		return "Disabled"
	case "not-found":
		return "Service not installed"
	case "":
		return "Enablement unavailable"
	default:
		return "Service state: " + snapshot.UnitFileState
	}
}

func wallsUnitRunningState(snapshot walls.Snapshot) string {
	if !snapshot.UnitKnown || snapshot.UnitStale {
		return "Runtime state unavailable"
	}
	switch {
	case snapshot.ActiveState == "active" && snapshot.SubState == "running":
		return "Running"
	case snapshot.ActiveState == "active":
		return "Active (" + snapshot.SubState + ")"
	case snapshot.ActiveState == "activating":
		return "Starting"
	case snapshot.ActiveState == "deactivating":
		return "Stopping"
	case snapshot.ActiveState == "failed":
		return "Failed"
	case snapshot.ActiveState == "inactive":
		return "Stopped"
	case snapshot.ActiveState != "":
		return "Runtime state: " + snapshot.ActiveState + "/" + snapshot.SubState
	default:
		return "Runtime state unavailable"
	}
}

func wallsServiceStatus(snapshot walls.Snapshot) string {
	if snapshot.UnitKnown && !snapshot.UnitStale && snapshot.LoadState == "not-found" {
		return "Screensaver service is not installed"
	}
	if !snapshot.UnitKnown || snapshot.UnitStale {
		if snapshot.StateError != "" {
			return "Service state unavailable: " + snapshot.StateError
		}
		return "Service state unavailable"
	}
	return wallsUnitEnabledState(snapshot) + " · " + wallsUnitRunningState(snapshot)
}

func wallsConfigStatus(snapshot walls.Snapshot) string {
	if !snapshot.ConfigKnown {
		return "Waiting for configuration discovery"
	}
	if !snapshot.ConfigReadable {
		if snapshot.ConfigReadError != "" {
			return "Configuration unavailable: " + snapshot.ConfigReadError
		}
		return "Configuration unavailable"
	}
	if !snapshot.ConfigExists {
		return "Using sysc-walls defaults; no file has been created"
	}
	return "Using " + snapshot.ConfigPath
}

func wallsCapabilityReason(snapshot walls.Snapshot, capability string) string {
	switch capability {
	case "apply":
		for _, reason := range []string{snapshot.ConfigSourceError, snapshot.ConfigReadError, snapshot.ClientIdentityError} {
			if strings.TrimSpace(reason) != "" {
				return reason
			}
		}
		if !snapshot.CanApply {
			return "The matching sysc-walls client and configuration have not been verified."
		}
	case "preview":
		for _, reason := range []string{snapshot.PreviewAvailabilityError, snapshot.ConfigSourceError, snapshot.ConfigReadError, snapshot.DaemonIdentityError, snapshot.DisplayIdentityError} {
			if strings.TrimSpace(reason) != "" {
				return reason
			}
		}
		if !snapshot.CanPreview {
			return "The preview tools, configuration, and service state have not been verified."
		}
	}
	return ""
}
