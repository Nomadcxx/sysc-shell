package shell

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	microphone     privacyActivity
	camera         privacyActivity
	screenShare    privacyActivity
	microphoneApps string
	cameraApps     string
	screenTargets  string
}

const (
	privacyMicrophone  = "microphone"
	privacyCamera      = "camera"
	privacyScreenShare = "screen-share"
	privacyStarted     = "started"
	privacyStopped     = "stopped"
	privacyPaused      = "paused"
	privacyResumed     = "resumed"
	privacyOSDCoalesce = 90 * time.Millisecond
)

type privacyChange struct {
	source string
	action string
}

func privacyStateFrom(pipe services.PrivacySnapshot, snapshot niri.Snapshot) privacyState {
	state := privacyState{
		microphone:    privacyUnknown,
		camera:        privacyUnknown,
		screenShare:   screenShareState(snapshot),
		screenTargets: privacyCastTargets(snapshot),
	}
	if !pipe.Known {
		return state
	}
	if pipe.MicrophoneActive {
		state.microphone = privacyActive
		state.microphoneApps = strings.Join(pipe.MicrophoneApps, ", ")
	} else if pipe.MicrophoneUnknown {
		state.microphone = privacyUnknown
	} else {
		state.microphone = privacyInactive
	}

	castNodes := make(map[uint32]struct{}, len(snapshot.Casts))
	for _, cast := range snapshot.Casts {
		if cast.HasPWNodeID && privacyCastActive(cast) {
			castNodes[cast.PWNodeID] = struct{}{}
		}
	}
	cameraActive, cameraUnknown := false, false
	var cameraApps []string
	for _, path := range pipe.VideoPaths {
		if _, isCast := castNodes[path.SourceNodeID]; isCast {
			continue
		}
		if path.Camera {
			cameraActive = true
			if path.ApplicationName != "" && !slices.Contains(cameraApps, path.ApplicationName) {
				cameraApps = append(cameraApps, path.ApplicationName)
			}
		} else {
			cameraUnknown = true
		}
	}
	slices.Sort(cameraApps)
	state.cameraApps = strings.Join(cameraApps, ", ")
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

func privacyCastTargets(snapshot niri.Snapshot) string {
	var targets []string
	for _, cast := range snapshot.Casts {
		if !privacyCastActive(cast) {
			continue
		}
		target := ""
		switch cast.Target.Kind {
		case "output":
			target = "Output " + privacyTargetName(cast.Target.Name)
		case "window":
			target = "Window " + strconv.FormatUint(uint64(cast.Target.WindowID), 10)
		}
		if target != "" && !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}
	slices.Sort(targets)
	return strings.Join(targets, ", ")
}

func privacyTargetName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	runes := []rune(name)
	if len(runes) > 96 {
		return string(runes[:95]) + "…"
	}
	return name
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

func privacyTransitions(previous, next privacyState, previousNiri, nextNiri niri.Snapshot) []privacyChange {
	var changes []privacyChange
	changes = appendActivityTransition(changes, privacyMicrophone, previous.microphone, next.microphone)
	changes = appendActivityTransition(changes, privacyCamera, previous.camera, next.camera)
	if previous.screenShare != privacyUnknown && next.screenShare != privacyUnknown &&
		previousNiri.CastsKnown && nextNiri.CastsKnown {
		before := make(map[uint64]niri.Cast, len(previousNiri.Casts))
		for _, cast := range previousNiri.Casts {
			before[cast.StreamID] = cast
		}
		after := make(map[uint64]niri.Cast, len(nextNiri.Casts))
		for _, cast := range nextNiri.Casts {
			after[cast.StreamID] = cast
		}
		switch {
		case previous.screenShare == privacyInactive && next.screenShare == privacyActive:
			action := privacyStarted
			for _, cast := range nextNiri.Casts {
				old, exists := before[cast.StreamID]
				if exists && !old.Active && validPrivacyCastTarget(old) && privacyCastActive(cast) {
					action = privacyResumed
					break
				}
			}
			changes = append(changes, privacyChange{source: privacyScreenShare, action: action})
		case previous.screenShare == privacyActive && next.screenShare == privacyInactive:
			action := privacyStopped
			for _, cast := range previousNiri.Casts {
				if !privacyCastActive(cast) {
					continue
				}
				current, exists := after[cast.StreamID]
				if exists && !current.Active {
					action = privacyPaused
					break
				}
			}
			changes = append(changes, privacyChange{source: privacyScreenShare, action: action})
		}
	}
	return mergePrivacyChanges(changes)
}

func privacyCastActive(cast niri.Cast) bool { return cast.Active && validPrivacyCastTarget(cast) }

func appendActivityTransition(changes []privacyChange, source string, previous, next privacyActivity) []privacyChange {
	if previous == privacyUnknown || next == privacyUnknown || previous == next {
		return changes
	}
	if next == privacyActive {
		return append(changes, privacyChange{source: source, action: privacyStarted})
	}
	return append(changes, privacyChange{source: source, action: privacyStopped})
}

func mergePrivacyChanges(groups ...[]privacyChange) []privacyChange {
	seen := make(map[privacyChange]struct{})
	var merged []privacyChange
	for _, group := range groups {
		for _, change := range group {
			if _, exists := seen[change]; exists {
				continue
			}
			seen[change] = struct{}{}
			merged = append(merged, change)
		}
	}
	return merged
}

func privacyChangeText(changes []privacyChange) string {
	changes = mergePrivacyChanges(changes)
	actions := make([]string, 0, len(changes))
	byAction := make(map[string][]string)
	for _, change := range changes {
		if _, exists := byAction[change.action]; !exists {
			actions = append(actions, change.action)
		}
		if !slices.Contains(byAction[change.action], change.source) {
			byAction[change.action] = append(byAction[change.action], change.source)
		}
	}
	var lines []string
	for _, action := range actions {
		sources := byAction[action]
		slices.SortFunc(sources, func(a, b string) int { return privacySourceOrder(a) - privacySourceOrder(b) })
		lines = append(lines, privacySourcesText(sources)+" "+action)
	}
	return strings.Join(lines, " · ")
}

func privacySourceOrder(source string) int {
	switch source {
	case privacyMicrophone:
		return 0
	case privacyCamera:
		return 1
	case privacyScreenShare:
		return 2
	default:
		return 3
	}
}

func privacySourcesText(sources []string) string {
	labels := make([]string, 0, len(sources))
	for _, source := range sources {
		switch source {
		case privacyMicrophone:
			labels = append(labels, "Microphone")
		case privacyCamera:
			labels = append(labels, "camera")
		case privacyScreenShare:
			labels = append(labels, "screen sharing")
		}
	}
	if len(labels) == 0 {
		return "Privacy activity"
	}
	if len(labels) == 1 {
		if sources[0] == privacyCamera {
			return "Camera"
		}
		if sources[0] == privacyScreenShare {
			return "Screen sharing"
		}
		return labels[0]
	}
	if len(labels) == 2 {
		return labels[0] + " and " + labels[1]
	}
	return strings.Join(labels[:len(labels)-1], ", ") + " and " + labels[len(labels)-1]
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
	return "Microphone: " + privacyDetail(state.microphone, state.microphoneApps) +
		"\nCamera: " + privacyDetail(state.camera, state.cameraApps) +
		"\nScreen sharing: " + privacyDetail(state.screenShare, state.screenTargets)
}

func privacyDetail(activity privacyActivity, detail string) string {
	if activity == privacyActive && detail != "" {
		return detail
	}
	return privacyStatus(activity)
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
	if p == nil {
		return
	}
	go r.relayPrivacy(p)
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
	previousNiri := r.niriSnapshot
	r.privacyPipe = snapshot.Clone()
	r.privacy = privacyStateFrom(r.privacyPipe, r.niriSnapshot)
	changes := privacyTransitions(previous, r.privacy, previousNiri, r.niriSnapshot)
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
	r.queuePrivacyOSD(changes)
	if pageOpen {
		r.publishSurface(pageOutput, panelSurfaceID(PanelControlCenter))
	}
}

func (r *Registry) queuePrivacyOSD(changes []privacyChange) {
	if len(changes) == 0 || r.privacyOSD == nil {
		return
	}
	select {
	case r.privacyOSD <- slices.Clone(changes):
	case <-r.closed:
	}
}

func (r *Registry) relayPrivacyOSD() {
	var pending []privacyChange
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case changes := <-r.privacyOSD:
			pending = mergePrivacyChanges(pending, changes)
			if timer == nil {
				timer = time.NewTimer(privacyOSDCoalesce)
				timerC = timer.C
			}
		case <-timerC:
			if text := privacyChangeText(pending); text != "" {
				r.OSD().Show(OSDView{Kind: osdPrivacy, Text: text})
			}
			pending = nil
			timer = nil
			timerC = nil
		case <-r.closed:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}
