# sysc-panel-preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A small command in sysc-shell that paints a plugin panel tree (JSON) to a PNG with the host's real layout, painter, fonts and default dark theme, so sysc-plugins can generate catalog screenshots without importing the shell.

**Architecture:** `cmd/sysc-panel-preview` reads a `v1.Node` as JSON, runs `plugin.Convert(root, v1.ViewPanel)`, lays it out with `ui.LayoutColumn` using real text metrics, paints it with `render.Paint` using `shell.DefaultTheme().PanelStyle()`, and writes a PNG. It lives in the shell repo because the theme and panel style are in `internal/shell`; a public package importing that would add about 35 modules to every consumer.

**Tech Stack:** Go (the shell's `internal/plugin`, `internal/render`, `internal/ui`, `internal/shell`, `plugin/v1`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-09-plugin-panel-captures-design.md` in the sysc-plugins repository (branch `docs/plugin-panel-captures-spec`, commit `8ddf8b3`, component 1). This plan covers only that component; the sysc-plugins half (capture helper and the 16 scenes) is planned separately once this command exists.

## Global Constraints

- Go only; no new dependency (`go.mod` and `go.sum` unchanged). `AGENTS.md` rules: stop at the first working rung, table tests for pure layout code, one focused runnable check for non-trivial logic.
- Reuse the host's pipeline; do not reimplement layout or painting. A tree the host refuses must be refused with the host's wording.
- Logical size is `1..8192` on each edge; scale is `1..480` in 120ths (120 = 100%, default **180** = 150%).
- The panel is **opaque** with the host's rounded corners; pixels outside the corners are transparent. The command is a stateless still: no animation, hover or focus.
- A failed render must leave **no output file** behind.
- Go commands must be capped on this machine: `go test -count=1 -p 2 <one package>`; never an uncapped `./...` (a hook blocks it; the machine has locked up twice).
- Commit messages contain **no** AI attribution (the repo's commit-msg hook rejects it). The bd pre-commit hook fails in fresh worktrees: commit with a scratch DB: `cp ~/sysc-shell/.beads/beads.db "$SCRATCH/beads-shell.db" && sqlite3 "$SCRATCH/beads-shell.db" 'delete from dirty_issues;'` then `BEADS_DB="$SCRATCH/beads-shell.db" git commit ...`.
- Work in the existing worktree `~/worktrees/sysc-shell-preview` (branch `feat/plugin-preview` off `origin/main` at `ee5049b5`); rename the branch to `feat/sysc-panel-preview` before pushing.
- `gofmt -l .` must print nothing before each commit.

## Review Focus

Inputs and conditions the spec implies but the tests below do not all pin:

1. **A tree the host cannot lay out** must fail with the host's own wording and write no file. Task 1 (`TestRenderPanelRefusesATreeThatCannotFit`) and Task 1 (`TestRunLeavesNoFileWhenTheRenderFails`).
2. **Absurd sizes or scales from a caller** (a scene bug asking for 100000 px or 9000%) must be refused, not allocate gigabytes. Task 1 (`TestRenderPanelRejectsSizesAndScalesOutOfRange`).
3. **JSON that is not exactly one tree** (unknown fields, trailing data, an empty body) must be rejected. Task 1 (`TestRunRejectsBadInput`).
4. **Text drawn with whatever fonts the machine has**: CI installs `fonts-inter` and Noto, so tests must not assert pixel text content, only structure (size, opacity, more than a flat fill).
5. **Fonts missing entirely**: the command must fail with a `fonts:` error, not paint blanks. Covered by the code path (`NewSystemFontMap` errors when no font resolves); not testable on a machine that has fonts, so verified by reading the code in Task 2.

## File Structure

| Path | Responsibility |
|---|---|
| `cmd/sysc-panel-preview/render.go` | `renderPanel`: validate, convert, lay out, paint, convert to `image.NRGBA`; size/scale caps |
| `cmd/sysc-panel-preview/main.go` | Flags, JSON decode (strict, one tree), render, write PNG; package doc |
| `cmd/sysc-panel-preview/render_test.go` | Tests for `renderPanel` |
| `cmd/sysc-panel-preview/main_test.go` | Tests for `run` (the CLI) |
| `docs/development.md` | One layout line and a short usage section |

---

### Task 1: The command

**Files:**
- Create: `cmd/sysc-panel-preview/render.go`, `cmd/sysc-panel-preview/main.go`
- Test: `cmd/sysc-panel-preview/render_test.go`, `cmd/sysc-panel-preview/main_test.go`

**Interfaces:**
- Produces: `func renderPanel(root *v1.Node, width, height, scale120 int, family string) (*image.NRGBA, error)`; `func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error`; CLI `sysc-panel-preview -width W -height H [-scale 180] [-font family] [-o out.png] [tree.json]`.

- [ ] **Step 1: Write the render tests**

Create `cmd/sysc-panel-preview/render_test.go`:

```go
package main

import (
	"image"
	"image/color"
	"strings"
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// A small panel with text and a progress bar, so a render has both ink and
// structure to check. sampleTree is the same tree as wire JSON.
const sampleTree = `{"kind":"column","padding":16,"gap":8,"children":[
  {"kind":"text","text":"Pomodoro Timer","role":"title"},
  {"kind":"text","text":"Work session 2 of 4"},
  {"kind":"progress","value":0.4,"name":"session","role":"progressbar"}
]}`

func sampleNode() *v1.Node {
	return &v1.Node{Kind: v1.KindColumn, Padding: 16, Gap: 8, Children: []*v1.Node{
		{Kind: v1.KindText, Text: "Pomodoro Timer", Role: "title"},
		{Kind: v1.KindText, Text: "Work session 2 of 4"},
		{Kind: v1.KindProgress, Value: 0.4, Name: "session", Role: "progressbar"},
	}}
}

func TestRenderPanelHasTheRequestedPhysicalSize(t *testing.T) {
	img, err := renderPanel(sampleNode(), 360, 240, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	// 180 in 120ths is 150%: 360x240 logical is 540x360 physical.
	if got := img.Bounds(); got != image.Rect(0, 0, 540, 360) {
		t.Fatalf("bounds = %v, want 540x360", got)
	}
}

func TestRenderPanelIsOpaqueInsideWithTransparentCorners(t *testing.T) {
	img, err := renderPanel(sampleNode(), 360, 240, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("corner alpha = %d, want 0 (outside the rounded surface)", a)
	}
	if a := img.NRGBAAt(270, 300).A; a != 0xff {
		t.Errorf("body alpha = %d, want 0xff (an opaque panel)", a)
	}
}

func TestRenderPanelPaintsMoreThanAFlatFill(t *testing.T) {
	img, err := renderPanel(sampleNode(), 360, 240, 180, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[color.NRGBA]bool{}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			seen[img.NRGBAAt(x, y)] = true
		}
	}
	if len(seen) < 8 {
		t.Fatalf("only %d distinct colours; the tree should paint text and a bar", len(seen))
	}
}

func TestRenderPanelRefusesATreeThatCannotFit(t *testing.T) {
	// An 8-padded row only 28 tall leaves 12 px for a 20 px icon: the host
	// refuses this view, and so must the preview.
	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindRow, Height: 28, Padding: 8, Children: []*v1.Node{
			{Kind: v1.KindIcon, Icon: "battery_full", IconSize: 20},
		}},
	}}
	_, err := renderPanel(root, 290, 200, 180, "")
	if err == nil || !strings.Contains(err.Error(), "does not fit") {
		t.Fatalf("err = %v, want the host's does-not-fit refusal", err)
	}
}

func TestRenderPanelRefusesAnInvalidTree(t *testing.T) {
	// A panel's root must be a column; a text root is invalid.
	if _, err := renderPanel(&v1.Node{Kind: v1.KindText, Text: "x"}, 200, 100, 180, ""); err == nil {
		t.Fatal("a text root rendered; want the host's refusal")
	}
}

func TestRenderPanelRejectsSizesAndScalesOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name        string
		w, h, scale int
		want        string
	}{
		{"zero width", 0, 100, 180, "outside 1.."},
		{"zero height", 200, 0, 180, "outside 1.."},
		{"negative width", -5, 100, 180, "outside 1.."},
		{"width past the cap", 9000, 100, 180, "outside 1.."},
		{"height past the cap", 200, 9000, 180, "outside 1.."},
		{"zero scale", 200, 100, 0, "scale"},
		{"negative scale", 200, 100, -5, "scale"},
		{"scale past 400%", 200, 100, 600, "scale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := renderPanel(sampleNode(), tc.w, tc.h, tc.scale, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -count=1 -p 2 ./cmd/sysc-panel-preview/`
Expected: FAIL to build, `undefined: renderPanel`.

- [ ] **Step 3: Implement `render.go` and a placeholder `main.go`**

Create `cmd/sysc-panel-preview/render.go`:

```go
package main

import (
	"fmt"
	"image"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	// defaultScale120 is 150%, a common laptop display scale.
	defaultScale120 = 180
	// maxLogical and maxScale120 bound the buffer a caller can ask for: 8192
	// logical pixels at 400% is a 32768 px edge, already far past any panel.
	maxLogical  = 8192
	maxScale120 = 480
)

// renderPanel lays out and paints root as a panel of width by height logical
// pixels at scale120 and returns the pixels. family names the font family;
// empty means the shell's default.
func renderPanel(root *v1.Node, width, height, scale120 int, family string) (*image.NRGBA, error) {
	if width < 1 || height < 1 || width > maxLogical || height > maxLogical {
		return nil, fmt.Errorf("size %dx%d is outside 1..%d", width, height, maxLogical)
	}
	scale := ui.Scale120(scale120)
	if !scale.Valid() || scale120 > maxScale120 {
		return nil, fmt.Errorf("scale %d is outside 1..%d (120 is 100%%)", scale120, maxScale120)
	}

	tree, err := plugin.Convert(root, v1.ViewPanel)
	if err != nil {
		return nil, err
	}

	fonts, err := render.NewSystemFontMap(family, render.DefaultFontCacheDir())
	if err != nil {
		return nil, fmt.Errorf("fonts: %w", err)
	}
	text := render.NewTextRendererWithFontMap(fonts)

	theme := shell.DefaultTheme()
	style := theme.PanelStyle()
	style.Rim = theme.Outline
	style.SurfaceOpacity = 0xff
	style.Scale120 = scale
	body := ui.Rect{W: width, H: height}
	style.Body = body

	measure := func(s string, attrs ui.TextAttrs) (int, int) {
		spec := render.SpecFor(style, attrs)
		if spec.Size > 0 {
			if mw, mh, err := text.Measure(s, spec, attrs.Tabular); err == nil {
				return scale.Logical(mw), scale.Logical(mh)
			}
		}
		return len(s) * 8, 16
	}
	if tree.Kind == ui.KindRow {
		err = ui.Layout(tree, body, measure)
	} else {
		err = ui.LayoutColumn(tree, body, measure)
	}
	if err != nil {
		return nil, err
	}

	pw, ph := scale.Physical(width), scale.Physical(height)
	pix := make([]byte, pw*ph*4)
	canvas, err := render.NewCanvas(pix, pw, ph, pw*4)
	if err != nil {
		return nil, err
	}
	if err := render.Paint(canvas, tree, text, style); err != nil {
		return nil, err
	}
	return fromBGRA(pix, pw, ph), nil
}

// fromBGRA converts the painter's premultiplied BGRA bytes to straight RGBA.
func fromBGRA(pix []byte, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		b, g, r, a := int(pix[i*4]), int(pix[i*4+1]), int(pix[i*4+2]), int(pix[i*4+3])
		if a == 0 {
			continue
		}
		o := i * 4
		img.Pix[o] = uint8(min(255, r*255/a))
		img.Pix[o+1] = uint8(min(255, g*255/a))
		img.Pix[o+2] = uint8(min(255, b*255/a))
		img.Pix[o+3] = uint8(a)
	}
	return img
}
```

A package `main` needs a `main` function to build, so create a placeholder `cmd/sysc-panel-preview/main.go` that Step 8 replaces:

```go
// Command sysc-panel-preview paints a plugin panel tree to a PNG.
package main

func main() {}
```

- [ ] **Step 4: Run to verify the render tests pass**

Run: `gofmt -l cmd; go vet -p 2 ./cmd/sysc-panel-preview/ && go test -count=1 -p 2 -v ./cmd/sysc-panel-preview/ 2>&1 | grep -E "^(\s*--- |ok|FAIL)"`
Expected: no gofmt output; every `TestRenderPanel*` line `--- PASS`; `ok`. (This code was prototyped and these tests passed against it.)

- [ ] **Step 5: Commit the render core**

```bash
git add cmd/sysc-panel-preview
BEADS_DB="$SCRATCH/beads-shell.db" git commit -m "feat(preview): render a plugin panel tree to pixels with the host's pipeline"
```

- [ ] **Step 6: Write the CLI tests**

Create `cmd/sysc-panel-preview/main_test.go`:

```go
package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, stdin string, args ...string) (stdout []byte, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = run(args, strings.NewReader(stdin), &out, &errOut)
	return out.Bytes(), err
}

func TestRunWritesAPNGToTheNamedFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "panel.png")
	if _, err := runCLI(t, sampleTree, "-width", "360", "-height", "240", "-o", out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("output is not a PNG: %v", err)
	}
	if cfg.Width != 540 || cfg.Height != 360 {
		t.Fatalf("PNG is %dx%d, want 540x360", cfg.Width, cfg.Height)
	}
}

func TestRunWritesToStandardOutputWithoutO(t *testing.T) {
	out, err := runCLI(t, sampleTree, "-width", "200", "-height", "120", "-scale", "120")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("stdout does not start with the PNG signature: %q", out[:min(8, len(out))])
	}
}

func TestRunReadsATreeFromAFile(t *testing.T) {
	tree := filepath.Join(t.TempDir(), "tree.json")
	if err := os.WriteFile(tree, []byte(sampleTree), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "", "-width", "200", "-height", "120", tree); err != nil {
		t.Fatal(err)
	}
}

func TestRunIsDeterministic(t *testing.T) {
	a, err := runCLI(t, sampleTree, "-width", "300", "-height", "200")
	if err != nil {
		t.Fatal(err)
	}
	b, err := runCLI(t, sampleTree, "-width", "300", "-height", "200")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two runs of one tree differ")
	}
}

func TestRunLeavesNoFileWhenTheRenderFails(t *testing.T) {
	out := filepath.Join(t.TempDir(), "panel.png")
	// A text root is not a valid panel, so the render fails.
	if _, err := runCLI(t, `{"kind":"text","text":"x"}`, "-width", "200", "-height", "100", "-o", out); err == nil {
		t.Fatal("an invalid tree rendered")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("a failed render left %s behind (stat err = %v)", out, err)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	size := []string{"-width", "200", "-height", "100"}
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"no size", sampleTree, nil, "outside 1.."},
		{"not json", "this is not json", size, "tree"},
		{"unknown field", `{"kind":"column","bogus":1}`, size, "tree"},
		{"trailing data", sampleTree + ` {"kind":"column"}`, size, "after the tree"},
		{"two files", sampleTree, append(append([]string{}, size...), "a.json", "b.json"), "at most one"},
		{"missing file", "", append(append([]string{}, size...), "/nonexistent/tree.json"), "no such file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runCLI(t, tc.stdin, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
			if len(out) != 0 {
				t.Errorf("wrote %d bytes of output despite the error", len(out))
			}
		})
	}
}
```

- [ ] **Step 7: Run to verify they fail**

Run: `go test -count=1 -p 2 ./cmd/sysc-panel-preview/ 2>&1 | head -5`
Expected: FAIL to build, `undefined: run`.

- [ ] **Step 8: Implement `main.go`**

Replace the placeholder `cmd/sysc-panel-preview/main.go` with:

```go
// Command sysc-panel-preview paints a plugin panel tree to a PNG the way the
// shell would, for catalog screenshots and documentation images.
//
// It reads a panel's wire tree as JSON (a v1.Node), lays it out and paints it
// with the host's own converter, layout and painter, and writes a PNG. A view
// the host would refuse is refused here with the host's wording and a non-zero
// exit.
//
//	sysc-panel-preview -width 360 -height 480 [-scale 180] [-font family] \
//	    [-o out.png] [tree.json]
//
// With no file argument it reads the tree from standard input; with no -o it
// writes the PNG to standard output. -scale is in 120ths of 100%: 120 is 100%,
// 180 (the default) is 150%.
//
// The picture is a stateless still of an opaque, detached panel on the default
// dark theme, with transparent corners. It has no animation, hover or focus
// state and no running plugin. Text is drawn with the fonts installed on the
// machine that runs it, so the same tree can produce different pixels on
// different machines.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "sysc-panel-preview:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("sysc-panel-preview", flag.ContinueOnError)
	flags.SetOutput(stderr)
	width := flags.Int("width", 0, "panel width in logical pixels")
	height := flags.Int("height", 0, "panel height in logical pixels")
	scale := flags.Int("scale", defaultScale120, "display scale in 120ths (120 = 100%)")
	font := flags.String("font", "", "font family (default: the shell's default)")
	out := flags.String("o", "", "write the PNG here instead of standard output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("at most one tree file")
	}

	in := stdin
	if flags.NArg() == 1 {
		f, err := os.Open(flags.Arg(0))
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	var root v1.Node
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&root); err != nil {
		return fmt.Errorf("tree: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("tree: unexpected data after the tree")
	}

	// Render before creating the output file, so a refused view leaves nothing
	// behind for a caller to mistake for a screenshot.
	img, err := renderPanel(&root, *width, *height, *scale, *font)
	if err != nil {
		return err
	}

	w := stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	return png.Encode(w, img)
}
```

Note `flags.Parse` stops at the first non-flag argument, so the tree file must come after the flags (as in the usage line), and the "two files" test passes the size flags first.

- [ ] **Step 9: Run to verify everything passes**

Run: `gofmt -l cmd; go vet -p 2 ./cmd/sysc-panel-preview/ && go test -race -count=1 -p 2 -v ./cmd/sysc-panel-preview/ 2>&1 | grep -E "^(\s*--- FAIL|--- PASS|ok|FAIL)"`
Expected: 12 `--- PASS` lines (the six `TestRenderPanel*` and six `TestRun*`), then `ok`.

- [ ] **Step 10: Commit the CLI**

```bash
git add cmd/sysc-panel-preview
BEADS_DB="$SCRATCH/beads-shell.db" git commit -m "feat(preview): add the sysc-panel-preview command"
```

---

### Task 2: Documentation, checks and PR

**Files:**
- Modify: `docs/development.md`

- [ ] **Step 1: Document the command**

In `docs/development.md`, add this line to the Layout block, after `cmd/sysc-shell/                executable`:

```text
cmd/sysc-panel-preview/        paints a plugin panel tree (JSON) to a PNG with the host's own layout and painter
```

and append this section at the end of the file:

```markdown
## Panel previews

`cmd/sysc-panel-preview` renders a plugin panel's wire tree to a PNG through the
host's own converter, layout and painter, for catalog screenshots and
documentation. It exists as a command, not an importable package, because the
default theme lives in `internal/shell` and a public package importing it would
drag the shell's whole dependency graph into every consumer.

```sh
go install ./cmd/sysc-panel-preview
sysc-panel-preview -width 360 -height 480 -o panel.png tree.json
```

`-scale` is in 120ths (120 is 100%; the default 180 is 150%). The output is an
opaque panel with transparent rounded corners on the default dark theme, with
no animation, hover or focus state. A tree the host would refuse fails with the
host's message and writes no file. Text uses the fonts installed on the
machine, so output can differ between machines.
```

- [ ] **Step 2: Verify the whole change**

Run, one at a time:

```bash
gofmt -l .
go vet -p 2 ./cmd/sysc-panel-preview/
go test -race -count=1 -p 2 ./cmd/sysc-panel-preview/
go build -p 2 -o "$SCRATCH/sysc-panel-preview" ./cmd/sysc-panel-preview
git diff origin/main --stat -- go.mod go.sum
```

Expected: no gofmt output; vet and tests pass; the build succeeds; the last command prints nothing (no dependency change).

- [ ] **Step 3: Look at a real render**

```bash
cat > "$SCRATCH/tree.json" <<'EOF'
{"kind":"column","padding":16,"gap":8,"children":[
 {"kind":"text","text":"Pomodoro Timer","role":"title"},
 {"kind":"text","text":"Work session 2 of 4"},
 {"kind":"progress","value":0.4,"name":"p","role":"progressbar"}
]}
EOF
"$SCRATCH/sysc-panel-preview" -width 360 -height 240 -o "$SCRATCH/panel.png" "$SCRATCH/tree.json"
file "$SCRATCH/panel.png"
```

Expected: `PNG image data, 540 x 360, 8-bit/color RGBA`. Open it: a dark rounded panel with the title, a line of text and a blue progress bar in the shell's accent.

Also check the refusal path leaves nothing behind:

```bash
echo '{"kind":"column","children":[{"kind":"row","height":28,"padding":8,"children":[{"kind":"icon","icon":"battery_full","icon_size":20}]}]}' > "$SCRATCH/bad.json"
"$SCRATCH/sysc-panel-preview" -width 290 -height 200 -o "$SCRATCH/bad.png" "$SCRATCH/bad.json"; echo "exit=$?"; ls "$SCRATCH/bad.png"
```

Expected: a message containing `does not fit`, `exit=1`, and `ls` reports no such file. (If `icon_size` is the wrong JSON key, the decoder rejects it as an unknown field; look up the key in `plugin/v1/node.go` and use that.)

- [ ] **Step 4: Read the font-failure path**

Open `cmd/sysc-panel-preview/render.go` and confirm `render.NewSystemFontMap` errors are returned as `fonts: ...` rather than ignored (Review Focus item 5). Nothing to run.

- [ ] **Step 5: Commit, rename the branch, push, open the PR**

```bash
git add docs/development.md
BEADS_DB="$SCRATCH/beads-shell.db" git commit -m "docs: describe sysc-panel-preview"
git branch -m feat/plugin-preview feat/sysc-panel-preview
git push -u origin feat/sysc-panel-preview
gh pr create --repo Nomadcxx/sysc-shell --base main --head feat/sysc-panel-preview \
  --title "feat: sysc-panel-preview, render a plugin panel tree to a PNG" --body "..."
```

PR body: why (catalog thumbnails need real panel screenshots; sysc-plugins must not import `internal/shell`, which would add about 35 modules), what (`cmd/sysc-panel-preview`, flags, behaviour), what it is not (stateless still, no host behaviour changed), the verification run, and the link to the spec in sysc-plugins (`docs/superpowers/specs/2026-10-09-plugin-panel-captures-design.md`). End with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`. Do not merge without the user's go-ahead.

---

## Self-Review

**Spec coverage** (component 1 of the spec): reads a tree from file or stdin with unknown fields rejected (Task 1, `TestRunRejectsBadInput`, `TestRunReadsATreeFromAFile`); writes PNG to `-o` or stdout (`TestRunWritesAPNGToTheNamedFile`, `...ToStandardOutputWithoutO`); validates, converts, lays out with real metrics, paints at the scale with the default dark theme and the host's panel style (`renderPanel`); refusals in the host's wording and non-zero exit (`TestRenderPanelRefusesATreeThatCannotFit`, `...AnInvalidTree`, `main()`'s exit); opaque panel with transparent corners (`TestRenderPanelIsOpaqueInsideWithTransparentCorners`); stateless still and font dependence documented in the package comment and `docs/development.md`; output is the requested physical size (`TestRenderPanelHasTheRequestedPhysicalSize`); not blank (`TestRenderPanelPaintsMoreThanAFlatFill`); deterministic on one machine (`TestRunIsDeterministic`); bad flags, bad JSON, unknown fields rejected (`TestRunRejectsBadInput`). Two additions beyond the spec's list, both safety: size and scale caps, and no output file on failure.

**Placeholder scan:** none; every code step has its code. The PR body is described, not quoted, because it depends on what was run.

**Type consistency:** `renderPanel(root *v1.Node, width, height, scale120 int, family string) (*image.NRGBA, error)` and `run(args []string, stdin io.Reader, stdout, stderr io.Writer) error` are used unchanged across Tasks 1 and 2 and the tests; `defaultScale120`, `maxLogical`, `maxScale120` are declared in `render.go` and used in `main.go`.

**Risks the executor should know**
- Output depends on installed fonts; the shell's CI installs `fonts-inter` and Noto, and the tests assert structure only.
- The command imports `internal/shell`, so a future shell refactor must keep it building; `go build ./...` in CI covers it.
- A later sysc-plugins plan consumes this command's flags (`-width`, `-height`, `-scale`, `-o`, JSON on stdin) as its interface; changing them later is a breaking change for that plan.
