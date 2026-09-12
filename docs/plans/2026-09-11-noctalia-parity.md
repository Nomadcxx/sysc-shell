# Noctalia v4 parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-base every numeric ladder onto measured Noctalia v4.7.7 constants, add the second radius axis and the `Display` type role, lower the nested-surface contrast floor to 1.30:1, and leave a conformance gate that fails when a surface re-derives its own chrome.

**Architecture:** The mechanism from the superseded theme-system-parity design survives untouched — one resolved `shell.Theme` per surface, sparse configuration, atomic live application. Only the *values* change. The conformance gate lands first, red against an enumerated worklist of 105 sites, and the re-base tasks drive it to green.

**Tech Stack:** Go 1.26.4, no new dependencies.

**Spec:** three designs execute through this one plan —
`docs/plans/2026-09-11-noctalia-parity-design.md` (the ladders),
`docs/plans/2026-09-11-token-conformance-design.md` (Task 1), and
`docs/plans/2026-09-11-component-parity-design.md` (Tasks 5A and 5B). Read all
three before starting; the component-parity design also **corrects** the parity
design's D4, so reading only the latter will give you a padding value that does
not exist in the reference.

## Global Constraints

- **Never run `go test ./...` or any `-race` build.** Run named tests in one package.
- `go test ./internal/shell` is safe to run directly — `runArgvDefault` (`popout_session.go:270`) refuses under `testing.Testing()`.
- **All constants below are logical pixels.** v4's `Style.qml` values are logical pixels and its font sizes are points; points convert at **×4/3**, established by measurement: the reference bar is 62 device px and its capsule exactly 50, each resolving at 2×, and the title em confirms 16 pt at 96 DPI.
- The chrome catalogue owns component construction (its D5). This plan changes token *values*, adds one radius axis and one type role. It does not add, remove or restructure a component tree. Trees change only where a re-based constant changes their measured size.
- These survive from the superseded design and must not regress: one resolved theme per surface; invalid candidates retain the previous theme; open surfaces survive a valid reload; components request semantic roles and never carry RGB; `theme-gen` owns palette generation.
- **The `commit-msg` hook rejects these substrings, case-insensitively:** `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`, `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `gpt-[0-9]`, `llm`, `ai assistant`, `bot`, `agent`. Ordinary words trip it — `both` contains `bot`. Screen every message:
  ```bash
  grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
  ```
  No `Co-Authored-By` trailer. Never `--no-verify`.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/shell/surfacerole_test.go:212` | The conformance scan gains a literal rule |
| `internal/theme/profile.go` | `SpacingScale`, `typeRoles`, `metrics`, `BaseMotion`, the input radius axis |
| `internal/render/style.go:145,192` | `textRoleCount`, the input radius on `Shapes` |
| `internal/shell/theme.go:199` | `resolveShapes` derives two ladders |
| `internal/theme/palettes.go:236` | The outline floor split |
| `internal/shell/theme_test.go:252,255` | The two 1.45 assertions become 1.30 |
| `internal/shell/*.go` | The 105 literal sites |
| `docs/plans/README.md` | Supersession already recorded; plan row added here |

---

### Task 1: The conformance gate, red on purpose

Lands first. 96 of the 105 literal sites are `Gap` or `Padding` — exactly the ladder Task 2 re-bases underneath them — so a literal that looks right today becomes silently wrong the moment the re-base lands. The gate is committed **failing**, against a named worklist, and Tasks 2 to 9 drive it to green.

**Files:**
- Modify: `internal/shell/surfacerole_test.go:212` (`TestSurfaceSourcesCarryNoLegacyVisuals`)

**Interfaces:**
- Consumes: nothing.
- Produces: a failing test naming every literal geometry site.

- [ ] **Step 1: Add the rule to the existing scan**

Do not create a new test file. The house already scans package sources here, and its comment at `:198` states why — a runtime walk "only reaches the trees a test happens to populate… the surfaces most likely to keep a legacy visual are the ones a walk misses."

In `TestSurfaceSourcesCarryNoLegacyVisuals`, add a third pattern beside `aliases` and `bold`:

```go
	// The thirteen geometry fields of ui.Node. The semantic fields --
	// TextRole, Fill, Tone, Shape, Kind -- cannot be written as a number and
	// need no rule.
	literals := regexp.MustCompile(`\b(Width|Height|IconSize|Radius|ItemHeight|ContentH|MaxWidth|ImageSize|ImageW|ImageH|Stroke|Padding|Gap):\s*[0-9]`)
```

The loop currently discards each line's comment. Keep it, so an exemption can be read at the site:

```go
		for i, line := range strings.Split(string(src), "\n") {
			code, comment, _ := strings.Cut(line, "//")
			if strings.Contains(comment, "token-exempt:") {
				continue
			}
```

Then, beside the existing checks:

```go
			if m := literals.FindString(code); m != "" && !strings.Contains(code, ": 0") {
				t.Errorf("%s:%d hardcodes %s; read the spacing ladder for Gap and Padding, "+
					"the density row for Height and control sizes, the icon scale for IconSize, "+
					"or the shape role for Radius", name, i+1, m)
			}
```

`Gap: 0` and `Padding: 0` mean "none", which is a composition choice rather than a measurement — no spacing ladder has a rung meaning absence.

- [ ] **Step 2: Run it and capture the worklist**

Run: `go test ./internal/shell -run TestSurfaceSourcesCarryNoLegacyVisuals 2>&1 | tee /tmp/literals.txt`
Expected: FAIL, roughly 105 lines. Expected distribution: `Gap` 58, `Padding` 38, `Height` 17, `Width` 13, `MaxWidth` 5, `IconSize` 4, `ContentH` 2, `ItemHeight` 1, `ImageSize` 1, concentrated in `popout_audio.go` (24), `popout_settings.go` (13), `popout_process.go` (11).

A materially different count means the tree moved since 2026-09-11 — re-read before continuing.

- [ ] **Step 3: Test the gate itself**

```go
func TestLiteralRuleDetectsAndExempts(t *testing.T) {
	t.Parallel()
	literals := regexp.MustCompile(`\b(Width|Height|IconSize|Radius|ItemHeight|ContentH|MaxWidth|ImageSize|ImageW|ImageH|Stroke|Padding|Gap):\s*[0-9]`)
	for _, s := range []string{"Gap: 12", "Padding: 4", "IconSize: 20", "Radius: 8"} {
		if !literals.MatchString(s) {
			t.Errorf("%q was not detected", s)
		}
	}
	// ui.Rect{W:, H:} is not node geometry. Panel surface sizes are measured
	// dimensions the superseded design explicitly permitted.
	for _, s := range []string{"ui.Rect{W: 420, H: 360}", "Gap: 0", "Padding: 0"} {
		if literals.MatchString(s) && !strings.Contains(s, ": 0") {
			t.Errorf("%q was wrongly detected", s)
		}
	}
}
```

- [ ] **Step 4: Commit the failing gate**

```bash
git add internal/shell/surfacerole_test.go
git commit -m "test(shell): fail on hardcoded node geometry"
```

The package is red from here until Task 9. That is deliberate: the gate is committed against a known list and closed by the work that follows, rather than committed green and broken later.

---

### Task 2: Re-base the spacing ladder

**Files:**
- Modify: `internal/theme/profile.go` (`SpacingScale`)
- Test: `internal/theme/profile_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestSpacingLadderMatchesTheReference(t *testing.T) {
	t.Parallel()
	// v4 Commons/Style.qml: marginXXXS..marginXL, logical px at scale 1.
	want := []int{1, 2, 4, 6, 9, 13, 18}
	if len(SpacingScale) != len(want) {
		t.Fatalf("ladder has %d rungs, want %d", len(SpacingScale), len(want))
	}
	for i, v := range want {
		if SpacingScale[i] != v {
			t.Errorf("rung %d = %d, want %d", i, SpacingScale[i], v)
		}
	}
}
```

- [ ] **Step 2: Run, change, run**

```go
var SpacingScale = []int{1, 2, 4, 6, 9, 13, 18}
```

The rungs are not a rescaling of the old `{2,4,8,12,16,24}`: the reference ladder is denser in the middle, which is what produces its tighter grouping inside cards.

Run: `go test ./internal/theme -run TestSpacing -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/theme/profile.go internal/theme/profile_test.go
git commit -m "feat(theme): re-base the spacing ladder on the reference constants"
```

---

### Task 3: A second radius ladder for inputs

**Files:**
- Modify: `internal/theme/profile.go` (`Composition`)
- Modify: `internal/render/style.go` (`Shapes`)
- Modify: `internal/shell/theme.go:199` (`resolveShapes`)
- Test: `internal/shell/theme_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestInputRadiusIsIndependentOfContainerRadius(t *testing.T) {
	t.Parallel()
	// The reference carries container radii scaled by radiusRatio and a
	// parallel input ladder scaled by iRadiusRatio. One axis cannot express
	// rounded cards with square-ish inputs, which is a composition it ships.
	s := resolveShapes(16, 4)
	if s.Card != 16 {
		t.Errorf("card radius = %d, want 16", s.Card)
	}
	if s.Input != 4 {
		t.Errorf("input radius = %d, want 4", s.Input)
	}
}

func TestStadiumAndCircleStayGeometricAtZeroRadius(t *testing.T) {
	t.Parallel()
	// Carried from the superseded D8: a pill stays a pill at radius zero.
	s := resolveShapes(0, 0)
	if s.For(ui.ShapeStadium, 0) != render.ShapeHalf {
		t.Error("stadium stopped being a proportion at radius zero")
	}
}
```

- [ ] **Step 2: Run, implement, run**

Add `InputRadius int` to `Composition` beside `Radius`, bounded 0–32 like its sibling. Add `Input int` to `render.Shapes`. Change `resolveShapes(radius int)` to `resolveShapes(radius, inputRadius int)` and update its one caller at `theme.go:174`.

Run: `go test ./internal/shell -run 'TestInputRadius|TestStadium' -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/theme/profile.go internal/render/style.go internal/shell/theme.go internal/shell/theme_test.go
git commit -m "feat(theme): add an independent input radius axis"
```

---

### Task 4: Re-base type and add the Display role

**Read this before editing.** `textRoleCount` is *derived*: `const textRoleCount = int(theme.RoleMono) + 1` at `internal/render/style.go:192`, and `TypeSet.Roles` is `[textRoleCount]TextSpec` at `:145`. Appending a role after `RoleMono` **without re-deriving that constant** leaves the array one slot short, and `set.Roles[RoleDisplay]` panics at runtime while the guard test at `style_test.go:42` still passes. Re-derive the constant from the new last role and update that guard.

Do **not** insert before `RoleBody`: it is the iota zero value so an unset node measures as body text.

**Files:**
- Modify: `internal/theme/profile.go` (`TextRole` const block, `typeRoles`, `String`)
- Modify: `internal/render/style.go:192` (`textRoleCount`)
- Modify: `internal/render/style_test.go:42` (the guard)
- Test: `internal/theme/profile_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestTypeLadderMatchesTheMeasuredReference(t *testing.T) {
	t.Parallel()
	// Points convert at 4/3. Sizes are logical px, rounded at paint.
	for _, tc := range []struct {
		role TextRole
		size int
	}{
		{RoleCaption, 12},  // 9 pt
		{RoleBody, 15},     // 11 pt -> 14.67
		{RoleLabel, 15},    // 11 pt
		{RoleTitle, 17},    // 13 pt -> 17.33
		{RoleHeadline, 21}, // 16 pt -> 21.33
		{RoleDisplay, 24},  // 18 pt
		{RoleMono, 13},     // 10 pt -> 13.33
	} {
		if got := TypeFor(tc.role).Size; got != tc.size {
			t.Errorf("%v size = %d, want %d", tc.role, got, tc.size)
		}
	}
}

func TestDisplayRoleIsAddressable(t *testing.T) {
	t.Parallel()
	// The role table is a fixed-size array derived from the last role. If the
	// constant was not re-derived, this indexes out of range.
	var set render.TypeSet
	set.Roles[theme.RoleDisplay] = render.TextSpec{Size: 24}
	if set.Roles[theme.RoleDisplay].Size != 24 {
		t.Error("the display role did not round-trip through the table")
	}
}
```

- [ ] **Step 2: Run and watch them fail**

Run: `go test ./internal/theme -run TestTypeLadder -v`
Expected: FAIL — `undefined: RoleDisplay`.

- [ ] **Step 3: Add the role and re-base the table**

Append after `RoleMono` in the `TextRole` const block, then:

```go
var typeRoles = map[TextRole]TypeSpec{
	RoleCaption:  {Size: 12, Weight: 400},
	RoleLabel:    {Size: 15, Weight: 500},
	RoleBody:     {Size: 15, Weight: 400},
	RoleTitle:    {Size: 17, Weight: 600},
	RoleHeadline: {Size: 21, Weight: 600},
	RoleDisplay:  {Size: 24, Weight: 600},
	RoleMono:     {Size: 13, Weight: 400, Mono: true},
}
```

Then in `internal/render/style.go:192`:

```go
// textRoleCount bounds the role table. It tracks the last role in the enum;
// adding one past it without changing this line leaves the array short and
// indexes out of range at paint.
const textRoleCount = int(theme.RoleDisplay) + 1
```

And update `style_test.go:42` to assert against `theme.RoleDisplay`.

Add `RoleDisplay` to `TextRole.String()`.

- [ ] **Step 4: Run and watch them pass**

Run: `go test ./internal/theme -run TestType -v && go test ./internal/render -run TestTypeSet -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/theme/profile.go internal/render/style.go internal/render/style_test.go internal/theme/profile_test.go
git commit -m "feat(theme): re-base the type ladder and add a display role"
```

---

### Task 5: Five density rows with odd-forced heights

**Files:**
- Modify: `internal/theme/profile.go` (`Density` consts, `metrics`, `Densities`)
- Test: `internal/theme/profile_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestBarHeightsAreOddAtEveryDensity(t *testing.T) {
	t.Parallel()
	// An odd band has a true centre row, so a centred glyph lands on a pixel
	// instead of straddling two.
	for _, d := range Densities() {
		m, ok := MetricsFor(d)
		if !ok {
			t.Fatalf("no row for %v", d)
		}
		if m.BarHeight%2 == 0 {
			t.Errorf("%v bar height %d is even", d, m.BarHeight)
		}
		if m.CapsuleHeight >= m.BarHeight {
			t.Errorf("%v capsule %d is not smaller than the bar %d", d, m.CapsuleHeight, m.BarHeight)
		}
		if m.CapsuleHeight%2 == 0 {
			t.Errorf("%v capsule height %d is even", d, m.CapsuleHeight)
		}
	}
}

func TestDensityRowsMatchTheReference(t *testing.T) {
	t.Parallel()
	want := map[Density]int{
		DensityMini: 21, DensityCompact: 25, DensityDefault: 31,
		DensityComfortable: 37, DensitySpacious: 47,
	}
	for d, h := range want {
		m, _ := MetricsFor(d)
		if m.BarHeight != h {
			t.Errorf("%v bar height = %d, want %d", d, m.BarHeight, h)
		}
	}
}
```

- [ ] **Step 2: Run, implement, run**

Add `DensityMini` and `DensitySpacious`, rename `DensityStandard` to `DensityDefault` keeping a wire alias so existing configuration loads, and add a `CapsuleHeight` to `Metrics` computed as `toOdd(round(BarHeight * r))` with r of 0.90, 0.85, 0.82, 0.75, 0.65.

**Corrected 2026-09-11.** This step originally said padding becomes 14 at every
density. That number came from **v5**, not v4 — `cardPadding` and `panelPadding`
do not exist in v4's `Style.qml` at all.

Padding is a margin-ladder rung chosen per surface: `marginM` (9) inside a card,
`marginL` (13) for a panel's outer inset, `marginS` (6) for a dense card. So
`Metrics.CardPadding` resolves to `marginM` and `PanelPadding` to `marginL`,
rather than to a literal. Density still does not vary them.

Control sizes are **not** tabulated here either — they derive from a base widget
dimension of 33. Both belong to `2026-09-11-component-parity-design.md`; do that
design's work before assuming this task covers the controls.

Add `toOdd(n int) int { return n/2*2 + 1 }` beside the table.

Run: `go test ./internal/theme -run TestDensity -v && go test ./internal/theme -run TestBarHeights -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/theme/
git commit -m "feat(theme): re-base density on five odd-height rows"
```

---

### Task 5A: One master control dimension

Implements `2026-09-11-component-parity-design.md` D1 and D2. That design has no
plan of its own; it executes here, because it changes `Metrics` and control
sizing — the same files this plan already touches.

**Files:**
- Modify: `internal/theme/profile.go` (`Metrics`, `metrics`)
- Test: `internal/theme/profile_test.go`

**Interfaces:**
- Produces: `Metrics.BaseWidget int`, `theme.ToOdd(int) int`, `theme.ToEven(int) int`, and control sizes derived rather than tabulated.

- [ ] **Step 1: Write the failing tests**

```go
func TestControlSizesDeriveFromTheBaseWidget(t *testing.T) {
	t.Parallel()
	// The reference has one master control dimension and expresses every
	// control as a ratio of it. A table of absolutes cannot stay in
	// proportion when the base moves, which is the whole reason to derive.
	m, _ := MetricsFor(DensityDefault)
	if m.BaseWidget != 33 {
		t.Fatalf("base widget = %d, want 33", m.BaseWidget)
	}
	for _, tc := range []struct {
		name  string
		got   int
		ratio float64
		odd   bool
	}{
		{"icon button", m.IconButton, 1.0, true},
		{"checkbox", m.Checkbox, 0.7, true},
		{"toggle base", m.ToggleBase, 0.8, false},
		{"slider knob", m.SliderKnob, 0.7, false},
		{"input height", m.InputHeight, 1.1, false},
		{"tab height", m.TabHeight, 1.0, false},
	} {
		want := int(float64(m.BaseWidget)*tc.ratio + 0.5)
		if tc.odd {
			want = ToOdd(want)
		} else {
			want = ToEven(want)
		}
		if tc.got != want {
			t.Errorf("%s = %d, want %d (base %d × %.2f)", tc.name, tc.got, want, m.BaseWidget, tc.ratio)
		}
	}
}

func TestOddAndEvenForcingIsPerShape(t *testing.T) {
	t.Parallel()
	// Icon buttons and checkboxes force odd so a centred glyph lands on a
	// pixel row. Toggles and sliders force even so the two-sided inset stays
	// symmetric. This is deliberate in the reference, not incidental.
	m, _ := MetricsFor(DensityDefault)
	for name, v := range map[string]int{"icon button": m.IconButton, "checkbox": m.Checkbox} {
		if v%2 == 0 {
			t.Errorf("%s = %d, want odd", name, v)
		}
	}
	for name, v := range map[string]int{"toggle base": m.ToggleBase, "slider knob": m.SliderKnob} {
		if v%2 != 0 {
			t.Errorf("%s = %d, want even", name, v)
		}
	}
}

func TestControlsStayInProportionWhenTheBaseMoves(t *testing.T) {
	t.Parallel()
	// The property that a table of absolutes cannot hold.
	small, _ := MetricsFor(DensityCompact)
	large, _ := MetricsFor(DensitySpacious)
	if !(small.IconButton < large.IconButton && small.InputHeight < large.InputHeight) {
		t.Error("control sizes did not track the base width across densities")
	}
}
```

- [ ] **Step 2: Run and watch them fail**

Run: `go test ./internal/theme -run 'TestControlSizes|TestOddAndEven' -v`
Expected: FAIL — `m.BaseWidget undefined`.

- [ ] **Step 3: Add the base and derive**

```go
// ToOdd and ToEven pin a dimension to a parity. An odd box has a true centre
// row, so a centred glyph lands on a pixel; an even box gives a symmetric
// two-sided inset. The reference chooses per shape, and so do we.
func ToOdd(n int) int  { return n/2*2 + 1 }
func ToEven(n int) int { return n / 2 * 2 }
```

Add `BaseWidget` to `Metrics` and derive the control fields from it in the
`metrics` table rather than writing absolutes. Ratios, from the component design
D1: icon button 1.0 odd, checkbox 0.7 odd, toggle base 0.8 even, slider knob 0.7
even, input and combo height 1.1, tab height 1.0, radio 0.625.

Keep `CompactControl` and `StandardControl` as derived aliases while call sites
migrate, and delete them once nothing reads them.

- [ ] **Step 4: Run and watch them pass**

Run: `go test ./internal/theme -run 'TestControl|TestOddAndEven' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/theme/
git commit -m "feat(theme): derive control sizes from one base dimension"
```

---

### Task 5B: Padding resolves to ladder rungs

Implements component-parity D3. Padding is not a constant in the reference; it is
a rung chosen per surface.

**Files:**
- Modify: `internal/theme/profile.go` (`Metrics`)
- Test: `internal/theme/profile_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPaddingResolvesToLadderRungs(t *testing.T) {
	t.Parallel()
	// Sourced from nine cards and seven panels: every panel insets at
	// marginL with a margin2L height reserve; the inter-card gap is marginM
	// everywhere except the control centre; card interiors are marginM.
	m, _ := MetricsFor(DensityDefault)
	rung := func(v int) bool {
		for _, s := range SpacingScale {
			if s == v {
				return true
			}
		}
		return false
	}
	for name, v := range map[string]int{
		"panel padding": m.PanelPadding,
		"card padding":  m.CardPadding,
		"card gap":      m.CardGap,
	} {
		if !rung(v) {
			t.Errorf("%s = %d, which is not a rung of %v", name, v, SpacingScale)
		}
	}
	if m.PanelPadding != 13 {
		t.Errorf("panel padding = %d, want marginL 13", m.PanelPadding)
	}
	if m.CardPadding != 9 || m.CardGap != 9 {
		t.Errorf("card padding/gap = %d/%d, want marginM 9 each", m.CardPadding, m.CardGap)
	}
}
```

- [ ] **Step 2: Run, implement, run**

`PanelPadding` becomes `marginL` (13), `CardPadding` and a new `CardGap` become
`marginM` (9), at every density — the reference does not vary them by density.

A surface needing a different rhythm names its own rung, which the conformance
gate permits because a rung is not a literal.

Run: `go test ./internal/theme -run TestPadding -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/theme/
git commit -m "feat(theme): resolve padding to spacing ladder rungs"
```

---

### Task 6: Re-base motion

**Files:**
- Modify: `internal/theme/profile.go` (`BaseMotion`)
- Test: `internal/theme/profile_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestMotionDurationsMatchTheReference(t *testing.T) {
	t.Parallel()
	// The reference runs calmer, most visibly at the long end: 750 ms against
	// our 400.
	want := MotionTokens{
		Instant: 0, Shorter: 75 * time.Millisecond, Short: 150 * time.Millisecond,
		Medium: 300 * time.Millisecond, Long: 450 * time.Millisecond,
		ExtraLong: 750 * time.Millisecond,
	}
	if BaseMotion.Shorter != want.Shorter || BaseMotion.ExtraLong != want.ExtraLong {
		t.Errorf("motion = %+v, want %+v", BaseMotion, want)
	}
}

func TestFrameCapStaysBelowTheShortestToken(t *testing.T) {
	t.Parallel()
	// From the smoothness design: a cap above the shortest duration makes a
	// short transition visibly steppy.
	if BaseMotion.FrameCap >= BaseMotion.Shorter {
		t.Errorf("frame cap %v is not below the shortest token %v", BaseMotion.FrameCap, BaseMotion.Shorter)
	}
}
```

The second test only applies once the smoothness plan's Task 3 has landed. If `FrameCap` does not exist yet, omit it and add it when that slice merges.

- [ ] **Step 2: Run, change, run**

Run: `go test ./internal/theme -run TestMotion -v`
Expected: PASS.

Then re-base the chrome catalogue's component recipes onto these tokens rather than discarding them: press in/out and hover to `Shorter`/`Short`, segmented selection to `Medium`, panel enter/exit to `Medium`/`Short`. Curves are unchanged, and reduced-motion behaviour is unchanged.

- [ ] **Step 3: Commit**

```bash
git add internal/theme/ internal/shell/animation.go
git commit -m "feat(theme): re-base motion durations and component recipes"
```

---

### Task 7: Lower the nested-surface floor, split the outline

**The 1.45 floor is not a constant.** `derive()` (`palettes.go:236`) carries only `text := 4.5` and `nonText := 3.0`; nested separation emerges from the ladder steps `step(0.45)`, `step(0.72)`, `step(1.0)` between `surface` and `ladderTop(...)`. The floor is asserted in exactly two places: `internal/shell/theme_test.go:252` and `:255`.

**Files:**
- Modify: `internal/shell/theme_test.go:252,255`
- Modify: `internal/theme/palettes.go:236` (outline only)
- Test: `internal/theme/palettes_test.go`

- [ ] **Step 1: Change the two assertions**

```go
	if got := contrast(th.Background, th.Capsule); got < 1.30 {
		t.Errorf("capsule/bar contrast = %.3f:1, want at least 1.30 so cards read as pills", got)
	}
	if got := contrast(th.Surface, th.SurfaceContainerHigh); got < 1.30 {
		t.Errorf("card/panel contrast = %.3f:1, want at least 1.30", got)
	}
```

1.30 is the measured reference value and is clearly above the 1.17:1 that `sysc-104` and `sysc-110` were filed at. This is not a return to that defect.

- [ ] **Step 2: Write the outline test**

```go
func TestOutlineFloorsSplitByFunction(t *testing.T) {
	t.Parallel()
	// WCAG 2.1 SC 1.4.11 covers user-interface components and focus
	// indication, so Outline keeps 3:1. A decorative divider carries no state
	// and loses no information if it is quieter.
	tk := FallbackFor(false)
	surface := mustColor(tk.Surface)
	if ContrastRatio(mustColor(tk.Outline), surface) < 3.0 {
		t.Error("Outline dropped below 3:1; focus rings must stay at 3:1")
	}
	if ContrastRatio(mustColor(tk.OutlineVariant), surface) >= 3.0 {
		t.Log("OutlineVariant happens to clear 3:1; that is allowed, not required")
	}
}

func TestTextFloorsAreUnchanged(t *testing.T) {
	t.Parallel()
	// The conflict was only ever about surface separation. Measured against
	// the reference palette, text passes with wide margin: body on panel
	// 11.34:1, body on card 8.69:1, on-primary 9.23:1.
	tk := FallbackFor(false)
	if ContrastRatio(mustColor(tk.OnSurface), mustColor(tk.Surface)) < 4.5 {
		t.Error("body text dropped below 4.5:1")
	}
}
```

- [ ] **Step 3: Run everything that touches the palette**

Run: `go test ./internal/theme && go test ./internal/shell -run 'TestTheme|TestSurface' -v`
Expected: PASS. A generated palette that now fails at 1.30 would be a real finding — record it rather than tuning the test.

High contrast is exempt from this relaxation and still forces full opacity and structural outlines.

- [ ] **Step 4: Commit**

```bash
git add internal/shell/theme_test.go internal/theme/
git commit -m "feat(theme): lower the nested surface floor to the measured value"
```

---

### Task 8: Map the reference colour roles

**Files:**
- Modify: `internal/theme/theme.go` (documentation only)
- Test: `internal/theme/theme_test.go`

- [ ] **Step 1: Write the test**

```go
func TestEveryRoleStillExportsAfterTheMapping(t *testing.T) {
	t.Parallel()
	// The reference ships 16 roles; this tree carries 49. Parity maps, it
	// never deletes: the template catalogue needs the complete set, and a
	// consumer keeps the same names whether or not any chrome paints them.
	tk := FallbackFor(false)
	if !tk.Complete() {
		t.Fatal("the fallback no longer defines every role")
	}
	if len(roles) != 49 {
		t.Errorf("role count = %d, want 49; parity must not remove roles", len(roles))
	}
}
```

- [ ] **Step 2: Record the mapping**

The one that matters: the reference fills **both** bar capsules and panel cards from its surface-variant role, which maps to our `SurfaceContainerHigh` — *not* to our separate `SurfaceVariant` field, which keeps its own meaning.

This independently confirms the 2026-09-03 amendment in `b399944`: capsules and cards belong on one level. That was decided here against a live bar, and the reference arrived at the same composition. Measured — its clock capsule samples `#313244`, exactly the surface-variant token.

Its hover role is **not** adopted. State layers composite the paired foreground at 8/12/12/16 percent (chrome D7), which is palette-independent; an explicit hover colour would reintroduce a fixed RGB every generated palette would have to satisfy.

- [ ] **Step 3: Commit**

```bash
git add internal/theme/
git commit -m "docs(theme): record the reference role mapping"
```

---

### Task 9: Drive the gate to green

The 105 sites. Work file by file, largest first, committing per file so a regression is attributable.

**Files:**
- Modify: `internal/shell/popout_audio.go` (24), `popout_settings.go` (13), `popout_process.go` (11), `popout_plugins.go` (8), `popout_launcher.go` (7), `notifycard.go` (7), then the remainder.

- [ ] **Step 1: For each file, replace literals with tokens**

- `Gap` and `Padding` → a rung of `theme.SpacingScale`
- `Height` and control sizes → the density row from `theme.MetricsFor`
- `IconSize` → the icon scale
- `Radius` → a shape role

Where a value is genuinely measured and belongs to no ladder, exempt it **at the site with a reason**:

```go
	Width: 168, // token-exempt: the wordmark's intrinsic aspect, not a ladder value
```

A bare marker with no reason fails the same as an unmarked literal.

- [ ] **Step 2: After each file, run the gate and the package**

Run: `go test ./internal/shell -run TestSurfaceSourcesCarryNoLegacyVisuals` then `go test ./internal/shell`
Expected: the error list shrinks by that file's count; no other test regresses.

- [ ] **Step 3: Commit per file**

```bash
git add internal/shell/popout_audio.go
git commit -m "refactor(shell): read audio panel geometry from tokens"
```

- [ ] **Step 4: Confirm the gate is green**

Run: `go test ./internal/shell -run TestSurface -v`
Expected: PASS, with an exemption census short enough to read in one screen:

```bash
grep -rn "token-exempt:" internal/shell/*.go | wc -l
```

---

### Task 10: Widen the behavioural checks

**Files:**
- Modify: `internal/shell/surfacerole_test.go:90,126,184`

- [ ] **Step 1: Iterate every panel**

`TestSurfaceCardPaddingFollowsDensity`, `TestSurfaceCardTitlesAreTitleRole` and `TestSurfaceHeadingsCarryARole` each iterate only `PanelMonitor` and `PanelSession`. Widen them to every `PanelID` that opens without external state.

A panel that builds no cards must not silently pass, and must not fail either: where `cardsOf` returns empty, record the panel as card-less rather than calling `t.Fatalf`. The current `t.Fatalf("no cards found")` would turn a widened loop red for panels that legitimately build none.

- [ ] **Step 2: Run**

Run: `go test ./internal/shell -run TestSurface -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/shell/surfacerole_test.go
git commit -m "test(shell): widen the surface checks past two panels"
```

---

### Task 11: Migration, settings, and the live gate

- [ ] **Step 1: Migration**

Existing configurations load as before, with current values treated as explicit overrides. The ladder re-base changes *defaults*: a user who never set a value moves to the reference number, and one who set a value keeps it.

Density needs care — three names become five. `compact` stays `compact`, `standard` maps to `default`, `comfortable` stays, and `mini` and `spacious` are newly available. A configuration naming a density keeps its name and changes height. That is the intended parity change and must be stated in the release note rather than discovered at runtime.

- [ ] **Step 2: Settings**

Add the input radius row beside the existing radius row, and extend the density list to five.

- [ ] **Step 3: The inherited live gate**

`sysc-142` is closed but its Task 13 live gate never ran, and the superseding design re-inherits it rather than discharging it. Re-basing every ladder makes it more necessary, not less.

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
```

Exercise the ten configurations from the superseded D15, plus a scale-1.0 confirmation that the re-based type and density do not clip the process table or the settings rows. **Type and density both shrink relative to today**, so clipping is the expected failure mode and the densest surfaces are where to look.

This machine has one output at 3440×1440. Record the two-output and fractional-scale cases as unrunnable; do not claim them.

- [ ] **Step 4: Commit**

```bash
git add internal/ docs/ .beads/issues.jsonl
git commit -m "feat(theme): complete the reference parity re-base"
```

---

## Self-Review

**Spec coverage.** Parity D1 spacing → Task 2. D2 two radius ladders → Task 3. D3 type ladder and `Display` → Task 4, with the derived-constant hazard called out. D4 five density rows and `toOdd` → Task 5. D5 motion and re-based chrome recipes → Task 6. D6 role mapping without deletion → Task 8. D7 contrast floor → Task 7, correctly targeting the two test assertions rather than a constant that does not exist. D8 outline split → Task 7 Step 2. D9 opacity floor → deliberately absent; the blur design owns it. D10 borders and shadows → no change, as specified. D11 chrome boundary → no tree restructuring anywhere in this plan. D12 migration → Task 11. D13 testing → every task. D15 risks → Task 11 Step 3 names clipping as the expected failure. Conformance D1–D4 → Tasks 1, 9 and 10. Conformance D5 sequencing → Task 1 is first and red by design.

**Component parity coverage** (added 2026-09-12, when Tasks 5A and 5B were
inserted). D1 one master control dimension with per-control ratios → Task 5A,
asserted three ways: the ratios themselves, the per-shape parity, and that
controls stay in proportion when the base moves — which is the property a table
of absolutes cannot hold. D2 odd and even forced per shape → Task 5A Step 1's
second test. D3 padding as ladder rungs → Task 5B, which also **supersedes Task
5's original padding sentence**; that sentence now carries its own correction
notice. D4 hero type as an inline multiplier → **not implemented here**: Task 4
adds the `Display` role, which is a real rung, but the multiplier pattern has no
consumer in this plan and would be a speculative field. D5 the inverted hero card
needs no primitive → nothing to do, recorded so a later reader does not go
looking for the task. D6 stacking's consumer → owned by the stacking design, not
this plan. D7 testing → Tasks 5A and 5B. D8 residual risks → Task 11 Step 3's
live gate is where panel-dimension divergence and settings composition will
actually be seen; neither is reconciled by this plan, deliberately.

**Placeholders.** None. Task 9 is repetitive by nature but states the mapping rule, the exemption idiom and the per-file verification rather than "fix the literals".

**Type consistency.** `resolveShapes(radius, inputRadius int)` is introduced in Task 3 and used in its test and at `theme.go:174`. `Shapes.Input` is added in Task 3 and read in Task 3's test. `RoleDisplay` is added in Task 4 and referenced in Task 4's tests and `textRoleCount`. `Metrics.CapsuleHeight` is added in Task 5 and asserted in Task 5's test. `MotionTokens.FrameCap` is referenced in Task 6 with an explicit note that it arrives from the smoothness plan.
