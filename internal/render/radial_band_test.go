package render

import (
	"bytes"
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// halfGaugeBands is the air quality ramp reduced to three zones, the shape the
// airquality plugin asks for.
func halfGaugeBands() []ui.GaugeBand {
	return []ui.GaugeBand{
		{Until: 0.5, Severity: ui.SeverityGood},
		{Until: 0.8, Severity: ui.SeverityModerate},
		{Until: 1, Severity: ui.SeverityHigh},
	}
}

func paintGauge(t *testing.T, n *ui.Node, w, h int) *Canvas {
	t.Helper()
	c := newTestCanvas(t, w, h)
	n.Kind = ui.KindRadialGauge
	n.Bounds = ui.Rect{W: w, H: h}
	if err := paintNode(c, n, NewTextRenderer(mustTestFace(t)), testStyle, testStyle.Size); err != nil {
		t.Fatalf("paint gauge: %v", err)
	}
	return c
}

// A gauge with neither bands nor a half form must keep painting the bytes it
// always has, because every bar summary in the shell uses that path.
func TestBandedGaugeLeavesPlainGaugeUntouched(t *testing.T) {
	t.Parallel()
	plain := paintGauge(t, &ui.Node{Value: .62, ValueText: "62%"}, 48, 48)
	// Rendering the same node through the legacy entry point directly is the
	// reference the dispatcher must still choose.
	reference := newTestCanvas(t, 48, 48)
	n := &ui.Node{
		Kind: ui.KindRadialGauge, Value: .62, ValueText: "62%",
		Bounds: ui.Rect{W: 48, H: 48},
	}
	if err := paintRadialGauge(reference, n, NewTextRenderer(mustTestFace(t)), testStyle); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain.Pix, reference.Pix) {
		t.Fatal("a plain gauge did not paint through the original path")
	}
}

func TestBandedGaugePaintsThreeZones(t *testing.T) {
	t.Parallel()
	c := paintGauge(t, &ui.Node{Value: .3, Bands: halfGaugeBands()}, 96, 96)

	// Sample the ramp at the middle of each zone, on the ring itself.
	seen := map[Color]bool{}
	for _, progress := range []float64{.2, .65, .95} {
		col := ringColorAt(c, progress)
		seen[col] = true
	}
	if len(seen) != 3 {
		t.Fatalf("three zones painted %d distinct colours: %v", len(seen), seen)
	}

	good := severityPaint(ui.SeverityGood, testStyle.ContainerHighest)
	moderate := severityPaint(ui.SeverityModerate, testStyle.ContainerHighest)
	high := severityPaint(ui.SeverityHigh, testStyle.ContainerHighest)
	for name, want := range map[string]Color{"good": good, "moderate": moderate, "high": high} {
		if !seen[want] {
			t.Fatalf("%s zone colour %#v is not on the ring", name, want)
		}
	}
}

// The zones are the scale, so they paint whether or not the reading reached
// them: a reader has to see where "high" begins.
func TestBandedGaugePaintsZonesBeyondTheValue(t *testing.T) {
	t.Parallel()
	c := paintGauge(t, &ui.Node{Value: .1, Bands: halfGaugeBands()}, 96, 96)
	if got, want := ringColorAt(c, .95), severityPaint(ui.SeverityHigh, testStyle.ContainerHighest); got != want {
		t.Fatalf("high zone at 95%% = %#v, want %#v", got, want)
	}
}

func TestHalfGaugePaintsOnlyTheUpperHalf(t *testing.T) {
	t.Parallel()
	full := paintGauge(t, &ui.Node{Value: 0, Bands: halfGaugeBands()}, 96, 96)
	half := paintGauge(t, &ui.Node{Value: 0, Bands: halfGaugeBands(), Form: ui.GaugeHalf}, 96, 96)

	// A full gauge with no value still paints its track all the way round, so
	// the lower half has ink. A half gauge leaves it empty.
	if countInk(full, ui.Rect{W: 96, Y: 60, H: 36}) == 0 {
		t.Fatal("full gauge painted nothing in the lower half, so the comparison proves nothing")
	}
	// The label sits under the arc, so the proof is the far corner: a full
	// ring paints its lower-left arc there and a half one cannot reach it.
	if got := countInk(half, ui.Rect{W: 24, Y: 62, H: 24}); got != 0 {
		t.Fatalf("half gauge painted %d pixels in the lower-left corner", got)
	}
}

func TestHalfGaugeSweepsLeftToRight(t *testing.T) {
	t.Parallel()
	// The needle sits mid-sweep, so the two ends are clear of it.
	c := paintGauge(t, &ui.Node{Value: .5, Bands: halfGaugeBands(), Form: ui.GaugeHalf}, 96, 96)
	// The half arc runs from nine o'clock to three, so both ends of the sweep
	// sit on the horizontal midline rather than at twelve o'clock.
	if got := ringColorAtSweep(c, .02, true); got != severityPaint(ui.SeverityGood, testStyle.ContainerHighest) {
		t.Fatalf("start of sweep = %#v, want the good zone", got)
	}
	if got := ringColorAtSweep(c, .98, true); got != severityPaint(ui.SeverityHigh, testStyle.ContainerHighest) {
		t.Fatalf("end of sweep = %#v, want the high zone", got)
	}
}

func TestBandedGaugeDrawsNeedleAtValue(t *testing.T) {
	t.Parallel()
	low := paintGauge(t, &ui.Node{Value: .15, Bands: halfGaugeBands()}, 96, 96)
	high := paintGauge(t, &ui.Node{Value: .85, Bands: halfGaugeBands()}, 96, 96)
	if bytes.Equal(low.Pix, high.Pix) {
		t.Fatal("15% and 85% gauges painted identically, so the needle never moved")
	}
	// The needle is foreground; the zones are not.
	if countInk(low, ui.Rect{W: 96, H: 96}) == countInk(high, ui.Rect{W: 96, H: 96}) {
		t.Fatal("needle drew nothing distinct from the zone paint")
	}
}

func TestFitGaugeTextLeavesRoomForTheArc(t *testing.T) {
	t.Parallel()
	// A banded gauge carries a reading and its category, which is far wider
	// than the percentage the arc-derived size was tuned for. Fitting must
	// shrink it to leave a margin, not merely stop at the arc's full width.
	tr := NewTextRenderer(mustTestFace(t))
	label := "19.9 µg/m³ Fair"
	const width = 200
	base := radialValueSize(width, label)
	got := fitGaugeText(tr, label, width, base)
	if got >= base {
		t.Fatalf("fitGaugeText kept the arc size %d for a %dpx box, so it never shrank", base, width)
	}
	measured, _, err := tr.Measure(label, TextSpec{Size: got}, true)
	if err != nil {
		t.Fatal(err)
	}
	if measured > width*9/10 {
		t.Fatalf("label measured %dpx in a %dpx box, leaving no margin", measured, width)
	}
}

func TestBandedGaugeAbsentPaintsNoZonesOrNeedle(t *testing.T) {
	t.Parallel()
	c := paintGauge(t, &ui.Node{Absent: true, Value: .5, Bands: halfGaugeBands()}, 96, 96)
	for _, progress := range []float64{.2, .65, .95} {
		if got, want := ringColorAt(c, progress), testStyle.ContainerHighest; got != want {
			t.Fatalf("absent gauge at %v%% painted %#v, want the empty track %#v", progress*100, got, want)
		}
	}
}

func TestBandColorAtUsesExclusiveCeilings(t *testing.T) {
	t.Parallel()
	bands := halfGaugeBands()
	colors := []Color{{R: 1}, {R: 2}, {R: 3}}
	for _, tc := range []struct {
		progress float64
		want     Color
	}{
		{0, colors[0]},
		{0.4999, colors[0]},
		{0.5, colors[1]},
		{0.7999, colors[1]},
		{0.8, colors[2]},
		{1, colors[2]},
	} {
		got, ok := bandColorAt(bands, colors, tc.progress)
		if !ok || got != tc.want {
			t.Fatalf("progress %v = %#v, want %#v", tc.progress, got, tc.want)
		}
	}
	if _, ok := bandColorAt(nil, nil, .5); ok {
		t.Fatal("no bands reported a band")
	}
}

// The severity ramp has to stay legible as a band on any surface, which is the
// whole reason it is fixed rather than theme-derived.
func TestSeverityPaintMeetsBandContrast(t *testing.T) {
	t.Parallel()
	for _, surface := range []Color{
		{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		{R: 0x10, G: 0x14, B: 0x18, A: 0xff},
		{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
	} {
		for _, s := range []ui.Severity{ui.SeverityGood, ui.SeverityModerate, ui.SeverityHigh} {
			painted := severityPaint(s, surface)
			got := theme.ContrastRatio(
				theme.Color{R: painted.R, G: painted.G, B: painted.B, A: painted.A},
				theme.Color{R: surface.R, G: surface.G, B: surface.B, A: surface.A},
			)
			if got < 2.9 {
				t.Fatalf("severity %d bands at %.2f:1 on %#v", s, got, surface)
			}
		}
	}
}

// ringColorAt reports the colour painted on a full gauge ring at a fraction of
// the sweep, taking the strongest sample along that radius.
func ringColorAt(c *Canvas, progress float64) Color {
	return ringColorAtSweep(c, progress, false)
}

// ringColorAtSweep samples the ring at a fraction of the sweep, for a full ring
// or a half one. A half gauge's arc is the upper half of the circle, so the
// same centre and radius apply; only the start angle and the extent differ.
func ringColorAtSweep(c *Canvas, progress float64, half bool) Color {
	size := min(c.Width, c.Height)
	cx := float64(c.Width) / 2
	cy := float64(c.Height) / 2
	angle := progress * 2 * math.Pi
	if half {
		angle = -math.Pi/2 + progress*math.Pi
	}
	best := Color{}
	bestA := uint8(0)
	for r := float64(size)/2 - float64(size/14) - 2; r < float64(size)/2; r++ {
		x := int(cx + math.Sin(angle)*r)
		y := int(cy - math.Cos(angle)*r)
		if x < 0 || y < 0 || x >= c.Width || y >= c.Height {
			continue
		}
		// Canvas memory order is B, G, R, A.
		col := Color{
			B: c.Pix[y*c.Stride+x*4],
			G: c.Pix[y*c.Stride+x*4+1],
			R: c.Pix[y*c.Stride+x*4+2],
			A: c.Pix[y*c.Stride+x*4+3],
		}
		if col.A > bestA {
			best, bestA = col, col.A
		}
	}
	return best
}

// inkAt reports whether a pixel carries any paint.
func inkAt(c *Canvas, x, y int) bool {
	if x < 0 || y < 0 || x >= c.Width || y >= c.Height {
		return false
	}
	return c.Pix[y*c.Stride+x*4+3] > 8
}

func countInk(c *Canvas, box ui.Rect) int {
	n := 0
	for y := box.Y; y < box.Y+box.H && y < c.Height; y++ {
		for x := box.X; x < box.X+box.W && x < c.Width; x++ {
			if c.Pix[y*c.Stride+x*4+3] > 8 {
				n++
			}
		}
	}
	return n
}
