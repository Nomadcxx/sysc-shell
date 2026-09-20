# Declarative Value Animation Design — `animate` on progress and gauge

Date: 2026-09-21
Status: Owner-approved 2026-09-21
Branch: docs-only on `main` (implementation later, after `feature/wire-minor-5`).
Commissioned by: the KDE Connect modern-panel infrastructure plan
(`~/.commandcode/plans/kdeconnect-modern-panel-infrastructure.md`, Design C).

Supersedes nothing. Adds one wire flag and one animator channel; no new rendering.

## Scope

Plugins declare *targets*; the host interpolates. A `progress` or `gauge` node flagged
`animate` glides to its new value instead of jumping, with no per-frame plugin traffic — the
plugin publishes a revision when the value changes, the host animates between revisions.

Enables: the charging fill (sysc-447), smooth battery/progress moves in panels, and the
modern feel the DMS reference gets for free from QML.

## Verified ground truth (tree at `348fd12` + in-flight minor-4 work)

- The animator is per-surface, keyed by `animKey{node, channel}` (`shell/animation.go:72-76`);
  `Target(node, channel, to)` retargets from wherever the value currently is (`:211-227`);
  `duration(channel, rising)` and `easeFor(channel)` are per-channel tables (`:164-186`);
  `TargetLoop` runs the marquee's linear sweep (`:230`).
- The declarative resolve-walk precedent is `resolveMediaMotionLocked` (`shell/bar.go:609-643`):
  walk the tree, `TargetSweep(key, animSweep, trip)`, write the interpolated
  `anim.Value(key, animSweep)` back into the node (`n.TextOffset`), retire unseen keys. Frames
  are scheduled by `animateSurface` (`:378-390`) until `settled()`; panels start frames from
  `rebuildPanel` → `startSurfaceFrames` (`panelhost.go:2145`).
- Reduced motion is wired at animator construction (`newAnimator(nil, cfg.Accessibility.ReducedMotion, theme.Motion)`, `panelhost.go:644`, `osd.go:114`) and the marquee walk checks `b.anim.reduced` directly (`bar.go:622`).
- `ui.Animated` (`ui/animkey.go:8-19`) tracks only button/segmented/capsule; `ValidateKeys`
  (`:23-65`) requires a distinct non-empty stable key on every tracked node.
- Wire: `v1.Node.Key` exists with uniqueness validation (`plugin/v1/node.go:143, 376-380`);
  there is no `animate` flag. `progress` converts to `ui.KindMeter`, `gauge` to
  `ui.KindRadialGauge` (`internal/plugin/view.go:345-349`).

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| D1 | `animate` is a new optional `Node` field, legal only on `progress` and `gauge`, and requires a non-empty `Key` when set. | Animating every progress node implicitly. Opt-in keeps the animator out of trees that do not ask, and the key requirement reuses `ValidateKeys`' existing contract. |
| D2 | The flag rides **protocol minor 6**. | Folding it into minor 5. Design A is committed and owner-approved; each tranche that adds wire fields bumps the minor honestly under the strict-decode lockstep (Design A D10). Minors are cheap; re-opening an approved design is not. |
| D3 | One new channel `animProgress` (0→1 value, the theme's standard ease and duration). | A per-node duration/ease field. No consumer has asked to tune motion; the theme owns motion. |
| D4 | The resolve walk is one shared function (shell side), invoked from the bar's motion pass and from `rebuildPanel`, mirroring the marquee walk: `Target(key, animProgress, n.Value)` then `n.Value = anim.Value(key, animProgress)`, retiring keys whose nodes vanished. | A paint-time read of the animator. Paint must stay a pure function of the tree; the write-back happens in the resolve pass the marquee already established. |
| D5 | `ui.Animated` returns true for a progress/gauge carrying `Animate`, so `ValidateKeys` enforces the key rule host-side too. | A separate validation path. One gate, one error shape. |
| D6 | Reduced motion collapses the animation to the target immediately (the existing `reduced` behaviour — the marquee walk already drops to static under it). | A config knob per surface. The accessibility preference already exists and is already honoured. |
| D7 | The converter passes `Animate` and `Key` through to `ui.Node` (`Key` already maps for text inputs); no new ui fields. | A host-side animation descriptor struct. The flag is one bool; the key is already there. |

## Wire addition (`plugin/v1/node.go`, minor 6)

```go
// Animate asks the host to glide this node's Value to each new revision's
// target instead of jumping. Progress and gauge only; requires Key.
Animate bool `json:"animate,omitempty"`
```

Validator (`minorSix`, beside `minorFour`): `Animate` on any other kind rejects; `Animate`
without `Key` rejects. Doc comment records what minor six carried.

## Host flow

1. Converter: `out.Animate = n.Animate` (new `ui.Node` field, one bool) for meter/radial
   gauge; `Key` mapping already exists.
2. `ui.Animated`: `case KindMeter, KindRadialGauge: return n.Animate`.
3. Shared resolve walk (shell): for each `KindMeter`/`KindRadialGauge` with `Animate` and a
   key — `anim.Target(key, animProgress, n.Value)`; `n.Value = anim.Value(key, animProgress)`;
   collect seen keys; retire the rest (the `resolveMediaMotionLocked` shape, `bar.go:636-643`).
   Under reduced motion, write the target directly and skip the channel.
4. Invocation: the bar's motion pass and `rebuildPanel` (which already starts frames via
   `startSurfaceFrames`). `animateSurface` keeps scheduling until the channel settles; the
   final frame writes the exact target.
5. `duration()` gains the `animProgress` case (theme motion duration, same rising/falling
   treatment as other channels).

## Files

| File | Change |
|---|---|
| `plugin/v1/node.go` | `Animate` field, `minorSix` validator, doc comment |
| `plugin/v1/node_test.go` | Accept/reject matrix (kind gate, key requirement) |
| `internal/plugin/view.go` | Pass `Animate` through for meter/gauge |
| `internal/plugin/view_test.go` | Converter mapping test |
| `internal/ui/tree.go` | `Animate` field on `ui.Node` |
| `internal/ui/animkey.go` | `Animated` tracks animated meter/gauge |
| `internal/ui/animkey_test.go` | Key-validation test for the new tracked set |
| `internal/shell/animation.go` | `animProgress` channel, `duration` case |
| `internal/shell/animation_test.go` | Target/retarget/settle for the new channel |
| `internal/shell/bar.go` + panel resolve path | Invoke the shared walk |
| `internal/shell/*_test.go` | Walk test: value glides across frames, retarget from mid-flight, reduced-motion jumps, stale keys retire |

## Automated evidence

- Validator: `animate` on progress with key accepts; on row rejects; on progress without key
  rejects; duplicate keys still reject through `ValidateKeys`.
- Animator: `Target` twice mid-flight continues from the current value; `settled` reports
  true only at the target; reduced motion writes the target at once.
- Walk: a panel view whose progress goes 0.2 → 0.8 publishes intermediate values across
  frames and lands exactly on 0.8; removing the node retires its key.
- Full gates per commit: `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`,
  `go test -race -count=1 ./...`, clean `git diff -- go.mod go.sum`.

## Dependencies and assumptions

- Depends on nothing from Designs A/B at the code level (separate minor, separate fields);
  sequenced after A only because the gap-fill consumes them in that order.
- Assumes value semantics stay 0→1 for progress and gauge (validated today); a unit change
  would amend D3's channel semantics, not the structure.
- The commissioning plan's claims — one new channel, the marquee/gradient precedent,
  `animateSurface` scheduling, reduced-motion collapse — verified as written, with
  `ui/animkey.go` and `shell/animation.go` named as the touch points the plan's correction
  added.
