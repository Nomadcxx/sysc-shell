package shell

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestCJKFontCommandForOS(t *testing.T) {
	tests := []struct {
		id      string
		manager string
		args    []string
	}{
		{"arch", "/usr/bin/pacman", []string{"-S", "--needed", "--noconfirm", "--", "noto-fonts-cjk"}},
		{"debian", "/usr/bin/apt-get", []string{"install", "-y", "--", "fonts-noto-cjk"}},
		{"ubuntu", "/usr/bin/apt-get", []string{"install", "-y", "--", "fonts-noto-cjk"}},
		{"fedora", "/usr/bin/dnf", []string{"install", "-y", "--", "google-noto-sans-cjk-ttc-fonts"}},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			spec, ok := cjkFontCommandForOS([]byte("ID=" + tc.id + "\n"))
			if !ok || spec.manager != tc.manager || !equalStrings(spec.args, tc.args) {
				t.Fatalf("spec = %+v, %v; want manager %s args %v", spec, ok, tc.manager, tc.args)
			}
		})
	}
	for _, id := range []string{"", "archlinux", "rhel", "opensuse"} {
		if _, ok := cjkFontCommandForOS([]byte("ID=" + id + "\n")); ok {
			t.Errorf("unexpected package command for %q", id)
		}
	}
}

func TestOnboardingFontOfferFollowsLanguageAndCoverage(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 800, OutH: 500}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbRegion
	h.onbFontLocale = "ja"
	h.onbFontState = onboardingFontCovered
	reg.rebuildPanel(h)
	if byAction(h.root, "onb-font-install") != nil {
		t.Fatal("covered Japanese fonts still offered an install")
	}
	h.onbFontState = onboardingFontMissing
	reg.rebuildPanel(h)
	if byAction(h.root, "onb-font-install") == nil {
		t.Fatal("missing Japanese fonts did not offer an install")
	}
	button := byAction(h.root, "onb-font-install")
	metrics := h.metrics()
	actions := onboardingActions(h.root)
	languageIndex, installIndex := -1, -1
	for i, action := range actions {
		if action == "set:session.language" {
			languageIndex = i
		}
		if action == "onb-font-install" {
			installIndex = i
		}
	}
	reg.mu.Unlock()
	if button.Width != 0 || button.Shape != ui.ShapeStadium || button.Padding != metrics.ButtonPadding || button.Height != metrics.StandardControl {
		t.Fatalf("font install pill = %+v; want natural width with shared pill metrics", button)
	}
	if languageIndex < 0 || installIndex < 0 || languageIndex >= installIndex {
		t.Fatalf("language/install action order = %v", actions)
	}
}

func TestOnboardingFontCopyUsesSelectedLanguage(t *testing.T) {
	cfg := config.Default()
	cfg.Session.Language = "es"
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 800, OutH: 500}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbRegion
	h.onbFontLocale = "ja"
	h.onbFontState = onboardingFontMissing
	h.set = reg.settingsForLocked(h.draft)
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
		t.Fatalf("configure narrow Spanish Region page: %v", err)
	}
	reg.mu.Lock()
	copy := treeText(h.root)
	reg.mu.Unlock()
	if !strings.Contains(copy, "Las fuentes instaladas no incluyen japonés") || !strings.Contains(copy, "Instalar Noto CJK") {
		t.Fatalf("Spanish font guidance = %q; want localized status and install action", copy)
	}
}

func TestOnboardingCJKCoverageSkipsInstallPrompt(t *testing.T) {
	cfg := config.Default()
	cfg.Session.Language = "ja"
	reg := NewRegistry(cfg)
	reg.onboardingFontCoverage = func(language string) bool { return language == "ja" }
	reg.onboardingFontInstallAvailable = func() bool {
		t.Error("covered fonts should skip the install availability check")
		return true
	}
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbRegion
	reg.checkOnboardingFont(h)
	reg.rebuildPanel(h)
	reg.mu.Unlock()

	waitOnboardingFontState(t, reg, onboardingFontCovered)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if byAction(reg.panelHosts[PanelOnboarding].root, "onb-font-install") != nil {
		t.Fatal("coverage probe found Japanese fonts but the install offer remained")
	}
}

func TestOnboardingFontInstallRequiresExplicitAction(t *testing.T) {
	reg := newPanelRegistry(t)
	var calls int
	reg.onboardingFontInstall = func() error { calls++; return nil }
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbRegion
	h.onbFontLocale = "ja"
	h.onbFontState = onboardingFontMissing
	reg.rebuildPanel(h)
	if calls != 0 {
		t.Fatal("font package install ran without user action")
	}
	if !h.activateOnboarding(reg, &ui.Node{Action: "onb-font-install"}) {
		t.Fatal("onboarding did not accept the explicit font install action")
	}
	reg.mu.Unlock()

	waitOnboardingFontState(t, reg, onboardingFontInstalled)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if calls != 1 {
		t.Fatalf("font install calls = %d, want 1", calls)
	}
	if got := onboardingFontCopy(reg.panelHosts[PanelOnboarding], "ja", reg.panelHosts[PanelOnboarding].onbFontState); got != "Noto CJK is installed. Restart sysc-shell to use the new fonts." {
		t.Fatalf("restart guidance = %q", got)
	}
}

func TestOnboardingFontInstallCannotDuplicateAfterReopen(t *testing.T) {
	reg := newPanelRegistry(t)
	var installed atomic.Bool
	var calls atomic.Int32
	started := make(chan struct{})
	finished := make(chan struct{})
	var finishOnce sync.Once
	releaseInstall := func() { finishOnce.Do(func() { close(finished) }) }
	defer releaseInstall()
	reg.onboardingFontCoverage = func(string) bool { return installed.Load() }
	reg.onboardingFontInstallAvailable = func() bool { return true }
	reg.onboardingFontInstall = func() error {
		calls.Add(1)
		close(started)
		<-finished
		installed.Store(true)
		return nil
	}
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbRegion
	h.onbFontLocale = "ja"
	h.onbFontState = onboardingFontMissing
	accepted := h.activateOnboarding(reg, &ui.Node{Action: "onb-font-install"})
	reg.mu.Unlock()
	if !accepted {
		t.Fatal("onboarding did not accept the explicit font install action")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("explicit font install did not start")
	}
	reg.ClosePanel(PanelOnboarding)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h = reg.panelHosts[PanelOnboarding]
	h.onbPage = onbRegion
	h.draft.Session.Language = "ko"
	reg.checkOnboardingFont(h)
	reg.mu.Unlock()
	state := waitOnboardingFontNotChecking(t, reg)
	reg.mu.Lock()
	h = reg.panelHosts[PanelOnboarding]
	reg.rebuildPanel(h)
	offered := byAction(h.root, "onb-font-install") != nil
	reg.mu.Unlock()
	if state != onboardingFontInstalling {
		t.Fatalf("reopened font state = %v, want installing", state)
	}
	if offered {
		t.Fatal("reopened wizard offered a duplicate install")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("font install calls = %d, want 1", got)
	}
	releaseInstall()
	waitOnboardingFontState(t, reg, onboardingFontInstalled)
}

func TestOnboardingFontRestartGuidancePersistsAfterCompletedInstallAndReopen(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.onboardingFontCoverage = func(string) bool { return true }
	reg.onboardingFontInstallAvailable = func() bool { return true }
	reg.onboardingFontInstall = func() error { return nil }
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	if h == nil {
		reg.mu.Unlock()
		t.Fatal("onboarding panel was not open")
	}
	h.onbPage = onbRegion
	h.onbFontLocale = "ja"
	h.onbFontState = onboardingFontMissing
	if !h.activateOnboarding(reg, &ui.Node{Action: "onb-font-install"}) {
		reg.mu.Unlock()
		t.Fatal("onboarding did not accept the explicit font install action")
	}
	reg.mu.Unlock()
	waitOnboardingFontState(t, reg, onboardingFontInstalled)
	reg.ClosePanel(PanelOnboarding)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h = reg.panelHosts[PanelOnboarding]
	h.onbPage = onbRegion
	h.draft.Session.Language = "ja"
	reg.checkOnboardingFont(h)
	state := h.onbFontState
	copy := onboardingFontCopy(h, h.onbFontLocale, h.onbFontState)
	reg.mu.Unlock()
	if state != onboardingFontInstalled || copy != "Noto CJKをインストールしました。反映するにはsysc-shellを再起動してください。" {
		t.Fatalf("reopened installed font state = %v (%q), want restart guidance", state, copy)
	}
}

func waitOnboardingFontState(t *testing.T, reg *Registry, want onboardingFontState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		h := reg.panelHosts[PanelOnboarding]
		got := onboardingFontOff
		if h != nil {
			got = h.onbFontState
		}
		reg.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("onboarding font state did not become %v", want)
}

func waitOnboardingFontNotChecking(t *testing.T, reg *Registry) onboardingFontState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		h := reg.panelHosts[PanelOnboarding]
		state := onboardingFontOff
		if h != nil {
			state = h.onbFontState
		}
		reg.mu.Unlock()
		if state != onboardingFontChecking {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("onboarding font state remained checking")
	return onboardingFontOff
}

func onboardingActions(root *ui.Node) []string {
	var actions []string
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Action != "" {
			actions = append(actions, n.Action)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return actions
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
