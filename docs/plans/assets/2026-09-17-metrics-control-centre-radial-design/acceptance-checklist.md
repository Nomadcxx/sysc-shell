# Acceptance checklist

Checks an engineer can run without reading the mockups by eye. Each names the
measurement or command that decides it. Source revision at design time:
`4119ce5`.

## Geometry

- [ ] `ccGaugeSize` is 40. A grep for `ccGaugeSize` finds one definition and no
      second diameter literal in `controlcenter_pages.go`.
- [ ] The System card is still 356 × 88 with 9px padding. `ccCardH` and
      `ccLeftColumnW` are unchanged.
- [ ] `ccPageH` is still 480 and the Home page still lays out inside the 480px
      scroll viewport without scrolling.
- [ ] Four slots of 77 with 9px gaps: the laid-out x of slot *n* is
      `card.X + 9 + n*86`.
- [ ] Content height is 57 at font scale 100%: ring 40, gap 2, caption 15.
- [ ] The row is centred in the 70px interior, so 6px sits above it and 7px
      below (or 7/6 — the split is stated, not arbitrary).

## Fit, measured rather than eyeballed

- [ ] `TestControlCentreHomeSystemGaugesFitInsideCardBounds` no longer uses a
      `(len(s)*8, 16)` stub. Its measure function returns a different height per
      `TextRole`, or it uses the real renderer.
- [ ] With that fixed stub, the test **fails** on the pre-change tree and passes
      after. A guard that passes both ways has not been fixed.
- [ ] Every descendant of the System card lies inside the padded bound at font
      scales 75, 100, 125 and 150.
- [ ] No node in the System card has a laid-out width smaller than its measured
      text width. At 150% the old layout squeezed a value box to 18px; that must
      not recur.

## Reading

- [ ] All four metrics are identifiable without hovering: the captions read
      `CPU`, `Memory`, `Temp`, `GPU`.
- [ ] Accessible names read `CPU usage`, `Memory usage`, `CPU temperature`,
      `GPU usage`. The short caption appears only in the painted string.
- [ ] The temperature ring's value ends in `°C`, never `%`.
      `renderText(ccHome(...))` contains `°C` and the temperature slot's value
      is not a percentage.
- [ ] The card carries an accessible group name now the `System` heading is
      gone.
- [ ] Every ring's value is legible with colour removed: convert a capture to
      greyscale and read all four values. The arc is reinforcement; the number
      is the datum.

## States

- [ ] A valid zero renders `0%` and `0°C`, and its gauge is not `Absent`.
- [ ] An invalid thermal reading and an invalid GPU usage both set `Absent` and
      neither leaves a stale value: `renderText` contains no previous figure.
- [ ] Unavailable preserves geometry — the laid-out bounds of every slot are
      identical between the all-valid and all-unavailable snapshots.
- [ ] `100%` and `100°C` fit inside a 40px ring at the reduced 12px size without
      touching the stroke.
- [ ] GPU unavailable and GPU identity ambiguous render identically but carry
      different accessible names.
- [ ] A multi-GPU snapshot with indistinguishable devices reports unavailable
      and never selects a list position. `selectGPU` remains the only chooser —
      grep finds no second GPU selection site.

## Primitive

- [ ] `paintRadialGauge` sizes its centred glyph and value from the box, not
      from the literals 11 and 8.
- [ ] A 22px ring renders the same as before the change — the bar widget is a
      regression surface, not a beneficiary.
- [ ] Existing `paint_test.go` radial cases still pass at 22 and 60.

## Invalidation

- [ ] `nodeVisualStateChanged` still compares `Value`, `Absent`, `ValueText` and
      `Values`, not `Text` alone.
- [ ] A snapshot differing only in temperature marks the bar changed.
- [ ] A snapshot differing only in GPU usage marks the bar changed.

## Gates

- [ ] `gofmt -l .` is empty; `go vet ./...` clean.
- [ ] `go test -count=1 ./internal/shell/ ./internal/render/ ./internal/services/`
      passes. Do not run `./...` with `-race` on the primary checkout — it
      hard-locks this machine.
- [ ] `git diff --exit-code -- go.mod go.sum`.
- [ ] Live Niri gate on `DP-1`, 3440×1440, scale 1.0: open Control Centre Home,
      capture, and record the real GPU value and device identity, or the
      unavailable slot if the machine has none. A mockup is not evidence that
      GPU telemetry works.
