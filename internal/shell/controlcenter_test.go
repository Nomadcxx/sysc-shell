package shell

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func renderText(n *ui.Node) string {
	if n == nil {
		return ""
	}
	parts := []string{n.Text}
	for _, child := range n.Children {
		parts = append(parts, renderText(child))
	}
	return strings.Join(parts, " ")
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func TestControlCentreNameAndFlushPlacement(t *testing.T) {
	id, err := parsePanelName("control-center")
	if err != nil {
		t.Fatalf("parsePanelName(control-center): %v", err)
	}
	if got := id.String(); got != "control-center" {
		t.Fatalf("String() = %q, want control-center", got)
	}

	cfg := config.Default()
	cfg.Panels.Gap = 19 // The fused panel ignores a configured floating gap.
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	if err := r.OpenPanelByName("control-center"); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	h := r.panelHosts[id]
	if h == nil {
		r.mu.Unlock()
		t.Fatal("control centre did not create a panel host")
	}
	place := h.place
	section := h.section
	rootKind := h.root.Kind
	leaseCount := len(h.leases)
	fillet := h.filletMargin()
	spec := r.panelSpec(h, place.Margins())
	r.mu.Unlock()

	if place.Panel != (ui.Rect{W: 700, H: 564}) {
		t.Errorf("panel size = %+v, want 700x564", place.Panel)
	}
	if place.Gap != 0 {
		t.Errorf("gap = %d, want 0", place.Gap)
	}
	if got := place.Margins(); got.Top != place.BarZone || got.Left != (place.Output.W-place.Panel.W)/2 {
		t.Errorf("margins = %+v, want flush and centred", got)
	}
	if section != "home" {
		t.Errorf("initial section = %q, want home", section)
	}
	if rootKind != ui.KindRow {
		t.Errorf("root kind = %v, want row", rootKind)
	}
	if leaseCount != 4 {
		t.Errorf("leases = %d, want CPU, memory, battery and clock", leaseCount)
	}
	if fillet != 12 || spec.Width != 724 {
		t.Errorf("fillet = %d, drawn width = %d, want 12 and 724", fillet, spec.Width)
	}
}

func TestControlCentrePanelOpaqueHintMatchesExpandedSilhouette(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)

	for _, tc := range []struct {
		name       string
		fillet     int
		wantOpaque bool
	}{
		{name: "fillet-expanded", fillet: 12, wantOpaque: false},
		{name: "plain opaque panel", fillet: 0, wantOpaque: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theme := DefaultTheme()
			theme.Fillet = tc.fillet
			h := &PanelHost{
				id:    PanelControlCenter,
				theme: theme,
				place: Placement{
					BarEdge: "top",
					Output:  ui.Rect{W: 1000, H: 800},
					Padding: 16,
					Panel:   ui.Rect{W: 700, H: 564},
				},
			}
			spec := r.panelSpec(h, h.place.Margins())
			if got := spec.Callbacks.OpaqueBackground; got != tc.wantOpaque {
				t.Fatalf("OpaqueBackground = %t, want %t (fillet margin %d)", got, tc.wantOpaque, h.filletMargin())
			}
		})
	}
}

func TestControlCentreRevealFollowsSurfaceAnimator(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edge    string
		reduced bool
		wantY   int
	}{
		{name: "top", edge: "top", wantY: -panelSlidePx},
		{name: "bottom", edge: "bottom", wantY: panelSlidePx},
		{name: "reduced", edge: "top", reduced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(0, 0)
			h := &PanelHost{id: PanelControlCenter, place: Placement{BarEdge: tc.edge}, theme: DefaultTheme()}
			h.anim = newAnimator(func() time.Time { return now }, tc.reduced, h.theme.Motion)
			h.anim.Target(panelSurfaceID(h.id), animVisible, 1)

			opacity, offsetY, fillet := h.panelReveal()
			if opacity != 0 || offsetY != tc.wantY || fillet != 0 {
				t.Fatalf("initial reveal = opacity %v offset %d fillet %d, want 0, %d, 0", opacity, offsetY, fillet, tc.wantY)
			}
			settle := h.theme.Motion.Durations.Medium
			if tc.reduced {
				settle = reducedPanelCap
			}
			now = now.Add(settle)
			opacity, offsetY, fillet = h.panelReveal()
			if opacity != 1 || offsetY != 0 || fillet != h.theme.Fillet {
				t.Fatalf("settled reveal = opacity %v offset %d fillet %d, want 1, 0, %d", opacity, offsetY, fillet, h.theme.Fillet)
			}
		})
	}
}

func TestWordmarkRightClickOpensControlCentre(t *testing.T) {
	widgets := buildWidgets([]config.Item{{ID: "wordmark"}}, 6)
	if len(widgets) != 1 {
		t.Fatalf("buildWidgets = %d widgets, want 1", len(widgets))
	}
	mark := widgets[0].node
	if mark.Kind != ui.KindWordmark || mark.Action != panelControlCenterAction ||
		mark.Name != "Control centre" || mark.Role != "button" {
		t.Fatalf("wordmark = %+v, want the accessible control-centre action", mark)
	}
	if got := buildWidgets([]config.Item{{ID: "control-center"}}, 6); len(got) != 0 {
		t.Fatalf("standalone control-center built %d widgets, want none", len(got))
	}
	for _, id := range config.KnownItemIDs() {
		if id == "control-center" {
			t.Fatal("standalone control-center remains in the configuration vocabulary")
		}
	}
	for _, item := range config.Default().Bar.Right {
		if item.ID == "control-center" {
			t.Fatal("default right section still carries the redundant trigger")
		}
	}

	r := newPanelRegistry(t)
	cb, err := r.NewHost(7, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	bar := r.bars[7]
	if err := bar.Layout(1536, 44); err != nil {
		t.Fatal(err)
	}
	target := bar.actionBounds(panelControlCenterAction)
	if target.W == 0 {
		t.Fatal("laid-out wordmark has no control-centre action bounds")
	}
	drainAuxQueue(r)
	if clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonLeft) {
		t.Fatal("left-click on the wordmark must stay inert")
	}
	if !clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonRight) {
		t.Fatal("right-click on the wordmark did not activate")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("right-click did not open the control centre")
	}
	if want := target.X + target.W/2; h.place.AnchorX != want {
		t.Errorf("anchor = %d, want wordmark centre %d", h.place.AnchorX, want)
	}
}

func TestControlCentreRailKeepsDisabledDestinationsAddressable(t *testing.T) {
	root := controlCentreTree(nil, &PanelHost{section: "home"})
	if len(root.Children) != 2 {
		t.Fatalf("control centre has %d regions, want rail and body", len(root.Children))
	}
	rail := root.Children[0]
	var entries []*ui.Node
	for _, n := range rail.Children {
		if n.Kind == ui.KindButton {
			entries = append(entries, n)
		}
	}
	if len(entries) != 10 {
		t.Fatalf("rail has %d entries, want 10", len(entries))
	}
	if entries[0].Name != "Home" || !entries[0].State.Has(ui.StateSelected) {
		t.Errorf("first entry = %+v, want selected Home", entries[0])
	}
	media := entries[1]
	if media.Name != "Media — not available yet" || media.Action != "" ||
		!media.Focusable || !media.State.Has(ui.StateDisabled) {
		t.Errorf("disabled Media entry = %+v", media)
	}
	if got := len(ui.Focusables(rail)); got != 10 {
		t.Errorf("focusable rail entries = %d, want all 10 including unavailable destinations", got)
	}
}

func TestControlCentreFocusListsRailBeforeBodyControls(t *testing.T) {
	focus := ui.Focusables(controlCentreTree(nil, &PanelHost{section: "home"}))
	if len(focus) <= len(ccSections) {
		t.Fatalf("focus ring has %d nodes, want the rail and body controls", len(focus))
	}
	for i, section := range ccSections {
		want := section.Label
		if !section.Enabled {
			want += " — not available yet"
		}
		if got := focus[i]; got.Role != "tab" || got.Name != want {
			t.Fatalf("focus %d = %+v, want rail section %q", i, got, section.Label)
		}
	}
	if got := focus[len(ccSections)]; got.Role == "tab" {
		t.Fatalf("first body focus = %+v, want the rail prefix to end", got)
	}
}

func TestControlCentreRetargetKeepsOneSelectedPageAndRailFocus(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, r, 2)

	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.panelHosts[PanelControlCenter]
	now := time.Unix(0, 0)
	h.anim = newAnimator(func() time.Time { return now }, false, h.theme.Motion)

	activateSection := func(action string) {
		t.Helper()
		for i, n := range h.focus {
			if n.Action == action {
				h.roving.Set(i)
				if !h.activate(r) {
					t.Fatalf("%s did not activate", action)
				}
				return
			}
		}
		t.Fatalf("no focusable %s", action)
	}
	activateSection("section:audio")
	if got := h.focused().Action; got != "section:audio" {
		t.Fatalf("focus moved to %q after page swap", got)
	}
	const pageKey = "control-centre-page"
	if page := findNode(h.root, func(n *ui.Node) bool { return n.Key == pageKey }); page == nil {
		t.Fatal("selected page has no stable animation wrapper")
	}
	if !h.anim.has(pageKey, animVisible) {
		t.Fatal("page swap did not target the surface animator")
	}
	// Half of the transition actually under test, not half of Medium. This
	// fixture turns reduced motion on, so the page fade resolves to the capped
	// Short rather than to Medium; once Medium reached 300 ms, Medium/2 landed
	// exactly on that fade's final instant, the value read a settled 1, and the
	// next swap legitimately restarted it from 0. Asking the animator keeps the
	// probe mid-flight whatever the tokens underneath are re-based to.
	now = now.Add(h.anim.duration(animVisible, true) / 2)
	before := h.anim.Value(pageKey, animVisible)
	activateSection("section:monitor")
	if got := h.anim.Value(pageKey, animVisible); got != before {
		t.Fatalf("mid-transition retarget jumped from %v to %v", before, got)
	}
	pages := 0
	var selected *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Key == pageKey {
			pages++
			selected = n
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(h.root)
	if pages != 1 || selected == nil || !strings.Contains(renderText(selected), "Network") {
		t.Fatalf("selected page count/text = %d/%q, want one Monitor page", pages, renderText(selected))
	}
}

func TestControlCentreEscapeClosesRoot(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, r, 2)[1].Open
	if !panel.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc}) {
		t.Fatal("Escape did not report the root close")
	}
	r.mu.Lock()
	_, open := r.panelHosts[PanelControlCenter]
	r.mu.Unlock()
	if open {
		t.Fatal("Escape left the control centre open")
	}
}

func TestControlCentreHeaderAndBodyComposition(t *testing.T) {
	root := controlCentreTree(nil, &PanelHost{section: "audio"})
	if len(root.Children) != 2 || len(root.Children[1].Children) != 2 {
		t.Fatalf("control centre composition = %+v, want rail beside header and body", root)
	}
	header := root.Children[1].Children[0]
	if header.Height != 40 || len(header.Children) != 2 || header.Children[0].Text != "Audio" {
		t.Fatalf("header = %+v, want a 40px Audio header", header)
	}
	want := map[string]string{
		"Settings": "cc:settings",
		"Power":    "cc:power",
		"Close":    "cc:close",
	}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if action, ok := want[n.Name]; ok {
			if n.Action != action || !n.Focusable {
				t.Errorf("%s control = %+v", n.Name, n)
			}
			delete(want, n.Name)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(header)
	if len(want) != 0 {
		t.Errorf("header is missing controls: %v", want)
	}
	if body := root.Children[1].Children[1]; body.Kind != ui.KindScroll || len(body.Children) != 1 {
		t.Errorf("body = %+v, want one scroll viewport", body)
	}
}

func TestControlCentreHomeFillsTheBodyContract(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	root := controlCentreTree(&Registry{}, h)
	body := root.Children[1].Children[1]
	if body.Height != 480 {
		t.Fatalf("body height = %d, want 480", body.Height)
	}
	home := body.Children[0]
	if home.Gap != 12 || len(home.Children) != 4 {
		t.Fatalf("Home composition = %+v, want four blocks separated by 12px", home)
	}
	want := []int{96, 48, 184, 116}
	for i, child := range home.Children {
		if child.Height != want[i] {
			t.Errorf("Home block %d height = %d, want %d", i, child.Height, want[i])
		}
	}
}

func TestHomeShowsDashesBeforeTheFirstSample(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	got := renderText(ccHome(&Registry{}, h))
	if strings.Contains(got, "0%") {
		t.Error("Home rendered 0% before a sample landed: stale must read as a dash")
	}
	if !strings.Contains(got, "—") {
		t.Error("Home rendered no dash for an unsampled value")
	}
}

func TestControlCentreHomeQuickAccessControlsAreSeparated(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	quick := ccHome(&Registry{}, h).Children[1]
	if quick.Kind != ui.KindRow || quick.Gap != 8 || len(quick.Children) != 2 {
		t.Fatalf("quick access = %+v, want two controls in an 8px-gap row", quick)
	}
	for _, name := range []string{"Caffeine", "Wallpaper"} {
		n := findByName(quick, name)
		if n == nil || n.Kind != ui.KindButton || n.Shape != ui.ShapeStadium || !n.Focusable {
			t.Errorf("%s = %+v, want independent capsule button", name, n)
		}
	}
	if segmented := findNode(quick, func(n *ui.Node) bool { return n.Kind == ui.KindSegmented }); segmented != nil {
		t.Fatalf("quick access still uses joined segmented chrome: %+v", segmented)
	}
}

func TestControlCentreHomeRadialResourcesPreserveSampleState(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	r := &Registry{sample: fixtureSnapshot()}
	home := ccHome(r, h)
	gauges := findAllKind(home, ui.KindRadialGauge)
	if len(gauges) != 2 {
		t.Fatalf("radial gauges = %d, want CPU and memory", len(gauges))
	}
	want := map[string]float64{"sysmon-cpu": .42, "sysmon-memory": .25}
	for _, gauge := range gauges {
		if gauge.Width != 40 || gauge.Height != 40 || gauge.Absent || gauge.Value != want[gauge.Icon] {
			t.Errorf("sampled gauge = %+v", gauge)
		}
	}
	for _, value := range []string{"42%", "25%"} {
		if !strings.Contains(renderText(home), value) {
			t.Errorf("resource values %q omit %s", renderText(home), value)
		}
	}

	absent := findAllKind(ccHome(&Registry{}, h), ui.KindRadialGauge)
	if len(absent) != 2 || !absent[0].Absent || !absent[1].Absent {
		t.Fatalf("unsampled gauges = %+v, want two absent gauges", absent)
	}

	zero := fixtureSnapshot()
	zero.CPU.Usage.Fraction = 0
	zero.Memory.Memory.UsedBytes = 0
	zeroHome := ccHome(&Registry{sample: zero}, h)
	for _, gauge := range findAllKind(zeroHome, ui.KindRadialGauge) {
		if gauge.Absent || gauge.Value != 0 {
			t.Errorf("valid zero gauge = %+v, want present zero", gauge)
		}
	}
	if got := strings.Count(renderText(zeroHome), "0%"); got != 2 {
		t.Fatalf("valid zero values = %q, want two 0%% labels", renderText(zeroHome))
	}

	for id, icon := range map[string]string{"cpu": "sysmon-cpu", "memory": "sysmon-memory"} {
		got, ok := render.GaugeIconName(id)
		if !ok || got != icon {
			t.Fatalf("%s gauge icon = %q/%v, want %q", id, got, ok, icon)
		}
	}
}

func TestHomeUsesTheRegistryIdentitySnapshot(t *testing.T) {
	r := &Registry{controlIdentity: ccIdentity{
		Name: "Nomad", Account: "nomadx@pony", Uptime: "2 hours",
	}}
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	got := renderText(ccHome(r, h))
	for _, want := range []string{"Nomad", "nomadx@pony", "2 hours"} {
		if !strings.Contains(got, want) {
			t.Errorf("Home text %q is missing cached identity %q", got, want)
		}
	}
}

func TestHomeIdentityIncludesCircularProfileFallback(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	home := ccHome(&Registry{}, h)
	avatar := findNode(home.Children[0], func(n *ui.Node) bool {
		return n.Width == ccAvatarSize && n.Height == ccAvatarSize && n.Shape == ui.ShapeCircle
	})
	if avatar == nil || avatar.Kind != ui.KindCapsule {
		t.Fatalf("identity avatar = %+v, want circular fallback", avatar)
	}
	icon := findNode(avatar, func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
	if icon == nil || icon.Icon != "person" {
		t.Fatalf("fallback icon = %+v, want person", icon)
	}
}

func TestHomeIdentityUsesDecodedProfileImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "face.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			source.Set(x, y, color.RGBA{R: 0xff, A: 0xff})
		}
	}
	if err := png.Encode(file, source); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	worker := icons.NewWorker(icons.NewResolver("", nil), nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = worker.Run(ctx) }()
	key := icons.Square(path, ccAvatarSize)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := worker.Lookup(key); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("profile image did not decode")
		}
		time.Sleep(time.Millisecond)
	}

	r := &Registry{controlIdentity: ccIdentity{ImagePath: path}, trayIcons: worker}
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme(), scale120: 120}
	avatar := findNode(ccHome(r, h).Children[0], func(n *ui.Node) bool {
		return n.Width == ccAvatarSize && n.Height == ccAvatarSize && n.Shape == ui.ShapeCircle
	})
	if avatar == nil || avatar.Kind != ui.KindImage || avatar.Image == nil {
		t.Fatalf("identity avatar = %+v, want decoded circular image", avatar)
	}
}

func TestFailedProfileImageIsRemembered(t *testing.T) {
	key := icons.Square("/tmp/unsupported.face", ccAvatarSize)
	r := &Registry{controlIdentity: ccIdentity{ImagePath: key.Name}}
	r.applyTrayIcon(key, nil)
	if _, ok := r.controlAvatarFailed[key]; !ok {
		t.Fatal("failed profile decode was not remembered")
	}
}

func TestHomeUnavailableControlsAreDisabledAndBatteryIsAReadout(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	home := ccHome(&Registry{}, h)
	for _, name := range []string{"Mute", "Power profile", "Volume", "Brightness"} {
		n := findByName(home, name)
		if n == nil || !n.State.Has(ui.StateDisabled) {
			t.Errorf("unavailable %s = %+v, want disabled", name, n)
		}
	}
	battery := findNode(home, func(n *ui.Node) bool {
		return n.Kind == ui.KindCapsule && strings.Contains(renderText(n), "Battery")
	})
	if battery == nil || battery.Focusable || battery.Action != "" || battery.Stroke == 0 {
		t.Errorf("battery = %+v, want an outlined non-actionable readout", battery)
	}
}

func TestHomeDNDControlUpdatesMemorySynchronously(t *testing.T) {
	r := &Registry{notify: newNotifyState()}
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	n := findByName(ccHome(r, h), "Do not disturb")
	if !h.activateControlCentre(r, n) {
		t.Fatal("DND control was not handled")
	}
	if _, on := r.notify.dndState(time.Time{}); !on {
		t.Fatal("DND control did not update the in-memory state")
	}
}

func TestHomeWallpaperControlOpensTheExistingPanel(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	n := findByName(h.root, "Wallpaper")
	if !h.activateControlCentre(r, n) {
		r.mu.Unlock()
		t.Fatal("Wallpaper control was not handled")
	}
	_, controlOpen := r.panelHosts[PanelControlCenter]
	_, wallpaperOpen := r.panelHosts[PanelWallpaper]
	r.mu.Unlock()
	if controlOpen || !wallpaperOpen {
		t.Fatalf("after Wallpaper: control open=%v wallpaper open=%v", controlOpen, wallpaperOpen)
	}
}

func TestWeatherPageKeepsTodayAndFourForecastSlots(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelControlCenter, section: "weather", theme: DefaultTheme()}
	page := ccWeather(r, h)
	if page.Height != 480 || page.Gap != 12 || len(page.Children) != 2 {
		t.Fatalf("weather page = %+v, want 336px Today and 132px forecast strip", page)
	}
	if page.Children[0].Height != 336 || page.Children[1].Height != 132 || len(page.Children[1].Children) != 4 {
		t.Fatalf("weather blocks = %+v, want Today plus four stable forecast cells", page.Children)
	}
	if got := renderText(page.Children[1]); strings.Count(got, ccDash) < 4 {
		t.Errorf("empty forecast = %q, want a dash in every stable slot", got)
	}
}

func TestWeatherUpdateRebuildsAnOpenControlCentre(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	h.section = "weather"
	r.rebuildPanel(h)
	r.mu.Unlock()
	r.UpdateWeather(services.Reading{Observed: true, Temperature: 23, Unit: services.UnitCelsius})
	r.mu.Lock()
	got := renderText(h.root)
	r.mu.Unlock()
	if !strings.Contains(got, "23°C") {
		t.Fatalf("weather update left the control centre stale: %q", got)
	}
}

func TestHomeRebuildsForClockAndMetricUpdates(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	before := h.root
	r.mu.Unlock()
	now := time.Date(2026, 9, 10, 13, 45, 0, 0, time.UTC)
	r.UpdateClock(now)
	r.mu.Lock()
	afterClock := h.root
	clockText := renderText(h.root)
	r.mu.Unlock()
	if afterClock == before || !strings.Contains(clockText, "13:45") {
		t.Fatalf("clock update left Home stale: %q", clockText)
	}
	r.UpdateMetrics(services.Snapshot{CollectedAt: now})
	r.mu.Lock()
	afterMetrics := h.root
	r.mu.Unlock()
	if afterMetrics == afterClock {
		t.Fatal("metric update did not rebuild Home")
	}
}

func TestAudioPageHasNoDevicePicker(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "audio", theme: DefaultTheme()}
	got := renderText(ccAudio(&Registry{}, h))
	for _, banned := range []string{"Output device", "Sink", "Device"} {
		if strings.Contains(got, banned) {
			t.Errorf("audio page drew %q: the service reports level and mute only", banned)
		}
	}
	for _, want := range []string{"Volume", "Mute"} {
		if !strings.Contains(got, want) {
			t.Errorf("audio page %q is missing %q", got, want)
		}
	}
}

func TestControlCentreNativePagesFillTheBody(t *testing.T) {
	r := &Registry{notify: newNotifyState(), now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	for _, tc := range []struct {
		section string
		want    []string
	}{
		{section: "audio", want: []string{"Volume", "Mute"}},
		{section: "monitor", want: []string{"CPU", "Memory", "Network"}},
		{section: "power", want: []string{"Battery", "Power profile", "Session"}},
		{section: "calendar", want: []string{"September 2026", "Previous month", "Next month"}},
		{section: "notifications", want: []string{"Do not disturb", "Nothing to see here"}},
	} {
		t.Run(tc.section, func(t *testing.T) {
			h := &PanelHost{id: PanelControlCenter, section: tc.section, theme: DefaultTheme()}
			page := ccPage(r, h)
			if page.Height != 480 {
				t.Errorf("%s page height = %d, want the full 480px body", tc.section, page.Height)
			}
			got := renderText(page)
			for _, want := range tc.want {
				if !strings.Contains(got, want) && findByName(page, want) == nil {
					t.Errorf("%s page %q is missing %q", tc.section, got, want)
				}
			}
		})
	}
}

func TestCalendarPageUsesTheHostMonthDelta(t *testing.T) {
	r := &Registry{now: time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)}
	h := &PanelHost{id: PanelControlCenter, section: "calendar", monthDelta: 1, theme: DefaultTheme()}
	if got := renderText(ccCalendar(r, h)); !strings.Contains(got, "October 2026") {
		t.Fatalf("calendar monthDelta was ignored: %q", got)
	}
}

func TestControlCentreLoadsPowerProfilesForItsOwnHost(t *testing.T) {
	r := newPanelRegistry(t)
	r.lookPath = func(string) (string, error) { return "/usr/bin/powerprofilesctl", nil }
	r.runArgvOutput = func([]string) (string, error) { return starredPowerProfilesList, nil }
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		h := r.panelHosts[PanelControlCenter]
		loaded := h != nil && h.profilesOK && h.profileActive == "balanced"
		r.mu.Unlock()
		if loaded {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("control centre never adopted the power profile list")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestControlCentreSessionActionRunsWithoutRegistryLock(t *testing.T) {
	r := &Registry{panelHosts: map[PanelID]*PanelHost{}, closed: make(chan struct{})}
	h := &PanelHost{id: PanelControlCenter, section: "power", theme: DefaultTheme()}
	r.panelHosts[PanelControlCenter] = h
	unlocked := make(chan bool, 1)
	r.runArgv = func([]string) error {
		ok := r.mu.TryLock()
		if ok {
			r.mu.Unlock()
		}
		unlocked <- ok
		return nil
	}
	r.mu.Lock()
	if !h.activateControlCentre(r, &ui.Node{Action: "session-suspend"}) {
		r.mu.Unlock()
		t.Fatal("control centre did not handle the session action")
	}
	r.mu.Unlock()
	select {
	case ok := <-unlocked:
		if !ok {
			t.Fatal("session command ran while Registry.mu was held")
		}
	case <-time.After(time.Second):
		t.Fatal("session command did not run")
	}
}

func TestNotificationUpdateRebuildsAnOpenControlCentre(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	h.section = "notifications"
	r.rebuildPanel(h)
	r.mu.Unlock()
	r.applyNotify(snap(1, note(1, "Mailbox")))
	r.mu.Lock()
	got := renderText(h.root)
	r.mu.Unlock()
	if !strings.Contains(got, "Mailbox") {
		t.Fatalf("notification update left the control centre stale: %q", got)
	}
}

func TestControlCentrePagesLayOutAtTheContractSize(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, r, 2)
	for _, section := range []string{"home", "audio", "monitor", "power", "weather", "calendar", "notifications"} {
		t.Run(section, func(t *testing.T) {
			r.mu.Lock()
			defer r.mu.Unlock()
			h := r.panelHosts[PanelControlCenter]
			h.section = section
			r.rebuildPanel(h)
			size := panelTargetSize(PanelControlCenter)
			if err := h.configure(size.W, size.H, int(ui.ScaleUnit)); err != nil {
				t.Fatalf("%s does not lay out at %dx%d: %v", section, size.W, size.H, err)
			}
			assertLaidOut(t, PanelControlCenter, h.root)
		})
	}
}

func TestControlCentreShowsCommandFailuresInline(t *testing.T) {
	r := &Registry{notify: newNotifyState()}
	for _, section := range []string{"home", "power"} {
		h := &PanelHost{id: PanelControlCenter, section: section, theme: DefaultTheme(), errLabel: "command failed"}
		if got := renderText(ccPage(r, h)); !strings.Contains(got, "command failed") {
			t.Errorf("%s page hid the command failure: %q", section, got)
		}
	}
}

func TestControlCentreHeaderRoutesPanelsAndClose(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		want   PanelID
		closed bool
	}{
		{name: "settings", action: "cc:settings", want: PanelSettings},
		{name: "power", action: "cc:power", want: PanelSession},
		{name: "close", action: "cc:close", closed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPanelRegistry(t)
			if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			h := r.panelHosts[PanelControlCenter]
			for i, n := range h.focus {
				if n.Action == tc.action {
					h.roving.Set(i)
					break
				}
			}
			if !h.activate(r) {
				r.mu.Unlock()
				t.Fatalf("%s did not activate", tc.action)
			}
			_, controlOpen := r.panelHosts[PanelControlCenter]
			_, targetOpen := r.panelHosts[tc.want]
			r.mu.Unlock()
			if controlOpen || (!tc.closed && !targetOpen) {
				t.Errorf("after %s: control open=%v target open=%v", tc.action, controlOpen, targetOpen)
			}
		})
	}
}

func TestCaffeineTogglesThroughTheRegistryHook(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	started := make(chan struct{})
	released := make(chan struct{})
	r.startInhibit = func() (io.Closer, error) {
		close(started)
		return closerFunc(func() error { close(released); return nil }), nil
	}
	r.setCaffeine(h, true)
	r.mu.Unlock()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("caffeine never started the idle inhibit")
	}
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		on := r.inhibit != nil
		r.mu.Unlock()
		if on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("started inhibit was not retained")
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	r.setCaffeine(h, false)
	r.mu.Unlock()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("turning caffeine off did not release the inhibit")
	}
}

func TestCaffeineHoldIsReleasedOnClose(t *testing.T) {
	r := NewRegistry(config.Default())
	released := false
	r.mu.Lock()
	r.inhibit = closerFunc(func() error { released = true; return nil })
	r.mu.Unlock()
	r.Close()
	if !released {
		t.Fatal("the idle inhibit outlived the shell")
	}
}

func TestPanelSectionValidationPrecedesMutation(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	call := r.HandlePanelByName

	if err := r.OpenPanelByName("control-center"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	original := r.panelHosts[PanelControlCenter]
	r.mu.Unlock()
	for _, section := range []string{"network", "nope"} {
		if err := call("open", "control-center", section); err == nil {
			t.Errorf("section %q was accepted", section)
		}
		r.mu.Lock()
		got := r.panelHosts[PanelControlCenter]
		selected := got.section
		r.mu.Unlock()
		if got != original || selected != "home" {
			t.Fatalf("rejected section %q changed host or selection", section)
		}
	}
	if err := call("open", "control-center", "audio"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	selected := r.panelHosts[PanelControlCenter].section
	r.mu.Unlock()
	if selected != "audio" {
		t.Errorf("selected section = %q, want audio", selected)
	}
	if err := call("open", "control-center", ""); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	selected = r.panelHosts[PanelControlCenter].section
	r.mu.Unlock()
	if selected != "home" {
		t.Errorf("omitted section selected %q, want home", selected)
	}

	if err := call("open", "settings", "Appearance"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	settingsSection := r.panelHosts[PanelSettings].section
	r.mu.Unlock()
	if settingsSection != "Appearance" {
		t.Errorf("settings section = %q, want Appearance", settingsSection)
	}
}

func TestCCProfileImagePathUsesAccountPrecedence(t *testing.T) {
	home := t.TempDir()
	accounts := t.TempDir()
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	account := filepath.Join(accounts, "nomadx")
	write(account)
	if got := ccProfileImagePath(home, "nomadx", accounts); got != account {
		t.Fatalf("AccountsService fallback = %q, want %q", got, account)
	}

	face := filepath.Join(home, ".face")
	write(face)
	if got := ccProfileImagePath(home, "nomadx", accounts); got != face {
		t.Fatalf(".face precedence = %q, want %q", got, face)
	}

	icon := filepath.Join(home, ".face.icon")
	write(icon)
	if got := ccProfileImagePath(home, "nomadx", accounts); got != icon {
		t.Fatalf(".face.icon precedence = %q, want %q", got, icon)
	}
}

func TestCCProfileImagePathRejectsDirectoriesAndMissingFiles(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".face.icon"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := ccProfileImagePath(home, "nomadx", t.TempDir()); got != "" {
		t.Fatalf("profile image = %q, want no regular source", got)
	}
}

func TestCCAvatarKeepsCircularGeometryForImageAndFallback(t *testing.T) {
	image := &ui.Image{Width: 1, Height: 1, Stride: 4, Pix: []byte{0xff, 0xff, 0xff, 0xff}}
	for name, tc := range map[string]struct {
		image *ui.Image
		kind  ui.Kind
	}{
		"decoded":  {image: image, kind: ui.KindImage},
		"fallback": {kind: ui.KindCapsule},
	} {
		t.Run(name, func(t *testing.T) {
			got := ccAvatarNode(tc.image)
			if got.Kind != tc.kind || got.Width != 56 || got.Height != 56 || got.Shape != ui.ShapeCircle {
				t.Fatalf("avatar = %+v, want %v 56x56 circle", got, tc.kind)
			}
			if tc.image == nil {
				icon := findNode(got, func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
				if icon == nil || icon.Icon != "person" {
					t.Fatalf("fallback = %+v, want person glyph", icon)
				}
			}
		})
	}
}
