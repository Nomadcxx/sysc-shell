# Architectural Proof Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build one interactive Niri layer surface that proves the Wayland, text, retained-layout, input, Niri IPC, event-driven rendering, and shutdown architecture.

**Architecture:** One goroutine owns the `dankgo` Wayland connection and every Wayland proxy. Pure UI and rendering packages build and paint a retained tree into released `wl_shm` buffers. A Niri client publishes typed workspace snapshots, and pointer actions mutate the proof model through the Wayland owner's command queue.

**Tech Stack:** Go 1.26+, pinned `dankgo`, pinned `go-text/typesetting`, `golang.org/x/image`, `golang.org/x/sys`, Niri JSON IPC, `wlr-layer-shell-unstable-v1`, and `wl_shm`.

---

## Working rules

- Execute this plan in a dedicated worktree from the documentation baseline.
- Use `github.com/Nomadcxx/sysc-shell` as the module path unless the repository owner supplies another canonical path before Task 1.
- Keep the first proof in `cmd/sysc-shell`; do not create a throwaway binary.
- Do not add a framework, dependency-injection container, logging package, configuration library, Makefile, or renderer interface.
- Keep the UI fixture fixed: one row with a workspace label, a meter, and a button.
- Commit after each task. Stop at Task 10 even if later roadmap work looks easy.

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
go get github.com/AvengeMedia/dankgo@10434658325c
go get github.com/go-text/typesetting@ddb7ff96ad4d2dc730cbcae9dd5140023f319c3e
go get golang.org/x/image@v0.44.0
go get golang.org/x/sys@v0.47.0
```

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

Use `golang.org/x/image/font/gofont/goregular.TTF` for the Latin fixture. Add one small test font fixture with Arabic coverage under `internal/render/testdata/` only if no dependency test font has a redistribution-compatible license.

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

Add a joined/right-to-left fixture and assert that shaping returns glyphs, positive bounds, and stable output across two calls. Do not assert a fragile exact glyph sequence.

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
5. receive frame done and buffer release;
6. verify exactly one coalesced redraw becomes ready;
7. verify an idle scheduler produces no work.

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
- Create: `protocols/fractional-scale-v1.xml`
- Create: `protocols/viewporter.xml`
- Create: `internal/platform/wayland/layershell/generate.go`
- Create: `internal/platform/wayland/fractionalscale/generate.go`
- Create: `internal/platform/wayland/viewporter/generate.go`
- Generate: `internal/platform/wayland/layershell/layer_shell.go`
- Generate: `internal/platform/wayland/fractionalscale/fractional_scale.go`
- Generate: `internal/platform/wayland/viewporter/viewporter.go`

**Step 1: Pin the protocol source**

Copy `wlr-layer-shell-unstable-v1.xml` from the same protocol revision used by the inspected DMS commit. Copy `fractional-scale-v1.xml` and `viewporter.xml` from the pinned `wayland-protocols` revision used by Noctalia. Preserve copyright headers and record upstream URLs and commits in each `generate.go`.

**Step 2: Add the generator command**

```go
//go:generate go run github.com/AvengeMedia/dankgo/cmd/go-wayland-scanner -pkg layershell -o layer_shell.go -i ../../../../protocols/wlr-layer-shell-unstable-v1.xml
```

Add equivalent commands in packages `fractionalscale` and `viewporter`. Do not trim protocol prefixes. Keeping protocol names makes source comparisons with XML and upstream generated bindings direct.

**Step 3: Generate and prove reproducibility**

```bash
go generate ./internal/platform/wayland/layershell
go generate ./internal/platform/wayland/fractionalscale
go generate ./internal/platform/wayland/viewporter
cp internal/platform/wayland/layershell/layer_shell.go /tmp/sysc-shell-layer-shell.go
cp internal/platform/wayland/fractionalscale/fractional_scale.go /tmp/sysc-shell-fractional-scale.go
cp internal/platform/wayland/viewporter/viewporter.go /tmp/sysc-shell-viewporter.go
go generate ./internal/platform/wayland/layershell
go generate ./internal/platform/wayland/fractionalscale
go generate ./internal/platform/wayland/viewporter
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

**Files:**

- Create: `internal/platform/wayland/client.go`
- Create: `internal/platform/wayland/output.go`
- Create: `internal/platform/wayland/shm.go`
- Create: `internal/platform/wayland/surface.go`
- Create: `internal/platform/wayland/lifecycle_test.go`

**Step 1: Write failing pure lifecycle tests**

Model registry events with local structs and test:

- required globals become ready only after compositor, shm, seat, layer-shell, fractional-scale manager, viewporter, and at least one output exist;
- bind versions use the lower server/client value;
- a named output selection fails when no output matches after the initial roundtrip;
- output removal returns the host ID to destroy;
- configure acknowledgement precedes buffer eligibility;
- cleanup order is buffer, pool, layer surface, `wl_surface`, output proxy, globals, display.

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
type App interface {
	Configure(width, height, scale int) error
	Render(pixels []byte, width, height, stride int) error
	Handle(Event) bool
	Invalidations() <-chan struct{}
}
func Run(ctx context.Context, options Options, app App) error
```

The interface has two implementations during tests: the proof app and a lifecycle fake. Keep it at the platform boundary.

Inside `Run`:

- connect with `client.Connect("")`;
- bind compositor, shm, seat, outputs, and layer-shell;
- bind fractional-scale manager and viewporter;
- cap each bound version to the generated client's supported version;
- select the named output or the first output after discovery;
- create `wl_surface` and a top-layer surface with namespace `sysc-shell:proof`;
- anchor top, left, and right;
- set size `0 x 48`, exclusive zone `48`, and keyboard interactivity `none`;
- commit once without a buffer;
- obtain one fractional-scale object and viewport for the surface;
- acknowledge configure, combine the preferred scale in 120ths with the logical configure size, allocate two memfd-backed buffers at physical scale, set viewport source and logical destination, attach the released slot, damage, request a frame callback, and commit;
- bind pointer input from the seat and route coordinates in logical surface units;
- coalesce redraw through the scheduler;
- poll context cancellation and `App.Invalidations()` without dispatching Wayland from another goroutine. A bridge goroutine may write to a wake pipe or eventfd; it may not invoke a Wayland proxy. Poll the Wayland fd and wake fd with `unix.Poll`;
- destroy in tested child-to-parent order.

Create anonymous files with `unix.MemfdCreate`, `Ftruncate`, and `Mmap`. Validate multiplication and `int32` conversions before allocating or sending sizes.

**Step 4: Run unit checks**

```bash
go test ./internal/platform/wayland -v
go vet ./internal/platform/wayland
```

Expected: pass without a compositor.

**Step 5: Add a temporary live smoke call**

Wire `cmd/sysc-shell` to a flat-color fake app. Run on Niri and verify configure, map, exclusive zone, and clean exit. Remove the fake wiring in Task 9, but keep the platform code.

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
- parses `WorkspacesChanged` and `WorkspaceActivated` lines;
- projects active workspace name and output into one snapshot;
- ignores an unknown top-level event while keeping the stream alive;
- rejects a line larger than 1 MiB;
- returns context cancellation without leaking the reader goroutine.

Use fixture lines copied from the installed Niri version's `niri msg -j event-stream` output. Remove window titles and user data from committed fixtures.

**Step 2: Run and confirm failure**

```bash
go test ./internal/platform/niri -v
```

Expected: compilation fails because the client does not exist.

**Step 3: Implement the client**

Expose:

```go
type Workspace struct { ID int64; Index int; Name, Output string; Active, Focused bool }
type Snapshot struct { Workspaces []Workspace; FocusedOutput string }
func Stream(ctx context.Context, socketPath string) (<-chan Snapshot, <-chan error)
```

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
- configure width and height;
- buffer width, height, and stride;
- successful click and workspace update;
- clean shutdown result.

**Step 3: Verify non-1 scale**

Use an existing scaled output or change one test output through Niri configuration. Confirm logical size, physical buffer size, text, meter, hit region, and exclusive zone.

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
