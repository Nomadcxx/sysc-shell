package wayland

import (
	"errors"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestAuxCloseUnknownIDIsNoOp(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 7, "DP-1")
	var dropped []string
	o := &owner{hosts: s, cb: Callbacks{DropAux: func(_ uint32, id string) { dropped = append(dropped, id) }}}
	o.closeAux(h, "panel:session")
	if len(dropped) != 0 {
		t.Fatalf("DropAux called for unknown id: %v", dropped)
	}
	if len(h.aux) != 0 {
		t.Fatalf("aux map = %d, want empty", len(h.aux))
	}
}

func TestCloseAuxRemovesUnitAndNotifiesDropAux(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 7, "DP-1")
	h.aux["shield:session"] = newSurfaceUnit("shield:session")
	var dropped string
	o := &owner{hosts: s, cb: Callbacks{DropAux: func(output uint32, id string) {
		if output != 7 {
			t.Fatalf("DropAux output = %d, want 7", output)
		}
		dropped = id
	}}}
	o.closeAux(h, "shield:session")
	if dropped != "shield:session" {
		t.Fatalf("DropAux id = %q", dropped)
	}
	if _, ok := h.aux["shield:session"]; ok {
		t.Fatal("closed aux unit remained in the map")
	}
}

func TestReloadKeepsAuxMapped(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 7, "DP-1")
	h.policy = config.Default().Bar
	h.aux["panel:session"] = newSurfaceUnit("panel:session")
	o := &owner{hosts: s, cfg: ptrCfg(config.Default())}
	prepared := preparedOwnerConfig{
		cfg: config.Default(),
		hosts: []preparedHostConfig{{
			host:             h,
			policy:           config.Default().Bar,
			opaqueBackground: true,
			app:              validHostCallbacks(),
		}},
		commit: func() {},
	}
	// mappedHost has no real wl_surface, so the enabled+mapped branch would
	// try to recreate the bar. The contract under test is that aux is not
	// walked at all; disable the bar to take the idle path.
	prepared.hosts[0].policy.Enabled = false
	if err := o.applyPreparedConfig(prepared); err != nil {
		t.Fatalf("applyPreparedConfig: %v", err)
	}
	if _, ok := h.aux["panel:session"]; !ok {
		t.Fatal("reload tore down an open aux surface")
	}
}

func TestTeardownHostDropsAuxUnits(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 7, "DP-1")
	h.aux["shield:session"] = newSurfaceUnit("shield:session")
	h.aux["panel:session"] = newSurfaceUnit("panel:session")
	var dropped []string
	o := &owner{hosts: s, cb: Callbacks{DropAux: func(_ uint32, id string) { dropped = append(dropped, id) }}}
	if err := o.teardownHost(h); err != nil {
		t.Fatalf("teardownHost: %v", err)
	}
	if len(h.aux) != 0 {
		t.Fatalf("aux survived host teardown: %d", len(h.aux))
	}
	if len(dropped) != 2 {
		t.Fatalf("DropAux count = %d, want 2, got %v", len(dropped), dropped)
	}
}

func TestHandleAuxRequestCloseUnknownIsNoOp(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	mappedHost(s, 7, "DP-1")
	o := &owner{hosts: s}
	o.handleAux(AuxRequest{Output: 7, ID: "missing"})
}

func TestWakePipeQueuesAuxRequests(t *testing.T) {
	t.Parallel()
	w, err := newWakePipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	w.pushAux(AuxRequest{Output: 1, ID: "panel:session"})
	w.pushAux(AuxRequest{Output: 1, Open: &AuxSpec{ID: "shield:session"}})
	got := w.takeAux()
	if len(got) != 2 {
		t.Fatalf("queued %d aux requests, want 2", len(got))
	}
	if got[0].ID != "panel:session" || got[1].Open == nil || got[1].Open.ID != "shield:session" {
		t.Fatalf("queue order = %+v", got)
	}
	if extra := w.takeAux(); len(extra) != 0 {
		t.Fatalf("second take drained %d leftover requests", len(extra))
	}
}

func ptrCfg(c config.Config) *config.Config { return &c }

func TestAuxUpdateRaisesKeyboardInteractivityInPlace(t *testing.T) {
	t.Parallel()
	u := newSurfaceUnit("panel:session")
	u.ss.logicalWidth, u.ss.logicalHeight = 400, 300
	u.policy = auxPolicy{keyboard: uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityNone)}

	onDemand := uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityOnDemand)
	next, err := planAuxUpdate(u, &AuxUpdate{Keyboard: &onDemand})
	if err != nil {
		t.Fatal(err)
	}
	if next.keyboard != onDemand {
		t.Fatalf("keyboard = %d, want %d", next.keyboard, onDemand)
	}
	if next.hasInputRegion {
		t.Fatal("a keyboard-only update claimed an input region")
	}
}

func TestAuxRejectsUnsupportedLayerShellVersionBeforeCreatingSurface(t *testing.T) {
	o := &owner{rs: newRegistryState()}
	o.rs.singletons["zwlr_layer_shell_v1"] = globalEntry{version: 3}
	err := o.openAux(nil, &AuxSpec{
		ID: "plugin-floating:note", Namespace: "sysc-shell-plugin-sticky",
		RequiredLayerShellVersion: 4,
	})
	if err == nil || !strings.Contains(err.Error(), "compositor provides 3") {
		t.Fatalf("openAux error = %v, want a layer-shell version error", err)
	}
}

func TestAuxUpdatePlansMoveResizeAndLayerTogether(t *testing.T) {
	t.Parallel()
	u := newSurfaceUnit("note:test")
	u.policy = auxPolicy{
		layer: layershell.ZwlrLayerShellV1LayerTop,
		width: 360, height: 480,
		marginTop: 20, marginLeft: 24,
	}
	w, h := uint32(420), uint32(520)
	x, y := int32(80), int32(100)
	layer := layershell.ZwlrLayerShellV1LayerOverlay
	next, err := planAuxUpdate(u, &AuxUpdate{Width: &w, Height: &h, MarginLeft: &x, MarginTop: &y, Layer: &layer})
	if err != nil {
		t.Fatal(err)
	}
	if next.width != w || next.height != h || next.marginLeft != x || next.marginTop != y || next.layer != layer {
		t.Fatalf("planned policy = %+v", next)
	}
	if u.policy.width != 360 || u.policy.marginLeft != 24 || u.policy.layer != layershell.ZwlrLayerShellV1LayerTop {
		t.Fatalf("planning mutated current policy: %+v", u.policy)
	}
}

func TestAuxUpdateRejectsUnknownLayerWithoutChangingPolicy(t *testing.T) {
	t.Parallel()
	u := newSurfaceUnit("note:test")
	u.policy = auxPolicy{layer: layershell.ZwlrLayerShellV1LayerTop}
	bad := layershell.ZwlrLayerShellV1Layer(4)
	if _, err := planAuxUpdate(u, &AuxUpdate{Layer: &bad}); err == nil {
		t.Fatal("invalid layer was accepted")
	}
	if u.policy.layer != layershell.ZwlrLayerShellV1LayerTop {
		t.Fatal("invalid update changed policy")
	}
}

func TestAuxUpdateReplacesTheInputRegionThenEmptiesIt(t *testing.T) {
	t.Parallel()
	u := newSurfaceUnit("panel:session")
	u.ss.logicalWidth, u.ss.logicalHeight = 400, 300

	bounded := []ui.Rect{{X: 0, Y: 0, W: 400, H: 40}, {X: 10, Y: 40, W: 100, H: 60}}
	next, err := planAuxUpdate(u, &AuxUpdate{SetInputRegion: true, InputRects: bounded})
	if err != nil {
		t.Fatal(err)
	}
	if !next.hasInputRegion || len(next.inputRects) != 2 {
		t.Fatalf("input region = %+v", next)
	}
	// The owner keeps its own copy: a caller reusing its slice cannot mutate
	// the region the compositor was given.
	bounded[0].W = 9999
	if next.inputRects[0].W != 400 {
		t.Fatalf("stored rect followed the caller's mutation: %+v", next.inputRects[0])
	}
	u.policy = next

	// An empty region is not the same as no region: it means the surface takes
	// no pointer input at all, where an unset region means the whole surface.
	empty, err := planAuxUpdate(u, &AuxUpdate{SetInputRegion: true})
	if err != nil {
		t.Fatal(err)
	}
	if !empty.hasInputRegion {
		t.Fatal("an empty region was recorded as no region")
	}
	if len(empty.inputRects) != 0 {
		t.Fatalf("empty region kept %d rectangles", len(empty.inputRects))
	}
	if empty.keyboard != u.policy.keyboard {
		t.Fatal("a region-only update disturbed keyboard interactivity")
	}
}

func TestAuxUpdateRejectsRectanglesOutsideTheSurface(t *testing.T) {
	t.Parallel()
	u := newSurfaceUnit("panel:session")
	u.ss.logicalWidth, u.ss.logicalHeight = 400, 300

	for name, rect := range map[string]ui.Rect{
		"negative origin": {X: -1, Y: 0, W: 10, H: 10},
		"empty width":     {X: 0, Y: 0, W: 0, H: 10},
		"negative height": {X: 0, Y: 0, W: 10, H: -5},
		"past the right":  {X: 396, Y: 0, W: 10, H: 10},
		"past the bottom": {X: 0, Y: 295, W: 10, H: 10},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := planAuxUpdate(u, &AuxUpdate{
				SetInputRegion: true, InputRects: []ui.Rect{rect},
			}); err == nil {
				t.Fatalf("planAuxUpdate accepted %+v", rect)
			}
		})
	}
}

// TestAuxUpdateBoundsTheInputRegionByTheRequestedSize: an update that resizes
// the surface carries the input region for the new size. Checking it against
// the size being replaced rejected every region that grew with the surface,
// and the fire-and-forget failure took the shell down (a plugin panel resized
// from 400x700 to 400x760 on the laptop).
func TestAuxUpdateBoundsTheInputRegionByTheRequestedSize(t *testing.T) {
	t.Parallel()
	u := newSurfaceUnit("panel:plugin")
	u.ss.logicalWidth, u.ss.logicalHeight = 424, 700
	w, hgt := uint32(424), uint32(772)
	grown := []ui.Rect{{X: 12, Y: 0, W: 400, H: 760}}
	next, err := planAuxUpdate(u, &AuxUpdate{Width: &w, Height: &hgt, SetInputRegion: true, InputRects: grown})
	if err != nil {
		t.Fatalf("region inside the requested size rejected: %v", err)
	}
	if len(next.inputRects) != 1 || next.inputRects[0] != grown[0] {
		t.Fatalf("input region = %+v", next.inputRects)
	}
	past := []ui.Rect{{X: 12, Y: 0, W: 400, H: 773}}
	if _, err := planAuxUpdate(u, &AuxUpdate{Width: &w, Height: &hgt, SetInputRegion: true, InputRects: past}); err == nil {
		t.Fatal("region past the requested size accepted")
	}
}

func TestAuxUpdateForAMissingSurfaceLeavesSiblingsAlone(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 7, "DP-1")
	sibling := newSurfaceUnit("panel:session")
	sibling.policy = auxPolicy{keyboard: 1}
	h.aux["panel:session"] = sibling

	var dropped []string
	o := &owner{hosts: s, cb: Callbacks{DropAux: func(_ uint32, id string) { dropped = append(dropped, id) }}}

	onDemand := uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityOnDemand)
	if err := o.updateAux(h, "panel:missing", &AuxUpdate{Keyboard: &onDemand}); err == nil {
		t.Fatal("updateAux accepted an unknown surface")
	}
	if len(dropped) != 0 {
		t.Fatalf("a rejected update dropped %v", dropped)
	}
	if got := h.aux["panel:session"]; got != sibling || got.policy.keyboard != 1 {
		t.Fatalf("a rejected update disturbed the sibling: %+v", got.policy)
	}

	// An update for an output that is not mapped is refused the same way.
	o.handleAux(AuxRequest{Output: 99, ID: "panel:session", Update: &AuxUpdate{Keyboard: &onDemand}})
	if o.fatal != nil {
		t.Fatalf("a stale async update made the Wayland owner fatal: %v", o.fatal)
	}
	if h.aux["panel:session"].policy.keyboard != 1 {
		t.Fatal("an update for an unknown output reached a live surface")
	}
	if len(h.aux) != 1 {
		t.Fatalf("aux map = %d, want the sibling only", len(h.aux))
	}
	reply := make(chan error, 1)
	o.handleAux(AuxRequest{Output: 99, ID: "panel:session", Update: &AuxUpdate{Keyboard: &onDemand}, Reply: reply})
	if err := <-reply; err == nil {
		t.Fatal("a synchronous update for an unknown output was accepted")
	}
}

func TestFailUnitClosesTheAuxSurfaceAndSparesTheOwner(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 7, "DP-1")
	h.aux["panel:monitor"] = newSurfaceUnit("panel:monitor")
	var dropped string
	o := &owner{hosts: s, cb: Callbacks{DropAux: func(_ uint32, id string) { dropped = id }}}

	o.failUnit(h, h.aux["panel:monitor"], errors.New("ui: child 0: unsupported kind 7"))

	if o.fatal != nil {
		t.Fatalf("one panel's layout error killed the owner: %v", o.fatal)
	}
	if dropped != "panel:monitor" {
		t.Fatalf("DropAux id = %q, want the failing panel", dropped)
	}
	if _, ok := h.aux["panel:monitor"]; ok {
		t.Fatal("the failing panel stayed mapped")
	}
}

func TestFailUnitOnTheBarStaysFatal(t *testing.T) {
	t.Parallel()
	s := newHostSet()
	h := mappedHost(s, 7, "DP-1")
	o := &owner{hosts: s}

	o.failUnit(h, h.bar, errors.New("bar cannot allocate"))

	if o.fatal == nil {
		t.Fatal("a bar failure must stay fatal; there is no shell without it")
	}
}

// A wl_buffer destroyed while its wl_surface still exists can still receive
// wl_buffer.release, and dispatching an event for a dead id panics the client
// with "invalid server object ID". Destroying the surface first makes the
// compositor drop its references, so the buffers become safe to destroy.
//
// This is sysc-42: closing the settings panel tore down its aux surface in
// that order and panicked the Wayland client.
func TestTeardownUnitDestroysTheSurfaceBeforeItsBuffers(t *testing.T) {
	t.Parallel()
	u := newSurfaceUnit("panel:settings")

	gen := &generation{id: 1, fd: -1, width: 64, height: 44}
	gen.retire.attached() // the compositor still holds this buffer
	u.current = gen

	// The surface destructor observes how far teardown has gone. Buffers must
	// still be attached to the unit when the surface is destroyed.
	var currentAtSurfaceDestroy *generation
	var retiringAtSurfaceDestroy int
	u.cleanup.push("surface", func() error {
		currentAtSurfaceDestroy = u.current
		retiringAtSurfaceDestroy = len(u.retiring)
		return nil
	})

	o := &owner{hosts: newHostSet()}
	if err := o.teardownUnit(u); err != nil {
		t.Fatalf("teardownUnit: %v", err)
	}

	if currentAtSurfaceDestroy == nil && retiringAtSurfaceDestroy == 0 {
		t.Fatal("buffers were destroyed before the surface; a late wl_buffer.release then hits a dead id")
	}
	if u.current != nil || len(u.retiring) != 0 {
		t.Fatal("generations survived teardown")
	}
	if !gen.retire.freeable() {
		t.Fatal("the generation was not marked freeable, so its storage leaked")
	}
}

func TestAuxUpdateResizesTheSurfaceInPlace(t *testing.T) {
	t.Parallel()
	u := newSurfaceUnit("panel:session")
	// openAux seeds the policy with the size the surface was opened at.
	u.policy.width, u.policy.height = 400, 300
	w, hgt := uint32(400), uint32(300)
	next, err := planAuxUpdate(u, &AuxUpdate{Width: &w, Height: &hgt})
	if err != nil {
		t.Fatal(err)
	}
	if next.width != 400 || next.height != 300 {
		t.Fatalf("size = %dx%d, want 400x300", next.width, next.height)
	}
	u.policy = next
	if next.hasInputRegion {
		t.Fatal("a size-only update claimed an input region")
	}

	// One axis at a time keeps the other at the opened size.
	only := uint32(480)
	next, err = planAuxUpdate(u, &AuxUpdate{Width: &only})
	if err != nil {
		t.Fatal(err)
	}
	if next.width != 480 || next.height != 300 {
		t.Fatalf("one-axis size = %dx%d, want 480x300", next.width, next.height)
	}
	u.policy = next

	// A nil size leaves the surface alone.
	next, err = planAuxUpdate(u, &AuxUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	if next.width != 480 || next.height != 300 {
		t.Fatalf("nil size changed the policy: %dx%d", next.width, next.height)
	}

	// A zero axis is meaningless.
	zero := uint32(0)
	if _, err := planAuxUpdate(u, &AuxUpdate{Height: &zero}); err == nil {
		t.Fatal("a zero height was accepted")
	}
}
