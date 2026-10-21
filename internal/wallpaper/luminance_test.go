package wallpaper

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAverageImageLuminance(t *testing.T) {
	tests := []struct {
		name   string
		colors []color.NRGBA
		want   float64
	}{
		{name: "black", colors: []color.NRGBA{{A: 0xff}}, want: 0},
		{name: "white", colors: []color.NRGBA{{R: 0xff, G: 0xff, B: 0xff, A: 0xff}}, want: 1},
		{name: "average per pixel", colors: []color.NRGBA{{A: 0xff}, {R: 0xff, G: 0xff, B: 0xff, A: 0xff}}, want: 0.5},
		{name: "transparent pixels composite over black", colors: []color.NRGBA{{R: 0xff, G: 0xff, B: 0xff}, {A: 0xff}}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "wallpaper.png")
			writeLuminancePNG(t, path, tt.colors...)

			got, err := averageImageLuminance(path, newDecodeBudget(4))
			if err != nil {
				t.Fatalf("averageImageLuminance: %v", err)
			}
			if math.Abs(got-tt.want) > 1e-6 {
				t.Fatalf("averageImageLuminance = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAverageImageLuminanceRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wallpaper.pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := averageImageLuminance(path, newDecodeBudget(4))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO was accepted as a still image")
		}
	case <-time.After(time.Second):
		t.Fatal("opening a FIFO blocked luminance calculation")
	}
}

func writeLuminancePNG(t *testing.T, path string, pixels ...color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, len(pixels), 1))
	for x, pixel := range pixels {
		img.SetNRGBA(x, 0, pixel)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
