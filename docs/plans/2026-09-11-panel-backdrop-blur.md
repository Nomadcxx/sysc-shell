# Panel backdrop blur Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Paint floating panels over a blurred capture of what sits behind them, so panel opacity can drop below the 80 percent floor that exists only because the shell cannot blur.

**Architecture:** Bind `zwlr_screencopy_manager_v1` as an *optional* global. When a panel opens, capture its logical rect once — before any of its surfaces exist — into a one-off `wl_shm` buffer, downsample by four, run three box passes per axis, and keep the result at quarter resolution. The painter composites it beneath the existing rounded root fill, scaling it during the blit the frame already pays for. Nothing recurs: no timer, no per-frame cost, no frame loop.

**Tech Stack:** Go 1.26.4, `sysc-wayland v0.2.1` and its `sysc-wayland-scanner@v0.1.1`, `wl_shm` ARGB8888, `golang.org/x/sys/unix` for memfd.

**Spec:** `docs/plans/2026-09-11-panel-backdrop-blur-design.md`

## Global Constraints

- **Never run `go test ./...` or any `-race` build.** A repo-wide race build exhausts memory on this machine. Run named tests in one package: `go test ./internal/render -run TestBlur`.
- Go only. No CGO, no C, no new module beyond what is listed here.
- Wayland types stay inside `internal/platform/wayland`. The shell asks for a backdrop; it never sees a protocol object.
- The Wayland dispatch loop stays on one goroutine. Other goroutines submit commands and receive immutable state through channels.
- Draw only after invalidation. This slice adds no continuous frame loop.
- Panel surfaces only. Not the bar, not toasts, not the OSD, not tooltips.
- `bd` runs only from `/home/nomadx/sysc-shell`. Committing from a worktree needs `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db`.
- **The `commit-msg` hook rejects these substrings, case-insensitively:** `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`, `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `gpt-[0-9]`, `llm`, `ai assistant`, `bot`, `agent`. Ordinary words trip it — `both` contains `bot`. Screen every message before committing:
  ```bash
  grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
  ```
  Do **not** add a `Co-Authored-By` trailer. Do **not** use `--no-verify`.
- Every commit also carries `.beads/issues.jsonl`, staged by the repository's own `pre-commit` hook whether or not you add it. That is intended.

## File Structure

| Path | Responsibility |
|---|---|
| `protocols/wlr-screencopy-unstable-v1.xml` | Upstream protocol source, new |
| `internal/platform/wayland/screencopy/generate.go` | Scanner directive, upstream revision and SHA-256, new |
| `internal/platform/wayland/screencopy/screencopy.go` | Generated binding, new, never hand-edited |
| `internal/platform/wayland/capture.go` | One-off capture: buffer, frame, copy, ready. New |
| `internal/render/blur.go` | Downsample and box blur over premultiplied ARGB. New, pure, no Wayland |
| `internal/render/style.go` | `Backdrop` on `Style` |
| `internal/render/paint.go` | Composite the backdrop beneath `rootFill` |
| `internal/render/image.go` | Bilinear sampling path for the backdrop only |
| `internal/platform/wayland/registry.go` | `interfaceMaximum` gains screencopy; `requiredSingletons` does not |
| `internal/platform/wayland/client.go` | `owner.screencopy` field, bound in `bindGlobals` |
| `internal/platform/wayland/aux.go` | Capture at the top of `openAux`, before any surface exists |
| `internal/theme/profile.go` | `OpacityMinBlurred`, `BlurBehind`, `BlurRadius` on `Composition` |
| `internal/shell/theme.go` | `opacityAlpha` takes the lower floor when a backdrop is present |
| `internal/config/load.go` | Wire fields and clamp entries |
| `internal/settings/registry.go` | Two settings rows |
| `docs/plans/2026-08-26-sysc-shell-design.md` | Rendering section and open-gate amendments |

---

### Task 1: Vendor the protocol and generate the binding

**Files:**
- Create: `protocols/wlr-screencopy-unstable-v1.xml`
- Create: `internal/platform/wayland/screencopy/generate.go`
- Create: `internal/platform/wayland/screencopy/screencopy.go` (generated)

**Interfaces:**
- Consumes: nothing.
- Produces: `screencopy.NewZwlrScreencopyManagerV1(ctx *client.Context) *ZwlrScreencopyManagerV1`, its `CaptureOutputRegion(overlayCursor int32, output *client.Output, x, y, width, height int32) (*ZwlrScreencopyFrameV1, error)`, and on the frame `SetBufferHandler`, `SetBufferDoneHandler`, `SetFlagsHandler`, `SetReadyHandler`, `SetFailedHandler`, `Copy(buffer *client.Buffer) error`, `Destroy() error`.

- [ ] **Step 1: Fetch the upstream XML and verify its digest**

```bash
curl -sL "https://gitlab.freedesktop.org/wlroots/wlr-protocols/-/raw/master/unstable/wlr-screencopy-unstable-v1.xml" \
  -o protocols/wlr-screencopy-unstable-v1.xml
sha256sum protocols/wlr-screencopy-unstable-v1.xml
```

Expected: `131b8f9b4aad0c8a9cf705e90d2a1511a5ca0c477637fd3400cf1cc4fa963fb8`. If it differs, the upstream file moved — stop and record the new digest in `generate.go` rather than proceeding silently.

- [ ] **Step 2: Write the generator directive**

Create `internal/platform/wayland/screencopy/generate.go`, modelled exactly on `internal/platform/wayland/layershell/generate.go`:

```go
// Package screencopy holds the generated wlr-screencopy binding.
//
// Upstream: https://gitlab.freedesktop.org/wlroots/wlr-protocols
// unstable/wlr-screencopy-unstable-v1.xml. It provides
// zwlr_screencopy_manager_v1 version 3, which is the version Niri 26.04
// advertises.
// SHA-256:  131b8f9b4aad0c8a9cf705e90d2a1511a5ca0c477637fd3400cf1cc4fa963fb8
package screencopy

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg screencopy -o screencopy.go -i ../../../../protocols/wlr-screencopy-unstable-v1.xml
```

- [ ] **Step 3: Generate**

Run: `cd internal/platform/wayland/screencopy && go generate ./...`
Expected: `screencopy.go` appears and `go build ./internal/platform/wayland/screencopy` succeeds.

If the scanner rejects the XML, stop. The design assumed this generates mechanically because `layershell` and `fractionalscale` do; a failure here is new information and invalidates that assumption.

- [ ] **Step 4: Confirm it compiles and exposes the region request**

```bash
go build ./internal/platform/wayland/screencopy
grep -n "func (i \*ZwlrScreencopyManagerV1) CaptureOutputRegion" internal/platform/wayland/screencopy/screencopy.go
```

Expected: build succeeds; the grep prints one line.

- [ ] **Step 5: Commit**

```bash
git add protocols/wlr-screencopy-unstable-v1.xml internal/platform/wayland/screencopy/
git commit -m "feat(wayland): generate the screencopy binding"
```

---

### Task 2: Bind the manager as an optional global

**Files:**
- Modify: `internal/platform/wayland/registry.go:21-31` (`interfaceMaximum`)
- Modify: `internal/platform/wayland/client.go:177-195` (`owner` struct)
- Modify: `internal/platform/wayland/client.go` (`bindGlobals`)
- Test: `internal/platform/wayland/registry_test.go`

**Interfaces:**
- Consumes: Task 1's `screencopy.NewZwlrScreencopyManagerV1`.
- Produces: `owner.screencopy *screencopy.ZwlrScreencopyManagerV1`, nil when the compositor does not advertise it.

- [ ] **Step 1: Write the failing test**

Add to `internal/platform/wayland/registry_test.go`:

```go
func TestScreencopyIsKnownButNotRequired(t *testing.T) {
	t.Parallel()
	// Known, so addGlobal records it and the manager can bind.
	if _, ok := bindVersion("zwlr_screencopy_manager_v1", 3); !ok {
		t.Fatal("screencopy is not a known interface; addGlobal will drop it")
	}
	// Not required, so a compositor without it still starts. Blur is
	// decoration: its absence must degrade to an opaque panel, never to a
	// refusal to run.
	for _, iface := range requiredSingletons {
		if iface == "zwlr_screencopy_manager_v1" {
			t.Fatal("screencopy is in requiredSingletons; a compositor without it would fail to start")
		}
	}
}

func TestScreencopyBindsAtOurMaximum(t *testing.T) {
	t.Parallel()
	// The server may offer a higher version later; we bind what we generated.
	if got, _ := bindVersion("zwlr_screencopy_manager_v1", 9); got != 3 {
		t.Errorf("bind version = %d, want 3", got)
	}
	if got, _ := bindVersion("zwlr_screencopy_manager_v1", 2); got != 2 {
		t.Errorf("server below our maximum should bind at the server version, got %d", got)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/platform/wayland -run TestScreencopy -v`
Expected: FAIL — `screencopy is not a known interface`.

- [ ] **Step 3: Add the interface maximum**

In `internal/platform/wayland/registry.go`, add one entry to `interfaceMaximum` and change nothing else:

```go
	"wp_cursor_shape_manager_v1":     1,
	"zwlr_screencopy_manager_v1":     3,
}
```

Do **not** add it to `requiredSingletons`. That omission is the whole mechanism: `missingRequired` never names it, so a compositor without it starts normally and `r.singletons` simply lacks the entry.

- [ ] **Step 4: Run the test and watch it pass**

Run: `go test ./internal/platform/wayland -run TestScreencopy -v`
Expected: PASS.

- [ ] **Step 5: Bind the manager when it is advertised**

In `internal/platform/wayland/client.go`, add the field to `owner`:

```go
	viewporter *viewporter.WpViewporter
	// screencopy is nil when the compositor does not advertise it. Blur is
	// decoration; its absence is not an error.
	screencopy *screencopy.ZwlrScreencopyManagerV1
```

In `bindGlobals`, construct and register it only when present. It is deliberately not in the `singletons` table above, because every entry there is required:

```go
	if _, ok := o.rs.singletons["zwlr_screencopy_manager_v1"]; ok {
		o.screencopy = screencopy.NewZwlrScreencopyManagerV1(ctx)
		if err := o.bindSingleton("zwlr_screencopy_manager_v1", o.screencopy); err != nil {
			return err
		}
	}
```

If `bindGlobals` binds its table inline rather than through a helper, extract the per-interface bind into `bindSingleton(iface string, proxy client.Proxy) error` first and have the existing loop call it, so this task adds one caller rather than a second binding path.

- [ ] **Step 6: Build and run the package tests**

Run: `go build ./internal/platform/wayland && go test ./internal/platform/wayland -run 'TestScreencopy|TestRegistry' -v`
Expected: build succeeds, tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/wayland/
git commit -m "feat(wayland): bind screencopy when the compositor offers it"
```

---

### Task 3: The blur kernel

Pure, no Wayland, no protocol. Written before the capture so the expensive half is proven independently.

**Files:**
- Create: `internal/render/blur.go`
- Test: `internal/render/blur_test.go`

**Interfaces:**
- Consumes: `ui.Image{Width, Height, Stride, Pix}`.
- Produces: `func Blur(src *ui.Image, factor, radius int) *ui.Image` — returns an image of `src.Width/factor` by `src.Height/factor`, or nil for a nil or degenerate source.

- [ ] **Step 1: Write the failing tests**

Create `internal/render/blur_test.go`:

```go
package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// solid builds a uniform premultiplied image.
func solid(w, h int, r, g, b, a byte) *ui.Image {
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = b, g, r, a
	}
	return img
}

func TestBlurLeavesAUniformFieldUnchanged(t *testing.T) {
	t.Parallel()
	// A blur of a constant is that constant. If the edge handling wraps or
	// reads outside the buffer, this is where it shows.
	src := solid(64, 64, 0x20, 0x30, 0x40, 0xff)
	got := Blur(src, 4, 24)
	if got == nil {
		t.Fatal("Blur returned nil for a valid source")
	}
	for i := 0; i < len(got.Pix); i += 4 {
		if got.Pix[i] != 0x40 || got.Pix[i+1] != 0x30 || got.Pix[i+2] != 0x20 || got.Pix[i+3] != 0xff {
			t.Fatalf("pixel %d = %v, want the source colour", i/4, got.Pix[i:i+4])
		}
	}
}

func TestBlurReducesByTheFactor(t *testing.T) {
	t.Parallel()
	got := Blur(solid(64, 32, 1, 2, 3, 0xff), 4, 24)
	if got.Width != 16 || got.Height != 8 {
		t.Fatalf("size = %dx%d, want 16x8", got.Width, got.Height)
	}
	if got.Stride != got.Width*4 {
		t.Errorf("stride = %d, want %d", got.Stride, got.Width*4)
	}
}

func TestBlurSpreadsASinglePixel(t *testing.T) {
	t.Parallel()
	// One bright pixel in a dark field must leak into its neighbours, and the
	// result must stay inside the source's range.
	src := solid(32, 32, 0, 0, 0, 0xff)
	o := (16*32 + 16) * 4
	src.Pix[o], src.Pix[o+1], src.Pix[o+2] = 0xff, 0xff, 0xff
	got := Blur(src, 1, 3)
	centre := (16*32 + 16) * 4
	near := (16*32 + 17) * 4
	if got.Pix[near] == 0 {
		t.Error("the neighbour stayed black; nothing spread")
	}
	if got.Pix[near] > got.Pix[centre] {
		t.Error("the neighbour is brighter than the centre")
	}
}

func TestBlurIsIndependentOfRadius(t *testing.T) {
	t.Parallel()
	// The sliding window means cost does not grow with radius. Correctness
	// must not either: a uniform field is uniform at any radius.
	for _, r := range []int{1, 8, 64} {
		got := Blur(solid(32, 32, 9, 9, 9, 0xff), 1, r)
		if got.Pix[0] != 9 {
			t.Errorf("radius %d changed a uniform field to %d", r, got.Pix[0])
		}
	}
}

func TestBlurRejectsDegenerateInput(t *testing.T) {
	t.Parallel()
	if Blur(nil, 4, 8) != nil {
		t.Error("nil source should return nil")
	}
	if Blur(solid(2, 2, 0, 0, 0, 0xff), 4, 8) != nil {
		t.Error("a source smaller than the factor should return nil, not a zero-sized image")
	}
	short := &ui.Image{Width: 8, Height: 8, Stride: 32, Pix: make([]byte, 16)}
	if Blur(short, 1, 2) != nil {
		t.Error("a buffer shorter than its declared geometry should return nil")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/render -run TestBlur -v`
Expected: FAIL — `undefined: Blur`.

- [ ] **Step 3: Implement the kernel**

Create `internal/render/blur.go`:

```go
package render

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// Blur downsamples src by factor, then runs three box passes per axis at a
// proportionally reduced radius, and returns the reduced image.
//
// There is deliberately no upsample pass. The caller hands the reduced image
// to the blit it already pays for. Measured 2026-09-11 on a 1120x960 panel,
// adding a full-resolution upsample turns an 8.8x saving into 2.6x, because
// the upsample is itself a full-resolution pass with four taps per pixel.
//
// A blur is a low-pass filter, so the detail discarded by downsampling is
// detail the blur would have destroyed. Premultiplied ARGB averages linearly,
// which is why the canvas stores premultiplied pixels, so neither resample
// needs an un-premultiply round trip.
func Blur(src *ui.Image, factor, radius int) *ui.Image {
	if src == nil || factor < 1 || src.Width < factor || src.Height < factor {
		return nil
	}
	if src.Stride < src.Width*4 || len(src.Pix) < src.Height*src.Stride {
		return nil
	}
	dst := downsample(src, factor)
	if radius < 1 {
		return dst
	}
	r := radius / factor
	if r < 1 {
		r = 1
	}
	scratch := &ui.Image{Width: dst.Width, Height: dst.Height, Stride: dst.Stride,
		Pix: make([]byte, len(dst.Pix))}
	for i := 0; i < 3; i++ {
		boxH(dst, scratch, r)
		boxV(scratch, dst, r)
	}
	return dst
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// downsample box-averages by an integer factor.
func downsample(src *ui.Image, k int) *ui.Image {
	dw, dh := src.Width/k, src.Height/k
	dst := &ui.Image{Width: dw, Height: dh, Stride: dw * 4, Pix: make([]byte, dw*dh*4)}
	n := k * k
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sum [4]int
			for j := 0; j < k; j++ {
				row := (y*k + j) * src.Stride
				for i := 0; i < k; i++ {
					o := row + (x*k+i)*4
					sum[0] += int(src.Pix[o])
					sum[1] += int(src.Pix[o+1])
					sum[2] += int(src.Pix[o+2])
					sum[3] += int(src.Pix[o+3])
				}
			}
			o := y*dst.Stride + x*4
			dst.Pix[o] = byte(sum[0] / n)
			dst.Pix[o+1] = byte(sum[1] / n)
			dst.Pix[o+2] = byte(sum[2] / n)
			dst.Pix[o+3] = byte(sum[3] / n)
		}
	}
	return dst
}

// boxH runs one horizontal box pass with a sliding window, so cost does not
// grow with radius. Edges clamp rather than wrap: a wrapped backdrop bleeds the
// opposite edge of the capture into the panel corner.
func boxH(src, dst *ui.Image, r int) {
	win := r*2 + 1
	for y := 0; y < src.Height; y++ {
		row := y * src.Stride
		var sum [4]int
		for i := -r; i <= r; i++ {
			o := row + clampInt(i, 0, src.Width-1)*4
			sum[0] += int(src.Pix[o])
			sum[1] += int(src.Pix[o+1])
			sum[2] += int(src.Pix[o+2])
			sum[3] += int(src.Pix[o+3])
		}
		for x := 0; x < src.Width; x++ {
			o := row + x*4
			dst.Pix[o] = byte(sum[0] / win)
			dst.Pix[o+1] = byte(sum[1] / win)
			dst.Pix[o+2] = byte(sum[2] / win)
			dst.Pix[o+3] = byte(sum[3] / win)
			out := row + clampInt(x-r, 0, src.Width-1)*4
			in := row + clampInt(x+r+1, 0, src.Width-1)*4
			sum[0] += int(src.Pix[in]) - int(src.Pix[out])
			sum[1] += int(src.Pix[in+1]) - int(src.Pix[out+1])
			sum[2] += int(src.Pix[in+2]) - int(src.Pix[out+2])
			sum[3] += int(src.Pix[in+3]) - int(src.Pix[out+3])
		}
	}
}

// boxV is the vertical counterpart. It is a separate function rather than a
// transpose because a transpose costs two extra full passes over the buffer.
func boxV(src, dst *ui.Image, r int) {
	win := r*2 + 1
	for x := 0; x < src.Width; x++ {
		col := x * 4
		var sum [4]int
		for i := -r; i <= r; i++ {
			o := clampInt(i, 0, src.Height-1)*src.Stride + col
			sum[0] += int(src.Pix[o])
			sum[1] += int(src.Pix[o+1])
			sum[2] += int(src.Pix[o+2])
			sum[3] += int(src.Pix[o+3])
		}
		for y := 0; y < src.Height; y++ {
			o := y*src.Stride + col
			dst.Pix[o] = byte(sum[0] / win)
			dst.Pix[o+1] = byte(sum[1] / win)
			dst.Pix[o+2] = byte(sum[2] / win)
			dst.Pix[o+3] = byte(sum[3] / win)
			out := clampInt(y-r, 0, src.Height-1)*src.Stride + col
			in := clampInt(y+r+1, 0, src.Height-1)*src.Stride + col
			sum[0] += int(src.Pix[in]) - int(src.Pix[out])
			sum[1] += int(src.Pix[in+1]) - int(src.Pix[out+1])
			sum[2] += int(src.Pix[in+2]) - int(src.Pix[out+2])
			sum[3] += int(src.Pix[in+3]) - int(src.Pix[out+3])
		}
	}
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `go test ./internal/render -run TestBlur -v`
Expected: all PASS.

- [ ] **Step 5: Record the cost on this machine**

```bash
cat > /tmp/blur_bench_test.go.note <<'NOTE'
Add a temporary benchmark, run it, record the number in the commit body, delete it.
NOTE
go test ./internal/render -run TestBlur -count=1
```

Add `BenchmarkBlurAudioPanel` timing `Blur(solid(1120, 960, ...), 4, 24)`, run `go test ./internal/render -bench BenchmarkBlur -run '^$' -benchtime 20x`, and record the ns/op in the commit message. The design predicts ~4.6 ms. A result far above that invalidates the cost model and should stop the plan for re-review rather than continue.

- [ ] **Step 6: Commit**

```bash
git add internal/render/blur.go internal/render/blur_test.go
git commit -m "feat(render): add the reduced-resolution box blur"
```

---

### Task 4: Bilinear sampling for the backdrop

**Files:**
- Modify: `internal/render/image.go`
- Test: `internal/render/image_test.go`

**Interfaces:**
- Consumes: `ui.Image`.
- Produces: `func paintImageSmooth(c *Canvas, box ui.Rect, img *ui.Image)` — same contract as `paintImage` but bilinear.

- [ ] **Step 1: Write the failing test**

Add to `internal/render/image_test.go`:

```go
func TestPaintImageSmoothInterpolatesBetweenPixels(t *testing.T) {
	t.Parallel()
	// A quarter-resolution backdrop scaled up 4x with nearest sampling bands
	// visibly across a large flat panel. Two source pixels scaled up must
	// produce intermediate values between them, not a hard step.
	src := &ui.Image{Width: 2, Height: 1, Stride: 8, Pix: []byte{
		0, 0, 0, 0xff,
		0xff, 0xff, 0xff, 0xff,
	}}
	c := NewCanvas(16, 1)
	paintImageSmooth(c, ui.Rect{W: 16, H: 1}, src)

	var seen int
	for x := 0; x < 16; x++ {
		v := c.Pix[x*4]
		if v != 0x00 && v != 0xff {
			seen++
		}
	}
	if seen == 0 {
		t.Error("no intermediate values; sampling is not bilinear")
	}
}

func TestPaintImageSmoothKeepsNearestForIcons(t *testing.T) {
	t.Parallel()
	// paintImage must not change. The icon worker produces the exact size the
	// node asked for, and resampling there would be a second, worse scaler.
	src := &ui.Image{Width: 2, Height: 1, Stride: 8, Pix: []byte{
		0, 0, 0, 0xff,
		0xff, 0xff, 0xff, 0xff,
	}}
	c := NewCanvas(16, 1)
	paintImage(c, ui.Rect{W: 16, H: 1}, src)
	for x := 0; x < 16; x++ {
		if v := c.Pix[x*4]; v != 0x00 && v != 0xff {
			t.Fatalf("paintImage interpolated at x=%d (%d); it must stay nearest", x, v)
		}
	}
}
```

If `NewCanvas` is spelled differently in this package, use the existing constructor — check the top of `canvas.go` and adjust these three call sites only.

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/render -run TestPaintImageSmooth -v`
Expected: FAIL — `undefined: paintImageSmooth`.

- [ ] **Step 3: Implement it beside `paintImage`**

Append to `internal/render/image.go`:

```go
// paintImageSmooth composites a raster with bilinear sampling.
//
// paintImage stays nearest-neighbour on purpose: the icon worker produces the
// size the node asked for. A backdrop is the opposite case — one capture scaled
// to whatever the panel measures — and nearest banding is visible across a
// large flat ground. This is a separate entry point rather than a flag so the
// icon contract is untouched.
func paintImageSmooth(c *Canvas, box ui.Rect, img *ui.Image) {
	if img == nil || img.Width <= 0 || img.Height <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	if img.Stride < img.Width*4 || len(img.Pix) < img.Height*img.Stride {
		return
	}
	x0, y0, x1, y1 := c.clip(box)
	for y := y0; y < y1; y++ {
		fy := ((y-box.Y)*img.Height*256)/box.H - 128
		sy0 := clampInt(fy>>8, 0, img.Height-1)
		sy1 := clampInt(sy0+1, 0, img.Height-1)
		wy := fy & 255
		if fy < 0 {
			wy = 0
		}
		for x := x0; x < x1; x++ {
			fx := ((x-box.X)*img.Width*256)/box.W - 128
			sx0 := clampInt(fx>>8, 0, img.Width-1)
			sx1 := clampInt(sx0+1, 0, img.Width-1)
			wx := fx & 255
			if fx < 0 {
				wx = 0
			}
			o00 := sy0*img.Stride + sx0*4
			o01 := sy0*img.Stride + sx1*4
			o10 := sy1*img.Stride + sx0*4
			o11 := sy1*img.Stride + sx1*4
			var px [4]byte
			for ch := 0; ch < 4; ch++ {
				top := int(img.Pix[o00+ch])*(256-wx) + int(img.Pix[o01+ch])*wx
				bot := int(img.Pix[o10+ch])*(256-wx) + int(img.Pix[o11+ch])*wx
				px[ch] = byte((top*(256-wy) + bot*wy) >> 16)
			}
			if px[3] == 0 {
				continue
			}
			dst := y*c.Stride + x*4
			if dst+4 > len(c.Pix) {
				continue
			}
			blendPixel(c.Pix[dst:dst+4], px, uint32(px[3]))
		}
	}
}
```

- [ ] **Step 4: Run and watch both pass**

Run: `go test ./internal/render -run TestPaintImage -v`
Expected: PASS, including the existing `paintImage` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/render/image.go internal/render/image_test.go
git commit -m "feat(render): add bilinear sampling for scaled backgrounds"
```

---

### Task 5: Carry the backdrop on `Style` and composite it

**Files:**
- Modify: `internal/render/style.go` (the `Style` struct, beside `SurfaceOpacity`)
- Modify: `internal/render/paint.go:181`
- Test: `internal/render/paint_test.go`

**Interfaces:**
- Consumes: Task 3's `Blur` output shape, Task 4's `paintImageSmooth`.
- Produces: `Style.Backdrop *ui.Image`.

- [ ] **Step 1: Write the failing test**

Add to `internal/render/paint_test.go`:

```go
func TestBackdropPaintsBeneathTheRootFill(t *testing.T) {
	t.Parallel()
	// The root fill is translucent, so the backdrop must show through it. A
	// nil backdrop must paint exactly as before.
	style := testStyle(t)
	style.SurfaceOpacity = 0x80
	style.Backdrop = solid(4, 4, 0xff, 0x00, 0x00, 0xff)

	withBackdrop := paintRootTo(t, style)
	style.Backdrop = nil
	without := paintRootTo(t, style)

	if withBackdrop == without {
		t.Fatal("the backdrop changed nothing; it is not being composited")
	}
}

func TestNilBackdropIsTodaysPaint(t *testing.T) {
	t.Parallel()
	style := testStyle(t)
	style.Backdrop = nil
	if got := paintRootTo(t, style); got == "" {
		t.Fatal("a nil backdrop must paint the ordinary opaque root")
	}
}
```

Use whatever style fixture and canvas-to-string helper `paint_test.go` already defines; `testStyle` and `paintRootTo` are placeholders for those existing helpers. Read the top of `paint_test.go` and substitute the real names rather than adding new helpers.

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/render -run TestBackdrop -v`
Expected: FAIL — `style.Backdrop undefined`.

- [ ] **Step 3: Add the field**

In `internal/render/style.go`, immediately after `SurfaceOpacity`:

```go
	// Backdrop is a blurred capture of what sat behind this surface when it
	// opened, held at reduced resolution and scaled during the blit. Nil is
	// today's paint: an opaque root over whatever the compositor shows.
	Backdrop *ui.Image
```

- [ ] **Step 4: Composite it**

In `internal/render/paint.go`, immediately before the existing `c.FillRounded(box, radius, style.rootFill())` at line 181:

```go
	if style.Backdrop != nil {
		paintImageSmooth(c, box, style.Backdrop)
	}
	c.FillRounded(box, radius, style.rootFill())
```

The backdrop is painted into the same `box` and then covered by the rounded fill, so the panel's corners stay transparent: the fill defines the shape, and any backdrop pixel outside the rounded body is overpainted by the surrounding clear.

If that leaves backdrop pixels visible in the corners, clip the backdrop to the rounded path instead — but check the rendered corners before adding that complexity, because `applyAuxRegions` already documents that a panel's corners are genuinely transparent.

- [ ] **Step 5: Run and watch it pass**

Run: `go test ./internal/render -run 'TestBackdrop|TestNilBackdrop|TestPaint' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/render/style.go internal/render/paint.go internal/render/paint_test.go
git commit -m "feat(render): composite a panel backdrop beneath the root fill"
```

---

### Task 6: Capture one region into a shm buffer

**Files:**
- Create: `internal/platform/wayland/capture.go`
- Test: `internal/platform/wayland/capture_test.go`

**Interfaces:**
- Consumes: `owner.screencopy` from Task 2, `newGeneration` from `shm.go:34`.
- Produces: `func (o *owner) captureRegion(out *client.Output, r ui.Rect) *ui.Image` — nil on any failure, never an error, because a missing backdrop is decoration.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/wayland/capture_test.go`:

```go
package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestCaptureWithoutTheManagerReturnsNil(t *testing.T) {
	t.Parallel()
	// A compositor without screencopy must produce an opaque panel, not a
	// failure. This is the whole of D12.
	o := &owner{}
	if got := o.captureRegion(nil, ui.Rect{W: 100, H: 100}); got != nil {
		t.Error("capture without a manager returned an image")
	}
}

func TestCaptureRejectsADegenerateRegion(t *testing.T) {
	t.Parallel()
	o := &owner{}
	for _, r := range []ui.Rect{{}, {W: 0, H: 10}, {W: 10, H: 0}, {W: -1, H: 5}} {
		if got := o.captureRegion(nil, r); got != nil {
			t.Errorf("capture of %+v returned an image", r)
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/platform/wayland -run TestCapture -v`
Expected: FAIL — `o.captureRegion undefined`.

- [ ] **Step 3: Implement the capture**

Create `internal/platform/wayland/capture.go`. The shape, with the protocol round trip made explicit:

```go
package wayland

// captureRegion copies one output region through zwlr_screencopy_v1 and
// returns it as a premultiplied ARGB image.
//
// It returns nil rather than an error on every failure path. A backdrop is
// decoration: a panel that cannot get one paints opaque, and must never fail
// to open because the compositor declined a copy.
//
// The region is in output logical coordinates, which is the same space as
// Placement, so no conversion happens here. The returned buffer is in output
// buffer pixels; the caller reconciles that through Scale120.
func (o *owner) captureRegion(out *client.Output, r ui.Rect) *ui.Image {
	if o == nil || o.screencopy == nil || out == nil {
		return nil
	}
	if r.W <= 0 || r.H <= 0 {
		return nil
	}

	// overlayCursor 0: a frozen pointer in the backdrop is an artefact.
	frame, err := o.screencopy.CaptureOutputRegion(0, out,
		int32(r.X), int32(r.Y), int32(r.W), int32(r.H))
	if err != nil {
		return nil
	}
	defer func() { _ = frame.Destroy() }()

	var (
		gen      *generation
		width    int32
		height   int32
		stride   int32
		gotSpec  bool
		done     bool
		ok       bool
		yInvert  bool
	)

	frame.SetBufferHandler(func(e screencopy.ZwlrScreencopyFrameV1BufferEvent) {
		// Take the first ARGB8888 offer and ignore the rest.
		if gotSpec || e.Format != formatARGB8888 {
			return
		}
		width, height, stride, gotSpec = int32(e.Width), int32(e.Height), int32(e.Stride), true
	})
	frame.SetBufferDoneHandler(func(screencopy.ZwlrScreencopyFrameV1BufferDoneEvent) { done = true })
	frame.SetFlagsHandler(func(e screencopy.ZwlrScreencopyFrameV1FlagsEvent) {
		yInvert = e.Flags&uint32(screencopy.ZwlrScreencopyFrameV1FlagsYInvert) != 0
	})
	frame.SetReadyHandler(func(screencopy.ZwlrScreencopyFrameV1ReadyEvent) { ok, done = true, true })
	frame.SetFailedHandler(func(screencopy.ZwlrScreencopyFrameV1FailedEvent) { ok, done = false, true })

	// Round trip until the buffer offer arrives, then allocate and copy.
	if !o.pumpUntil(&done, func() bool { return gotSpec }) || !gotSpec {
		return nil
	}
	done = false

	gen, err = newGeneration(o.shm, captureGenerationID, width, height)
	if err != nil {
		return nil
	}
	defer func() { _ = gen.destroy() }()

	if err := frame.Copy(gen.slots[0]); err != nil {
		return nil
	}
	if !o.pumpUntil(&done, func() bool { return done }) || !ok {
		return nil
	}

	src := gen.pixels(0)
	img := &ui.Image{Width: int(width), Height: int(height), Stride: int(stride),
		Pix: make([]byte, len(src))}
	if yInvert {
		rows := int(height)
		for y := 0; y < rows; y++ {
			copy(img.Pix[y*int(stride):(y+1)*int(stride)],
				src[(rows-1-y)*int(stride):(rows-y)*int(stride)])
		}
	} else {
		copy(img.Pix, src)
	}
	return img
}
```

`pumpUntil` is a small helper that calls `o.display.Roundtrip()` until a predicate holds or a bounded number of trips elapse, returning false on timeout. Write it in this file; it must be bounded, because a compositor that never answers must not hang the dispatch goroutine. `captureGenerationID` is a constant distinct from any surface generation id.

Adjust the event and flag type names to whatever Task 1's generator actually emitted — read `screencopy.go` and use those names rather than these.

- [ ] **Step 4: Run and watch the tests pass**

Run: `go build ./internal/platform/wayland && go test ./internal/platform/wayland -run TestCapture -v`
Expected: PASS.

- [ ] **Step 5: Measure the copy on this machine**

This is the design's largest open risk (D16.1): the copy is a GPU-to-CPU readback and is entirely unmeasured. Add a temporary log of the wall time from `CaptureOutputRegion` to `ready` for a 1120x960 region, run the shell live, record the number, and remove the log.

**If the copy exceeds roughly 15 ms, stop and re-review.** The design's cost model assumes it is small relative to the 4.6 ms blur, and a large readback changes the shape of the answer.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/wayland/capture.go internal/platform/wayland/capture_test.go
git commit -m "feat(wayland): capture one output region into a buffer"
```

---

### Task 7: Capture on panel open, before any surface exists

**Files:**
- Modify: `internal/platform/wayland/aux.go` (`openAux`, before `o.compositor.CreateSurface()`)
- Modify: `internal/platform/wayland/aux.go` (`AuxSpec`)
- Test: `internal/platform/wayland/aux_test.go`

**Interfaces:**
- Consumes: Task 6's `captureRegion`, Task 3's `Blur`.
- Produces: `AuxSpec.BlurRegion *ui.Rect` and `AuxSpec.BlurRadius int`; the resulting image reaches the surface through `HostCallbacks`.

- [ ] **Step 1: Write the failing test**

```go
func TestAuxCaptureHappensBeforeTheSurfaceExists(t *testing.T) {
	t.Parallel()
	// Screencopy captures the composited output. If the panel's own surface
	// exists when the copy runs, the panel is blurred into its own backdrop.
	// The shield maps alongside it, so the capture must precede both.
	var order []string
	o := &owner{}
	o.captureHook = func() { order = append(order, "capture") }
	o.createHook = func() { order = append(order, "create") }

	_ = o.openAuxForTest(&AuxSpec{ID: "panel:test", Namespace: "sysc", BlurRegion: &ui.Rect{W: 10, H: 10}})

	if len(order) < 2 || order[0] != "capture" {
		t.Fatalf("order = %v, want capture before create", order)
	}
}
```

If `owner` has no seam for this, add the two function fields as unexported test hooks defaulting to nil, called at the two points. A nil hook costs one comparison and keeps the ordering assertable without a live compositor — the ordering is the design's stated risk and must be tested, not assumed.

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/platform/wayland -run TestAuxCapture -v`
Expected: FAIL.

- [ ] **Step 3: Add the spec fields and the capture**

In `AuxSpec`:

```go
	// BlurRegion is the output-logical rect to capture behind this surface
	// before it is created. Nil disables the backdrop.
	BlurRegion *ui.Rect
	BlurRadius int
```

At the top of `openAux`, after the existing validation and before `o.compositor.CreateSurface()`:

```go
	// Before any surface exists. Screencopy captures the composited output,
	// so a capture taken after this panel or its shield maps would blur the
	// panel into its own backdrop.
	if spec.BlurRegion != nil {
		if shot := o.captureRegion(h.output, *spec.BlurRegion); shot != nil {
			u.backdrop = render.Blur(shot, backdropDownsample, spec.BlurRadius)
		}
	}
```

`backdropDownsample` is 4. `u.backdrop` is a new field on `surfaceUnit`, handed to the painter wherever the unit's `Style` is assembled.

- [ ] **Step 4: Run and watch it pass**

Run: `go test ./internal/platform/wayland -run TestAux -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/wayland/aux.go internal/platform/wayland/aux_test.go
git commit -m "feat(wayland): capture a panel backdrop before its surface exists"
```

---

### Task 8: Lift the opacity floor when a backdrop is present

**Files:**
- Modify: `internal/theme/profile.go` (the `OpacityMin` const block)
- Modify: `internal/shell/theme.go:250-270` (`resolveSurfaces`, `opacityAlpha`)
- Test: `internal/shell/theme_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `theme.OpacityMinBlurred`, and `opacityAlpha(percent int, blurred bool) uint8`.

- [ ] **Step 1: Write the failing test**

```go
func TestOpacityFloorLiftsOnlyWithABackdrop(t *testing.T) {
	t.Parallel()
	// The 80 floor exists only because the shell had no blur behind text.
	// With a backdrop it may go lower; without one the reason still holds.
	if got := opacityAlpha(50, false); got != opacityAlpha(theme.OpacityMin, false) {
		t.Errorf("without a backdrop, 50%% must clamp to the %d%% floor", theme.OpacityMin)
	}
	if got := opacityAlpha(50, true); got == opacityAlpha(theme.OpacityMin, true) {
		t.Error("with a backdrop, 50% should not clamp to the unblurred floor")
	}
	if theme.OpacityMinBlurred >= theme.OpacityMin {
		t.Error("the blurred floor must be lower than the unblurred one")
	}
}

func TestHighContrastStaysOpaqueEvenWithABackdrop(t *testing.T) {
	t.Parallel()
	// High contrast forces full opacity. Blur does not change that: it exists
	// to make text unambiguous, and a backdrop is still content behind text.
	s := resolveSurfaces(theme.Composition{PanelOpacity: 50, BlurBehind: true}, true)
	if s.Panel != 0xff {
		t.Errorf("panel alpha under high contrast = %#x, want 0xff", s.Panel)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/shell -run TestOpacity -v`
Expected: FAIL — `OpacityMinBlurred` undefined.

- [ ] **Step 3: Add the floor and thread the flag**

In `internal/theme/profile.go`, beside `OpacityMin`:

```go
	// OpacityMinBlurred is the floor when a surface paints over a blurred
	// backdrop. The unblurred floor of 80 exists only because wallpaper detail
	// reads through a label; a blurred ground removes that detail, so the
	// limit becomes taste rather than legibility.
	OpacityMinBlurred = 60
```

In `internal/shell/theme.go`, give `opacityAlpha` the flag and pass it from `resolveSurfaces`. High contrast still returns opaque before either call is reached, so that path is untouched.

- [ ] **Step 4: Run and watch it pass**

Run: `go test ./internal/shell -run 'TestOpacity|TestHighContrast|TestSurface' -v`
Expected: PASS.

Note: `go test ./internal/shell` is safe to run. `runArgvDefault` (`popout_session.go:270`) refuses under `testing.Testing()`, so no test can reach a real `loginctl`.

- [ ] **Step 5: Commit**

```bash
git add internal/theme/profile.go internal/shell/theme.go internal/shell/theme_test.go
git commit -m "feat(theme): lower the opacity floor behind a blurred backdrop"
```

---

### Task 9: Configuration and settings

**Files:**
- Modify: `internal/theme/profile.go` (`Composition`)
- Modify: `internal/config/load.go:99-101` and `:1062-1064`
- Modify: `internal/settings/registry.go:74-76`
- Test: `internal/config/load_test.go`

**Interfaces:**
- Consumes: Task 8's floor.
- Produces: `Composition.BlurBehind bool`, `Composition.BlurRadius int`; wire keys `blur-behind`, `blur-radius`.

- [ ] **Step 1: Write the failing test**

```go
func TestBlurConfigRoundTripsAndClamps(t *testing.T) {
	t.Parallel()
	cfg := mustLoadString(t, `{"appearance":{"blur-behind":true,"blur-radius":999}}`)
	if !cfg.Theme.BlurBehind {
		t.Error("blur-behind did not load")
	}
	if cfg.Theme.BlurRadius > blurRadiusMax {
		t.Errorf("blur-radius = %d, want clamped to %d", cfg.Theme.BlurRadius, blurRadiusMax)
	}
}

func TestBlurDefaultsOff(t *testing.T) {
	t.Parallel()
	// It ships off. The live gate flips the default, not this slice.
	if Default().Theme.BlurBehind {
		t.Error("blur is on by default; it must ship off until the live gate runs")
	}
}
```

Use the package's existing config-loading helper rather than `mustLoadString` if one exists.

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/config -run TestBlur -v`
Expected: FAIL.

- [ ] **Step 3: Add the axes**

Add `BlurBehind bool` and `BlurRadius int` to `theme.Composition` beside `PanelOpacity`. Add the wire fields and a clamp entry modelled on the existing opacity rows at `load.go:1062-1064`, with bounds `0` to `blurRadiusMax = 64`. Add two rows to `internal/settings/registry.go` beside the opacity rows.

- [ ] **Step 4: Run and watch it pass**

Run: `go test ./internal/config -run TestBlur -v && go test ./internal/settings`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/theme/profile.go internal/config/ internal/settings/
git commit -m "feat(config): add the backdrop blur axes"
```

---

### Task 10: Amend the architecture document and close the gate

**Files:**
- Modify: `docs/plans/2026-08-26-sysc-shell-design.md` (Rendering, L172-196; open gates, L372)
- Modify: `docs/roadmap.md` (Milestone 8)
- Modify: `.beads/issues.jsonl` via `bd`

- [ ] **Step 1: Amend the rendering section**

In the Rendering section, after the damage paragraph, record that a panel backdrop is captured once per open through `zwlr_screencopy_manager_v1` and blurred on the CPU at reduced resolution, that it is paid per open rather than per frame, and that full-buffer damage is unchanged.

At L372, the open gate "Measure shared-memory rendering before deciding whether to add EGL/OpenGL ES" is satisfied **for blurred panels** and cites this design. It stays open for animation frame time, image-heavy grids, and CPU/power — those are the rendering-smoothness design's.

L19 needs no change. A static backdrop introduces no frame loop.

- [ ] **Step 2: Amend the roadmap**

In Milestone 8, record that "large blurred panels" is measured and resolved in favour of `wl_shm`, citing the measured figures from Task 3 Step 5 and Task 6 Step 5.

- [ ] **Step 3: Record the outcome in bd**

```bash
cd /home/nomadx/sysc-shell
bd create "Panel backdrop blur: live Niri gate" --deps discovered-from:sysc-202
bd export -o .beads/issues.jsonl
```

- [ ] **Step 4: Run the live gate**

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Build to the scratchpad and run it. Stop an old scratch binary by pid from `pgrep -f 'scratchpad/<name>'`; never `pkill -f` a name you also typed.

Confirm, with `blur-behind` on:

1. The Audio panel opens over a maximised window and shows that window blurred.
2. The same panel over bare wallpaper shows the wallpaper blurred.
3. Panel opacity below 80 is legible.
4. Closing and reopening re-captures.
5. `blur-behind` off is pixel-identical to before this slice.
6. A 60-minute idle run shows no continuous redraw.
7. `niri msg -j layers` before and after closing shows no leaked surface.

Record scale-1.0 results. This machine has one output at 3440x1440; the two-output case is unrunnable here — record it as such and do not claim it.

- [ ] **Step 5: Commit**

```bash
git add docs/ .beads/issues.jsonl
git commit -m "docs: record the measured backdrop blur outcome"
```

---

## Self-Review

**Spec coverage.** D1 source → Tasks 1, 2, 6. D2 static per open → Task 7. D3 region capture → Task 6. D4 reduced resolution, no upsample → Task 3. D5 bilinear → Task 4. D6 capture before map → Task 7. D7 scale contract → Task 6 returns buffer pixels, the caller reconciles. D8 opacity floor → Task 8. D9 generated binding → Task 1. D10 amendment → Task 10. D11 config → Task 9. D12 silent failure → Tasks 2 and 6 tests. D13 panels only → Task 7 touches `openAux` alone. D14 testing → every task. D15 tracker → Task 10. D16 risks → Task 3 Step 5 and Task 6 Step 5 are explicit stop-and-re-review gates.

**Placeholders.** None. Two tasks say "use the existing helper and substitute the real name" — that is an instruction to read neighbouring code, not a deferred decision, and the surrounding code is named exactly.

**Type consistency.** `Blur(src *ui.Image, factor, radius int) *ui.Image` in Task 3 is called with that signature in Task 7. `paintImageSmooth(c, box, img)` in Task 4 is called identically in Task 5. `captureRegion(out, r) *ui.Image` in Task 6 is called with that signature in Task 7. `opacityAlpha(percent int, blurred bool)` in Task 8 matches its test. `Style.Backdrop` is added in Task 5 and read in Task 5's paint site.
