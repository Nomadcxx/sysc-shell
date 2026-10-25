package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestCapabilityWidgetsRenderWithoutTheCapability pins the absent-capability
// path for each of the six widgets. A bar must keep its layout when the
// backing service is missing: the widget either paints nothing (absent) or
// paints the reachable off state, never a placeholder glyph with no meaning.
func TestCapabilityWidgetsRenderWithoutTheCapability(t *testing.T) {
	t.Parallel()
	// The zero barView names no capability: no night-light gamma control, no
	// brightness service, no profile list, no lock-key lease, the inhibitor
	// off, and the theme at its default mode.
	views := map[string]func() barView{
		"nightlight":     func() barView { return barView{} },
		"idle_inhibitor": func() barView { return barView{} },
		"brightness":     func() barView { return barView{} },
		"power_profile":  func() barView { return barView{} },
		"lock_keys":      func() barView { return barView{} },
		"theme_mode":     func() barView { return barView{} },
	}
	wantAbsent := map[string]bool{
		"nightlight":     true,
		"idle_inhibitor": false,
		"brightness":     true,
		"power_profile":  true,
		"lock_keys":      true,
		// The mode always has a value, so a theme toggle must stay reachable.
		"theme_mode": false,
	}
	wantAction := map[string]string{
		"nightlight":     panelNightLightAction,
		"idle_inhibitor": panelIdleInhibitorAction,
		"brightness":     panelBrightnessAction,
		"power_profile":  panelPowerProfileAction,
		"lock_keys":      panelLockKeysAction,
		"theme_mode":     panelThemeModeAction,
	}
	for _, id := range []string{"nightlight", "idle_inhibitor", "brightness", "power_profile", "lock_keys", "theme_mode"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			got := buildWidgets([]config.Item{{ID: id}}, 8, standardMetrics())
			if len(got) != 1 {
				t.Fatalf("built %d widgets, want 1", len(got))
			}
			w := got[0]
			if w.refresh == nil {
				t.Fatal("the widget has no refresh, so nothing can ever paint it")
			}
			if w.tooltip == "" {
				t.Error("the widget carries no tooltip")
			}
			if got[0].node.Action != wantAction[id] {
				t.Errorf("action = %q, want %q", got[0].node.Action, wantAction[id])
			}
			w.refresh(views[id]())
			if w.node.Absent != wantAbsent[id] {
				t.Errorf("absent = %v, want %v", w.node.Absent, wantAbsent[id])
			}
		})
	}
}

// TestCapabilityWidgetsPaintTheirStates exercises the non-absent side of each
// widget so the tone and text helpers stay wired to real state.
func TestCapabilityWidgetsPaintTheirStates(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		id       string
		view     barView
		wantText string
		wantTone ui.Tone
	}{
		{
			id: "nightlight", view: barView{
				NightLight: services.NightLightState{Supported: true, Active: true},
			},
			wantText: "", wantTone: ui.ToneAccent,
		},
		{
			id: "nightlight", view: barView{
				NightLight: services.NightLightState{Supported: true},
			},
			wantText: "", wantTone: ui.ToneSubtle,
		},
		{
			id: "idle_inhibitor", view: barView{CaffeineWanted: true},
			wantText: "", wantTone: ui.ToneAccent,
		},
		{
			id: "idle_inhibitor", view: barView{},
			wantText: "", wantTone: ui.ToneSubtle,
		},
		{
			id: "idle_inhibitor", view: barView{
				Now: now, CaffeineWanted: true, CaffeineUntil: now.Add(15 * time.Minute),
			},
			wantText: "15m", wantTone: ui.ToneAccent,
		},
		{
			id: "brightness", view: barView{Brightness: services.BrightnessState{Level: 72}, BrightOK: true},
			wantText: "72%", wantTone: 0,
		},
		{
			id: "lock_keys", view: barView{LockKeys: services.LockState{Caps: true}, LockKeysOK: true},
			wantText: "Caps", wantTone: ui.ToneNormal,
		},
		{
			id: "lock_keys", view: barView{LockKeysOK: true},
			wantText: "", wantTone: ui.ToneSubtle,
		},
		{
			id: "lock_keys", view: barView{
				LockKeys: services.LockState{Caps: true, Num: true}, LockKeysOK: true,
			},
			wantText: "Caps Num", wantTone: ui.ToneNormal,
		},
		{
			id: "theme_mode", view: barView{ThemeMode: "light"},
			wantText: "Light", wantTone: 0,
		},
		{
			id: "theme_mode", view: barView{ThemeMode: "dark"},
			wantText: "Dark", wantTone: 0,
		},
		{
			id: "power_profile", view: barView{Profiles: []string{"performance", "balanced"}, ProfileActive: "performance", ProfilesOK: true},
			wantText: "performance", wantTone: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.id+"/"+tc.wantText, func(t *testing.T) {
			t.Parallel()
			got := buildWidgets([]config.Item{{ID: tc.id}}, 8, standardMetrics())
			if len(got) != 1 {
				t.Fatalf("built %d widgets, want 1", len(got))
			}
			w := got[0]
			w.refresh(tc.view)
			if w.node.Absent {
				t.Fatalf("%s widget went absent with the capability present", tc.id)
			}
			if tc.wantText == "" {
				return
			}
			text := ""
			walkNodes(w.node, func(n *ui.Node) {
				if n.Kind == ui.KindText && n.Text != "" {
					text = n.Text
				}
			})
			if text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
		})
	}
}

// TestPowerProfileCycleStepsTheList pins the bar's cycle order: next in the
// service's order with a wrap, and the first entry when the active name is
// unknown to the list.
func TestPowerProfileCycleStepsTheList(t *testing.T) {
	t.Parallel()
	profiles := []string{"performance", "balanced", "power-saver"}
	for _, tc := range []struct{ active, want string }{
		{active: "performance", want: "balanced"},
		{active: "balanced", want: "power-saver"},
		{active: "power-saver", want: "performance"},
		{active: "unknown-profile", want: "performance"},
	} {
		if got := nextPowerProfile(profiles, tc.active); got != tc.want {
			t.Errorf("nextPowerProfile from %q = %q, want %q", tc.active, got, tc.want)
		}
	}
	if got := nextPowerProfile(nil, "performance"); got != "" {
		t.Errorf("nextPowerProfile with no profiles = %q, want empty", got)
	}
}

// TestCapabilityWidgetVocabularyCount records the bar vocabulary size so the
// parity measurement stays honest: 27 kinds before the six capability
// widgets, 31 in the upstream reference, 33 here after them.
func TestCapabilityWidgetVocabularyCount(t *testing.T) {
	t.Parallel()
	const want = 33
	if got := len(config.KnownItemIDs()); got != want {
		t.Fatalf("bar vocabulary holds %d kinds, want %d (27 before the capability widgets, 31 upstream)", got, want)
	}
}
