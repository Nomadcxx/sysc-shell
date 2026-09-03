package theme

import (
	"fmt"
	"math"
	"strconv"
)

// Color is the parsed form of a palette role. The theme package owns it so
// palette validation can run without importing the renderer: a token set is
// checked long before any surface exists to paint it on.
type Color struct{ R, G, B, A uint8 }

// ParseColor reads #RRGGBB or #RRGGBBAA. A role that is not one of those two
// shapes is an error rather than a silent black, because a palette that half
// parses paints a surface in two themes at once.
func ParseColor(s string) (Color, error) {
	if len(s) != 7 && len(s) != 9 {
		return Color{}, fmt.Errorf("theme: %q is not #RRGGBB or #RRGGBBAA", s)
	}
	if s[0] != '#' {
		return Color{}, fmt.Errorf("theme: %q does not start with #", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return Color{}, fmt.Errorf("theme: %q is not hexadecimal", s)
	}
	if len(s) == 7 {
		return Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, nil
	}
	return Color{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}, nil
}

// Hex renders the colour back to the wire form, dropping a fully opaque alpha
// so a round trip through ParseColor reproduces the original string.
func (c Color) Hex() string {
	if c.A == 0xff {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

// channel converts one sRGB byte to its linear-light value.
func channel(v uint8) float64 {
	f := float64(v) / 255
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}

// Luminance is the WCAG relative luminance of the colour's sRGB channels.
// Alpha is not part of the definition: callers compare colours as they will be
// displayed, so a translucent foreground is composited before it gets here.
func (c Color) Luminance() float64 {
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// ContrastRatio is the WCAG 2.1 ratio between two colours, from 1 (identical)
// to 21 (black against white). The order of the arguments does not matter.
func ContrastRatio(a, b Color) float64 {
	la, lb := a.Luminance(), b.Luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// lerpChannel moves one sRGB byte a fraction of the way toward dest.
func lerpChannel(from, dest uint8, t float64) uint8 {
	return uint8(math.Round(float64(from) + (float64(dest)-float64(from))*t))
}

// EnsureContrast returns the smallest change to foreground that reaches ratio
// against background, or foreground unchanged when it already does.
//
// The repair travels toward black or toward white, whichever endpoint reaches
// the higher ratio against this background, and binary-searches the shortest
// distance along that line. Moving in sRGB rather than rewriting the hue keeps
// a repaired role recognisably the colour the palette asked for. When neither
// endpoint can reach the requested ratio -- a mid-grey background has no
// foreground at 7:1 -- the best reachable endpoint is returned rather than an
// error, because a frame still has to paint.
//
// Alpha is carried through untouched: the repair answers a legibility
// question, not a compositing one.
func EnsureContrast(foreground, background Color, ratio float64) Color {
	if ContrastRatio(foreground, background) >= ratio {
		return foreground
	}
	black := Color{R: 0, G: 0, B: 0, A: foreground.A}
	white := Color{R: 0xff, G: 0xff, B: 0xff, A: foreground.A}
	endpoint := black
	if ContrastRatio(white, background) > ContrastRatio(black, background) {
		endpoint = white
	}
	if ContrastRatio(endpoint, background) < ratio {
		return endpoint
	}

	at := func(t float64) Color {
		return Color{
			R: lerpChannel(foreground.R, endpoint.R, t),
			G: lerpChannel(foreground.G, endpoint.G, t),
			B: lerpChannel(foreground.B, endpoint.B, t),
			A: foreground.A,
		}
	}
	// Luminance is monotonic along the line and the endpoint is known to
	// satisfy the ratio, so the satisfying set is a suffix of [0,1] and
	// bisection finds its start. 24 steps resolve finer than one sRGB step.
	lo, hi := 0.0, 1.0
	for range 24 {
		mid := (lo + hi) / 2
		if ContrastRatio(at(mid), background) >= ratio {
			hi = mid
		} else {
			lo = mid
		}
	}
	// Rounding to whole bytes can land just under the target; walk the
	// remaining fraction out rather than returning a colour that fails.
	out := at(hi)
	for step := 0; step < 256 && ContrastRatio(out, background) < ratio; step++ {
		hi = math.Min(1, hi+1.0/255)
		out = at(hi)
	}
	return out
}
