package shell

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestPluginWallpaperSnapshotNeedsService(t *testing.T) {
	h := &pluginHost{r: &Registry{}}
	if _, err := h.wallpaperSnapshot(context.Background()); err == nil || err.Error() != "wallpaper service is not available" {
		t.Fatalf("snapshot error = %v, want wallpaper service unavailable", err)
	}
}

func TestGrantedCapabilitiesIncludeWallpaper(t *testing.T) {
	for _, capability := range hostPluginCaps {
		if capability == plugin.CapWallpaper {
			return
		}
	}
	t.Fatal("host plugin capabilities omit wallpaper")
}

func TestPluginWallpaperSnapshotProjectsOutputs(t *testing.T) {
	projection := &pluginWallpaperProjection{}
	snapshot := wallpaper.Snapshot{
		Connectors: []string{"DP-5", "DP-4", "DP-3", "DP-2", "DP-1"},
		Assignments: map[string]wallpaper.Assignment{
			"DP-1": {Kind: wallpaper.KindImage, Path: "/wallpaper.jpg"},
			"DP-2": {Kind: wallpaper.KindVideo, Path: "/wallpaper.mp4"},
			"DP-4": {Kind: wallpaper.KindImage, Path: "/transition.jpg"},
			"DP-5": {Kind: wallpaper.KindImage, Path: "/covered.jpg"},
		},
		Runtime: map[string]wallpaper.Runtime{
			"DP-4": {State: wallpaper.StateStarting},
			"DP-5": {State: wallpaper.StateStarting},
		},
		Covered: map[string]string{"DP-5": "foreign-background"},
	}

	got := projection.project(snapshot, "fill")
	want := []v1.WallpaperOutput{
		{Output: "DP-1", State: v1.WallpaperImage, Path: "/wallpaper.jpg"},
		{Output: "DP-2", State: v1.WallpaperVideo},
		{Output: "DP-3", State: v1.WallpaperNone},
		{Output: "DP-4", State: v1.WallpaperTransitioning},
		{Output: "DP-5", State: v1.WallpaperCovered},
	}
	if !reflect.DeepEqual(got.Outputs, want) {
		t.Fatalf("outputs = %+v, want %+v", got.Outputs, want)
	}
	if got.Scale != "fill" || got.Revision != 1 {
		t.Fatalf("projection = %+v, want scale fill and initial revision 1", got)
	}
}

func TestPluginWallpaperSnapshotRevisionTracksVisibleChanges(t *testing.T) {
	projection := &pluginWallpaperProjection{}
	snapshot := wallpaper.Snapshot{
		Connectors: []string{"DP-1"},
		Assignments: map[string]wallpaper.Assignment{
			"DP-1": {Kind: wallpaper.KindImage, Path: "/first.jpg"},
		},
	}

	first := projection.project(snapshot, "fill")
	if got := projection.project(snapshot, "fill").Revision; got != first.Revision {
		t.Fatalf("unchanged revision = %d, want %d", got, first.Revision)
	}

	snapshot.Assignments["DP-1"] = wallpaper.Assignment{Kind: wallpaper.KindImage, Path: "/second.jpg"}
	pathChange := projection.project(snapshot, "fill")
	if pathChange.Revision != first.Revision+1 {
		t.Fatalf("path change revision = %d, want %d", pathChange.Revision, first.Revision+1)
	}

	snapshot.Runtime = map[string]wallpaper.Runtime{"DP-1": {State: wallpaper.StateStarting}}
	stateChange := projection.project(snapshot, "fill")
	if stateChange.Revision != pathChange.Revision+1 {
		t.Fatalf("state change revision = %d, want %d", stateChange.Revision, pathChange.Revision+1)
	}

	scaleChange := projection.project(snapshot, "stretch")
	if scaleChange.Revision != stateChange.Revision+1 {
		t.Fatalf("scale change revision = %d, want %d", scaleChange.Revision, stateChange.Revision+1)
	}
}

func TestWallpaperMaskRegistrationAcceptsCurrentImage(t *testing.T) {
	f := newWallpaperMaskFixture(t, wallpaper.KindImage, false, false)
	params := v1.WallpaperMaskSetParams{Output: "DP-1", WallpaperPath: f.wallpaperA, MaskPath: f.mask}
	if err := f.host.registerWallpaperMask(context.Background(), "plugin-a", params); err != nil {
		t.Fatalf("register current image mask: %v", err)
	}
	got := f.h.surfaces["DP-1"].descriptor
	if got.owner != "plugin-a" || got.wallpaperPath != f.wallpaperA || got.maskPath != f.mask {
		t.Fatalf("registered descriptor = %+v", got)
	}
	if got.mask == nil || got.mask.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Fatalf("registered mask = %v, want a decoded 2x1 alpha mask", got.mask)
	}
	f.host.clearWallpaperMasks("plugin-a")
	if err := f.host.registerWallpaperMask(context.Background(), "plugin-a", params); err != nil {
		t.Fatalf("register after session cleanup: %v", err)
	}
}

func TestWallpaperMaskRegistrationRejectsInvalidStateWithoutReplacing(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       wallpaper.Kind
		transition bool
		covered    bool
		output     string
		path       string
		mask       string
		owner      string
	}{
		{name: "stale wallpaper path", kind: wallpaper.KindImage, output: "DP-1", path: "other"},
		{name: "video", kind: wallpaper.KindVideo, output: "DP-1", path: "current"},
		{name: "transition", kind: wallpaper.KindImage, transition: true, output: "DP-1", path: "current"},
		{name: "covered", kind: wallpaper.KindImage, covered: true, output: "DP-1", path: "current"},
		{name: "unknown connector", kind: wallpaper.KindImage, output: "DP-2", path: "current"},
		{name: "invalid dimensions", kind: wallpaper.KindImage, output: "DP-1", path: "current", mask: "wide"},
		{name: "another plugin owns output", kind: wallpaper.KindImage, output: "DP-1", path: "current", owner: "plugin-b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWallpaperMaskFixture(t, tc.kind, tc.transition, tc.covered)
			prior := depthClockTestDescriptor("plugin-a", 17)
			if err := f.h.set("DP-1", prior); err != nil {
				t.Fatal(err)
			}
			path := f.wallpaperA
			if tc.path == "other" {
				path = f.wallpaperB
			}
			mask := f.mask
			if tc.mask == "wide" {
				mask = f.wideMask
			}
			owner := tc.owner
			if owner == "" {
				owner = "plugin-a"
			}
			if err := f.host.registerWallpaperMask(context.Background(), owner, v1.WallpaperMaskSetParams{
				Output: tc.output, WallpaperPath: path, MaskPath: mask,
			}); err == nil {
				t.Fatal("invalid mask registration succeeded")
			}
			got := f.h.surfaces["DP-1"].descriptor
			if got.mask != prior.mask || got.owner != prior.owner || got.wallpaperPath != prior.wallpaperPath {
				t.Fatalf("rejected registration replaced descriptor: %+v", got)
			}
		})
	}
}

func TestWallpaperMaskClearIsScopedToTheOwningPlugin(t *testing.T) {
	r, h, _ := newDepthClockTestHost(t, map[string]struct {
		global uint32
		width  int
		height int
	}{"DP-1": {global: 7, width: 1920, height: 1080}})
	prior := depthClockTestDescriptor("plugin-a", 17)
	if err := h.set("DP-1", prior); err != nil {
		t.Fatal(err)
	}
	clear := v1.WallpaperMaskSetParams{Output: "DP-1"}
	pluginHost := &pluginHost{r: r}
	if err := pluginHost.registerWallpaperMask(context.Background(), "plugin-b", clear); err != nil {
		t.Fatal(err)
	}
	if got := h.surfaces["DP-1"].descriptor; got.mask != prior.mask || got.owner != "plugin-a" {
		t.Fatalf("foreign clear changed descriptor: %+v", got)
	}
	if err := pluginHost.registerWallpaperMask(context.Background(), "plugin-a", clear); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.surfaces["DP-1"]; ok {
		t.Fatal("owner clear left its depth clock surface open")
	}
	if r.depthClockLease != nil {
		t.Fatal("last cleared surface retained the clock lease")
	}
}

func TestWallpaperMaskCleanupOnDisableAndHostClose(t *testing.T) {
	for _, tc := range []struct {
		name string
		stop func(*pluginHost)
	}{
		{name: "disable", stop: func(h *pluginHost) { h.stopPlugin("org.sysc.timer") }},
		{name: "host close", stop: func(h *pluginHost) { h.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, h := bindWallpaperMaskPlugin(t, "ok")
			tc.stop(h)
			assertWallpaperMaskCleared(t, r)
		})
	}
}

func TestWallpaperMaskCleanupOnOrderlySessionEnd(t *testing.T) {
	r, h := bindWallpaperMaskPlugin(t, "ok")
	h.mu.Lock()
	slot := h.slots["org.sysc.timer"]
	h.mu.Unlock()
	if slot == nil {
		t.Fatal("plugin runtime was not started")
	}
	if err := slot.rt.Send(&v1.HostShutdown{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		_, exists := r.depthClocks.surfaces["DP-1"]
		r.mu.Unlock()
		if !exists {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("orderly session end retained its depth clock")
}

func bindWallpaperMaskPlugin(t *testing.T, mode string) (*Registry, *pluginHost) {
	t.Helper()
	r := bindTestPlugin(t, mode)
	bar := &Bar{conn: "DP-1"}
	bar.setOutputSize(1920, 1080)
	r.mu.Lock()
	r.bars[7] = bar
	h := r.plugins
	r.mu.Unlock()
	if err := r.depthClocks.set("DP-1", depthClockTestDescriptor("org.sysc.timer", 0)); err != nil {
		t.Fatal(err)
	}
	return r, h
}

func assertWallpaperMaskCleared(t *testing.T, r *Registry) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.depthClocks.surfaces["DP-1"]; exists {
		t.Fatal("plugin cleanup retained its depth clock")
	}
}

type wallpaperMaskTestEngine struct{ gate <-chan struct{} }

func (e wallpaperMaskTestEngine) Apply(wallpaper.Job, wallpaper.Settings) (string, error) {
	if e.gate != nil {
		<-e.gate
	}
	return "", nil
}
func (wallpaperMaskTestEngine) Restore(string, string) error { return nil }
func (wallpaperMaskTestEngine) SetPaused(string, bool) error { return nil }
func (wallpaperMaskTestEngine) Capabilities() wallpaper.Capabilities {
	return wallpaper.Capabilities{}
}

type wallpaperMaskFixture struct {
	r          *Registry
	h          *depthClockHost
	host       *pluginHost
	svc        *wallpaper.Service
	wallpaperA string
	wallpaperB string
	mask       string
	wideMask   string
}

func newWallpaperMaskFixture(t *testing.T, kind wallpaper.Kind, transition, covered bool) wallpaperMaskFixture {
	t.Helper()
	r, h, _ := newDepthClockTestHost(t, map[string]struct {
		global uint32
		width  int
		height int
	}{"DP-1": {global: 7, width: 1920, height: 1080}})
	dir := t.TempDir()
	wallpaperA := filepath.Join(dir, "a.png")
	wallpaperB := filepath.Join(dir, "b.png")
	mask := filepath.Join(dir, "mask.png")
	wideMask := filepath.Join(dir, "wide-mask.png")
	writeWallpaperMaskPNG(t, wallpaperA, image.NewRGBA(image.Rect(0, 0, 2, 1)))
	writeWallpaperMaskPNG(t, wallpaperB, image.NewRGBA(image.Rect(0, 0, 2, 1)))
	writeWallpaperMaskPNG(t, mask, image.NewGray(image.Rect(0, 0, 2, 1)))
	writeWallpaperMaskPNG(t, wideMask, image.NewGray(image.Rect(0, 0, 3, 1)))
	var gate chan struct{}
	if transition {
		gate = make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(gate) }) })
	}
	coverage := map[string]string(nil)
	if covered {
		coverage = map[string]string{"DP-1": "foreign-background"}
	}
	svc := wallpaper.NewService(wallpaper.ServiceConfig{
		Engine:     wallpaperMaskTestEngine{gate: gate},
		Settings:   wallpaper.Settings{Scale: "fill"},
		Connectors: []string{"DP-1"},
		Coverage: func() (map[string]string, error) {
			return coverage, nil
		},
	})
	r.mu.Lock()
	r.wallpaperSvc = svc
	r.mu.Unlock()
	path := wallpaperA
	svc.Enqueue(wallpaper.Command{Op: wallpaper.OpApply, Token: "DP-1", Path: path, Kind: kind})
	waitForWallpaperSnapshot(t, svc, func(s wallpaper.Snapshot) bool {
		if transition {
			return s.Runtime["DP-1"].State == wallpaper.StateStarting
		}
		return s.Assignments["DP-1"].Path == path
	})
	return wallpaperMaskFixture{
		r: r, h: h, host: &pluginHost{r: r}, svc: svc, wallpaperA: wallpaperA, wallpaperB: wallpaperB,
		mask: mask, wideMask: wideMask,
	}
}

func writeWallpaperMaskPNG(t *testing.T, path string, src image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, src); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func waitForWallpaperSnapshot(t *testing.T, svc *wallpaper.Service, done func(wallpaper.Snapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done(svc.Snapshot()) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("wallpaper snapshot did not reach the expected state: %+v", svc.Snapshot())
}
