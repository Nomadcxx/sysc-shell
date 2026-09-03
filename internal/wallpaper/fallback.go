package wallpaper

import "errors"

// Static engine names, in preference order. awww is LGFae's swww successor and
// is what this machine has; swaybg is the floor.
const (
	engineAwww       = "awww"
	engineAwwwDaemon = "awww-daemon"
	engineSwaybg     = "swaybg"
)

// ErrNoStaticEngine reports that neither fallback is installed. It is an error
// rather than a silent no-op because the user asked for a wallpaper and would
// otherwise be looking at an unchanged desktop with nothing to explain it.
var ErrNoStaticEngine = errors.New("wallpaper: neither awww nor swaybg is on PATH")

// awwwImgArgs sets one still on one output through awww.
func awwwImgArgs(path, connector string) []string {
	return []string{engineAwww, "img", "--outputs", connector, path}
}

// swaybgArgs sets one still on one output through swaybg.
func swaybgArgs(path, connector string) []string {
	return []string{engineSwaybg, "-o", connector, "-i", path, "-m", "fill"}
}

// pickFallback chooses the static engine to use. lookup reports whether a
// binary is on PATH; it is injected so the choice is testable without one.
func pickFallback(lookup func(name string) bool) (string, error) {
	for _, name := range []string{engineAwww, engineSwaybg} {
		if lookup(name) {
			return name, nil
		}
	}
	return "", ErrNoStaticEngine
}

// fallbackArgs is the argv for the chosen static engine.
func fallbackArgs(engine, path, connector string) ([]string, error) {
	switch engine {
	case engineAwww:
		return awwwImgArgs(path, connector), nil
	case engineSwaybg:
		return swaybgArgs(path, connector), nil
	}
	return nil, ErrNoStaticEngine
}
