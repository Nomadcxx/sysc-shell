package wayland

import (
	"errors"
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
	if h.aux["panel:session"].policy.keyboard != 1 {
		t.Fatal("an update for an unknown output reached a live surface")
	}
	if len(h.aux) != 1 {
		t.Fatalf("aux map = %d, want the sibling only", len(h.aux))
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
