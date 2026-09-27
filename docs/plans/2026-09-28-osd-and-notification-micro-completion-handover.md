# OSD and Notification Micro-interactions Completion Handover

- Date: 2026-09-28
- Plan: `2026-09-27-osd-and-notification-micro.md`
- Epic: `sysc-592`
- Follow-ups: `sysc-638`, `sysc-639`

## Merge and push

Commit `08f4fa5` merges OSD integration commit `5b4f927` with current
`origin/main` at `573b550`. I pushed it to `origin/main`. The merge had one
conflict in `internal/shell/toasthost.go`; the resolution keeps compositor
surface cleanup and theme refresh alongside expanded toast cards and the
dismiss slide.

## Automated checks

- `gofmt -l` on changed Go files: no output.
- `git diff --check`: pass.
- `git diff --exit-code -- go.mod go.sum`: pass.
- `GOMAXPROCS=4 GOWORK=off go vet ./...`: pass.
- `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/shell -run 'OSD|Toast|Notify|Notification|Badge|DND|EmptyState'`: pass.
- `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/services -run 'LockKeys|Media'`: pass.
- `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/platform/niri ./internal/ui`: pass.

The full non-race suite failed on both the merged tree and a detached clean
worktree at `origin/main` commit `573b550`. Both runs reported the same four
shell failures and sixteen tray integration failures.

```text
internal/shell:
  TestPanelSectionValidationPrecedesMutation
  TestRightClickingTheBarBatteryOpensSession
  TestRightClickingBatteryCapsulePaddingOpensSession
  TestABatteryWidgetOpensTheSessionPanel

tests/integration:
  TestTrayProjectsOnBothOutputsAndActivates
  TestTraySecondaryActivationAndScroll
  TestTrayRightClickOpensOneMenuSurface
  TestTrayMenuOpensWhenTheServicePublishesIt
  TestTrayFailedReplyDropsThePendingMenu
  TestTrayStaleReplyIsInert
  TestTrayReconnectRetiresTheOldGeneration
  TestTrayServiceLossClosesTheMenu
  TestTrayItemLossClosesOnlyItsMenu
  TestTrayRootReplacementClosesTheMenu
  TestTrayRootReplacementDropsThePendingMenu
  TestTrayMalformedSiblingsSelectNothing
  TestTrayPreferenceCollisionIgnoresThePreference
  TestTrayOutputHotplug
  TestTrayCompositorCloseReleasesTheRoot
  TestTrayCloseReleasesEverySurface
```

The repository records a hard-lock risk for the full race suite, so I did not
run it.

## Laptop observations

The owner reported that toggling Caps Lock and Num Lock shows an OSD
notification. The owner did not separately confirm the lock labels or icon.
The owner saw the media OSD title, icon, and progress meter while testing
Spotify. A Niri layer sample 300 ms after a pause showed no OSD layer.

The laptop has one `eDP-1` output and one English US layout. Brightness stayed
at maximum; remote writes returned permission-denied. The owner waived the
remaining Niri checks. I did not verify layout switching, DND, toast gestures
and slide, badge pop, empty states, hover expiry, or reduced-motion behavior.

## Review follow-ups

The independent review found no blocking defect. It found two DND edge cases:

- After a timed DND preset expires, the raw stored flag can remain on while
  `dndState` reports off. Enabling DND again can skip the change hook and its
  OSD. `sysc-638` tracks the state comparison and a focused check.
- Separate goroutines publish DND OSDs. Rapid toggles can publish out of
  order and leave an older state visible. `sysc-639` tracks ordering and a
  focused check.

## Laptop deployment

`scripts/deploy --host laptop` refused to replace the installed binary because
the laptop runs revision
`ba16ab14d809779a6f7729328f012b0418b564e6`, which contains commits absent from
`main`. `go version -m` reports `vcs.modified=false` for that binary. I left it
untouched and did not override the deploy guard. I left the clean `origin/main`
build undeployed.
