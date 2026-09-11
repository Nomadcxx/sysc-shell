# Token conformance — Design

Date: 2026-09-11. Status lives in bd.

Approach owner-approved 2026-09-11 in brainstorming: a test that fails on
literals, with an allowlist for genuinely measured constants. The decisions
below are pending owner review.

Third document of the parity tranche, after the backdrop blur design and the
Noctalia v4 parity design. It answers a narrow question: why did two approved
designs — the chrome catalogue and theme system parity, both landed 2026-09-02
— fail to stop first-party surfaces re-deriving their own chrome, and what
makes the next one fail instead of accumulate.

The honest answer is that enforcement is **not** absent. It is partial, and its
gap is specific.

Sources (read, not imported):

- `internal/shell/surfacerole_test.go:212` — `TestSurfaceSourcesCarryNoLegacyVisuals`,
  the existing source-scan gate, and the comment at `:198` stating why it scans
  source rather than walking trees
- `internal/shell/surfacerole_test.go:42`, `:54` — `walkNodes`, `cardsOf`
- `internal/shell/surfacerole_test.go:90`, `:126`, `:184` — the behavioural
  role and density checks
- `internal/shell/chromefit_test.go` — conformance by construction: every panel
  laid out at its narrowest supported width
- `internal/ui/tree.go` — `Node`, 58 fields
- `docs/plans/2026-09-11-noctalia-parity-design.md` — D1, the spacing re-base
  this design exists to make safe
- `docs/plans/2026-09-02-chrome-catalogue-design.md` — D5, the parity boundary

## What already exists

| Gate | Covers | Blind spot |
|---|---|---|
| `TestSurfaceSourcesCarryNoLegacyVisuals` | Scans every non-test source in `internal/shell`; bans the legacy flat aliases (`BarPadding`, `Spacing`, `CapsulePadding`, `CardRadius`, …) and `Bold: true` | Says nothing about a bare number |
| `TestSurfaceCardPaddingFollowsDensity` | Card padding matches the density row, at both ends of the table | Only `PanelMonitor` and `PanelSession`; finds cards via `cardsOf`, so a panel that builds none is unchecked |
| `TestSurfaceCardTitlesAreTitleRole`, `TestSurfaceHeadingsCarryARole` | Semantic text roles | Same two panels |
| `chromefit_test.go` | Every panel lays out at its narrowest width | Proves it *fits*, not that it used a token |

The project also already settled the mechanism argument, at
`surfacerole_test.go:198`: a runtime walk "only reaches the trees a test happens
to populate — a launcher row exists only when there are results, a notification
card only when there is a notification — so the surfaces most likely to keep a
legacy visual are the ones a walk misses. This reads the package source
instead, which cannot be dodged by an empty panel."

That reasoning applies unchanged to numeric literals. This design therefore adds
a rule to an existing gate. It does not add a gate.

## Measured inventory

Literal assignments to `ui.Node` geometry fields in `internal/shell`, excluding
tests, 2026-09-11:

| Field | Sites |
|---|---:|
| `Gap` | 58 |
| `Padding` | 38 |
| `Height` | 17 |
| `Width` | 13 |
| `MaxWidth` | 5 |
| `IconSize` | 4 |
| `ContentH` | 2 |
| `ItemHeight` | 1 |
| `ImageSize` | 1 |
| **Total** | **105** |

Worst files: `popout_audio.go` 24, `popout_settings.go` 13, `popout_process.go`
11, `popout_plugins.go` 8, `popout_launcher.go` 7, `notifycard.go` 7. The same
files carry 72 correct token-sourced references, so the split is roughly 60/40
against.

`internal/ui` and `internal/render` are **clean at zero**. The violation is
entirely inside `internal/shell`, which is precisely the package the existing
scan already reads. No new scope is required.

**96 of the 105 are `Gap` or `Padding`** — spacing, which is exactly the ladder
the parity design re-bases from 2, 4, 8, 12, 16, 24 onto 1, 2, 4, 6, 9, 13, 18.
Every one of those literals is a value that currently *looks* right because it
coincides with a rung of the old scale, and that becomes silently wrong the
moment the re-base lands. This is the whole argument for the design, and it is
measured rather than asserted.

## Goal and scope

In:

- One additional rule in the existing source scan: no numeric literal on a
  `Node` geometry field.
- An exemption idiom for genuinely measured constants.
- Widening the existing behavioural checks past two panels.

Out:

- Any new test file, lint binary, or build step. The gate exists.
- `go/ast`. The house scan is regexp over source, deliberately — there are zero
  `go/ast` imports in the repository. A literal rule does not justify
  introducing a second technique.
- Making geometry a distinct Go type. Considered and rejected below (D6).
- Fixing the 105 sites. They are the parity plan's work; this design only makes
  them fail.
- `internal/ui`, `internal/render`, and the reference plugins. The first two are
  clean; plugin trees belong to the blocked plugin-visual-polish work.

## Decisions

### D1 — Extend the existing scan; add no mechanism

`TestSurfaceSourcesCarryNoLegacyVisuals` grows a third regexp beside `aliases`
and `bold`. It keeps its existing shape: `os.ReadDir(".")`, skip directories,
tests and `theme.go`, strip the comment from each line before matching, and
fail if `scanned == 0` so the gate cannot silently stop looking.

Rejected: a new `tokenconformance_test.go`. Two scans over the same sources with
two exemption lists, and a reader who fixes one and is failed by the other.

### D2 — The rule

A numeric literal assigned to a geometry field of `ui.Node` is a violation:

```
\b(Width|Height|IconSize|Radius|ItemHeight|ContentH|MaxWidth|ImageSize|ImageW|ImageH|Stroke|Padding|Gap):\s*[0-9]
```

Those thirteen are the geometry subset of the 58-field `Node`. The semantic
fields — `TextRole`, `Fill`, `Tone`, `Shape`, `Kind` — are already unrepresentable
as numbers and need no rule.

**Zero is exempt.** `Gap: 0` and `Padding: 0` mean "none", which is a
composition choice rather than a measurement, and no spacing ladder contains a
rung that means absence.

`ui.Rect{W:, H:}` is deliberately **not** matched. Panel surface sizes from
`panelTargetSize` are measured dimensions the superseded theme design explicitly
permitted ("a measured fixed dimension such as the 420 px session panel"), and
they are not node geometry.

### D3 — Exemption is a marked comment with a reason

The existing loop already splits each line at `//`. It keeps the comment instead
of discarding it, and a line whose comment carries `token-exempt:` followed by a
reason is skipped:

```go
code, comment, _ := strings.Cut(line, "//")
if strings.Contains(comment, "token-exempt:") {
    continue
}
```

An exemption is therefore visible at the site, greppable as a census, and
carries its justification in the same line a reviewer reads. A bare marker with
no reason after it fails the same as an unmarked literal.

Rejected: a file-level skip list in the test. The existing `theme.go` skip is
exactly that and is defensible because that file *is* the compatibility layer,
but a list of panel filenames would exempt whole surfaces to permit one
constant.

### D4 — Widen the behavioural checks

`TestSurfaceCardPaddingFollowsDensity`, `TestSurfaceCardTitlesAreTitleRole` and
`TestSurfaceHeadingsCarryARole` each iterate `PanelMonitor` and `PanelSession`.
They iterate every `PanelID` that opens without external state.

Panels building no cards must not silently pass: where `cardsOf` returns empty,
the subtest records the panel as card-less rather than calling `t.Fatalf`, and a
panel that is *expected* to have cards and has none still fails. The current
`t.Fatalf("no cards found")` would otherwise turn a widened loop red for panels
that legitimately build none.

This is conformance by construction and it catches what the scan cannot: a
literal laundered through a variable or a helper.

### D5 — Sequencing, which revises the tranche order

The tranche was proposed as blur → parity → conformance → panel passes. The
inventory argues conformance must not come third.

96 of 105 literals are spacing, and the parity re-base changes the spacing
ladder underneath them. Landing the re-base first means shipping 105 sites that
silently stop matching any rung; landing conformance first means the parity plan
has an exact, enumerated worklist and a gate that goes green as it is worked.

The order becomes: the scan lands **with the parity re-base, at the head of its
plan**, as task one. It fails immediately with all 105 sites named, and the
re-base tasks drive it to zero. The gate is not committed green and then broken;
it is committed red against a known list and closed deliberately.

This is a sequencing change to the tranche, not to any decision inside the
parity design.

### D6 — Types are the stronger guarantee and are still rejected

Named types (`theme.Space`, `theme.Radius`) would make a bare literal fail to
compile — illegal states unrepresentable rather than merely detected. That is
genuinely stronger than any scan.

Rejected for this tranche: it is a mechanical change across the 50 non-test
sources of `internal/shell` and the 64 test files beside them, landing
simultaneously with a re-base of every ladder. Two
large changes to the same call sites at once makes any resulting visual
regression unattributable. The scan gives most of the benefit now and does not
foreclose the typed version later; if the census in D3 ever grows large enough
to be meaningless, that is the signal to revisit.

### D7 — Failure names the replacement

A violation reports the file, line, field and value, and names what to read
instead — the spacing ladder for `Gap` and `Padding`, the density row for
`Height` and control sizes, the icon scale for `IconSize`, the shape role for
`Radius`. The existing alias failure already does this ("read the metrics row");
a message that only says "no literals" sends the reader to the design document
to work out which token applies.

### D8 — Testing

Per-package named tests only. **Do not run `go test ./...` or `-race`**: a
repo-wide race build exhausts memory on this machine.

`go test ./internal/shell` is safe to run directly. `runArgvDefault`
(`popout_session.go:270`) refuses under `testing.Testing()` and names the field
to replace, so a test can no longer reach `loginctl terminate-session self`.
That guard post-dates the incident this warning used to carry; only four test
files inject `runArgv`/`runArgvOutput`, and the rest are covered by the refusal
rather than by discipline.

The gate is itself tested:

- A fixture line with a literal on each of the thirteen fields is detected.
- `Gap: 0` and `Padding: 0` are not detected.
- A `ui.Rect{W: 420, H: 360}` is not detected.
- A line carrying `token-exempt:` with a reason is skipped; a bare marker is not.
- A literal inside a comment is not detected, which the existing comment split
  already gives.
- `scanned == 0` still fails.

### D9 — Tracker

Task one of the parity implementation plan rather than its own epic, per D5.
Records that it closes the enforcement half of the drift that produced the
correction and polish passes in the register. Status lives in bd.

### D10 — Open risks

1. **A regexp is not a parser.** A literal assigned through a variable, a
   helper return, or a composite literal spread across lines escapes it. D4's
   widened behavioural checks are the second net, and D6's types remain the
   real answer if this proves porous.
2. **105 failures at once is a large red gate.** If the parity plan stalls
   mid-way, the package sits failing. The mitigation is D5's ordering — the gate
   lands as task one of a plan that exists to drive it to zero — not a skip
   flag, which would simply become permanent.
3. The exemption census has no cap. A reviewer must actually read the reasons,
   and nothing here forces that.

## Files

| Path | Change |
|---|---|
| `internal/shell/surfacerole_test.go` | third regexp in the existing scan; keep the comment half of the line split; widen three behavioural tests past two panels; card-less panels recorded rather than fatal |
| `internal/shell/*.go` | 105 sites, fixed by the parity plan — not by this design |
| `docs/plans/README.md` | register row |

## Stop

`go test ./internal/shell -run TestSurface` passes with zero literal geometry
outside marked exemptions, the behavioural checks cover every panel that opens
without external state, and the exemption census is short enough to read in one
screen. A new panel that re-derives its own spacing fails before review.

## Verified during design

- The existing scan's shape, exemption idiom, comment split, and `scanned == 0`
  guard, read from `surfacerole_test.go:212`.
- The project's own rationale for scanning source over walking trees, quoted
  from the comment at `:198`.
- `ui.Node` has 58 fields; the thirteen geometry fields listed in D2 are its
  complete geometric subset.
- 105 literal geometry assignments in `internal/shell`, non-test, broken down by
  field and file; 72 correct token references in the same files.
- `internal/ui` and `internal/render` carry zero, so the existing scan's package
  scope is already correct.
- Zero `go/ast` or `go/parser` imports in the repository.
- `cardsOf` matches `KindCapsule` + `FillContainerHigh`, which is why a
  card-less panel is currently unchecked rather than failing.

Not verified: whether the 105 figure is stable — it was taken against `main` at
`d84c9da` and a parallel session is committing to `docs/` — and how many of the
105 turn out to warrant an exemption rather than a token.
