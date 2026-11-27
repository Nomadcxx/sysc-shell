package wallpaper

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"sync"
	"syscall"
	"time"

	_ "golang.org/x/image/webp"
)

const (
	MaxDepthMaskFileBytes int64 = 64 << 20
	MaxDepthMaskPixels    int64 = 64 << 20
	// MaxDepthMaskDecodeBytes bounds the decoded images that concurrent
	// mask loads may hold at once. A full-size mask reserves 5 bytes per
	// pixel plus its encoded size, so large masks are decoded one at a
	// time instead of multiplying into gigabytes.
	MaxDepthMaskDecodeBytes int64 = 512 << 20
	depthMaskHeaderBytes    int64 = 1 << 20
	depthMaskBudgetPoll           = 25 * time.Millisecond
)

// depthDecodeBudget is a weighted gate for decoded mask memory. Waiters poll
// so a cancelled host call does not stay queued behind a large decode.
type depthDecodeBudget struct {
	mu       sync.Mutex
	used     int64
	capacity int64
}

func newDepthDecodeBudget(capacity int64) *depthDecodeBudget {
	return &depthDecodeBudget{capacity: capacity}
}

func (b *depthDecodeBudget) acquire(ctx context.Context, n int64) error {
	if n <= 0 {
		n = 1
	}
	if n > b.capacity {
		return fmt.Errorf("reservation %d exceeds %d-byte budget", n, b.capacity)
	}
	for {
		b.mu.Lock()
		if b.used <= b.capacity-n {
			b.used += n
			b.mu.Unlock()
			return nil
		}
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(depthMaskBudgetPoll):
		}
	}
}

func (b *depthDecodeBudget) release(n int64) {
	b.mu.Lock()
	b.used -= n
	if b.used < 0 {
		b.used = 0
	}
	b.mu.Unlock()
}

var depthMaskDecodeBudget = newDepthDecodeBudget(MaxDepthMaskDecodeBytes)

func depthMaskDecodeReservation(width, height int, fileBytes int64) int64 {
	if width <= 0 || height <= 0 {
		return 1
	}
	return int64(width)*int64(height)*5 + fileBytes
}

// DepthGeometry describes the logical placement of a clock surface and the
// wallpaper transform it must follow. Surface and output dimensions and
// offsets are logical pixels; ImageWidth and ImageHeight are source pixels.
type DepthGeometry struct {
	Mode                        string
	Scale120                    int
	SurfaceX, SurfaceY          int
	SurfaceWidth, SurfaceHeight int
	OutputWidth, OutputHeight   int
	ImageWidth, ImageHeight     int
}

// LoadDepthMask reads a bounded grayscale PNG and verifies it matches the
// selected wallpaper's decoded header dimensions.
func LoadDepthMask(maskPath, wallpaperPath string) (*image.Alpha, error) {
	return LoadDepthMaskContext(context.Background(), maskPath, wallpaperPath)
}

// LoadDepthMaskContext is LoadDepthMask with a cancellation-aware wait for
// aggregate decode memory.
func LoadDepthMaskContext(ctx context.Context, maskPath, wallpaperPath string) (*image.Alpha, error) {
	maskFile, err := openDepthFile(maskPath)
	if err != nil {
		return nil, fmt.Errorf("wallpaper: open depth mask %q: %w", maskPath, err)
	}
	defer maskFile.Close()
	info, err := maskFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("wallpaper: stat depth mask %q: %w", maskPath, err)
	}
	if info.Size() > MaxDepthMaskFileBytes {
		return nil, fmt.Errorf("wallpaper: depth mask %q exceeds %d bytes", maskPath, MaxDepthMaskFileBytes)
	}
	header, err := io.ReadAll(io.LimitReader(maskFile, depthMaskHeaderBytes))
	if err != nil {
		return nil, fmt.Errorf("wallpaper: read depth mask %q: %w", maskPath, err)
	}
	maskConfig, err := png.DecodeConfig(bytes.NewReader(header))
	if err != nil {
		return nil, fmt.Errorf("wallpaper: decode depth-mask PNG %q: %w", maskPath, err)
	}
	if err := validateDepthImageSize(maskConfig.Width, maskConfig.Height); err != nil {
		return nil, fmt.Errorf("wallpaper: depth mask %q: %w", maskPath, err)
	}
	reservation := depthMaskDecodeReservation(maskConfig.Width, maskConfig.Height, info.Size())
	if err := depthMaskDecodeBudget.acquire(ctx, reservation); err != nil {
		return nil, fmt.Errorf("wallpaper: wait to decode depth mask %q: %w", maskPath, err)
	}
	defer depthMaskDecodeBudget.release(reservation)

	remaining := MaxDepthMaskFileBytes - int64(len(header)) + 1
	rest, err := io.ReadAll(io.LimitReader(maskFile, remaining))
	if err != nil {
		return nil, fmt.Errorf("wallpaper: read depth mask %q: %w", maskPath, err)
	}
	if int64(len(header))+int64(len(rest)) > MaxDepthMaskFileBytes {
		return nil, fmt.Errorf("wallpaper: depth mask %q exceeds %d bytes", maskPath, MaxDepthMaskFileBytes)
	}
	data := make([]byte, 0, len(header)+len(rest))
	data = append(data, header...)
	data = append(data, rest...)

	wallpaperFile, err := openDepthFile(wallpaperPath)
	if err != nil {
		return nil, fmt.Errorf("wallpaper: open wallpaper %q: %w", wallpaperPath, err)
	}
	defer wallpaperFile.Close()
	wallpaperConfig, _, err := image.DecodeConfig(wallpaperFile)
	if err != nil {
		return nil, fmt.Errorf("wallpaper: decode wallpaper header %q: %w", wallpaperPath, err)
	}
	if err := validateDepthImageSize(wallpaperConfig.Width, wallpaperConfig.Height); err != nil {
		return nil, fmt.Errorf("wallpaper: wallpaper %q: %w", wallpaperPath, err)
	}
	if maskConfig.Width != wallpaperConfig.Width || maskConfig.Height != wallpaperConfig.Height {
		return nil, fmt.Errorf("wallpaper: depth mask is %dx%d, wallpaper is %dx%d", maskConfig.Width, maskConfig.Height, wallpaperConfig.Width, wallpaperConfig.Height)
	}

	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("wallpaper: decode depth-mask PNG %q: %w", maskPath, err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != maskConfig.Width || bounds.Dy() != maskConfig.Height {
		return nil, fmt.Errorf("wallpaper: decoded depth mask dimensions changed from its header")
	}
	packed := image.NewAlpha(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			packed.Pix[y*packed.Stride+x] = color.GrayModel.Convert(decoded.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.Gray).Y
		}
	}
	return packed, nil
}

// ApplyDepthMask multiplies the premultiplied BGRA channels in pix by the
// inverse bilinear mask coverage at each pixel centre.
func ApplyDepthMask(pix []byte, width, height, stride int, mask *image.Alpha, g DepthGeometry) error {
	if mask == nil {
		return fmt.Errorf("wallpaper: nil depth mask")
	}
	if err := validateDepthGeometry(g); err != nil {
		return err
	}
	if mask.Bounds().Dx() != g.ImageWidth || mask.Bounds().Dy() != g.ImageHeight {
		return fmt.Errorf("wallpaper: depth mask is %dx%d, geometry image is %dx%d", mask.Bounds().Dx(), mask.Bounds().Dy(), g.ImageWidth, g.ImageHeight)
	}
	maxInt := int(^uint(0) >> 1)
	if width <= 0 || height <= 0 || width > maxInt/4 {
		return fmt.Errorf("wallpaper: invalid depth-mask buffer dimensions %dx%d", width, height)
	}
	rowBytes := width * 4
	if stride < rowBytes || height > maxInt/stride || len(pix) < stride*height {
		return fmt.Errorf("wallpaper: depth-mask buffer has invalid stride or length")
	}

	// ponytail: this is a CPU pass per visible pixel; compact clock surfaces keep it small, and measured material frame time is the trigger to move it into the renderer backend.
	for y := 0; y < height; y++ {
		row := y * stride
		for x := 0; x < width; x++ {
			sx, sy, inside := depthMaskSource(g, x, y)
			if !inside {
				continue
			}
			coverage := sampleDepthMask(mask, sx, sy)
			keep := 255 - int(coverage)
			pixel := row + x*4
			for channel := range 4 {
				pix[pixel+channel] = byte(int(pix[pixel+channel]) * keep / 255)
			}
		}
	}
	return nil
}

func depthMaskSource(g DepthGeometry, pixelX, pixelY int) (x, y float64, inside bool) {
	scale := float64(g.Scale120) / 120
	localX := (float64(pixelX) + 0.5) / scale
	localY := (float64(pixelY) + 0.5) / scale
	if localX < 0 || localY < 0 || localX >= float64(g.SurfaceWidth) || localY >= float64(g.SurfaceHeight) {
		return 0, 0, false
	}
	outX := float64(g.SurfaceX) + localX
	outY := float64(g.SurfaceY) + localY
	if outX < 0 || outY < 0 || outX >= float64(g.OutputWidth) || outY >= float64(g.OutputHeight) {
		return 0, 0, false
	}

	switch g.Mode {
	case "stretch":
		x = outX*float64(g.ImageWidth)/float64(g.OutputWidth) - 0.5
		y = outY*float64(g.ImageHeight)/float64(g.OutputHeight) - 0.5
	case "fill":
		fit := max(float64(g.OutputWidth)/float64(g.ImageWidth), float64(g.OutputHeight)/float64(g.ImageHeight))
		x = (outX-float64(g.OutputWidth)/2)/fit + float64(g.ImageWidth)/2 - 0.5
		y = (outY-float64(g.OutputHeight)/2)/fit + float64(g.ImageHeight)/2 - 0.5
	case "panscan":
		fit := min(float64(g.OutputWidth)/float64(g.ImageWidth), float64(g.OutputHeight)/float64(g.ImageHeight))
		edgeX := (outX-float64(g.OutputWidth)/2)/fit + float64(g.ImageWidth)/2
		edgeY := (outY-float64(g.OutputHeight)/2)/fit + float64(g.ImageHeight)/2
		if edgeX < 0 || edgeY < 0 || edgeX >= float64(g.ImageWidth) || edgeY >= float64(g.ImageHeight) {
			return 0, 0, false
		}
		x, y = edgeX-0.5, edgeY-0.5
	case "original":
		edgeX := outX + (float64(g.ImageWidth)-float64(g.OutputWidth))/2
		edgeY := outY + (float64(g.ImageHeight)-float64(g.OutputHeight))/2
		if edgeX < 0 || edgeY < 0 || edgeX >= float64(g.ImageWidth) || edgeY >= float64(g.ImageHeight) {
			return 0, 0, false
		}
		x, y = edgeX-0.5, edgeY-0.5
	}
	return x, y, true
}

func sampleDepthMask(mask *image.Alpha, x, y float64) uint8 {
	bounds := mask.Bounds()
	x = min(max(x, 0), float64(bounds.Dx()-1))
	y = min(max(y, 0), float64(bounds.Dy()-1))
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := min(x0+1, bounds.Dx()-1), min(y0+1, bounds.Dy()-1)
	fx, fy := x-float64(x0), y-float64(y0)
	alpha := func(px, py int) float64 {
		return float64(mask.Pix[py*mask.Stride+px])
	}
	top := alpha(x0, y0)*(1-fx) + alpha(x1, y0)*fx
	bottom := alpha(x0, y1)*(1-fx) + alpha(x1, y1)*fx
	return uint8(math.Round(top*(1-fy) + bottom*fy))
}

func validateDepthGeometry(g DepthGeometry) error {
	switch g.Mode {
	case "fill", "stretch", "original", "panscan":
	default:
		return fmt.Errorf("wallpaper: unknown depth-mask scale mode %q", g.Mode)
	}
	if g.Scale120 <= 0 {
		return fmt.Errorf("wallpaper: depth-mask scale120 must be positive")
	}
	if g.SurfaceWidth <= 0 || g.SurfaceHeight <= 0 || g.OutputWidth <= 0 || g.OutputHeight <= 0 {
		return fmt.Errorf("wallpaper: depth-mask surface and output dimensions must be positive")
	}
	if err := validateDepthImageSize(g.ImageWidth, g.ImageHeight); err != nil {
		return fmt.Errorf("wallpaper: invalid depth-mask image geometry: %w", err)
	}
	return nil
}

func validateDepthImageSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("dimensions %dx%d are not positive", width, height)
	}
	if int64(width) > MaxDepthMaskPixels/int64(height) {
		return fmt.Errorf("dimensions %dx%d exceed %d pixels", width, height, MaxDepthMaskPixels)
	}
	return nil
}

func openDepthFile(path string) (*os.File, error) {
	// O_NONBLOCK prevents a plugin-supplied FIFO from stalling the Wayland owner
	// before fstat can reject it. It has no effect on regular-file reads.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("not a regular file")
	}
	return f, nil
}
