# Centre Clock, Media, and Wordmark Completion Handover

Date: 2026-09-17
Branch: `feature/sysc-314`
Worktree: `/home/nomadx/sysc-shell/.worktrees/feature/sysc-314`
Design: `2026-09-16-centre-clock-media-wordmark-design.md`
Plan: `2026-09-17-centre-clock-media-wordmark.md`
Issue: `sysc-314`

Implementation commit: `e7d6c5514fcdce9800410a9a65b6e80e22b87719`.
The commit changes the default centre composition, propagates clock width
floors through groups, collapses unavailable media before every bar consumer,
and anchors a single wordmark at the content-band centre.

## Commits

| Commit | Contents |
|---|---|
| `4119ce5` | Executable bar-composition plan |
| `1088300` | Centre clock, media, and wordmark design and issue claim |
| `e7d6c55` | Implementation and focused regression tests |

## Automated gate

Fresh runs on 2026-09-17 from the feature worktree:

```text
go test -p 2 -count=1 ./internal/config                 ok
go test -p 2 -count=1 ./internal/ui                    ok
go test -p 2 -count=1 ./internal/shell                 ok, 15.569s
go build ./cmd/sysc-shell                              exit 0
go vet -p 2 ./...                                      exit 0
gofmt -l changed files                                 no output
git diff --check                                      no output
git diff --exit-code -- go.mod go.sum                 exit 0
```

The focused new checks also pass:

```text
./internal/config -run TestDefaultVocabularyShipsTimeDateGroupWordmarkAndMedia  ok
./internal/ui -run TestAnchoredWordmark                                      ok
./internal/shell -run TestDefaultCentre|TestMediaWidget|TestBarCollapsesAbsentMedia  ok
```

The tests cover the one time/date group, recursive clock floors, initial and
transitional media absence, bounded marquee titles, art and playback glyphs,
stable actions, and the wordmark centre invariant.

## Live DP-1 gate

Niri reports one output, `DP-1`, at 3440×1440 logical pixels and scale 1.0.
The earlier live sequence captured the default bar with no player, with a
player and a long title, and after a minute transition. The wordmark stayed at
the content-band centre. Media disappeared without a gap when unavailable and
returned with its action and title when the player appeared. The original
single bar/toast surface pair remained; the temporary branch surfaces did not
leak.

A fresh layer assertion after the sequence returned:

```text
slapper          DP-1  Background
sysc-shell:bar   DP-1  Top
sysc-shell-toast DP-1  Overlay
```

A fresh 3440×1440 screenshot at `/tmp/sysc-314-dp1-current.png` showed the
centred SYSC mark, the clock, and the active media title. An unrelated keyring
authentication prompt appeared in that desktop capture and does not affect
the bar assertion. The existing shell process stayed running, and the MPV
fixture remained paused with its original title after the gate.

This machine has no second output, so no multi-output live claim belongs to
this ticket. The `sysc-313` overflow work also remains out of scope.

## Limits

The repository-wide `go test ./...` and race gate remain machine-blocked on
this workstation. The affected package suites, build, vet, formatting, and
module-file checks above provide the available automated proof.

The local review covered the implementation diff. `AGENTS.md` requires approval
before external review tools, so no external review ran.
