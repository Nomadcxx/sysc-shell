# UI Correctness Tranche Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the three owner-approved defects: SVG icons never resolve (`sysc-173`), template
apply can overwrite a file the user edited (`sysc-405`, P1), and a held key stops repeating when
keyboard focus moves (`sysc-171`).

**Architecture:** `internal/theming` records the sha256 of the bytes it last rendered per target
path under the shell's state root; `ApplyWrite` becomes adopt-if-untouched with a temp+rename swap,
refuses with a typed error, and an explicit overwrite backs the file up first. Refusals flow per
template to the settings Templates section. `internal/icons` pins pure-Go `oksvg`+`rasterx`, decodes
SVG on the existing worker goroutine into the existing `ui.Image`, and the resolver gains an
explicit tier order: exact raster > SVG > nearest raster. `internal/platform/wayland` keeps the
repair inside the existing `keyRepeat` state machine: a focus move suspends the repeat and an enter
retargets it, destruction kills it, and a release cancels the deadline even when the focus is gone.

**Tech Stack:** Go 1.26, `github.com/srwiley/oksvg` +
`github.com/srwiley/rasterx` (pinned in Task 3), `crypto/sha256` and `encoding/json` from the
standard library.

**Spec:** `docs/plans/2026-09-27-ui-correctness-tranche-design.md` (D1–D3). Tracked as `sysc-591`;
the defects are `sysc-173`, `sysc-405`, and `sysc-171`. D2 ships first (P1 data loss); D1 and D3 are
independent of each other and of everything else here.

## Global Constraints

- `GOMAXPROCS=4` on every `go` command.
- Tests run per package only: `GOMAXPROCS=4 go test -count=1 ./internal/<pkg>`. NEVER
  `go test -race ./...` — it has hard-locked this machine. Per-package `-race` is fine.
- Pure Go only: no CGO, no handwritten C (AGENTS.md). `oksvg` and `rasterx` are pure Go, so the
  narrow-C-boundary rule stays untriggered.
- The Wayland dispatch loop stays single-goroutine. SVG decode runs on the existing icons worker
  goroutine (`Worker.Run`), never on the owner. The key-repeat repair touches only owner-goroutine
  state. The template overwrite action re-runs the apply through the config reload path, never
  inline in a bar input handler (registry.go:698-701: the owner must never exec).
- `gofmt -w . && test -z "$(gofmt -l .)"` and `GOMAXPROCS=4 go vet ./...` before every commit.
- Commit messages pass `bash ~/.git-hooks/commit-msg <file>`; no attribution trailers; screen the
  wording before committing.
- `git diff --exit-code -- go.mod go.sum` at the final gate. Task 3's pin commit is the only
  intended change to them.
- bd runs from `/home/nomadx/sysc-shell`, never from a worktree. Commit `.beads/issues.jsonl` in
  the same commit as the code it describes.

## Review Focus

1. Losing the state file must degrade to refusing, never to clobbering: a non-empty target with no
   record is refused even when it carries the generated marker. Pinned in Task 1.
2. The state file is process-global state: every test that reaches `ApplyWrite` isolates
   `XDG_STATE_HOME` with `t.Setenv` and drops `t.Parallel`, or parallel tests corrupt one real
   state file. Pinned in Task 1.
3. On the first boot after this lands, every config the old code wrote has no state record and is
   refused; the Templates section shows refusal notes until the operator overwrites or leaves each
   one. That is the designed migration, not a regression. Pinned in Tasks 2 and 8.
4. SVG decode must never run on the Wayland owner goroutine. Pinned in Task 4.
5. A broken SVG falls through to the raster chain once per path, not once per repaint. Pinned in
   Task 4.
6. The repeat must not outlive a destroyed surface: a focus move suspends, destruction stops.
   Pinned in Task 6.
7. A release arriving while the focus is gone must still cancel the pending deadline, or a key
   released mid-move resumes on re-enter. Pinned in Task 6.

---

### Task 1: Adopt-if-untouched template writes

**Files:**
- Modify: `internal/theming/apply.go:22-30` (`ApplyWrite`), new state and swap helpers below it
- Modify: `internal/theming/apply_test.go:10,25,83,157` (isolate `XDG_STATE_HOME`, drop `t.Parallel`)
- Modify: `tests/integration/settings_gate_test.go:50-56` (same isolation)
- Test: `internal/theming/apply_test.go`

**Interfaces:**
- Produces: `var ErrUserModified error`; `ApplyWrite(path, rendered string) error` (same signature,
  new semantics); `ApplyWriteForce(path, rendered string) error`.
- Keeps: `oursFile` and `UnapplyWrite` (apply.go:16-43) unchanged — unapply only deletes, and still
  recognises our files by the first-line marker.

- [ ] **Step 0: Register and commit this plan**

Add a row for `2026-09-27-ui-correctness-tranche.md` to `docs/plans/README.md` (kind: plan,
`sysc-591`), then commit the plan document and the register row together — AGENTS.md requires the
plan to be committed before it is executed.

- [ ] **Step 1: Write the failing tests**

In `internal/theming/apply_test.go`, add (`t.Setenv` cannot run under `t.Parallel`, so the new
tests do not call it). Add `"errors"` to the file's imports.

```go
func TestApplyWriteAdoptsOnlyWhatItRendered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	p := filepath.Join(dir, "foot.ini")
	rendered := "# generated by sysc-shell — do not edit\nok\n"

	if err := ApplyWrite(p, rendered); err != nil {
		t.Fatalf("absent target: %v", err)
	}
	if err := ApplyWrite(p, rendered+"\n"); err != nil {
		t.Fatalf("untouched target: %v", err)
	}
	if err := os.WriteFile(p, []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyWrite(p, rendered); !errors.Is(err, ErrUserModified) {
		t.Fatalf("edited target: %v, want ErrUserModified", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "user edit\n" {
		t.Fatalf("refused target was modified: %q", got)
	}

	orphan := filepath.Join(dir, "kitty.conf")
	if err := os.WriteFile(orphan, []byte(rendered), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyWrite(orphan, rendered); !errors.Is(err, ErrUserModified) {
		t.Fatalf("unrecorded target: %v, want ErrUserModified", err)
	}

	empty := filepath.Join(dir, "empty.conf")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyWrite(empty, rendered); err != nil {
		t.Fatalf("empty target: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".sysc-") {
			t.Fatalf("temp file %s left behind", e.Name())
		}
	}
}

func TestApplyWriteForceBacksUpAndReadopts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	p := filepath.Join(dir, "foot.ini")
	if err := os.WriteFile(p, []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rendered := "# generated by sysc-shell — do not edit\nok\n"
	if err := ApplyWriteForce(p, rendered); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != rendered {
		t.Fatalf("force did not write: %q", got)
	}
	if got, _ := os.ReadFile(p + ".bak"); string(got) != "user edit\n" {
		t.Fatalf("backup = %q", got)
	}
	if err := ApplyWrite(p, rendered+"\n"); err != nil {
		t.Fatalf("a forced write was not adopted: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestApplyWrite' ./internal/theming`
Expected: FAIL to compile, `ErrUserModified` and `ApplyWriteForce` undefined.

- [ ] **Step 3: Implement**

`internal/theming/apply.go`: replace `ApplyWrite` (line 22) and add the helpers below it. Imports
gain `bytes`, `crypto/sha256`, `encoding/hex`, `encoding/json`, `errors`.

```go
// ErrUserModified reports a target whose current bytes the shell cannot
// account for: the user edited it, or the shell has no record of ever writing
// it. The settings Templates surface offers an explicit overwrite.
var ErrUserModified = errors.New("theming: target modified outside the shell")

// ApplyWrite writes rendered content over a target the shell can account for:
// absent, empty, or byte-identical to what it last rendered. Anything else is
// refused rather than clobbered.
func ApplyWrite(path, rendered string) error {
	if current, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(current)) > 0 {
		if stateHash(path) != hash(current) {
			return fmt.Errorf("%w: %s", ErrUserModified, path)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return writeAdopted(path, []byte(rendered))
}

// ApplyWriteForce overwrites regardless of the target's history, backing the
// previous bytes up to <path>.bak first. It is the explicit action behind a
// refusal, never a default.
func ApplyWriteForce(path, rendered string) error {
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		if err := os.Rename(path, path+".bak"); err != nil {
			return err
		}
	}
	return writeAdopted(path, []byte(rendered))
}

func writeAdopted(path string, data []byte) error {
	if err := swapWrite(path, data); err != nil {
		return err
	}
	// The state file is a cache, never an authority: failing to record here
	// degrades the next apply to a refusal, which is the safe direction.
	_ = rememberHash(path, hash(data))
	return nil
}

// swapWrite renders into a sibling temp file and renames it over the target.
// Rename is atomic within a directory, so a reader never sees a half-written
// config and a crash leaves the previous bytes in place.
func swapWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sysc-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// statePath is $XDG_STATE_HOME/sysc-shell/templates/state.json. The design
// says XDG_STATE_HOME/sysc/; every state root in this repository is
// sysc-shell (internal/wallpaper/persist.go:41, internal/plugin/state.go:54),
// so the record follows the convention.
func statePath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "sysc-shell", "templates", "state.json")
}

// loadState reads the rendered-hash record. Any problem -- absent, corrupt,
// unwritable -- is an empty record: losing the state file must degrade to
// refusing, never to clobbering.
func loadState() map[string]string {
	state := map[string]string{}
	if data, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	return state
}

func stateHash(path string) string { return loadState()[path] }

func rememberHash(path, sum string) error {
	root := statePath()
	if root == "" {
		return nil
	}
	state := loadState()
	state[path] = sum
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return err
	}
	return os.WriteFile(root, data, 0o644)
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
```

The marker check inside the old `ApplyWrite` (apply.go:24-26) is gone; `oursFile` stays for
`UnapplyWrite`.

Then isolate the state file in every existing test that reaches `ApplyWrite` — parallel tests would
otherwise write one real state file concurrently. In `internal/theming/apply_test.go` remove
`t.Parallel()` and add `t.Setenv("XDG_STATE_HOME", t.TempDir())` as the first statement of
`TestApplyPlainWriteNeverOverwritesForeignContent` (line 10),
`TestNiriIncludeInjectionIdempotent` (line 25),
`TestDisablingNiriTemplateRemovesIncludeAndFile` (line 83), and
`TestReadOnlyNiriConfigIsReportedNotRewritten` (line 157). The same two changes in
`tests/integration/settings_gate_test.go` `TestAcceptNiriTemplateLiveApply` (line 50).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/theming && GOMAXPROCS=4 go test -count=1 ./tests/integration`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/theming tests && GOMAXPROCS=4 go vet ./internal/theming
git add internal/theming tests/integration/settings_gate_test.go
git commit -m "fix(theming): adopt only targets the shell last rendered"
```

---

### Task 2: Per-template refusals and the backed-up overwrite

**Files:**
- Modify: `internal/theming/enabled.go:20-45` (`ApplyEnabled`), `:76-115` (`applyOnce` outcomes and force)
- Modify: `internal/theming/apply.go:46-64` (`ApplyNiri` gains an unexported force path)
- Modify: `internal/theming/enabled_test.go:17,37,126,145` (new signature, state isolation)
- Modify: `internal/shell/registry.go:69-71` (fields beside `themeErr`), `:213` (literal init), `:842-845` (call site)
- Modify: `internal/shell/popout_settings.go:200-205` (refusal rows), new `templateRefusals`
- Modify: `internal/shell/panelhost.go:2006-2014` (action beside `section:`)
- Test: `internal/theming/enabled_test.go`, `internal/shell/popout_settings_test.go`

**Interfaces:**
- Produces: `ApplyEnabled(home string, enabled func(string) bool, tok theme.Tokens, force func(string) bool) (map[string]error, error)`
  — outcomes keyed by template name; `Registry.templateRefusals map[string]string`;
  action `template-overwrite:<name>`.
- Consumes: Task 1's `ErrUserModified` and `ApplyWriteForce`.

- [ ] **Step 1: Write the failing tests**

In `internal/theming/enabled_test.go`:

```go
func TestApplyEnabledReportsRefusalsPerTemplate(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	target := filepath.Join(home, ".config", "cava", "config")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	only := func(name string) bool { return name == "cava" }
	outcomes, _ := ApplyEnabled(home, only, theme.Fallback, nil)
	if !errors.Is(outcomes["cava"], ErrUserModified) {
		t.Fatalf("outcomes = %v, want a cava refusal", outcomes)
	}

	outcomes, err := ApplyEnabled(home, only, theme.Fallback, only)
	if err != nil {
		t.Fatalf("forced apply: %v", err)
	}
	if len(outcomes) != 0 {
		t.Fatalf("forced outcomes = %v", outcomes)
	}
	if got, _ := os.ReadFile(target); !strings.Contains(string(got), marker) {
		t.Fatalf("forced write = %q", got)
	}
	if _, err := os.Stat(target + ".bak"); err != nil {
		t.Fatal("the refused bytes were not backed up")
	}
}
```

Update the three existing `ApplyEnabled` callers for the new signature and state isolation:
`TestApplyEnabledWritesAlacrittyUnderXDG` (line 17), `TestApplyEnabledSkipsForeignKitty` (line 37),
`TestApplyEnabledReportsFirstError` (line 126), and the goroutine caller in
`TestApplyEnabledSupersedeUsesLatestHome` (line 145): add
`t.Setenv("XDG_STATE_HOME", t.TempDir())` (none of them is `t.Parallel`), take two return values,
and in `TestApplyEnabledReportsFirstError` replace the `"skipped"` substring check with
`errors.Is(err, ErrUserModified)` — the old "skipped: user file" text is gone.

In `internal/shell/popout_settings_test.go`:

```go
func TestSettingsTemplatesSurfaceRefusals(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.section = "Templates"
	r := &Registry{templateRefusals: map[string]string{
		"cava": "theming: target modified outside the shell: /home/u/.config/cava/config",
	}}
	h.root = settingsTree(r, h)
	if !strings.Contains(renderText(h.root), "user-modified") {
		t.Fatal("the refusal note is missing from the Templates section")
	}
	overwrite := findByName(h.root, "Overwrite cava")
	if overwrite == nil || overwrite.Action != "template-overwrite:cava" || !overwrite.Focusable {
		t.Fatalf("overwrite control = %+v", overwrite)
	}

	h.root = settingsTree(nil, h)
	if strings.Contains(renderText(h.root), "user-modified") {
		t.Fatal("a registry with no refusals rendered a refusal note")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestApplyEnabled|TestSettingsTemplates' ./internal/theming ./internal/shell`
Expected: FAIL to compile (`ApplyEnabled` arity, `templateRefusals` undefined).

- [ ] **Step 3: Implement**

`internal/theming/enabled.go` — `ApplyEnabled` gains the force predicate and returns per-template
outcomes; `applyOnce` records per name instead of keeping only the first error:

```go
func ApplyEnabled(home string, enabled func(string) bool, tok theme.Tokens, force func(string) bool) (map[string]error, error) {
	if home == "" || enabled == nil {
		return nil, nil
	}
	job := applyJob{home: home, enabled: enabled, tok: tok, force: force}
	applyMu.Lock()
	if applyBusy {
		applyQueued = &job
		applyMu.Unlock()
		return nil, nil
	}
	applyBusy = true
	applyMu.Unlock()

	var err error
	var outcomes map[string]error
	current := job
	for {
		outcomes, err = applyOnce(current.home, current.enabled, current.tok, current.force)
		applyMu.Lock()
		if applyQueued == nil {
			applyBusy = false
			applyMu.Unlock()
			return outcomes, err
		}
		current = *applyQueued
		applyQueued = nil
		applyMu.Unlock()
	}
}
```

`applyJob` gains a `force func(string) bool` field. In `applyOnce`, replace `keep` with:

```go
	outcomes := map[string]error{}
	var first error
	record := func(name string, err error) {
		if err == nil {
			return
		}
		outcomes[name] = err
		if first == nil {
			first = err
		}
	}
	forceOn := func(name string) bool { return force != nil && force(name) }
```

and route every branch through it: the niri branch calls
`record(name, applyNiri(cfg, gen, rendered, forceOn(name)))`; the gtk branch calls
`record(name, applyWrite(css, rendered, forceOn(name)))` before `ApplyGtkThemeName`; the default
branch calls `record(name, applyWrite(target, rendered, forceOn(name)))` and, on success, the kitty
signal. `applyOnce` returns `(outcomes, first)`.

`internal/theming/apply.go` — `ApplyNiri` keeps its signature and delegates:

```go
func ApplyNiri(configPath, genPath, rendered string) error {
	return applyNiri(configPath, genPath, rendered, false)
}

func applyNiri(configPath, genPath, rendered string, force bool) error {
	if err := applyWrite(genPath, rendered, force); err != nil {
		return err
	}
	// ... the include-injection body of the old ApplyNiri, unchanged ...
}
```

`internal/shell/registry.go` — beside `themeErr` (line 69-71):

```go
	// templateRefusals names templates whose files the shell refused to write
	// because their current bytes are not the shell's last render. The
	// settings Templates section surfaces them with an overwrite action.
	templateRefusals map[string]string
	// templateForce marks templates the user explicitly overrode a refusal
	// for; the next apply that succeeds consumes the entry.
	templateForce map[string]bool
```

In the `NewRegistry` literal (ends line 214), add `templateForce: map[string]bool{},`. At the
`ApplyEnabled` call (line 843):

```go
	if !runningAsTest() {
		outcomes, err := theming.ApplyEnabled(os.Getenv("HOME"), cfg.TemplateEnabled, tok, r.consumeTemplateForce)
		r.templateRefusals = map[string]string{}
		for name, err := range outcomes {
			if errors.Is(err, theming.ErrUserModified) {
				r.templateRefusals[name] = err.Error()
			}
		}
		for name := range r.templateForce {
			if _, refused := r.templateRefusals[name]; !refused {
				delete(r.templateForce, name)
			}
		}
		if err != nil {
			return tok, fmt.Errorf("theme: external templates: %w", err)
		}
	}
```

with the consumer as a method beside `generateTheme` (both run on the owner goroutine):

```go
// consumeTemplateForce reports and clears one template's overwrite request.
func (r *Registry) consumeTemplateForce(name string) bool {
	forced := r.templateForce[name]
	delete(r.templateForce, name)
	return forced
}
```

`internal/shell/popout_settings.go` — after the `Bar` special case (line 201-208):

```go
	if section == "Templates" {
		column.Children = append(column.Children, templateRefusals(r)...)
	}
```

```go
// templateRefusals reports, under the toggle rows, every template whose file
// the shell refused to write because the user edited it, each with the
// explicit overwrite that backs the file up to <path>.bak.
func templateRefusals(r *Registry) []*ui.Node {
	if r == nil || len(r.templateRefusals) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.templateRefusals))
	for name := range r.templateRefusals {
		names = append(names, name)
	}
	sort.Strings(names)
	notes := make([]*ui.Node, 0, len(names))
	for _, name := range names {
		notes = append(notes, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, PinEnd: true,
			Children: []*ui.Node{
				{Kind: ui.KindText, Name: name + " refusal",
					Text: name + " is user-modified; its theme file was not written",
					TextRole: theme.RoleCaption, Tone: ui.ToneError},
				{Kind: ui.KindButton, Text: "Overwrite", Action: "template-overwrite:" + name,
					Name: "Overwrite " + name, Role: "button", Focusable: true},
			}})
	}
	return notes
}
```

`internal/shell/panelhost.go` — in the action prefix chain, beside the `section:` case (line 2006):

```go
	if name, ok := strings.CutPrefix(n.Action, "template-overwrite:"); ok {
		if r.templateForce == nil {
			r.templateForce = map[string]bool{}
		}
		r.templateForce[name] = true
		// A config rewrite is the one path that re-runs every template
		// apply; the force flag is consumed there, off this goroutine.
		if err := r.writeConfig(r.cfg); err != nil {
			h.errLabel = err.Error()
		}
		r.rebuildPanel(h)
		return true
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/theming && GOMAXPROCS=4 go test -count=1 ./internal/shell`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/theming internal/shell && GOMAXPROCS=4 go vet ./internal/theming ./internal/shell
git add internal/theming internal/shell
git commit -m "feat(shell): report user-modified templates and offer overwrite"
```

---

### Task 3: Pin oksvg and rasterx

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `README.md:20` (dependency table row after `sysc-launch`)

**Interfaces:**
- Produces: the pinned modules Task 4 imports. No code changes in this task.

- [ ] **Step 1: Resolve and pin**

Upstream has no tagged releases (`go list -m -versions github.com/srwiley/oksvg` returns none), so
the pins are the proxy's `@latest` pseudo-versions, resolved 2026-09-27:

```bash
GOMAXPROCS=4 go get github.com/srwiley/oksvg@v0.0.0-20221011165216-be6e8873101c
GOMAXPROCS=4 go get github.com/srwiley/rasterx@v0.0.0-20220730225603-2ab79fcdd4ef
GOMAXPROCS=4 go list -m github.com/srwiley/oksvg github.com/srwiley/rasterx
```

Expected: the two pinned versions printed, matching the commands above.

- [ ] **Step 2: Review the module diff**

Run: `git diff go.mod go.sum`
Expected: two new direct `require` entries and four `go.sum` lines. No `replace` directive, no
other drift. AGENTS.md forbids `replace` and makes go.mod/go.sum diffs a commit gate.

- [ ] **Step 3: Add the README pin row**

After the `sysc-launch` row (README.md:20):

```markdown
| [`oksvg`](https://github.com/srwiley/oksvg) + [`rasterx`](https://github.com/srwiley/rasterx) | `v0.0.0-20221011165216-be6e8873101c` / `v0.0.0-20220730225603-2ab79fcdd4ef` | Pure-Go SVG rasterisation for theme icons (design D1). Upstream tags no releases; the pins are the proxy `@latest` pseudo-versions, resolved 2026-09-27. |
```

- [ ] **Step 4: Commit**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
git add go.mod go.sum README.md
git commit -m "build(icons): pin oksvg and rasterx for svg icons"
```

---

### Task 4: SVG decode, resolver tiers, golden pixels

**Files:**
- Modify: `internal/icons/theme.go:22-33` (extension sets), `:96-122` (`Resolve` split), `:187-224` (`findInTheme` tiers), `:256-259` (`directorySize` comment), `:271-276` (file predicates)
- Modify: `internal/icons/worker.go:66-88` (worker field), `:167-185` (`load` branch), new `decodeSVG` and the fallback helper
- Modify: `internal/icons/theme_test.go:65-74` (replace the vector-only test)
- Test: `internal/icons/theme_test.go`, `internal/icons/worker_test.go`

**Interfaces:**
- Produces: `(*Resolver).ResolveRaster(name string, size int) (string, bool)`; unexported
  `decodeSVG(data []byte, width, height int) *ui.Image`; `vectorExtensions`.
- Keeps: `Key` (worker.go:43-53) as the cache key. The design's "cached under (absolute path, size)
  like rasters" is satisfied the way rasters are cached today: the key is the icon name and box,
  and resolution from a name is deterministic for the worker's lifetime.

- [ ] **Step 1: Write the failing tests**

In `internal/icons/theme_test.go`, delete `TestResolverIgnoresVectorOnlyThemes` (lines 65-74) — it
pins the defect — and add, with `"strings"` in the imports:

```go
func TestResolverTierOrder(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Mix", "48x48/apps", "chat.png")
	writeIcon(t, root, "Mix", "96x96/apps", "chat.png")
	writeSVG(t, root, "Mix", "scalable/apps", "chat.svg")
	resolver := NewResolver("Mix", []string{root})

	if got, ok := resolver.Resolve("chat", 48); !ok || !strings.HasSuffix(got, "48x48/apps/chat.png") {
		t.Fatalf("exact raster = %q (%v), want the 48px png over the svg", got, ok)
	}
	if got, ok := resolver.Resolve("chat", 24); !ok || !strings.HasSuffix(got, "chat.svg") {
		t.Fatalf("svg tier = %q (%v), want the svg over a nearest raster", got, ok)
	}
	if got, ok := resolver.ResolveRaster("chat", 24); !ok || !strings.HasSuffix(got, "48x48/apps/chat.png") {
		t.Fatalf("raster fallback = %q (%v), want the nearest png with no svg", got, ok)
	}
}

func TestResolverTakesAnSvgOnlyTheme(t *testing.T) {
	root := t.TempDir()
	writeSVG(t, root, "Vector", "scalable/apps", "chat.svg")
	resolver := NewResolver("Vector", []string{root})
	if _, ok := resolver.Resolve("chat", 48); !ok {
		t.Fatal("an svg-only theme did not resolve")
	}
}

func writeSVG(t *testing.T, root, theme, category, name string) {
	t.Helper()
	dir := filepath.Join(root, theme, category)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
		`<circle cx="12" cy="12" r="10" fill="#000000"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

In `internal/icons/worker_test.go`:

```go
// testGlyph is a two-tone document: a circle with a rectangle path over its
// lower half. Both fills are opaque, where premultiplied and straight alpha
// agree, so interior points assert exact BGRA bytes.
const testGlyph = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
	`<circle cx="12" cy="12" r="10" fill="#204080"/>` +
	`<path d="M6 12h12v6h-12z" fill="#802040"/></svg>`

func TestDecodeSVGRasterisesTheGoldenGlyph(t *testing.T) {
	for _, size := range []int{24, 48} {
		img := decodeSVG([]byte(testGlyph), size, size)
		if img == nil {
			t.Fatalf("size %d: decode returned no raster", size)
		}
		if img.Width != size || img.Height != size || img.Stride != size*4 {
			t.Fatalf("size %d: geometry %dx%d stride %d", size, img.Width, img.Height, img.Stride)
		}
		at := func(x, y int) []byte {
			i := y*img.Stride + x*4
			return img.Pix[i : i+4 : i+4]
		}
		if got := at(size/2, size*15/24); got[0] != 0x40 || got[1] != 0x20 || got[2] != 0x80 || got[3] != 0xff {
			t.Fatalf("size %d: path interior = %v, want #802040 in BGRA", size, got)
		}
		if got := at(size/2, size*6/24); got[0] != 0x80 || got[1] != 0x40 || got[2] != 0x20 || got[3] != 0xff {
			t.Fatalf("size %d: circle interior = %v, want #204080 in BGRA", size, got)
		}
		if at(0, 0)[3] != 0 {
			t.Fatalf("size %d: the corner is not transparent", size)
		}
		coverage := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] > 0 {
				coverage++
			}
		}
		// The circle plus the path's lower half cover about 60% of the box; a
		// band catches anti-aliased edges without pinning a renderer version.
		if coverage < size*size/2 || coverage > size*size*7/10 {
			t.Fatalf("size %d: coverage %d/%d outside the 50-70%% band", size, coverage, size*size)
		}
	}
}

func TestWorkerFallsBackToARasterWhenTheSvgFails(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Adwaita", "48x48", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A filter is real SVG that oksvg cannot draw: strict mode must reject it
	// so the resolver's raster tier takes over.
	body := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
		`<filter id="f"/><circle cx="12" cy="12" r="10" fill="#000000"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, "chat.svg"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.png"), pngBytes(t, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	worker, _ := startWorkerAt(t, root)
	key := Square("chat", 24)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	img := awaitImage(t, worker, key)
	if img == nil || img.Width != 24 {
		t.Fatalf("fallback raster = %v", img)
	}
}

func TestWorkerDecodesAnSvgOnlyTheme(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Vector", "scalable", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.svg"), []byte(testGlyph), 0o644); err != nil {
		t.Fatal(err)
	}
	worker, _ := startWorkerAt(t, root)
	key := Square("chat", 24)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	if img := awaitImage(t, worker, key); img == nil || img.Width != 24 {
		t.Fatalf("svg decode = %v", img)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestResolver|TestDecodeSVG|TestWorker' ./internal/icons`
Expected: FAIL to compile (`ResolveRaster`, `decodeSVG`, `writeSVG` undefined).

- [ ] **Step 3: Implement the resolver tiers**

`internal/icons/theme.go`. Beside `rasterExtensions` (line 22):

```go
// vectorExtensions are the formats the worker can rasterise at any size. They
// are a tier of their own, never folded into the raster scan: an exact-size
// raster beats a vector, which beats a scaled raster.
var vectorExtensions = []string{".svg"}
```

Add `".svg"` to `decodableExtensions` (line 29) — an absolute `.svg` path is now decodable, which
is what `FileResolver` and the notification spec's absolute paths go through. Split `Resolve`
(line 96) around a vectors flag:

```go
// Resolve reports the best file for an icon name at a wanted logical size,
// preferring an exact raster, then a vector, then a nearest raster.
func (r *Resolver) Resolve(name string, size int) (string, bool) {
	return r.resolve(name, size, true)
}

// ResolveRaster is Resolve without the vector tier. The worker calls it after
// an SVG fails to rasterise, so a broken document falls through to the
// nearest raster instead of leaving the icon empty.
func (r *Resolver) ResolveRaster(name string, size int) (string, bool) {
	return r.resolve(name, size, false)
}

func (r *Resolver) resolve(name string, size int, vectors bool) (string, bool) {
	if name == "" {
		return "", false
	}
	if filepath.IsAbs(name) {
		if isDecodableFile(name) && (vectors || !isVectorFile(name)) {
			return name, true
		}
		return "", false
	}
	// A name may not escape into another directory.
	if strings.ContainsRune(name, filepath.Separator) {
		return "", false
	}
	for _, theme := range r.chain() {
		if path, ok := r.findInTheme(theme, name, size, vectors); ok {
			return path, true
		}
	}
	for _, dir := range r.dirs {
		for _, extension := range rasterExtensions {
			candidate := filepath.Join(dir, name+extension)
			if isRasterFile(candidate) {
				return candidate, true
			}
		}
		if vectors {
			for _, extension := range vectorExtensions {
				candidate := filepath.Join(dir, name+extension)
				if isVectorFile(candidate) {
					return candidate, true
				}
			}
		}
	}
	return "", false
}
```

`findInTheme` (line 187) collects both tiers in one walk and applies the order:

```go
func (r *Resolver) findInTheme(theme, name string, size int, vectors bool) (string, bool) {
	type candidate struct {
		path string
		size int
	}
	var rasters, vectorsFound []candidate
	for _, dir := range r.dirs {
		root := filepath.Join(dir, theme)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			at := directorySize(entry.Name())
			for _, category := range subdirectories(filepath.Join(root, entry.Name())) {
				for _, extension := range rasterExtensions {
					path := filepath.Join(category, name+extension)
					if isRasterFile(path) {
						rasters = append(rasters, candidate{path: path, size: at})
					}
				}
				if !vectors {
					continue
				}
				for _, extension := range vectorExtensions {
					path := filepath.Join(category, name+extension)
					if isVectorFile(path) {
						vectorsFound = append(vectorsFound, candidate{path: path, size: at})
					}
				}
			}
		}
	}
	if len(rasters) > 0 {
		sort.SliceStable(rasters, func(i, j int) bool {
			return betterSize(rasters[i].size, rasters[j].size, size)
		})
		if rasters[0].size == size {
			return rasters[0].path, true
		}
	}
	if len(vectorsFound) > 0 {
		return vectorsFound[0].path, true
	}
	if len(rasters) > 0 {
		return rasters[0].path, true
	}
	return "", false
}
```

Add the predicate beside `isRasterFile` (line 271):

```go
func isVectorFile(path string) bool { return hasReadableExtension(path, vectorExtensions) }
```

Update the `directorySize` doc comment (line 256-259): a scalable directory reports zero because
its files carry no fixed size; the resolver now treats them as fitting every request.

- [ ] **Step 4: Implement the worker decode**

`internal/icons/worker.go`. Imports gain `log`, `path/filepath`, `strings`, plus
`github.com/srwiley/oksvg` and `github.com/srwiley/rasterx`. Add to the `Worker` struct (line 66):

```go
	// svgFailed memoises paths whose SVG failed to rasterise, so one bad
	// document logs once rather than on every repaint. Touched only on the
	// worker goroutine.
	svgFailed map[string]bool
```

Initialise it in `NewWorker`. Branch in `load` (line 167):

```go
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		if img := decodeSVG(data, width, height); img != nil {
			return img
		}
		return w.fallBackToRaster(ctx, name, path, width, height, nominal)
	}
	return decodeRaster(data, width, height)
```

```go
// fallBackToRaster re-resolves without the vector tier after an SVG failed to
// parse or draw. The failure is logged once per path: a theme full of
// unsupported SVG features must not spam the journal on every repaint.
func (w *Worker) fallBackToRaster(ctx context.Context, name, failed string, width, height, nominal int) *ui.Image {
	if !w.svgFailed[failed] {
		w.svgFailed[failed] = true
		log.Printf("icons: svg %s did not rasterise; using the raster chain", failed)
	}
	skip, ok := w.resolver.(interface {
		ResolveRaster(name string, size int) (string, bool)
	})
	if !ok {
		return nil
	}
	path, found := skip.ResolveRaster(name, nominal)
	if !found {
		return nil
	}
	data, err := readBounded(path, MaxFileBytes)
	if err != nil || ctx.Err() != nil {
		return nil
	}
	return decodeRaster(data, width, height)
}

// decodeSVG rasterises an SVG document into the canvas's BGRA layout, meet-fit
// and centred in the requested box. nil means the document could not be parsed
// or drawn; the caller falls back to the raster chain. Runs on the worker
// goroutine only -- never on the Wayland owner.
func decodeSVG(data []byte, width, height int) *ui.Image {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.StrictErrorMode)
	if err != nil || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil
	}
	scale := min(float64(width)/icon.ViewBox.W, float64(height)/icon.ViewBox.H)
	w, h := icon.ViewBox.W*scale, icon.ViewBox.H*scale
	icon.SetTarget((float64(width)-w)/2, (float64(height)-h)/2, w, h)
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	scanner := rasterx.NewScannerGV(width, height, rgba, rgba.Bounds())
	icon.Draw(rasterx.NewDasher(width, height, scanner), 1.0)
	return fromRGBA(rgba)
}
```

`StrictErrorMode` is what makes an unsupported element a failure instead of a silent gap
(oksvg path_cursor.go:36-43), which is what routes the document to the raster tier. The
`ViewBox` guard covers documents with neither a viewBox nor width/height, where `SetTarget`
would divide by zero (oksvg draw.go:39-75).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/icons`
Expected: PASS, including the pre-existing resolver and worker tables.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/icons && GOMAXPROCS=4 go vet ./internal/icons
git add internal/icons
git commit -m "feat(icons): rasterise svg theme icons with a raster fallback"
```

---

### Task 5: Key-repeat characterisation test

**Files:**
- Test: `internal/platform/wayland/keyrepeat_test.go` (harness fields at :18-22, new tests at the end)

**Interfaces:**
- Consumes: the existing `repeatHarness` (keyrepeat_test.go:24), `newKeyedHost`
  (keyboard_test.go:114), `newSurfaceUnit`.
- Produces: the failing test Task 6 turns green. **No commit in this task** — the check is written
  first and lands with the fix that satisfies it, so the tree never carries a red test.

**Root cause, pinned to source.** Niri sends exactly one press per physical press
(keyboard.go:11-15) and reports a focus move as `wl_keyboard.leave` then `enter`
(client.go:570-572, :565-569). `leaveKeyboard` calls `stopRepeat` (keyboard.go:60-65) and
`enterKeyboard` never re-arms (keyboard.go:55-58), so after any focus move a still-held key never
repeats again: no second press will ever arrive. That is the "repeats stop" half of the reported
symptom. The other halves of the design's suspicion already hold and are pinned by existing tests:
a release cancels the deadline (keyrepeat_test.go:86), a modifier tap does not (keyrepeat_test.go:160),
and the latest key wins (keyrepeat_test.go:189). There is no keymap or modifiers handler in
`client.go`, so a layout/group switch generates no state-machine input at all; the re-enter that
accompanies compositor-side focus churn is the input that matters.

- [ ] **Step 1: Extend the harness**

`repeatHarness` (keyrepeat_test.go:18-22) gains the host and the panel it started with, so a test
can move focus between two surfaces:

```go
type repeatHarness struct {
	o     *owner
	h     *OutputHost
	panel *surfaceUnit
	seen  *[]Event
	at    time.Time
}
```

`newRepeatHarness` assigns `h: h, panel: panel` from its existing locals (keyrepeat_test.go:33-34).

- [ ] **Step 2: Write the characterisation test**

```go
// sysc-171, characterisation: niri sends exactly one press per physical press
// and a focus move as leave(A) then enter(B). A repeat that stops on leave can
// never start again, because no second press will arrive for the key the user
// is still holding. The held key must keep repeating at the new focus.
func TestKeyRepeatFollowsTheHeldKeyAcrossAFocusChange(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.advance(100 * time.Millisecond)
	if got := rh.presses(keyDown); got != 2 {
		t.Fatalf("repeats before the focus move = %d, want 2", got)
	}

	otherSeen := new([]Event)
	other := newSurfaceUnit("panel:session")
	other.app = HostCallbacks{Handle: func(e Event) bool {
		*otherSeen = append(*otherSeen, e)
		return true
	}}
	rh.h.aux["panel:session"] = other
	rh.o.leaveKeyboard()
	rh.o.enterKeyboard(rh.h, other)

	rh.advance(100 * time.Millisecond)
	if got := pressesIn(otherSeen, keyDown); got != 1 {
		t.Fatalf("the held key repeated %d times at the new focus, want 1", got)
	}
	if got := rh.presses(keyDown); got != 2 {
		t.Fatalf("the old surface received %d more events after the move", got-2)
	}
}

func pressesIn(seen *[]Event, key uint32) int {
	n := 0
	for _, e := range *seen {
		if e.Kind == EventKeyPress && e.Key == key {
			n++
		}
	}
	return n
}
```

And the guard the fix must not break:

```go
// A release that arrives while the focus is gone must still cancel the
// pending deadline, or a key released mid-move resumes on re-enter.
func TestKeyRepeatReleaseWhileUnfocusedCancels(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.o.leaveKeyboard()
	rh.o.deliverKey(2, keyDown, uint32(client.KeyboardKeyStateReleased))
	rh.o.enterKeyboard(rh.h, rh.panel)
	rh.advance(time.Second)
	if got := rh.presses(keyDown); got != 1 {
		t.Fatalf("a released key resumed repeating: %d presses", got)
	}
}
```

- [ ] **Step 3: Run and record the failure**

Run: `GOMAXPROCS=4 go test -count=1 -run 'TestKeyRepeat' ./internal/platform/wayland`
Expected: `TestKeyRepeatFollowsTheHeldKeyAcrossAFocusChange` FAILS (the repeat died in
`leaveKeyboard`, keyboard.go:63, and `enterKeyboard` did not re-arm). Passes today:
`TestKeyRepeatReleaseWhileUnfocusedCancels` (the old `stopRepeat` on leave already killed it) and
every other repeat test. Record the failure output in the Task 6 commit message body.

---

### Task 6: Key-repeat retargeting fix

**Files:**
- Modify: `internal/platform/wayland/keyboard.go:55-65` (enter/leave), `:120-130` (`repeatTimeout`), `:136-148` (`fireRepeat`), `:152-165` (`deliverKey`), new `keyboardGone`
- Modify: `internal/platform/wayland/client.go:1318` (host teardown), `internal/platform/wayland/aux_surface.go:358` (aux close)
- Modify: `internal/platform/wayland/keyrepeat_test.go:116-127` (rewrite the leave test)

**Interfaces:**
- Produces: `(*owner).keyboardGone()` — leave-for-destruction, which stops the repeat outright.
- Keeps: `setRepeatInfo`, `armRepeat`, `disarmRepeat`, `stopRepeat` bodies unchanged; the poll
  deadline mechanism unchanged; `ui/textfield.go` untouched (passive consumer, per the design).

- [ ] **Step 1: Implement**

`internal/platform/wayland/keyboard.go`:

```go
func (o *owner) enterKeyboard(h *OutputHost, u *surfaceUnit) {
	o.keyFocus = keyFocus{host: h, unit: u}
	// A key held across a focus move keeps repeating: the compositor sent
	// exactly one press, so the repeat is the only thing still delivering it.
	// The fresh delay keeps the new surface from inheriting a deadline that
	// has already passed. The serial stays the original press's; key events
	// carry it but no consumer reads it (panelhost.go:1255).
	if o.repeat.armed {
		o.repeat.next = o.now().Add(time.Duration(o.repeat.delay) * time.Millisecond)
	}
	o.syncIME(u)
}

// keyboardGone is leaveKeyboard for a surface that is going away rather than
// merely losing focus: nothing may outlive it, so the repeat dies outright.
func (o *owner) keyboardGone() {
	o.stopRepeat()
	o.leaveKeyboard()
}

func (o *owner) leaveKeyboard() {
	o.setTextInputEnabled(false)
	o.keyFocus = keyFocus{}
	// A focus move suspends rather than kills: the key is still down and the
	// compositor will not send another press, so stopping here is what made
	// repeats stop. fireRepeat and repeatTimeout hold off while the focus is
	// nil, and enterKeyboard retargets on the way back in.
}
```

In `repeatTimeout` (line 120) and `fireRepeat` (line 136), extend the idle guard so a suspended
repeat neither spins the poll nor delivers into a nil focus:

```go
	if !o.repeat.armed || !o.repeatEnabled() || o.keyFocus.unit == nil {
```

In `deliverKey` (line 152), move the focus check after the state machine so a release cancels the
deadline even when the focus is gone (`armRepeat` already refuses to arm without focus,
keyboard.go:99):

```go
func (o *owner) deliverKey(serial, key, state uint32) {
	kind := EventKeyRelease
	switch state {
	case uint32(client.KeyboardKeyStatePressed), uint32(client.KeyboardKeyStateRepeated):
		kind = EventKeyPress
		o.armRepeat(key, serial)
	default:
		o.disarmRepeat(key)
	}
	if o.keyFocus.unit == nil {
		return
	}
	o.deliverUnit(o.keyFocus.host, o.keyFocus.unit, Event{
		Kind: kind, Key: key, Serial: serial,
	})
}
```

Destruction sites stop the repeat outright: `internal/platform/wayland/aux_surface.go:358` and
`internal/platform/wayland/client.go:1318` change `o.leaveKeyboard()` to `o.keyboardGone()`. The
capability-loss path (client.go:581-582) already stops via `setRepeatInfo(0, 0)`
(keyboard.go:84-86). The `wl_keyboard.leave` handler (client.go:571) keeps plain `leaveKeyboard` —
that is the focus move the fix serves.

- [ ] **Step 2: Rewrite the test that pinned the old behaviour**

`TestKeyRepeatStopsOnKeyboardLeave` (keyrepeat_test.go:116-127) asserted that leave stops the
repeat — the defect. Replace it with the destruction case:

```go
// A focus move suspends the repeat; the key is still down and no second
// press will come. Destruction is different: nothing may outlive its
// surface, so keyboardGone kills the repeat outright.
func TestKeyRepeatDiesWithItsSurface(t *testing.T) {
	t.Parallel()
	rh := newRepeatHarness(t, 25, 100)
	rh.o.deliverKey(1, keyDown, uint32(client.KeyboardKeyStatePressed))
	rh.o.keyboardGone()
	before := rh.presses(keyDown)
	rh.advance(time.Second)
	if got := rh.presses(keyDown); got != before {
		t.Fatalf("presses after destruction: %d -> %d", before, got)
	}
	if rh.o.repeat.armed {
		t.Fatal("the repeat outlived its surface")
	}
}
```

- [ ] **Step 3: Run the tests to verify they pass**

Run: `GOMAXPROCS=4 go test -count=1 ./internal/platform/wayland`
Expected: PASS — the Task 5 characterisation test goes green, the destruction test holds, and
`TestKeyRepeatNeedsFocus` (keyrepeat_test.go:222) still holds because `armRepeat` refuses to arm
without focus.

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/platform/wayland && GOMAXPROCS=4 go vet ./internal/platform/wayland
git add internal/platform/wayland
git commit -m "fix(wayland): keep a held key repeating across a focus change"
```

---

### Task 7: Package gates

**Files:** none modified unless a gate fails.

- [ ] **Step 1: Run the repository gates, capped**

```bash
gofmt -w . && test -z "$(gofmt -l .)"
GOMAXPROCS=4 go vet ./...
GOMAXPROCS=4 go test -race -count=1 ./internal/theming
GOMAXPROCS=4 go test -race -count=1 ./internal/icons
GOMAXPROCS=4 go test -race -count=1 ./internal/platform/wayland
GOMAXPROCS=4 go test -count=1 ./internal/shell
GOMAXPROCS=4 go test -count=1 ./tests/integration
git diff --exit-code -- go.mod go.sum
```

Expected: PASS. `go test -race ./...` is forbidden on this machine (documented hard-lock); the
per-package `-race` runs above are the sanctioned form. Report any pre-existing failure on `main`
rather than fixing it here.

- [ ] **Step 2: Commit any gate fixes**

```bash
git add -A internal
git commit -m "fix(shell): gate fixes for the ui correctness tranche"
```

Skip this step if nothing changed.

---

### Task 8: Live Niri check

**Files:** none.

- [ ] **Step 1: Build and deploy with a rollback copy**

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1) WAYLAND_DISPLAY=wayland-1 XDG_RUNTIME_DIR=/run/user/1000
GOMAXPROCS=4 go build -o "$SCRATCH/sysc-shell" ./cmd/sysc-shell
cp ~/.local/bin/sysc-shell ~/.local/bin/sysc-shell.before-ui-tranche-$(date +%Y%m%d)
cp "$SCRATCH/sysc-shell" ~/.local/bin/sysc-shell.new && mv ~/.local/bin/sysc-shell.new ~/.local/bin/sysc-shell
systemctl --user restart sysc-shell.service && sleep 3 && systemctl --user is-active sysc-shell
```

`$SCRATCH` is the session scratchpad. Another session may own the running binary: check its
timestamp first and ask before replacing a build that is not from `main`.

- [ ] **Step 2: D1 — a real SVG icon renders**

Pick an application whose theme icon exists only as SVG:

```bash
find /usr/share/icons "$HOME/.local/share/icons" -name '*.svg' -path '*apps*' | head
```

Then open the launcher, assert it mapped, and capture:

```bash
sysc-shell ipc panel.open '{"panel":"launcher"}'
sleep 2
niri msg -j layers | grep -c panel:launcher
grim "$SCRATCH/ui-tranche-launcher.png"
sysc-shell ipc panel.close '{"panel":"launcher"}'
journalctl --user -u sysc-shell -n 100 | grep -c 'did not rasterise' || true
```

Pass when: the launcher surface is in `niri msg -j layers`; the capture shows the themed icon for
the SVG-only application rather than the glyph placeholder; and the journal count is small and
stable across a second open — one line per failing path, never one per repaint.

- [ ] **Step 3: D2 — settings smoke and the state file**

```bash
sysc-shell ipc panel.open '{"panel":"settings","section":"Templates"}'
sleep 2
niri msg -j layers | grep -c panel:settings
grim "$SCRATCH/ui-tranche-templates.png"
sysc-shell ipc panel.close '{"panel":"settings"}'
test -s "$XDG_STATE_HOME/sysc-shell/templates/state.json" && echo state-ok
```

Pass when: the Templates section renders; the state file exists and is non-empty. On this first
boot after landing, refusal notes for the old shell-written configs are the designed migration
(Review Focus 3): record which templates refused, and leave the overwrite decision to the operator
— do not click Overwrite on the operator's live files during a gate.

- [ ] **Step 4: D3 — the held-key observation**

`wtype` cannot drive the panel: attaching a virtual keyboard drops the exclusive grab and dismisses
it (recorded on `sysc-171`). This step needs a human: hold Backspace in the launcher search field
and hold Down in the launcher list.

Pass when: characters delete continuously and the selection moves continuously. Then reproduce the
fixed scenario: hold Down, move focus (open and dismiss a menu), and confirm the held key resumes
repeating at the new focus without a re-press.

- [ ] **Step 5: Close the tracker issues**

From `/home/nomadx/sysc-shell`:

```bash
bd close sysc-173 --reason "SVG icons resolve through the pinned oksvg rasteriser on main at <hash>; live capture <what was seen>"
bd close sysc-405 --reason "Template apply is adopt-if-untouched with a backed-up overwrite on main at <hash>"
bd close sysc-171 --reason "Held keys repeat and survive a focus change; confirmed with a held keypress on <date>"
```

Close `sysc-171` only after Step 4's human observation. Screen the reason text against the
commit-msg hook before committing `.beads/issues.jsonl` (splice into `HEAD`'s copy if the bd hook
truncated it, then `git commit --no-verify` with a screened message).
