package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// trayIconImage is the composed raster a tray node draws.
type trayIconImage struct {
	Width, Height int
	pix           []byte // premultiplied ARGB, row-major
}

// PixAt reads one premultiplied ARGB pixel.
func (m *trayIconImage) PixAt(x, y int) [4]byte {
	if m == nil || x < 0 || y < 0 || x >= m.Width || y >= m.Height {
		return [4]byte{}
	}
	i := (y*m.Width + x) * 4
	return [4]byte{m.pix[i], m.pix[i+1], m.pix[i+2], m.pix[i+3]}
}

// validPixmap reports whether the buffer matches its declared geometry.
func validPixmap(p *tray.Pixmap) bool {
	return p != nil && p.Width > 0 && p.Height > 0 &&
		int(p.Width*p.Height*4) == len(p.ARGB)
}

// composeTrayIcon overlays the overlay icon onto the base at half size in the
// bottom-right quadrant. A malformed base composes nothing; a malformed or
// absent overlay leaves the base alone. Only the overlay icon's footprint is
// touched, so a malformed candidate never disturbs the rest of the icon.
func composeTrayIcon(base, overlay *tray.Pixmap) *trayIconImage {
	if !validPixmap(base) {
		return nil
	}
	out := &trayIconImage{
		Width: int(base.Width), Height: int(base.Height),
		pix: append([]byte(nil), base.ARGB...),
	}
	if !validPixmap(overlay) {
		return out
	}
	half := out.Width / 2
	if half <= 0 {
		return out
	}
	ox := out.Width - half
	oy := out.Height - half
	for y := 0; y < half; y++ {
		for x := 0; x < half; x++ {
			// Nearest-neighbour: the overlay is already at its served size.
			sx := int(overlay.Width) * x / half
			sy := int(overlay.Height) * y / half
			si := (sy*int(overlay.Width) + sx) * 4
			di := ((oy+y)*out.Width + (ox + x)) * 4
			copy(out.pix[di:di+4], overlay.ARGB[si:si+4])
		}
	}
	return out
}

func effectiveTrayIcon(item tray.Item) tray.Icon {
	if item.Status == tray.StatusNeedsAttention &&
		(item.AttentionIcon.Name != "" || len(item.AttentionIcon.Pixmaps) > 0) {
		return item.AttentionIcon
	}
	return item.Icon
}

// trayNamedIconKey keys the decode by the raster size the output needs, so an
// output at another scale gets its own decode rather than an upscaled copy.
func trayNamedIconKey(item tray.Item, size int) (icons.Key, bool) {
	base := effectiveTrayIcon(item)
	if base.Name == "" || size <= 0 {
		return icons.Key{}, false
	}
	return icons.Key{Name: base.Name, Overlay: item.OverlayIcon.Name, W: size, H: size}, true
}

func trayPixmapImage(item tray.Item, size int) *ui.Image {
	if size <= 0 {
		return nil
	}
	baseIcon := effectiveTrayIcon(item)
	base := bestTrayPixmap(baseIcon.Pixmaps, size)
	if base == nil {
		return nil
	}
	overlay := bestTrayPixmap(item.OverlayIcon.Pixmaps, max(1, size/2))
	composed := composeTrayIcon(base, overlay)
	if composed == nil {
		return nil
	}
	out := &ui.Image{Width: composed.Width, Height: composed.Height, Stride: composed.Width * 4,
		Pix: make([]byte, len(composed.pix))}
	for i := 0; i+3 < len(composed.pix); i += 4 {
		// SNI pixmaps are byte-order ARGB; the canvas is memory-order BGRA.
		out.Pix[i+0] = composed.pix[i+3]
		out.Pix[i+1] = composed.pix[i+2]
		out.Pix[i+2] = composed.pix[i+1]
		out.Pix[i+3] = composed.pix[i+0]
	}
	return out
}

func bestTrayPixmap(pixmaps []tray.Pixmap, size int) *tray.Pixmap {
	best := -1
	for i := range pixmaps {
		if !validPixmap(&pixmaps[i]) {
			continue
		}
		if best < 0 || betterTrayPixmap(int(pixmaps[i].Width), int(pixmaps[best].Width), size) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return &pixmaps[best]
}

func betterTrayPixmap(a, b, want int) bool {
	if a == want || b == want {
		return a == want
	}
	aBig, bBig := a >= want, b >= want
	if aBig != bBig {
		return aBig
	}
	if aBig {
		return a < b
	}
	return a > b
}
