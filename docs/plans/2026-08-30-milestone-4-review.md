# Milestone 4 Design and Plan Review

Date: 2026-08-30
Reviewer: independent review of the Tranche 4A and 4B designs and plans.
Reviewed at: `milestone/panels-controls` @ `08d88d5`, against the roadmap, the project design, and the
Tranche 3A charter's recorded decisions.

No document under review was edited.

## Verdict

**Proceed after the two blocking findings are resolved.** The engineering is sound: the panel surface
model, placement maths, keyboard approach, and gate tests are all better than adequate, and the risky
assumptions are tested live rather than asserted. The blockers are a dependency-policy violation and a
cross-tranche interaction that neither document notices because each is correct in isolation.

## Blocking

### B1 — 4A imports `sysc-metrics` through a local `replace`

`panel-foundation.md` Task 5 runs:

```
go mod edit -require=github.com/Nomadcxx/sysc-metrics@v0.0.0
go mod edit -replace=github.com/Nomadcxx/sysc-metrics=/home/nomadx/sysc-metrics
```

This is a direct hit on a recorded stop condition. The Tranche 3A charter states: *"Do not add a local
`replace`, copy its code into the shell, or import an untagged commit,"* and lists *"imports untagged
`sysc-metrics` code or adds a local module replacement"* as a stop-and-return condition. The qualification
sequence it mandates — audit the public API, qualify from one proposed consumer, tag and push, pin the tag
— is tracked as `sysc-7` and has not run. `sysc-metrics` still has zero tags.

There is also an ordering problem underneath it. CPU, memory, filesystem, block and network consumption is
**Tranche 3B** work in the roadmap. 4A's system-monitor popout would make Milestone 4 the first consumer
of `sysc-metrics`, ahead of the Milestone 3 tranche that owns it and ahead of its qualification gate.

**Recommended fix, which costs almost nothing.** The Milestone 4 exit gate reads *"clock/calendar **or**
system-monitor popout works on each output."* 4A passes on clock/calendar alone. Deferring system-monitor
until after 3B removes the dependency, the `replace`, and `MetricsService` (Task 5) entirely.

By the design's own D7 rule — controls enter only with a consumer — `tabs` and `graphs` then lose their
only 4A consumer and defer with it. 4A shrinks to panel machinery, placement, rounding and shadows,
button/label/separator, theming, clock/calendar and session/power, and IPC. That still passes the gate.

The alternative is to gate 4A on `sysc-7` and keep system-monitor. Either is defensible; shipping the
`replace` is not.

### B2 — a config reload closes the settings panel that triggered it

Each tranche is self-consistent; together they are not.

- 4A Task 7b: *"`reloadConfig` closes every aux surface before committing new bars (documented ceiling:
  open panels do not survive a config reload; the user reopens them)."*
- 4B Task 8: *"Settings apply path: entry.Set mutates a copy of the live config → `Write` → trigger the
  existing reload channel … settings-triggered reload reuses SIGHUP's reload path verbatim — one reload
  mechanism, not two."*

The settings modal is an aux surface. So every change made in settings writes the config, triggers a
reload, and the reload destroys the settings panel. The user would be ejected from the settings UI on
every toggle, slider drag, and dropdown selection. A visible OSD dies the same way.

Neither document is wrong on its own; the interaction is unhandled. Options, cheapest first:

1. Exempt open aux surfaces from reload teardown: rebuild bars, leave panels mapped, and re-resolve their
   theme and content in place. Panels already rebuild content per render, so this is mostly deleting the
   teardown call.
2. Reopen the settings panel after a settings-triggered reload — restores state but flickers, and loses
   scroll position and focus.
3. Have settings defer the reload until it closes — breaks the design's own live-preview promise.

Option 1 is the one that matches what both documents say they want.

## Significant

### S1 — generated tokens silently orphan the existing theme configuration

4A Task 4 replaces `ThemeFrom(cfg, bar)` with `ThemeFromTokens(tok, radius)`. The existing
`config.Theme` fields — `background`, `foreground`, `accent`, `muted`, `error` — keep their place in the
schema and their validation, and stop having any effect. A user who edits `theme.background` sees nothing
change and gets no error.

Silent no-op configuration is a bad failure mode, and the project's own rule is that an invalid or
misspelled entry fails the whole candidate rather than being ignored. Decide explicitly: remove the colour
fields from the schema (there is no compatibility promise), or define them as explicit overrides applied
on top of generated tokens. Either is fine; leaving them inert is not.

### S2 — a second configuration validation entry point

4A Task 2 adds `Config.Valid()` and tests through it, *"(or add it if M3 has not)"*. Milestone 2 and 3
have no such method: validation happens inside `Parse` via `applyBar`/`validateBar`/`resolveItem`, and
every failure names its exact field path — `config: bar.items.left[1]: "nope" is not a known item`.

Two validation surfaces will diverge, and the one being added returns errors with no field path. Extend
the existing `Parse`-time validation instead, and keep the field-path convention.

### S3 — the template catalog has no removal path

4B Task 13 appends `include "sysc-shell.kdl"` to the user's live `~/.config/niri/config.kdl`. The apply
side is careful — idempotent injection, never overwrite foreign content, gtk theme only when unset or
ours — but there is no disable path. Turning the per-template toggle off, or uninstalling the shell,
leaves the include line behind pointing at a file that may no longer exist, and niri then fails to load
its configuration.

An edit to the user's compositor configuration must be reversible by the same toggle that made it.
Disabling a template should remove its generated file and, for niri, its include line — the same
ours-only line management the apply path already implements.

### S4 — four new external binary dependencies

matugen and `loginctl` in 4A; `wpctl` and `brightnessctl` in 4B. The shell currently has none: it is
pure Go over a pinned Wayland client. Each has a clean fallback (stock palette, hidden actions, service
reported unavailable), and each is justified against the heavier alternative — a D-Bus client dependency,
the PipeWire wire protocol, CGO. That reasoning is good.

This is still a policy call the owner should make explicitly rather than inherit through a task. The
dependency ladder in the project design ends at "new code"; it does not cover "exec a binary the user may
not have". Record the decision wherever the dependency policy lives.

### S5 — "Milestone 3 has merged" is ambiguous, and B1 depends on which is meant

4A's prerequisites require *"Milestone 3 (widget foundation) has merged to main."* Milestone 3 is four
tranches. If this means 3A only, B1 stands as written. If it means all of Milestone 3, then 3B has
shipped, `sysc-metrics` is tagged and pinned, and the `replace` is unnecessary rather than merely
disallowed. State which.

### S6 — 4B is larger than a tranche

4B carries six controls, the settings modal, a schema registry, atomic persistence, two polling services,
OSD surfaces, ten stock themes, sixteen app templates with three kinds of apply hook, and a cross-repository
`sysc-wayland` protocol release — in fourteen tasks.

For calibration: Tranche 3A ships four read-only text widgets and one service in sixteen tasks. 4B is
several times that scope at a third the granularity. The likely outcome is tasks that cannot be reviewed
independently and a tranche that cannot be landed incrementally.

Splitting along the seams the design already names would give three reviewable pieces: controls and the
settings modal; OSD with its two services; themes and the template catalog. The `sysc-wayland` release
that the text field needs is a prerequisite of the first, not of all three.

## Minor

- `reg.Tokens() == theme.Tokens{}` in 4A Task 4 does not compile — a composite literal in an `if`
  condition needs parentheses: `reg.Tokens() == (theme.Tokens{})`.
- 4A Task 6 contains an unresolved *"Wait — clock and calendar are one popout"* self-correction mid-task.
  The conclusion is right; the deliberation should not ship.
- 4A Task 2 hedges *"name the field `ThemeGen` if `Theme` collides"*. It does collide — `config.Theme`
  exists today. State the name.

## What the review confirmed as sound

Recorded so these are not re-litigated.

- **Keyboard without xkbcommon.** Ignoring the keymap and handling only layout-independent evdev codes
  (Escape, Tab, arrows, Enter, Space) avoids a CGO dependency for exactly the keys 4A needs, with the
  ceiling named and 4B's text input arriving through text-input-v3 preedit instead. Modifier state comes
  from `wl_keyboard.modifiers` and needs no keymap, so Shift+Tab works.
- **The Milestone 2 modification is scoped and staged.** Task 7 splits into extract-`surfaceUnit`,
  auxiliary surfaces, and keyboard, with the full existing test suite green after each, and it keeps the
  single-Wayland-goroutine invariant: `AuxRequest` is consumed in the owner's select loop exactly as
  `Invalidations` is.
- **Placement maths.** Clamp cases including the oversized-panel case, and margins computed against the
  bar's reserved zone with `exclusive_zone -1`, are correct.
- **Gate tests test the gate.** Placement under 1.0/1.5/2.0 scale and 90/180 transforms, accessible name
  and role on every focusable node, keyboard-only operation, exclusive zone unchanged across open/close.
- **The live checklist tests the assumptions rather than asserting them** — focus fall-through, shield
  pointer delivery, exclusive-versus-windows, compositor keybinds surviving, and Overlay-versus-fullscreen
  are all verification steps with a named contingency, not claims.
- **Template apply safety.** Idempotent include injection, refusing to overwrite foreign content, and
  switching the gtk theme only when unset or already ours.

## One finding about the reviewing side

4A's prerequisites list names `Registry.Invalidations() <-chan wayland.Invalidation` as Milestone 3
surface it builds on. The Tranche 3A plan had deleted that accessor without replacement while
`cmd/sysc-shell/main.go` still passed it into `wayland.Callbacks`, and nothing published the changed
globals onto any transport — so no bar would have repainted. Fixed in `9644f9c` before this review
continued. The Milestone 4 prerequisites list found a defect that the Tranche 3A audit did not.
