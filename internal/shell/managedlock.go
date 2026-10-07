package shell

import (
	"context"
	"fmt"
	locksession "github.com/Nomadcxx/sysc-shell/internal/lock"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
	"log"
	"os"
	"path/filepath"
	"time"
)

func (r *Registry) initManagedLock() {
	ctx, cancel := context.WithCancel(context.Background())
	r.lockCancel = cancel
	r.backgroundRequests = make(chan struct{}, 1)
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" || !filepath.IsAbs(runtimeDir) {
		r.backgroundError = "XDG_RUNTIME_DIR must be an absolute path"
		r.backgroundHeld = true
	} else {
		r.backgroundLeasePath = filepath.Join(runtimeDir, "sysc-shell", "lock-background.json")
		lease, err := locksession.LoadLease(r.backgroundLeasePath)
		if err != nil {
			r.backgroundError = err.Error()
			r.backgroundHeld = true
		} else if lease != nil {
			// The previous locker left a lease behind: wallpaper and
			// screensaver stay held until the next seal, and the reason has
			// to be somewhere the user can read it.
			r.backgroundHeld = true
			r.backgroundError = "the last lock session did not finish; background is held until the next unlock"
		}
	}
	client := locksession.New(os.Getenv("XDG_SESSION_ID"), os.Getenv("NIRI_SOCKET"))
	client.Start(ctx)
	r.managedLock = client
	r.managedState = client.State()
	if isManagedLocker(sessionArgv("session-lock", r.cfg.Session.Locker)) &&
		!lockStateAllowsBackground(r.managedState) {
		r.backgroundHeld = true
		if r.backgroundError == "" {
			if r.managedState.Known {
				r.backgroundError = "the lock owner is mid-lock; background is held"
			} else {
				// An owner the shell cannot reach is not a lock in progress;
				// "mid-lock" here sent people looking for a lock that was
				// never happening (#114).
				r.backgroundError = "the lock session service is not reporting; background is held until it does"
			}
		}
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.backgroundRequests:
				r.coordinateBackground()
			}
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case state := <-client.Updates():
				r.applyManagedSnapshot(state)
			}
		}
	}()
}

// Coordination runs off the Wayland owner; failures never delay sealing.
func (r *Registry) coordinateBackground() {
	r.backgroundMu.Lock()
	defer r.backgroundMu.Unlock()
	r.mu.Lock()
	state, svc, path := r.currentLockStateLocked(), r.wallsService, r.backgroundLeasePath
	r.mu.Unlock()
	if path == "" {
		return
	}
	lease, err := locksession.LoadLease(path)
	if err != nil {
		r.backgroundFailure(err)
		return
	}
	if state.Known && state.Phase == "sealed" {
		if lease == nil {
			lease = &locksession.Lease{Session: state.Session, Compositor: state.Compositor, Generation: state.Generation}
			if svc != nil {
				snap := svc.Snapshot()
				lease.WallsKnown = snap.UnitKnown && !snap.UnitStale
				lease.WallsRunning = snap.Running()
			}
			if err = lease.Save(path); err != nil {
				r.backgroundFailure(err)
				return
			}
		}
		if lease.Session != state.Session || lease.Compositor != state.Compositor {
			r.backgroundFailure(fmt.Errorf("background lease belongs to another session"))
			return
		}
		// Keep the original resource snapshot; the latest acquisition owns its receipt.
		if lease.Generation < state.Generation {
			lease.Generation = state.Generation
			if err = lease.Save(path); err != nil {
				r.backgroundFailure(err)
				return
			}
		}
		r.mu.Lock()
		r.backgroundHeld = true
		wall := r.wallpaperSvc
		r.mu.Unlock()
		if wall != nil {
			wall.Enqueue(wallpaper.Command{Op: wallpaper.OpPause, Token: wallpaper.AllOutputs})
		}
		if svc != nil {
			svc.StopPreview()
			if !lease.WallsKnown {
				snap := svc.Snapshot()
				if snap.UnitKnown && !snap.UnitStale {
					lease.WallsKnown = true
					lease.WallsRunning = snap.Running()
					if err = lease.Save(path); err != nil {
						r.backgroundFailure(err)
						return
					}
				} else {
					r.backgroundFailure(fmt.Errorf("screensaver state unavailable"))
					return
				}
			}
			if svc.Snapshot().Running() && !svc.SetRuntimeRunning(false) {
				r.backgroundFailure(fmt.Errorf("screensaver stop unavailable"))
			}
		}
		return
	}
	if lease == nil {
		// No resource snapshot exists to restore. A fresh known owner can admit ordinary startup.
		r.mu.Lock()
		current := r.currentLockStateLocked()
		if lockStateAllowsBackground(current) && r.backgroundHeld {
			r.backgroundHeld = false
			r.wallpaperStartLocked()
		}
		r.mu.Unlock()
		return
	}
	if !lease.CanRestore(state) {
		return
	}
	if lease.WallsKnown && lease.WallsRunning {
		r.mu.Lock()
		valid := lease.CanRestore(r.currentLockStateLocked())
		started := valid && svc != nil && svc.SetRuntimeRunning(true)
		r.mu.Unlock()
		if !valid {
			return
		}
		if !started {
			r.backgroundFailure(fmt.Errorf("screensaver restore unavailable"))
			return
		}
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		poll := time.NewTicker(50 * time.Millisecond)
		defer poll.Stop()
		for {
			r.mu.Lock()
			valid := lease.CanRestore(r.currentLockStateLocked())
			r.mu.Unlock()
			if !valid {
				svc.SetRuntimeRunning(false)
				r.queueBackground()
				return
			}
			snap := svc.Snapshot()
			if snap.Running() && !snap.ActionPending {
				break
			}
			if snap.ActionError != "" && !snap.ActionPending {
				r.backgroundFailure(fmt.Errorf("screensaver restore failed"))
				return
			}
			select {
			case <-r.closed:
				return
			case <-deadline.C:
				r.backgroundFailure(fmt.Errorf("screensaver restore deadline expired"))
				return
			case <-poll.C:
			}
		}
	}
	r.mu.Lock()
	if !lease.CanRestore(r.currentLockStateLocked()) {
		r.mu.Unlock()
		if svc != nil && lease.WallsRunning {
			svc.SetRuntimeRunning(false)
		}
		r.queueBackground()
		return
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		r.mu.Unlock()
		r.backgroundFailure(err)
		return
	}
	r.backgroundHeld = false
	r.backgroundError = ""
	wall := r.wallpaperStartLocked()
	r.mu.Unlock()
	if wall != nil {
		wall.Enqueue(wallpaper.Command{Op: wallpaper.OpResume, Token: wallpaper.AllOutputs})
	}
}
func (r *Registry) backgroundFailure(err error) {
	log.Printf("shell: lock background coordination: %v", err)
	r.mu.Lock()
	r.backgroundError = err.Error()
	r.mu.Unlock()
}

// Caller holds Registry.mu; RPC replies may precede the UI relay.
func (r *Registry) currentLockStateLocked() locksession.State {
	if r.managedLock != nil {
		return r.managedLock.State()
	}
	return r.managedState
}
func (r *Registry) queueBackground() {
	select {
	case r.backgroundRequests <- struct{}{}:
	default:
	}
}

func (r *Registry) applyManagedSnapshot(state locksession.State) {
	r.mu.Lock()
	r.managedState = state
	r.lockerRunning = state.Phase != "idle" && state.Phase != "failed-before-acquisition" && state.Phase != "unavailable"
	r.lockerAcquired = state.Known && state.Phase == "sealed"
	var publish []waylandSurface
	for _, id := range []PanelID{PanelControlCenter, PanelSettings} {
		if h := r.panelHosts[id]; h != nil {
			r.rebuildPanel(h)
			publish = append(publish, waylandSurface{output: h.output, id: panelSurfaceID(id)})
		}
	}
	r.mu.Unlock()
	for _, surface := range publish {
		r.publishSurface(surface.output, surface.id)
	}
	r.queueBackground()
}

func lockStateAllowsBackground(s locksession.State) bool {
	return s.Known && (s.Phase == "idle" || s.Phase == "failed-before-acquisition")
}
