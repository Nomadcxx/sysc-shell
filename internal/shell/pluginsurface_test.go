package shell

import (
	"context"
	"errors"
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

type observedContext struct {
	context.Context
	checked chan struct{}
}

func (c observedContext) Err() error {
	err := c.Context.Err()
	select {
	case c.checked <- struct{}{}:
	default:
	}
	return err
}

func TestFloatingSurfaceDragUsesCursorPositionAcrossSurfaceMoves(t *testing.T) {
	// The pointer remains at the same surface-local point while the surface
	// follows it; adding each origin back recovers the cursor's screen motion.
	dx, dy := surfacePointerDelta(80, 30, 40, 20, 92, 42, 40, 20)
	if dx != 12 || dy != 12 {
		t.Fatalf("drag delta = %d,%d, want 12,12", dx, dy)
	}
}

func TestClampFloatingSurfaceKeepsItsRectangleOnOutput(t *testing.T) {
	got := clampPluginSurface(pluginSurfaceState{X: 1000, Y: 700, Width: 360, Height: 440}, 1280, 800)
	if got.X != 920 || got.Y != 360 || got.Width != 360 || got.Height != 440 {
		t.Fatalf("clamped surface = %+v", got)
	}
}

func TestFloatingSurfaceRectRejectsOverflowingPluginPosition(t *testing.T) {
	if pluginSurfaceRectFits(math.MaxInt, 0, 360, 440, 1280, 800) {
		t.Fatal("accepted a position that overflows addition-based bounds checks")
	}
	if !pluginSurfaceRectFits(920, 360, 360, 440, 1280, 800) {
		t.Fatal("rejected a rectangle that exactly fits the output edge")
	}
}

func TestFloatingSurfaceTreeShowsResizeGrip(t *testing.T) {
	p := &pluginSurfaceHost{title: "Note", panel: &PanelHost{theme: DefaultTheme()}}
	root := p.wrapTree(&ui.Node{Kind: ui.KindColumn, Fill: ui.FillNoteSun})
	last := root.Children[len(root.Children)-1]
	if last.Kind != ui.KindRow || len(last.Children) != 2 || last.Children[1].Kind != ui.KindIcon || last.Children[1].Icon != "drag_indicator" {
		t.Fatalf("resize grip = %+v", last)
	}
}

func TestFloatingSurfaceDropDoesNotQueueCloseForReplacement(t *testing.T) {
	r := &Registry{aux: make(chan wayland.AuxRequest, 1), closed: make(chan struct{})}
	host := &pluginHost{
		r:        r,
		views:    map[string]*hostedView{"v1": {ID: "v1"}, "v2": {ID: "v2"}},
		surfaces: make(map[string]*pluginSurfaceHost),
	}
	old := &pluginSurfaceHost{
		host: host, panel: &PanelHost{theme: DefaultTheme()}, viewID: "v1",
		surfaceID: "plugin-floating:note", global: 7,
	}
	replacement := &pluginSurfaceHost{
		host: host, panel: &PanelHost{theme: DefaultTheme()}, viewID: "v2",
		surfaceID: old.surfaceID, global: 7,
	}
	host.surfaces[old.viewID] = old
	host.surfaces[replacement.viewID] = replacement

	drop := old.spec().OnDrop
	if drop == nil {
		t.Fatal("sticky note aux has no instance-bound drop callback")
	}
	drop()

	if _, ok := host.views[old.viewID]; ok {
		t.Fatal("delayed drop left its own view open")
	}
	if host.views[replacement.viewID] == nil || host.surfaces[replacement.viewID] != replacement {
		t.Fatal("delayed drop retired the replacement sticky note")
	}
	select {
	case req := <-r.aux:
		t.Fatalf("dropped surface queued an aux request for reusable id %q", req.ID)
	default:
	}
}

func TestFloatingSurfaceCloseWaitsForOpenSerialization(t *testing.T) {
	const pluginID = "org.sysc.floating"
	r := &Registry{aux: make(chan wayland.AuxRequest, 1), closed: make(chan struct{})}
	surface := &pluginSurfaceHost{surfaceID: "plugin-floating:note", viewID: "v1", global: 7}
	host := &pluginHost{
		r:        r,
		views:    map[string]*hostedView{"v1": {ID: "v1", Plugin: pluginID, Kind: v1.ViewFloating}},
		surfaces: map[string]*pluginSurfaceHost{"v1": surface},
	}
	surface.host = host

	host.surfaceMu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- host.closeFloatingSurface(context.Background(), pluginID, v1.SurfaceCloseParams{View: "v1"})
	}()
	<-started
	runtime.Gosched()
	select {
	case err := <-done:
		host.surfaceMu.Unlock()
		t.Fatalf("close returned while an open held surfaceMu: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	host.surfaceMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if req := <-r.aux; req.ID != surface.surfaceID || req.Open != nil || req.Update != nil {
		t.Fatalf("queued close = %+v", req)
	}
}

const floatingLifecyclePluginID = "org.sysc.floating"

func floatingLifecycleHost(t *testing.T) (*Registry, *pluginHost) {
	t.Helper()
	const pluginID = floatingLifecyclePluginID
	bar := &Bar{conn: "DP-1"}
	bar.output.width, bar.output.height = 1280, 800
	r := &Registry{
		aux:    make(chan wayland.AuxRequest),
		closed: make(chan struct{}),
		bars:   map[uint32]*Bar{7: bar},
	}
	state, err := plugin.OpenStore(t.TempDir(), pluginID)
	if err != nil {
		t.Fatal(err)
	}
	host := &pluginHost{
		r: r,
		slots: map[string]*pluginSlot{pluginID: {
			rt:    plugin.NewRuntime(plugin.Candidate{}, plugin.RuntimeOptions{}),
			store: state,
		}},
		views:    make(map[string]*hostedView),
		surfaces: make(map[string]*pluginSurfaceHost),
	}
	return r, host
}

func TestStopPluginWaitsForFloatingSurfaceOpen(t *testing.T) {
	const pluginID = floatingLifecyclePluginID
	r, host := floatingLifecycleHost(t)
	openDone := make(chan error, 1)
	go func() {
		_, err := host.openFloatingSurface(context.Background(), pluginID, v1.SurfaceOpenParams{
			Key: "note", Title: "Note", Output: "DP-1", Generation: 7,
			X: 20, Y: 30, Width: 360, Height: 480,
		})
		openDone <- err
	}()
	var openReq wayland.AuxRequest
	select {
	case openReq = <-r.aux:
		if openReq.Open == nil {
			t.Fatalf("first request was not an open: %+v", openReq)
		}
	case <-time.After(time.Second):
		t.Fatal("floating open did not reach the Wayland queue")
	}

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		host.stopPlugin(pluginID)
		close(done)
	}()
	<-started
	runtime.Gosched()
	var earlyClose *wayland.AuxRequest
	select {
	case req := <-r.aux:
		earlyClose = &req
	case <-time.After(100 * time.Millisecond):
	}
	openReq.Reply <- nil
	if err := <-openDone; err != nil {
		t.Fatalf("floating open failed: %v", err)
	}
	if earlyClose == nil {
		select {
		case req := <-r.aux:
			if req.ID != pluginSurfaceID(pluginID, "note", "DP-1") || req.Open != nil || req.Update != nil {
				t.Fatalf("queued stop request = %+v", req)
			}
		case <-time.After(time.Second):
			t.Fatal("plugin stop did not close its floating surface")
		}
	}
	<-done

	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.views) != 0 || len(host.surfaces) != 0 {
		t.Fatal("plugin stop left its floating view registered")
	}
	if earlyClose != nil {
		t.Fatalf("plugin stop queued a close before the Wayland open completed: %+v", *earlyClose)
	}
}

func TestOutputLostDuringFloatingSurfaceOpenClosesTheOpenedSurface(t *testing.T) {
	const pluginID = floatingLifecyclePluginID
	r, host := floatingLifecycleHost(t)
	openDone := make(chan error, 1)
	go func() {
		_, err := host.openFloatingSurface(context.Background(), pluginID, v1.SurfaceOpenParams{
			Key: "note", Title: "Note", Output: "DP-1", Generation: 7,
			X: 20, Y: 30, Width: 360, Height: 480,
		})
		openDone <- err
	}()
	var openReq wayland.AuxRequest
	select {
	case openReq = <-r.aux:
		if openReq.Open == nil {
			t.Fatalf("first request was not an open: %+v", openReq)
		}
	case <-time.After(time.Second):
		t.Fatal("floating open did not reach the Wayland queue")
	}

	lostDone := make(chan struct{})
	go func() {
		host.outputLost(7)
		close(lostDone)
	}()
	select {
	case req := <-r.aux:
		if req.ID != pluginSurfaceID(pluginID, "note", "DP-1") || req.Open != nil || req.Update != nil {
			t.Fatalf("output loss queued request = %+v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("output loss did not close the floating surface")
	}
	<-lostDone
	openReq.Reply <- nil

	followupClose := false
	select {
	case req := <-r.aux:
		followupClose = req.ID == pluginSurfaceID(pluginID, "note", "DP-1") && req.Open == nil && req.Update == nil
	case <-time.After(100 * time.Millisecond):
	}
	err := <-openDone
	host.mu.Lock()
	views, surfaces := len(host.views), len(host.surfaces)
	host.mu.Unlock()
	if err == nil {
		t.Fatal("open succeeded after output loss retired its view")
	}
	if !followupClose {
		t.Fatal("open did not queue a close after output loss when it completed")
	}
	if views != 0 || surfaces != 0 {
		t.Fatalf("output loss left %d views and %d surfaces registered", views, surfaces)
	}
}

func TestFloatingSurfaceOpenChecksCancellationAfterSerializationWait(t *testing.T) {
	host := &pluginHost{}
	host.surfaceMu.Lock()
	base, cancel := context.WithCancel(context.Background())
	checked := make(chan struct{}, 1)
	ctx := observedContext{Context: base, checked: checked}
	done := make(chan error, 1)
	go func() {
		_, err := host.openFloatingSurface(ctx, "org.sysc.floating", v1.SurfaceOpenParams{})
		done <- err
	}()
	<-checked
	cancel()
	host.surfaceMu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("floating open after cancellation = %v, want context.Canceled", err)
	}
}

func TestFloatingSurfaceCloseChecksCancellationAfterSerializationWait(t *testing.T) {
	const pluginID = "org.sysc.floating"
	r := &Registry{aux: make(chan wayland.AuxRequest, 1), closed: make(chan struct{})}
	surface := &pluginSurfaceHost{surfaceID: "plugin-floating:note", viewID: "v1", global: 7}
	host := &pluginHost{
		r:        r,
		views:    map[string]*hostedView{"v1": {ID: "v1", Plugin: pluginID, Kind: v1.ViewFloating}},
		surfaces: map[string]*pluginSurfaceHost{"v1": surface},
	}
	surface.host = host

	host.surfaceMu.Lock()
	base, cancel := context.WithCancel(context.Background())
	checked := make(chan struct{}, 1)
	ctx := observedContext{Context: base, checked: checked}
	done := make(chan error, 1)
	go func() {
		done <- host.closeFloatingSurface(ctx, pluginID, v1.SurfaceCloseParams{View: "v1"})
	}()
	<-checked
	cancel()
	host.surfaceMu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("floating close after cancellation = %v, want context.Canceled", err)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.views[surface.viewID] == nil || host.surfaces[surface.viewID] != surface {
		t.Fatal("cancelled close retired its floating view")
	}
	select {
	case req := <-r.aux:
		t.Fatalf("cancelled close queued an aux request: %+v", req)
	default:
	}
}
