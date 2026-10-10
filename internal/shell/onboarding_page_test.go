package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestOnboardingPageNavigation(t *testing.T) {
	seq := []onboardingPage{onbWelcome, onbAppearance, onbRegion, onbIdle, onbReady}
	for i, p := range seq {
		if i < len(seq)-1 && p.next() != seq[i+1] {
			t.Errorf("page %d next()=%d want %d", i, p.next(), seq[i+1])
		}
		if i > 0 && p.prev() != seq[i-1] {
			t.Errorf("page %d prev()=%d want %d", i, p.prev(), seq[i-1])
		}
	}
	if onbWelcome.prev() != onbWelcome {
		t.Errorf("welcome prev escaped clamp")
	}
	if onbReady.next() != onbReady {
		t.Errorf("ready next escaped clamp")
	}
	if len(onbTitles) != int(onbPages) || len(onbIntros) != int(onbPages) {
		t.Fatalf("copy arrays must hold one entry per page")
	}
	for i, title := range onbTitles {
		if title == "" || onbIntros[i] == "" {
			t.Errorf("page %d has empty copy", i)
		}
	}
}

func TestOnboardingOfferConsumedOnce(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.onboardingPending = true
	reg.mu.Unlock()
	if !reg.consumeOnboardingOffer() {
		t.Fatal("the first host did not claim the offer")
	}
	if reg.consumeOnboardingOffer() {
		t.Fatal("a second host stole the offer")
	}
}

func TestOnboardingIdlePageUsesSharedIdleEntries(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	if h == nil {
		t.Fatal("the wizard did not host")
	}
	h.onbPage = onbIdle
	reg.rebuildPanel(h)
	if byAction(h.root, "pick:idle.after=nothing") == nil {
		t.Fatal("the idle page lost the shared idle.after control")
	}
	lock := byAction(h.root, "pick:idle.after=lock")
	if lock == nil {
		t.Fatal("the idle page lost the lock option")
	}
	if lock.State&ui.StateDisabled == 0 {
		t.Fatal("lock stayed selectable with no locker command")
	}
}

func TestOnboardingReopensFromSessionSettings(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if err := reg.selectPanelSectionLocked(PanelSettings, "Session"); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelSettings]
	reopen := byAction(h.root, "onboarding:open")
	if reopen == nil {
		t.Fatal("the Session section lost the reopen action")
	}
	h.setFocus(reopen)
	if !h.activate(reg) {
		t.Fatal("the reopen action was not accepted")
	}
	if !reg.panelOpenLocked(PanelOnboarding) {
		t.Fatal("reopen did not open the wizard")
	}
	if reg.panelHosts[PanelSettings] != nil {
		t.Fatal("the wizard left Settings open")
	}
}

func TestOnboardingOpensOnPrimaryAndReusesWallpaperPicker(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	if h == nil {
		t.Fatal("the wizard did not host")
	}
	f := h.focused()
	if f == nil || f.Name != onbNextLabel {
		t.Fatalf("initial focus name=%v, want %q", f, onbNextLabel)
	}
	h.onbPage = onbAppearance
	reg.rebuildPanel(h)
	pick := byAction(h.root, "onb-wallpaper")
	if pick == nil {
		t.Fatal("the appearance page lost the wallpaper picker action")
	}
	h.setFocus(pick)
	if !h.activate(reg) {
		t.Fatal("the picker action was not accepted")
	}
	if !reg.panelOpenLocked(PanelWallpaper) {
		t.Fatal("the shared picker did not open")
	}
	if !reg.panelOpenLocked(PanelOnboarding) {
		t.Fatal("the wizard closed underneath the picker")
	}
}

func TestOnboardingActionRowHierarchyAndKeptFocus(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	skip := byAction(h.root, "onb-skip")
	back := byAction(h.root, "onb-back")
	next := byAction(h.root, "onb-next")
	if skip == nil || back == nil || next == nil {
		t.Fatal("the action row lost a control")
	}
	if skip.Fill != ui.FillOutline || back.Fill != ui.FillOutline {
		t.Fatal("secondary actions are not visually subordinate")
	}
	if next.Fill != ui.FillNone {
		t.Fatal("the primary action lost its default fill")
	}
	for _, p := range []onboardingPage{onbAppearance, onbRegion, onbIdle, onbReady} {
		nav := byAction(h.root, "onb-next")
		if nav == nil {
			t.Fatalf("the Next action vanished before page %d", p)
		}
		h.setFocus(nav)
		if !h.activate(reg) {
			t.Fatalf("Next rejected on page %d", p)
		}
		if h.onbPage != p {
			t.Fatalf("advanced to %d, want %d", h.onbPage, p)
		}
		if f := h.focused(); f == nil || f.Name != onbMainLabel(p) {
			t.Errorf("page %d focus=%v, want %q", p, f, onbMainLabel(p))
		}
	}
}
