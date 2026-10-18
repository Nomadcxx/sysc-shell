package shell

import (
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

type privacyActivity uint8

const (
	privacyUnknown privacyActivity = iota
	privacyInactive
	privacyActive
)

type privacyState struct {
	microphone  privacyActivity
	camera      privacyActivity
	screenShare privacyActivity
}

func privacyStateFrom(pipe services.PrivacySnapshot, snapshot niri.Snapshot) privacyState {
	state := privacyState{
		microphone:  privacyUnknown,
		camera:      privacyUnknown,
		screenShare: screenShareState(snapshot),
	}
	if !pipe.Known {
		return state
	}
	if pipe.MicrophoneActive {
		state.microphone = privacyActive
	} else if pipe.MicrophoneUnknown {
		state.microphone = privacyUnknown
	} else {
		state.microphone = privacyInactive
	}

	castNodes := make(map[uint32]privacyActivity, len(snapshot.Casts))
	for _, cast := range snapshot.Casts {
		if !cast.HasPWNodeID || !cast.Active || cast.TargetKnown && cast.Target.Kind == "nothing" {
			continue
		}
		activity := privacyUnknown
		if validPrivacyCastTarget(cast) {
			activity = privacyActive
		}
		if castNodes[cast.PWNodeID] != privacyActive {
			castNodes[cast.PWNodeID] = activity
		}
	}
	cameraActive, cameraUnknown := false, false
	for _, path := range pipe.VideoPaths {
		if cast, isCast := castNodes[path.SourceNodeID]; isCast {
			if cast == privacyUnknown {
				cameraUnknown = true
			}
			continue
		}
		if path.Camera {
			cameraActive = true
		} else {
			cameraUnknown = true
		}
	}
	switch {
	case cameraActive:
		state.camera = privacyActive
	case cameraUnknown:
		state.camera = privacyUnknown
	default:
		state.camera = privacyInactive
	}
	return state
}

func screenShareState(snapshot niri.Snapshot) privacyActivity {
	active, unknown := false, !snapshot.CastsKnown
	for _, cast := range snapshot.Casts {
		if !cast.Active {
			continue
		}
		if validPrivacyCastTarget(cast) {
			active = true
		} else if !cast.TargetKnown || cast.Target.Kind != "nothing" {
			unknown = true
		}
	}
	switch {
	case active:
		return privacyActive
	case unknown:
		return privacyUnknown
	default:
		return privacyInactive
	}
}

func validPrivacyCastTarget(cast niri.Cast) bool {
	if !cast.TargetKnown || !cast.TargetSet {
		return false
	}
	switch cast.Target.Kind {
	case "output":
		return strings.TrimSpace(cast.Target.Name) != ""
	case "window":
		return cast.Target.WindowID > 0
	default:
		return false
	}
}

func privacyStatus(activity privacyActivity) string {
	switch activity {
	case privacyActive:
		return "In use"
	case privacyInactive:
		return "Not in use"
	default:
		return "Unavailable"
	}
}

func privacyAccessibleName(state privacyState) string {
	return "Privacy activity. Microphone " + strings.ToLower(privacyStatus(state.microphone)) +
		". Camera " + strings.ToLower(privacyStatus(state.camera)) +
		". Screen sharing " + strings.ToLower(privacyStatus(state.screenShare))
}

func privacyTooltip(state privacyState) string {
	return "Microphone: " + privacyStatus(state.microphone) +
		"\nCamera: " + privacyStatus(state.camera) +
		"\nScreen sharing: " + privacyStatus(state.screenShare)
}

func (r *Registry) setPrivacyService(p *services.Privacy) {
	r.mu.Lock()
	old := r.privacySvc
	previous := r.privacy
	r.privacySvc = p
	r.privacyPipe = services.PrivacySnapshot{}
	if p != nil {
		r.privacyPipe = p.Snapshot()
	}
	r.privacy = privacyStateFrom(r.privacyPipe, r.niriSnapshot)
	changed := make([]uint32, 0, len(r.bars))
	if r.privacy != previous {
		for global, bar := range r.bars {
			if bar.apply(r.viewLocked(bar.connector())) {
				changed = append(changed, global)
			}
		}
	}
	var pageOutput uint32
	pageOpen := false
	if r.privacy != previous {
		if h := r.panelHosts[PanelControlCenter]; h != nil && h.section == "privacy" {
			r.rebuildPanel(h)
			pageOutput, pageOpen = h.output, true
		}
	}
	r.mu.Unlock()
	if old != nil && old != p {
		old.Close()
	}
	r.publish(changed)
	if pageOpen {
		r.publishSurface(pageOutput, panelSurfaceID(PanelControlCenter))
	}
	if p != nil {
		go r.relayPrivacy(p)
	}
}

func (r *Registry) relayPrivacy(p *services.Privacy) {
	if p == nil {
		return
	}
	for {
		select {
		case <-r.closed:
			return
		case snapshot := <-p.Changes():
			r.publishPrivacySnapshot(p, snapshot)
		}
	}
}

func (r *Registry) publishPrivacySnapshot(p *services.Privacy, snapshot services.PrivacySnapshot) {
	r.mu.Lock()
	if r.privacySvc != p {
		r.mu.Unlock()
		return
	}
	previous := r.privacy
	r.privacyPipe = snapshot.Clone()
	r.privacy = privacyStateFrom(r.privacyPipe, r.niriSnapshot)
	if r.privacy == previous {
		r.mu.Unlock()
		return
	}
	changed := make([]uint32, 0, len(r.bars))
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	var pageOutput uint32
	pageOpen := false
	if h := r.panelHosts[PanelControlCenter]; h != nil && h.section == "privacy" {
		r.rebuildPanel(h)
		pageOutput, pageOpen = h.output, true
	}
	r.mu.Unlock()
	r.publish(changed)
	if pageOpen {
		r.publishSurface(pageOutput, panelSurfaceID(PanelControlCenter))
	}
}
