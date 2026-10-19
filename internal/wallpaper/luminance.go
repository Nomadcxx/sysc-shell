package wallpaper

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

type assignedImageLuminance struct {
	path  string
	value float64
}

type imageLuminanceResult struct {
	connector string
	path      string
	value     float64
}

// averageImageLuminance returns the average relative luminance of a still.
// Transparent pixels are composited over black, the fallback beneath a still.
func averageImageLuminance(path string, budget *decodeBudget) (float64, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("wallpaper: %s is not a regular file", path)
	}
	if info.Size() > thumbMaxSource {
		return 0, fmt.Errorf("wallpaper: %s is %d bytes", path, info.Size())
	}

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, err
	}
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > thumbMaxDimension || config.Height > thumbMaxDimension {
		return 0, fmt.Errorf("wallpaper: %s is %dx%d", path, config.Width, config.Height)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return 0, err
	}

	cost := int64(config.Width) * int64(config.Height) * 4
	budget.acquire(cost)
	defer budget.release(cost)

	decoded, _, err := image.Decode(file)
	if err != nil {
		return 0, err
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return 0, fmt.Errorf("wallpaper: decoded dimensions changed from %dx%d to %dx%d", config.Width, config.Height, bounds.Dx(), bounds.Dy())
	}

	var sum float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			luminance := theme.Color{R: c.R, G: c.G, B: c.B}.Luminance()
			sum += luminance * float64(c.A) / 255
		}
	}
	return sum / float64(config.Width*config.Height), nil
}

// queueImageLuminance keeps image decoding off the service loop and the bar
// render path. The result is accepted only while this still is assigned.
func (s *Service) queueImageLuminance(connector string, assignment Assignment) {
	path := stillFor(assignment)
	delete(s.imageLuminance, connector)
	if path == "" || s.luminanceBudget == nil || s.luminanceResults == nil {
		return
	}
	s.work.Add(1)
	go func() {
		defer s.work.Done()
		value, err := averageImageLuminance(path, s.luminanceBudget)
		if err != nil {
			return
		}
		result := imageLuminanceResult{connector: connector, path: path, value: value}
		select {
		case s.luminanceResults <- result:
		case <-s.quit:
		}
	}()
}

func (s *Service) recordImageLuminance(result imageLuminanceResult) {
	assignment, ok := s.store.Assignment(result.connector)
	if !ok || stillFor(assignment) != result.path {
		return
	}
	if cached, ok := s.imageLuminance[result.connector]; ok &&
		cached.path == result.path && cached.value == result.value {
		return
	}
	if s.imageLuminance == nil {
		s.imageLuminance = make(map[string]assignedImageLuminance)
	}
	s.imageLuminance[result.connector] = assignedImageLuminance{path: result.path, value: result.value}
	s.publish()
}
