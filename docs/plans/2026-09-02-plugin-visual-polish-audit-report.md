# Plugin visual polish — Hallmark audit

/* Hallmark · pre-emit critique: P4 H5 E4 S5 R4 V3 */

`hallmark audit` of sysc-shell plugin surfaces on `milestone/plugin-host` at
`eb27ea2`. Genre: utilitarian shell chrome, not a landing page. Landing-page
tells (purple hero, Inter-everywhere, AI nav/footer) do not apply. The named
tells below are the ones that survive that mapping.

**Do not edit.** Notes, Weather, and Screen Recorder were not shipped; they are
out of this audit.

Target files:

- `plugins/reference/timer/view.go`
- `plugins/reference/worldclock/view.go`
- `internal/shell/pluginwidget.go`
- `internal/shell/popout_plugins.go`
- `internal/shell/pluginhost.go` (`pluginPanelError`)
- `internal/render/paint.go` (button / drag / drop paint)
- `internal/plugin/view.go` (`convertNode`)

No `design.md`. No Hallmark stamp. Stamp-vs-page check: n/a.

---

## Critical

**Tell:** Default-attractor sameness + structural fingerprint (equal-weight
control row — the widget equivalent of the 3-column feature grid).

**Where:** `plugins/reference/timer/view.go` 5–35; `plugins/reference/worldclock/view.go`
9–80; `internal/shell/popout_plugins.go` 11–93; `internal/shell/pluginhost.go`
596–608.

**Severity:** critical.

**Fix:** One job per surface. Bar = readout that opens the panel. Panel = one
display object + one action row. Manager = name + enable. Stop composing every
plugin as `KindColumn, Gap: 8` of peer controls.

**Tell:** Generic emoji as feature icon (ASCII standing in for the symbol
catalogue).

**Where:** `internal/shell/pluginwidget.go` 99–109 (`Text: "!"`);
`plugins/reference/worldclock/view.go` 40 (`Text: "="`).

**Severity:** critical.

**Fix:** Failed slot shows the plugin name at `ToneError` inside the existing
capsule. Grip is a `KindDragSource` the host paints as a handle, not `"="`.

---

## Major

**Tell:** Wrap-to-two-lines clickable text.

**Where:** `plugins/reference/timer/view.go` 12–18 (countdown + Start/Pause +
Reset inside one bar capsule).

**Severity:** major.

**Fix:** Bar shows remaining time only. Start/Pause/Reset live on the panel.

**Tell:** Mid-render token improvisation (every interactive is the accent fill).

**Where:** `internal/render/paint.go` 142–143; consumed by Timer buttons, World
Clock Remove, drag grips, manager Retry, panel-error Close/Retry/Disable.

**Severity:** major.

**Fix:** Map from existing semantic `Tone` and kind: `KindButton` + normal =
primary (`FillAccent`); `KindButton` + `ToneError` = destructive; `KindDragSource`
= ghost. Do not invent hex.

**Tell:** Hover-only affordances.

**Where:** `plugins/reference/worldclock/view.go` 51–59 (drop zones as extra
children inside the zone row); `internal/render/paint.go` 182–190 (`KindDropZone`
paints nothing).

**Severity:** major.

**Fix:** Inter-row gaps (`Height` 3) that thicken only while a matching drag is
active. Not invisible siblings in the row.

**Tell:** Confirmation dialogs for reversible actions.

**Where:** `plugins/reference/worldclock/view.go` 68–73 (footer “Remove X?” /
Confirm / Cancel).

**Severity:** major.

**Fix:** Tranche 1 (prior art) = inline confirm in the row. Tranche 2 = optimistic
remove + Undo if notifications can carry an action; otherwise keep inline confirm.
No modal.

**Tell:** Mismatched icon sets.

**Where:** ASCII `"!"` / `"="` next to an unused `KindIcon` path whose catalogue
is weather + battery (`internal/render/iconfont.go`).

**Severity:** major.

**Fix:** One catalogue. Expand PUA only when a shipped plugin needs a named
glyph. Until then, host-drawn handle and named text, not mixed ASCII.

**Tell:** Missing hierarchy (flat type; the shell analogue of “centred everything”).

**Where:** `plugins/reference/timer/view.go` 27–34; World Clock row at one weight
for zone, time, offset, and Remove.

**Severity:** major.

**Fix:** Host maps `Tabular` countdown text in a padded panel row to bold +
primary colour. Muted labels wait on a v1.1 `Tone` if v1 cannot express them.
Do not add `fontSize` to the wire.

---

## Minor

**Tell:** Variety drift.

**Where:** Timer panel and World Clock panel are the same `KindColumn, Gap: 8`
list.

**Severity:** minor.

**Fix:** After prior-art IA lands, Timer is an instrument card and World Clock is
a stacked place-list. One token set, two voices.

**Tell:** Generic copy (manager meta dump).

**Where:** `internal/shell/popout_plugins.go` 51–64.

**Severity:** minor.

**Fix:** Name on its own line; version and source muted; stderr only when failed.

---

2 critical · 6 major · 2 minor
