package ui

import "math"

// RoundedInset is how far row y of a rounded rectangle of the given height
// starts in from each side. The painter fills and clears with it, and blur
// regions are cut with it, so a region never shows past the painted corner.
func RoundedInset(y, height, radius int) int {
	edgeY := min(y, height-1-y)
	if edgeY >= radius {
		return 0
	}
	radiusSquared := float64(radius) * float64(radius)
	dy := float64(radius-edgeY) - 0.5
	dx := math.Sqrt(max(0, radiusSquared-dy*dy))
	return max(0, int(math.Ceil(float64(radius)-dx-0.5)))
}

// FilletCoverage is the coverage, 0 to 255, of one pixel of a concave
// fillet: the wedge that joins a straight edge to a body side meeting it at
// a right angle. x is the pixel's distance from the body side and y its
// distance from the edge.
//
// The arc is centred radius away from both, so it runs tangent into the edge
// at x = radius and into the side at y = radius, and the filled region is the
// square minus that disc. A one-pixel band antialiases the arc.
func FilletCoverage(x, y, radius int) uint8 {
	if radius <= 0 || x < 0 || y < 0 || x >= radius || y >= radius {
		return 0
	}
	r := float64(radius)
	distance := math.Hypot(r-(float64(x)+0.5), r-(float64(y)+0.5))
	coverage := min(max(distance-r+0.5, 0.0), 1.0)
	return uint8(coverage * 255)
}

// FilletExtent is how many pixels of row y the fillet touches at all,
// counted out from the body side. Clearing keeps these.
func FilletExtent(y, radius int) int {
	for x := radius - 1; x >= 0; x-- {
		if FilletCoverage(x, y, radius) > 0 {
			return x + 1
		}
	}
	return 0
}

// FilletSpan is how many pixels of row y the fillet covers at least half,
// counted out from the body side. Blur regions use it, so the blur stops
// inside the antialiased arc rather than past it.
func FilletSpan(y, radius int) int {
	for x := 0; x < radius; x++ {
		if FilletCoverage(x, y, radius) < 128 {
			return x
		}
	}
	return max(radius, 0)
}
