package shell

import tray "github.com/Nomadcxx/sysc-tray/protocol"

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
