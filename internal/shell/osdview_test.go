package shell

import (
	"slices"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPrivacyOSDTransitionsIgnoreBaselinesAndUnknown(t *testing.T) {
	t.Parallel()
	knownInactive := privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyInactive}
	active := knownInactive
	active.microphone = privacyActive
	niriKnown := niri.Snapshot{CastsKnown: true}
	if got := privacyTransitions(privacyState{}, knownInactive, niri.Snapshot{}, niriKnown); len(got) != 0 {
		t.Fatalf("initial baseline changes = %+v, want none", got)
	}
	if got, want := privacyTransitions(knownInactive, active, niriKnown, niriKnown), []privacyChange{
		{source: privacyMicrophone, action: privacyStarted},
	}; !slices.Equal(got, want) {
		t.Fatalf("known start changes = %+v, want %+v", got, want)
	}
	unknown := active
	unknown.microphone = privacyUnknown
	if got := privacyTransitions(active, unknown, niriKnown, niriKnown); len(got) != 0 {
		t.Fatalf("known-to-unknown changes = %+v, want none", got)
	}
	if got := privacyTransitions(unknown, active, niriKnown, niriKnown); len(got) != 0 {
		t.Fatalf("unknown-to-known changes = %+v, want none", got)
	}
}

func TestPrivacyOSDNamesCastStartPauseResumeAndStop(t *testing.T) {
	t.Parallel()
	pipe := services.PrivacySnapshot{Known: true}
	active := niri.Cast{StreamID: 9, Active: true,
		Target: niri.CastTarget{Kind: "output", Name: "DP-1"}, TargetSet: true, TargetKnown: true}
	paused := active
	paused.Active = false
	state := func(casts ...niri.Cast) (privacyState, niri.Snapshot) {
		snapshot := niri.Snapshot{CastsKnown: true, Casts: casts}
		return privacyStateFrom(pipe, snapshot), snapshot
	}
	old, empty := state()
	started, startSnapshot := state(active)
	pausedState, pauseSnapshot := state(paused)
	_, stoppedSnapshot := state()
	cases := []struct {
		name   string
		from   privacyState
		to     privacyState
		before niri.Snapshot
		after  niri.Snapshot
		want   privacyChange
	}{
		{"start", old, started, empty, startSnapshot, privacyChange{source: privacyScreenShare, action: privacyStarted}},
		{"pause", started, pausedState, startSnapshot, pauseSnapshot, privacyChange{source: privacyScreenShare, action: privacyPaused}},
		{"resume", pausedState, started, pauseSnapshot, startSnapshot, privacyChange{source: privacyScreenShare, action: privacyResumed}},
		{"stop", started, old, startSnapshot, stoppedSnapshot, privacyChange{source: privacyScreenShare, action: privacyStopped}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := privacyTransitions(tc.from, tc.to, tc.before, tc.after); !slices.Equal(got, []privacyChange{tc.want}) {
				t.Fatalf("changes = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPrivacyOSDTracksAggregateScreenShareTransitions(t *testing.T) {
	pipe := services.PrivacySnapshot{Known: true}
	cast := func(id uint64, active bool, target niri.CastTarget) niri.Cast {
		return niri.Cast{StreamID: id, Active: active, Target: target, TargetSet: true, TargetKnown: true}
	}
	outputA := niri.CastTarget{Kind: "output", Name: "DP-1"}
	outputB := niri.CastTarget{Kind: "output", Name: "DP-2"}
	nothing := niri.CastTarget{Kind: "nothing"}
	snapshot := func(casts ...niri.Cast) (privacyState, niri.Snapshot) {
		s := niri.Snapshot{CastsKnown: true, Casts: casts}
		return privacyStateFrom(pipe, s), s
	}

	one, oneSnapshot := snapshot(cast(1, true, outputA))
	two, twoSnapshot := snapshot(cast(1, true, outputA), cast(2, true, outputB))
	if got := privacyTransitions(one, two, oneSnapshot, twoSnapshot); len(got) != 0 {
		t.Fatalf("adding a second active cast emitted OSD changes %+v", got)
	}
	if got := privacyTransitions(two, one, twoSnapshot, oneSnapshot); len(got) != 0 {
		t.Fatalf("removing one of two active casts emitted OSD changes %+v", got)
	}
	knownInactive, inactiveSnapshot := snapshot(cast(3, true, nothing))
	activated, activatedSnapshot := snapshot(cast(3, true, outputA))
	if got, want := privacyTransitions(knownInactive, activated, inactiveSnapshot, activatedSnapshot), []privacyChange{{source: privacyScreenShare, action: privacyStarted}}; !slices.Equal(got, want) {
		t.Fatalf("known target activation changes = %+v, want %+v", got, want)
	}
}

func TestPrivacyOSDCoalescesNearbySourceEvents(t *testing.T) {
	t.Parallel()
	if got, want := privacyChangeText([]privacyChange{
		{source: privacyCamera, action: privacyStarted},
		{source: privacyMicrophone, action: privacyStarted},
	}), "Microphone and camera started"; got != want {
		t.Fatalf("privacy change text = %q, want %q", got, want)
	}

	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.queuePrivacyOSD([]privacyChange{{source: privacyMicrophone, action: privacyStarted}})
	r.queuePrivacyOSD([]privacyChange{{source: privacyCamera, action: privacyStarted}})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		view := r.osd.view
		r.mu.Unlock()
		if view.Kind == osdPrivacy {
			if view.Text != "Microphone and camera started" {
				t.Fatalf("privacy OSD text = %q", view.Text)
			}
			if got := osdIcon(view); got != "security" {
				t.Fatalf("privacy OSD icon = %q, want security", got)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("privacy OSD relay did not publish combined changes")
}

func TestPrivacyOSDAnnouncesRegistryCastStart(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.mu.Lock()
	r.privacyPipe = services.PrivacySnapshot{Known: true}
	r.niriSnapshot = niri.Snapshot{CastsKnown: true}
	r.privacy = privacyStateFrom(r.privacyPipe, r.niriSnapshot)
	r.mu.Unlock()

	r.UpdateNiri(niri.Snapshot{CastsKnown: true, Casts: []niri.Cast{{
		StreamID: 3, Active: true,
		Target: niri.CastTarget{Kind: "output", Name: "DP-1"}, TargetSet: true, TargetKnown: true,
	}}})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		view := r.osd.view
		r.mu.Unlock()
		if view.Kind == osdPrivacy {
			if view.Text != "Screen sharing started" {
				t.Fatalf("privacy OSD text = %q", view.Text)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("known cast start did not reach the privacy OSD")
}

func TestOSDIconAndLabelPerKind(t *testing.T) {
	cases := []struct {
		v           OSDView
		icon, label string
	}{
		{OSDView{Kind: osdAudio, Level: 40}, "volume_up", "Volume"},
		{OSDView{Kind: osdAudio, Muted: true}, "volume_off", "Muted"},
		{OSDView{Kind: osdBrightness, Level: 70}, "brightness_high", "Brightness"},
		{OSDView{Kind: osdMedia, Text: "Song", On: true}, "play_arrow", "Song"},
		{OSDView{Kind: osdMedia, Text: "Song"}, "pause", "Song"},
		{OSDView{Kind: osdMedia, Muted: true}, "music_note", "Stopped"},
		{OSDView{Kind: osdMedia, On: true}, "play_arrow", "Media"},
		{OSDView{Kind: osdCapsLock, On: true}, "keyboard", "Caps Lock on"},
		{OSDView{Kind: osdNumLock}, "keyboard", "Num Lock off"},
		{OSDView{Kind: osdLayout, Text: "German"}, "", "German"},
		{OSDView{Kind: osdConfigError, Text: "Config not applied"}, "notifications", "Config not applied"},
		{OSDView{Kind: osdDND, On: true}, "do_not_disturb_on", "Do not disturb on"},
		{OSDView{Kind: osdDND}, "notifications", "Do not disturb off"},
	}
	for _, tc := range cases {
		if got := osdIcon(tc.v); got != tc.icon {
			t.Errorf("%+v icon = %q, want %q", tc.v, got, tc.icon)
		}
		if got := osdLabel(tc.v); got != tc.label {
			t.Errorf("%+v label = %q, want %q", tc.v, got, tc.label)
		}
	}
}

func TestOSDTreeShapePerKind(t *testing.T) {
	count := func(n *ui.Node, k ui.Kind) int {
		c := 0
		var walk func(*ui.Node)
		walk = func(n *ui.Node) {
			if n == nil {
				return
			}
			if n.Kind == k {
				c++
			}
			for _, ch := range n.Children {
				walk(ch)
			}
		}
		walk(n)
		return c
	}
	for _, kind := range []string{osdAudio, osdBrightness, osdMedia} {
		tree := osdTree(OSDView{Kind: kind, Level: 50})
		if tree.Kind != ui.KindColumn || count(tree, ui.KindMeter) != 1 {
			t.Errorf("%s: want a column root with one meter", kind)
		}
	}
	for _, kind := range []string{osdCapsLock, osdNumLock, osdDND, osdLayout} {
		if count(osdTree(OSDView{Kind: kind}), ui.KindMeter) != 0 {
			t.Errorf("%s: toggle and text kinds carry no meter", kind)
		}
	}
	if count(osdTree(OSDView{Kind: osdLayout, Text: "German"}), ui.KindIcon) != 0 {
		t.Error("layout is text-only")
	}
}

func TestConfigErrorOSDHasSafeTextAndNoMeter(t *testing.T) {
	view := OSDView{Kind: osdConfigError}
	if got := osdLabel(view); got != "Config not applied" {
		t.Fatalf("label = %q, want fixed safe summary", got)
	}
	if got := osdIcon(view); got != "notifications" {
		t.Fatalf("icon = %q, want an existing notification glyph", got)
	}
	if _, ok := render.IconByName(osdIcon(view)); !ok && !render.ValidMaterialIcon(osdIcon(view)) {
		t.Fatalf("icon %q is not in the shipped subset", osdIcon(view))
	}

	root := osdTree(view)
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindMeter || n.Focusable {
			t.Fatalf("config-error OSD acquired meter or focus: %+v", n)
		}
		if n.Kind == ui.KindIcon || n.Kind == ui.KindText {
			if n.Tone != ui.ToneError {
				t.Errorf("config-error %v tone = %v, want error", n.Kind, n.Tone)
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
}

// Every kind lays out inside the 220x64 OSD body with real layout rules.
func TestOSDTreesFitTheBody(t *testing.T) {
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 7, 16 }
	body := ui.Rect{W: osdWidth - 16, H: osdHeight - 16}
	for _, v := range []OSDView{
		{Kind: osdAudio, Level: 100}, {Kind: osdBrightness}, {Kind: osdCapsLock, On: true},
		{Kind: osdMedia, Text: "A very long track title that must be clipped not overflow", Level: 30, On: true},
		{Kind: osdLayout, Text: "English (US, intl., with dead keys)"},
		{Kind: osdConfigError}, {Kind: osdDND, On: true},
	} {
		root := osdTree(v)
		if err := ui.LayoutColumn(root, body, measure); err != nil {
			t.Errorf("%+v: %v", v, err)
			continue
		}
		var checkBounds func(*ui.Node)
		checkBounds = func(n *ui.Node) {
			b := n.Bounds
			if b.X < body.X || b.Y < body.Y || b.X+b.W > body.X+body.W || b.Y+b.H > body.Y+body.H {
				t.Errorf("%+v: node %v outside OSD body: %+v", v, n.Kind, b)
			}
			for _, child := range n.Children {
				checkBounds(child)
			}
		}
		checkBounds(root)
	}
}

func TestOSDHandleRidesTheFillAndCrossfadesNearFull(t *testing.T) {
	cases := []struct {
		level      int
		x          int
		icon, text uint8
	}{
		{0, 10, 100, 0},
		{50, 60, 100, 0},
		{85, 95, 100, 0},
		{90, 100, 50, 50},
		{95, 105, 0, 100},
		{100, 110, 0, 100},
		{140, 110, 0, 100},
		{-5, 10, 100, 0},
	}
	for _, tc := range cases {
		x, icon, text := osdHandle(tc.level, 10, 100)
		if x != tc.x || icon != tc.icon || text != tc.text {
			t.Errorf("level %d: x=%d icon=%d text=%d, want %d %d %d", tc.level, x, icon, text, tc.x, tc.icon, tc.text)
		}
	}
}

func TestOSDHandleNodesOnlyForMeteredKinds(t *testing.T) {
	meter := &ui.Node{Kind: ui.KindMeter, Bounds: ui.Rect{X: 40, Y: 30, W: 150, H: 6}}
	if n := osdHandleNodes(OSDView{Kind: osdCapsLock}, meter); len(n) != 0 {
		t.Fatal("a toggle kind got a handle")
	}
	nodes := osdHandleNodes(OSDView{Kind: osdAudio, Level: 90}, meter)
	if len(nodes) != 2 || nodes[0].Opacity != 50 || nodes[1].Text != "90%" {
		t.Fatalf("handle nodes at 90%%: %+v", nodes)
	}
}
