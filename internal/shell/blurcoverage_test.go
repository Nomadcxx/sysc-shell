package shell

import (
	"os"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestEveryPanelRequestsABackdrop walks the whole PanelID enum.
//
// A backdrop is a property of *being a panel*, not of the panels that happened
// to exist when blur was written: one added later has to inherit it without
// anyone remembering to wire it. Every panel routes through panelSpec, so this
// walks the enum rather than a list a new panel could be left out of, and fails
// if the enum grows past the end this walk knows about.
func TestEveryPanelRequestsABackdrop(t *testing.T) {
	t.Parallel()
	if got := PanelID(PanelControlCenter + 1).String(); got != "unknown" {
		t.Fatalf("the panel enum grew past PanelControlCenter (%q); widen this walk", got)
	}

	onCfg := config.Default()
	onCfg.Theme.BlurBehind = true
	onCfg.Theme.BlurRadius = 24
	on := NewRegistry(onCfg)
	t.Cleanup(on.Close)

	offCfg := config.Default()
	offCfg.Theme.BlurBehind = false
	off := NewRegistry(offCfg)
	t.Cleanup(off.Close)

	for id := PanelClock; id <= PanelControlCenter; id++ {
		h := &PanelHost{id: id, place: Placement{
			Output: ui.Rect{W: 1920, H: 1080},
			Panel:  ui.Rect{W: 400, H: 300},
		}}
		m := h.place.Margins()

		spec := on.panelSpec(h, m)
		if spec.BlurRegion == nil {
			t.Errorf("panel %s asks for no backdrop while blur is on", id)
			continue
		}
		if spec.BlurRegion.W <= 0 || spec.BlurRegion.H <= 0 {
			t.Errorf("panel %s asks to capture a degenerate region %+v", id, *spec.BlurRegion)
		}
		if spec.Callbacks.Backdrop == nil {
			t.Errorf("panel %s asks for a backdrop with no way to receive it", id)
		}
		// Blur off must cost nothing: no capture, no readback, no blur.
		if got := off.panelSpec(h, m).BlurRegion; got != nil {
			t.Errorf("panel %s captures %+v with blur off; that cost must not be paid", id, *got)
		}
	}
}

// TestEveryAuxSurfaceDecidesAboutBlur is a scan gate, in the shape of
// TestSurfaceSourcesCarryNoLegacyVisuals.
//
// panelSpec is the only AuxSpec that requests a backdrop, and every panel routes
// through it, so panels are covered. The risk this guards is a *different* kind
// of surface added later that builds its own AuxSpec and silently has no
// backdrop, with every test still passing.
//
// So the decision has to be written down at the site: an AuxSpec either sets
// BlurRegion or carries `blur-exempt:` with a reason. Design D13 scopes the
// backdrop to panels and names the bar, toasts, the OSD and tooltips as out;
// the menu and drawer surfaces are neither panels nor named there, which is a
// scope question their exemptions record rather than hide.
func TestEveryAuxSurfaceDecidesAboutBlur(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	scanned, found := 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "wayland.AuxSpec{") {
				continue
			}
			found++
			block := auxSpecContext(lines, i)
			if strings.Contains(block, "BlurRegion:") || strings.Contains(block, "blur-exempt:") {
				continue
			}
			t.Errorf("%s:%d builds an auxiliary surface that neither sets BlurRegion nor carries "+
				"a `blur-exempt: <reason>` comment; a panel blurs, and anything that does not has to say why",
				name, i+1)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no sources; the gate is not looking at the package")
	}
	if found == 0 {
		t.Fatal("found no AuxSpec literals; the gate is not matching what it is meant to")
	}
}

// auxSpecContext returns the composite literal beginning at start, plus the few
// lines above it, so the marker may sit on the surrounding comment rather than
// inside the literal.
func auxSpecContext(lines []string, start int) string {
	var b strings.Builder
	for _, prior := range lines[max(0, start-6):start] {
		b.WriteString(prior)
		b.WriteString("\n")
	}
	depth := 0
	for j := start; j < len(lines); j++ {
		b.WriteString(lines[j])
		b.WriteString("\n")
		code, _, _ := strings.Cut(lines[j], "//")
		depth += strings.Count(code, "{") - strings.Count(code, "}")
		if j > start && depth <= 0 {
			break
		}
	}
	return b.String()
}
