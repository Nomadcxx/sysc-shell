package ui

import "math"

// MorphRadius eases a resolved radius toward half its value while pressed.
func MorphRadius(base int, progress float64) int {
	return lerpInt(base, max(base/2, 1), clampProgress(progress))
}

const rippleGrowPhase = 0.625

// RippleDisc resolves a ripple's radius and normalized alpha over its phase.
// The disc grows with the shell's spatial curve, then fades for the rest.
func RippleDisc(phase float64, box Rect, originX, originY int) (radius, alpha float64) {
	p := clampProgress(phase)
	radius = EaseOutCubic(clampProgress(p/rippleGrowPhase)) * farthestCorner(box, originX, originY)
	alpha = 1
	if p > rippleGrowPhase {
		alpha = 1 - (p-rippleGrowPhase)/(1-rippleGrowPhase)
	}
	return radius, clampProgress(alpha)
}

func farthestCorner(box Rect, x, y int) float64 {
	if box.W <= 0 || box.H <= 0 {
		return 0
	}
	best := 0.0
	for _, c := range [4][2]int{
		{box.X, box.Y}, {box.X + box.W, box.Y},
		{box.X, box.Y + box.H}, {box.X + box.W, box.Y + box.H},
	} {
		best = max(best, math.Hypot(float64(c[0]-x), float64(c[1]-y)))
	}
	return best
}
