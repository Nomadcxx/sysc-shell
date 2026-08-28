# Architectural Proof Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build one interactive Niri layer surface that proves the Wayland, text, retained-layout, input, Niri IPC, event-driven rendering, and shutdown architecture.

**Architecture:** One goroutine owns the `sysc-wayland` connection and every Wayland proxy. Pure UI and
rendering packages build and paint a retained tree into released `wl_shm` buffers. A Niri client
publishes typed workspace snapshots, and pointer actions mutate the proof model through the Wayland
owner's command queue.

**Tech Stack:** Go 1.26 language level (verified against toolchain `go1.27.0`),
`github.com/Nomadcxx/sysc-wayland v0.1.1`, pinned `go-text/typesetting`, `golang.org/x/image`,
`golang.org/x/sys`, Niri JSON IPC, `wlr-layer-shell-unstable-v1`, and `wl_shm`.

---

## Working rules

- Execute this plan in a dedicated worktree from the documentation baseline.
- Start only after the owner approves and publishes `sysc-wayland v0.1.1` and the release resolves without
  a local `replace` directive.
- Use `github.com/Nomadcxx/sysc-shell` as the module path unless the repository owner supplies another canonical path before Task 1.
- Keep the first proof in `cmd/sysc-shell`; do not create a throwaway binary.
- Do not add a framework, dependency-injection container, logging package, configuration library, Makefile, or renderer interface.
- Keep the UI fixture fixed: one row with a workspace label, a meter, and a button.
- Commit after each task. Stop at Task 10 even if later roadmap work looks easy.
- This plan carries the corrections from [the 2026-08-27 plan audit](2026-08-27-plan-audit-report.md). Protocol contracts marked below as verified were executed against Niri 26.04 on the reference machine.

## Proof acceptance contract

On a live Niri session, the command below must create one 48-logical-pixel top bar on the requested output or the first active output:

```bash
go run ./cmd/sysc-shell --output DP-1
```

The bar must:

- reserve 48 logical pixels;
- render `sysc-shell`, the active Niri workspace, a meter, and a clickable button;
- update the workspace label after a Niri workspace event;
- toggle the meter and button color after a pointer click;
- render correctly at scale 1 and one available non-1 scale;
- submit no new frames while state stays unchanged;
- release all resources after SIGINT or SIGTERM.

## Task 1: Initialise the Go module

**Files:**

- Create: `go.mod`
- Create: `go.sum`
- Create: `cmd/sysc-shell/main.go`
- Create: `cmd/sysc-shell/main_test.go`

**Step 1: Write the failing CLI test**

Create a test for a pure parser function in package `main`:

```go
func TestParseOptions(t *testing.T) {
	t.Parallel()

	got, err := parseOptions([]string{"--output", "DP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "DP-1" {
		t.Fatalf("output = %q, want DP-1", got.Output)
	}
}
```

Also test an unknown flag and a missing `--output` value.

**Step 2: Run the test and confirm failure**

```bash
go test ./cmd/sysc-shell
```

Expected: compilation fails because `parseOptions` does not exist.

**Step 3: Initialise and pin dependencies**

```bash
go mod init github.com/Nomadcxx/sysc-shell
go mod edit -go=1.26
go get github.com/Nomadcxx/sysc-wayland@v0.1.1
go get github.com/go-text/typesetting@ddb7ff96ad4d2dc730cbcae9dd5140023f319c3e
go get golang.org/x/image@v0.44.0
go get golang.org/x/sys@v0.47.0
```

All four direct dependencies resolve at these versions: `sysc-wayland v0.1.1`,
`typesetting v0.3.5-0.20260729084153-ddb7ff96ad4d`, `x/image v0.44.0`, and `x/sys v0.47.0`.
After Task 6 runs the scanner in its version-suffixed module context, `go mod tidy` must leave `go.sum`
holding only these and their transitive requirements.

Use `flag.NewFlagSet` with `flag.ContinueOnError`. `main()` parses arguments, creates a signal-aware context, calls a stub `run(ctx, options)`, writes errors to stderr, and exits non-zero. The stub returns `errors.New("architectural proof not implemented")`.

**Step 4: Run checks**

```bash
go test ./cmd/sysc-shell
go vet ./cmd/sysc-shell
```

Expected: both pass.

**Step 5: Commit**

```bash
git add go.mod go.sum cmd/sysc-shell
git commit -m "build: initialize sysc-shell module"
```

## Task 2: Build the retained proof tree and row layout

**Files:**

- Create: `internal/ui/tree.go`
- Create: `internal/ui/layout.go`
- Create: `internal/ui/layout_test.go`

**Step 1: Write failing layout and hit tests**

Use these public data types inside the internal package:

```go
type Kind uint8

const (
	KindRow Kind = iota
	KindText
	KindMeter
	KindButton
)

type Rect struct{ X, Y, W, H int }

type Node struct {
	Kind     Kind
	Text     string
	Value    float64
	Width    int
	Padding  int
	Gap      int
	Action   string
	Bounds   Rect
	Children []*Node
}

type MeasureText func(string) (width, height int)
```

Create a root row with text, meter, and button. Use a fake text measurer that returns `len(text)*8, 16`. Assert:

- children appear in source order;
- padding and gap affect positions once;
- fixed meter width is preserved;
- button width includes text and horizontal padding;
- the row height matches the supplied 48-pixel bound;
- `Hit(root, x, y)` returns the button action inside its bounds and no action outside.

**Step 2: Run and confirm failure**

```bash
go test ./internal/ui -run 'TestLayout|TestHit' -v
```

Expected: compilation fails because layout and hit-test functions do not exist.

**Step 3: Implement the minimum retained tree**

Implement:

```go
func Layout(root *Node, bounds Rect, measure MeasureText) error
func Hit(root *Node, x, y int) (action string, ok bool)
```

Reject a nil root, a non-row root, negative bounds, unsupported kinds, meter values outside zero through one, and a child that cannot fit. Walk children in reverse order during hit testing so the result matches reverse paint order.

Do not add flexbox, vertical layout, margins, percentages, constraints, or interfaces.

**Step 4: Run checks**

```bash
go test ./internal/ui -v
go vet ./internal/ui
```

Expected: pass.

**Step 5: Commit**

```bash
git add internal/ui
git commit -m "feat: add proof UI tree and row layout"
```

## Task 3: Qualify pure-Go shaping and rasterisation

**Files:**

- Create: `internal/render/text.go`
- Create: `internal/render/text_test.go`

**Step 1: Write failing text tests**

Use `golang.org/x/image/font/gofont/goregular.TTF` for the Latin fixture; it has no Arabic coverage.

For the joined-script fixture, copy `Amiri-Regular.ttf` and `OFL.txt` from the pinned `go-text/typesetting` module's `font/testdata/` into `internal/render/testdata/`. Amiri is SIL OFL 1.1; keep `OFL.txt` beside the font and record its provenance in `docs/prior-art.md`.

Test these behaviors:

```go
func TestTextMeasureAndRaster(t *testing.T) {
	face := mustTestFace(t)
	r := NewTextRenderer(face)

	w, h, err := r.Measure("sysc-shell", 16)
	if err != nil || w <= 0 || h <= 0 {
		t.Fatalf("measure = %dx%d, %v", w, h, err)
	}

	mask, err := r.Raster("sysc-shell", 16)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNonZeroAlpha(mask) {
		t.Fatal("raster contains no glyph pixels")
	}
}
```

Add a joined/right-to-left fixture and assert that shaping returns glyphs, positive bounds, and stable output across two calls.

Do not assert an exact glyph sequence, and do not stop at "shaping returned glyphs" -- that passes even when shaping degenerates to a plain `cmap` lookup with no joining. Assert instead that **no shaped glyph ID equals the nominal glyph ID** of its source rune (`Face.NominalGlyph`). Contextual substitution is what makes the result different, and the assertion stays stable across font and shaper updates.

**Step 2: Run and confirm failure**

```bash
go test ./internal/render -run Text -v
```

Expected: compilation fails because `TextRenderer` does not exist.

**Step 3: Implement shaping and rasterisation**

- Parse fonts with `typesetting/font.ParseTTF`.
- Shape horizontal runs with `shaping.HarfbuzzShaper`.
- Use explicit direction, script, language, face, and 26.6 fixed-point size in `shaping.Input`.
- Convert `font.GlyphOutline` segments into `golang.org/x/image/vector.Rasterizer` paths.
- Return an `image.Alpha` mask plus baseline and advance data.
- Cache parsed faces. Do not add a global glyph cache until painting measures a need.
- Return errors for empty font data, invalid size, missing glyph data, and unsupported bitmap/color glyphs.
- Shape at the **physical** pixel size (`base * scale120 / 120`). Do not shape at the logical size and upscale the mask.
- `font.GlyphData` returns a `font.GlyphData` interface; only `font.GlyphOutline` is supported. Map its `opentype.SegmentOp` values onto `vector.Rasterizer`'s `MoveTo`, `LineTo`, `QuadTo` and `CubeTo`, negating Y to convert font units to raster coordinates.

Use the upstream pinned `shaping/render_test.go` only as a segment and coordinate reference. Do not copy its diagnostic point renderer.

**Step 4: Run checks**

```bash
go test ./internal/render -run Text -v
go vet ./internal/render
```

Expected: pass for Latin and joined/right-to-left fixtures.

**Step 5: Commit**

```bash
git add internal/render
git commit -m "feat: shape and rasterize proof text"
```

## Task 4: Paint the tree into an ARGB buffer

**Files:**

- Create: `internal/render/canvas.go`
- Create: `internal/render/paint.go`
- Create: `internal/render/paint_test.go`

**Step 1: Write failing pixel tests**

Create a 240 by 48 buffer and a laid-out tree. Assert exact pixels for:

- opaque background;
- meter track and filled portion;
- button background before and after toggled state;
- at least one non-background text pixel;
- clipping when a glyph or rectangle reaches the buffer edge.

The canvas format is little-endian premultiplied ARGB8888, represented in memory as B, G, R, A bytes.

**Step 2: Run and confirm failure**

```bash
go test ./internal/render -run Paint -v
```

Expected: compilation fails because canvas and painting functions do not exist.

**Step 3: Implement the painter**

Implement only:

```go
type Color struct{ R, G, B, A uint8 }
type Canvas struct { Pix []byte; Width, Height, Stride int }
func NewCanvas(pix []byte, width, height, stride int) (*Canvas, error)
func Paint(c *Canvas, root *ui.Node, text *TextRenderer, state ProofStyle) error
```

Validate stride and buffer length at construction. Premultiply colors once. Clip every fill and mask blend to canvas bounds. Paint row children in order.

**Step 4: Run checks**

```bash
go test ./internal/render -v
go vet ./internal/render
```

Expected: pass.

**Step 5: Commit**

```bash
git add internal/render
git commit -m "feat: paint proof tree into ARGB buffers"
```

## Task 5: Add buffer-slot and frame scheduling state

**Files:**

- Create: `internal/render/schedule.go`
- Create: `internal/render/schedule_test.go`

**Step 1: Write failing state-machine tests**

Test a two-slot scheduler:

1. acquire slot zero;
2. mark it submitted and busy;
3. request another redraw while a frame callback is pending;
4. verify no second submission occurs;
5. receive **frame done while the slot is still busy** and verify the scheduler does not offer that slot;
6. receive the release afterwards and verify exactly one coalesced redraw becomes ready;
7. repeat 5 and 6 with the **release arriving first**, and verify the same single redraw;
8. receive a configure while a frame is pending: verify the pending render is discarded and a redraw at the new size becomes ready;
9. receive a close while a frame is pending: verify the scheduler produces no further work;
10. verify an idle scheduler produces no work.

Frame completion never implies a free buffer. Observed on Niri 26.04: `wl_callback.done` for a commit
arrives while that commit's buffer is still held, and `wl_buffer.release` arrives only after the **next**
buffer is attached and committed. A scheduler that treats frame-done as release will reuse a busy buffer.

**Step 2: Run and confirm failure**

```bash
go test ./internal/render -run Scheduler -v
```

Expected: compilation fails because `Scheduler` does not exist.

**Step 3: Implement pure scheduling state**

Use a small struct with two slot states, `dirty`, and `framePending`. It must not import Wayland. Return explicit transitions to the platform layer:

```go
type Decision uint8
const (
	DecisionWait Decision = iota
	DecisionRender
)
```

Reject release or frame-done events that do not match current state. Do not add locks; the Wayland owner calls the scheduler from one goroutine.

**Step 4: Run checks and commit**

```bash
go test ./internal/render -v
git add internal/render
git commit -m "feat: add frame and buffer scheduling state"
```

## Task 6: Vendor and generate the required shell protocol bindings

**Files:**

- Create: `protocols/wlr-layer-shell-unstable-v1.xml`
- Create: `protocols/xdg-shell.xml`
- Create: `protocols/fractional-scale-v1.xml`
- Create: `protocols/viewporter.xml`
- Create: `internal/platform/wayland/xdgshell/generate.go`
- Create: `internal/platform/wayland/layershell/generate.go`
- Create: `internal/platform/wayland/fractionalscale/generate.go`
- Create: `internal/platform/wayland/viewporter/generate.go`
- Generate: `internal/platform/wayland/xdgshell/xdg_shell.go`
- Generate: `internal/platform/wayland/layershell/layer_shell.go`
- Generate: `internal/platform/wayland/fractionalscale/fractional_scale.go`
- Generate: `internal/platform/wayland/viewporter/viewporter.go`

**Step 1: Pin the protocol source**

Copy `wlr-layer-shell-unstable-v1.xml` from `wlr-protocols` at the revision providing
`zwlr_layer_shell_v1` version 5, which is the version Niri 26.04 advertises. Copy `xdg-shell.xml`,
`fractional-scale-v1.xml`, and `viewporter.xml` from the pinned `wayland-protocols` revision used by
Noctalia. Preserve copyright headers and record upstream URLs and commits in each `generate.go`.

Layer-shell references `xdg_popup`, so its generated package needs the local xdg-shell package even
though the proof does not create an xdg surface.

**Step 2: Add the generator command**

```go
// internal/platform/wayland/xdgshell/generate.go
//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg xdgshell -prefix xdg_ -o xdg_shell.go -i ../../../../protocols/xdg-shell.xml

// internal/platform/wayland/layershell/generate.go
//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg layershell -xdg-shell-import github.com/Nomadcxx/sysc-shell/internal/platform/wayland/xdgshell -o layer_shell.go -i ../../../../protocols/wlr-layer-shell-unstable-v1.xml

// internal/platform/wayland/fractionalscale/generate.go
//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg fractionalscale -o fractional_scale.go -i ../../../../protocols/fractional-scale-v1.xml

// internal/platform/wayland/viewporter/generate.go
//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.1 -pkg viewporter -o viewporter.go -i ../../../../protocols/viewporter.xml
```

Keep the `@v0.1.1` suffix. Without it the scanner resolves inside the main module and writes its build
dependencies into this repository's `go.sum`. The version suffix builds it in an isolated module
context.

Generation is reproducible, with two conditions. The generated header embeds the literal `-i` argument,
so the relative path must not change; and the generator shells out to `go list -m -f '{{.GoVersion}}'`
to pick a `gofumpt` language version, so the `go` directive in `go.mod` must not change without
regenerating.

Add a scanner command for `xdgshell` and equivalent commands in `fractionalscale` and `viewporter`.
Only the layer-shell command needs `-xdg-shell-import`, and only the xdg-shell command needs
`-prefix xdg_`. That prefix is mandatory: the layer-shell generator emits its `xdg_popup` references
against the trimmed names, so an xdg-shell package generated without it does not compile against the
layer-shell package. Do not trim prefixes in any other protocol package. Keeping protocol names there
makes source comparisons with XML and upstream generated bindings direct.

**Step 3: Generate and prove reproducibility**

```bash
go generate ./internal/platform/wayland/xdgshell
go generate ./internal/platform/wayland/layershell
go generate ./internal/platform/wayland/fractionalscale
go generate ./internal/platform/wayland/viewporter
cp internal/platform/wayland/xdgshell/xdg_shell.go /tmp/sysc-shell-xdg-shell.go
cp internal/platform/wayland/layershell/layer_shell.go /tmp/sysc-shell-layer-shell.go
cp internal/platform/wayland/fractionalscale/fractional_scale.go /tmp/sysc-shell-fractional-scale.go
cp internal/platform/wayland/viewporter/viewporter.go /tmp/sysc-shell-viewporter.go
go generate ./internal/platform/wayland/xdgshell
go generate ./internal/platform/wayland/layershell
go generate ./internal/platform/wayland/fractionalscale
go generate ./internal/platform/wayland/viewporter
cmp /tmp/sysc-shell-xdg-shell.go internal/platform/wayland/xdgshell/xdg_shell.go
cmp /tmp/sysc-shell-layer-shell.go internal/platform/wayland/layershell/layer_shell.go
cmp /tmp/sysc-shell-fractional-scale.go internal/platform/wayland/fractionalscale/fractional_scale.go
cmp /tmp/sysc-shell-viewporter.go internal/platform/wayland/viewporter/viewporter.go
```

Expected: `cmp` exits zero.

**Step 4: Run checks and commit**

```bash
go test ./internal/platform/wayland/...
go vet ./internal/platform/wayland/...
git add protocols internal/platform/wayland
git commit -m "build: generate shell protocol bindings"
```

## Task 7: Implement the Wayland owner and shared-memory surface

**Dependency gate:** `sysc-wayland v0.1.1` must pass its full release gate before this plan starts. Task 7
must resolve that tag without a local `replace` directive. Do not compensate for a broken wire reader,
descriptor path, or proxy lifecycle in the shell.

**Files:**

- Create: `internal/platform/wayland/client.go`
- Create: `internal/platform/wayland/output.go`
- Create: `internal/platform/wayland/shm.go`
- Create: `internal/platform/wayland/surface.go`
- Create: `internal/platform/wayland/lifecycle_test.go`

**Step 1: Write failing pure lifecycle tests**

Model registry events with local structs and test:

- required globals become ready only after compositor, shm, seat, layer-shell, fractional-scale manager, viewporter, and at least one output exist;
- bind versions use the lower server/client value, taken from an explicit client-side table (see Step 3);
- `ARGB8888` selected from the advertised `wl_shm.format` list, and a named error when it is absent;
- a named output selection matches on the `wl_output.name` string and fails when no output matches after the initial roundtrips;
- output hosts are keyed by `wl_registry` global name, never by connector string;
- output removal returns the host ID to destroy;
- configure acknowledgement precedes buffer eligibility;
- cleanup order is buffer, pool, mapping, viewport, fractional-scale object, layer surface, `wl_surface`, output proxy, globals, display;
- a buffer generation is retired only after every buffer in it has been released, or the surface is destroyed.

**Step 2: Run and confirm failure**

```bash
go test ./internal/platform/wayland -run 'Lifecycle|Registry|Configure' -v
```

Expected: compilation fails because lifecycle types do not exist.

**Step 3: Implement the live owner**

Implement:

```go
type Options struct { Output string; Height int }
type Event struct { Kind EventKind; X, Y int; Button uint32 }
type Callbacks struct {
	// logicalWidth and logicalHeight come from zwlr_layer_surface_v1.configure.
	// scale120 is the wp_fractional_scale_v1 numerator over 120; 120 means scale 1.0.
	Configure func(logicalWidth, logicalHeight, scale120 int) error
	// pixels is the physical buffer: width and height are buffer pixels, not logical units.
	Render func(pixels []byte, width, height, stride int) error
	Handle func(Event) bool
	// The caller owns this channel. Run only receives from it and never closes it.
	Invalidations <-chan struct{}
}
func Run(ctx context.Context, options Options, callbacks Callbacks) error
```

`scale120` must not be reduced to an integer scale factor. The compositor reports the preferred scale as
a numerator over 120, so 150 (1.25), 180 (1.5) and 200 (1.667) all collapse onto the same integer and
produce the wrong buffer size.

Validate the three function fields before connecting to Wayland. The proof supplies one concrete set of
callbacks. Lifecycle tests exercise the pure state machines directly and do not need an application fake.

Inside `Run`:

- connect with `client.Connect("")`;
- install an optional `wl_display` error handler before any other request to add object and message
  context to logs. `sysc-wayland` records the protocol error on the connection before calling this
  handler, and later dispatch calls return that sticky error;
- bind compositor, shm, seat, outputs, and layer-shell;
- bind fractional-scale manager and viewporter; treat both as **required** and fail with a named error
  when either is absent. The proof exists to qualify this path, so an integer-scale fallback would let it
  pass without proving its stated architecture;
- cap each bound version at `min(server version, client maximum)`. The generated client exports no per-interface
  version constant, so the client maximum is an explicit table owned by this package:

  | Interface | Client max | Niri 26.04 offers |
  |---|---|---|
  | `wl_compositor` | 6 | 6 |
  | `wl_shm` | 1 | 2 |
  | `wl_seat` | 7 | 9 |
  | `wl_output` | 4 | 4 |
  | `zwlr_layer_shell_v1` | 4 | 5 |
  | `wp_fractional_scale_manager_v1` | 1 | 1 |
  | `wp_viewporter` | 1 | 1 |

- select the output by matching `--output` against the `wl_output.name` event, which `wl_output`
  version 4 provides directly. Do not bind `zxdg_output_manager_v1`, and do not correlate through Niri
  IPC. Two roundtrips are required: the first delivers the globals, the second the per-output
  `name`, `scale`, `geometry` and `done` events;
- collect `wl_shm.format` events during those roundtrips and confirm `ARGB8888` is advertised before
  creating a pool. It is present on Niri but arrives last in the list;
- create `wl_surface` and a top-layer surface with namespace `sysc-shell:proof`;
- anchor top, left, and right;
- set size `0 x 48`, exclusive zone `48`, and keyboard interactivity `none`;
- commit once without a buffer;
- obtain one fractional-scale object and viewport for the surface;
- acknowledge every configure before attaching a buffer, then derive sizes as follows:

  - the configure width and height are **logical** units, and they are **not** the output size. They are
    what remains after other layer surfaces' exclusive zones. Observed on the reference machine: a
    3440-wide output produced a configure width of 3396 because another shell's bar held 44 logical
    pixels. Never compute the surface size from `wl_output` mode or from Niri IPC;
  - buffer width and height are `(logical * scale120 + 60) / 120` -- multiply by the scale and round half
    away from zero, as `fractional-scale-v1` specifies;
  - default `scale120` to 120 until the first `preferred_scale` arrives. It is not ordered against
    `zwlr_layer_surface_v1.configure`, and it can also arrive **alone**, with no configure following,
    when the output scale changes at an unchanged logical size. Treat that as a reconfigure;

- leave `wl_surface.set_buffer_scale` at its default of 1 and call **`wp_viewport.set_destination`** with
  the logical configure size. Do **not** call `wp_viewport.set_source`: the default source is the whole
  buffer, which is what is wanted, and a source rectangle in the wrong units is a protocol error;
- allocate two memfd-backed buffers at the physical size, attach the released slot, submit damage with
  **`wl_surface.damage_buffer`** in buffer pixels (not `wl_surface.damage`, which is in surface units and
  ambiguous under a viewport), request a frame callback, and commit;
- own the memfd, mapping, pool and buffers together as one **buffer generation**. A pool may be destroyed
  while its buffers live, but storage the compositor still reads must not be written or unmapped. On
  reconfigure, allocate a new generation and retire the old one only after every buffer in it has emitted
  `wl_buffer.release`, or the surface is destroyed;
- bind pointer input from the seat on `wl_seat.capabilities` when the pointer bit is set, and release it
  when the bit clears. `sysc-wayland` delivers `SurfaceX`/`SurfaceY` as `float64` already converted from
  `wl_fixed`; under a viewport those are logical units, matching hit testing. Track focus with
  enter/leave, record the node on button press, and treat a release inside the same node as the click;
- coalesce redraw through the scheduler;
- poll context cancellation and `callbacks.Invalidations` without dispatching Wayland from another goroutine.
  A bridge goroutine may write to a wake pipe or eventfd; it may not invoke a Wayland proxy. Poll the
  Wayland descriptor and the wake descriptor with `unix.Poll` inside `Context.ControlFD`; do not retain
  the Wayland descriptor after its callback. `sysc-wayland` has no `prepare_read`/`read_events` split and
  no write buffer. `Dispatch()` reads one message and blocks when none is pending, so dispatch once per
  readiness and re-poll with a zero timeout to drain. Return any `ControlFD` or dispatch error;
- destroy in tested child-to-parent order.

Create anonymous files with `unix.MemfdCreate`, `Ftruncate`, and `Mmap`. Validate multiplication and `int32` conversions before allocating or sending sizes.

**Step 4: Run unit checks**

```bash
go test ./internal/platform/wayland -v
go vet ./internal/platform/wayland
```

Expected: pass without a compositor.

**Step 5: Add a temporary live smoke call**

Wire `cmd/sysc-shell` to flat-color callbacks. Run on Niri and verify configure, map, exclusive zone, and
clean exit. Task 9 replaces this temporary wiring with the proof application. Do not add a permanent
diagnostic flag.

**Step 6: Commit**

```bash
git add internal/platform/wayland cmd/sysc-shell
git commit -m "feat: add Wayland layer-surface owner"
```

## Task 8: Implement typed Niri event streaming

**Files:**

- Create: `internal/platform/niri/client.go`
- Create: `internal/platform/niri/events.go`
- Create: `internal/platform/niri/client_test.go`

**Step 1: Write failing socket tests**

Start a temporary Unix socket server in the test. Assert that the client:

- writes JSON string `"EventStream"` followed by a newline;
- reads the reply line **before** any event and requires `{"Ok":"Handled"}`;
- fails startup on `{"Err":"..."}` instead of discarding it as an unknown event;
- parses `WorkspacesChanged` and `WorkspaceActivated` lines;
- treats the first `WorkspacesChanged` as the complete initial snapshot;
- projects active workspace name and output into one snapshot;
- rejects a known event with a missing or mistyped required workspace field without publishing a partial snapshot;
- accepts nullable `name` and `output` fields;
- ignores an unknown top-level event while keeping the stream alive;
- rejects a line larger than 1 MiB;
- returns context cancellation without leaking the reader goroutine.

Use fixture lines copied from the installed Niri version's `niri msg -j event-stream` output. Remove
window titles and user data from committed fixtures.

Verified against Niri 26.04 on the reference machine: the reply is the bare string form
`{"Ok":"Handled"}`, and a malformed request returns `{"Err":"error parsing request"}`. Because the client
must tolerate unknown events, an unread `Err` reply would be silently discarded and the client would wait
forever -- so the reply must be read and checked explicitly.

**Step 2: Run and confirm failure**

```bash
go test ./internal/platform/niri -v
```

Expected: compilation fails because the client does not exist.

**Step 3: Implement the client**

Expose:

```go
type Workspace struct { ID uint64; Index int; Name, Output string; Active, Focused bool }
type Snapshot struct { Workspaces []Workspace; FocusedOutput string }
func Stream(ctx context.Context, socketPath string) (<-chan Snapshot, <-chan error)
```

Wire shape observed on Niri 26.04:

```json
{"id":5,"idx":1,"name":null,"output":"DP-3","is_urgent":false,"is_active":true,"is_focused":false,"active_window_id":80}
```

Use a private wire struct with pointer fields to distinguish a missing required field from its zero value.
Require valid `id`, `idx`, `is_active`, and `is_focused` values. Treat `name` and `output` as nullable and
project null to `""`. Decode `id` as `uint64`; narrowing it has no benefit. `FocusedOutput` has no dedicated
event or field, so derive it from the workspace whose `is_focused` is true.

Niri may add fields without breaking `encoding/json`; `is_urgent` and `active_window_id` are examples the
projection ignores. A malformed known event is different: reject the complete event with a descriptive
error and publish no partial snapshot. Unknown top-level events remain ignorable.

**Niri emits no output event.** `OutputsChanged` does not exist. Workspaces carry an output *name* only.
Output existence, identity, scale, mode, transform and hotplug all come from Wayland. Do not project
output state from this package.

Read with a buffered reader and an explicit maximum line size. Decode the top-level event key first, then decode known payloads. Sort snapshots by output, index, and ID so the UI receives stable ordering. Send only the newest snapshot when the consumer channel is full.

Use `$NIRI_SOCKET` in production and an explicit path in tests. Do not call `niri msg`.

**Step 4: Run checks and commit**

```bash
go test ./internal/platform/niri -v
go vet ./internal/platform/niri
git add internal/platform/niri
git commit -m "feat: stream typed Niri workspace state"
```

## Task 9: Integrate the proof application

**Files:**

- Create: `internal/shell/proof.go`
- Create: `internal/shell/proof_test.go`
- Modify: `cmd/sysc-shell/main.go`

**Step 1: Write failing model tests**

Test that:

- the first snapshot sets `Workspace: <name or index>`;
- a later snapshot changes text and marks the model dirty once;
- a click on action `toggle-meter` changes the meter between `0.25` and `0.75`;
- clicking outside an action changes nothing;
- repeated equivalent state produces no redraw request;
- layout uses the configured logical width and 48-pixel height.

**Step 2: Run and confirm failure**

```bash
go test ./internal/shell -v
```

Expected: compilation fails because `Proof` does not exist.

**Step 3: Implement the proof app**

`Proof` owns:

- the current Niri snapshot;
- toggled state;
- the retained root node;
- text renderer and style;
- one buffered invalidation channel.

Protect model fields with one mutex. `UpdateNiri`, pointer handling, and rendering may arrive from different goroutines. Hold the lock only while copying or changing state; do text shaping and painting after copying the view state.

Use this fixed tree:

```text
row padding=12 gap=12
├── text "sysc-shell"
├── text "Workspace: <value>"
├── meter width=120 value=0.25|0.75
└── button action="toggle-meter" text="Toggle"
```

The Niri goroutine sends immutable snapshots to the proof model. The model emits coalesced invalidations. The Wayland owner consumes them and performs layout and paint. No other goroutine invokes a Wayland proxy.

Replace the Task 7 flat-color app in `main.go`. Validate `NIRI_SOCKET` before opening Wayland so the startup error names the missing environment variable.

**Step 4: Run checks**

```bash
go test ./...
go vet ./...
```

Expected: pass.

**Step 5: Commit**

```bash
git add internal/shell cmd/sysc-shell
git commit -m "feat: integrate interactive Niri proof"
```

## Task 10: Run the live Niri acceptance gate

**Files:**

- Create: `tests/integration/README.md`
- Modify: `docs/roadmap.md` only if the observed gate needs a correction

**Step 1: Run automated checks**

```bash
go test -race ./...
go vet ./...
go build ./cmd/sysc-shell
```

Expected: all commands exit zero.

**Step 2: Verify one output**

```bash
go run ./cmd/sysc-shell --output <connector>
```

Record:

- Niri version;
- output name, mode, transform, and scale;
- configure width and height, and the output's logical width for comparison;
- `preferred_scale` numerator, and whether it arrived before or after the first configure;
- buffer width, height, and stride;
- the `wl_shm` format list and the selected format;
- successful click and workspace update;
- clean shutdown result.

**Step 3: Verify non-1 scale**

Prefer an output already running at a non-1 scale and select it with `--output`; that path mutates
nothing. When every output is at scale 1, change a **non-focused** output temporarily.
`niri msg output` is documented as changing configuration "temporarily and not saved into the config
file", so no configuration file is touched and no backup is required -- but capture the current value so
the restore is exact:

```bash
OUT=DP-3
BEFORE=$(niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['$OUT']['logical']['scale'])")
niri msg output "$OUT" scale 1.5

go run ./cmd/sysc-shell --output "$OUT"

niri msg output "$OUT" scale "$BEFORE"      # or: niri msg action load-config-file
niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['$OUT']['logical']['scale'])"
```

Confirm logical size, physical buffer size, text, meter, hit region, and exclusive zone. Record the
logical configure size and the derived buffer size together; they are different numbers and both must be
right. Expect the buffer to be `(logical * scale120 + 60) / 120` and **not** a multiple of the output's
pixel width -- a 1707-logical surface at scale 1.5 yields a 2561-pixel buffer on a 2560-pixel output,
which is correct because the viewport scales the buffer to the destination.

Also confirm that Niri's own `logical.width` and the layer-surface configure width may differ. Both were
observed to differ on the reference machine, in both directions.

**Step 4: Verify idle rendering**

Run the proof for 60 seconds without input or workspace changes. Instrument submitted frame count in debug output for this gate. Expected: the count remains unchanged after the initial frame.

**Step 5: Verify restart cleanup**

Start and stop the proof ten times. Confirm that every run maps the surface and no prior process or private socket remains.

**Step 6: Document the gate**

Write the commands and required observations in `tests/integration/README.md`. Do not commit machine-specific output, usernames, or a benchmark claim from one machine.

**Step 7: Final verification and commit**

```bash
go test -race ./...
go vet ./...
git status --short
git add tests/integration/README.md docs/roadmap.md
git commit -m "test: document Niri proof gate"
```

Expected: tests pass and the worktree is clean after the commit.

## Stop condition

The architectural proof ends after Task 10 passes. Start the multi-output bar design review next. Do not add output hotplug, configuration reload, built-in services, panels, plugins, or GPU rendering in this plan.
