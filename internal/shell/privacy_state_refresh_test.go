package shell

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestPrivacyPageRebuildsWhenPluginPublishes(t *testing.T) {
	reg := newPanelRegistryWithoutBridge(t)
	reg.now = time.Date(2026, time.October, 20, 12, 1, 0, 0, time.UTC)
	withPrivacyPlugin(t, reg, `{"control_center":`+privacySnapshotFixture+`}`)

	reg.mu.Lock()
	host := reg.plugins
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	if err := reg.selectPanelSectionLocked(PanelControlCenter, "privacy"); err != nil {
		reg.mu.Unlock()
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelControlCenter]
	before := renderText(h.root)
	reg.mu.Unlock()
	if !strings.Contains(before, "Updated 1 min ago") {
		t.Fatalf("initial privacy page = %q, want the stored snapshot", before)
	}
	for len(reg.invalidations) > 0 {
		<-reg.invalidations
	}

	// Other keys do not back the Control Centre privacy section.
	host.stateChanged(ccPrivacyPluginID, "unrelated")
	select {
	case inv := <-reg.invalidations:
		t.Fatalf("unrelated state invalidated the Control Centre: %+v", inv)
	default:
	}

	updated := strings.Replace(privacySnapshotFixture,
		`"generated_at":"2026-10-20T12:00:00Z"`,
		`"generated_at":"2026-10-20T12:01:00Z"`, 1)
	runtime := plugin.NewRuntime(plugin.Candidate{Manifest: plugin.Manifest{ID: ccPrivacyPluginID}}, plugin.RuntimeOptions{})
	env := host.callEnv(ccPrivacyPluginID, runtime, host.slots[ccPrivacyPluginID].store)
	env.Granted = []plugin.Capability{plugin.CapStateWrite}
	params, err := json.Marshal(v1.StateSetParams{Key: ccPrivacyStateKey, Value: json.RawMessage(updated)})
	if err != nil {
		t.Fatal(err)
	}
	reply := plugin.NewDispatcher(env).Handle(context.Background(), &v1.HostCall{
		ID: "privacy-snapshot", Call: v1.CallStateSet, Params: params,
	})
	if !reply.OK {
		t.Fatalf("state.set reply = %+v", reply)
	}

	want := wayland.Invalidation{Global: 7, SurfaceID: panelSurfaceID(PanelControlCenter)}
	timer := time.NewTimer(asyncTestWait)
	defer timer.Stop()
	for {
		select {
		case inv := <-reg.invalidations:
			if inv != want {
				t.Fatalf("privacy invalidation = %+v, want %+v", inv, want)
			}
			reg.mu.Lock()
			got := renderText(h.root)
			reg.mu.Unlock()
			if strings.Contains(got, "Updated just now") {
				return
			}
		case <-timer.C:
			reg.mu.Lock()
			got := renderText(h.root)
			reg.mu.Unlock()
			t.Fatalf("privacy page remained stale after state.set: %q", got)
		}
	}
}
