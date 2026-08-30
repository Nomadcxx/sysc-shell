# Panel Foundation Implementation Plan — Milestone 4, Tranche 4A

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship the Milestone 4 gate surfaces: panel machinery (shield + panel layer surfaces,
Exclusive keyboard, single instance), floating placement with clamping, shell-rendered rounding and
shadows, the 4A control vocabulary with roving keyboard focus, matugen core theming, the
clock/calendar and session/power popouts, and the v1 IPC socket with documented niri hotkeys.

**Architecture:** Panels reuse the Milestone 2/3 ownership shape. `shell.Registry` (process-scoped)
owns panel state the way it owns bars; the Wayland client gains generic auxiliary-surface support
(create/destroy at runtime, per-surface callbacks, keyboard delivery) because today it maps exactly
one bar surface per output — that is the one Milestone 2 modification this plan requires, and it is
explicit. All new logic is pure-Go stdlib. **This tranche adds no module dependency: `go.mod` is
untouched.** M3 already pins `sysc-metrics@v0.2.0` and owns its sampler lifecycle. The
system-monitor popout reuses `services.Metrics`; 4A adds no wrapper service or dependency (design D10).

**Tech Stack:** Go 1.26 stdlib, sysc-wayland v0.1.x generated bindings (layer-shell,
fractional-scale, viewporter), matugen 4.2.0 (external binary, exec), loginctl (external binary,
exec).

**Design:** [2026-08-30-panel-foundation-design.md](2026-08-30-panel-foundation-design.md)
**Research:** [2026-08-30-panels-and-controls-research.md](2026-08-30-panels-and-controls-research.md)

---

## Prerequisites (verify before Task 1)

1. Milestones 1, 2, and 3 have merged to main. `sysc-5` still tracks the owner-deferred M2
   multi-output hardware qualification; record it as unrun, not passed. Tranches 3B, 3C, and 3D
   supply the metrics service, graph node, config fields, pumps, and invalidation paths that the
   system monitor consumes and that every 4A edit must preserve.
   Rebase the existing `milestone/panels-controls` worktree onto this main before Task 2.
2. The merged tree contains the M3 surface this plan builds on:
   - `internal/shell/registry.go` — `Registry` with `NewHost(global, connector)`,
     `DropHost(global)`, `UpdateClock(now) []uint32`, `UpdateMetrics(snap) []uint32`,
     `UpdateWeather(reading) []uint32`, `UpdateNiri(snap) []uint32`,
     `PrepareConfig(cfg, hosts)`, `Clock() *services.Clock`, `Invalidations() <-chan wayland.Invalidation`.
   - `internal/shell/bar.go` — `Bar` with `Handle(wayland.Event) bool`, `NewWithTheme(theme, policy, connector)`.
   - `internal/services/clock.go` — `NewClock()`, `Acquire(boundary) (*Lease, error)`,
     `(*Lease).Release()` (idempotent, nil-safe), `Updates() <-chan time.Time`, `Close()`.
   - `internal/platform/wayland/client.go` — `Run(ctx, cfg, Callbacks)` with
     `Callbacks{NewHost, PrepareConfig, DropHost, Invalidations, Reloads, ConfigPath}`,
     `HostCallbacks{Configure, Render, Handle}`, `Event{Kind, X, Y, Button, Serial}` (pointer kinds only).
   - `cmd/sysc-shell/main.go` — `run(ctx)` wiring Registry into `wayland.Callbacks`.
3. `matugen` (4.2.0) and `loginctl` exist on `PATH`. Both are optional at runtime — a missing
   matugen falls back to the compiled-in palette, and a missing loginctl hides the session actions —
   but the live gate needs them present. (`wpctl` and `brightnessctl` are Tranche 4B's concern.)
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
	if c.ThemeGen.Source != "wallpaper" || c.ThemeGen.Scheme != "scheme-tonal-spot" || c.ThemeGen.Mode != "dark" {
		t.Fatalf("theme defaults wrong: %+v", c.ThemeGen)
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

// Validation goes through Parse, like every other field, so failures name
// their exact path and there is only one validation entry point.
func TestThemeSourceValidation(t *testing.T) {
	for _, bad := range []string{"gradient", "auto", ""} {
		body := []byte(`{"theme-gen":{"source":"` + bad + `"}}`)
		if _, err := Parse(body); err == nil {
			t.Fatalf("source %q must be rejected", bad)
		} else if !strings.Contains(err.Error(), "theme-gen.source") {
			t.Fatalf("error %q must name the field path", err)
		}
	}
	for _, ok := range []string{"wallpaper", "hex"} {
		body := []byte(`{"theme-gen":{"source":"` + ok + `","seed":"#3050a0"}}`)
		if _, err := Parse(body); err != nil {
			t.Fatalf("source %q must be accepted: %v", ok, err)
		}
	}
}

func TestHexSeedValidation(t *testing.T) {
	if _, err := Parse([]byte(`{"theme-gen":{"source":"hex","seed":"blue"}}`)); err == nil {
		t.Fatal("hex source with non-hex seed must fail")
	}
	if _, err := Parse([]byte(`{"theme-gen":{"source":"hex","seed":"#3050a0"}}`)); err != nil {
		t.Fatalf("hex seed must pass: %v", err)
	}
}

// The colour fields generation now owns must be gone from the schema, not
// silently ignored.
func TestRetiredThemeColourFieldsAreRejected(t *testing.T) {
	for _, field := range []string{"background", "foreground", "accent", "muted", "error"} {
		body := []byte(`{"theme":{"` + field + `":"#101418"}}`)
		if _, err := Parse(body); err == nil {
			t.Fatalf("retired theme.%s must be rejected, not ignored", field)
		}
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
	Source string // wallpaper | hex; 4B adds stock names with its catalog
	Seed   string // image path or #RRGGBB — meaning follows Source
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

Add to `Config`: `ThemeGen ThemeConfig`, `Accessibility`, `Session`, `Panels`. The field is named
`ThemeGen` because `Config.Theme` already exists and stays — it now carries only `radius` (see below).
Extend `Default()` with the values asserted above.

**Validate inside `Parse`, not through a new `Config.Valid()`.** Milestone 2 and 3 have no `Valid()`
method: validation lives in `applyBar`/`validateBar`/`resolveItem` and every failure names its exact
field path through `pathErr` — `config: theme.source: "gradient" is not one of wallpaper, hex`.
A second entry point would diverge from that one and would return errors with no field path. Add
`applyThemeGen`, `applyAccessibility`, `applySession`, `applyPanels` beside the existing helpers,
each validating as it merges: source enum, mode enum, hex-seed shape when source is `hex`,
non-negative gap and padding.

**Remove the colour fields from `Theme`** — `background`, `foreground`, `accent`, `muted`, `error` —
and their `colorPattern` validation. Generation owns colour now (design §Theming core), so leaving
them would make a user's edit a silent no-op, which this project's validation rule forbids. Keep
`theme.radius`. Remove `config.Theme.BackgroundOpaque()`: `shell.Theme.BackgroundOpaque()` reads
the generated `surface` token, and `HostCallbacks.OpaqueBackground` carries that resolved boolean
to the Wayland client. The platform layer must not import shell or theme types.

Mirror wire types in `load.go` with pointer fields (`*string`, `*bool`, `*int`) and merge over
defaults exactly like the existing `wireBar` pattern.

**Keep unknown-field rejection.** Landed M3 already uses `json.Decoder.DisallowUnknownFields()`.
Deleting the retired colour fields from `wireTheme` must keep that decoder path, including the
second decode that rejects trailing JSON values:

```go
dec := json.NewDecoder(bytes.NewReader(data))
dec.DisallowUnknownFields()
if err := dec.Decode(&wire); err != nil {
	return Config{}, fmt.Errorf("config: %w", err)
}
```

The retired-field test proves that the existing rejection still covers the reduced schema. Existing
stray keys already fail on main; Task 2 must not claim that behavior as a new M4 change.

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
	Kind string // wallpaper | hex; 4B extends this with resolved stock seeds
	Seed string
}

type Options struct {
	Mode         string // dark | light
	Scheme       string
	HighContrast bool
}

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
		base, err := os.UserCacheDir()
		if err != nil { return Fallback, nil }
		dir = filepath.Join(base, "sysc-shell")
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
	case "hex":
		args = append(args, "color", "hex", src.Seed)
	default:
		return Fallback, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.Matugen, args...)
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
	// Put a fake matugen executable on PATH and point XDG_CACHE_HOME at t.TempDir().
	reg := NewRegistry(cfgWithWallpaperSource)
	if reg.Tokens() == (theme.Tokens{}) {
		t.Fatal("registry must hold generated tokens after construction")
	}
}
```

**Step 2: Run to verify failure** — `go test ./internal/shell/ -run Theme` → FAIL.

**Step 3: Implement**

- Add `ThemeFromTokens(tok theme.Tokens, radius int) Theme` in `bar.go` performing the mapping
  above (Theme is the existing M3 shell theme struct consumed by `NewWithTheme`).
- `Registry` gains `tokens theme.Tokens` and a concrete `themeGen theme.Generator`. Construction path:
  `NewRegistry` calls the generator with the config's `ThemeConfig` + `Accessibility.HighContrast`;
  fallback is returned by the generator itself, so startup cannot fail on theming.
- Reload path (`PrepareConfig`): regenerate before building replacement bars; bars are rebuilt
  with `ThemeFromTokens` under acquire-before-release exactly like today's config-only reload.
  ponytail: one regeneration per reload; if generation is slow the reload blocks — acceptable,
  matugen runs in tens of ms.
- `ReducedMotion()` accessor on Registry reads config; Task 9 consumes it.
- Preserve the landed M3 registry state and construction: `services.Metrics`, `services.Weather`,
  their latest snapshots, `Weather.Reconfigure`, `UpdateMetrics`, `UpdateWeather`, `Tooltips`, and
  all three process pumps remain live. Add a regression test that a theme-only reload does not
  restart or drop a metrics or weather lease.

**Step 4: Run to verify pass** — `go test ./internal/shell/` → PASS (full suite).

**Step 5: Commit**

```bash
git add internal/shell/
git commit -m "feat(shell): render from generated Material 3 tokens"
```

---

### Task 5: Panel model — placement math and single-instance state

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
	if where, ok := ps.Output(PanelSession); !ok || where != 2 {
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
	PanelClock PanelID = iota
	PanelMonitor
	PanelSession
	PanelSettings // accepted by IPC once Tranche 4B lands; inert here
)

func (p PanelID) String() string { ... PanelClock: "clock", PanelMonitor: "system-monitor" ... }
```

Clock and calendar are one popout (design §Popouts). IPC names it `clock`; the calendar grid is
its content. The system-monitor spelling matches the public IPC name rather than the shorter Go
identifier.

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
func (ps *PanelSet) Output(p PanelID) (uint32, bool) { ... } // ordinary map lookup
func (ps *PanelSet) Close(p PanelID) { ... }
```

**Step 4: Run to verify pass** — `go test ./internal/shell/` → PASS.

**Step 5: Commit**

```bash
git add internal/shell/panel.go internal/shell/panel_test.go
git commit -m "feat(shell): panel placement math and single-instance state"
```

---

### Task 6: Wayland client — auxiliary surfaces and keyboard (Milestone 2 modification)

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

#### Task 6a: Extract `surfaceUnit` (no behavior change)

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

#### Task 6b: Auxiliary surface open/close

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
- Reload semantics: `reloadConfig` rebuilds bars and **leaves aux surfaces mapped**. A panel
  re-resolves its theme and content on its next frame, which is the same work a theme change already
  does. Do not tear panels down here: Tranche 4B's settings modal is itself an aux surface and writes
  the configuration on every change, so closing panels on reload would eject the user from settings
  on every toggle and would kill a visible OSD. Add a test that a mapped aux surface survives a
  reload and re-renders with the new tokens.

**Step 4: Run to verify pass** — `go test ./internal/platform/wayland/` → PASS (all).

**Step 5: Commit**

```bash
git add internal/platform/wayland/
git commit -m "feat(wayland): auxiliary layer surfaces with per-surface callbacks"
```

#### Task 6c: Keyboard binding and event routing

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
	Key    uint32 // evdev code from wl_keyboard.key, key events only
}
```

- `keyboard.go`: when `onSeatCapabilities` reports keyboard, bind `wl_keyboard`. Handlers:
  - Keymap: ignore. ponytail: 4A needs only layout-independent keys (Escape/Tab/arrows/
    Enter/Space), so no xkbcommon; 4B's text input arrives via text-input-v3 preedit, and full
    keymap parsing is only needed if direct character input without IME ever becomes a requirement.
  - Enter(surface): find the unit owning that surface across hosts; set `o.keyFocus = unit`.
  - Leave: `o.keyFocus = nil`.
  - Key(time, key, state): if `o.keyFocus != nil`, deliver
    `Event{Kind: EventKeyPress|EventKeyRelease, Key: key, Serial: serial}` to the unit's
    `app.Handle`; a true return triggers the same invalidation path pointer events use.
    `wl_keyboard.key` already carries the evdev code. The `+8` offset belongs only at an XKB
    lookup boundary; subtracting 8 here underflows Escape (`KEY_ESC == 1`).
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

### Task 7: Control vocabulary — node kinds, layout, roving focus

**Files:**
- Modify: `internal/ui/tree.go`
- Create: `internal/ui/column.go`
- Create: `internal/ui/focus.go`
- Test: `internal/ui/column_test.go`
- Test: `internal/ui/focus_test.go`

Controls shipping with 4A consumers (design D7): button (`KindButton` exists), label (`KindText`),
separator, and tabs. The system monitor reuses landed `KindGraph`; preserve it in the enum and
painter rather than adding another graph kind. Every focusable node carries accessible name + role.

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
```

**Step 2: Run to verify failure** — `go test ./internal/ui/ -run 'Column|Focus|Roving'` → FAIL.

**Step 3: Implement**

`tree.go` additions:

```go
const (
	KindRow Kind = iota
	KindText
	KindMeter
	KindButton
	KindGraph // landed M3 kind; preserve its numeric value
	KindColumn
	KindSeparator
	KindTab
)

type Node struct {
	Kind     Kind
	Text     string
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

}

func (n *Node) Active() int { return int(n.Value) }
```

`column.go`: `LayoutColumn(root *Node, r Rect, m MeasureText)` — vertical stack mirroring the
existing bar row layout: padding inset, children fill width, heights from measure (text), fixed
(KindSeparator = 1); gap between children; recurse
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

### Task 8: Rounded corners and shadows in the renderer

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

### Task 9: Panel host — surfaces, rendering, keyboard, motion

**Files:**
- Create: `internal/shell/panelhost.go`
- Modify: `internal/shell/registry.go` (open/close/toggle + DropAux + Invalidations routing)
- Modify: `internal/platform/wayland/client.go` (`Invalidation` gains `SurfaceID string`;
  owner routes id-tagged invalidations to the matching aux unit's scheduler)
- Test: `internal/shell/panelhost_test.go`

This joins Task 5 (model), Task 6 (transport), Task 7 (controls), and Task 8 (paint) into the open
panel. Popout content builders come in Task 10; this task wires a placeholder builder so the
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

func TestReloadKeepsOpenPanels(t *testing.T) { ... } // settings depends on this contract
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
	leases []*services.Lease // clock or metrics leases acquired at open
	animStart time.Time
	build  func(*PanelHost) // content builder, Task 10
}

type Trigger struct {
	BarEdge string
	BarZone int
	Align   string // empty in 4A; reserved for a future keyboard-accessible bar launcher
	OutW, OutH int
}

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
  config. Build content tree (placeholder builder: column with a label; Task 10 replaces per id).
  Acquire leases for the id: clock uses `r.clock.Acquire(boundary)`; system-monitor calls
  `monitorSelectors(r.cfg.ForConnector(connector))`, then acquires each selector from
  `r.metrics` at one second; session holds none. `monitorSelectors` returns CPU and memory plus
  the first configured filesystem, block, and network selector on that bar, at most one per
  source. Send shield AuxRequest then panel AuxRequest. Keep `[]*services.Lease`; do not add a
  one-implementation release interface.
- HostCallbacks for the panel unit:
  - `Configure`: store logical size; `ui.LayoutColumn(root, ...)` with the fitted size.
  - `Render`: `canvas.DrawShadow` → `canvas.FillRounded(surface bg, radius 12)` → paint nodes
    (reuse M3's paint path for text/buttons/graphs; separator = 1px line in `outline`; tabs are a
    row of buttons with the active tab underlined in `primary`;
    bounds) → focus ring: 2px `primary` outline around `focus[roving.Index()].Bounds`.
    Reveal: if animating, apply alpha + slide offset toward the bar edge
    (fade the whole frame by scaling drawn alpha; offset = `8 * (1 - t)` px).
  - `Handle`: pointer — hit-test focusables (Rect.Contains), set roving on press, activate on
    release if same node (M3 press/release matching pattern); keys — evdev codes:
    KEY_ESC 1 → close; KEY_TAB 15 (+KEY_LEFTSHIFT 42 tracked from press/release) → roving
    Next/Prev; KEY_LEFT/RIGHT/UP/DOWN 105/106/103/108 → arrows (composites move within on
    left/right, content repaints); KEY_SPACE 57 / KEY_ENTER 28 → activate focused.
    Activation dispatches `node.Action`: session actions run their command (Task 10), tab
    switches set `Value`. Any state change returns true (owner invalidates).
- Shield unit callbacks: Configure no-op, Render transparent frame (input region only),
  Handle: any press → `ClosePanel` + return true.
- Motion: if `!cfg.Accessibility.ReducedMotion`, set `animStart = time.Now()` and start a
  ticker goroutine pushing `wayland.Invalidation{SurfaceID: panelID}` every 16 ms until
  t ≥ 1, then one final invalidation. Reduced motion: no ticker, render final state.
  ponytail: 16ms ticker per animating panel; at most one panel animates at a time by
  single-instance, so this cannot pile up. Give the host a cancellation channel; close, move,
  output loss, and `Registry.Close` stop the ticker before releasing the host. Test that closing
  during reveal produces no later invalidation.
- `DropAux`: remove host, release leases (idempotent), update PanelSet. Covers compositor-side
  close and output loss.
- `UpdateMetrics`: after applying the snapshot to bars, enqueue a SurfaceID invalidation for the
  open system-monitor panel. The existing M3 process pump remains the only snapshot receiver.
- `Registry.Close()` closes all panels (leases already released by ClosePanel path).

`client.go`: extend the landed type to `Invalidation{Global uint32; SurfaceID string}`. The owner
routes SurfaceID-tagged invalidations to the matching aux unit's scheduler; Global-tagged bar
invalidations keep the M2/M3 reconnect-safe identity. Do not rekey them to connector.

**Step 4: Run to verify pass** — `go test ./internal/shell/ ./internal/platform/wayland/` → PASS.

**Step 5: Commit**

```bash
git add internal/shell/ internal/platform/wayland/client.go
git commit -m "feat(shell): panel host with shield, exclusive keyboard, and reveal motion"
```

---

### Task 10: Popout content builders

**Files:**
- Create: `internal/shell/popout_clock.go`
- Create: `internal/shell/popout_monitor.go`
- Create: `internal/shell/popout_session.go`
- Test: `internal/shell/popout_clock_test.go`
- Test: `internal/shell/popout_monitor_test.go`
- Test: `internal/shell/popout_session_test.go`

Each builder produces the `*ui.Node` tree for its panel and its activation behavior. Register them
in the `PanelHost.build` dispatch from Task 9.

#### 10a: clock/calendar

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

#### 10b: system-monitor

**Step 1: Failing tests**

```go
func TestMonitorSelectorsUseLandedMetricVocabulary(t *testing.T) {
	bar := config.Default().Bar
	bar.Right = append(bar.Right,
		config.Item{ID: "filesystem", Path: "/"},
		config.Item{ID: "network", Interface: "eth0", Direction: "rx"})
	got := monitorSelectors(bar)
	// CPU, memory, filesystem:/, network:eth0:rx; no second selector per source.
}

func TestMonitorUsesRegistrySnapshotAndHistory(t *testing.T) {
	// open against a registry snapshot; active tab label shows the current value
	// and its KindGraph.Values equal normalise(registry history for that selector).
}

func TestMonitorAbsentSampleShowsCollecting(t *testing.T) {
	// latest selector value absent: label "collecting", graph marked absent.
}

func TestMonitorLeaseReusesM3Service(t *testing.T) {
	// opening acquires CPU/memory on Registry.Metrics(); closing releases them;
	// no second service or direct sysc-metrics sampler exists in the popout.
}
```

**Step 2-3: Implement** — `monitorSelectors(config.Bar) []services.Selector` starts with CPU and
memory, then walks the focused bar's items in display order and keeps the first filesystem, block,
and network selector. Build one focusable `KindTab` per selector. The active body formats the newest
`Registry.sample` with a small `formatMonitorMetric(sel, snap)` helper that calls `Fraction` or
`Rate` and reuses `formatRate`; it paints a `KindGraph` from
`normalise(Registry.metrics.History(sel))`. An absent latest value renders `collecting` and an absent
graph. Left/Right changes the active tab through the Task 9 roving handler. Panel size target
~640x480. Do not import `github.com/Nomadcxx/sysc-metrics` or create a sampler in this file.

**Step 4-5: Pass. Commit** `feat(shell): system monitor over landed metrics service`

#### 10c: session/power

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
Resolve commands with `exec.LookPath`; use argv slices and no shell. The locker string uses
`strings.Fields`, so quoted arguments are a documented ceiling. Start the locker, then close the
panel if process creation succeeds. Run `loginctl` with a bounded context; close the panel after a
successful command and leave it open with an error label on failure. Destructive actions get no
confirmation dialog in 4A; neither reference shell confirms by default, and the confirmation row
is a future knob.

**Step 4-5: Pass. Commit** `feat(shell): session power menu with loginctl actions`

---

### Task 11: IPC socket, methods, CLI

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

- Serve: `os.MkdirAll(dir, 0o700)`; bind, then `os.Chmod(sock, 0o600)`; on `EADDRINUSE` probe-connect — success means a live
  shell → return single-instance error; failure means stale file → unlink, rebind once.
- Per connection: `bufio.Scanner` lines, `json.Unmarshal` into
  `struct{ ID json.Number; Method string; Params json.RawMessage }`, dispatch, write one line.
  Panel params decode to `{"panel": string}` validated against the known ids
  (`clock|system-monitor|session`; `settings` returns "not yet available" until 4B).
  Unknown method → `{"id":…,"error":"unknown method"}`. Malformed JSON → error envelope,
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

### Task 12: Process wiring and hotkey documentation

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
- Reload path: Task 6b leaves aux surfaces mapped. The registry rebuilds their content and theme
  in place; verify with `TestReloadKeepsOpenPanels` from Task 9.

**Step 2: Hotkey docs** (`docs/niri-hotkeys.md`)

Documented niri keybinds (user adds to `~/.config/niri/config.kdl`; compositor owns keys, shell
owns panels — DMS pattern):

```kdl
bind {
    Super+P { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"clock\"}"; }
    Super+M { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"system-monitor\"}"; }
    Super+X { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"session\"}"; }
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

### Task 13: Gate tests — integration and live verification

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
6. **Hotkeys:** add the documented binds; Super+P/M/X toggle clock, system-monitor, and session
   panels from anywhere.
7. **High contrast:** set `accessibility.high-contrast: true`, reload; tokens measurably differ
   (compare colors.json).
8. **Multi-output:** focus a window on each output and trigger the same panel through IPC; it closes
   and reopens on the newly focused output.

**Step 3: Run** — `go test ./...` green; live checklist executed and recorded.

**Step 4: Commit**

```bash
git add tests/
git commit -m "test(shell): tranche 4A gate coverage and live checklist"
```

---

## Done criteria

- `go build ./...` and `go test ./...` green from a clean checkout.
- All Task 13 fake-compositor gate tests pass; live checklist recorded.
- `gofmt -l .` empty; `git diff origin/main -- go.mod go.sum` empty — this tranche adds no
  dependency.
- Design doc risks updated with verification outcomes (focus fall-through, shield delivery,
  matugen color flags).
- No panel code path touches the bar's exclusive zone; no second surface ever requests keyboard
  while a panel is open.

## Skipped, and when to add

- Per-panel OnDemand keyboard demotion → config knob when a pointer-first panel wants it.
- Open-near-click pointer anchoring → when a panel's trigger position matters visually.
- Clickable bar launchers → when the bar gains a keyboard-focus contract. 4A opens panels through
  IPC and compositor hotkeys so it does not ship a pointer-only interactive bar node.
- Confirmation dialogs for destructive session actions → parity knob, not gate material.
- D-Bus (PrepareForSleep, inhibitors) → the first-party lockscreen milestone.
