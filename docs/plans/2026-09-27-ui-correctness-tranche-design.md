# UI Correctness Tranche — Design

Tracking: `sysc-591` (UI parity slice 2). Source: `reports/sysc-shell UI design gap analysis.md`,
critical gaps 1 and 2 plus `sysc-171`. Owner-approved 2026-09-27.

Three defects, not features: SVG icons never resolve (critical gap 1, `sysc-173`, blocks `sysc-117`
and `sysc-186`), template apply overwrites files the user edited (critical gap 2, `sysc-405`, P1),
and held keys do not repeat in text fields (`sysc-171`). Each has its own owner, its own fix, and
its own failure-mode ruling below.

## D1. SVG rasterisation (`sysc-173`)

**Decision.** Pin `github.com/srwiley/oksvg` with `github.com/srwiley/rasterx` — pure Go, no CGO,
which keeps the AGENTS narrow-C-boundary rule untriggered. Decode SVG to the existing
`ui.Image` (premultiplied BGRA) at the requested logical size, in the existing
`internal/icons/worker.go` off-loop pipeline, cached under (absolute path, size) like rasters.

**Resolution order.** Exact-size PNG beats SVG; SVG beats a nearest-size raster; a failing SVG
(parse or unsupported feature) falls through to the raster chain and logs once per path. Icons in
the wild are path-and-fill documents; oksvg's gaps (filters, embedded text, external refs) mostly
do not apply, and where they do the fallback makes the miss invisible to the user.

**Touch points.** `internal/icons/theme.go`: add `.svg` to the scan set (`rasterExtensions` today
lists `.png`, `.xpm`) as its own tier, not folded into "decodable". `worker.go`: rasterize step.
No protocol, theme-config or settings change; the icon-theme spec already says SVG-first and the
resolver simply obeyed the decoder's limits.
(Shipped note: `.svg` is also in the decodable-extension set so an absolute `.svg` path — a
FileResolver or notification `image-path` — decodes; the theme-scan tier order above is unchanged.)

**Check.** Golden-pixel assertions on a small embedded glyph (circle plus two-tone path) at 24 and
48 px: coverage, alpha, colour; plus a resolver-order table test with a fixture theme dir holding
competing png/svg/xpm files.

## D2. Template apply safety (`sysc-405`)

**Defect.** `internal/theming.ApplyWrite` is `os.WriteFile`: a template applied over a config the
user edited silently destroys their work.

**Decision — adopt-if-untouched.** Per applied path, remember the sha256 of the bytes the shell
last rendered. On apply: if the target is absent, empty, or its current bytes hash equal the
remembered value, render → write a sibling temp → `os.Rename` swap, atomically, and update the
state record under `XDG_STATE_HOME/sysc/`. Anything else — the user edited it, or the shell never
wrote it — refuses with a "user-modified" outcome the control-centre/settings surface reports,
and offers an explicit overwrite action that backs the previous file up to `<path>.bak` first.
The state file is a cache, never an authority: losing it degrades to refusing, not clobbering.
(Shipped note: the record lives under `XDG_STATE_HOME/sysc-shell/templates/state.json`, the
repo-wide state-root convention, not `sysc/`.)

**Why not managed-block markers.** Marker regions need per-format comment syntax (TOML, ini, CSS,
JSON do not agree), and a half-owned file still lets a user edit inside the block. Hash-adopt is
format-agnostic, is smaller, and refuses exactly the dangerous case. Revisit per-format blocks if
a real template needs to coexist with heavy user edits in its own region.

**Check.** Table tests over target states (absent / empty / untouched / edited / no-state-file)
asserting write, refuse, backup, and swap behaviour in a temp dir; no live Niri gate beyond the
existing settings smoke.

## D3. Key repeat in text fields (`sysc-171`)

**Existing mechanism.** Client-side repeat already lives in
`internal/platform/wayland/keyboard.go`: driven by `repeat_info`, timed on the Wayland loop's
poll deadline (no per-key timers), evdev modifier codes excluded from repeating. The fix belongs
there or at its delivery seam — not in `ui/textfield.go`, which should stay a passive consumer.

**Decision.** Task 1 is a characterisation test that reproduces the reported symptom exactly (held
key, focused text field, repeats stop or never start) before touching anything. The expected root
cause class is retargeting: repeat must follow the currently-held key across focus change, key
release, and layout/group switches, and a release must cancel the pending deadline. Whichever of
those the test pins, the repair stays inside the existing `keyRepeat` state machine; no new
repeat machinery is added at shell level.

**Check.** A keyboard-simulation regression test in `internal/platform/wayland` driving press,
deadline fire, release, and a focus switch; it is the one runnable check and it fails today.

## Sequencing

D2 is the P1 data-loss defect and ships first. D1 and D3 are independent of each other and of the
other slices. Nothing here gates or is gated by `sysc-590`/`sysc-592`, except that media OSD icons
in slice 3 read better once D1 lands.
