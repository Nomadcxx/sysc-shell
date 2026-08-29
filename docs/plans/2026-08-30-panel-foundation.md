# Panel Foundation Implementation Plan — Milestone 4, Tranche 4A

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship the Milestone 4 gate surfaces: panel machinery (shield + panel layer surfaces,
Exclusive keyboard, single instance), floating placement with clamping, shell-rendered rounding and
shadows, the 4A control vocabulary with roving keyboard focus, matugen core theming, the
clock/calendar, system-monitor, and session/power popouts, and the v1 IPC socket with documented
niri hotkeys.

**Architecture:** Panels reuse the Milestone 2/3 ownership shape. `shell.Registry` (process-scoped)
owns panel state the way it owns bars; the Wayland client gains generic auxiliary-surface support
(create/destroy at runtime, per-surface callbacks, keyboard delivery) because today it maps exactly
one bar surface per output — that is the one Milestone 2 modification this plan requires, and it is
explicit. All new logic is pure-Go stdlib; the only new dependency is `github.com/Nomadcxx/sysc-metrics`
via a local `replace`.

**Tech Stack:** Go 1.26 stdlib, sysc-wayland v0.1.x generated bindings (layer-shell,
fractional-scale, viewporter), sysc-metrics samplers, matugen 4.2.0 (external binary, exec),
loginctl (external binary, exec).

**Design:** [2026-08-30-panel-foundation-design.md](2026-08-30-panel-foundation-design.md)
**Research:** [2026-08-30-panels-and-controls-research.md](2026-08-30-panels-and-controls-research.md)

---

## Prerequisites (verify before Task 1)

1. Milestone 2 has passed its live Niri gate and merged to main. Milestone 3 (widget foundation)
   has merged to main. This plan executes from a fresh worktree of main containing both.
2. The merged tree contains the M3 surface this plan builds on:
   - `internal/shell/registry.go` — `Registry` with `NewHost(global, connector)`,
     `DropHost(global)`, `UpdateClock(now) []uint32`, `UpdateNiri(snap) []uint32`,
     `PrepareConfig(cfg, hosts)`, `Clock() *services.Clock`, `Invalidations() <-chan wayland.Invalidation`.
   - `internal/shell/bar.go` — `Bar` with `Handle(wayland.Event) bool`, `NewWithTheme(theme, policy, connector)`.
   - `internal/services/clock.go` — `NewClock()`, `Acquire(boundary) (*Lease, error)`,
     `(*Lease).Release()` (idempotent, nil-safe), `Updates() <-chan time.Time`, `Close()`.
   - `internal/platform/wayland/client.go` — `Run(ctx, cfg, Callbacks)` with
     `Callbacks{NewHost, PrepareConfig, DropHost, Invalidations, Reloads, ConfigPath}`,
     `HostCallbacks{Configure, Render, Handle}`, `Event{Kind, X, Y, Button, Serial}` (pointer kinds only).
   - `cmd/sysc-shell/main.go` — `run(ctx)` wiring Registry into `wayland.Callbacks`.
3. `matugen` (4.2.0), `wpctl`, `brightnessctl`, `loginctl` exist at `/usr/bin`. (wpctl/brightnessctl
   are consumed by Tranche 4B; only matugen and loginctl matter here.)
4. Commit messages must not contain AI-tool substrings (repo hook).

---

### Task 1: Verify matugen `color` subcommand flag symmetry

The design assumes `matugen color hex <HEX>` accepts the same `-c <config>` / template output flags
as `matugen image`. Verify before writing code against it.

**Step 1: Run the probe**

```bash
mkdir -p /tmp/matugen-probe && cd /tmp/matugen-probe
cat > config.toml <<'EOF'
[templates]
probe = { input_path = "tpl.json", output_path = "colors.json" }
EOF
cat > tpl.json <<'EOF'
{"surface":"{{colors.surface.dark.hex}}","on_surface":"{{colors.on_surface.dark.hex}}"}
EOF
matugen color hex '#3050a0' -c config.toml --prefer saturation
cat colors.json
```

Expected: `colors.json` contains rendered hex values for `surface` and `on_surface`.

**Step 2: Record the outcome**

- If it works: note the exact working invocation in the design doc's Risks section (replace
  "assumed-verify" with "verified") and commit that one-line amendment.
- If flags differ: stop and report the actual flags; the theme generator in Task 3 takes its
  invocation from this probe.

No code changes. Commit only the doc amendment if made.

---

### Task 2: Configuration additions

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/load.go` (wire types + decoding)
- Test: `internal/config/config_test.go`

New config surface (design §Configuration): `theme.source|seed|scheme|mode`,
`accessibility.reduced-motion|high-contrast`, `session.locker`, `panels.gap|padding`.
Pointer wire types throughout; absent fields inherit defaults.

**Step 1: Write the failing tests**

```go
func TestDefaultPanelAndSessionValues(t *testing.T) {
	c := Default()
	if c.Theme.Source != "wallpaper" || c.Theme.Scheme != "scheme-tonal-spot" || c.Theme.Mode != "dark" {
		t.Fatalf("theme defaults wrong: %+v", c.Theme)
	}
	if c.Panels.Gap != 8 || c.Panels.Padding != 8 {
		t.Fatalf("panels defaults wrong: %+v", c.Panels)
	}
	if c.Accessibility.ReducedMotion || c.Accessibility.HighContrast {
		t.Fatalf("accessibility must default off")
	}
	if c.Session.Locker != "" {
		t.Fatalf("locker must default empty")
	}
}

func TestThemeSourceValidation(t *testing.T) {
	for _, bad := range []string{"", "gradient", "auto"} {
		c := Default()
		c.Theme.Source = bad
		if err := c.Valid(); err == nil {
			t.Fatalf("source %q must be rejected", bad)
		}
	}
	for _, ok := range []string{"wallpaper", "hex", "stock"} {
		c := Default()
		c.Theme.Source = ok
		if err := c.Valid(); err != nil {
			t.Fatalf("source %q must be accepted: %v", ok, err)
		}
	}
}

func TestHexSeedValidation(t *testing.T) {
	c := Default()
	c.Theme.Source = "hex"
	c.Theme.Seed = "blue" // hex source needs a hex seed
	if err := c.Valid(); err == nil {
		t.Fatal("hex source with non-hex seed must fail")
	}
	c.Theme.Seed = "#3050a0"
	if err := c.Valid(); err != nil {
		t.Fatalf("hex seed must pass: %v", err)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/config/`
Expected: FAIL — unknown fields/types.

**Step 3: Implement**

In `config.go`, extend the resolved model:

```go
// ThemeSource selects how the Material 3 palette is seeded.
type ThemeConfig struct {
	Source string // wallpaper | hex | stock
	Seed   string // image path, #RRGGBB, or stock name — meaning follows Source
	Scheme string // matugen scheme-*, default scheme-tonal-spot
	Mode   string // dark | light
}

type Accessibility struct {
	ReducedMotion bool
	HighContrast  bool
}

type Session struct {
	Locker string // external locker command; empty hides the lock action
}

type Panels struct {
	Gap     int // offset from the bar edge, logical px
	Padding int // output edge inset for clamping, logical px
}
```

Add to `Config`: `ThemeConfig ThemeConfig` (name the field `ThemeGen` if `Theme` collides with the
existing bar `Theme` struct — keep both; they are different concerns), `Accessibility`,
`Session`, `Panels`. Extend `Default()` with the values asserted above. Extend `Valid()` (or add it
if M3 has not): source enum, mode enum, hex seed shape when source is hex, non-negative gap/padding.

Mirror wire types in `load.go` with pointer fields (`*string`, `*bool`, `*int`) and merge over
defaults exactly like the existing `wireBar` pattern.

**Step 4: Run to verify pass**

Run: `go test ./internal/config/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add theme source, accessibility, session, and panel tokens"
```

---

### Task 3: Theme token generation package

**Files:**
- Create: `internal/theme/theme.go`
- Create: `internal/theme/generate.go`
- Create: `internal/theme/embed.go`
- Create: `internal/theme/matugen/config.toml`
- Create: `internal/theme/matugen/tpl.json`
- Test: `internal/theme/theme_test.go`

This package owns: the embedded matugen config + template, palette generation via exec, the
colors.json cache under `$XDG_CACHE_HOME/sysc-shell/`, and the compiled-in fallback palette.
It has no Wayland or shell imports.

**Step 1: Write the failing tests**

```go
func TestGenerateFromImageWritesCacheFile(t *testing.T) {
	dir := t.TempDir()
	fakeMatugen(t, dir) // installs an executable "matugen" stub on PATH via t.Setenv
	g := Generator{CacheDir: dir, Matugen: filepath.Join(dir, "matugen")}
	tok, err := g.Generate(Source{Kind: "wallpaper", Seed: "/tmp/wall.jpg"}, Options{Mode: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	if tok.Surface == "" || tok.OnSurface == "" || tok.Primary == "" {
		t.Fatalf("tokens not populated: %+v", tok)
	}
	if _, err := os.Stat(filepath.Join(dir, "colors.json")); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
}

func TestGenerateFallbackWhenMatugenMissing(t *testing.T) {
	g := Generator{CacheDir: t.TempDir(), Matugen: "/nonexistent/matugen"}
	tok, err := g.Generate(Source{Kind: "wallpaper", Seed: "/tmp/wall.jpg"}, Options{Mode: "dark"})
	if err != nil {
		t.Fatalf("fallback must not error: %v", err)
	}
	if tok != Fallback {
		t.Fatalf("expected fallback palette, got %+v", tok)
	}
}

func TestHighContrastPassesContrastFlag(t *testing.T) {
	dir := t.TempDir()
	argsFile := fakeMatugenRecordingArgs(t, dir)
	g := Generator{CacheDir: dir, Matugen: filepath.Join(dir, "matugen")}
	if _, err := g.Generate(Source{Kind: "hex", Seed: "#3050a0"}, Options{Mode: "dark", HighContrast: true}); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--contrast") {
		t.Fatalf("expected --contrast in %q", args)
	}
}
```

`fakeMatugen` writes a shell stub that emits a minimal valid colors JSON:

```bash
#!/bin/sh
cat > "$OUTPUT_PLACEHOLDER" <<'EOF'
{"colors":{"dark":{"surface":"#1a1c1e","on_surface":"#e2e2e6","primary":"#a8c7fa","on_primary":"#062e6f","on_surface_variant":"#c3c6cf","error":"#ffb4ab","surface_container":"#1d1f21","outline":"#8d9199"},"light":{...same keys...}}}
EOF
```

(The stub must honor however the generator invokes matugen — see Step 3 for the invocation, which
writes the template output path itself; the stub just needs to produce that file. Implement the
stub in the test to write to the path the generator passes via the embedded template's
`output_path`, i.e. the cache file.)

**Step 2: Run to verify failure**

Run: `go test ./internal/theme/`
Expected: FAIL — package does not exist.

**Step 3: Implement**

`theme.go`:

```go
// Package theme generates the Material 3 token set the shell renders from.
package theme

// Tokens is the subset of Material 3 tokens the shell consumes. Dark and
// light variants are generated together; Mode selects which is active.
type Tokens struct {
	Surface            string
	SurfaceContainer   string
	OnSurface          string
	OnSurfaceVariant   string
	Primary            string
	OnPrimary          string
	PrimaryContainer   string
	OnPrimaryContainer string
	Outline            string
	Error              string
	OnError            string
}

// Fallback is the compiled-in palette used when matugen is absent or fails.
// Seeded from the Milestone 2 ProofStyle colors so the shell never renders
// without a theme.
var Fallback = Tokens{
	Surface: "#101214", SurfaceContainer: "#181a1d",
	OnSurface: "#e6e6e6", OnSurfaceVariant: "#9aa0a6",
	Primary: "#0080ff", OnPrimary: "#ffffff",
	PrimaryContainer: "#003a75", OnPrimaryContainer: "#d6e3ff",
	Outline: "#4a4f55", Error: "#ff5449", OnError: "#ffffff",
}

type Source struct {
	Kind string // wallpaper | hex | stock
	Seed string
}

type Options struct {
	Mode         string // dark | light
	Scheme       string
	HighContrast bool
}

// Active returns the token set for the requested mode. Generation always
// produces both modes; this selects.
func (t Tokens) Active(mode string) Tokens { return t } // tokens are mode-resolved at Generate
```

`generate.go`:

```go
type Generator struct {
	CacheDir string // defaults to $XDG_CACHE_HOME/sysc-shell
	Matugen  string // defaults to "matugen" (PATH lookup)
}

//go:embed matugen/config.toml
var matugenConfig string

//go:embed matugen/tpl.json
var matugenTemplate string

// Generate renders the palette for src. It is single-flight per process by
// construction: callers (Registry reload path) serialize. One queued rerun is
// the caller's concern, not the generator's.
func (g Generator) Generate(src Source, opts Options) (Tokens, error) {
	if g.Matugen == "" {
		g.Matugen = "matugen"
	}
	dir := g.CacheDir
	if dir == "" {
		dir = filepath.Join(os.Getenv("XDG_CACHE_HOME"), "sysc-shell")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Fallback, nil // ponytail: cache dir failure degrades to fallback, never blocks startup
	}

	cfgPath := filepath.Join(dir, "matugen.toml")
	tplPath := filepath.Join(dir, "matugen-template.json")
	outPath := filepath.Join(dir, "colors.json")
	// config.toml embeds input_path/output_path relative to the config file.
	cfg := strings.ReplaceAll(matugenConfig, "@TPL@", tplPath)
	cfg = strings.ReplaceAll(cfg, "@OUT@", outPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		return Fallback, nil
	}
	if err := os.WriteFile(tplPath, []byte(matugenTemplate), 0o644); err != nil {
		return Fallback, nil
	}

	args := []string{"-c", cfgPath, "-t", scheme(opts), "--prefer", "saturation"}
	if opts.HighContrast {
		args = append(args, "--contrast", "1")
	}
	switch src.Kind {
	case "wallpaper":
		args = append(args, "image", src.Seed)
	case "hex", "stock":
		args = append(args, "color", "hex", src.Seed)
	default:
		return Fallback, nil
	}

	cmd := exec.Command(g.Matugen, args...)
	if err := cmd.Run(); err != nil {
		return Fallback, nil // ponytail: any matugen failure degrades to fallback
	}
	tok, err := parseColors(outPath, opts.Mode)
	if err != nil {
		return Fallback, nil
	}
	return tok, nil
}
```

`parseColors` decodes `{"colors":{"dark":{token:hex},"light":{...}}}` into `Tokens` for the
requested mode, tolerating missing optional tokens by substituting `Fallback` field-by-field.

`matugen/config.toml` (embedded):

```toml
[templates.shell]
input_path = "@TPL@"
output_path = "@OUT@"
```

`matugen/tpl.json` (embedded): one JSON object mapping token names to
`{{colors.<token>.dark.hex}}` and `{{colors.<token>.light.hex}}` for all eleven tokens, e.g.:

```json
{"dark":{"surface":"{{colors.surface.dark.hex}}", ...}, "light":{...}}
```

**Step 4: Run to verify pass**

Run: `go test ./internal/theme/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/theme/
git commit -m "feat(theme): matugen token generation with compiled-in fallback"
```

---

### Task 4: Theme integration — Tokens become the render source

**Files:**
- Modify: `internal/shell/bar.go` (theme resolution)
- Modify: `internal/shell/registry.go` (generate at startup + on reload)
- Test: `internal/shell/theme_test.go`

Design mapping: fg → `on_surface`, bg → `surface`, accent → `primary`, muted →
`on_surface_variant`; error and radius carry over. The bar and M3 widgets render from the same
resolved tokens, so the whole shell follows the generated palette. Reduced-motion and high-contrast
are read from config here; motion itself lands in Task 9.

**Step 1: Write the failing test**

```go
func TestTokensResolveToBarTheme(t *testing.T) {
	tok := theme.Tokens{
		Surface: "#111318", OnSurface: "#e2e2e6", Primary: "#a8c7fa",
		OnSurfaceVariant: "#c3c6cf", Error: "#ffb4ab",
	}
	th := ThemeFromTokens(tok, 12)
	if th.Background != tok.Surface || th.Foreground != tok.OnSurface ||
		th.Accent != tok.Primary || th.Muted != tok.OnSurfaceVariant ||
		th.Error != tok.Error || th.Radius != 12 {
		t.Fatalf("mapping wrong: %+v", th)
	}
}

func TestRegistryGeneratesThemeAtStartup(t *testing.T) {
	// fake matugen on PATH (reuse internal/theme test helper via export_test or
	// duplicate the two-line stub here)
	reg := NewRegistryWith(cfgWithWallpaperSource, deps{ThemeGen: fakeGen})
	if reg.Tokens() == theme.Tokens{} {
		t.Fatal("registry must hold generated tokens after construction")
	}
}
```

**Step 2: Run to verify failure** — `go test ./internal/shell/ -run Theme` → FAIL.

**Step 3: Implement**

- Add `ThemeFromTokens(tok theme.Tokens, radius int) Theme` in `bar.go` performing the mapping
  above (Theme is the existing M3 shell theme struct consumed by `NewWithTheme`).
- `Registry` gains `tokens theme.Tokens` and `themeGen theme.Generator`. Construction path:
  `NewRegistry` calls the generator with the config's `ThemeConfig` + `Accessibility.HighContrast`;
  fallback is returned by the generator itself, so startup cannot fail on theming.
- Reload path (`PrepareConfig`): regenerate before building replacement bars; bars are rebuilt
  with `ThemeFromTokens` under acquire-before-release exactly like today's config-only reload.
  ponytail: one regeneration per reload; if generation is slow the reload blocks — acceptable,
  matugen runs in tens of ms.
- `ReducedMotion()` accessor on Registry reads config; Task 9 consumes it.

**Step 4: Run to verify pass** — `go test ./internal/shell/` → PASS (full suite).

**Step 5: Commit**

```bash
git add internal/shell/
git commit -m "feat(shell): render from generated Material 3 tokens"
```

---

### Task 5: MetricsService

**Files:**
- Modify: `go.mod` (add sysc-metrics + replace)
- Create: `internal/services/metrics.go`
- Test: `internal/services/metrics_test.go`

Design D10: lease-counted like the clock; ~1 s poll while leased; process-lifetime ring buffer
(~2 min) so history survives close/open; `Valid == false` samples are kept and rendered as
"collecting" by the consumer; sampling pauses at zero leases.

**Step 1: Add the dependency**

```bash
go mod edit -require=github.com/Nomadcxx/sysc-metrics@v0.0.0
go mod edit -replace=github.com/Nomadcxx/sysc-metrics=/home/nomadx/sysc-metrics
go mod tidy
```

(The replace path is the local checkout until sysc-metrics is published; the plan executor keeps
the absolute path used by the build machine's worktree layout — record any deviation in the
commit body.)

**Step 2: Write the failing tests**

```go
type fakeSampler struct {
	n   int
	err error
}

func (f *fakeSampler) Sample() (metrics.CPUSnapshot, error) {
	f.n++
	if f.err != nil {
		return metrics.CPUSnapshot{}, f.err
	}
	return metrics.CPUSnapshot{Valid: f.n > 1, Usage: float64(f.n) / 100}, nil
}

func TestMetricsLeaseCounting(t *testing.T) {
	m := NewMetrics(10*time.Millisecond, &fakeSampler{}, ...) // inject all samplers
	if m.Running() {
		t.Fatal("must not run before first lease")
	}
	l1, _ := m.Acquire()
	l2, _ := m.Acquire()
	if !m.Running() {
		t.Fatal("must run while leased")
	}
	l1.Release()
	if !m.Running() {
		t.Fatal("must still run with one lease left")
	}
	l2.Release()
	if m.Running() {
		t.Fatal("must stop when last lease releases")
	}
}

func TestMetricsRingBufferSurvivesLeaseGap(t *testing.T) {
	m := NewMetrics(1*time.Millisecond, fakeSamplers...)
	l, _ := m.Acquire()
	waitForSamples(t, m, 3)
	l.Release()
	hist := m.History("cpu")
	if len(hist) < 3 {
		t.Fatalf("history lost across lease gap: %d", len(hist))
	}
}

func TestMetricsReleaseIdempotent(t *testing.T) {
	m := NewMetrics(time.Second, fakeSamplers...)
	l, _ := m.Acquire()
	l.Release()
	l.Release() // must not panic or double-stop
}
```

**Step 3: Run to verify failure** — `go test ./internal/services/ -run Metrics` → FAIL.

**Step 4: Implement**

Mirror the clock's structure exactly (same file's sibling, same idiom):

```go
// Metrics owns the sysc-metrics samplers behind the same lease contract as
// Clock. Samplers want one sequential polling owner; Metrics is that owner.
type Metrics struct {
	mu      sync.Mutex
	samplers map[string]Sampler // "cpu", "memory", "fs", "block", "net", "uptime"
	ring    map[string]*ring    // capacity ~120 entries (2 min at 1 s)
	leases  int
	stop    chan struct{}
	interval time.Duration
	running bool
}

// Sampler is the shape every sysc-metrics per-resource sampler satisfies.
// It is declared here, not in sysc-metrics, because it is this service's
// seam, not the library's contract.
type Sampler interface{ Sample() (any, error) }
```

 ponytail: `Sampler` returns `any` because the six sysc-metrics samplers have distinct snapshot
types and the ring stores them opaquely; consumers type-assert per resource key. If that proves
awkward in Task 11, split into six typed getters — not before.

- `Acquire() (*MetricsLease, error)`: first lease starts the poll goroutine (`time.Ticker` at
  interval; each tick samples every sampler sequentially; append to ring; notify via the same
  invalidation channel pattern the clock uses so the open monitor panel repaints).
- `Release`: idempotent, nil-safe; last lease stops the ticker goroutine. Ring persists.
- `History(key string) []Sample` returns a copy of the ring contents.
- `Close()` releases everything and stops (Registry.Close calls it, same as the clock).
- First/discontinuous samples keep `Valid == false` in the ring; consumers decide rendering.

**Step 5: Run to verify pass** — `go test ./internal/services/` → PASS.

**Step 6: Commit**

```bash
git add go.mod go.sum internal/services/
git commit -m "feat(services): lease-counted metrics sampler with history ring"
```

---

### Task 6: Panel model — placement math and single-instance state

**Files:**
- Create: `internal/shell/panel.go`
- Test: `internal/shell/panel_test.go`

Pure logic only: no Wayland imports. Placement per design §Placement; single-instance semantics
per design D4.

**Step 1: Write the failing tests**

```go
func TestClampKeepsPanelInsideOutput(t *testing.T) {
	out := ui.Rect{W: 1920, H: 1080}
	cases := []struct{ desired, size, pad, want int }{
		{-50, 400, 8, 8},
		{2000, 400, 8, 1512}, // 1920-400-8
		{760, 400, 8, 760},
	}
	for _, c := range cases {
		if got := clampAxis(c.desired, c.size, out.W, c.pad); got != c.want {
			t.Fatalf("clamp(%d,%d)=%d want %d", c.desired, c.size, got, c.want)
		}
	}
}

func TestPanelLargerThanOutputClampsToPadding(t *testing.T) {
	if got := clampAxis(0, 2000, 1080, 8); got != 8 {
		t.Fatalf("oversized panel must sit at padding, got %d", got)
	}
}

func TestAnchorMarginsForTopBar(t *testing.T) {
	g := Placement{BarEdge: "top", Output: ui.Rect{W: 1920, H: 1080},
		BarZone: 40, Gap: 8, Padding: 8, Panel: ui.Rect{W: 700, H: 520}, Align: "center"}
	m := g.Margins()
	// top bar: anchor top+left; margin.top clears the reserved zone + gap.
	if m.Top != 48 || m.Left != 610 { // (1920-700)/2
		t.Fatalf("margins wrong: %+v", m)
	}
}

func TestSingleInstanceToggleAndMove(t *testing.T) {
	ps := &PanelSet{}
	if ps.Toggle(PanelSession, 1) != Opened {
		t.Fatal("first toggle opens")
	}
	if ps.Toggle(PanelSession, 1) != Closed {
		t.Fatal("same-output toggle closes")
	}
	ps.Toggle(PanelSession, 1)
	if ps.Toggle(PanelSession, 2) != Moved {
		t.Fatal("other-output toggle closes+reopens there")
	}
	if _, where := ps.Open(PanelSession); where != 2 {
		t.Fatal("panel must now live on output 2")
	}
	if ps.Toggle(PanelMonitor, 1) != Opened {
		t.Fatal("different panel id is independent")
	}
}
```

**Step 2: Run to verify failure** — `go test ./internal/shell/ -run 'Clamp|Anchor|SingleInstance'` → FAIL.

**Step 3: Implement**

```go
type PanelID uint8

const (
	PanelClockCalendar PanelID = iota
	PanelMonitor
	PanelSession
	PanelSettings // accepted by IPC once Tranche 4B lands; inert here
)

func (p PanelID) String() string { ... "clock","calendar" map both clock+calendar ... }
```

Wait — clock and calendar are one popout (design §Popouts). IPC names it `clock`; the calendar
grid is its content. Keep `PanelClock` as the id; `panel.toggle {"panel":"clock"}`.

```go
// Placement computes layer-shell anchor + margins for one panel instance.
// All values logical px. The panel anchors the bar's edge plus left, so both
// offsets are exact; niri never chooses placement for an anchored surface.
type Placement struct {
	BarEdge string // top | bottom (left/right bars: post-M2 edges)
	Output  ui.Rect
	BarZone int // bar reserved zone incl. gap, from our own config
	Gap, Padding int
	Panel   ui.Rect // W,H set; X,Y computed
	Align   string // left | center | right
}

type Margins struct{ Top, Bottom, Left, Right int }

func clampAxis(desired, size, extent, pad int) int {
	if size+2*pad > extent {
		return pad
	}
	if desired < pad {
		return pad
	}
	if max := extent - size - pad; desired > max {
		return max
	}
	return desired
}

func (p Placement) Margins() Margins {
	x := alignX(p)
	x = clampAxis(x, p.Panel.W, p.Output.W, p.Padding)
	y := clampAxis(p.Panel.H /*desired irrelevant*/, p.Panel.H, p.Output.H, p.Padding)
	_ = y
	switch p.BarEdge {
	case "top":
		return Margins{Top: p.BarZone + p.Gap, Left: x}
	default: // bottom
		return Margins{Bottom: p.BarZone + p.Gap, Left: x}
	}
}

func alignX(p Placement) int {
	switch p.Align {
	case "left":
		return p.Padding
	case "right":
		return p.Output.W - p.Panel.W - p.Padding
	default:
		return (p.Output.W - p.Panel.W) / 2
	}
}
```

Vertical clamping: panels open against the bar edge; if `BarZone + Gap + Panel.H + Padding >
Output.H`, shrink panel height to fit (the render step consumes the clamped size). Add
`(p Placement) FittedSize() (w, h int)` implementing that and test it:

```go
func TestFittedSizeShrinksTallPanel(t *testing.T) {
	p := Placement{BarEdge: "top", Output: ui.Rect{W: 800, H: 600}, BarZone: 40, Gap: 8, Padding: 8, Panel: ui.Rect{W: 700, H: 900}}
	_, h := p.FittedSize()
	if h != 600-40-8-8 {
		t.Fatalf("height must shrink to fit: %d", h)
	}
}
```

Single-instance state:

```go
type ToggleResult uint8

const (
	Opened ToggleResult = iota
	Closed
	Moved
)

// PanelSet tracks which panel is open where. One instance per panel id,
// process-wide (design D4). Guarded by Registry.mu; no own lock.
type PanelSet struct {
	open map[PanelID]uint32 // panel -> output global
}

func (ps *PanelSet) Toggle(p PanelID, output uint32) ToggleResult { ... }
func (ps *PanelSet) Open(p PanelID) (PanelID, uint32) { ... } // reports where open
func (ps *PanelSet) Close(p PanelID) { ... }
```

**Step 4: Run to verify pass** — `go test ./internal/shell/` → PASS.

**Step 5: Commit**

```bash
git add internal/shell/panel.go internal/shell/panel_test.go
git commit -m "feat(shell): panel placement math and single-instance state"
```

---

### Task 7: Wayland client — auxiliary surfaces and keyboard (Milestone 2 modification)

This is the one M2 change the design requires: today `OutputHost` maps exactly one bar surface
and the client binds no keyboard. Panels need runtime-created layer surfaces (shield + panel) with
per-surface callbacks, pointer routing, and key delivery. Split into three commits; after each,
the entire existing M2 test suite must stay green.

**Files:**
- Modify: `internal/platform/wayland/host.go`
- Modify: `internal/platform/wayland/client.go`
- Modify: `internal/platform/wayland/pointer.go`
- Create: `internal/platform/wayland/aux.go`
- Create: `internal/platform/wayland/keyboard.go`
- Test: `internal/platform/wayland/aux_test.go`
- Test: `internal/platform/wayland/keyboard_test.go`

#### Task 7a: Extract `surfaceUnit` (no behavior change)

**Step 1: Baseline**

Run: `go test ./internal/platform/wayland/`
Expected: PASS (record the count; it must match after the refactor).

**Step 2: Refactor**

Move the per-surface machinery out of `OutputHost` into a new struct in `host.go`:

```go
// surfaceUnit owns one layer surface and its buffer lifecycle. The bar is
// the first unit; auxiliary panels are more.
type surfaceUnit struct {
	id string // "bar" or the AuxSpec id

	surface  *client.Surface
	layer    *layershell.ZwlrLayerSurfaceV1
	scale    *fractionalscale.WpFractionalScaleV1
	viewport *viewporter.WpViewport

	ss    *surfaceState
	sched *render.Scheduler

	current  *generation
	retiring []*generation
	genID    int

	frameCallback *client.Callback
	cleanup cleanupStack

	app HostCallbacks
}
```

`OutputHost` keeps output identity/state (`global`, `proxy`, `connector`, transform/mode,
`policy`, `state`, `alive`, close budget) and gains `bar *surfaceUnit` plus
`aux map[string]*surfaceUnit` (initialized empty). Every function that took `*OutputHost` for
surface work — `createBar`, `applyGeometryRequests`, `onConfigure`, `onPreferredScale`,
`onBufferRelease`, `sweepRetired`, `renderJob`, `onLayerClosed` — takes the unit (plus the host
where output-level state is needed). `nextJob` iterates the bar unit then aux units in map order.

No new tests in this step; the existing suite is the test.

**Step 3: Run**

Run: `go test ./internal/platform/wayland/ && go test ./...`
Expected: PASS, same test count as baseline.

**Step 4: Commit**

```bash
git add internal/platform/wayland/
git commit -m "refactor(wayland): extract surfaceUnit from OutputHost"
```

#### Task 7b: Auxiliary surface open/close

**Step 1: Write the failing test** (`aux_test.go`, using the existing fake-compositor harness —
`startFakeNiri` and the lifecycle-test patterns):

```go
func TestAuxSurfaceOpensConfiguresRendersAndCloses(t *testing.T) {
	// harness: one fake output, bar mapped (reuse lifecycle_test setup)
	reqs := make(chan AuxRequest, 4)
	configured := make(chan struct{}, 1)
	rendered := make(chan struct{}, 1)
	// open a shield then a panel on the same output
	reqs <- AuxRequest{Output: outGlobal, Open: &AuxSpec{
		ID: "shield:session", Namespace: "sysc-shell-shield",
		Layer: layershell.ZwlrLayerShellV1LayerOverlay,
		Width: 0, Height: 0, // fill output
		ExclusiveZone: -1, Keyboard: ZwlrKeyboardNone,
		Callbacks: HostCallbacks{
			Configure: func(w, h, s int) error { configured <- struct{}{}; return nil },
			Render:    func(p []byte, w, h, st int) error { rendered <- struct{}{}; return nil },
			Handle:    func(Event) bool { return false },
		},
	}}
	<-configured
	<-rendered
	// panel above shield: opened second, same layer
	reqs <- AuxRequest{Output: outGlobal, Open: &AuxSpec{ID: "panel:session", ...}}
	// close both
	reqs <- AuxRequest{Output: outGlobal, ID: "panel:session"}
	reqs <- AuxRequest{Output: outGlobal, ID: "shield:session"}
	// harness asserts surfaces destroyed; bar still mapped and rendering
}

func TestAuxCloseUnknownIDIsNoOp(t *testing.T) { ... }

func TestAuxRequestsDrainOnShutdown(t *testing.T) { ... } // Run returns cleanly with pending reqs
```

**Step 2: Run to verify failure** — `go test ./internal/platform/wayland/ -run Aux` → FAIL.

**Step 3: Implement** (`aux.go`)

```go
// AuxSpec describes one auxiliary layer surface. Callbacks travel with the
// spec because the app supplies them per surface at open time.
type AuxSpec struct {
	ID        string
	Namespace string
	Layer     layershell.ZwlrLayerShellV1Layer
	Anchor    uint32 // layershell anchor bits; 0 = none
	MarginTop, MarginBottom, MarginLeft, MarginRight int32
	Width, Height int32 // 0 fills that axis
	ExclusiveZone int32
	Keyboard  uint32 // zwlr_layer_surface_v1 keyboard_interactivity
	Callbacks HostCallbacks
}

// AuxRequest opens (Open != nil) or closes (Open == nil, ID set) one aux
// surface on the output identified by its wl_registry global.
type AuxRequest struct {
	Output uint32
	ID     string
	Open   *AuxSpec
}
```

- `Callbacks` gains `Aux <-chan AuxRequest` (optional; nil disables) and
  `DropAux func(output uint32, id string)` (optional; called when an aux surface dies, whether by
  request, compositor close, or output loss).
- `Run`'s select loop gains the `Aux` case (wake-channel pattern identical to `Invalidations`).
- `owner.openAux(h *OutputHost, spec *AuxSpec) error`: create unit exactly like `createBar`
  (CreateSurface → GetLayerSurface with `spec.Layer` and `spec.Namespace` → set size/anchor/
  margins/exclusive-zone/keyboard via layer requests → fractional scale + viewport → initial
  commit), insert into `h.aux[spec.ID]`. If the id already exists, destroy the old unit first
  (replace semantics; single-instance is enforced app-side, this is defense).
- `owner.closeAux(h *OutputHost, id string)`: destroy proxies via the unit's cleanup stack,
  delete from map, call `DropAux`.
- Output destruction (`destroyGlobals` / host drop) closes all aux units first, notifying
  `DropAux` for each.
- Reload semantics: `reloadConfig` closes every aux surface before committing new bars
  (documented ceiling: open panels do not survive a config reload; the user reopens them).

**Step 4: Run to verify pass** — `go test ./internal/platform/wayland/` → PASS (all).

**Step 5: Commit**

```bash
git add internal/platform/wayland/
git commit -m "feat(wayland): auxiliary layer surfaces with per-surface callbacks"
```

#### Task 7c: Keyboard binding and event routing

**Step 1: Write the failing tests** (`keyboard_test.go` + pointer additions):

```go
func TestKeyboardKeysRouteToFocusedAuxSurface(t *testing.T) {
	// open panel with Keyboard exclusive; fake compositor sends keyboard enter
	// for the panel surface, then key press 1 (KEY_ESC)
	// assert panel HostCallbacks.Handle received Event{Kind: EventKeyPress, Key: 1}
}

func TestKeyboardEnterClearsOnLeave(t *testing.T) { ... }

func TestPointerRoutesToAuxSurfaceUnderPointer(t *testing.T) {
	// fake compositor sends pointer enter on shield surface id;
	// shield Handle receives motion/button events; bar Handle receives none
}

func TestBarUnaffectedByKeyboardBinding(t *testing.T) {
	// bar stays keyboard-none; keys never reach bar Handle
}
```

**Step 2: Run to verify failure** — FAIL.

**Step 3: Implement**

- `client.go` Event: add kinds and field:

```go
const (
	EventPointerMotion EventKind = iota
	EventPointerPress
	EventPointerRelease
	EventPointerLeave
	EventPointerEnter
	EventKeyPress
	EventKeyRelease
)

type Event struct {
	Kind   EventKind
	X, Y   float64
	Button uint32
	Serial uint32
	Key    uint32 // evdev code (wl_keyboard keycode minus 8), key events only
}
```

- `keyboard.go`: when `onSeatCapabilities` reports keyboard, bind `wl_keyboard`. Handlers:
  - Keymap: ignore. ponytail: 4A needs only layout-independent keys (Escape/Tab/arrows/
    Enter/Space), so no xkbcommon; 4B's text input arrives via text-input-v3 preedit, and full
    keymap parsing is only needed if direct character input without IME ever becomes a requirement.
  - Enter(surface): find the unit owning that surface across hosts; set `o.keyFocus = unit`.
  - Leave: `o.keyFocus = nil`.
  - Key(time, key, state): if `o.keyFocus != nil`, deliver
    `Event{Kind: EventKeyPress|EventKeyRelease, Key: key - 8, Serial: serial}` to the unit's
    `app.Handle`; a true return triggers the same invalidation path pointer events use.
- `pointer.go`: wl_pointer enter/leave already carry the surface; extend routing from
  bar-only to "whichever unit owns the entered surface" (bar or aux). Motion/button/axis go to
  that unit. Leave clears.
- Aux units created with `Keyboard: exclusive` get `layer.SetKeyboardInteractivity(1)` in
  `openAux`; the compositor then sends keyboard enter automatically (niri honors this — verified
  in research).

**Step 4: Run to verify pass** — `go test ./internal/platform/wayland/` → PASS (all), plus full
`go test ./...`.

**Step 5: Commit**

```bash
git add internal/platform/wayland/
git commit -m "feat(wayland): keyboard binding and per-surface event routing"
```

---

### Task 8: Control vocabulary — node kinds, layout, roving focus

**Files:**
- Modify: `internal/ui/tree.go`
- Create: `internal/ui/column.go`
- Create: `internal/ui/focus.go`
- Test: `internal/ui/column_test.go`
- Test: `internal/ui/focus_test.go`

Controls shipping with 4A consumers (design D7): button (KindButton exists), label (KindText),
separator, tabs, graphs. Every focusable node carries accessible name + role (gate item).

**Step 1: Write the failing tests**

```go
func TestColumnLayoutStacksAndCentersText(t *testing.T) {
	root := &Node{Kind: KindColumn, Gap: 8, Padding: 12, Children: []*Node{
		{Kind: KindText, Text: "Power"},
		{Kind: KindSeparator},
		{Kind: KindButton, Text: "Lock", Name: "Lock", Role: "button", Focusable: true},
	}}
	LayoutColumn(root, Rect{W: 300, H: 400}, measure) // measure: 7px/char, 16 high
	// assert: children stacked top-to-bottom with gap 8, padding 12,
	// each child width = 300-24 (fill), heights from measure/KindSeparator (1px)
}

func TestFocusOrderIsTreeOrder(t *testing.T) {
	root := samplePanelTree() // column with nested rows
	f := Focusables(root)
	if len(f) != 4 || f[0].Text != "Lock" {
		t.Fatalf("focus order wrong: %v", f)
	}
}

func TestRovingIndexWrapsAndClamps(t *testing.T) {
	r := &Roving{Count: 3}
	r.Next(); r.Next(); r.Next()
	if r.Index() != 0 { t.Fatal("must wrap") }
	r.Prev()
	if r.Index() != 2 { t.Fatal("must wrap back") }
}

func TestTabsActivateByArrow(t *testing.T) {
	tabs := &Node{Kind: KindTabs, Children: []*Node{
		{Kind: KindText, Text: "CPU"}, {Kind: KindText, Text: "Memory"},
	}}
	LayoutColumn(&Node{Children: []*Node{tabs}}, Rect{W: 400, H: 30}, measure)
	if got := tabs.Active(); got != 0 { t.Fatal("default first tab") }
}
```

**Step 2: Run to verify failure** — `go test ./internal/ui/ -run 'Column|Focus|Roving|Tabs'` → FAIL.

**Step 3: Implement**

`tree.go` additions:

```go
const (
	KindRow Kind = iota
	KindText
	KindMeter
	KindButton
	KindColumn
	KindSeparator
	KindTabs
	KindGraph
)

type Node struct {
	Kind     Kind
	Text     string
	Value    float64   // tabs: active index
	Width    int
	Padding  int
	Gap      int
	Action   string
	Bounds   Rect
	Children []*Node

	// Accessibility and keyboard (Milestone 4). Name/Role are mandatory on
	// every Focusable node; the gate asserts them.
	Focusable bool
	Name      string
	Role      string

	Series []float64 // KindGraph: samples 0..1, oldest first
}

func (n *Node) Active() int { return int(n.Value) }
```

`column.go`: `LayoutColumn(root *Node, r Rect, m MeasureText)` — vertical stack mirroring the
existing bar row layout: padding inset, children fill width, heights from measure (text), fixed
(KindSeparator = 1), or intrinsic (KindTabs = text height, KindGraph = node.Width as height hint
via `Width` reuse — document that for graphs `Width` means height); gap between children; recurse
into KindColumn/KindRow children.

`focus.go`:

```go
// Focusables flattens the tree in traversal order, returning focusable nodes.
func Focusables(root *Node) []*Node { ... }

// Roving tracks the single focus index for one panel.
type Roving struct{ idx, Count int }

func (r *Roving) Index() int
func (r *Roving) Next()      // wrap forward
func (r *Roving) Prev()      // wrap back
func (r *Roving) Set(i int)  // clamp
```

**Step 4: Run to verify pass** — `go test ./internal/ui/` → PASS.

**Step 5: Commit**

```bash
git add internal/ui/
git commit -m "feat(ui): column layout, panel node kinds, roving focus"
```

---

### Task 9: Rounded corners and shadows in the renderer

**Files:**
- Modify: `internal/render/canvas.go`
- Create: `internal/render/mask.go`
- Test: `internal/render/mask_test.go`

Design D6: SDF rounded-rect alpha masks cached per (radius,w,h); pre-blurred shadow textures
cached per (w,h,radius). Both composite through the existing `blendMask`. Two elevations only.

**Step 1: Write the failing tests**

```go
func TestRoundedMaskCornersTransparentCenterOpaque(t *testing.T) {
	m := RoundedMask(12, 100, 60)
	if m.AlphaAt(0, 0) != 0 { t.Fatal("corner must be transparent") }
	if m.AlphaAt(50, 30) != 255 { t.Fatal("center must be opaque") }
	if m.AlphaAt(12, 12) == 0 || m.AlphaAt(12, 12) == 255 {
		// edge of the arc: partial coverage expected
	}
}

func TestRoundedMaskCacheReuses(t *testing.T) {
	a := RoundedMask(12, 100, 60)
	b := RoundedMask(12, 100, 60)
	if a != b { t.Fatal("same key must return same mask") }
}

func TestShadowTextureExtendsBeyondBounds(t *testing.T) {
	s := ShadowTexture(100, 60, 12, ElevPanel)
	if s.Bounds().Dx() <= 100 { t.Fatal("shadow must spread beyond panel") }
	// alpha decays outward: center-of-edge > far corner
}

func TestCanvasFillRoundedMatchesMask(t *testing.T) {
	// render 40x40 rounded rect into canvas; corner pixel untouched,
	// center pixel = color
}
```

**Step 2: Run to verify failure** — `go test ./internal/render/ -run 'Rounded|Shadow'` → FAIL.

**Step 3: Implement** (`mask.go`)

```go
type Elevation int

const (
	ElevPanel Elevation = iota // popout surfaces
	ElevMenu                   // in-panel menus (reserved; same texture today)
)

var (
	maskMu   sync.Mutex
	masks    = map[maskKey]*image.Alpha{}
	shadows  = map[shadowKey]*image.Alpha{}
)

// RoundedMask returns a cached alpha mask: full coverage inside the rounded
// rect, zero outside, linear coverage on the arc (SDF distance clamped to
// one pixel). Exact per-pixel coverage keeps edges clean without AA passes.
func RoundedMask(radius, w, h int) *image.Alpha { ... }

// ShadowTexture returns a cached pre-blurred rounded-rect shadow. The
// texture is the panel size plus spread margin; alpha = rounded rect,
// box-blurred three passes (approximates gaussian), scaled by elevation.
// ponytail: pre-blurred textures, not realtime blur — two elevations,
// cached per size; revisit only if memory or variety demands it.
func ShadowTexture(w, h, radius int, e Elevation) *image.Alpha { ... }
```

`canvas.go` additions:

```go
// FillRounded fills a rounded rectangle using the cached mask.
func (c *Canvas) FillRounded(r ui.Rect, radius int, col Color) {
	blendMask(c, RoundedMask(radius, r.W, r.H), r.X, r.Y, col)
}

// DrawShadow composites the cached shadow texture offset so the panel rect
// sits centered over it.
func (c *Canvas) DrawShadow(r ui.Rect, radius int, e Elevation, col Color) { ... }
```

Spread/alpha per elevation: ElevPanel blur 12 alpha 0.55 (Noctalia-measured values from
prior-art), ElevMenu blur 8 alpha 0.45.

**Step 4: Run to verify pass** — `go test ./internal/render/` → PASS.

**Step 5: Commit**

```bash
git add internal/render/
git commit -m "feat(render): cached rounded masks and pre-blurred shadows"
```

---

### Task 10: Panel host — surfaces, rendering, keyboard, motion

**Files:**
- Create: `internal/shell/panelhost.go`
- Modify: `internal/shell/registry.go` (open/close/toggle + DropAux + Invalidations routing)
- Modify: `internal/platform/wayland/client.go` (`Invalidation` gains `SurfaceID string`;
  owner routes id-tagged invalidations to the matching aux unit's scheduler)
- Test: `internal/shell/panelhost_test.go`

This joins Task 6 (model), Task 7 (transport), Task 8 (controls), Task 9 (paint) into the open
panel. Popout content builders come in Task 11; this task wires a placeholder builder so the
machinery is testable first.

**Step 1: Write the failing tests**

```go
func TestOpenPanelSendsShieldThenPanel(t *testing.T) {
	// registry with fake wayland request sink (channel recorder)
	reg := newTestRegistry(t)
	reg.OpenPanel(PanelSession, outGlobal, Trigger{BarEdge: "top", BarZone: 40, Align: "center"})
	reqs := reg.drainAux()
	if len(reqs) != 2 || !strings.HasPrefix(reqs[0].Open.ID, "shield:") ||
		!strings.HasPrefix(reqs[1].Open.ID, "panel:") {
		t.Fatalf("expected shield then panel, got %+v", reqs)
	}
	if reqs[0].Open.ExclusiveZone != -1 || reqs[1].Open.ExclusiveZone != -1 {
		t.Fatal("both surfaces must use exclusive zone -1")
	}
	if reqs[1].Open.Keyboard != keyboardExclusive {
		t.Fatal("panel must request exclusive keyboard")
	}
}

func TestEscapeClosesPanel(t *testing.T) {
	// open panel, deliver Event{Kind: EventKeyPress, Key: 1} (KEY_ESC) to panel Handle
	// assert close requests for both surfaces and leases released
}

func TestTabMovesRovingFocus(t *testing.T) {
	// open panel with 3 focusables; Tab, Tab, Shift+Tab; assert focus index 1
}

func TestSpaceActivatesFocusedButton(t *testing.T) {
	// focus session "lock" button (fake locker records exec), press Space
}

func TestRevealAnimationInvalidatesUntilDone(t *testing.T) {
	// open panel with motion enabled; expect >= 5 invalidations within 200ms;
	// with ReducedMotion: exactly one render at final state
}

func TestReloadClosesOpenPanels(t *testing.T) { ... } // documented ceiling
```

**Step 2: Run to verify failure** — FAIL.

**Step 3: Implement**

`panelhost.go`:

```go
// PanelHost is one open panel: its two surfaces' callbacks, content tree,
// focus, leases, and reveal state. Owned by Registry; all calls under
// Registry.mu unless noted.
type PanelHost struct {
	id     PanelID
	output uint32
	place  Placement
	root   *ui.Node
	focus  []*ui.Node
	roving ui.Roving
	leases []releaser // clock/metrics leases acquired at open
	animStart time.Time
	build  func(*PanelHost) // content builder, Task 11
}

type Trigger struct {
	BarEdge string
	BarZone int
	Align   string // section of the triggering widget; "" = center (hotkey)
	OutW, OutH int
}

type releaser interface{ Release() }
```

Registry methods:

```go
// OpenPanel opens (or moves) one panel instance. It acquires the panel's
// service leases before requesting surfaces (acquire-before-release, same
// discipline as config reload).
func (r *Registry) OpenPanel(id PanelID, output uint32, trig Trigger) error
func (r *Registry) ClosePanel(id PanelID)
func (r *Registry) TogglePanel(id PanelID, output uint32, trig Trigger) error
func (r *Registry) DropAux(output uint32, surfaceID string) // wayland callback
```

- `OpenPanel`: PanelSet.Toggle decides Opened/Moved/Closed; on Moved, close first (close requests
  + lease release), then open on the new output. Placement from Task 6 with `Panels.Gap/Padding`
  config. Build content tree (placeholder builder: column with a label; Task 11 replaces per id).
  Acquire leases for the id (clock: `r.clock.Acquire(boundary)` finest of consumers; monitor:
  `r.metrics.Acquire()`; session: none). Send shield AuxRequest then panel AuxRequest.
- HostCallbacks for the panel unit:
  - `Configure`: store logical size; `ui.LayoutColumn(root, ...)` with the fitted size.
  - `Render`: `canvas.DrawShadow` → `canvas.FillRounded(surface bg, radius 12)` → paint nodes
    (reuse M3's paint path for text/buttons; separator = 1px line in `outline`; tabs = row of
    labels with active underlined in `primary`; graph = filled polyline of Series scaled to
    bounds) → focus ring: 2px `primary` outline around `focus[roving.Index()].Bounds`.
    Reveal: if animating, apply alpha + slide offset toward the bar edge
    (fade the whole frame by scaling drawn alpha; offset = `8 * (1 - t)` px).
  - `Handle`: pointer — hit-test focusables (Rect.Contains), set roving on press, activate on
    release if same node (M3 press/release matching pattern); keys — evdev codes:
    KEY_ESC 1 → close; KEY_TAB 15 (+KEY_LEFTSHIFT 42 tracked from press/release) → roving
    Next/Prev; KEY_LEFT/RIGHT/UP/DOWN 105/106/103/108 → arrows (tabs switch active on
    left/right, content repaints); KEY_SPACE 57 / KEY_ENTER 28 → activate focused.
    Activation dispatches `node.Action`: session actions run their command (Task 11), tab
    switches set `Value`. Any state change returns true (owner invalidates).
- Shield unit callbacks: Configure no-op, Render transparent frame (input region only),
  Handle: any press → `ClosePanel` + return true.
- Motion: if `!cfg.Accessibility.ReducedMotion`, set `animStart = time.Now()` and start a
  ticker goroutine pushing `wayland.Invalidation{SurfaceID: panelID}` every 16 ms until
  t ≥ 1, then one final invalidation. Reduced motion: no ticker, render final state.
  ponytail: 16ms ticker per animating panel; at most one panel animates at a time by
  single-instance, so this cannot pile up.
- `DropAux`: remove host, release leases (idempotent), update PanelSet. Covers compositor-side
  close and output loss.
- `Registry.Close()` closes all panels (leases already released by ClosePanel path).

`client.go`: `Invalidation{Connector string; SurfaceID string}` — owner routes SurfaceID-tagged
invalidations to the matching aux unit's `sched.Invalidate()`; Connector-tagged behave as today.

**Step 4: Run to verify pass** — `go test ./internal/shell/ ./internal/platform/wayland/` → PASS.

**Step 5: Commit**

```bash
git add internal/shell/ internal/platform/wayland/client.go
git commit -m "feat(shell): panel host with shield, exclusive keyboard, and reveal motion"
```

---

### Task 11: Popout content builders

**Files:**
- Create: `internal/shell/popout_clock.go`
- Create: `internal/shell/popout_monitor.go`
- Create: `internal/shell/popout_session.go`
- Test: `internal/shell/popout_clock_test.go`
- Test: `internal/shell/popout_monitor_test.go`
- Test: `internal/shell/popout_session_test.go`

Each builder produces the `*ui.Node` tree for its panel and its activation behavior. Register them
in the `PanelHost.build` dispatch from Task 10.

#### 11a: clock/calendar

**Step 1: Failing tests**

```go
func TestCalendarGridSevenColumns(t *testing.T) {
	g := calendarGrid(time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC))
	if len(g.Weeks) < 4 || len(g.Weeks[0]) != 7 {
		t.Fatalf("grid shape wrong: %+v", g)
	}
	if !g.Weeks[0][0].InMonth && g.Weeks[0][0].Day == 0 {
		t.Fatal("leading cells must be blanks or previous-month days")
	}
	// Aug 30 2026 is a Sunday: find the cell with Day==30 & InMonth, assert col 0
}

func TestCalendarMarksToday(t *testing.T) { ... }
```

**Step 2: Verify failure. Step 3: Implement** — `calendarGrid(now time.Time)` from stdlib `time`
only: first of month, `Weekday()` offset, days via `Date(y, m+1, 0).Day()`. Tree: column →
big clock row (leased clock, format "15:04" + date line), month/year header with prev/next
buttons (Action "cal-prev"/"cal-next" re-build the tree with a month offset held on the
PanelHost), then a 7-column grid of day labels; today gets `primary` background. Panel size
target ~360x420 logical.

**Step 4: Pass. Step 5: Commit** `feat(shell): clock and calendar popout`

#### 11b: system-monitor

**Step 1: Failing tests**

```go
func TestMonitorTabsPerResource(t *testing.T) {
	h := newMonitorHost(fakeMetricsWith(history...))
	tabNames := childTexts(h.root, KindTabs)
	// CPU, Memory, Filesystems, Block, Network, Uptime
}

func TestMonitorGraphSeriesFromRing(t *testing.T) {
	// graph node Series == fake ring contents, normalized 0..1
}

func TestMonitorInvalidSamplesRenderCollecting(t *testing.T) {
	// ring with Valid==false snapshots -> label "collecting", no graph series point
}
```

**Step 2-3: Implement** — tabs (KindTabs) over the six sysc-metrics resources; each tab body:
current-value labels + KindGraph with `Series` from `Metrics.History(key)` normalized per
resource (usage fractions already 0..1; fs/block/net normalized against their max in the ring).
Snapshot access: type-assert the ring's `any` entries per resource key; `Valid == false` entries
are skipped in Series and a "collecting" label shows when the latest is invalid. Panel size
~640x480. Repaint: metrics tick invalidation (Task 5 notifies through the registry invalidation
channel with the panel's SurfaceID while open).

**Step 4-5: Pass. Commit** `feat(shell): system monitor popout over sysc-metrics`

#### 11c: session/power

**Step 1: Failing tests**

```go
func TestSessionActionsList(t *testing.T) {
	h := newSessionHost(configWithLocker("swaylock"))
	names := focusableNames(h.root)
	// Lock, Log out, Suspend, Reboot, Power off — in that order
}

func TestLockHiddenWithoutLocker(t *testing.T) {
	h := newSessionHost(configWithoutLocker)
	// 4 actions, no Lock
}

func TestSessionExecMapping(t *testing.T) {
	fake := fakeExec(t) // records argv; PATH stub for loginctl + locker
	activate(h, "Log out"); fake.expect("loginctl", "terminate-session", "self")
	activate(h, "Suspend"); fake.expect("loginctl", "suspend")
	activate(h, "Reboot"); fake.expect("loginctl", "reboot")
	activate(h, "Power off"); fake.expect("loginctl", "poweroff")
	activate(h, "Lock"); fake.expect("swaylock") // configured locker, shell-split
}
```

**Step 2-3: Implement** — button grid (column of KindButton, each Focusable with Name/Role).
Activation runs the command via `exec.Command` (locker string split on whitespace — ponytail:
no shell quoting; lockers with quoted args are a documented ceiling), closes the panel first for
logout/reboot/poweroff so the session teardown finds no stuck surface, and reports exec failure by
leaving the panel open with an error label (rendered in `error` token). Destructive actions get no
confirmation dialog in 4A — parity note: neither reference shell confirms by default; the
confirmation row is a future knob.

**Step 4-5: Pass. Commit** `feat(shell): session power menu with loginctl actions`

---

### Task 12: IPC socket, methods, CLI

**Files:**
- Create: `internal/ipc/server.go`
- Create: `internal/ipc/client.go`
- Modify: `cmd/sysc-shell/main.go` (subcommand dispatch)
- Test: `internal/ipc/server_test.go`

Design §IPC: `$XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock`, 0700, newline-delimited JSON,
`{"id","method","params"}` → `{"id","ok"|"error"}`. Bind failure doubles as single-instance
check. Methods: `panel.toggle|open|close`, `status`; `osd.step` reserved for 4B.

**Step 1: Write the failing tests**

```go
func TestServerRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ipc.v1.sock")
	srv := NewServer(sock, Handlers{
		Panel: func(action, panel string) error { got = action + ":" + panel; return nil },
		Status: func() map[string]any { return map[string]any{"version": "test"} },
	})
	go srv.Serve(ctx)
	out, err := Call(ctx, sock, "panel.toggle", map[string]string{"panel": "session"})
	if err != nil || !strings.Contains(out, `"ok"`) { t.Fatalf(...) }
	if got != "toggle:session" { t.Fatalf(...) }
}

func TestUnknownMethodErrors(t *testing.T) { ... `"error"` envelope ... }

func TestStaleSocketReplaced(t *testing.T) {
	// create dead socket file first; Serve must unlink and bind
}

func TestLiveSocketFailsAsSingleInstance(t *testing.T) {
	// first Serve holds the socket; second Serve returns single-instance error
}

func TestPanelParamValidation(t *testing.T) {
	// panel "bogus" -> error envelope, handler never called
}
```

**Step 2: Run to verify failure. Step 3: Implement**

```go
// Handlers carry the server's effects. The server itself is transport only.
type Handlers struct {
	Panel  func(action, panel string) error
	Status func() map[string]any
}

type Server struct {
	path string
	h    Handlers
	ln   net.Listener
}

func NewServer(path string, h Handlers) *Server
func (s *Server) Serve(ctx context.Context) error // accept loop; per-conn goroutine
func (s *Server) Close() error

// Call sends one request and returns the raw response line.
func Call(ctx context.Context, sock, method string, params any) (string, error)
```

- Serve: `os.MkdirAll(dir, 0o700)`; bind; on `EADDRINUSE` probe-connect — success means a live
  shell → return single-instance error; failure means stale file → unlink, rebind once.
- Per connection: `bufio.Scanner` lines, `json.Unmarshal` into
  `struct{ ID json.Number; Method string; Params json.RawMessage }`, dispatch, write one line.
  Panel params decode to `{"panel": string}` validated against the known ids
  (`clock|calendar|system-monitor|session|settings`; `settings` returns "not yet available" until
  4B). Unknown method → `{"id":…,"error":"unknown method"}`. Malformed JSON → error envelope,
  connection stays up.
- Call: dial with 2 s deadline, write request line, read one line, return it.

CLI in `main.go`:

```go
// sysc-shell ipc <method> [params-json]
// Connects, sends one request, prints the response line, exits 0 on "ok",
// 1 on "error" or transport failure.
```

Parse `os.Args`: `ipc` subcommand → method + optional params JSON (default `{}`), socket path
derived the same way the server derives it, print response.

**Step 4: Run to verify pass** — `go test ./internal/ipc/` → PASS.

**Step 5: Commit**

```bash
git add internal/ipc/ cmd/sysc-shell/
git commit -m "feat(ipc): versioned unix socket with panel verbs and cli"
```

---

### Task 13: Process wiring and hotkey documentation

**Files:**
- Modify: `cmd/sysc-shell/main.go`
- Modify: `internal/shell/registry.go` (Aux request channel plumbing)
- Create: `docs/niri-hotkeys.md`

**Step 1: Wire**

- `Registry` gains `AuxRequests() <-chan wayland.AuxRequest`; main passes it as
  `Callbacks.Aux`, plus `DropAux: registry.DropAux`.
- main starts the IPC server (goroutine, ctx-scoped) with handlers bound to the registry:
  `panel.toggle` → `registry.TogglePanelByName(panel, focusedOutputTrigger)` — IPC triggers have
  no pointer anchor: output = the output of the bar whose connector matches the Niri projection's
  focused window output (M3 projection holds window→output); Align "" (centered). No focused
  output → first bar's output.
- Single-instance: IPC `Serve` returning the single-instance error aborts startup with a clear
  message (design §IPC).
- Reload path: `reloadConfig` in the wayland client already closes aux surfaces (Task 7b); the
  registry's `DropAux` keeps PanelSet consistent — verify with the Task 10 reload test.

**Step 2: Hotkey docs** (`docs/niri-hotkeys.md`)

Documented niri keybinds (user adds to `~/.config/niri/config.kdl`; compositor owns keys, shell
owns panels — DMS pattern):

```kdl
bind {
    Super+P { spawn "sysc-shell" "ipc" "panel.toggle" `{"panel":"clock"}`; }
    Super+M { spawn "sysc-shell" "ipc" "panel.toggle" `{"panel":"system-monitor"}`; }
    Super+X { spawn "sysc-shell" "ipc" "panel.toggle" `{"panel":"session"}`; }
}
```

Note in the doc: media/brightness keys ship with Tranche 4B's OSD.

**Step 3: Run everything** — `go build ./... && go test ./...` → PASS.

**Step 4: Commit**

```bash
git add cmd/ internal/shell/registry.go docs/niri-hotkeys.md
git commit -m "feat(shell): wire ipc and panel requests, document hotkeys"
```

---

### Task 14: Gate tests — integration and live verification

**Files:**
- Modify: `tests/integration/README.md` (live checklist)
- Create: `tests/integration/panel_gate_test.go` (fake-compositor coverage)
- Create: `tests/integration/focus_fallthrough_test.go` (live, tagged)

The roadmap exit gate evaluated on 4A surfaces (design §Tranche gate).

**Step 1: Fake-compositor integration tests** (extend the M2/M3 harness):

```go
func TestGateExclusiveZoneUnchangedByPanels(t *testing.T) {
	// map bar, record its exclusive zone; open+close each panel; assert unchanged
}

func TestGateKeyboardOnlyCoversControls(t *testing.T) {
	// for each popout: Tab through every focusable, assert each receives focus
	// ring in turn and Space/Enter/arrow operate it — no pointer events sent
}

func TestGateAccessibleNamesAndRoles(t *testing.T) {
	// walk every popout tree: Focusable => Name != "" && Role != ""
}

func TestGatePlacementWithinBounds(t *testing.T) {
	// open each panel on outputs with scale 1.0/1.5/2.0 and 90/180 transforms;
	// assert anchor+margins keep the panel inside logical output bounds
}

func TestGateReducedMotionInstant(t *testing.T) {
	// reduced-motion config: first render is final state, no ticker invalidations
}
```

**Step 2: Live verification checklist** (append to `tests/integration/README.md`, run on the
live Niri session, record results in the commit body of the gate commit):

1. **Focus fall-through (design D3, risk 1):** open session panel from a foot window's bar, press
   Escape; verify keyboard focus returns to foot without any `focus-window` call. If focus lands
   wrong: implement the contingency — on close, `niri msg action focus-window <tracked
   active_window_id>` (projection already tracks it), then re-run.
2. **Shield pointer delivery (risk 2):** with a panel open, click outside it; the panel closes,
   the click does not reach the window beneath (shield consumed it).
3. **Exclusive beats windows:** with a panel open, click a window, press Escape — the panel
   closes (keyboard never left it).
4. **Compositor keybinds survive:** with a panel open, press a niri keybind (e.g. Super+Return) —
   it fires.
5. **Fullscreen does not hide panels:** fullscreen a window; open the clock panel — visible
   (Overlay layer).
6. **Hotkeys:** add the documented binds; Super+P/M/X toggle panels from anywhere.
7. **High contrast:** set `accessibility.high-contrast: true`, reload; tokens measurably differ
   (compare colors.json).
8. **Multi-output:** trigger the same panel from each bar; it closes and reopens per output.

**Step 3: Run** — `go test ./...` green; live checklist executed and recorded.

**Step 4: Commit**

```bash
git add tests/
git commit -m "test(shell): tranche 4A gate coverage and live checklist"
```

---

## Done criteria

- `go build ./...` and `go test ./...` green from a clean checkout.
- All Task 14 fake-compositor gate tests pass; live checklist recorded.
- `gofmt -l .` empty; no new dependencies beyond sysc-metrics (replace).
- Design doc risks updated with verification outcomes (focus fall-through, shield delivery,
  matugen color flags).
- No panel code path touches the bar's exclusive zone; no second surface ever requests keyboard
  while a panel is open.

## Skipped, and when to add

- Per-panel OnDemand keyboard demotion → config knob when a pointer-first panel wants it.
- Open-near-click pointer anchoring → when a panel's trigger position matters visually.
- Panel state surviving config reload → if users complain; close-and-reopen is honest today.
- Confirmation dialogs for destructive session actions → parity knob, not gate material.
- D-Bus (PrepareForSleep, inhibitors) → the first-party lockscreen milestone.
