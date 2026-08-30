# Panel Foundation Implementation Plan — Milestone 4, Tranche 4A

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship the Milestone 4 gate surfaces: panel machinery (shield + panel layer surfaces,
Exclusive keyboard, single instance), floating placement with clamping, shell-rendered rounding and
shadows, the 4A control vocabulary with roving keyboard focus, matugen core theming, the
clock/calendar and session/power popouts, and the v1 IPC socket with documented niri hotkeys.

**Architecture:** Tranche 3D supplies the generic auxiliary-surface lifecycle and application render
callbacks. This tranche extends that host with keyboard routing and in-place input updates, then uses a
process-wide root coordinator to ensure one interactive chain owns focus, serials, and leases. All new
logic is pure-Go stdlib. **This tranche adds no module dependency: `go.mod` is untouched.** The
system-monitor popout remains deferred under design D10.

**Tech Stack:** Go 1.26 stdlib, sysc-wayland v0.1.x generated bindings (layer-shell,
fractional-scale, viewporter), matugen 4.2.0 (external binary, exec), loginctl (external binary,
exec).

**Design:** [2026-08-30-panel-foundation-design.md](2026-08-30-panel-foundation-design.md)
**Research:** [2026-08-30-panels-and-controls-research.md](2026-08-30-panels-and-controls-research.md)

---

## Prerequisites (verify before Task 1)

1. Milestone 2 and Tranche 3A have merged. Tranche 3D Tasks 9–11 have also merged and passed their
   auxiliary lifecycle tests. Tranches 3B and 3C are not required. Execute from a fresh worktree that
   contains those prerequisites.
2. The merged tree contains the M3 surface this plan builds on:
   - `internal/shell/registry.go` — `Registry` with `NewHost(global, connector)`,
     `DropHost(global)`, `UpdateClock(now) []uint32`, `UpdateNiri(snap) []uint32`,
     `PrepareConfig(cfg, hosts)`, `Clock() *services.Clock`, `Invalidations() <-chan wayland.Invalidation`.
   - `internal/shell/bar.go` — `Bar` with `Handle(wayland.Event) bool`, `NewWithTheme(theme, policy, connector)`.
   - `internal/services/clock.go` — `NewClock()`, `Acquire(boundary) (*Lease, error)`,
     `(*Lease).Release()` (idempotent, nil-safe), `Updates() <-chan time.Time`, `Close()`.
   - `internal/platform/wayland/host.go`, `aux.go` — `surfaceUnit`, `AuxSpec`, `AuxRequest`, empty input
     regions, `Callbacks.Aux`, and `DropAux` with fake-compositor lifecycle coverage.
   - `internal/platform/wayland/client.go` — `Run(ctx, cfg, Callbacks)` with
     `HostCallbacks{Configure, Render, Handle}` and pointer-only `Event` values.
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
	for _, ok := range []string{"wallpaper", "hex", "stock"} {
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

Add to `Config`: `ThemeGen ThemeConfig`, `Accessibility`, `Session`, `Panels`. The field is named
`ThemeGen` because `Config.Theme` already exists and stays — it now carries only `radius` (see below).
Extend `Default()` with the values asserted above.

**Validate inside `Parse`, not through a new `Config.Valid()`.** Milestone 2 and 3 have no `Valid()`
method: validation lives in `applyBar`/`validateBar`/`resolveItem` and every failure names its exact
field path through `pathErr` — `config: theme.source: "gradient" is not one of wallpaper, hex, stock`.
A second entry point would diverge from that one and would return errors with no field path. Add
`applyThemeGen`, `applyAccessibility`, `applySession`, `applyPanels` beside the existing helpers,
each validating as it merges: source enum, mode enum, hex-seed shape when source is `hex`,
non-negative gap and padding.

**Remove the colour fields from `Theme`** — `background`, `foreground`, `accent`, `muted`, `error` —
and their `colorPattern` validation. Generation owns colour now (design §Theming core), so leaving
them would make a user's edit a silent no-op, which this project's validation rule forbids. Keep
`theme.radius`. `Theme.BackgroundOpaque()` moves to reading the generated `surface` token: opaque
when it is six-digit hex.

Mirror wire types in `load.go` with pointer fields (`*string`, `*bool`, `*int`) and merge over
defaults exactly like the existing `wireBar` pattern.

**Reject unknown fields.** `Parse` currently calls `json.Unmarshal`, which silently ignores any key
it does not recognise — so simply deleting the retired colour fields from `wireTheme` would make a
stale `theme.background` a silent no-op, which is the failure this change exists to remove. Replace
the call with a decoder that refuses them:

```go
dec := json.NewDecoder(bytes.NewReader(data))
dec.DisallowUnknownFields()
if err := dec.Decode(&wire); err != nil {
	return Config{}, fmt.Errorf("config: %w", err)
}
```

This matches the vocabulary rule the project already applies to widget ids — a typo is visible rather
than silently dropping something — and it is what makes the retired-field test above meaningful. Note
it is a behaviour change for any existing configuration carrying stray keys: those now fail at load
with the offending key named. There is no compatibility promise, and a named failure beats a silent
one.

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
	if reg.Tokens() == (theme.Tokens{}) {
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

### Task 5: Placement and process-wide interactive-root state

**Files:**
- Create: `internal/shell/panel.go`, `internal/shell/root.go`
- Test: `internal/shell/panel_test.go`, `internal/shell/root_test.go`

Pure logic only. The root coordinator is process-wide; it does not import Wayland.

**Step 1: Write failing placement tests**

Cover top, bottom, left, and right bar edges, output padding, oversized panels, and every section
alignment. All values are logical pixels.

```go
func TestPanelPlacementStaysInsideOutput(t *testing.T) {
	cases := []Placement{
		{BarEdge: "top", Output: ui.Rect{W: 1920, H: 1080}, BarZone: 40,
			Gap: 8, Padding: 8, Panel: ui.Rect{W: 700, H: 520}, Align: "center"},
		{BarEdge: "right", Output: ui.Rect{W: 1080, H: 1920}, BarZone: 44,
			Gap: 8, Padding: 8, Panel: ui.Rect{W: 500, H: 900}, Align: "end"},
	}
	for _, c := range cases {
		got := c.Fit()
		if !c.Output.ContainsRect(got.Bounds) {
			t.Fatalf("%+v placed outside %+v", got.Bounds, c.Output)
		}
	}
}

func TestOversizedPanelShrinksBeforePlacement(t *testing.T) { /* both axes */ }
func TestPanelStartsBeyondItsBarEdge(t *testing.T) { /* four-edge table */ }
```

Run: `go test ./internal/shell/ -run 'PanelPlacement|OversizedPanel' -v`

Expected: FAIL.

**Step 2: Implement placement**

```go
type Placement struct {
	BarEdge       string // top | bottom | left | right
	Output        ui.Rect
	Trigger       ui.Rect
	BarZone       int
	Gap, Padding  int
	Panel         ui.Rect
	Align         string // start | center | end
}

type FittedPlacement struct {
	Bounds ui.Rect
	Anchor uint32
	Margins Margins
}

type Margins struct{ Top, Bottom, Left, Right int32 }

func clampAxis(desired, size, extent, pad int) int {
	if size+2*pad > extent {
		return pad
	}
	if desired < pad {
		return pad
	}
	if max := extent-size-pad; desired > max {
		return max
	}
	return desired
}

func (p Placement) Fit() FittedPlacement { /* edge-aware fit, shrink, anchor, margins */ }
```

`Fit` shrinks the panel to the output minus padding and the bar reservation, centres it on
`Trigger` along the bar axis when a trigger exists, applies start/centre/end for hotkeys, then clamps.
Return an error for an unknown edge or non-positive usable area.

Run: `go test ./internal/shell/ -run 'PanelPlacement|OversizedPanel' -v`

Expected: PASS.

**Step 3: Write failing root-chain tests**

```go
func TestOpeningUnrelatedRootReplacesCurrentChain(t *testing.T) {
	var roots RootCoordinator
	roots.Open(Root{ID: "panel:clock", Output: 1})
	closed, _ := roots.Open(Root{ID: "panel:session", Output: 2})
	if closed.Root.ID != "panel:clock" || roots.Current().Root.ID != "panel:session" {
		t.Fatalf("replacement = %+v current = %+v", closed, roots.Current())
	}
}

func TestSameRootTriggerTogglesClosed(t *testing.T) { /* same id and output */ }
func TestAttachedChildMustBelongToCurrentRoot(t *testing.T) { /* reject stale parent generation */ }
func TestClosingRootReturnsWholeChain(t *testing.T) { /* child then root cleanup order */ }
func TestOnlyOneInteractiveRootExists(t *testing.T) { /* invariant after mixed operations */ }
```

Run: `go test ./internal/shell/ -run Root -v`

Expected: FAIL.

**Step 4: Implement the coordinator**

```go
type Root struct {
	ID         string
	Output     uint32
	Generation uint64
}

type RootChain struct {
	Root  Root
	Child *Root
}

type RootCoordinator struct {
	next    uint64
	current *RootChain
}

func (r *RootCoordinator) Open(root Root) (closed *RootChain, opened *RootChain)
func (r *RootCoordinator) Attach(parentGeneration uint64, child Root) error
func (r *RootCoordinator) Detach(childID string) *Root
func (r *RootCoordinator) Close(generation uint64) *RootChain
func (r *RootCoordinator) Current() *RootChain
```

`Open` toggles the same root on the same output; every other open returns the old chain for cleanup
and installs a new generation. `Attach` accepts one child only when the generation matches. Later tray
popups use that path. The coordinator owns identity and ordering only; `Registry` performs surface,
keyboard, serial, text-input, and lease cleanup in Task 9.

Run: `go test -count=1 ./internal/shell/`

Expected: PASS.

Commit:

```bash
git add internal/shell/panel.go internal/shell/panel_test.go internal/shell/root.go internal/shell/root_test.go
git commit -m "feat(shell): coordinate one interactive root"
```

---

### Task 6: Extend the 3D auxiliary host with input and updates

Tranche 3D already supplies `surfaceUnit`, basic `AuxSpec`/`AuxRequest` open-close, application
render callbacks, scale-aware buffers, empty input regions, and complete unit cleanup. This task adds
the M4 requirements. Do not duplicate or move the 3D host.

**Files:**
- Modify: `internal/platform/wayland/client.go`, `internal/platform/wayland/aux.go`
- Modify: `internal/platform/wayland/pointer.go`
- Create: `internal/platform/wayland/keyboard.go`
- Test: `internal/platform/wayland/aux_test.go`, `internal/platform/wayland/keyboard_test.go`

**Step 1: Verify the supplied seam**

Run:

```bash
go test -count=1 ./internal/platform/wayland/ -run Aux -v
rg -n 'type surfaceUnit|type AuxSpec|InputRegion|DropAux' internal/platform/wayland
```

Expected: the 3D lifecycle tests pass and each supplied symbol has one owner. Stop if 3D did not land;
do not recreate the seam in 4A.

**Step 2: Write failing keyboard and routing tests**

```go
func TestKeyboardRoutesToEnteredAuxUnit(t *testing.T) {
	// Open an Exclusive panel, send wl_keyboard.enter then KEY_ESC.
	// Only that unit's HostCallbacks.Handle receives the key.
}

func TestKeyboardLeaveClearsFocus(t *testing.T) { /* later keys go nowhere */ }
func TestPointerRoutesByEnteredSurface(t *testing.T) { /* bar, shield, panel table */ }
func TestBarRemainsKeyboardNone(t *testing.T) { /* no key delivery */ }
func TestDestroyedFocusedUnitClearsSeatState(t *testing.T) { /* key and pointer */ }
```

Run: `go test ./internal/platform/wayland/ -run 'Keyboard|PointerRoutes' -v`

Expected: FAIL.

**Step 3: Bind keyboard and route events per surface**

Extend events:

```go
const (
	EventPointerMotion EventKind = iota
	EventPointerPress
	EventPointerRelease
	EventPointerLeave
	EventPointerEnter
	EventPointerAxis
	EventKeyPress
	EventKeyRelease
)

type Event struct {
	Kind          EventKind
	X, Y          float64
	Button, Serial uint32
	Key           uint32
}
```

Bind `wl_keyboard` when the seat advertises it. Track key and pointer focus as `*surfaceUnit`, found
from the protocol event's `wl_surface`. Route subsequent events only to that unit. Clear focus on
leave, unit close, output loss, seat capability loss, and shutdown. A true callback result invalidates
that unit's scheduler.

Ignore the keymap in 4A:

```go
// ponytail: 4A uses layout-independent navigation keys only. Add xkbcommon
// when a consumer needs direct character input without text-input-v3.
```

Deliver the evdev code after the Wayland offset. Keep bar keyboard interactivity at None.

Run:

```bash
go test -race -count=1 ./internal/platform/wayland/
```

Expected: PASS.

**Step 4: Write failing `AuxUpdate` tests**

```go
func TestAuxUpdateChangesKeyboardWithoutRecreatingSurface(t *testing.T) {
	// Open keyboard None; update to OnDemand.
	// Assert one layer surface generation and a new committed keyboard request.
}

func TestAuxUpdateReplacesInputRegion(t *testing.T) {
	// Apply a union of card rectangles, then an empty region.
	// Assert both commits and no surface recreation.
}

func TestAuxUpdateRejectsUnknownUnitAndInvalidRect(t *testing.T) { /* typed error/log, no mutation */ }
func TestAuxUpdateIsGenerationSafe(t *testing.T) { /* stale generation cannot update replacement */ }
```

Run: `go test ./internal/platform/wayland/ -run AuxUpdate -v`

Expected: FAIL.

**Step 5: Implement in-place updates**

Add the current unit generation to open and update requests:

```go
type AuxUpdate struct {
	ID         string
	Generation uint64
	Keyboard   *uint32
	InputRegion *[]ui.Rect
}

type AuxRequest struct {
	Output uint32
	ID     string
	Open   *AuxSpec
	Update *AuxUpdate
}
```

Exactly one of `Open`, `Update`, or close-by-ID is valid. The owner validates the output, ID,
generation, keyboard enum, rectangle count, coordinates, and bounds before mutation. It applies changed
layer properties and input region, then commits the existing surface. Unknown or stale updates leave
the current unit unchanged. Keep the protocol owner path non-blocking.

Run:

```bash
go test -race -count=1 ./internal/platform/wayland/ && go test -count=1 ./...
```

Expected: PASS; fake-compositor counts prove no recreation.

Commit:

```bash
git add internal/platform/wayland/
git commit -m "feat(wayland): route input and update auxiliary surfaces"
```

---

### Task 7: Control vocabulary — node kinds, layout, roving focus

**Files:**
- Modify: `internal/ui/tree.go`
- Create: `internal/ui/column.go`
- Create: `internal/ui/focus.go`
- Test: `internal/ui/column_test.go`
- Test: `internal/ui/focus_test.go`

Controls shipping with 4A consumers (design D7): button (KindButton exists), label (KindText),
separator. `tabs` and `graphs` are deferred with the system-monitor popout that was their only
consumer (design D7, D10). Every focusable node carries accessible name + role (gate item).

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
	KindColumn
	KindSeparator
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

### Task 9: Panel host and root-coordinator integration

**Files:**
- Create: `internal/shell/panelhost.go`
- Modify: `internal/shell/registry.go`
- Modify: `internal/platform/wayland/client.go`
- Test: `internal/shell/panelhost_test.go`

This joins Task 5's root model, Task 6's transport, Task 7's controls, and Task 8's paint path.
Content builders arrive in Task 10; use one labelled placeholder here.

**Step 1: Write the failing ownership and lifecycle tests**

```go
func TestOpenPanelClosesTooltipAndOldRootBeforeMapping(t *testing.T) {
	reg := newTestRegistry(t)
	reg.showFixtureTooltip()
	if err := reg.OpenPanel(PanelClock, 1, fixtureTrigger("top")); err != nil {
		t.Fatal(err)
	}
	if err := reg.OpenPanel(PanelSession, 2, fixtureTrigger("bottom")); err != nil {
		t.Fatal(err)
	}
	// Request order: close tooltip; open clock shield+panel; close clock
	// panel+shield; open session shield+panel. One root remains.
}

func TestOpenPanelSendsShieldThenExclusivePanel(t *testing.T) {
	// Assert both use Overlay and exclusive zone -1; shield keyboard None,
	// panel keyboard Exclusive; sizes, regions, callbacks, and output match.
}

func TestSamePanelTriggerTogglesWholeRootClosed(t *testing.T) { /* releases leases */ }
func TestEscapeClosesRootChain(t *testing.T) { /* panel then shield */ }
func TestShieldPressClosesAndConsumes(t *testing.T) { /* one close path */ }
func TestDropEitherPanelSurfaceClosesSiblingAndRoot(t *testing.T) { /* idempotent */ }
func TestOutputLossReleasesRootKeyboardAndLeases(t *testing.T) { /* no stale generation */ }

func TestReloadKeepsPanelMappedAndRendersNewTheme(t *testing.T) {
	// Commit a valid theme reload. Assert no Aux close/open request, one
	// invalidation for the mapped panel, and Render observes the new tokens.
}

func TestRevealTickerStopsWhenRootIsReplaced(t *testing.T) { /* no late invalidations */ }
```

Keep roving focus coverage:

```go
func TestTabAndShiftTabMoveFocus(t *testing.T) { /* three focusables */ }
func TestSpaceAndEnterActivateFocusedButton(t *testing.T) { /* exact action once */ }
func TestArrowsRouteWithinComposite(t *testing.T) { /* no root escape */ }
```

Run: `go test ./internal/shell/ -run 'Panel|Root|ReloadKeeps' -v`

Expected: FAIL.

**Step 2: Implement the host**

```go
// PanelHost owns one panel root generation and its two auxiliary units.
// Registry.mu guards it; timer messages carry generation and never touch Wayland.
type PanelHost struct {
	id         PanelID
	generation uint64
	output     uint32
	place      FittedPlacement
	tree       *ui.Node
	focus      []*ui.Node
	roving     ui.Roving
	leases     []releaser
	animStart  time.Time
	build      func(*PanelHost)
}

type Trigger struct {
	BarEdge      string
	BarZone      int
	Align        string
	Source       ui.Rect
	OutputBounds ui.Rect
}

type releaser interface{ Release() }
```

Registry entry points:

```go
func (r *Registry) OpenPanel(id PanelID, output uint32, trig Trigger) error
func (r *Registry) ClosePanel(id PanelID)
func (r *Registry) TogglePanel(id PanelID, output uint32, trig Trigger) error
func (r *Registry) DropAux(output uint32, surfaceID string)
```

`OpenPanel` must:

1. validate the output and fit geometry before changing current state;
2. acquire the new root's leases;
3. close any tooltip through the 3D controller;
4. ask `RootCoordinator.Open` for replacement or toggle behavior;
5. close the returned old chain, release its keyboard/text-input/serial state and leases, and cancel
   its animation generation;
6. map the new shield, then panel. If either open fails, close both, release leases, and leave no root.

No two interactive root generations may remain mapped after the method returns.

Panel callbacks:

- `Configure` stores logical size and lays out `tree`.
- `Render` draws shadow, rounded surface, nodes, and a 2 px primary focus ring. Reveal applies the
  150 ms fade plus 8 px edge-relative slide; reduced motion paints the final state immediately.
- `Handle` preserves press/release matching. Escape closes the root. Tab and Shift+Tab move roving
  focus; arrows stay inside composites; Space and Enter activate the focused node.
- The shield renders transparent, retains a full input region, and consumes any press by closing the
  root.

A generation-tagged 16 ms ticker publishes `Invalidation{SurfaceID: panelSurfaceID}` until reveal
ends. Stop it on close, replacement, output loss, or shutdown.

`DropAux` routes by ID. Tooltip loss clears tooltip state. Loss of either panel unit closes its sibling,
clears the root generation, releases input ownership, and releases leases once.

Extend `Invalidation` with `SurfaceID string`; the Wayland owner routes an ID-tagged invalidation to
that auxiliary unit's scheduler and preserves connector-tagged bar behavior.

**Step 3: Preserve roots across accepted reload**

A successful reload replaces configuration and content builders under `Registry.mu`, rebuilds the
open panel tree, and invalidates its unit. It does not close or recreate the panel or shield. A rejected
candidate changes nothing. Tooltips remain transient and close on an accepted reload.

Run:

```bash
go test -race -count=1 ./internal/shell/ ./internal/platform/wayland/
```

Expected: PASS with one root, one keyboard owner, and no late timer work.

**Step 4: Commit**

```bash
git add internal/shell/ internal/platform/wayland/client.go
git commit -m "feat(shell): host panels in one root chain"
```

---

### Task 10: Popout content builders

**Files:**
- Create: `internal/shell/popout_clock.go`
- Create: `internal/shell/popout_session.go`
- Test: `internal/shell/popout_clock_test.go`
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

#### 10b: session/power

**Step 1: Failing tests**

```go
func TestSessionActionsList(t *testing.T) {
	h := newSessionHost(configWithLocker("swaylock"))
	names := focusableNames(h.tree)
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

- Serve: `os.MkdirAll(dir, 0o700)`; bind; on `EADDRINUSE` probe-connect — success means a live
  shell → return single-instance error; failure means stale file → unlink, rebind once.
- Per connection: `bufio.Scanner` lines, `json.Unmarshal` into
  `struct{ ID json.Number; Method string; Params json.RawMessage }`, dispatch, write one line.
  Panel params decode to `{"panel": string}` validated against the known ids
  (`clock|session`; `system-monitor` and `settings` return "not yet available" until
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
- Reload path: the registry closes the transient tooltip, keeps the root chain mapped, rebuilds panel
  content, and invalidates its existing auxiliary unit. Verify this with Task 9's reload test.

**Step 2: Hotkey docs** (`docs/niri-hotkeys.md`)

Documented niri keybinds (user adds to `~/.config/niri/config.kdl`; compositor owns keys, shell
owns panels — DMS pattern):

```kdl
bind {
    Super+P { spawn "sysc-shell" "ipc" "panel.toggle" `{"panel":"clock"}`; }
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
- Confirmation dialogs for destructive session actions → parity knob, not gate material.
- D-Bus (PrepareForSleep, inhibitors) → the first-party lockscreen milestone.
