package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPanelWeatherIdentity(t *testing.T) {
	t.Parallel()
	if got := PanelWeather.String(); got != "weather" {
		t.Fatalf("String() = %q, want weather", got)
	}
	if got := panelSurfaceID(PanelWeather); got != "panel:weather" {
		t.Fatalf("surface id = %q, want panel:weather", got)
	}
	if got := panelTargetSize(PanelWeather); got != (ui.Rect{W: 460, H: 560}) {
		t.Fatalf("target size = %+v, want 460x560", got)
	}
}

func TestPanelWeatherAnswersItsIPCName(t *testing.T) {
	t.Parallel()
	id, err := parsePanelName("weather")
	if err != nil || id != PanelWeather {
		t.Fatalf("parsePanelName(weather) = %v, %v", id, err)
	}
	id, ok := panelIDFromAux("panel:weather")
	if !ok || id != PanelWeather {
		t.Fatalf("panelIDFromAux(panel:weather) = %v, %v", id, ok)
	}
}

func TestWeatherBarActionTogglesTheStandalonePanel(t *testing.T) {
	r := newPanelRegistry(t)
	bar := &Bar{conn: "DP-1"}
	r.bars = map[uint32]*Bar{7: bar}
	r.bindBarPanelActionsLocked(7, bar)

	if !bar.onAction(panelWeatherAction, buttonLeft) {
		t.Fatal("left-click did not handle the weather action")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h := r.panelHosts[PanelWeather]
	r.mu.Unlock()
	if h == nil {
		t.Fatal("left-click did not create PanelWeather")
	}

	if !bar.onAction(panelWeatherAction, buttonLeft) {
		t.Fatal("the second left-click was not handled")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h = r.panelHosts[PanelWeather]
	r.mu.Unlock()
	if h != nil {
		t.Fatal("the second left-click did not close PanelWeather")
	}
}
