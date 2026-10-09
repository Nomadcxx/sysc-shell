package shell

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The asset scenes paint the shell's real panels from fixture state, at the
// laptop's output (1536x864 logical, 1.25 scale), over a wallpaper that the
// shell's own blur softens the way the compositor would. They write files only
// when SYSC_ASSETS_DIR names a directory; scripts/capture-assets sets it.
const (
	assetsRevEnv      = "SYSC_ASSETS_REV"
	assetsDirEnv      = "SYSC_ASSETS_DIR"
	assetsBackdropEnv = "SYSC_ASSETS_BACKDROP"

	assetOutW     = 1536
	assetOutH     = 864
	assetScale120 = 150 // 125%
	// assetPad is the logical margin of backdrop kept around a panel so the
	// glass edge reads against the wallpaper.
	assetPad = 24
)

// assetsDir returns the output directory, or skips the test.
func assetsDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv(assetsDirEnv)
	if dir == "" {
		t.Skip("set " + assetsDirEnv + " to write documentation assets")
	}
	return dir
}

// assetBackdrop is the wallpaper behind every fixture panel: the image named by
// SYSC_ASSETS_BACKDROP scaled to cover the output, or a deterministic gradient
// when none is given.
func assetBackdrop(t *testing.T) *image.NRGBA {
	t.Helper()
	s := ui.Scale120(assetScale120)
	w, h := s.Physical(assetOutW), s.Physical(assetOutH)
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	if path := os.Getenv(assetsBackdropEnv); path != "" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		src, _, err := image.Decode(f)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		coverScale(out, src)
		return out
	}
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(40 + 90*fx*fy), G: uint8(60 + 110*(1-fy)*fx + 30*fy),
				B: uint8(110 + 120*(1-fx)), A: 255,
			})
		}
	}
	return out
}

// coverScale fills dst with src scaled to cover it, centred, nearest-neighbour
// (a wallpaper is background; the blur hides the resampling).
func coverScale(dst *image.NRGBA, src image.Image) {
	db, sb := dst.Bounds(), src.Bounds()
	sx, sy := float64(sb.Dx())/float64(db.Dx()), float64(sb.Dy())/float64(db.Dy())
	f := min(sx, sy)
	offX := (float64(sb.Dx()) - f*float64(db.Dx())) / 2
	offY := (float64(sb.Dy()) - f*float64(db.Dy())) / 2
	for y := db.Min.Y; y < db.Max.Y; y++ {
		for x := db.Min.X; x < db.Max.X; x++ {
			dst.Set(x, y, src.At(sb.Min.X+int(offX+f*float64(x)), sb.Min.Y+int(offY+f*float64(y))))
		}
	}
}

// bgraImage converts an opaque NRGBA crop to the painter's premultiplied BGRA.
func bgraImage(src *image.NRGBA) *ui.Image {
	b := src.Bounds()
	img := &ui.Image{Width: b.Dx(), Height: b.Dy(), Stride: b.Dx() * 4, Pix: make([]byte, b.Dx()*b.Dy()*4)}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := src.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			o := y*img.Stride + x*4
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c.B, c.G, c.R, 0xff
		}
	}
	return img
}

// captureAssetPanel opens panel id on a fresh registry, lets setup put fixture
// state on its host, paints it over the blurred backdrop and writes
// <dir>/shell/<surface>/<name>.png. It returns the path.
func captureAssetPanel(t *testing.T, id PanelID, surface, name string, setup func(reg *Registry, h *PanelHost)) string {
	t.Helper()
	assetsDir(t)
	reg := newPanelRegistry(t)
	keepInvalidationsDrained(t, reg)
	if err := reg.OpenPanel(id, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: assetOutW, OutH: assetOutH}); err != nil {
		t.Fatal(err)
	}
	panel := drainAux(t, reg, 2)[1].Open
	h := reg.panelHosts[id]
	settleHostAnimation(reg, h)

	reg.mu.Lock()
	if setup != nil {
		setup(reg, h)
	}
	reg.mu.Unlock()
	return paintAssetPanel(t, reg, id, panel, surface, name)
}

// paintAssetPanel renders an already open panel at the asset scale over the
// blurred backdrop and writes <dir>/shell/<surface>/<name>.png.
func paintAssetPanel(t *testing.T, reg *Registry, id PanelID, panel *wayland.AuxSpec, surface, name string) string {
	t.Helper()
	dir := assetsDir(t)
	h := reg.panelHosts[id]
	reg.mu.Lock()
	reg.rebuildPanel(h)
	region := h.place.Rect()
	radius := reg.cfg.Theme.BlurRadius
	reg.mu.Unlock()

	s := ui.Scale120(assetScale120)
	w, hgt := int(panel.Width), int(panel.Height)
	if err := panel.Callbacks.Configure(w, hgt, assetScale120); err != nil {
		t.Fatal(err)
	}
	wall := assetBackdrop(t)
	crop := image.Rect(s.Physical(region.X), s.Physical(region.Y), s.Physical(region.X+region.W), s.Physical(region.Y+region.H)).Intersect(wall.Bounds())
	if blurred := render.Blur(bgraImage(wall.SubImage(crop).(*image.NRGBA)), 4, radius); blurred != nil {
		panel.Callbacks.Backdrop(blurred)
	}
	pw, ph := s.Physical(w), s.Physical(hgt)
	pix := make([]byte, pw*ph*4)
	if err := panel.Callbacks.Render(pix, pw, ph, pw*4); err != nil {
		t.Fatal(err)
	}

	// The picture is the surface plus a margin of wallpaper, so the panel's
	// glass edge reads against what is behind it.
	pad := s.Physical(assetPad)
	frame := image.Rect(crop.Min.X-pad, crop.Min.Y-pad, crop.Min.X+pw+pad, crop.Min.Y+ph+pad).Intersect(wall.Bounds())
	out := image.NewNRGBA(image.Rect(0, 0, frame.Dx(), frame.Dy()))
	draw.Draw(out, out.Bounds(), wall, frame.Min, draw.Src)
	over(out, pix, pw, ph, crop.Min.X-frame.Min.X, crop.Min.Y-frame.Min.Y)

	path := filepath.Join(dir, "shell", surface, name+".png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		t.Fatal(err)
	}
	writeAssetRow(t, dir, assetRow{
		Path: filepath.ToSlash(filepath.Join("shell", surface, name+".png")), Kind: "fixture",
		Source: "internal/shell " + t.Name(), Scale: "125%", Width: out.Bounds().Dx(), Height: out.Bounds().Dy(),
		Description: assetTitle(surface) + ": " + strings.ReplaceAll(name, "-", " "),
	})
	return path
}

// assetRow is one line of the assets manifest. The scenes write one file per
// image under <dir>/.manifest.d; scripts/capture-assets merges them into
// manifest.json beside the images.
type assetRow struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	Rev         string `json:"rev"`
	Scale       string `json:"scale"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Date        string `json:"date"`
	Description string `json:"description"`
}

func writeAssetRow(t *testing.T, dir string, row assetRow) {
	t.Helper()
	row.Rev = os.Getenv(assetsRevEnv)
	row.Date = time.Now().UTC().Format("2006-01-02")
	data, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	rows := filepath.Join(dir, ".manifest.d")
	if err := os.MkdirAll(rows, 0o755); err != nil {
		t.Fatal(err)
	}
	file := strings.NewReplacer("/", "__", ".png", ".json").Replace(row.Path)
	if err := os.WriteFile(filepath.Join(rows, file), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assetTitle is a surface directory name as words.
func assetTitle(surface string) string {
	words := strings.ReplaceAll(surface, "-", " ")
	return strings.ToUpper(words[:1]) + words[1:]
}

// over composites premultiplied BGRA pixels onto an opaque NRGBA at (ox, oy).
func over(dst *image.NRGBA, pix []byte, w, h, ox, oy int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			a := int(pix[i+3])
			if a == 0 || !image.Pt(ox+x, oy+y).In(dst.Bounds()) {
				continue
			}
			d := dst.NRGBAAt(ox+x, oy+y)
			dst.SetNRGBA(ox+x, oy+y, color.NRGBA{
				R: uint8(int(pix[i+2]) + int(d.R)*(255-a)/255),
				G: uint8(int(pix[i+1]) + int(d.G)*(255-a)/255),
				B: uint8(int(pix[i]) + int(d.B)*(255-a)/255),
				A: 255,
			})
		}
	}
}

// assetSlug is a section name as a file name.
func assetSlug(name string) string { return strings.ReplaceAll(strings.ToLower(name), " ", "-") }

func contains(list []string, want string) bool { return slices.Contains(list, want) }
