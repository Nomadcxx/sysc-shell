package shell

import (
	"slices"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/walls"
)

func enabledAtLoginSnapshot() walls.Snapshot {
	s := readyWallsSnapshot()
	s.UnitFileState = "enabled"
	return s
}

func newOpenIdleSettings(t *testing.T, snapshot walls.Snapshot, locker string) (*Registry, *PanelHost, *fakeWallsService) {
	t.Helper()
	r := newPanelRegistry(t)
	r.cfg.Session.Locker = locker
	service := newFakeWallsService(snapshot)
	r.attachWallsService(service)
	if err := r.OpenPanel(PanelSettings, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if err := r.selectPanelSectionLocked(PanelSettings, "Session"); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	h := r.panelHosts[PanelSettings]
	r.mu.Unlock()
	return r, h, service
}

func pickIdleAfter(t *testing.T, r *Registry, h *PanelHost, mode string) {
	t.Helper()
	n := findNode(h.root, func(n *ui.Node) bool { return n.Action == "pick:idle.after="+mode })
	if n == nil {
		t.Fatalf("no pick:idle.after=%s", mode)
	}
	h.setFocus(n)
	if !h.activate(r) {
		t.Fatalf("activate pick:idle.after=%s", mode)
	}
}

func selectedIdleAfter(h *PanelHost) string {
	for _, mode := range []string{"nothing", "screensaver", "lock"} {
		n := findNode(h.root, func(n *ui.Node) bool { return n.Action == "pick:idle.after="+mode })
		if n != nil && n.State&ui.StateSelected != 0 {
			return mode
		}
	}
	return ""
}

func TestSettingsAfterIdleApply(t *testing.T) {
	t.Run("enabled unit overlay and apply", func(t *testing.T) {
		r, h, fake := newOpenIdleSettings(t, enabledAtLoginSnapshot(), "sysc-lock")
		r.mu.Lock()
		defer r.mu.Unlock()

		if h.draft.Idle.Lock != 0 {
			t.Fatalf("setup Idle.Lock = %v, want 0", h.draft.Idle.Lock)
		}
		ss := findNode(h.root, func(n *ui.Node) bool { return n.Action == "pick:idle.after=screensaver" })
		if ss == nil || ss.State&ui.StateSelected == 0 {
			got := settings.WhenIdleMode(h.draft.Idle.Lock, false)
			t.Fatalf("After idle = %+v (catalog Get %q), want screensaver from unit snapshot", ss, got)
		}

		h.applySetting(r, &ui.Node{Kind: ui.KindMenu, Action: "set:idle.after", Text: "lock"})
		if h.draft.Idle.Lock <= 0 {
			t.Fatalf("set:idle.after lock left Idle.Lock=%v", h.draft.Idle.Lock)
		}
		fake.mu.Lock()
		enables := slices.Clone(fake.enables)
		patches := slices.Clone(fake.patches)
		fake.mu.Unlock()
		if len(enables) == 0 || enables[len(enables)-1] {
			t.Fatalf("lock apply SetEnabled = %v, want false", enables)
		}
		if len(patches) != 0 {
			t.Fatalf("lock apply sent walls timeout %v", patches)
		}

		fake.mu.Lock()
		fake.patches = nil
		fake.mu.Unlock()
		h.applySetting(r, &ui.Node{Kind: ui.KindTextField, Action: "set:idle.delay", Text: "10m"})
		if h.draft.Idle.Lock != 10*time.Minute {
			t.Fatalf("delay while lock wrote Idle.Lock=%v", h.draft.Idle.Lock)
		}
		fake.mu.Lock()
		if len(fake.patches) != 0 {
			t.Fatalf("delay while lock applied walls timeout %v", fake.patches)
		}
		fake.mu.Unlock()

		h.applySetting(r, &ui.Node{Kind: ui.KindMenu, Action: "set:idle.after", Text: "screensaver"})
		fake.mu.Lock()
		fake.patches = nil
		fake.mu.Unlock()
		h.applySetting(r, &ui.Node{Kind: ui.KindTextField, Action: "set:idle.delay", Text: "3m"})
		if h.draft.Idle.Lock != 0 {
			t.Fatalf("delay while screensaver set Idle.Lock=%v", h.draft.Idle.Lock)
		}
		fake.mu.Lock()
		patches = slices.Clone(fake.patches)
		fake.mu.Unlock()
		wantTimeout := (3 * time.Minute).String()
		found := false
		for _, batch := range patches {
			for _, s := range batch {
				if s.Key == "timeout" && s.Value == wantTimeout {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("delay while screensaver patches = %v, want timeout %q", patches, wantTimeout)
		}

		h.draft.Session.Locker = ""
		r.rebuildPanel(h)
		lock := findNode(h.root, func(n *ui.Node) bool { return n.Action == "pick:idle.after=lock" })
		if lock == nil || lock.State&ui.StateDisabled == 0 {
			t.Fatalf("Lock option with empty locker = %+v, want disabled", lock)
		}

		r.wallsSnapshot.ServiceAvailable = false
		r.rebuildPanel(h)
		ss = findNode(h.root, func(n *ui.Node) bool { return n.Action == "pick:idle.after=screensaver" })
		if ss == nil || ss.State&ui.StateDisabled == 0 {
			t.Fatalf("Screensaver option without service = %+v, want disabled", ss)
		}

		h.draft.Idle.Lock = 0
		r.wallsSnapshot.UnitFileState = "disabled"
		r.rebuildPanel(h)
		delay := findNode(h.root, func(n *ui.Node) bool { return n.Action == "set:idle.delay" })
		if delay == nil || delay.State&ui.StateDisabled == 0 {
			t.Fatalf("delay while nothing = %+v, want StateDisabled", delay)
		}
	})

	t.Run("disabled unit pick screensaver", func(t *testing.T) {
		r, h, fake := newOpenIdleSettings(t, readyWallsSnapshot(), "sysc-lock")
		r.mu.Lock()
		defer r.mu.Unlock()

		if selectedIdleAfter(h) != "nothing" {
			t.Fatalf("After idle = %q, want nothing on a disabled unit", selectedIdleAfter(h))
		}

		pickIdleAfter(t, r, h, "screensaver")
		if selectedIdleAfter(h) != "screensaver" {
			t.Fatalf("After idle = %q, want screensaver without waiting for Updates()", selectedIdleAfter(h))
		}
		delay := findNode(h.root, func(n *ui.Node) bool { return n.Action == "set:idle.delay" })
		wantDelay := settings.WhenIdleDelay(0, r.wallsSnapshot.Timeout).String()
		if delay == nil || delay.Text != wantDelay {
			got := ""
			if delay != nil {
				got = delay.Text
			}
			t.Fatalf("delay text = %q, want %q after leaving nothing", got, wantDelay)
		}

		h.applySetting(r, &ui.Node{Kind: ui.KindTextField, Action: "set:idle.delay", Text: "3m"})
		if h.draft.Idle.Lock != 0 {
			t.Fatalf("screensaver delay set Idle.Lock=%v", h.draft.Idle.Lock)
		}
		pickIdleAfter(t, r, h, "lock")
		if h.draft.Idle.Lock != 3*time.Minute {
			t.Fatalf("lock after typed delay: Idle.Lock=%v snapshot timeout=%q, want 3m not the stale unit timeout", h.draft.Idle.Lock, r.wallsSnapshot.Timeout)
		}
		fake.mu.Lock()
		enables := slices.Clone(fake.enables)
		fake.mu.Unlock()
		if len(enables) == 0 || enables[len(enables)-1] {
			t.Fatalf("pick lock SetEnabled = %v, want false", enables)
		}
	})

	t.Run("skip unit while locked or pending", func(t *testing.T) {
		r, h, fake := newOpenIdleSettings(t, enabledAtLoginSnapshot(), "sysc-lock")
		r.mu.Lock()
		defer r.mu.Unlock()

		r.lockerAcquired = true
		fake.mu.Lock()
		fake.enables, fake.patches = nil, nil
		fake.mu.Unlock()
		h.commitSetting(r, h.set.ByPath("idle.after"), "lock")
		fake.mu.Lock()
		enables, patches := slices.Clone(fake.enables), slices.Clone(fake.patches)
		fake.mu.Unlock()
		if len(enables) != 0 || len(patches) != 0 {
			t.Fatalf("toggled unit while locker acquired: enables=%v patches=%v", enables, patches)
		}

		r.lockerAcquired = false
		r.wallsSnapshot.ActionPending = true
		fake.mu.Lock()
		fake.enables, fake.patches = nil, nil
		fake.mu.Unlock()
		h.commitSetting(r, h.set.ByPath("idle.after"), "nothing")
		fake.mu.Lock()
		enables, patches = slices.Clone(fake.enables), slices.Clone(fake.patches)
		fake.mu.Unlock()
		if len(enables) != 0 || len(patches) != 0 {
			t.Fatalf("toggled unit while ActionPending: enables=%v patches=%v", enables, patches)
		}
	})

	t.Run("pending enable keeps screensaver", func(t *testing.T) {
		r, h, fake := newOpenIdleSettings(t, readyWallsSnapshot(), "sysc-lock")
		r.mu.Lock()
		pickIdleAfter(t, r, h, "screensaver")
		if selectedIdleAfter(h) != "screensaver" {
			r.mu.Unlock()
			t.Fatalf("After idle = %q, want screensaver", selectedIdleAfter(h))
		}
		r.mu.Unlock()

		pending := readyWallsSnapshot()
		pending.ActionPending = true
		fake.Publish(pending)
		waitFor(t, func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.wallsSnapshot.ActionPending
		})
		r.mu.Lock()
		if selectedIdleAfter(h) != "screensaver" {
			r.mu.Unlock()
			t.Fatalf("After idle during pending = %q, want screensaver", selectedIdleAfter(h))
		}
		r.mu.Unlock()

		fake.Publish(enabledAtLoginSnapshot())
		waitFor(t, func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return !r.wallsSnapshot.ActionPending && r.wallsSnapshot.EnabledAtLogin()
		})
		r.mu.Lock()
		defer r.mu.Unlock()
		if selectedIdleAfter(h) != "screensaver" {
			t.Fatalf("After idle after enable = %q, want screensaver", selectedIdleAfter(h))
		}
	})
}
