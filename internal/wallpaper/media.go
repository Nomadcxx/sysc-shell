// Package wallpaper owns the wallpaper library, the per-connector assignments,
// and the engines that put an image or a video on an output. Nothing in this
// package touches Wayland: the shell submits commands and reads back immutable
// snapshots, the same shape the notify and tray clients already use.
package wallpaper

import (
	"path/filepath"
	"strings"
)

// Kind classifies one library entry. The zero value is meaningful: a file
// whose extension is not in the vocabulary is not a wallpaper at all, and the
// index drops it rather than offering a tile that cannot be applied.
type Kind uint8

const (
	KindUnknown Kind = iota
	KindImage
	KindVideo
)

// extensionKinds is the whole vocabulary. Anything absent is KindUnknown.
//
// GIF is a video. gSlapper lists it under its *image* formats, but an animated
// GIF on the still path is a frozen first frame, so the library routes it to
// the engine that plays it. What the active strip then offers for a GIF
// follows the kind gSlapper reports at query time rather than this table, so a
// file never shows a control its pipeline cannot honour.
var extensionKinds = map[string]Kind{
	".jpg":  KindImage,
	".jpeg": KindImage,
	".png":  KindImage,
	".webp": KindImage,
	".jxl":  KindImage,
	".bmp":  KindImage,

	".gif":  KindVideo,
	".mp4":  KindVideo,
	".mkv":  KindVideo,
	".webm": KindVideo,
	".mov":  KindVideo,
	".avi":  KindVideo,
	".m4v":  KindVideo,
}

// ClassifyName maps a filename to its kind by extension, case-folded so a
// camera's .JPEG classifies like a .jpg.
func ClassifyName(name string) Kind {
	return extensionKinds[strings.ToLower(filepath.Ext(name))]
}

// connectorByteAllowed reports whether a byte may appear in a socket filename.
func connectorByteAllowed(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '.', c == '_', c == '-':
		return true
	}
	return false
}

// SanitizeConnector reduces a connector name to [A-Za-z0-9._-] so it can name a
// socket file, collapsing each run of rejected bytes into a single dash.
//
// The name arrives from the compositor rather than from us, so it is reduced
// before it reaches a path: a connector carrying a separator or a shell
// metacharacter must not be able to steer where we bind, and the collapse
// keeps the result readable in `pgrep -af gslapper`, which is how the live
// gate proves we only ever stop sockets we own.
func SanitizeConnector(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	dashed := false
	for i := 0; i < len(name); i++ {
		if c := name[i]; connectorByteAllowed(c) {
			b.WriteByte(c)
			dashed = false
			continue
		}
		if !dashed {
			b.WriteByte('-')
			dashed = true
		}
	}
	return b.String()
}

// socketPath names the gSlapper control socket for one connector inside dir.
// One process per output means one socket per output (D13/D14): the path is
// the only handle we use to stop an instance, so it must never be shared.
func socketPath(dir, connector string) string {
	return filepath.Join(dir, "gslapper-"+SanitizeConnector(connector)+".sock")
}
