# Density Configuration Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Preserve `48/6/4` bar geometry for selector-free existing configurations while keeping fresh defaults and the standard preset on the current 31 px density.

**Architecture:** `internal/theme` resolves the hidden `standard` wire density as the current `default` metrics with only the legacy bar height, padding, and spacing restored. `internal/config` classifies selector-free parsed documents as legacy and writes an explicit, semantically true preset so current sparse documents retain their generation across reload. The shell and Wayland layers continue consuming a fully resolved `config.Config` without migration branches.

**Tech Stack:** Go standard library, existing config/theme packages, table-driven Go tests.

---

### Task 1: Restore the hidden bar-geometry compatibility row

**Files:**

- Modify: `internal/theme/profile_test.go:486-510`
- Modify: `internal/theme/profile.go:17-31,224-235`

**Step 1: Change the legacy-row test to state the compatibility contract**

Replace the equality assertion in `TestLegacyDensityNameStillResolves` with a
check for legacy bar geometry and equality of every non-bar field:

```go
legacy, ok := MetricsFor(DensityStandard)
if !ok {
	t.Fatal("the legacy density name no longer resolves; existing files would be rejected")
}
current, _ := MetricsFor(DensityDefault)
if legacy.BarHeight != 48 || legacy.BarPadding != 6 || legacy.BarSpacing != 4 {
	t.Errorf("legacy bar = %d/%d/%d, want 48/6/4",
		legacy.BarHeight, legacy.BarPadding, legacy.BarSpacing)
}
legacy.BarHeight = current.BarHeight
legacy.BarPadding = current.BarPadding
legacy.BarSpacing = current.BarSpacing
if legacy != current {
	t.Errorf("legacy non-bar metrics = %+v, want default %+v", legacy, current)
}
```

Keep the existing assertion that `Densities()` does not list
`DensityStandard`.

**Step 2: Run the focused test and verify it fails**

Run: `go test ./internal/theme -run TestLegacyDensityNameStillResolves -count=1`

Expected: FAIL because `standard` still reports `31/2/4`.

**Step 3: Implement the compatibility projection in `MetricsFor`**

Replace the alias fold with a copy of the current row and three overrides:

```go
func MetricsFor(d Density) (Metrics, bool) {
	if d == DensityStandard {
		m := metrics[DensityDefault]
		m.BarHeight = 48
		m.BarPadding = 6
		m.BarSpacing = 4
		return m, true
	}
	m, ok := metrics[d]
	return m, ok
}
```

Update the nearby comments to say that `standard` is a hidden compatibility
value preserving only pre-rebase bar geometry. Do not add it to `metrics` or
`Densities()`; deriving it from `default` prevents the control metrics from
drifting.

**Step 4: Run the theme package tests**

Run: `go test ./internal/theme -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/theme/profile.go internal/theme/profile_test.go
git commit -m "fix(theme): restore legacy bar geometry"
```

### Task 2: Distinguish old sparse documents from current defaults

**Files:**

- Modify: `internal/config/config_test.go:230-260,1038-1083`
- Modify: `internal/config/write_test.go:237-305`
- Modify: `internal/config/load.go:223-275`
- Modify: `internal/config/write.go:17-24,74-88,412-435`

**Step 1: Add the parsing migration matrix**

Add a table test beside the existing missing-file and theme migration tests:

```go
func TestThemeDensityGenerationMigration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		doc        string
		want       theme.Density
		height     int
		padding    int
		spacing    int
	}{
		{"selector-free document", `{}`, theme.DensityStandard, 48, 6, 4},
		{"empty theme block", `{"theme":{}}`, theme.DensityStandard, 48, 6, 4},
		{"current standard preset", `{"theme":{"preset":"standard"}}`, theme.DensityDefault, 31, 2, 4},
		{"explicit legacy density", `{"theme":{"density":"standard"}}`, theme.DensityStandard, 48, 6, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Theme.Density != tc.want || cfg.Bar.Height != tc.height ||
				cfg.Bar.Padding != tc.padding || cfg.Bar.Spacing != tc.spacing {
				t.Fatalf("density/bar = %q %d/%d/%d, want %q %d/%d/%d",
					cfg.Theme.Density, cfg.Bar.Height, cfg.Bar.Padding, cfg.Bar.Spacing,
					tc.want, tc.height, tc.padding, tc.spacing)
			}
		})
	}
}
```

Keep `TestLoadTreatsAMissingFileAsDefaults` and add an assertion there that a
missing file still resolves to `DensityDefault` and `31/2/4`. Missing files
must bypass the existing-document migration.

**Step 2: Run the parsing tests and verify they fail**

Run: `go test ./internal/config -run 'TestThemeDensityGenerationMigration|TestLoadTreatsAMissingFileAsDefaults' -count=1`

Expected: FAIL for the selector-free cases, which currently resolve to
`DensityDefault` and `31/2/4`.

**Step 3: Seed the compatibility density before applying wire fields**

Immediately after `cfg := Default()` in `Parse`, select the legacy density only
when the parsed document carries no selector:

```go
if wire.Theme == nil || (wire.Theme.Preset == nil && wire.Theme.Density == nil) {
	cfg.Theme.Density = theme.DensityStandard
}
```

Leave the existing application order intact: preset, explicit theme axes,
derived bar, explicit bar, and output overrides. Do not add migration state to
`Config`.

**Step 4: Add writer round-trip tests for both generations**

Replace `TestThemeDefaultWritesNoThemeBlock` with a test asserting that
`toWire(Default())` records only `preset: "standard"`. Extend it through
`Write` and `Load` and assert that the current default remains
`DensityDefault` and `31/2/4`.

Add a second test that parses `{}`, writes it to a temporary file, checks that
the wire theme contains `density: "standard"`, and reloads it at `48/6/4`.

**Step 5: Run the writer tests and verify they fail**

Run: `go test ./internal/config -run 'TestThemeDefaultWrites|TestThemeLegacyDensityRoundTrip' -count=1`

Expected: FAIL because the default writer currently emits no theme selector.

**Step 6: Always write the selected preset while keeping axes sparse**

Change `themeDiff` to record `got.Preset` unconditionally, then compare every
axis against that preset as it does today:

```go
func themeDiff(got Theme) *wireTheme {
	base, ok := theme.PresetComposition(got.Preset)
	if !ok {
		base = standardComposition()
	}
	v := string(got.Preset)
	w := wireTheme{Preset: &v}
	// Existing per-axis comparisons follow unchanged.
	return &w
}
```

Remove the unused `defaultPreset` argument and `set` bookkeeping while
retaining all axis comparisons. Update `toWire` and the `Write` comment: the
document is sparse relative to its selected preset, but records that preset as
generation provenance.

Do not emit explicit bar dimensions. `barDiff` must continue comparing against
the bar derived from the selected theme so preset changes remain unpinned.

**Step 7: Run all configuration tests**

Run: `go test ./internal/config -count=1`

Expected: PASS.

**Step 8: Commit**

```bash
git add internal/config/config_test.go internal/config/write_test.go internal/config/load.go internal/config/write.go
git commit -m "fix(config): preserve density generation"
```

### Task 3: Align the Wayland policy proof and run the migration gate

**Files:**

- Modify: `internal/platform/wayland/policy_test.go:75-90,147-178`

**Step 1: Update stale current-default surface expectations**

Change the two hard-coded default surface heights from 44 to 27. The current
default bar is 31 high with a 4 px outer gap, so `Geometry` produces a 27 px
surface. Keep the explicit `56/6 -> 50` policy case unchanged.

**Step 2: Run the focused package gates**

Run: `go test ./internal/theme ./internal/config ./internal/platform/wayland -count=1`

Expected: PASS.

**Step 3: Run formatting and code-touching invariants**

```bash
gofmt -w internal/theme/profile.go internal/theme/profile_test.go \
  internal/config/config_test.go internal/config/write_test.go \
  internal/config/load.go internal/config/write.go \
  internal/platform/wayland/policy_test.go
test -z "$(gofmt -l .)"
git diff --check
git diff --exit-code -- go.mod go.sum
```

Expected: every command exits zero and `go.mod`/`go.sum` are unchanged.

**Step 4: Commit**

```bash
git add internal/platform/wayland/policy_test.go
git commit -m "test(wayland): follow current bar geometry"
```

**Step 5: Continue the existing integration work**

Return to `docs/plans/2026-09-11-noctalia-parity.md` Task 9 for `sysc-265`, then
merge `feature/bluetooth-panel` according to its committed plan and completion
handover. Resolve `internal/shell/registry.go` by preserving both wallpaper and
Bluetooth shutdown lifecycles. Run the repository-wide gates only after the
combined tree is assembled.

## Amendment: owner clarification, 2026-09-16

The owner confirmed that the standard preset and fresh/default configuration
must retain the legacy `48/6/4` bar. The smaller `DensityDefault` row at
`31/2/4` is an opt-in settings choice for the later settings pass. Therefore
the plan's original “fresh defaults and standard preset on 31 px” wording and
its `31/2/4` expectations for missing files and `preset: "standard"` are
superseded. The executed theme/config tests now assert `DensityStandard` and
`48/6/4` for those cases; no second migration or explicit bar override is
needed.
