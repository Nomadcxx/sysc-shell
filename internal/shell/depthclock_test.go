package shell

import (
	"image"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func newDepthClockTestHost(t *testing.T, outputs map[string]struct {
	global uint32
	width  int
	height int
}) (*Registry, *depthClockHost, *hostHarness) {
	t.Helper()
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	harness := &hostHarness{}
	h := newDepthClockHost(r, harness)
	r.depthClocks = h
	r.mu.Lock()
	r.now = time.Date(2026, time.September, 23, 15, 4, 0, 0, time.Local)
	for connector, output := range outputs {
		bar := &Bar{conn: connector}
		bar.setOutputSize(output.width, output.height)
		r.bars[output.global] = bar
	}
	r.mu.Unlock()
	return r, h, harness
}

func depthClockTestDescriptor(owner string, coverage uint8) depthClockDescriptor {
	mask := image.NewAlpha(image.Rect(0, 0, 1, 1))
	mask.Pix[0] = coverage
	return depthClockDescriptor{owner: owner, wallpaperPath: "/wall.png", maskPath: "/mask.png", mask: mask}
}

func TestDepthClockSurfaceSpecAndClickThrough(t *testing.T) {
	_, h, harness := newDepthClockTestHost(t, map[string]struct {
		global uint32
		width  int
		height int
	}{"DP-1": {global: 7, width: 1920, height: 1080}})
	if err := h.set("DP-1", depthClockTestDescriptor("plugin-a", 0)); err != nil {
		t.Fatal(err)
	}
	if len(harness.opens) != 1 {
		t.Fatalf("opens = %d, want 1", len(harness.opens))
	}
	spec := harness.opens[0]
	if spec.Layer != layershell.ZwlrLayerShellV1LayerBottom || spec.Anchor != 0 || spec.ExclusiveZone != -1 || spec.Keyboard != keyboardNone {
		t.Fatalf("surface policy = layer %v anchor %d zone %d keyboard %d", spec.Layer, spec.Anchor, spec.ExclusiveZone, spec.Keyboard)
	}
	if spec.Namespace != depthClockNamespace || spec.ID != depthClockSurfaceID("DP-1") {
		t.Fatalf("surface identity = %q/%q", spec.Namespace, spec.ID)
	}
	if spec.Width != depthClockWidth || spec.Height != depthClockHeight {
		t.Fatalf("surface size = %dx%d, want %dx%d", spec.Width, spec.Height, depthClockWidth, depthClockHeight)
	}
	if spec.Callbacks.Handle == nil {
		t.Fatal("click-through surface has no required input callback")
	}
	if spec.Callbacks.Handle(wayland.Event{}) {
		t.Fatal("click-through surface handled input")
	}
	if err := spec.Callbacks.Configure(int(spec.Width), int(spec.Height), 150); err != nil {
		t.Fatal(err)
	}
	if len(harness.updates) != 1 || !harness.updates[0].SetInputRegion || len(harness.updates[0].InputRects) != 0 {
		t.Fatalf("configure input-region update = %+v, want an empty region", harness.updates)
	}
}

func TestDepthClockSurfaceClampsOnlyToSmallerOutput(t *testing.T) {
	for _, tc := range []struct {
		name             string
		outputW, outputH int
		wantW, wantH     int
	}{
		{name: "large", outputW: 1920, outputH: 1080, wantW: 560, wantH: 176},
		{name: "narrow", outputW: 480, outputH: 900, wantW: 480, wantH: 176},
		{name: "small", outputW: 320, outputH: 120, wantW: 320, wantH: 120},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if w, h := depthClockBoxSize(tc.outputW, tc.outputH); w != tc.wantW || h != tc.wantH {
				t.Fatalf("box = %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestDepthClockSpecUsesSmallOutputBounds(t *testing.T) {
	_, h, harness := newDepthClockTestHost(t, map[string]struct {
		global uint32
		width  int
		height int
	}{"small": {global: 9, width: 320, height: 120}})
	if err := h.set("small", depthClockTestDescriptor("plugin-a", 0)); err != nil {
		t.Fatal(err)
	}
	spec := harness.opens[0]
	if spec.Width != 320 || spec.Height != 120 {
		t.Fatalf("small-output surface = %dx%d, want 320x120", spec.Width, spec.Height)
	}
}

func TestDepthClockMaskReplacementClearAndOutputRemoval(t *testing.T) {
	r, h, harness := newDepthClockTestHost(t, map[string]struct {
		global uint32
		width  int
		height int
	}{
		"DP-1": {global: 7, width: 1920, height: 1080},
		"DP-2": {global: 8, width: 1920, height: 1080},
	})
	first := depthClockTestDescriptor("plugin-a", 0)
	if err := h.set("DP-1", first); err != nil {
		t.Fatal(err)
	}
	for len(r.invalidations) > 0 {
		<-r.invalidations
	}
	if err := h.set("DP-1", depthClockTestDescriptor("plugin-a", 255)); err != nil {
		t.Fatal(err)
	}
	if len(harness.opens) != 1 || len(harness.closes) != 0 {
		t.Fatalf("replacement recreated surface: %d opens, %d closes", len(harness.opens), len(harness.closes))
	}
	if got := h.surfaces["DP-1"].descriptor.mask.Pix[0]; got != 255 {
		t.Fatalf("replacement mask coverage = %d, want 255", got)
	}
	select {
	case invalidation := <-r.invalidations:
		if invalidation.SurfaceID != depthClockSurfaceID("DP-1") {
			t.Fatalf("replacement invalidated %q", invalidation.SurfaceID)
		}
	default:
		t.Fatal("replacement did not redraw the existing surface")
	}
	if err := h.set("DP-2", depthClockTestDescriptor("plugin-a", 0)); err != nil {
		t.Fatal(err)
	}
	if !r.clock.Running() || r.clock.Starts() != 1 {
		t.Fatalf("clock after first descriptor: running=%v starts=%d", r.clock.Running(), r.clock.Starts())
	}
	h.clear("DP-1", "other-plugin")
	if len(harness.closes) != 0 {
		t.Fatal("another plugin cleared the surface")
	}
	h.clear("DP-1", "plugin-a")
	if len(harness.closes) != 1 || !r.clock.Running() {
		t.Fatalf("first clear: closes=%d, clock running=%v", len(harness.closes), r.clock.Running())
	}
	h.clear("DP-2", "plugin-a")
	if len(harness.closes) != 2 || r.clock.Running() {
		t.Fatalf("clear: closes=%d, clock running=%v", len(harness.closes), r.clock.Running())
	}

	if err := h.set("DP-1", first); err != nil {
		t.Fatal(err)
	}
	r.SyncToastOutputs(nil)
	if len(harness.closes) != 3 || r.clock.Running() {
		t.Fatalf("output removal: closes=%d, clock running=%v", len(harness.closes), r.clock.Running())
	}
}

func TestDepthClockRedrawsOnScaleAndThemeReload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*config.Config)
	}{
		{name: "scale", change: func(cfg *config.Config) { cfg.Wallpaper.Scale = "stretch" }},
		{name: "theme", change: func(cfg *config.Config) { cfg.ThemeGen.Mode = "light" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, h, _ := newDepthClockTestHost(t, map[string]struct {
				global uint32
				width  int
				height int
			}{"DP-1": {global: 7, width: 1920, height: 1080}})
			if err := h.set("DP-1", depthClockTestDescriptor("plugin-a", 0)); err != nil {
				t.Fatal(err)
			}
			for len(r.invalidations) != 0 {
				<-r.invalidations
			}
			cfg := r.cfg
			tc.change(&cfg)
			prepared, err := r.PrepareConfig(cfg, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
			if err != nil {
				t.Fatal(err)
			}
			prepared.Commit()
			select {
			case got := <-r.invalidations:
				if got.SurfaceID != depthClockSurfaceID("DP-1") {
					t.Fatalf("reload invalidated %q", got.SurfaceID)
				}
			case <-time.After(time.Second):
				t.Fatal("config reload did not redraw the depth clock")
			}
		})
	}
}

func TestDepthClockSurfaceIDEscapesConnector(t *testing.T) {
	if got, want := depthClockSurfaceID("DP-1/A"), "depth-clock:DP-1%2FA"; got != want {
		t.Fatalf("surface id = %q, want %q", got, want)
	}
}

func TestDepthClockMinuteUpdateRebuildsAndPublishes(t *testing.T) {
	r, h, _ := newDepthClockTestHost(t, map[string]struct {
		global uint32
		width  int
		height int
	}{"DP-1": {global: 7, width: 1920, height: 1080}, "DP-2": {global: 8, width: 1920, height: 1080}})
	if err := h.set("DP-1", depthClockTestDescriptor("plugin-a", 0)); err != nil {
		t.Fatal(err)
	}
	if err := h.set("DP-2", depthClockTestDescriptor("plugin-a", 0)); err != nil {
		t.Fatal(err)
	}
	for len(r.invalidations) > 0 {
		<-r.invalidations
	}
	now := time.Date(2026, time.September, 24, 9, 7, 0, 0, time.Local)
	r.UpdateClock(now)
	for _, connector := range []string{"DP-1", "DP-2"} {
		content := h.surfaces[connector].tree.Children[0]
		if got, want := content.Children[0].Text, now.Format("15:04"); got != want {
			t.Errorf("%s time = %q, want %q", connector, got, want)
		}
		if got, want := content.Children[1].Text, now.Format("Mon 2 Jan"); got != want {
			t.Errorf("%s date = %q, want %q", connector, got, want)
		}
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case invalidation := <-r.invalidations:
			seen[invalidation.SurfaceID] = true
		case <-time.After(time.Second):
			t.Fatal("minute update did not publish both depth-clock surfaces")
		}
	}
	if !seen[depthClockSurfaceID("DP-1")] || !seen[depthClockSurfaceID("DP-2")] {
		t.Fatalf("published surfaces = %v", seen)
	}
}

func TestDepthClockRenderPaintsCardBeforeMasking(t *testing.T) {
	_, h, harness := newDepthClockTestHost(t, map[string]struct {
		global uint32
		width  int
		height int
	}{"DP-1": {global: 7, width: 1920, height: 1080}})
	if err := h.set("DP-1", depthClockTestDescriptor("plugin-a", 0)); err != nil {
		t.Fatal(err)
	}
	spec := harness.opens[0]
	if err := spec.Callbacks.Configure(int(spec.Width), int(spec.Height), 120); err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, int(spec.Width)*int(spec.Height)*4)
	if err := spec.Callbacks.Render(pixels, int(spec.Width), int(spec.Height), int(spec.Width)*4); err != nil {
		t.Fatal(err)
	}
	painted := false
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			painted = true
			break
		}
	}
	if !painted {
		t.Fatal("depth-clock card remained fully transparent with zero mask coverage")
	}
}

func TestDepthClockTreeUsesThemedTimeAndDateRoles(t *testing.T) {
	now := time.Date(2026, time.September, 24, 9, 7, 0, 0, time.Local)
	tree := depthClockTree(now)
	if len(tree.Children) != 1 || len(tree.Children[0].Children) != 2 {
		t.Fatalf("clock tree = %+v", tree)
	}
	timeNode, dateNode := tree.Children[0].Children[0], tree.Children[0].Children[1]
	if timeNode.Text != "09:07" || timeNode.TextRole != theme.RoleDisplay || !timeNode.Tabular {
		t.Fatalf("time node = %+v", timeNode)
	}
	if dateNode.Text != "Thu 24 Sep" || dateNode.TextRole != theme.RoleCaption {
		t.Fatalf("date node = %+v", dateNode)
	}
}
