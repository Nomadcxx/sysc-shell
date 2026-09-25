package wallpaper

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeDepthTestImage(t *testing.T, path, format string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var encodeErr error
	switch format {
	case "png":
		encodeErr = png.Encode(f, img)
	case "jpeg":
		encodeErr = jpeg.Encode(f, img, nil)
	case "gif":
		encodeErr = gif.Encode(f, img, nil)
	default:
		t.Fatalf("unknown test format %q", format)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
}

func writeDepthTestMask(t *testing.T, path string, mask *image.Gray) {
	t.Helper()
	writeDepthTestImage(t, path, "png", mask)
}

func testDepthMaskImage(width, height int, values []uint8) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for i, value := range values {
		img.Pix[i] = value
	}
	return img
}

func TestLoadDepthMaskReturnsPackedAlpha(t *testing.T) {
	dir := t.TempDir()
	maskPath, wallpaperPath := filepath.Join(dir, "mask.png"), filepath.Join(dir, "wall.png")
	values := []uint8{0, 64, 128, 192, 255, 17}
	writeDepthTestMask(t, maskPath, testDepthMaskImage(3, 2, values))
	writeDepthTestImage(t, wallpaperPath, "png", image.NewRGBA(image.Rect(0, 0, 3, 2)))

	got, err := LoadDepthMask(maskPath, wallpaperPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(got).(*image.Alpha); !ok {
		t.Fatalf("LoadDepthMask returned %T, want *image.Alpha", got)
	}
	if got.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatalf("mask bounds = %v", got.Bounds())
	}
	for i, want := range values {
		if got.Pix[i] != want {
			t.Fatalf("mask pixel %d = %d, want %d", i, got.Pix[i], want)
		}
	}
}

func TestLoadDepthMaskRejectsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	wallpaperPath := filepath.Join(dir, "wall.png")
	writeDepthTestImage(t, wallpaperPath, "png", image.NewRGBA(image.Rect(0, 0, 2, 2)))
	validMask := filepath.Join(dir, "valid.png")
	writeDepthTestMask(t, validMask, testDepthMaskImage(2, 2, []uint8{0, 1, 2, 3}))
	largeHeader := filepath.Join(dir, "large.png")
	if err := os.WriteFile(largeHeader, pngIHDR(8193, 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	textFile := filepath.Join(dir, "not-png.png")
	if err := os.WriteFile(textFile, []byte("not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(dir, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, maskPath string
		wallpaperPath  string
	}{
		{name: "mismatched dimensions", maskPath: validMask, wallpaperPath: func() string {
			path := filepath.Join(dir, "other-size.png")
			writeDepthTestImage(t, path, "png", image.NewRGBA(image.Rect(0, 0, 3, 2)))
			return path
		}()},
		{name: "oversized header", maskPath: largeHeader, wallpaperPath: wallpaperPath},
		{name: "non-PNG", maskPath: textFile, wallpaperPath: wallpaperPath},
		{name: "missing", maskPath: filepath.Join(dir, "missing.png"), wallpaperPath: wallpaperPath},
		{name: "non-regular", maskPath: directory, wallpaperPath: wallpaperPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := LoadDepthMask(tc.maskPath, tc.wallpaperPath); err == nil {
				t.Fatal("LoadDepthMask unexpectedly succeeded")
			}
		})
	}
}

func TestLoadDepthMaskFileLimit(t *testing.T) {
	if MaxDepthMaskFileBytes != 64<<20 || MaxDepthMaskPixels != 64<<20 {
		t.Fatalf("depth-mask limits = %d bytes, %d pixels", MaxDepthMaskFileBytes, MaxDepthMaskPixels)
	}
	dir := t.TempDir()
	maskPath := filepath.Join(dir, "oversized.png")
	f, err := os.Create(maskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxDepthMaskFileBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	wallpaperPath := filepath.Join(dir, "wall.png")
	writeDepthTestImage(t, wallpaperPath, "png", image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if _, err := LoadDepthMask(maskPath, wallpaperPath); err == nil {
		t.Fatal("oversized mask file was accepted")
	}
}

func TestLoadDepthMaskWallpaperHeaders(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	for _, format := range []string{"jpeg", "png", "gif"} {
		t.Run(format, func(t *testing.T) {
			wallpaperPath := filepath.Join(dir, format+".image")
			maskPath := filepath.Join(dir, format+".png")
			writeDepthTestImage(t, wallpaperPath, format, img)
			writeDepthTestMask(t, maskPath, testDepthMaskImage(2, 3, make([]uint8, 6)))
			if _, err := LoadDepthMask(maskPath, wallpaperPath); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("webp", func(t *testing.T) {
		const fixture = "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEAAUAmJaQAA3AA/v89WAAAAA=="
		data, err := base64.StdEncoding.DecodeString(fixture)
		if err != nil {
			t.Fatal(err)
		}
		wallpaperPath, maskPath := filepath.Join(dir, "webp.image"), filepath.Join(dir, "webp.png")
		if err := os.WriteFile(wallpaperPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		writeDepthTestMask(t, maskPath, testDepthMaskImage(1, 1, []uint8{128}))
		if _, err := LoadDepthMask(maskPath, wallpaperPath); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDepthMaskWallpaperGeometry(t *testing.T) {
	cases := []struct {
		name         string
		g            DepthGeometry
		x, y         int
		wantX, wantY float64
		inside       bool
	}{
		{
			name: "fill center crops landscape",
			g:    DepthGeometry{Mode: "fill", Scale120: 120, SurfaceWidth: 2, SurfaceHeight: 2, OutputWidth: 2, OutputHeight: 2, ImageWidth: 4, ImageHeight: 2},
			x:    0, y: 0, wantX: 1, wantY: 0, inside: true,
		},
		{
			name: "stretch maps full source",
			g:    DepthGeometry{Mode: "stretch", Scale120: 120, SurfaceWidth: 2, SurfaceHeight: 2, OutputWidth: 2, OutputHeight: 2, ImageWidth: 4, ImageHeight: 2},
			x:    0, y: 0, wantX: 0.5, wantY: 0, inside: true,
		},
		{
			name: "original centers native pixels",
			g:    DepthGeometry{Mode: "original", Scale120: 120, SurfaceWidth: 6, SurfaceHeight: 4, OutputWidth: 6, OutputHeight: 4, ImageWidth: 4, ImageHeight: 2},
			x:    1, y: 1, wantX: 0, wantY: 0, inside: true,
		},
		{
			name: "original leaves outside uncovered",
			g:    DepthGeometry{Mode: "original", Scale120: 120, SurfaceWidth: 6, SurfaceHeight: 4, OutputWidth: 6, OutputHeight: 4, ImageWidth: 4, ImageHeight: 2},
			x:    0, y: 0, inside: false,
		},
		{
			name: "panscan 1 fits inside",
			g:    DepthGeometry{Mode: "panscan", Scale120: 120, SurfaceWidth: 4, SurfaceHeight: 4, OutputWidth: 4, OutputHeight: 4, ImageWidth: 4, ImageHeight: 2},
			x:    0, y: 1, wantX: 0, wantY: 0, inside: true,
		},
		{
			name: "panscan 1 leaves letterbox uncovered",
			g:    DepthGeometry{Mode: "panscan", Scale120: 120, SurfaceWidth: 4, SurfaceHeight: 4, OutputWidth: 4, OutputHeight: 4, ImageWidth: 4, ImageHeight: 2},
			x:    0, y: 0, inside: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y, inside := depthMaskSource(tc.g, tc.x, tc.y)
			if inside != tc.inside || (inside && (x != tc.wantX || y != tc.wantY)) {
				t.Fatalf("source = (%v, %v, inside %v), want (%v, %v, inside %v)", x, y, inside, tc.wantX, tc.wantY, tc.inside)
			}
		})
	}
}

func TestDepthMaskGeometryAtFractionalScales(t *testing.T) {
	for _, tc := range []struct {
		scale                  int
		landscapeX, landscapeY float64
		portraitX, portraitY   float64
	}{
		{scale: 120, landscapeX: 3.75, landscapeY: 1.75, portraitX: 1.75, portraitY: 3.75},
		{scale: 150, landscapeX: 3.8, landscapeY: 1.8, portraitX: 1.8, portraitY: 3.8},
		{scale: 240, landscapeX: 3.625, landscapeY: 1.625, portraitX: 1.625, portraitY: 3.625},
	} {
		for _, source := range []struct {
			name         string
			w, h         int
			wantX, wantY float64
		}{
			{name: "landscape", w: 8, h: 4, wantX: tc.landscapeX, wantY: tc.landscapeY},
			{name: "portrait", w: 4, h: 8, wantX: tc.portraitX, wantY: tc.portraitY},
		} {
			t.Run(source.name+"/"+strconv.Itoa(tc.scale), func(t *testing.T) {
				g := DepthGeometry{Mode: "fill", Scale120: tc.scale, SurfaceX: 1, SurfaceY: 1, SurfaceWidth: 6, SurfaceHeight: 6, OutputWidth: 8, OutputHeight: 8, ImageWidth: source.w, ImageHeight: source.h}
				px := uiScalePhysical(tc.scale, 3)
				py := uiScalePhysical(tc.scale, 3)
				sx, sy, inside := depthMaskSource(g, px, py)
				if !inside || math.Abs(sx-source.wantX) > 1e-9 || math.Abs(sy-source.wantY) > 1e-9 {
					t.Fatalf("mapped (%v, %v, inside %v), want (%v, %v, inside true)", sx, sy, inside, source.wantX, source.wantY)
				}
			})
		}
	}
}

func uiScalePhysical(scale, logical int) int {
	return (logical*scale + 60) / 120
}

func TestApplyDepthMaskScalesPremultipliedBGRA(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 3, 1))
	copy(mask.Pix, []byte{0, 128, 255})
	pix := []byte{
		10, 20, 30, 40,
		10, 20, 30, 40,
		10, 20, 30, 40,
	}
	g := DepthGeometry{Mode: "stretch", Scale120: 120, SurfaceWidth: 3, SurfaceHeight: 1, OutputWidth: 3, OutputHeight: 1, ImageWidth: 3, ImageHeight: 1}
	if err := ApplyDepthMask(pix, 3, 1, 12, mask, g); err != nil {
		t.Fatal(err)
	}
	want := []byte{10, 20, 30, 40, 4, 9, 14, 19, 0, 0, 0, 0}
	if !bytes.Equal(pix, want) {
		t.Fatalf("masked pixels = %v, want %v", pix, want)
	}
}

func TestApplyDepthMaskUsesBilinearCoverage(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 2, 1))
	copy(mask.Pix, []byte{0, 255})
	pix := bytes.Repeat([]byte{255, 255, 255, 255}, 4)
	g := DepthGeometry{Mode: "stretch", Scale120: 120, SurfaceWidth: 4, SurfaceHeight: 1, OutputWidth: 4, OutputHeight: 1, ImageWidth: 2, ImageHeight: 1}
	if err := ApplyDepthMask(pix, 4, 1, 16, mask, g); err != nil {
		t.Fatal(err)
	}
	want := []uint8{255, 191, 64, 0}
	for x, alpha := range want {
		got := pix[x*4+3]
		if got != alpha {
			t.Fatalf("alpha at x=%d = %d, want %d", x, got, alpha)
		}
	}
}

func TestDepthMaskSamplingHonorsAlphaImageBounds(t *testing.T) {
	mask := &image.Alpha{Pix: []byte{0, 128, 255}, Stride: 3, Rect: image.Rect(5, 7, 8, 8)}
	if got := sampleDepthMask(mask, 1, 0); got != 128 {
		t.Fatalf("sample at image-relative (1,0) = %d, want 128", got)
	}
}

func TestApplyDepthMaskLeavesOriginalModeOutsideSource(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 4, 2))
	for i := range mask.Pix {
		mask.Pix[i] = 255
	}
	pix := bytes.Repeat([]byte{10, 20, 30, 40}, 6*4)
	g := DepthGeometry{Mode: "original", Scale120: 120, SurfaceWidth: 6, SurfaceHeight: 4, OutputWidth: 6, OutputHeight: 4, ImageWidth: 4, ImageHeight: 2}
	if err := ApplyDepthMask(pix, 6, 4, 24, mask, g); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pix[:4], []byte{10, 20, 30, 40}) {
		t.Fatalf("outside source pixel = %v, want unchanged", pix[:4])
	}
	if !bytes.Equal(pix[(1*6+1)*4:(1*6+1)*4+4], []byte{0, 0, 0, 0}) {
		t.Fatalf("inside source pixel = %v, want cleared", pix[(1*6+1)*4:(1*6+1)*4+4])
	}
}

func TestApplyDepthMaskRejectsInvalidBufferAndGeometry(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 1, 1))
	good := DepthGeometry{Mode: "stretch", Scale120: 120, SurfaceWidth: 1, SurfaceHeight: 1, OutputWidth: 1, OutputHeight: 1, ImageWidth: 1, ImageHeight: 1}
	for _, tc := range []struct {
		name                  string
		pix                   []byte
		width, height, stride int
		g                     DepthGeometry
	}{
		{name: "short buffer", pix: make([]byte, 3), width: 1, height: 1, stride: 4, g: good},
		{name: "short stride", pix: make([]byte, 4), width: 1, height: 1, stride: 3, g: good},
		{name: "unknown mode", pix: make([]byte, 4), width: 1, height: 1, stride: 4, g: DepthGeometry{Mode: "contain", Scale120: 120, SurfaceWidth: 1, SurfaceHeight: 1, OutputWidth: 1, OutputHeight: 1, ImageWidth: 1, ImageHeight: 1}},
		{name: "mask dimensions", pix: make([]byte, 4), width: 1, height: 1, stride: 4, g: DepthGeometry{Mode: "stretch", Scale120: 120, SurfaceWidth: 1, SurfaceHeight: 1, OutputWidth: 1, OutputHeight: 1, ImageWidth: 2, ImageHeight: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ApplyDepthMask(tc.pix, tc.width, tc.height, tc.stride, mask, tc.g); err == nil {
				t.Fatal("ApplyDepthMask unexpectedly succeeded")
			}
		})
	}
}

func pngIHDR(width, height int) []byte {
	data := make([]byte, 8+4+4+13+4)
	copy(data, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], uint32(width))
	binary.BigEndian.PutUint32(data[20:24], uint32(height))
	data[24], data[25] = 8, 0
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}
