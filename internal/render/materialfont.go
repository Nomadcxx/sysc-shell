package render

import (
	_ "embed"
	"fmt"
	"sort"

	"github.com/go-text/typesetting/font"
)

// materialTTF is a static subset of Material Symbols Rounded, cut at author
// time by icons/material/build.py. It is committed rather than fetched or
// generated during the build; SOURCE.md records the pinned upstream commit,
// hashes, instanced axes, and the Apache-2.0 licence.
//
//go:embed icons/material/material-symbols-rounded.ttf
var materialTTF []byte

// materialIcons is the exact inventory the subset carries. It mirrors ICONS in
// build.py: a name the font does not hold would shape to nothing and paint an
// invisible control, so the two lists are kept in step by hand and asserted by
// test rather than discovered at runtime.
var materialIcons = map[string]struct{}{
	"lock": {}, "logout": {}, "bedtime": {}, "restart_alt": {}, "power_settings_new": {},
	"speed": {}, "balance": {}, "energy_savings_leaf": {}, "check": {},
	"close": {}, "chevron_left": {}, "chevron_right": {},
	"search": {}, "settings": {}, "notifications": {}, "do_not_disturb_on": {},
	"volume_up": {}, "volume_off": {}, "brightness_high": {},
}

// ValidMaterialIcon reports whether name is one the embedded subset can draw.
// Composition code calls this so an unknown name is a build-time or test-time
// error rather than a control that silently paints nothing.
func ValidMaterialIcon(name string) bool {
	_, ok := materialIcons[name]
	return ok
}

// MaterialIconNames lists the inventory in sorted order, for tests and errors.
func MaterialIconNames() []string {
	out := make([]string, 0, len(materialIcons))
	for name := range materialIcons {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// materialFace returns this renderer's own face for the subset, parsing it on
// first use. ParseFace caches the parsed *font.Font and hands back a fresh
// face, which is the rule a face carries mutable caches and must not be shared
// across goroutines while the read-only font may be.
func (r *TextRenderer) materialFace() (*font.Face, error) {
	if r == nil {
		return nil, fmt.Errorf("render: nil renderer")
	}
	if r.material != nil {
		return r.material, nil
	}
	if r.materialErr != nil {
		return nil, r.materialErr
	}
	face, err := ParseFace(materialTTF)
	if err != nil {
		r.materialErr = fmt.Errorf("render: parse material subset: %w", err)
		return nil, r.materialErr
	}
	r.material = face
	return face, nil
}

// RasterMaterialIcon shapes one icon name through the embedded subset and
// rasterises it. The name is shaped as text so the font's own ligature turns
// "chevron_left" into the glyph; it is never routed through the system font
// map, which would spell the name out in the body face instead.
func (r *TextRenderer) RasterMaterialIcon(name string, size int) (Mask, error) {
	if !ValidMaterialIcon(name) {
		return Mask{}, fmt.Errorf("render: %q is not in the material icon subset", name)
	}
	face, err := r.materialFace()
	if err != nil {
		return Mask{}, err
	}
	out, err := r.shapeFace(face, name, size, false)
	if err != nil {
		return Mask{}, err
	}
	return rasterRuns([]shapedFaceRun{{face: face, text: name, output: out}}, size)
}
