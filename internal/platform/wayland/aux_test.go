package wayland

import (
	"errors"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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
