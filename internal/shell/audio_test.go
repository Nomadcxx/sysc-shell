package shell

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestAudioPanelNameAndTriggerCentredPlacement(t *testing.T) {
	id, err := parsePanelName("audio")
	if err != nil || id != PanelAudio {
		t.Fatalf("parsePanelName(audio) = %v, %v", id, err)
	}
	if got := PanelAudio.String(); got != "audio" {
		t.Errorf("String() = %q, want audio", got)
	}
	// Fused: Gap 0 anchors at the bar zone; trigger-centred: the panel's
	// middle sits under the widget's middle, clamped to the output.
	p := Placement{
		BarEdge: "top", Output: ui.Rect{W: 3440, H: 1440},
		BarZone: 34, Gap: 0, Padding: 8,
		Panel: ui.Rect{W: 584, H: 496}, AnchorX: 400,
	}
	m := p.Margins()
	if m.Top != 34 {
		t.Errorf("Top = %d, want 34 (flush with the bar zone)", m.Top)
	}
	if want := 400 - 584/2; m.Left != want {
		t.Errorf("Left = %d, want %d (centred on the trigger)", m.Left, want)
	}
	// Clamp: a trigger near the right edge cannot push the panel off-screen.
	p.AnchorX = 3440 - 40
	if got := p.Margins().Left; got > 3440-584-8 {
		t.Errorf("Left = %d, want clamped inside the output", got)
	}
}

func TestAudioPanelResponsiveSize(t *testing.T) {
	for _, tc := range []struct {
		name       string
		outW, outH int
		want       ui.Rect
	}{
		{name: "minimum", outW: 1280, outH: 720, want: ui.Rect{W: 720, H: 640}},
		{name: "desktop", outW: 1920, outH: 1080, want: ui.Rect{W: 720, H: 720}},
		{name: "wide desktop", outW: 3440, outH: 1440, want: ui.Rect{W: 1120, H: 960}},
		{name: "maximum", outW: 5120, outH: 2160, want: ui.Rect{W: 1120, H: 992}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := audioPanelSize(tc.outW, tc.outH); got != tc.want {
				t.Fatalf("audioPanelSize(%d, %d) = %+v, want %+v", tc.outW, tc.outH, got, tc.want)
			}
		})
	}
}

func TestAudioPanelOpenUsesTheOutputResponsiveSize(t *testing.T) {
	reg := newPanelRegistry(t)
	trig := Trigger{BarEdge: "top", BarZone: 34, OutW: 3440, OutH: 1440}
	if err := reg.OpenPanel(PanelAudio, 7, trig); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelAudio]
	if h == nil {
		t.Fatal("audio panel has no host")
	}
	if got, want := h.place.Panel, audioPanelSize(trig.OutW, trig.OutH); got != want {
		t.Fatalf("opened panel size = %+v, want %+v", got, want)
	}
}

func TestAudioTreeUsesTheResponsiveWidth(t *testing.T) {
	size := audioPanelSize(3440, 1440)
	h := &PanelHost{
		id: PanelAudio, audioTab: "volumes", theme: DefaultTheme(),
		place: Placement{Panel: size},
	}
	root := audioTree(nil, h)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 18 }
	if err := ui.LayoutColumn(root, size, measure); err != nil {
		t.Fatal(err)
	}

	var close *ui.Node
	var sliders []*ui.Node
	walkAudio(root, func(n *ui.Node) {
		if n.Action == "audio-close" {
			close = n
		}
		if n.Kind == ui.KindSlider {
			sliders = append(sliders, n)
		}
	})
	if close == nil || close.Bounds.X+close.Bounds.W < size.W-h.theme.Metrics.PanelPadding-h.theme.Metrics.CardPadding {
		t.Fatalf("close bounds = %+v, want it pinned to the header's right edge", close)
	}
	if len(sliders) < 2 {
		t.Fatalf("sliders = %d, want output and input", len(sliders))
	}
	for _, slider := range sliders[:2] {
		if slider.Bounds.W < size.W/2 {
			t.Errorf("slider width = %d, want it to use the responsive card width", slider.Bounds.W)
		}
	}
	if len(root.Children) != 2 || root.Children[1].Kind != ui.KindScroll {
		t.Fatalf("root children = %+v, want fixed header plus the only scroll viewport", root.Children)
	}
	wantScrollH := size.H - 2*h.theme.Metrics.PanelPadding - root.Children[0].Height - root.Gap
	if got := root.Children[1].Bounds.H; got != wantScrollH {
		t.Fatalf("scroll height = %d, want remaining body height %d", got, wantScrollH)
	}
	body := root.Children[1].Children[0]
	if len(body.Children) < 2 || body.Children[0].Kind != ui.KindCapsule || body.Children[1].Kind != ui.KindCapsule {
		t.Fatal("output and input rows are not full-width cards")
	}
}

func TestAudioHeaderHeightFitsSpaciousChrome(t *testing.T) {
	m, ok := theme.MetricsFor(theme.DensityComfortable)
	if !ok {
		t.Fatal("comfortable metrics missing")
	}
	h := &PanelHost{theme: Theme{Metrics: m}}
	header := audioHeaderCard(h, m)
	want := 2*m.CardPadding + 2*m.StandardControl + 12
	if header.Height < want {
		t.Fatalf("header height = %d, want at least %d for two controls and chrome", header.Height, want)
	}
}

func TestAudioPanelSurfacesMixerPollFailure(t *testing.T) {
	if got := audioPanelError("", services.AudioSnapshot{Error: "services: pw-dump failed"}); got != "services: pw-dump failed" {
		t.Fatalf("audioPanelError = %q, want the mixer failure", got)
	}
}

func TestVolumeRowCarriesTheFullContract(t *testing.T) {
	n := services.AudioNode{ID: 42, Name: "dev", Description: "AD106M High Definition Audio Controller", Level: 51}
	row := audioVolumeRow(n, "Output", nil, standardMetrics())
	if len(row.Children) != 4 {
		t.Fatalf("row has %d regions, want 4 (per D4)", len(row.Children))
	}
	var slider *ui.Node
	walkAudio(row, func(x *ui.Node) {
		if x.Kind == ui.KindSlider {
			slider = x
		}
	})
	if slider == nil {
		t.Fatal("row carries no slider")
	}
	if slider.Value != 51 {
		t.Errorf("slider.Value = %v, want 51 (cubic from the snapshot)", slider.Value)
	}
	if row.Height != 0 && (row.Height < 68 || row.Height > 76) {
		t.Errorf("row height = %d, want the 68 px contract (density may scale)", row.Height)
	}
}

func TestEmptyApplicationsSectionIsNotAbsent(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelAudio, audioTab: "volumes"}
	got := renderAudioText(audioVolumesTree(r, h))
	if !strings.Contains(got, "Applications") {
		t.Error("Volumes renders no Applications label for an empty mixer: it looks broken (D12)")
	}
	if strings.Contains(got, "0%") {
		t.Error("Volumes painted 0% before a sample landed: stale must read as a dash")
	}
}

func TestScheduleControlDropsAStaleResult(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelAudio}
	done := make(chan struct{})
	r.mu.Lock()
	r.scheduleControl(h, func() error { close(done); return errors.New("boom") })
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.errLabel != "" {
		t.Errorf("errLabel = %q, want empty: the host was stale", h.errLabel)
	}
}

func TestAudioControlsUseTheScheduledWpctlPath(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	bin := filepath.Join(dir, "wpctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + logPath + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Registry{audio: services.NewAudio(time.Hour, bin)}
	h := &PanelHost{id: PanelAudio, audioTab: "volumes", theme: DefaultTheme()}
	controls := []*ui.Node{
		{Action: "audio-vol:42", Value: 63},
		{Action: "audio-mute:42"},
		{Action: "audio-dev:45"},
	}
	for _, control := range controls {
		r.mu.Lock()
		handled := h.applyAudioControl(r, control)
		r.mu.Unlock()
		if !handled {
			t.Fatalf("action %q was not handled", control.Action)
		}
	}

	deadline := time.Now().Add(time.Second)
	var got string
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(logPath)
		if err == nil {
			got = string(raw)
			if strings.Count(got, "\n") == len(controls) {
				break
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	for _, want := range []string{
		"set-volume 42 63%",
		"set-mute 42 1",
		"set-default 45",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("wpctl log %q does not contain %q", got, want)
		}
	}
}

func TestClosingAudioPanelDoesNotWaitForMixerPoll(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	script := "#!/bin/sh\n" +
		"touch \"$SYSC_MIXER_STARTED\"\n" +
		"while [ ! -e \"$SYSC_MIXER_RELEASE\" ]; do sleep 0.01; done\n" +
		"printf '[]'\n"
	if err := os.WriteFile(filepath.Join(dir, "pw-dump"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("SYSC_MIXER_STARTED", started)
	t.Setenv("SYSC_MIXER_RELEASE", release)
	defer os.WriteFile(release, nil, 0o600) // unblock cleanup after a failing assertion

	reg := newPanelRegistry(t)
	reg.audio = services.NewAudio(time.Hour, "/bin/true")
	if err := reg.OpenPanel(PanelAudio, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("mixer poll did not start")
		}
		time.Sleep(time.Millisecond)
	}

	done := make(chan struct{})
	go func() {
		reg.ClosePanel(PanelAudio)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		_ = os.WriteFile(release, nil, 0o600)
		<-done
		t.Fatal("closing the audio panel waited for the active pw-dump poll")
	}
}

func TestDevicesRowsAreSelectedWellsNotRadios(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelAudio, audioTab: "devices"}
	tree := audioDevicesTree(r, h)
	got := renderAudioText(tree)
	if !strings.Contains(got, "Output device") || !strings.Contains(got, "Input device") {
		t.Error("Devices must carry both section labels even when empty")
	}
	current := audioDeviceRow(services.AudioNode{
		ID: 7, Description: "AD106M High Definition Audio Controller Digital Stereo (HDMI)", Default: true,
	}, standardMetrics())
	if current.Kind != ui.KindCapsule || current.Fill != ui.FillSoft {
		t.Fatalf("current device = kind %v fill %v, want KindCapsule FillSoft", current.Kind, current.Fill)
	}
	var check *ui.Node
	walkAudio(current, func(x *ui.Node) {
		if x.Kind == ui.KindIcon && x.Icon == "check" {
			check = x
		}
	})
	if check == nil {
		t.Fatal("current device well carries no trailing check")
	}
	plain := audioDeviceRow(services.AudioNode{ID: 8, Description: "other"}, standardMetrics())
	if plain.Fill != ui.FillNone {
		t.Errorf("sibling fill = %v, want a plain row", plain.Fill)
	}
}

func TestAudioDensityContract(t *testing.T) {
	long := "AD106M High Definition Audio Controller Digital Stereo (HDMI)"
	row := audioVolumeRow(services.AudioNode{ID: 1, Description: long, Level: 100}, "Output", nil, standardMetrics())
	if row.Height != 68 {
		t.Errorf("volume row height = %d, want 68 so compact still holds role+name+slider", row.Height)
	}
	var value *ui.Node
	walkAudio(row, func(x *ui.Node) {
		if x.MinWidthText == "100%" {
			value = x
		}
	})
	if value == nil {
		t.Fatal("value column does not reserve 100% width")
	}
	compact, ok := theme.MetricsFor(theme.DensityCompact)
	if !ok {
		t.Fatal("compact metrics missing")
	}
	// D2: 560 minus panel pad, row pad, check, gap. Compact pad is 12, so
	// the name box is wider than the 448 px the mock sized at standard.
	remain := 560 - 2*compact.PanelPadding - 2*12 - 20 - 12
	// Body is 14 px; a 7 px/rune stand-in matches D2's 448 px / 61 chars.
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 7, 16 }
	w, _ := measure(long, ui.TextAttrs{})
	if w > remain {
		t.Errorf("compact name box %d px, 61-char name measures %d", remain, w)
	}
	// 125% body is 18 px (~9 px/rune). Record a miss; live type decides
	// whether Devices elides, which is the only trigger to widen 560.
	remainStd := 560 - 2*16 - 2*12 - 20 - 12
	if got := len([]rune(long)) * 9; got > remainStd {
		t.Logf("fontScale 125%% stand-in: name %d px in a %d px box", got, remainStd)
	}
}

func walkAudio(n *ui.Node, fn func(*ui.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walkAudio(c, fn)
	}
}

func renderAudioText(n *ui.Node) string {
	var b strings.Builder
	walkAudio(n, func(x *ui.Node) {
		if x.Text != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(x.Text)
		}
	})
	return b.String()
}

func audioLogLines(t *testing.T, dir string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "log"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	return strings.Count(string(raw), "\n")
}

func TestViewLockedServesCachedAudio(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vol"), []byte("0.40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
echo "$1 $*" >> '` + dir + `/log'
if [ "$1" = get-volume ]; then printf 'Volume: %s\n' "$(cat '` + dir + `/vol')"; fi
`
	bin := filepath.Join(dir, "wpctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := newPanelRegistry(t)
	audio := services.NewAudio(time.Hour, bin)
	reg.setAudio(audio)
	reg.bars[1] = &Bar{conn: "DP-1"}
	deadline := time.Now().Add(3 * time.Second)
	st, _ := audio.CachedState()
	for st.Level != 40 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		st, _ = audio.CachedState()
	}
	if st.Level != 40 {
		t.Fatalf("baseline poll level = %d, want 40", st.Level)
	}
	before := audioLogLines(t, dir)
	reg.mu.Lock()
	view := reg.viewLocked("DP-1")
	reg.mu.Unlock()
	if got := audioLogLines(t, dir); got != before {
		t.Fatalf("viewLocked exec'd wpctl: log lines %d -> %d", before, got)
	}
	if view.Audio.Level != 40 {
		t.Fatalf("view.Audio.Level = %d, want 40", view.Audio.Level)
	}
}

func TestVolumeOwnerHandlersDoNotBlockOnExec(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	script := `#!/bin/sh
if [ "$1" = get-volume ]; then
  while [ ! -f '` + release + `' ]; do sleep 0.01; done
  printf 'Volume: 0.40\n'
fi
`
	bin := filepath.Join(dir, "wpctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := newPanelRegistry(t)
	reg.setAudio(services.NewAudio(time.Hour, bin))
	bar := &Bar{conn: "DP-1"}
	reg.bars[1] = bar
	reg.mu.Lock()
	reg.bindBarPanelActionsLocked(1, bar)
	reg.mu.Unlock()

	start := time.Now()
	if !bar.onAction(panelAudioAction, buttonRight) {
		t.Fatal("right-click on the volume widget was not handled")
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("right-click mute blocked the owner for %v", d)
	}
	start = time.Now()
	if !bar.onAxis(panelAudioAction, 1) {
		t.Fatal("wheel step on the volume widget was not handled")
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("wheel step blocked the owner for %v", d)
	}
	if err := os.WriteFile(release, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
}
