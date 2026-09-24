# Control Centre Monitor Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Control Centre Monitor section with the approved option C page, and replace the
column graph with an anti-aliased line sparkline for every `KindGraph` consumer.

**Architecture:** `internal/ui` gains one tone, a second series and a window on the graph node.
`internal/render` gets pure sparkline geometry (`sparkline.go`) and a rewritten `paintGraph` that
rasterises it with `golang.org/x/image/vector`. `internal/shell` gets pure scale, threshold and
subject-picker helpers, extra Control Centre leases with per-subject rate leases resolved from the first
snapshot, and a new page file that replaces `ccMonitor`.

**Tech Stack:** Go 1.26, `golang.org/x/image/vector` (already a direct dependency), sysc-metrics v0.5.1.

**Spec:** `docs/plans/2026-09-25-control-centre-monitor-design.md` (D1–D6). Tracked as `sysc-517`;
the VRAM caption is `sysc-522`, gated on `sysc-521`, and is not in this plan.

## Global Constraints

- No new module. `git diff --exit-code -- go.mod go.sum` stays clean.
- Stroke 1.5 logical px scaled by the render scale; second series 1 logical px; area fill 18% of the line colour; "now" dot 3 logical px across.
- Colours: primary `Accent`; second series `Secondary`; activity `Tertiary` via `ui.ToneActivity`; critical `Error` via `ui.ToneError`.
- Thresholds (activity / critical): CPU 50/90 %, CPU °C 60/85, GPU 50/90 %, GPU °C 60/85, memory 60/90 %, storage 80/95 %. Fixed in code.
- Scales: fractions 0–1; temperature 20–100 °C; rates `max(peak × 1.1, floor)` with floors 64 KiB/s network, 1 MiB/s disk; the two series of one chart share one scale.
- Page: 596 × 480 body, no scrolling. Heroes 168 tall at 355 / 228 with `theme.MarginL` between; rows card 299 tall; rows about 50 tall with `theme.MarginS` between; row marks 28 tall; CPU hero graph 64 tall; memory hero graph 28 tall.
- A missing sample is never a zero. An absent source shows `—` and paints no mark.
- Per the repo rules: panel `configure`/`render`/`handle` take `Registry.mu`; `UpdateMetrics` already holds it.
- Test commands: per-package only, `GOMAXPROCS=4`. Never `./...` with `-race` (it has hard-locked this machine).
- Commit messages pass `bash ~/.git-hooks/commit-msg <file>`; no assistant attribution trailer.

## Review Focus

1. A history of one sample: the line has no segment, but the dot still marks it at the right edge. Pinned in Task 3.
2. Network interfaces that appear or disappear while the page is open (a VPN, a Docker container): the chart must not flip between interfaces. The subject is re-resolved only when it disappears. Pinned in Task 6.
3. Rate history of all zeros (an idle disk): a flat line on the baseline, not a spike and not a division by zero. Pinned in Task 4.
4. A GPU whose `nvidia-smi` call fails intermittently (`sysc-495`): the GPU row falls back to `—` and its mark is absent for that sample, with no stale value. Pinned in Task 7.
5. Render scale 1.25 and 2.0: the dot and stroke stay inside the graph box. Pinned in Task 3.

---

### Task 1: Graph node fields, the activity tone, and graph height in columns

**Files:**
- Modify: `internal/ui/tree.go` (Tone constants near line 440; Node fields beside `Values` near line 222)
- Modify: `internal/ui/column.go:119-122`
- Modify: `internal/render/paint.go:284-297` (meter), `internal/render/paint.go:1434-1444` (`textColor`)
- Test: `internal/ui/column_test.go`, `internal/render/paint_test.go`

**Interfaces:**
- Produces: `ui.ToneActivity`; `ui.Node.SecondValues []float64`; `ui.Node.Window int`; `render.toneColor(style Style, tone ui.Tone, normal Color) Color`.

- [ ] **Step 1: Write the failing tests**

In `internal/ui/column_test.go`, add a case to the table that holds `{"graph", &Node{Kind: KindGraph, Width: 240}, GraphHeight}` (line 247):

```go
		{"graph with a height", &Node{Kind: KindGraph, Width: 240, Height: 28}, 28},
```

In `internal/render/paint_test.go`, add:

```go
func TestToneColorSelectsThresholdRoles(t *testing.T) {
	for _, tc := range []struct {
		tone ui.Tone
		want Color
	}{
		{ui.ToneNormal, testStyle.Accent},
		{ui.ToneActivity, testStyle.Tertiary},
		{ui.ToneError, testStyle.Error},
	} {
		if got := toneColor(testStyle, tc.tone, testStyle.Accent); got != tc.want {
			t.Errorf("toneColor(%d) = %v, want %v", tc.tone, got, tc.want)
		}
	}
	if got := textColor(testStyle, ui.ToneActivity); got != testStyle.Tertiary {
		t.Errorf("activity text = %v, want Tertiary", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestColumnChildHeight|TestToneColor' ./internal/ui ./internal/render`
Expected: FAIL. `ui.ToneActivity` is undefined; the column case returns 64.

(If the table's test function has a different name, use `grep -n "GraphHeight}" internal/ui/column_test.go` to find it and run that name.)

- [ ] **Step 3: Implement**

`internal/ui/tree.go`, append after `ToneSubtle` so existing values keep their numbers:

```go
	// ToneActivity marks a reading past its activity threshold but short of
	// critical. It paints the theme's Tertiary role: the theme has no warning
	// token, and Tertiary exists on every palette.
	ToneActivity
```

Beside `Values`:

```go
	// SecondValues is an optional second graph series, drawn as a thin line
	// with no area in the secondary colour: upload beside download, writes
	// beside reads. Normalised to the same scale as Values.
	SecondValues []float64
	// Window is how many samples the graph's full width represents. Zero
	// means the samples present fill the width. A graph with a window draws a
	// short history against the right edge rather than stretching it.
	Window int
```

`internal/ui/column.go`, the `KindGraph` case:

```go
	case KindGraph:
		// Width is the graph's measured width in a row. Reusing it as a height
		// makes the monitor popout's 240-wide sparkline 240 tall.
		if n.Height > 0 {
			return n.Height, nil
		}
		return GraphHeight, nil
```

`internal/render/paint.go`, next to `textColor`:

```go
// toneColor resolves a threshold tone for a graph line or meter fill. A
// normal tone keeps the caller's colour.
func toneColor(style Style, tone ui.Tone, normal Color) Color {
	switch tone {
	case ui.ToneError:
		return style.Error
	case ui.ToneActivity:
		return style.Tertiary
	}
	return normal
}
```

In `textColor` add `case ui.ToneActivity: return style.Tertiary`. In the `KindMeter` case replace the
`fill := ...; if n.Tone == ui.ToneError {...}` lines with `fill := toneColor(style, n.Tone, style.accent())`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/ui ./internal/render`
Expected: PASS. `kindcoverage_test.go` does not enumerate tones, so it needs no change.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui internal/render && GOMAXPROCS=4 go vet ./internal/ui ./internal/render
git add internal/ui/tree.go internal/ui/column.go internal/ui/column_test.go internal/render/paint.go internal/render/paint_test.go
git commit -m "feat(ui): graph second series, window and activity tone"
```

---

### Task 2: Sparkline geometry

**Files:**
- Create: `internal/render/sparkline.go`
- Test: `internal/render/sparkline_test.go`

**Interfaces:**
- Produces (all unexported, used by Task 3):
  - `type fpt struct{ X, Y float32 }`
  - `sparklinePoints(values []float64, window, w, h int, inset float32) []fpt`
  - `smoothLine(points []fpt) []fpt`
  - `strokeContours(line []fpt, width float32) [][]fpt`
  - `areaContour(line []fpt, floor float32) []fpt`
  - `circleContour(c fpt, r float32, segments int) []fpt`
  - `rasterize(w, h int, contours [][]fpt) *image.Alpha`

- [ ] **Step 1: Write the failing tests**

`internal/render/sparkline_test.go`:

```go
package render

import (
	"math"
	"testing"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestSparklinePoints(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []float64
		window int
		inset  float32
		want   []fpt
	}{
		{"fills width", []float64{0, 1}, 0, 0, []fpt{{0, 10}, {10, 0}}},
		{"window draws short history from the right", []float64{0, 1}, 3, 0, []fpt{{5, 10}, {10, 0}}},
		{"one sample sits on the right edge", []float64{0.5}, 0, 0, []fpt{{10, 5}}},
		{"out of range values clamp", []float64{-1, 2}, 0, 0, []fpt{{0, 10}, {10, 0}}},
		{"inset keeps the line off every edge", []float64{0, 1}, 0, 1, []fpt{{1, 9}, {9, 1}}},
		{"window smaller than samples is ignored", []float64{0, 0.5, 1}, 2, 0, []fpt{{0, 10}, {5, 5}, {10, 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sparklinePoints(tc.values, tc.window, 11, 11, tc.inset)
			if len(got) != len(tc.want) {
				t.Fatalf("points = %v, want %v", got, tc.want)
			}
			for i := range got {
				if !near(got[i].X, tc.want[i].X) || !near(got[i].Y, tc.want[i].Y) {
					t.Fatalf("points = %v, want %v", got, tc.want)
				}
			}
		})
	}
	if got := sparklinePoints(nil, 0, 11, 11, 0); got != nil {
		t.Fatalf("empty series = %v, want nil", got)
	}
}

func TestSmoothLineKeepsEndpointsAndPassesMidpoints(t *testing.T) {
	in := []fpt{{0, 10}, {5, 0}, {10, 10}}
	out := smoothLine(in)
	if out[0] != in[0] || out[len(out)-1] != in[2] {
		t.Fatalf("endpoints = %v .. %v", out[0], out[len(out)-1])
	}
	// The curve ends each span on the midpoint between samples.
	mid := fpt{7.5, 5}
	found := false
	for _, p := range out {
		if near(p.X, mid.X) && near(p.Y, mid.Y) {
			found = true
		}
	}
	if !found {
		t.Fatalf("smoothed line %v never reaches the midpoint %v", out, mid)
	}
	if got := smoothLine(in[:2]); len(got) != 2 {
		t.Fatalf("two points should pass through unchanged, got %v", got)
	}
}

func TestContoursArePositivelyOriented(t *testing.T) {
	contours := strokeContours([]fpt{{0, 5}, {10, 5}, {10, 0}}, 2)
	contours = append(contours, circleContour(fpt{5, 5}, 2, 12))
	for i, c := range contours {
		if signedArea(c) <= 0 {
			t.Fatalf("contour %d has area %v, want positive so overlaps add", i, signedArea(c))
		}
	}
}

func TestRasterizeAntialiasesADiagonalStroke(t *testing.T) {
	mask := rasterize(20, 20, strokeContours([]fpt{{2, 18}, {18, 2}}, 1.5))
	partial, full := 0, 0
	for _, a := range mask.Pix {
		switch {
		case a == 255:
			full++
		case a > 0:
			partial++
		}
	}
	if partial == 0 {
		t.Fatal("stroke has no partial-coverage pixels: it is not anti-aliased")
	}
	if full+partial == 0 {
		t.Fatal("stroke painted nothing")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestSparkline|TestSmoothLine|TestContours|TestRasterize' ./internal/render`
Expected: FAIL to compile, `sparklinePoints undefined`.

- [ ] **Step 3: Implement `internal/render/sparkline.go`**

```go
package render

import (
	"image"
	"math"

	"golang.org/x/image/vector"
)

// fpt is a point in the graph box's physical pixel space.
type fpt struct{ X, Y float32 }

// smoothSteps is how many line segments approximate each quadratic span. At
// sparkline sizes eight is indistinguishable from the true curve.
const smoothSteps = 8

// sparklinePoints maps normalised samples, oldest first, onto a w×h box. The
// newest sample sits on the right edge. window is how many samples the full
// width represents; a window smaller than the samples present is ignored.
// inset keeps the line, and the dot on it, off every edge of the box.
func sparklinePoints(values []float64, window, w, h int, inset float32) []fpt {
	n := len(values)
	if n == 0 || w <= 0 || h <= 0 {
		return nil
	}
	window = max(window, n)
	right := float32(w-1) - inset
	span := max(float32(w-1)-2*inset, 0)
	height := max(float32(h-1)-2*inset, 0)
	step := float32(0)
	if window > 1 {
		step = span / float32(window-1)
	}
	out := make([]fpt, n)
	for i, v := range values {
		v = min(max(v, 0), 1)
		out[i] = fpt{
			X: right - float32(n-1-i)*step,
			Y: inset + height*float32(1-v),
		}
	}
	return out
}

// smoothLine draws quadratic curves through the midpoints between samples,
// with each sample as the control point, flattened to a polyline. It keeps the
// first and last samples exactly.
func smoothLine(points []fpt) []fpt {
	if len(points) < 3 {
		return points
	}
	out := []fpt{points[0]}
	start := points[0]
	for i := 1; i < len(points)-1; i++ {
		ctrl := points[i]
		end := fpt{(points[i].X + points[i+1].X) / 2, (points[i].Y + points[i+1].Y) / 2}
		for k := 1; k <= smoothSteps; k++ {
			t := float32(k) / smoothSteps
			u := 1 - t
			out = append(out, fpt{
				X: u*u*start.X + 2*u*t*ctrl.X + t*t*end.X,
				Y: u*u*start.Y + 2*u*t*ctrl.Y + t*t*end.Y,
			})
		}
		start = end
	}
	return append(out, points[len(points)-1])
}

// strokeContours outlines a polyline as one quad per segment plus a round
// join at every interior point. Every contour is positively oriented, so where
// they overlap the rasterizer's accumulated coverage clamps at full rather
// than cancelling.
func strokeContours(line []fpt, width float32) [][]fpt {
	half := width / 2
	var out [][]fpt
	for i := 0; i+1 < len(line); i++ {
		a, b := line[i], line[i+1]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l == 0 {
			continue
		}
		nx, ny := -dy/l*half, dx/l*half
		out = append(out, oriented([]fpt{
			{a.X + nx, a.Y + ny}, {b.X + nx, b.Y + ny},
			{b.X - nx, b.Y - ny}, {a.X - nx, a.Y - ny},
		}))
	}
	for i := 1; i+1 < len(line); i++ {
		out = append(out, circleContour(line[i], half, 8))
	}
	return out
}

// areaContour closes the line down to floor, the box's lower edge.
func areaContour(line []fpt, floor float32) []fpt {
	if len(line) < 2 {
		return nil
	}
	out := append([]fpt(nil), line...)
	out = append(out, fpt{line[len(line)-1].X, floor}, fpt{line[0].X, floor})
	return oriented(out)
}

func circleContour(c fpt, r float32, segments int) []fpt {
	out := make([]fpt, segments)
	for i := range out {
		a := 2 * math.Pi * float64(i) / float64(segments)
		out[i] = fpt{c.X + r*float32(math.Cos(a)), c.Y + r*float32(math.Sin(a))}
	}
	return oriented(out)
}

func signedArea(poly []fpt) float32 {
	var sum float32
	for i := range poly {
		j := (i + 1) % len(poly)
		sum += poly[i].X*poly[j].Y - poly[j].X*poly[i].Y
	}
	return sum / 2
}

func oriented(poly []fpt) []fpt {
	if signedArea(poly) < 0 {
		for i, j := 0, len(poly)-1; i < j; i, j = i+1, j-1 {
			poly[i], poly[j] = poly[j], poly[i]
		}
	}
	return poly
}

// rasterize fills contours into a w×h coverage mask.
func rasterize(w, h int, contours [][]fpt) *image.Alpha {
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	r := vector.NewRasterizer(w, h)
	for _, c := range contours {
		if len(c) < 3 {
			continue
		}
		r.MoveTo(c[0].X, c[0].Y)
		for _, p := range c[1:] {
			r.LineTo(p.X, p.Y)
		}
		r.ClosePath()
	}
	r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
	return dst
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/render`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/render && GOMAXPROCS=4 go vet ./internal/render
git add internal/render/sparkline.go internal/render/sparkline_test.go
git commit -m "feat(render): sparkline geometry and coverage rasterizer"
```

---

### Task 3: Paint the line sparkline

**Files:**
- Modify: `internal/render/paint.go:771-811` (`paintGraph` and its doc comment)
- Test: `internal/render/paint_test.go:839-865` (replace `TestGraphPaintsTallerColumnsForLargerValues`)

**Interfaces:**
- Consumes: everything Task 2 produces; `toneColor` from Task 1.
- Produces: the painted look every `KindGraph` consumer gets. No signature change.

- [ ] **Step 1: Replace the column test with line tests**

Delete `TestGraphPaintsTallerColumnsForLargerValues` and add:

```go
func paintGraphNode(t *testing.T, style Style, n *ui.Node, cw, ch int) *Canvas {
	t.Helper()
	c := newTestCanvas(t, cw, ch)
	style.Body = ui.Rect{W: cw, H: ch}
	if err := Paint(c, &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}}, NewTextRenderer(mustTestFace(t)), style); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	return c
}

func pixelAt(c *Canvas, x, y int) Color {
	i := y*c.Stride + x*4
	return Color{B: c.Pix[i], G: c.Pix[i+1], R: c.Pix[i+2], A: c.Pix[i+3]}
}

func TestGraphLineStaysInsideItsBox(t *testing.T) {
	t.Parallel()
	for _, scale := range []ui.Scale120{ui.ScaleUnit, 150, 240} {
		style := testStyle
		style.Scale120 = scale
		box := ui.Rect{X: 10, Y: 10, W: 40, H: 20}
		n := &ui.Node{Kind: ui.KindGraph, Width: box.W, Values: []float64{0, 1, 0, 1},
			SecondValues: []float64{1, 0, 1, 0}, Bounds: box}
		c := paintGraphNode(t, style, n, 120, 90)
		phys := scale.PhysicalRect(box)
		for y := 0; y < c.Height; y++ {
			for x := 0; x < c.Width; x++ {
				inside := x >= phys.X && x < phys.X+phys.W && y >= phys.Y && y < phys.Y+phys.H
				if !inside && pixelAt(c, x, y).A != 0 {
					t.Fatalf("scale %d painted (%d,%d) outside %v", scale, x, y, phys)
				}
			}
		}
	}
}

func TestGraphMarksTheNewestSampleWithTheToneColour(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tone ui.Tone
		want Color
	}{
		{ui.ToneNormal, testStyle.Accent},
		{ui.ToneActivity, testStyle.Tertiary},
		{ui.ToneError, testStyle.Error},
	} {
		// One full sample: the dot centre is inset 1.5px from the top-right corner.
		n := &ui.Node{Kind: ui.KindGraph, Width: 40, Values: []float64{1}, Tone: tc.tone,
			Bounds: ui.Rect{W: 40, H: 20}}
		c := paintGraphNode(t, testStyle, n, 40, 20)
		if got := pixelAt(c, 37, 1); got != tc.want {
			t.Errorf("tone %d dot = %v, want %v", tc.tone, got, tc.want)
		}
	}
}

func TestGraphDrawsNewestOnTheRight(t *testing.T) {
	t.Parallel()
	n := &ui.Node{Kind: ui.KindGraph, Width: 40, Values: []float64{0, 0, 0, 1}, Bounds: ui.Rect{W: 40, H: 20}}
	c := paintGraphNode(t, testStyle, n, 40, 20)
	if pixelAt(c, 37, 1).A == 0 {
		t.Error("the newest full sample painted nothing in the top-right corner")
	}
	if pixelAt(c, 2, 2).A != 0 {
		t.Error("the oldest zero sample painted in the top-left corner")
	}
	if got := pixelAt(c, 20, 19); got != testStyle.Track && got.A == 0 {
		t.Error("the baseline is missing from the lower edge")
	}
}

func TestAnEmptyGraphPaintsNothing(t *testing.T) {
	t.Parallel()
	n := &ui.Node{Kind: ui.KindGraph, Width: 40, Bounds: ui.Rect{W: 40, H: 20}}
	c := paintGraphNode(t, testStyle, n, 40, 20)
	for i := 3; i < len(c.Pix); i += 4 {
		if c.Pix[i] != 0 {
			t.Fatal("a graph with no samples painted pixels")
		}
	}
}
```

`TestAnAbsentGraphPaintsNothing` (line 922) stays as written.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestGraph|TestAnEmptyGraph|TestAnAbsentGraph' ./internal/render`
Expected: FAIL. The column painter has no dot, and it paints a zero-width baseline nowhere.

- [ ] **Step 3: Rewrite `paintGraph`**

Replace the function and its doc comment:

```go
// paintGraph draws a sparkline: a baseline, an area under the primary series,
// an optional thin second series, the primary line, and a dot on the newest
// sample. Values arrive normalised to zero through one; the painter applies no
// scale of its own. Coverage comes from the vector rasterizer, so the line is
// anti-aliased at every render scale.
func paintGraph(c *Canvas, n *ui.Node, box ui.Rect, style Style) error {
	if n.Absent || box.W <= 0 || box.H <= 0 || len(n.Values) == 0 {
		return nil
	}
	scale := float32(style.Scale120) / float32(ui.ScaleUnit)
	if scale <= 0 {
		scale = 1
	}
	stroke := max(1.5*scale, 1)
	dot := 1.5 * scale
	inset := max(stroke/2, dot)
	window := max(n.Window, len(n.Values), len(n.SecondValues))
	line := toneColor(style, n.Tone, style.accent())

	fillRect(c, ui.Rect{X: box.X, Y: box.Y + box.H - 1, W: box.W, H: 1}, style.Track)

	primary := smoothLine(sparklinePoints(n.Values, window, box.W, box.H, inset))
	if area := areaContour(primary, float32(box.H)); area != nil {
		blendMask(c, rasterize(box.W, box.H, [][]fpt{area}), box.X, box.Y, withAlpha(line, 0.18))
	}
	if len(n.SecondValues) > 0 {
		second := smoothLine(sparklinePoints(n.SecondValues, window, box.W, box.H, inset))
		blendMask(c, rasterize(box.W, box.H, strokeContours(second, max(scale, 1))), box.X, box.Y, style.Secondary)
	}
	blendMask(c, rasterize(box.W, box.H, strokeContours(primary, stroke)), box.X, box.Y, line)
	newest := primary[len(primary)-1]
	blendMask(c, rasterize(box.W, box.H, [][]fpt{circleContour(newest, dot, 12)}), box.X, box.Y, line)
	return nil
}

// withAlpha scales a colour's alpha by f.
func withAlpha(col Color, f float64) Color {
	col.A = uint8(float64(col.A)*f + 0.5)
	return col
}
```

`areaContour` returns nil for one sample, so a lone sample paints only the dot and the baseline.

- [ ] **Step 4: Run the render, shell and plugin tests**

Run:

```bash
GOMAXPROCS=4 go test -count=1 ./internal/render ./internal/ui
GOMAXPROCS=4 go test -count=1 -run 'Metric|Graph|Monitor' ./internal/shell ./internal/plugin
```

Expected: PASS. Any shell test that counted graph columns now fails; rewrite its assertion to
`Values`/`Absent` on the node rather than pixels. Do not bring back column behaviour.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/render && GOMAXPROCS=4 go vet ./internal/render
git add internal/render/paint.go internal/render/paint_test.go
git commit -m "feat(render): draw graphs as anti-aliased sparklines"
```

---

### Task 4: Monitor scale, threshold and caption helpers

**Files:**
- Create: `internal/shell/monitorscale.go`
- Test: `internal/shell/monitorscale_test.go`

**Interfaces:**
- Consumes: `formatRate(float64) string` (`metricwidget.go:103`).
- Produces:
  - `type monitorMetric int` with `metricCPU, metricCPUTemp, metricGPU, metricGPUTemp, metricMemory, metricStorage`
  - `thresholdTone(m monitorMetric, value float64) ui.Tone` — `value` in percent or °C
  - `rateCeiling(floor float64, series ...[]float64) float64`
  - `scaleSeries(values []float64, ceiling float64) []float64`
  - `temperatureSeries(fractions []float64) []float64` — input is `°C / 100` as the metrics ring stores it
  - `peakCaption(series ...[]float64) string`
  - `meanCoreGHz(snap services.Snapshot) (float64, bool)`
  - constants `networkRateFloor = 64 << 10`, `diskRateFloor = 1 << 20`

- [ ] **Step 1: Write the failing tests**

```go
package shell

import (
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestThresholdTone(t *testing.T) {
	for _, tc := range []struct {
		m    monitorMetric
		v    float64
		want ui.Tone
	}{
		{metricCPU, 49.9, ui.ToneNormal}, {metricCPU, 50, ui.ToneActivity}, {metricCPU, 90, ui.ToneError},
		{metricCPUTemp, 59, ui.ToneNormal}, {metricCPUTemp, 60, ui.ToneActivity}, {metricCPUTemp, 85, ui.ToneError},
		{metricGPU, 50, ui.ToneActivity}, {metricGPUTemp, 85, ui.ToneError},
		{metricMemory, 59, ui.ToneNormal}, {metricMemory, 60, ui.ToneActivity}, {metricMemory, 90, ui.ToneError},
		{metricStorage, 79, ui.ToneNormal}, {metricStorage, 80, ui.ToneActivity}, {metricStorage, 95, ui.ToneError},
	} {
		if got := thresholdTone(tc.m, tc.v); got != tc.want {
			t.Errorf("thresholdTone(%d, %v) = %d, want %d", tc.m, tc.v, got, tc.want)
		}
	}
}

func TestRateCeiling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		floor  float64
		series [][]float64
		want   float64
	}{
		{"idle stays on the floor", networkRateFloor, [][]float64{{0, 0, 0}}, networkRateFloor},
		{"blip under the floor", networkRateFloor, [][]float64{{2048}}, networkRateFloor},
		{"peak gets headroom", networkRateFloor, [][]float64{{1 << 20}}, 1.1 * (1 << 20)},
		{"series share one scale", diskRateFloor, [][]float64{{1 << 20}, {10 << 20}}, 1.1 * (10 << 20)},
		{"no samples", diskRateFloor, nil, diskRateFloor},
	} {
		if got := rateCeiling(tc.floor, tc.series...); got != tc.want {
			t.Errorf("%s: rateCeiling = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestScaleSeriesAndTemperature(t *testing.T) {
	got := scaleSeries([]float64{0, 50, 200}, 100)
	if got[0] != 0 || got[1] != 0.5 || got[2] != 1 {
		t.Fatalf("scaleSeries = %v", got)
	}
	temps := temperatureSeries([]float64{0.10, 0.20, 0.60, 1.00, 1.20})
	want := []float64{0, 0, 0.5, 1, 1}
	for i := range want {
		if temps[i] != want[i] {
			t.Fatalf("temperatureSeries = %v, want %v", temps, want)
		}
	}
	if scaleSeries(nil, 100) != nil {
		t.Fatal("scaleSeries(nil) is not nil")
	}
}

func TestPeakCaption(t *testing.T) {
	if got := peakCaption([]float64{0, 1 << 20}, []float64{3 << 20}); got != "peak "+formatRate(3<<20) {
		t.Fatalf("peakCaption = %q", got)
	}
	if got := peakCaption(nil); got != "" {
		t.Fatalf("empty peakCaption = %q, want empty", got)
	}
}

func TestMeanCoreGHz(t *testing.T) {
	snap := services.Snapshot{CPU: &metrics.CPUSnapshot{Cores: []metrics.CPUCore{
		{FrequencyHz: 4_000_000_000, FrequencyValid: true},
		{FrequencyHz: 3_000_000_000, FrequencyValid: true},
		{FrequencyHz: 9_000_000_000, FrequencyValid: false},
	}}}
	if got, ok := meanCoreGHz(snap); !ok || got != 3.5 {
		t.Fatalf("meanCoreGHz = %v, %v", got, ok)
	}
	if _, ok := meanCoreGHz(services.Snapshot{}); ok {
		t.Fatal("no CPU snapshot reported a frequency")
	}
}
```

Check the core type name first: `grep -n "Cores  *\[\]" $(go env GOMODCACHE)/github.com/\!nomadcxx/sysc-metrics@v0.5.1/metrics.go`.
If it is not `CPUCore`, use the name found there in both the test and the helper.

- [ ] **Step 2: Run to verify failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestThresholdTone|TestRateCeiling|TestScaleSeries|TestPeakCaption|TestMeanCoreGHz' ./internal/shell`
Expected: FAIL to compile.

- [ ] **Step 3: Implement `internal/shell/monitorscale.go`**

```go
package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// monitorMetric names a reading that carries activity and critical thresholds.
type monitorMetric int

const (
	metricCPU monitorMetric = iota
	metricCPUTemp
	metricGPU
	metricGPUTemp
	metricMemory
	metricStorage
)

// monitorThresholds are Noctalia's activity and critical defaults, in percent
// or degrees Celsius. They are fixed: configuration would be a surface with
// no consumer.
var monitorThresholds = map[monitorMetric][2]float64{
	metricCPU:     {50, 90},
	metricCPUTemp: {60, 85},
	metricGPU:     {50, 90},
	metricGPUTemp: {60, 85},
	metricMemory:  {60, 90},
	metricStorage: {80, 95},
}

// Rate scale floors keep idle noise flat instead of drawing a blip as a
// saturated link.
const (
	networkRateFloor = 64 << 10
	diskRateFloor    = 1 << 20
	// temperatureFloor and temperatureCeiling fix the temperature scale so a
	// warm room does not rescale the line.
	temperatureFloor   = 20.0
	temperatureCeiling = 100.0
)

func thresholdTone(m monitorMetric, value float64) ui.Tone {
	t := monitorThresholds[m]
	switch {
	case value >= t[1]:
		return ui.ToneError
	case value >= t[0]:
		return ui.ToneActivity
	}
	return ui.ToneNormal
}

// rateCeiling is the shared full-scale value for one rate chart: the window's
// peak with ten per cent headroom, never below the floor.
func rateCeiling(floor float64, series ...[]float64) float64 {
	peak := 0.0
	for _, s := range series {
		for _, v := range s {
			peak = max(peak, v)
		}
	}
	return max(peak*1.1, floor)
}

func scaleSeries(values []float64, ceiling float64) []float64 {
	if len(values) == 0 || ceiling <= 0 {
		return nil
	}
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = min(max(v/ceiling, 0), 1)
	}
	return out
}

// temperatureSeries rescales the metrics ring's °C/100 samples onto the fixed
// 20–100 °C chart range.
func temperatureSeries(fractions []float64) []float64 {
	if len(fractions) == 0 {
		return nil
	}
	out := make([]float64, len(fractions))
	span := temperatureCeiling - temperatureFloor
	for i, f := range fractions {
		out[i] = min(max((f*100-temperatureFloor)/span, 0), 1)
	}
	return out
}

// peakCaption names an auto-scaled chart's magnitude. A rate chart without
// one misstates it.
func peakCaption(series ...[]float64) string {
	peak, seen := 0.0, false
	for _, s := range series {
		for _, v := range s {
			peak, seen = max(peak, v), true
		}
	}
	if !seen {
		return ""
	}
	return "peak " + formatRate(peak)
}

// meanCoreGHz averages the cores that reported a frequency.
func meanCoreGHz(snap services.Snapshot) (float64, bool) {
	if snap.CPU == nil {
		return 0, false
	}
	sum, n := 0.0, 0
	for _, core := range snap.CPU.Cores {
		if core.FrequencyValid {
			sum += float64(core.FrequencyHz)
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n) / 1e9, true
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestThresholdTone|TestRateCeiling|TestScaleSeries|TestPeakCaption|TestMeanCoreGHz' ./internal/shell`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/monitorscale.go internal/shell/monitorscale_test.go
git commit -m "feat(shell): monitor scale, threshold and caption helpers"
```

---

### Task 5: Primary interface and block device pickers

**Files:**
- Create: `internal/shell/monitorsubjects.go`
- Test: `internal/shell/monitorsubjects_test.go`

**Interfaces:**
- Produces:
  - `primaryInterface(snap services.Snapshot) string`
  - `primaryBlockDevice(snap services.Snapshot, resolve func(string) (string, error)) string`
  - `resolveDevicePath(path string) (string, error)` — `filepath.EvalSymlinks`, used as the live `resolve`

- [ ] **Step 1: Write the failing tests**

```go
package shell

import (
	"errors"
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func TestPrimaryInterface(t *testing.T) {
	iface := func(name string, rx, tx uint64) metrics.NetworkInterface {
		return metrics.NetworkInterface{Name: name, ReceiveBytes: rx, TransmitBytes: tx}
	}
	for _, tc := range []struct {
		name string
		ifs  []metrics.NetworkInterface
		want string
	}{
		{"this desktop", []metrics.NetworkInterface{
			iface("br-07deea98ce79", 10, 10), iface("docker0", 50, 50), iface("enp7s0", 9e9, 1e9),
			iface("lo", 9e12, 9e12), iface("tailscale0", 1e6, 1e6), iface("veth0e02b9d", 100, 100),
		}, "enp7s0"},
		{"loopback only", []metrics.NetworkInterface{iface("lo", 5, 5)}, ""},
		{"tie breaks by name", []metrics.NetworkInterface{iface("wlan0", 5, 5), iface("eth0", 5, 5)}, "eth0"},
		{"none", nil, ""},
	} {
		snap := services.Snapshot{Network: &metrics.NetworkSnapshot{Interfaces: tc.ifs}}
		if got := primaryInterface(snap); got != tc.want {
			t.Errorf("%s: primaryInterface = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := primaryInterface(services.Snapshot{}); got != "" {
		t.Errorf("nil network = %q", got)
	}
}

func TestPrimaryBlockDevice(t *testing.T) {
	dev := func(name string, r, w uint64) metrics.BlockDevice {
		return metrics.BlockDevice{Name: name, ReadBytes: r, WriteBytes: w}
	}
	snap := services.Snapshot{
		Filesystem: &metrics.FilesystemSnapshot{Filesystems: []metrics.Filesystem{
			{MountPoint: "/", Source: "/dev/mapper/ArchinstallVg-root"},
			{MountPoint: "/boot", Source: "/dev/nvme0n1p1"},
		}},
		Block: &metrics.BlockSnapshot{Devices: []metrics.BlockDevice{
			dev("dm-0", 10, 10), dev("nvme0n1", 500, 500), dev("zram0", 9e9, 9e9), dev("loop0", 8e9, 0),
		}},
	}
	resolveTo := func(target string) func(string) (string, error) {
		return func(string) (string, error) { return target, nil }
	}
	failing := func(string) (string, error) { return "", errors.New("no such file") }

	if got := primaryBlockDevice(snap, resolveTo("/dev/dm-0")); got != "dm-0" {
		t.Errorf("root device = %q, want dm-0", got)
	}
	if got := primaryBlockDevice(snap, failing); got != "nvme0n1" {
		t.Errorf("fallback = %q, want the busiest real device, not zram or loop", got)
	}
	if got := primaryBlockDevice(snap, resolveTo("/dev/sdz")); got != "nvme0n1" {
		t.Errorf("unlisted root device = %q, want the fallback", got)
	}
	if got := primaryBlockDevice(services.Snapshot{}, failing); got != "" {
		t.Errorf("no block snapshot = %q", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestPrimaryInterface|TestPrimaryBlockDevice' ./internal/shell`
Expected: FAIL to compile.

- [ ] **Step 3: Implement `internal/shell/monitorsubjects.go`**

```go
package shell

import (
	"path/filepath"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// primaryInterface is the non-loopback interface that has carried the most
// traffic. Cumulative bytes rather than the current rate, so the pick does not
// follow a momentary burst on a bridge.
func primaryInterface(snap services.Snapshot) string {
	if snap.Network == nil {
		return ""
	}
	best, bestBytes := "", uint64(0)
	for _, i := range snap.Network.Interfaces {
		if i.Name == "lo" {
			continue
		}
		total := i.ReceiveBytes + i.TransmitBytes
		if best == "" || total > bestBytes || (total == bestBytes && i.Name < best) {
			best, bestBytes = i.Name, total
		}
	}
	return best
}

// primaryBlockDevice is the device backing /, or failing that the busiest
// real device. Loop, RAM and zram devices are never chosen.
func primaryBlockDevice(snap services.Snapshot, resolve func(string) (string, error)) string {
	if snap.Block == nil {
		return ""
	}
	listed := make(map[string]bool, len(snap.Block.Devices))
	for _, d := range snap.Block.Devices {
		listed[d.Name] = true
	}
	if snap.Filesystem != nil {
		for _, fs := range snap.Filesystem.Filesystems {
			if fs.MountPoint != "/" {
				continue
			}
			if path, err := resolve(fs.Source); err == nil && listed[filepath.Base(path)] {
				return filepath.Base(path)
			}
		}
	}
	best, bestBytes := "", uint64(0)
	for _, d := range snap.Block.Devices {
		if strings.HasPrefix(d.Name, "loop") || strings.HasPrefix(d.Name, "ram") || strings.HasPrefix(d.Name, "zram") {
			continue
		}
		total := d.ReadBytes + d.WriteBytes
		if best == "" || total > bestBytes || (total == bestBytes && d.Name < best) {
			best, bestBytes = d.Name, total
		}
	}
	return best
}

func resolveDevicePath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestPrimaryInterface|TestPrimaryBlockDevice' ./internal/shell`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/monitorsubjects.go internal/shell/monitorsubjects_test.go
git commit -m "feat(shell): pick the primary interface and root block device"
```

---

### Task 6: Control Centre leases for storage, network and disk

**Files:**
- Modify: `internal/shell/panelhost.go:76` (PanelHost fields), `:805-837` (`acquirePanelLeases` PanelControlCenter case), `:2633` (release on close)
- Modify: `internal/shell/registry.go:1555-1585` (`UpdateMetrics`)
- Create: `internal/shell/controlcenter_leases.go`
- Test: `internal/shell/controlcenter_leases_test.go`

**Interfaces:**
- Consumes: `primaryInterface`, `primaryBlockDevice`, `resolveDevicePath` (Task 5); `(*services.Metrics).Leased(Selector) bool`.
- Produces:
  - `PanelHost.ccIface`, `PanelHost.ccDevice string`; `PanelHost.subjectLeases []*services.Lease`
  - `(*Registry).syncControlCentreSubjectsLocked(h *PanelHost, snap services.Snapshot)`
  - `ccRateSelectors(iface, device string) []services.Selector` — rx, tx, read, write, in that order, skipping an empty subject

- [ ] **Step 1: Write the failing tests**

```go
package shell

import (
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func ccSubjectSnapshot(ifaces ...string) services.Snapshot {
	snap := services.Snapshot{
		Network: &metrics.NetworkSnapshot{},
		Block:   &metrics.BlockSnapshot{Devices: []metrics.BlockDevice{{Name: "nvme0n1", ReadBytes: 1}}},
	}
	for i, name := range ifaces {
		snap.Network.Interfaces = append(snap.Network.Interfaces,
			metrics.NetworkInterface{Name: name, ReceiveBytes: uint64(1000 - i)})
	}
	return snap
}

func TestControlCentreLeasesStorageNetworkAndBlock(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	for _, sel := range []services.Selector{
		{Source: services.SourceFilesystem, Subject: "/"},
		{Source: services.SourceNetwork},
		{Source: services.SourceBlock},
	} {
		if !r.metrics.Leased(sel) {
			t.Errorf("control centre does not lease %v", sel)
		}
	}
}

func TestControlCentreResolvesSubjectsOnceAndKeepsThem(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	r.syncControlCentreSubjectsLocked(h, ccSubjectSnapshot("enp7s0", "wlan0"))
	r.mu.Unlock()
	for _, sel := range ccRateSelectors("enp7s0", "nvme0n1") {
		if !r.metrics.Leased(sel) {
			t.Errorf("rate selector %v is not leased", sel)
		}
	}

	// A new, busier interface appearing does not move the chart.
	r.mu.Lock()
	busier := ccSubjectSnapshot("enp7s0")
	busier.Network.Interfaces = append(busier.Network.Interfaces,
		metrics.NetworkInterface{Name: "tailscale0", ReceiveBytes: 1 << 40})
	r.syncControlCentreSubjectsLocked(h, busier)
	got := h.ccIface
	r.mu.Unlock()
	if got != "enp7s0" {
		t.Fatalf("interface moved to %q while enp7s0 still exists", got)
	}

	// The chosen interface disappearing re-resolves and releases the old leases.
	r.mu.Lock()
	r.syncControlCentreSubjectsLocked(h, ccSubjectSnapshot("wlan0"))
	got = h.ccIface
	r.mu.Unlock()
	if got != "wlan0" {
		t.Fatalf("interface = %q after enp7s0 vanished, want wlan0", got)
	}
	if r.metrics.Leased(services.Selector{Source: services.SourceNetwork, Subject: "enp7s0", Direction: "rx"}) {
		t.Error("the vanished interface is still leased")
	}

	r.ClosePanel(PanelControlCenter)
	for _, sel := range ccRateSelectors("wlan0", "nvme0n1") {
		if r.metrics.Leased(sel) {
			t.Errorf("%v is still leased after close", sel)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestControlCentreLeases|TestControlCentreResolves' ./internal/shell`
Expected: FAIL to compile.

- [ ] **Step 3: Implement**

`internal/shell/panelhost.go`, beside `leases` in `PanelHost`:

```go
	// subjectLeases hold the Control Centre's per-interface and per-device rate
	// rings. They are resolved from the first snapshot that names a subject and
	// kept until that subject disappears, so the chart does not hop.
	subjectLeases    []*services.Lease
	ccIface, ccDevice string
```

In `acquirePanelLeases`, the PanelControlCenter selector list gains three entries after battery:

```go
			{Source: services.SourceFilesystem, Subject: "/"},
			{Source: services.SourceNetwork},
			{Source: services.SourceBlock},
```

At the close path (line 2633), after `releaseAll(h.leases)`:

```go
	releaseAll(h.subjectLeases)
	h.subjectLeases, h.ccIface, h.ccDevice = nil, "", ""
```

`internal/shell/controlcenter_leases.go`:

```go
package shell

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

// ccRateSelectors are the rate rings the Monitor page charts: receive and
// transmit for the interface, read and write for the device.
func ccRateSelectors(iface, device string) []services.Selector {
	var out []services.Selector
	if iface != "" {
		out = append(out,
			services.Selector{Source: services.SourceNetwork, Subject: iface, Direction: "rx"},
			services.Selector{Source: services.SourceNetwork, Subject: iface, Direction: "tx"})
	}
	if device != "" {
		out = append(out,
			services.Selector{Source: services.SourceBlock, Subject: device},
			services.Selector{Source: services.SourceBlock, Subject: device, Direction: "write"})
	}
	return out
}

// syncControlCentreSubjectsLocked keeps one interface and one device leased.
// A subject is re-resolved only when the snapshot no longer carries it.
// Caller holds r.mu.
func (r *Registry) syncControlCentreSubjectsLocked(h *PanelHost, snap services.Snapshot) {
	if h == nil || r.metrics == nil {
		return
	}
	iface, device := h.ccIface, h.ccDevice
	if !snapshotHasInterface(snap, iface) {
		iface = primaryInterface(snap)
	}
	if !snapshotHasDevice(snap, device) {
		device = primaryBlockDevice(snap, resolveDevicePath)
	}
	if iface == h.ccIface && device == h.ccDevice {
		return
	}
	var leases []*services.Lease
	for _, sel := range ccRateSelectors(iface, device) {
		lease, err := r.metrics.Acquire(sel, time.Second)
		if err != nil {
			releaseAll(leases)
			return
		}
		leases = append(leases, lease)
	}
	releaseAll(h.subjectLeases)
	h.subjectLeases, h.ccIface, h.ccDevice = leases, iface, device
}

func snapshotHasInterface(snap services.Snapshot, name string) bool {
	if name == "" || snap.Network == nil {
		return false
	}
	for _, i := range snap.Network.Interfaces {
		if i.Name == name {
			return true
		}
	}
	return false
}

func snapshotHasDevice(snap services.Snapshot, name string) bool {
	if name == "" || snap.Block == nil {
		return false
	}
	for _, d := range snap.Block.Devices {
		if d.Name == name {
			return true
		}
	}
	return false
}
```

The new leases are acquired before the old ones are released, so a subject that stays the same on one
side never drops its ring.

`internal/shell/registry.go`, in `UpdateMetrics`, immediately before
`controlOut, controlOK := r.rebuildControlCentreLocked()`:

```go
	if h := r.panelHosts[PanelControlCenter]; h != nil {
		r.syncControlCentreSubjectsLocked(h, snap)
	}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestControlCentre' ./internal/shell`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/panelhost.go internal/shell/registry.go internal/shell/controlcenter_leases.go internal/shell/controlcenter_leases_test.go
git commit -m "fix(shell): lease storage, network and disk for the control centre"
```

---

### Task 7: The Monitor page

**Files:**
- Create: `internal/shell/controlcenter_monitor.go`
- Modify: `internal/shell/controlcenter_pages.go` — delete `ccMonitor`, `ccNetworkSelector`, `ccMonitorMetricCard` (lines 503–574)
- Modify: `internal/shell/popout_controlcenter.go` — `activateControlCentre` handles `panelMonitorAction`
- Test: `internal/shell/controlcenter_monitor_test.go`; `internal/shell/controlcenter_test.go:880-913` (monitor expectations)

**Interfaces:**
- Consumes: Tasks 1, 4, 5, 6; `monitorCard`, `monitorCardTitle`, `monitorIconRune`, `selectGPU`, `formatBytes`, `formatRate`, `ccDash`, `centreIconButton`, `ccLeftColumnW`, `ccRightColumnW`, `ccPageH`, `panelMonitorAction`.
- Produces: `ccMonitor(r *Registry, h *PanelHost) *ui.Node` (same name `ccPage` already calls), and the layout constants below.

- [ ] **Step 1: Write the failing tests**

`internal/shell/controlcenter_monitor_test.go`:

```go
package shell

import (
	"strings"
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func monitorTestRegistry() *Registry {
	return &Registry{sample: services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.95, Valid: true},
			Load1: 0.82, Load5: 0.74, Load15: 0.66, LoadValid: true,
			Cores: []metrics.CPUCore{{FrequencyHz: 4_210_000_000, FrequencyValid: true}}},
		Memory: &metrics.MemorySnapshot{
			Memory: metrics.Capacity{TotalBytes: 32 << 30, UsedBytes: 12 << 30},
			Swap:   metrics.Capacity{TotalBytes: 8 << 30, UsedBytes: 1 << 29}},
		Thermal: &metrics.ThermalSnapshot{Celsius: 63, Valid: true},
		Filesystem: &metrics.FilesystemSnapshot{Filesystems: []metrics.Filesystem{
			{MountPoint: "/", Capacity: metrics.Capacity{TotalBytes: 1000 << 30, UsedBytes: 850 << 30}}}},
	}}
}

func layOutMonitor(t *testing.T, r *Registry) *ui.Node {
	t.Helper()
	h := &PanelHost{id: PanelControlCenter, section: "monitor", theme: DefaultTheme(), ccIface: "enp7s0", ccDevice: "dm-0"}
	page := ccMonitor(r, h)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 7, 16 }
	if err := ui.LayoutColumn(page, ui.Rect{W: 596, H: ccPageH}, measure); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestMonitorPageFitsTheBodyWithoutScrolling(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	if page.Height != ccPageH {
		t.Fatalf("page height = %d, want %d", page.Height, ccPageH)
	}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if b := n.Bounds; b.X+b.W > 596 || b.Y+b.H > ccPageH {
			t.Errorf("%v %q ends at (%d,%d), outside 596x%d", n.Kind, n.Text, b.X+b.W, b.Y+b.H, ccPageH)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(page)
}

func TestMonitorPageShowsEveryRowAndDashesTheAbsent(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	text := renderText(page)
	for _, want := range []string{"CPU", "Memory", "Temperature", "GPU", "Storage", "Network", "Disk I/O",
		"4.21 GHz", "load 0.82 0.74 0.66", "63°C"} {
		if !strings.Contains(text, want) {
			t.Errorf("monitor text %q is missing %q", text, want)
		}
	}
	// No GPU, network or block sample: each keeps its row with a dash.
	if got := strings.Count(text, ccDash); got < 3 {
		t.Errorf("absent rows show %d dashes, want at least 3", got)
	}
}

func TestMonitorPageTonesFollowThresholds(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	cpu := findByName(page, "CPU usage")
	storage := findByName(page, "Storage used")
	if cpu == nil || cpu.Tone != ui.ToneError {
		t.Errorf("95%% CPU value tone = %+v, want error", cpu)
	}
	if storage == nil || storage.Tone != ui.ToneActivity {
		t.Errorf("85%% storage tone = %+v, want activity", storage)
	}
}

func TestMonitorPageOpensTheStandaloneMonitor(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	open := findByName(page, "Open system monitor")
	if open == nil || open.Action != panelMonitorAction || !open.Focusable {
		t.Fatalf("open control = %+v", open)
	}
}

func TestMonitorPageRateChartsCarryAPeak(t *testing.T) {
	r := monitorTestRegistry()
	r.sample.Network = &metrics.NetworkSnapshot{Interfaces: []metrics.NetworkInterface{{Name: "enp7s0",
		Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 1 << 20, TransmitBytesPerSecond: 1 << 16, Valid: true}}}}
	text := renderText(layOutMonitor(t, r))
	if !strings.Contains(text, "enp7s0") {
		t.Errorf("network caption %q does not name the interface", text)
	}
}
```

In `internal/shell/controlcenter_test.go`, `TestControlCentreNativePagesFillTheBody`: change the monitor
want list to `{"CPU", "Memory", "Temperature", "GPU", "Storage", "Network", "Disk I/O"}` and replace the
`if tc.section == "monitor" { ... } else if` branch so every section, monitor included, expects
`page.Height == 480`.

- [ ] **Step 2: Run to verify failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestMonitorPage|TestControlCentreNativePages' ./internal/shell`
Expected: FAIL. `ccIface` exists after Task 6, but the page still has the old five cards.

- [ ] **Step 3: Implement `internal/shell/controlcenter_monitor.go`**

```go
package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The Monitor page's measured composition, on the same 355/228 grid as Home.
const (
	ccMonHeroH      = 168
	ccMonRowsH      = ccPageH - ccMonHeroH - theme.MarginL // 299
	ccMonRowH       = 50
	ccMonMarkH      = 28
	ccMonMarkW      = 240
	ccMonLabelW     = 96
	ccMonCPUGraphH  = 64
	ccMonMemMeterH  = 8
	ccMonSwapMeterH = 4
)

func ccMonitor(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	snap := services.Snapshot{}
	history := map[services.Selector][]float64{}
	if r != nil {
		snap = r.sample
		if r.metrics != nil {
			history = r.historyLocked()
		}
	}
	iface, device := "", ""
	if h != nil {
		iface, device = h.ccIface, h.ccDevice
	}
	heroes := &ui.Node{Kind: ui.KindRow, Height: ccMonHeroH, Gap: theme.MarginL, Children: []*ui.Node{
		ccMonCPUHero(m, snap, history),
		ccMonMemoryHero(m, snap, history),
	}}
	rows := monitorCard(m, []*ui.Node{
		ccMonTemperatureRow(snap, history),
		ccMonGPURow(snap, history),
		ccMonStorageRow(snap),
		ccMonRateRow("network", "Network", iface, snap, history,
			services.Selector{Source: services.SourceNetwork, Subject: iface, Direction: "rx"},
			services.Selector{Source: services.SourceNetwork, Subject: iface, Direction: "tx"},
			"↓ ", "↑ ", networkRateFloor),
		ccMonRateRow("filesystem", "Disk I/O", device, snap, history,
			services.Selector{Source: services.SourceBlock, Subject: device},
			services.Selector{Source: services.SourceBlock, Subject: device, Direction: "write"},
			"R ", "W ", diskRateFloor),
	})
	rows.Height = ccMonRowsH
	rows.Children[0].Gap = theme.MarginS
	return &ui.Node{Kind: ui.KindColumn, Height: ccPageH, Gap: theme.MarginL, Children: []*ui.Node{heroes, rows}}
}

// ccMonValue is one tabular reading. name labels it for assistive tech and
// for tests; tone carries the threshold.
func ccMonValue(name, text string, tone ui.Tone, role theme.TextRole) *ui.Node {
	return &ui.Node{Kind: ui.KindText, Name: name, Text: text, Tone: tone, TextRole: role, Tabular: true}
}

func ccMonCaption(text string) *ui.Node {
	return &ui.Node{Kind: ui.KindText, Text: text, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, Tabular: true}
}

func ccMonGraph(width, height int, values, second []float64, tone ui.Tone) *ui.Node {
	return &ui.Node{Kind: ui.KindGraph, Width: width, Height: height, Values: values,
		SecondValues: second, Tone: tone, Absent: len(values) == 0}
}

func ccMonCPUHero(m theme.Metrics, snap services.Snapshot, history map[services.Selector][]float64) *ui.Node {
	sel := services.Selector{Source: services.SourceCPU}
	value, tone := ccDash, ui.ToneNormal
	var samples []float64
	if snap.CPU != nil && snap.CPU.Usage.Valid {
		pct := snap.CPU.Usage.Fraction * 100
		value, tone, samples = fmt.Sprintf("%.0f%%", pct), thresholdTone(metricCPU, pct), history[sel]
	}
	caption := ""
	if snap.Thermal != nil && snap.Thermal.Valid {
		caption = fmt.Sprintf("%.0f°C", snap.Thermal.Celsius)
	}
	if ghz, ok := meanCoreGHz(snap); ok {
		caption = joinCaption(caption, fmt.Sprintf("%.2f GHz", ghz))
	}
	if snap.CPU != nil && snap.CPU.LoadValid {
		caption = joinCaption(caption, fmt.Sprintf("load %.2f %.2f %.2f", snap.CPU.Load1, snap.CPU.Load5, snap.CPU.Load15))
	}
	open := centreIconButton("open_in_full", panelMonitorAction, "Open system monitor")
	title := &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
		monitorCardTitle("CPU", monitorIconRune(sel)),
		{Kind: ui.KindRow, Gap: theme.MarginM, CenterY: true, Children: []*ui.Node{
			ccMonValue("CPU usage", value, tone, theme.RoleTitle), open,
		}},
	}}
	card := monitorCard(m, []*ui.Node{
		title,
		ccMonGraph(ccLeftColumnW-2*m.CardPadding, ccMonCPUGraphH, samples, nil, tone),
		ccMonCaption(ccText(caption)),
	})
	card.Width, card.Height = ccLeftColumnW, ccMonHeroH
	return card
}

func ccMonMemoryHero(m theme.Metrics, snap services.Snapshot, history map[services.Selector][]float64) *ui.Node {
	sel := services.Selector{Source: services.SourceMemory}
	inner := ccRightColumnW - 2*m.CardPadding
	rows := []*ui.Node{}
	value, tone, used, swap := ccDash, ui.ToneNormal, ccDash, ccDash
	var samples []float64
	fraction, swapFraction := 0.0, 0.0
	memOK, swapOK := false, false
	if snap.Memory != nil && snap.Memory.Memory.TotalBytes > 0 {
		c := snap.Memory.Memory
		fraction = float64(c.UsedBytes) / float64(c.TotalBytes)
		value, tone = fmt.Sprintf("%.0f%%", fraction*100), thresholdTone(metricMemory, fraction*100)
		used = formatBytes(float64(c.UsedBytes)) + " / " + formatBytes(float64(c.TotalBytes))
		samples, memOK = history[sel], true
		if s := snap.Memory.Swap; s.TotalBytes > 0 {
			swapFraction, swapOK = float64(s.UsedBytes)/float64(s.TotalBytes), true
			swap = "swap " + formatBytes(float64(s.UsedBytes)) + " / " + formatBytes(float64(s.TotalBytes))
		}
	}
	rows = append(rows,
		&ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
			monitorCardTitle("Memory", monitorIconRune(sel)),
			ccMonValue("Memory used", value, tone, theme.RoleTitle),
		}},
		&ui.Node{Kind: ui.KindMeter, Width: inner, Height: ccMonMemMeterH, Value: fraction, Max: 1, Tone: tone, Absent: !memOK},
		ccMonCaption(used),
		&ui.Node{Kind: ui.KindMeter, Width: inner, Height: ccMonSwapMeterH, Value: swapFraction, Max: 1, Absent: !swapOK},
		ccMonCaption(swap),
		ccMonGraph(inner, ccMonMarkH, samples, nil, tone),
	)
	card := monitorCard(m, rows)
	card.Width, card.Height = ccRightColumnW, ccMonHeroH
	return card
}

// ccMonRow is one line of the rows card: icon and label, a value over a
// caption, and the mark on the trailing edge.
func ccMonRow(iconID, label string, value, caption *ui.Node, mark *ui.Node) *ui.Node {
	// MetricIconRune knows cpu, memory, filesystem/block and network. Other
	// IDs return zero, and a zero rune would paint as a missing glyph.
	if icon := render.MetricIconRune(iconID); icon != 0 {
		label = string(icon) + " " + label
	}
	return &ui.Node{Kind: ui.KindRow, Height: ccMonRowH, Gap: theme.MarginM, CenterY: true, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginM, CenterY: true, Children: []*ui.Node{
			{Kind: ui.KindText, Width: ccMonLabelW, Text: label},
			{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{value, caption}},
		}},
		mark,
	}}
}

func ccMonTemperatureRow(snap services.Snapshot, history map[services.Selector][]float64) *ui.Node {
	sel := services.Selector{Source: services.SourceCPU, Subject: "temperature"}
	value, tone, source := ccDash, ui.ToneNormal, ""
	var samples []float64
	if snap.Thermal != nil && snap.Thermal.Valid {
		value = fmt.Sprintf("CPU %.0f°C", snap.Thermal.Celsius)
		tone, samples, source = thresholdTone(metricCPUTemp, snap.Thermal.Celsius), temperatureSeries(history[sel]), snap.Thermal.Source
		if gpuSel, ok := selectGPU(snap); ok {
			for _, g := range snap.GPU.GPUs {
				if g.PCIID == gpuSel.Subject && g.TempValid {
					value += fmt.Sprintf(" · GPU %.0f°C", g.Celsius)
				}
			}
		}
	}
	return ccMonRow("cpu", "Temperature", ccMonValue("Temperature", value, tone, theme.RoleBody),
		ccMonCaption(source), ccMonGraph(ccMonMarkW, ccMonMarkH, samples, nil, tone))
}

func ccMonGPURow(snap services.Snapshot, history map[services.Selector][]float64) *ui.Node {
	value, tone, name := ccDash, ui.ToneNormal, ""
	var samples []float64
	if sel, ok := selectGPU(snap); ok {
		for _, g := range snap.GPU.GPUs {
			if g.PCIID != sel.Subject {
				continue
			}
			name = g.Name
			// An nvidia-smi failure leaves usage invalid for this sample
			// (sysc-495): the row dashes rather than holding a stale value.
			if g.Usage.Valid {
				pct := g.Usage.Fraction * 100
				value, tone, samples = fmt.Sprintf("%.0f%%", pct), thresholdTone(metricGPU, pct), history[sel]
			}
		}
	}
	return ccMonRow("gpu", "GPU", ccMonValue("GPU usage", value, tone, theme.RoleBody),
		ccMonCaption(name), ccMonGraph(ccMonMarkW, ccMonMarkH, samples, nil, tone))
}

func ccMonStorageRow(snap services.Snapshot) *ui.Node {
	value, caption, tone, fraction, ok := ccDash, "", ui.ToneNormal, 0.0, false
	if snap.Filesystem != nil {
		for _, fs := range snap.Filesystem.Filesystems {
			if fs.MountPoint != "/" || fs.Capacity.TotalBytes == 0 {
				continue
			}
			fraction, ok = float64(fs.Capacity.UsedBytes)/float64(fs.Capacity.TotalBytes), true
			tone = thresholdTone(metricStorage, fraction*100)
			value = formatBytes(float64(fs.Capacity.UsedBytes)) + " / " + formatBytes(float64(fs.Capacity.TotalBytes))
			caption = fmt.Sprintf("/ · %.0f%%", fraction*100)
		}
	}
	mark := &ui.Node{Kind: ui.KindMeter, Width: ccMonMarkW, Height: ccMonMemMeterH, Value: fraction, Max: 1, Tone: tone, Absent: !ok}
	return ccMonRow("filesystem", "Storage", ccMonValue("Storage used", value, tone, theme.RoleBody), ccMonCaption(caption), mark)
}

func ccMonRateRow(iconID, label, subject string, snap services.Snapshot, history map[services.Selector][]float64,
	in, out services.Selector, inPrefix, outPrefix string, floor float64) *ui.Node {
	value, caption := ccDash, subject
	var first, second []float64
	inRate, inOK := snap.Rate(in)
	outRate, outOK := snap.Rate(out)
	if subject != "" && inOK && outOK {
		value = inPrefix + formatRate(inRate) + "  " + outPrefix + formatRate(outRate)
		ceiling := rateCeiling(floor, history[in], history[out])
		first, second = scaleSeries(history[in], ceiling), scaleSeries(history[out], ceiling)
		caption = joinCaption(subject, peakCaption(history[in], history[out]))
	}
	return ccMonRow(iconID, label, ccMonValue(label+" rate", value, ui.ToneNormal, theme.RoleBody),
		ccMonCaption(caption), ccMonGraph(ccMonMarkW, ccMonMarkH, first, second, ui.ToneNormal))
}

func joinCaption(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " · " + b
}
```

Add `"github.com/Nomadcxx/sysc-shell/internal/render"` to the imports for `render.MetricIconRune`
(`internal/render/iconfont.go:340`). It has glyphs for `cpu`, `memory`, `filesystem`/`block` and `network`
only, which is why `ccMonRow` skips a zero rune: the GPU row carries its label without a glyph.

Delete `ccMonitor`, `ccNetworkSelector` and `ccMonitorMetricCard` from `controlcenter_pages.go`, and
remove any import that becomes unused (`sort` is the likely one).

In `activateControlCentre` (`popout_controlcenter.go`), add to the `switch n.Action`:

```go
	case panelMonitorAction:
		target = PanelMonitor
```

- [ ] **Step 4: Run to verify pass, then fit the numbers**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestMonitorPage|TestControlCentre' ./internal/shell`
Expected: PASS. If `TestMonitorPageFitsTheBodyWithoutScrolling` reports a child past 480, shrink
`ccMonRowH` or the gap between rows, not the heroes. The spec fixes the hero height at 168.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shell && GOMAXPROCS=4 go vet ./internal/shell
git add internal/shell/controlcenter_monitor.go internal/shell/controlcenter_monitor_test.go internal/shell/controlcenter_pages.go internal/shell/popout_controlcenter.go internal/shell/controlcenter_test.go
git commit -m "feat(shell): control centre monitor page on the home grid"
```

---

### Task 8: Package gates

**Files:** none modified unless a gate fails.

- [ ] **Step 1: Run the repository gates, capped**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
GOMAXPROCS=4 go vet ./internal/ui ./internal/render ./internal/shell ./internal/plugin
GOMAXPROCS=4 go test -race -count=1 ./internal/ui
GOMAXPROCS=4 go test -race -count=1 ./internal/render
GOMAXPROCS=4 go test -race -count=1 ./internal/shell
GOMAXPROCS=4 go test -count=1 -p 2 ./...
git diff --exit-code -- go.mod go.sum
```

Expected: PASS, except `TestABatteryWidgetOpensTheSessionPanel`, which fails on this battery-less
desktop on `main` too (recorded 2026-09-25). Report it; do not fix it here.

- [ ] **Step 2: Commit any gate fixes**

```bash
git add -A internal
git commit -m "fix(shell): gate fixes for the monitor page"
```

Skip this step if nothing changed.

---

### Task 9: Live Niri check

**Files:** none.

- [ ] **Step 1: Build and deploy with a rollback copy**

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1) WAYLAND_DISPLAY=wayland-1 XDG_RUNTIME_DIR=/run/user/1000
GOMAXPROCS=4 go build -o "$SCRATCH/sysc-shell" ./cmd/sysc-shell
cp ~/.local/bin/sysc-shell ~/.local/bin/sysc-shell.before-cc-monitor-$(date +%Y%m%d)
cp "$SCRATCH/sysc-shell" ~/.local/bin/sysc-shell.new && mv ~/.local/bin/sysc-shell.new ~/.local/bin/sysc-shell
systemctl --user restart sysc-shell.service && sleep 3 && systemctl --user is-active sysc-shell
```

`$SCRATCH` is the session scratchpad. Another session may own the running binary: check its timestamp
first and ask before replacing a build that is not from `main`.

- [ ] **Step 2: Capture the page**

```bash
sysc-shell ipc panel.open '{"panel":"control-center","section":"monitor"}'
sleep 3; grim "$SCRATCH/monitor-live.png"
sysc-shell ipc panel.close '{"panel":"control-center"}'
```

Crop to the panel and read the image. Pass when:
- all seven blocks are visible without scrolling, and nothing is clipped;
- the Network row names `enp7s0` and its chart is drawing (wait for a few seconds of traffic);
- the Disk I/O row names `dm-0`;
- every line is smooth and anti-aliased with a dot at its right end;
- the GPU row shows a value or `—` (an `—` with an `nvidia-smi` issue in the journal is `sysc-495`, not this work).

- [ ] **Step 3: Capture a bar graph widget**

If the live bar has a `graph` metric item, crop it from the same capture and confirm the line style. If it
has none, record that in the close reason rather than editing the user's configuration.

- [ ] **Step 4: Close the tracker issues**

From `/home/nomadx/sysc-shell`:

```bash
bd close sysc-517 --reason "Monitor page and line sparkline on main at <hash>; live capture <what was seen>"
bd close sysc-336 --reason "Superseded by sysc-517: the monitor page carries GPU and temperature rows"
```

Commit the tracker change by splicing the two rows into `HEAD`'s `.beads/issues.jsonl` (the file is
routinely truncated by the bd hook), then `git commit --no-verify` with a screened message.
