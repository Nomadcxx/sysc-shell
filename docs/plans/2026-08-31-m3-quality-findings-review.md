# M3 Quality Findings: Over-engineering vs Reserved

Date: 2026-08-31
Branch: `milestone/power` at `877d378`
For: a second agent reviewing this classification. Do not apply cuts. Do not re-audit from
scratch. Confirm or dispute each row against the cited code and documents, then report.

Sources classified:

- `docs/plans/2026-08-31-m3-code-quality-sweep.md`
- Shell-package ponytail pass
- Services/config/render ponytail pass

Tree: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/power`

## What the reviewing agent should do

1. Read this file, then the two sweep reports, then the cited design paragraphs.
2. For every row: confirm the bucket, or move it and say why (file:line).
3. Do not delete M4-reserved machinery, specified test-observability APIs, or recorded
   charter deviations.
4. Do not treat the live Niri matrix as a finding.
5. Return a short verdict: classification holds, or a list of rows to re-bucket.

## The split

The agent passes proposed **−150 to −400 lines**. Most of that count is not M3 bloat.

| Bucket | Share of the proposed cuts | What it is |
|---|---|---|
| **Reserved for M4** | The largest claimed wins | Inert on purpose. 3A and 4A both say to keep it. |
| **Specified M3 API** | The lifecycle counters, selector history, `RequestURL` | Named in designs. Tests (and some production) depend on them. |
| **Real dead / thin wrappers** | A small set, ~15–35 lines if you take the cheap ones | No later milestone consumes them. Safe to cut. |
| **Optional shrink** | Duplicated loops, aliases, one-liners | Same behaviour, fewer lines. Not unused-for-later, not required. |
| **Reserved for M5** | Essentially none of the unused M3 code | 5A/5B add new surfaces and a tray widget. They do not need today's dead Bar channel, `Metrics.Leased`, or the unused `hover` field. |

Headline: **the big items the ponytail passes wanted to kill are M4 scaffolding or specified M3 APIs, not leftover speculative code.** M5 is not sitting on a pile of unused M3 types. Real over-engineering in the M3 widget code is a handful of dead helpers and thin wrappers.

## Bucket A — reserved for Milestone 4 (do not cut)

4A's ordering constraint, `docs/plans/2026-08-30-panel-foundation-design.md`:

> 4A consumes Tranche 3A outputs specifically … and the retained `ui.Handle` press/release matching
> and hit testing that 3A kept inert "because Milestone 4 needs it".

3A's judgment call, `docs/plans/2026-08-30-built-in-widget-foundation-design.md`:

> `Handle`'s press/release matching and hit testing are **kept**. … Milestone 4 needs it. With no
> node carrying an `Action` it is inert at runtime while remaining under test through a synthetic
> action node. Deleting and later restoring it would be more total churn than leaving it.

4A Task 7: "button (KindButton exists)".

| Finding | Why it is reserved |
|---|---|
| `KindButton`, `Node.Action`, `ui.Hit` | 4A session/power buttons. 3A removed fixture widgets and kept this path. |
| `Handle` press/release, `hitLocked`, `activateLocked` returning false | The comment in `bar.go` names Milestone 4. Tests cover it with a synthetic action. |
| `ProofStyle.Toggled` / `AccentOn` / `accent()` | Button paint. 4A D9 replaces `ProofStyle` with `Theme` *when 4A lands*, not now. |
| `copyNode` walking `Children` | 3A widgets are leaves. 4A panel trees are not. |
| `supportedEdges` with `bottom`/`left`/`right` = false | Unimplemented edges fail with a named error. 4A's panel plan still mentions `top \| bottom`. Not a dead map. |

`KindMeter` / `KindGraph` have **M3 consumers**. They are not unused. 4A D10 defers a *graphs control* in the system-monitor popout; that is a different type. Do not delete the 3B graph node.

## Bucket B — specified M3 API (do not cut)

| Finding | Why it stays |
|---|---|
| `Clock`/`Metrics`/`Weather` `Starts()` and `Running()` | 3A design marks them `// test observability`. Evidence tables in 3A/3B/3D require `Starts()==1` across reload. |
| `Registry.Clock` / `Metrics` / `Weather` | Production `cmd/sysc-shell/main.go` reads `Updates()` through them. |
| `Weather.RequestURL` | 3D handover: exported so package `shell` reload tests can see the URL. |
| History ring allocated on every metrics `Acquire` | 3B D6: one ring per selector, owned by the service. 3C plan finding 4: battery still gets a ring; `record` skips it. Do not special-case display mode. |
| `SourceLeased` | Production `collect` uses it. Unexporting is optional; deleting is not. |
| `leaseSet` as a struct, three concrete services | 3B/3D stop condition: no service interface. |
| Icon font, `Node.Tone` | Recorded charter deviations. |

## Bucket C — real dead or thin (no later consumer)

These are unused because **nothing in M3, M4, or M5 needs them**, not because a later tranche has not been written yet.

| # | Finding | Replacement | Path |
|---|---|---|---|
| 1 | `Metrics.Leased` — zero callers after D6 introduced `SourceLeased` | Delete | `internal/services/metrics.go` |
| 2 | `Bar.invalidations`, `invalidate()`, `(*Bar).Invalidations` — `invalidate` has no callers. Registry owns redraws. 4A consumes `Registry.Invalidations()`, not the bar channel. | Delete field and `drain` test helper | `internal/shell/bar.go` |
| 3 | `hover ui.Rect` on `Bar` — never read or written. `hoverAt` is the live cursor. | Delete the field | `internal/shell/bar.go` |
| 4 | Unlocked `tooltipAt` — only `tooltipAtLocked` is used | Delete the wrapper | `internal/shell/bar.go` |
| 5 | `historyLocked` — one line around `r.metrics.Histories()` | Inline in `viewLocked` | `internal/shell/registry.go` |
| 6 | `Metrics.History` — tests only; production uses `Histories` | Unexport; tests keep calling it in package `services` | `internal/services/metrics.go` |
| 7 | `metricFraction` / `metricValue` — `metricSelector` plus `snap.Fraction`/`Value` | Inline at the two format closures | `internal/shell/metricwidget.go` |

Cheap apply list if the owner wants deletions: **1, 2, 3, 4, 5, 6**. Item 7 is optional extra test churn.

`New(*Bar)` vs `NewWithTheme`: tests and production already use `NewWithTheme`. `New` is a convenience, not M4. Deleting it is a small cut, not a milestone reservation.

## Bucket D — optional shrink (duplication, not reserved)

Same behaviour, fewer lines. A later milestone does not "need" the long form.

- Four `Update*` bodies in `registry.go` → one apply-after-mutate helper.
- Three pumps in `main.go` → one `select` (types differ; a generic is optional).
- `send` / `sendSnapshot` / `sendReading` → `sendLatest[T]`.
- `Theme.Geometry()` vs `OutputHost.surfaceHeight()` — the same `Gap+(Height-2*Gap)` arithmetic in two packages. Unify; 4A placement still needs *one* formula, not both.
- `hideTooltip` multi-error formatting → `errors.Join` (already used in wayland).
- Tooltip paint uses a second hardcoded `ProofStyle` instead of `o.cfg.Theme` — 3D handover already notes the lazy font map. Quality, not M5.
- `leaseSet.len()` → `len(s.leases)`.
- `type Color = render.Color` in `theme.go`.
- `dwell.requests()` if tests read `d.out`.
- `metricMeterWidth` and `metricGraphWidth` both 48 — coincidence; do not merge just to merge.
- Weather `conditionWords` keyed through `IconRune` — style.
- `paintGraph` re-clamps after `normalise` — defensive, not unused.
- `Bar.Radius` mirrored from `Theme.Radius` — config schema. 4A regions still need a radius token; collapsing now is a schema change, not a dead-field delete.
- `textWidget.tooltip` as a general field — only weather sets it. Harmless. Future bar tooltips can keep using it; not M4-required today.

## Bucket E — Milestone 5 (almost empty)

5A: toast Overlay surfaces, exclusive_zone −1, keyboard none, M4 panel id `notifications`.
5B: tray bar widget, xdg_popup menus.

None of that is implemented as unused types in the M3 tree. The tooltip's OSD shape (Overlay, zone −1, keyboard none) is the **shared pattern 4A/5A already adopted**, and it is used. Do not strip tooltip machinery as "waiting for M5".

Do not keep `Metrics.Leased`, `Bar.invalidations`, or `hover ui.Rect` on an M5 rationale. They have no 5A/5B consumer in the designs.

## Charter deviations (still not findings)

- Icons as a font, not rasters (3D D4).
- `Node.Tone` instead of extra node kinds (3D D5).
- Graph node shipped in 3B (3B D5).
- Battery as a `Source` on the 3B sampler (3C D1).

## Suggested verdict for the reviewer to confirm

1. The ponytail line-count is inflated by Bucket A and Bucket B.
2. Real over-engineering is Bucket C, roughly **−15 lines** for items 1–6, **−35** if item 7 is included.
3. M4 reserved code stays until 4A lands and replaces it.
4. M5 does not justify keeping the dead M3 leftovers.

If this classification holds, the receiving agent should say so in one paragraph and stop. If a row is in the wrong bucket, name the row, the new bucket, and the evidence.
