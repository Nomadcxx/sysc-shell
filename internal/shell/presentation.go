package shell

import "github.com/Nomadcxx/sysc-notify/protocol"

// presentationView is one record's placement across outputs at one instant:
// which outputs hover it, which show it, which queue it.
type presentationView struct {
	hovered    []string
	visible    []string
	queued     []string
	fullscreen bool
}

func (v presentationView) on(set []string, output string) bool {
	for _, o := range set {
		if o == output {
			return true
		}
	}
	return false
}

func notificationSuppressed(dnd, centerOpen, fullscreen bool, urgency protocol.Urgency) bool {
	return centerOpen || fullscreen || (dnd && urgency != protocol.UrgencyCritical)
}

// aggregatePresentation collapses per-output placement into the one state the
// shell reports: hovered > visible > queued > suppressed. Queued requires
// every configured output to queue the record. DND suppresses ordinary
// notifications; an open centre and zero outputs suppress every notification.
func (r *Registry) aggregatePresentation(id uint32, v presentationView) protocol.PresentationState {
	r.notify.mu.Lock()
	outputs := r.notify.outputs
	dnd := r.notify.dndActiveLocked(r.clockNow())
	centerOpen := r.notify.centerOpen
	urgency := r.notify.active[id].Urgency
	r.notify.mu.Unlock()

	if notificationSuppressed(dnd, centerOpen, v.fullscreen, urgency) || len(outputs) == 0 {
		return protocol.PresentationSuppressed
	}
	for _, o := range outputs {
		if v.on(v.hovered, o) {
			return protocol.PresentationHovered
		}
	}
	for _, o := range outputs {
		if v.on(v.visible, o) {
			return protocol.PresentationVisible
		}
	}
	// An output with no queue entry presents the card directly. Queued needs
	// an explicit queue entry on every configured output.
	queuedEverywhere := len(outputs) > 0
	for _, o := range outputs {
		if !v.on(v.queued, o) {
			queuedEverywhere = false
			break
		}
	}
	if queuedEverywhere {
		return protocol.PresentationQueued
	}
	if len(v.visible) == 0 && len(v.queued) == 0 && len(v.hovered) == 0 {
		// The record is projected nowhere at all.
		return protocol.PresentationSuppressed
	}
	return protocol.PresentationVisible
}

// Test seams.
func (r *Registry) outputsForTest(outputs []string) {
	r.notify.mu.Lock()
	r.notify.outputs = outputs
	r.notify.mu.Unlock()
}

func (r *Registry) setDNDForTest(dnd bool) {
	r.notify.mu.Lock()
	r.notify.dnd = dnd
	r.notify.mu.Unlock()
}

func (r *Registry) setCenterOpen(open bool) {
	r.notify.mu.Lock()
	r.notify.centerOpen = open
	r.notify.mu.Unlock()
	if r.toasts != nil {
		r.toasts.recompute()
	}
}

func (r *Registry) setCenterOpenForTest(open bool) {
	r.setCenterOpen(open)
}
