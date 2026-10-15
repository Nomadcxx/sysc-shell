package shell

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	locksession "github.com/Nomadcxx/sysc-shell/internal/lock"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestConfigErrorPersistsUntilSuccessfulReload(t *testing.T) {
	reg := newPanelRegistry(t)
	refused := &config.Diagnostic{Field: "bar.height", Message: "value is not positive"}
	reg.RecordConfigReload(refused)
	first := reg.Status()
	message, ok := first["config_error"].(string)
	stamp, hasStamp := first["config_error_at"].(string)
	if !ok || !hasStamp || message == "" || stamp == "" {
		t.Fatalf("no refusal: %+v", first)
	}
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		t.Fatalf("config_error_at = %q: %v", stamp, err)
	}
	if !strings.Contains(message, "bar.height") || !strings.Contains(message, "value is not positive") {
		t.Fatalf("incomplete diagnostic: %q", message)
	}

	reg.RecordConfigReload(&config.Diagnostic{Field: "bar.height", Message: "value is not positive"})
	if got := reg.Status()["config_error_at"]; got != stamp {
		t.Fatalf("identical refusal reset timestamp: got %v, want %s", got, stamp)
	}

	reg.RecordConfigReload(nil)
	last := reg.Status()
	if last["config_error"] != "" || last["config_error_at"] != "" {
		t.Fatalf("recovery left stale refusal: %+v", last)
	}
}

func TestConfigErrorRejectsUnknownTextAndBoundsDiagnostic(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.RecordConfigReload(errors.New("secret-token"))
	status := reg.Status()
	message, ok := status["config_error"].(string)
	if !ok || message == "" || strings.Contains(message, "secret-token") {
		t.Fatalf("unknown error leaked or missing: %q", message)
	}

	reg.RecordConfigReload(&config.Diagnostic{
		Field:   "bar.height",
		Message: strings.Repeat("x", 8192),
	})
	message, _ = reg.Status()["config_error"].(string)
	if len(message) > 4096 {
		t.Fatalf("diagnostic exceeded the bound: %d bytes", len(message))
	}
}

func TestConfigReloadSettingsPresentationPersistsAndRecovers(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSettings]
	reg.mu.Lock()
	h.errLabel = "Local save failed"
	h.focusByName("Appearance")
	reg.rebuildPanel(h)
	if h.focused() == nil {
		reg.mu.Unlock()
		t.Fatal("Settings has no focused control")
	}
	focusBefore := h.focused().StableKey()
	reg.mu.Unlock()

	reg.RecordConfigReload(&config.Diagnostic{
		File: "config.json", Field: "bar.height", Message: "value is not positive",
		Excerpt: "line 2: [redacted]",
	})
	opened := drainAux(t, reg, 1)
	if opened[0].Open == nil || opened[0].Open.ID != "osd:7" {
		t.Fatalf("reload OSD request = %+v", opened[0])
	}

	reg.mu.Lock()
	reloadNodes := configReloadStatusNodes(h.root)
	focused := h.focused()
	focusAfter := ""
	if focused != nil {
		focusAfter = focused.StableKey()
	}
	localErr := h.errLabel
	view := reg.osd.view
	reg.mu.Unlock()
	if len(reloadNodes) != 1 {
		t.Fatalf("Settings reload diagnostics = %d, want one", len(reloadNodes))
	}
	diagnostic := reloadNodes[0]
	if diagnostic.Name != "Configuration reload error" || diagnostic.Role != "status" || diagnostic.Tone != ui.ToneError || diagnostic.Focusable {
		t.Fatalf("reload diagnostic semantics = %+v", diagnostic)
	}
	if !strings.Contains(diagnostic.Text, "bar.height") || localErr != "Local save failed" || !strings.Contains(treeText(h.root), localErr) {
		t.Fatalf("Settings did not keep both error sources: local=%q diagnostic=%q", localErr, diagnostic.Text)
	}
	if focusAfter == "" || focusAfter != focusBefore {
		t.Fatalf("Settings focus changed from %q to %q", focusBefore, focusAfter)
	}
	if view.Kind != osdConfigError || view.Text != "Config not applied" || strings.Contains(view.Text, "bar.height") {
		t.Fatalf("OSD exposed diagnostic details: %+v", view)
	}

	reg.ClosePanel(PanelSettings)
	drainAuxQueue(reg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	h = reg.panelHosts[PanelSettings]
	reg.mu.Lock()
	reloadNodes = configReloadStatusNodes(h.root)
	h.errLabel = "Local save failed"
	reg.rebuildPanel(h)
	reg.mu.Unlock()
	if len(reloadNodes) != 1 {
		t.Fatalf("reopened Settings lost stored diagnostic: %v", treeText(h.root))
	}

	reg.RecordConfigReload(nil)
	reg.mu.Lock()
	reloadNodes = configReloadStatusNodes(h.root)
	localErr = h.errLabel
	reg.mu.Unlock()
	if len(reloadNodes) != 0 || localErr != "Local save failed" {
		t.Fatalf("successful reload cleared wrong error source: reload=%d local=%q", len(reloadNodes), localErr)
	}
}

func TestRepeatedConfigReloadDoesNotReopenDismissedOSD(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	refused := &config.Diagnostic{Field: "bar.height", Message: "value is not positive"}
	reg.RecordConfigReload(refused)
	_ = drainAux(t, reg, 1)
	reg.OSD().hideAll()
	_ = drainAux(t, reg, 1)

	reg.RecordConfigReload(refused)
	if reg.OSD().Visible() {
		t.Fatal("identical refusal reopened a dismissed OSD")
	}
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("identical refusal enqueued another OSD request: %+v", req)
	default:
	}
}

func TestConfigReloadOSDIsSuppressedWhileLockerRuns(t *testing.T) {
	cases := []struct {
		name  string
		state locksession.State
		local bool
	}{
		{name: "tracked lock", local: true},
		{name: "pending acquisition", state: locksession.State{Known: true, Snapshot: locksession.Snapshot{Phase: "requesting"}}},
		{name: "sealed", state: locksession.State{Known: true, Snapshot: locksession.Snapshot{Phase: "sealed"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := newPanelRegistry(t)
			withTestBar(t, reg, 7, reg.cfg)
			reg.lockerRunning = tc.local
			reg.managedState = tc.state
			reg.RecordConfigReload(&config.Diagnostic{Field: "bar.height", Message: "value is not positive"})
			if reg.OSD().Visible() {
				t.Fatal("reload OSD mapped while the locker was active")
			}
			if status := reg.Status()["config_error"]; status == "" {
				t.Fatal("lock suppression discarded the persistent diagnostic")
			}
			select {
			case req := <-reg.AuxRequests():
				t.Fatalf("lock suppression sent an auxiliary request: %+v", req)
			default:
			}
			if tc.state.Phase == "requesting" {
				reg.applyManagedSnapshot(locksession.State{Known: true, Snapshot: locksession.Snapshot{Phase: "idle"}})
				if reg.OSD().Visible() {
					t.Fatal("unlock replayed the suppressed reload OSD")
				}
			}
		})
	}
}

func TestConfigReloadOSDIsNotSuppressedByDND(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	reg.setDND(true)
	reg.RecordConfigReload(&config.Diagnostic{Field: "bar.height", Message: "value is not positive"})
	reqs := drainAux(t, reg, 1)
	if reqs[0].Open == nil || reqs[0].Open.ID != "osd:7" {
		t.Fatalf("DND suppressed the reload OSD: %+v", reqs[0])
	}
}

func TestConfigReloadOSDUsesReducedMotion(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	reg.RecordConfigReload(&config.Diagnostic{Field: "bar.height", Message: "value is not positive"})
	m := reg.OSD()
	reg.mu.Lock()
	if got := m.slidePx(); got != 0 {
		reg.mu.Unlock()
		t.Fatalf("reduced-motion reload OSD slid %d pixels", got)
	}
	reg.mu.Unlock()
	if got := countSurfaceInvalidations(reg, reducedPanelCap+50*time.Millisecond); got == 0 {
		t.Fatal("reduced-motion reload OSD did not reveal")
	}
	reg.mu.Lock()
	settled := m.anim != nil && m.anim.Settled()
	reg.mu.Unlock()
	if !settled {
		t.Fatal("reduced-motion reload OSD did not settle within the existing fade cap")
	}
	if got := countSurfaceInvalidations(reg, 100*time.Millisecond); got != 0 {
		t.Fatalf("reduced-motion reload OSD kept invalidating after settlement: %d", got)
	}
}

func TestConfigReloadWithoutOutputRetainsStatus(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.RecordConfigReload(&config.Diagnostic{Field: "bar.height", Message: "value is not positive"})
	if reg.OSD().Visible() {
		t.Fatal("OSD became visible without an output")
	}
	status, ok := reg.Status()["config_error"].(string)
	if !ok || status == "" {
		t.Fatalf("no-output reload lost status: %+v", reg.Status())
	}
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("no-output reload enqueued an OSD request: %+v", req)
	default:
	}
}

func configReloadStatusNodes(root *ui.Node) []*ui.Node {
	var out []*ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Name == "Configuration reload error" && n.Role == "status" {
			out = append(out, n)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return out
}
