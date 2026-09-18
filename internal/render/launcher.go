package render

import _ "embed"

// launcherPNG is the supplied nested-gates launcher mark, copied byte-for-byte
// from the owner's artwork. It is committed rather than generated during the
// build; SOURCE.md records the source path, hash, and copy step.
//
// The asset keeps its transparent margin: the visible mark is smaller than its
// 1024x1024 canvas, and callers size the box, not the mark.
//
//go:embed icons/launcher/sysc-aperture.png
var launcherPNG []byte

// LauncherPNG returns the embedded launcher mark bytes. Callers decode them
// through the bounded PNG path so the raster work stays off the Wayland owner.
func LauncherPNG() []byte {
	return launcherPNG
}
