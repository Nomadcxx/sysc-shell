package render

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The inventory the shell is allowed to name. This list is the contract with
// icons/material/build.py: a name here that the subset does not carry would
// shape to nothing and paint an invisible control.
var materialInventory = []string{
	"lock", "logout", "bedtime", "restart_alt", "power_settings_new",
	"speed", "balance", "energy_savings_leaf", "check",
	"close", "chevron_left", "chevron_right",
	"search", "settings", "notifications", "do_not_disturb_on",
	"volume_up", "volume_off", "brightness_high",
}

func TestMaterialInventoryMatchesTheSubset(t *testing.T) {
	t.Parallel()
	got := MaterialIconNames()
	if len(got) != len(materialInventory) {
		t.Fatalf("subset advertises %d names, want %d: %v", len(got), len(materialInventory), got)
	}
	for _, name := range materialInventory {
		if !ValidMaterialIcon(name) {
			t.Errorf("%q is not accepted but is in the inventory", name)
		}
	}
}

func TestMaterialRejectsUnknownNames(t *testing.T) {
	t.Parallel()
	r := NewTextRenderer(mustTestFace(t))
	for _, name := range []string{"", "rocket", "Lock", "chevron-left", "power_settings"} {
		if ValidMaterialIcon(name) {
			t.Errorf("ValidMaterialIcon(%q) = true, want false", name)
		}
		if _, err := r.RasterMaterialIcon(name, 20); err == nil {
			t.Errorf("RasterMaterialIcon(%q) succeeded, want an error naming the subset", name)
		} else if !strings.Contains(err.Error(), "material") {
			t.Errorf("error %q does not say the name is outside the subset", err)
		}
	}
}

func TestMaterialIconsShapeToCoverageAtChromeSizes(t *testing.T) {
	t.Parallel()
	// Every approved name has to reach a real glyph through the font's own
	// ligature. A name that fell back to spelled-out letters, or to nothing,
	// shows up here as the wrong coverage.
	r := NewTextRenderer(mustTestFace(t))
	for _, name := range materialInventory {
		for _, size := range []int{18, 20, 24} {
			mask, err := r.RasterMaterialIcon(name, size)
			if err != nil {
				t.Errorf("%s at %d px: %v", name, size, err)
				continue
			}
			lit := 0
			for _, a := range mask.Alpha.Pix {
				if a > 0 {
					lit++
				}
			}
			if lit == 0 {
				t.Errorf("%s at %d px rasterised nothing", name, size)
			}
			// A ligature collapses the whole name into one square glyph. If it
			// had not fired, the mask would be as wide as the spelled-out word.
			if w := mask.Advance; w > 2*size {
				t.Errorf("%s at %d px advanced %d px, wide enough to be spelled out", name, size, w)
			}
		}
	}
}

func TestKindIconUsesTheMaterialFaceNotTheBodyFace(t *testing.T) {
	t.Parallel()
	// The body face is the proof font, which has no icon glyphs. If KindIcon
	// went through it -- or through a system fallback -- "check" would either
	// paint the four letters or paint nothing.
	r := NewTextRenderer(mustTestFace(t))

	icon, err := r.RasterMaterialIcon("check", 24)
	if err != nil {
		t.Fatal(err)
	}
	body, err := r.Raster("check", TextSpec{Size: 24, Weight: 400}, false)
	if err != nil {
		t.Fatal(err)
	}
	if icon.Advance >= body.Advance {
		t.Errorf("icon advance %d is not narrower than the spelled-out %d; the ligature did not fire",
			icon.Advance, body.Advance)
	}
}

func TestPaintIconDrawsIntoItsNodeBox(t *testing.T) {
	t.Parallel()
	style := testStyle
	n := &ui.Node{Kind: ui.KindIcon, Icon: "check", IconSize: 20}
	c := paintChromeNode(t, n, style)

	if n.Bounds.W != 20 || n.Bounds.H != 20 {
		t.Fatalf("icon bounds = %+v, want 20x20", n.Bounds)
	}
	lit := 0
	for y := n.Bounds.Y; y < n.Bounds.Y+n.Bounds.H; y++ {
		for x := n.Bounds.X; x < n.Bounds.X+n.Bounds.W; x++ {
			if pixelAt(t, c, x, y) != style.Background {
				lit++
			}
		}
	}
	if lit == 0 {
		t.Error("KindIcon painted nothing inside its box")
	}
}

func TestPaintIconRejectsAnUnknownName(t *testing.T) {
	t.Parallel()
	r := NewTextRenderer(mustTestFace(t))
	c := newTestCanvas(t, 40, 40)
	n := &ui.Node{Kind: ui.KindIcon, Icon: "rocket", IconSize: 20, Bounds: ui.Rect{W: 20, H: 20}}
	if err := paintNode(c, n, r, testStyle, testStyle.Size); err == nil {
		t.Error("painting an unknown icon succeeded; it must not fail silently")
	}
}
