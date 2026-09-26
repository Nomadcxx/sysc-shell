package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestRegistryRecordsCompositorBlur(t *testing.T) {
	t.Parallel()
	r := &Registry{}
	r.mu.Lock()
	if r.blurAvailableLocked() {
		t.Fatal("a registry that heard nothing reports blur; the zero answer must be no")
	}
	r.mu.Unlock()
	r.SetCapabilities(wayland.Capabilities{Blur: true})
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.blurAvailableLocked() {
		t.Fatal("blur was reported but not recorded")
	}
}

// TestBlurCapabilityRestylesTheLiveBar: a frosted bar built before the
// compositor answered paints solid, then frosts in place when blur arrives and
// returns to solid when it goes, without a reload.
func TestBlurCapabilityRestylesTheLiveBar(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right = nil, nil, nil
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	reg.tokens = theme.Fallback
	bar, leases, _, err := reg.buildBar(cfg, "DP-2", reg.tokens)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseAll(leases) })
	reg.setTestBar(7, bar)

	solid := bar.themeSnapshot()
	if solid.Blur || solid.PillAlpha != 0xff {
		t.Fatalf("before any capability the bar must paint solid: blur %v pill %#x", solid.Blur, solid.PillAlpha)
	}
	reg.SetCapabilities(wayland.Capabilities{Blur: true})
	frosted := bar.themeSnapshot()
	if !frosted.Blur || frosted.Surfaces.Bar >= solid.Surfaces.Bar || frosted.PillAlpha == 0xff {
		t.Fatalf("blur arrived but the bar did not frost: blur %v ground %#x pill %#x",
			frosted.Blur, frosted.Surfaces.Bar, frosted.PillAlpha)
	}
	reg.SetCapabilities(wayland.Capabilities{})
	if back := bar.themeSnapshot(); back.Blur || back.Surfaces.Bar != solid.Surfaces.Bar || back.PillAlpha != 0xff {
		t.Fatalf("blur went away but the bar stayed frosted: blur %v ground %#x pill %#x",
			back.Blur, back.Surfaces.Bar, back.PillAlpha)
	}
}
