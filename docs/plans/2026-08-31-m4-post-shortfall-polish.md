# M4 Post-Shortfall Polish

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** After verifying sysc-38/39/40, close the leftovers those fixes left on the floor: hide still blocks under `mu`, template supersede drops the latest palette, IPC `status` is thinner than 4A/4B, and settings virtual-list focus ignores `Item`.

**Architecture:** Same owners. No new packages.

---

### Task 1: OSD hide sends aux without the registry lock

**Files:** `internal/shell/osd.go`, `internal/shell/osd_test.go`

**Step 1:** `TestOsdHideReleasesLockBeforeAux` — fill `aux`, `Show` then drain the open, fill again, call `hideAll` from a goroutine, assert `mu.Lock` succeeds within 200 ms.

**Step 2:** `go test ./internal/shell -run TestOsdHideReleasesLockBeforeAux -count=1` fails.

**Step 3:** `hideAll` copies close requests, drops `mu`, then `sendAux`.

**Step 4:** Pass. Commit with Task 2–4 if they land in the same sitting.

---

### Task 2: ApplyEnabled supersede uses the latest job

**Files:** `internal/theming/enabled.go`, `internal/theming/enabled_test.go`

**Step 1:** `TestApplyEnabledSupersedeUsesLatestHome` — first `ApplyEnabled` blocks in `enabled("alacritty")`; second call uses a different `$HOME`; unblock; second home must contain `alacritty.toml`.

**Step 2:** Fail (rerun uses the first home).

**Step 3:** Queue `applyJob{home, enabled, tok}`; busy path overwrites the queue; drain runs the queued job.

**Step 4:** Pass.

---

### Task 3: IPC status reports panels, matugen, templates

**Files:** `internal/shell/registry.go`, `cmd/sysc-shell/main.go`, test in `internal/shell`

**Step 1:** `TestRegistryStatusReportsOpenPanelsAndTemplates` — open settings; `Status()` has `panels` containing `"settings"`, `templates["niri"]==true`, `matugen` bool, `audio`/`brightness`/`version` keys.

**Step 2:** Fail.

**Step 3:** `Registry.Status()` copies under lock, `LookPath("matugen")` after unlock. `main` uses it.

**Step 4:** Pass.

---

### Task 4: Virtual-list focus walks `Item`

**Files:** `internal/ui/focus.go`, `internal/ui/focus_test.go`, `internal/shell/popout_settings.go`

**Step 1:** `TestFocusablesWalksVirtualListItem` — node with empty `Children`, `ItemCount=3`, `Item` returning a focusable; `Focusables` length 3.

**Step 2:** Fail.

**Step 3:** `Focusables` walks `Item(i)` for `KindVirtualList`. Drop the pre-fill `Children` loop in `settingsTree`.

**Step 4:** Pass. `go test ./...`

OSD glyph/label remain geometric stand-ins (dash track, accent square). Real icon-font + shaped text is a later consumer of `render.TextRenderer`, not this polish. Live Niri stays unrun. `sysc-3` stays open.
