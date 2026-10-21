package shell

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

// slot holds one handle the shutdown path has to reach without Registry.mu.
//
// The handle lives behind an atomic pointer, so a Close that cannot take the
// lock still finds the resource it owns. A slot is written where the resource
// is created or replaced, and the handle in it is the only copy the shutdown
// path needs.
type slot[T any] struct {
	p atomic.Pointer[T]
}

// store records the handle. A zero handle empties the slot.
func (s *slot[T]) store(handle T) {
	s.p.Store(&handle)
}

// load returns the recorded handle, or the zero value when the slot is empty.
func (s *slot[T]) load() T {
	if p := s.p.Load(); p != nil {
		return *p
	}
	var empty T
	return empty
}

// shutdownState holds the resources Registry.Close owns.
//
// Registry.mu can be stranded by a slow or panicked holder, and a Close that
// cannot take it still has to stop what the shell owns. The handles are
// recorded here instead of read from the registry fields, because reading a
// field the lock protects without the lock is the data race the fallback
// must not have.
type shutdownState struct {
	wallpaper      slot[*wallpaper.Service]
	walls          slot[io.Closer]
	sound          slot[io.Closer]
	privacy        slot[*services.Privacy]
	bluetooth      slot[*services.Bluetooth]
	inhibit        slot[io.Closer]
	launcher       slot[*launcher.Service]
	mediaArt       slot[*mediaArtWorker]
	plugins        slot[*pluginHost]
	polkitCancel   slot[context.CancelFunc]
	thumbCancel    slot[context.CancelFunc]
	trayIconCancel slot[context.CancelFunc]
	bars           slot[[]*Bar]

	// releaseOnce keeps a second Close from releasing the same resources
	// again, and launcherOnce keeps the launcher service from being closed
	// twice: its Close closes a channel, so a second call would panic.
	releaseOnce  sync.Once
	launcherOnce sync.Once
}

// closeLauncher stops the launcher service once. Its Close closes a channel,
// so a second call would panic.
func (s *shutdownState) closeLauncher(svc *launcher.Service) {
	if svc == nil {
		return
	}
	s.launcherOnce.Do(func() { svc.Close() })
}

// release stops the resources the shell owns. It runs without Registry.mu, so
// it only touches handles whose owners keep their own state.
//
// The leases are not listed here: every lease is held by a service, and
// closing a service forgets the leases it handed out and stops it when the
// last one goes. Close releases those services below, outside the lock, on
// this path as well.
func (s *shutdownState) release() {
	s.releaseOnce.Do(func() {
		if handle := s.launcher.load(); handle != nil {
			s.closeLauncher(handle)
		}
		if handle := s.wallpaper.load(); handle != nil {
			handle.Close()
		}
		if handle := s.walls.load(); handle != nil {
			_ = handle.Close()
		}
		if handle := s.sound.load(); handle != nil {
			_ = handle.Close()
		}
		if handle := s.privacy.load(); handle != nil {
			handle.Close()
		}
		if handle := s.bluetooth.load(); handle != nil {
			_ = handle.Close()
		}
		if handle := s.mediaArt.load(); handle != nil {
			handle.Close()
		}
		if handle := s.plugins.load(); handle != nil {
			handle.Close()
		}
		if handle := s.inhibit.load(); handle != nil {
			_ = handle.Close()
		}
		if cancel := s.polkitCancel.load(); cancel != nil {
			cancel()
		}
		if cancel := s.thumbCancel.load(); cancel != nil {
			cancel()
		}
		if cancel := s.trayIconCancel.load(); cancel != nil {
			cancel()
		}
		for _, bar := range s.bars.load() {
			bar.stopAnimation()
		}
	})
}

// barListLocked copies the bars the registry owns so the shutdown path can
// stop their animations without the lock. Caller holds r.mu.
func (r *Registry) barListLocked() []*Bar {
	bars := make([]*Bar, 0, len(r.bars))
	for _, bar := range r.bars {
		bars = append(bars, bar)
	}
	return bars
}
