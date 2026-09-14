# Media page, bar parity and D3 configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax; bd, not this file, tracks state.

**Goal:** Replace the control-centre Media disabled destination with a live page and Home tile, close the bar widget's parity gaps (state glyph, marquee), and give the service its configuration surface — the remainder of sysc-156 plus sysc-283 and sysc-284.

**Architecture:** One shared body (`mediaBody`) over cached service state, writes through `scheduleControl`, a third small `icons.Worker` for art, one new render primitive (`paintTextMarquee`) behind a linear sweep animator mode. Everything rides machinery that already holds: the CC spine, the animator, the worker bounds, the slider cycle.

**Tech Stack:** Go 1.26.4; no new module dependencies. The only new asset is four glyphs subset into the existing embedded Material Symbols TTF by the existing `build.py`.

**Spec:** `docs/plans/2026-09-15-media-page-design.md`

## Global Constraints

- **Never run `go test ./...` or any `-race` build.** Run named tests in one package: `go test ./internal/services -run TestMedia`, `go test ./internal/shell -run TestMedia`, and full single packages where a task says so.
- **Tests use a fake bus, never a real session bus.** The registry tests install `services.NewUnavailableMedia()` or a fake-backed service; nothing reaches the developer's desktop.
- **Docs land on `main` before code.** Task 0 commits the design and this plan as docs-only commits with register rows. The sixteen-task Milestone 2 plan died uncommitted; do not repeat it.
- Work in `.worktrees/feature/media-page` off `main`. bd runs from `/home/nomadx/sysc-shell` (primary checkout) with `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db` when exporting into the worktree; reset `export_hashes` before any export that must contain the full graph.
- The docs and tracker claim from Task 0 already landed on `main` (`0b148a4`, `9fb1765`, `c996e9f`). Start execution at Task 1; do not copy or recommit the documents.
- Go only, no CGO, no new modules. `go.mod`/`go.sum` must not change; every commit verifies `git diff --exit-code -- go.mod go.sum`.
- **The `commit-msg` hook rejects these substrings case-insensitively:** `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`, `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `gpt-[0-9]`, `llm`, `ai assistant`, `bot`, `agent`. Ordinary words trip it — `both` contains `bot`. Screen every message:
  ```bash
  grep -oiE "(claude|anthropic|chatgpt|openai|copilot|cursor|cody|tabnine|codex|gemini|bard|gpt-[0-9]|llm|ai assistant|bot|agent)" msg.txt && echo BANNED || echo clean
  ```
  No `Co-Authored-By` trailer. Never `--no-verify`.
- Code-touching commits run: `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`, the task's named tests.
- Line numbers in this plan were true when written; locate anchors by symbol, as the smoothness plan's amendment teaches.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/services/media.go` | `CanSeek`, `Configure`, selection order, blacklist filter |
| `internal/config/config.go`, `internal/config/load.go`, `internal/config/write.go` | strict `media.preferred` and `media.blacklist` configuration |
| `internal/render/paint.go` | `paintTextMarquee` |
| `internal/ui/tree.go` | `Marquee`, `TextOffset` node fields |
| `internal/shell/animation.go` | `animSweep` linear wrap mode |
| `internal/shell/bar.go` | media resolve step beside the gradient resolve |
| `internal/shell/mediawidget.go` | state glyph, `MaxWidth`, `Key`, marquee |
| `internal/shell/mediabody.go` | new: the page body, activation, lease hooks |
| `internal/shell/popout_controlcenter.go` | `Enabled: true`, `ccPage` case, hook calls |
| `internal/shell/controlcenter_pages.go` | Home Now-playing tile |
| `internal/shell/registry.go` | `mediaArt` worker, `Configure` on construct/reload, `media:` dispatch |
| `internal/render/icons/material/build.py` + `materialfont.go` + TTF | four glyphs |

---

### Task 0: Land the documents, open the worktree, claim the work

- [ ] **Step 1:** Copy `~/.commandcode/plans/2026-09-15-media-page-design.md` to `docs/plans/2026-09-15-media-page-design.md` on `main`, add its register row, commit `docs(plans): design the media page and bar parity slice`.
- [ ] **Step 2:** Copy `~/.commandcode/plans/2026-09-15-media-page.md` (this file) to `docs/plans/2026-09-15-media-page.md`, add its register row, commit `docs(plans): plan the media page and bar parity slice`.
- [ ] **Step 3:** `git worktree add .worktrees/feature/media-page -b feature/media-page main`. Baseline: `go build ./...` and `go test ./internal/services ./internal/shell ./internal/config -run '^$'`.
- [ ] **Step 4:** From the primary checkout: `bd update sysc-156 --status in_progress`, `bd update sysc-283 --status in_progress`; export into the worktree (with the `export_hashes` reset) and commit `chore: claim the media page slice in the tracker`.

---

### Task 1: Service — `CanSeek`, `Configure`, selection order (sysc-283)

**Files:** Modify `internal/services/media.go`, `internal/config/config.go`, `internal/config/load.go`, and `internal/config/write.go`; test `internal/services/media_test.go` and `internal/config/config_test.go`.

**Interfaces:** Produces `MediaState.CanSeek bool`, `func (m *Media) Configure(preferred string, blacklist []string)`.

- [ ] **Step 1: failing tests**

```go
func TestMediaDecodesCanSeek(t *testing.T)          // fake prop CanSeek:true -> State().CanSeek
func TestMediaBlacklistedNamesNeverJoin(t *testing.T) // Configure with a blacklist before
                                                     // construction-equivalent discovery: a
                                                     // blacklisted MPRIS name in the fake's
                                                     // names never appears in Players()
func TestMediaConfigureReconcilesTheLiveSet(t *testing.T) // Configure after discovery drops a
                                                          // blacklisted player and republishes
func TestMediaMostRecentlyPlayingWinsTheMiddleRule(t *testing.T)
// two players; b goes Playing then Paused via a Status change event; a never plays;
// active resolves to b until a plays
func TestMediaConfiguredPreferredBeatsHistory(t *testing.T) // configured preferred wins over
                                                            // most-recently-playing; runtime
                                                            // Prefer still beats configuration
func TestMediaUnconfiguredSelectionIsUnchanged(t *testing.T) // zero config: the three existing
                                                             // selection tests' outcomes hold
func TestMediaConfigRoundTrips(t *testing.T)                 // preferred and blacklist survive
                                                             // strict parse and sparse write
```

- [ ] **Step 2:** Run `go test ./internal/services -run TestMedia -v`; expect failures (`CanSeek` false, `Configure` undefined).
- [ ] **Step 3:** Implement: add the strict `config.Media` block; decode `CanSeek` in `probePlayer`; record `lastPlaying` only when a refresh crosses into `Playing`; `reselectLocked` gains the configured-preferred rung between the runtime preference and the most-recently-playing scan, with the candidate set filtered by the blacklist; `Configure` filters the retained set, republishes, and starts a service-owned enumeration to reconcile names that were previously blacklisted. Apply configuration at construction and after a committed reload, after `Registry.mu` is released.
- [ ] **Step 4:** `go test ./internal/services -run TestMedia -v` green; `go vet ./internal/services`; commit:
  `feat(services): gate media selection with configuration and play history`

---

### Task 2: Four transport glyphs

**Files:** `internal/render/icons/material/build.py` (`ICONS`), `internal/render/materialfont.go` (`materialIcons`), rebuilt `material-symbols-rounded.ttf`.

- [ ] **Step 1:** Add `play_arrow`, `pause`, `skip_next`, `skip_previous` to **both** hand-kept lists. The existing sync test is the check that they agree.
- [ ] **Step 2:** Rebuild: `python3 internal/render/icons/material/build.py <pinned MaterialSymbolsRounded.ttf>`. If the pinned upstream file cannot be fetched, **stop and report** — do not substitute a font of unknown provenance.
- [ ] **Step 3:** `go test ./internal/render -run TestMaterialFont -v` green; verify each glyph shapes to a non-empty advance via the existing coverage check. Commit (the larger TTF included):
  `feat(render): add the media transport glyphs to the subset`

---

### Task 3: Marquee and the bar's state glyph (sysc-284)

**Files:** `internal/render/paint.go`, `internal/ui/tree.go`, `internal/shell/animation.go`, `bar.go`, `mediawidget.go`; tests in `internal/render`, `internal/ui`, `internal/shell`.

**Interfaces:** `Node.Marquee bool`, `Node.TextOffset int`; `paintTextMarquee`; animator sweep mode `TargetSweep(key, animSweep, trip)` (0→1 linear wrap, never settles, pinned 0 reduced-motion); bar resolve `resolveMediaMotionLocked` beside `resolveGradientMotionLocked`.

- [ ] **Step 1 (render, failing):** table tests over the marquee math: advance fits → static paint, no clip; overflow → draw at `-offset`, second copy at `-offset + advance + gap`, wrap at `advance + gap`; gap = eight space advances measured on the face; reduced-motion/not-active → the truncation path (ellipsis), never a bare clip. Run `go test ./internal/render -run TestMarquee -v`; expect fail.
- [ ] **Step 2:** Implement `paintTextMarquee` + fields; `kindcoverage` stays green (fields need no kind). Commit: `feat(render): paint a clipped, wrapping marquee text cell`
- [ ] **Step 3 (animator + widget, failing):** `TestMediaWidgetSwapsGlyphWithStatus` (playing→`pause`, paused→`play_arrow`, stopped→`music_note`); `TestMediaWidgetSetsMarqueeConfig` (`Marquee` true on the title node, `MaxWidth` from `item.MaxWidth`, stable `Key`); `TestMarqueeResolveOnlyRunsOnOverflow` (resolve writes offset and starts frames only when measured advance exceeds the cell, and clears `Marquee` on the copy when reduced motion). Expect fail; implement `TargetSweep` + resolve; the resolve measures via `ui.MeasureText` with the same tabular flag the paint uses.
- [ ] **Step 4:** `go test ./internal/shell -run TestMedia -v && go test ./internal/shell -run TestMarquee -v && go test ./internal/render -run TestMarquee -v` green; full `internal/shell` package run. Commit: `feat(shell): scroll overflowing media titles and swap the state glyph`

---

### Task 4: The art worker

**Files:** `internal/shell/registry.go` (`mediaArt` field, lazy starter, cancel in `Close`), new `internal/shell/mediaart.go`; Test `internal/shell/mediaart_test.go`.

**Interfaces:** `func (r *Registry) mediaArtFor() *icons.Worker`; `func mediaArtRequestPath(artKey string) (string, bool)`; per-job timeout + negative cache live in a small wrapper around the worker, not in `internal/icons`.

- [ ] **Step 1 (failing):** `TestMediaArtKeyParsesToAPath` — `file:///tmp/a%20b.png` → `/tmp/a b.png`; `https://…`, `file://host/p`, and garbage → `false`. `TestMediaArtTimeoutPublishesNilOnce` — a job pointed at a path that never satisfies the read (a FIFO or a slow fake) publishes nil within the watchdog and the negative cache suppresses retries until the key changes. Expect fail.
- [ ] **Step 2:** Implement: key = `icons.Key{Name: path, W: artBox, H: artBox}`; watchdog five seconds per job (session-ops precedent); negative cache `map[string]struct{}` keyed by `ArtKey`; the wrapper documents the bounded abandoned-reader. Lazy start mirrors `wallpaperThumbsLocked`; cancelled where `wallpaperThumbCancel` is.
- [ ] **Step 3:** `go test ./internal/shell -run TestMediaArt -v` green; full `internal/shell` run. Commit: `feat(shell): resolve album art off the paint path with bounded reads`

---

### Task 5: The Media page (sysc-156)

**Files:** new `internal/shell/mediabody.go`; `popout_controlcenter.go`; `registry.go`; tests in `internal/shell`.

**Interfaces:** `func mediaBody(r *Registry, h *PanelHost) *ui.Node`; `startMediaBodyLocked(h)` / `leaveMediaBodyLocked(h)`; `h.mediaLease *services.Lease`; actions `media:playpause`, `media:next`, `media:prev`, `media:seek:<us>`, `media:player:<bus>`.

- [ ] **Step 1 (failing):**

```go
func TestControlCentreMediaPageIsNoLongerDisabled(t *testing.T)
// selectControlCentreSection(h, "media") returns true; ccPage builds the
// now-playing card, transport row and player list from a fake-backed service
// installed on the registry; the disabled rail entry is gone
func TestMediaBodyLeaseTracksTheSection(t *testing.T)
// entering the section runs the service (Running() true); leaving releases
// it (false); closing the host releases too
func TestMediaBodyTransportWritesThroughTheControlSeam(t *testing.T)
// a play/pause click dispatches via r.scheduleControl's seam, not on the
// owner; the click handler records no bus call under Registry.mu
func TestMediaBodyPlayerClickPrefers(t *testing.T) // row click calls Prefer; active row StateSelected
func TestMediaSeekWritesOnRelease(t *testing.T)    // slider release -> SetPosition via the seam
                                                   // with the snapshot's trackID available;
                                                   // drag pending shown, cleared on track change
```

- [ ] **Step 2:** Run `go test ./internal/shell -run TestMedia -v`; expect fail (`mediaBody` undefined, section still disabled).
- [ ] **Step 3:** Implement per design D2/D3/D4/D10: `Enabled: true` on the `ccSections` media entry; `ccPage` case; the three cards; `h.mediaLease` hooks called from `selectControlCentreSection` (enter/leave) and the CC branch of `closeAllPanelsLocked`; `media:` prefix dispatch beside the `audio-` handlers; position row `KindMeter` normally, `KindSlider` when `CanSeek && LengthUS > 0`, animated by the surface frame loop reading `CachedState()` at paint (frames wanted while `Status == PlaybackPlaying` or a seek drag is pending). Art node requests through the Task 4 wrapper and `Lookup`s at build, never decodes. Keep `publishMediaSnapshot` as the only service-to-retained-tree bridge.
- [ ] **Step 4:** `go test ./internal/shell -run TestMedia -v` green; full `internal/shell` package. Commit: `feat(shell): replace the disabled media destination with a live page`

---

### Task 6: Home tile and the IPC route

**Files:** `controlcenter_pages.go` (`ccHome`), `panelhost.go` (no change expected — verify), tests in `internal/shell`.

- [ ] **Step 1 (failing):** `TestHomeShowsNowPlayingTile` — with a fake-backed service holding a playing snapshot, `ccHome`'s right column contains the Now-playing tile with the title and the state glyph; with none, the tile is disabled via `ccDisable`. `TestPanelSectionMediaIPCUnblocked` — `panelSection(PanelControlCenter, "media")` returns no error and `HandlePanelByName("control-center", "media", …)` lands on the media section.
- [ ] **Step 2:** Implement the tile (D9); expect the IPC test to pass with no `panelhost.go` change — if it does not, the design's D1 claim is wrong and the design gets amended before proceeding.
- [ ] **Step 3:** Full `internal/shell` + `internal/config` runs. Commit: `feat(shell): summarize media on the control-centre home`

---

### Task 7: Gates, tracker, live evidence

- [ ] **Step 1:** `gofmt -w . && test -z "$(gofmt -l .)"`, `go vet ./...`, `go build ./...`, full `services`/`shell`/`config` packages, `git diff --exit-code -- go.mod go.sum`.
- [ ] **Step 2:** Close sysc-156, sysc-283, sysc-284 in bd with commit hashes; export (reset first) and commit the JSONL: `chore: close the media page slice in the tracker`.
- [ ] **Step 3:** Rebase onto current `main`; run the live gate (design D11) with a real player and a browser's per-tab players on the laptop; record the marquee trip-speed measurement and every unrun item.
- [ ] **Step 4:** Land `docs/plans/2026-09-15-media-page-completion-handover.md` + register row as docs-only commits on `main`: gate output, live observations, the measured tunables, known defects.

---

## Self-Review

**Spec coverage.** D1 → Task 5 (Enabled, ccPage case, no second tree, and the retained-state relay). D2 → Task 5 (three cards, rungs, dash-not-zero, disabled-without-player card). D3 → Task 5 (cached reads, scheduleControl writes, Prefer under the lock). D4 → Task 5 (hooks + close path + lease test). D5 → Task 1. D6 → Task 4 (worker, decode, timeout, negative cache; KindStack explicitly out). D7 → Task 3 (fields, primitive, sweep mode, resolve, MaxWidth/Key, fallback). D8 → Task 3 (glyph swap). D9 → Task 6. D10 → Task 5 (dispatch, seek drag, keyboard path). D11 → Task 7 Step 3. D12 → Task 7 Step 2.

**Placeholders.** Two measured tunables are deliberately open: the art box size and the marquee trip speed (30 px·s⁻¹ is the initial value). Both are measured against the reference capture in Task 7 and recorded in the completion handover; neither can be invented from a document.

**Type consistency.** `CanSeek` and `Configure` are produced in Task 1 and consumed in Tasks 5 and 6. `Marquee`/`TextOffset`/`animSweep` are produced in Task 3's render/animator steps and consumed by the resolve. `mediaArt` is produced in Task 4 and consumed in Task 5. `media:` actions are produced in Task 5's dispatch and exercised by its tests. `panel:media` (already landed) is untouched; the page makes its `selectPanelSectionLocked` call succeed.

**Constraints.** No `./...` or `-race` runs anywhere; every bus-touching test uses the fake; `go.mod`/`go.sum` verified per commit; every commit message screened; the docs land on `main` before the first code commit.
