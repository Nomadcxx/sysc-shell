# Plugin store UI — execution handover

Commissions implementing **sysc-508** from `2026-09-26-plugin-store-ui.md`. Written 2026-09-26 by the
session that designed the plugin system and shipped its backend. Read this, then the plan, then the two
design documents it cites. Nothing here overrides `AGENTS.md`.

## What you are building, in one paragraph

The user interface for the plugin store, which is its last missing part. Users open a dedicated **store
panel** (`plugin-store`, 1280×820) from Settings. It has a searchable, filterable 4-column card grid of
plugins from every enabled catalog source, and a **detail view** with a screenshot, the rendered README,
capabilities, and an Install, Update or Remove action behind a consent step. **Settings → Plugins** is
rebuilt as **Installed** (with update, rollback and remove) and **Sources** (add, enable, refresh,
remove). The owner's bar: **the UI gets the same rigour as the backend, and parity with DMS and
Noctalia is critical.** Every state is proven headless, then compared live against captured reference
screenshots on two machines, then given a visual review.

## Read in this order

1. `docs/plans/2026-09-26-plugin-store-ui.md`: the nine-task plan you execute.
2. `docs/plans/2026-09-26-plugin-store-ui-design.md`: amendment U1–U5, and the reference image table.
3. `docs/plans/2026-09-24-plugin-sources-design.md`: the base design. D5 (consent), D7 (worker) and D9
   (UI) matter most. The amendment supersedes parts of D9.
4. `docs/plans/assets/2026-09-26-plugin-store-references/*.png`: the eight reference images. **Look at
   them before designing any tree.**

## State of the world (verified 2026-09-26)

- **Backend is done and on `main`**, which was `4f38a8f` when this was written:
  - `internal/plugin/store` (catalog reads over git, pinned downloads, defensive extract, the
    installer with rollback, one worker, listings, consent);
  - the public `plugin/catalog` package, the schema shared with the `sysc-plugins` tooling;
  - managed root and local override in `internal/plugin/discovery.go`;
  - `plugins.sources` in config;
  - `plugins.*` IPC methods (`internal/shell/pluginstore.go`);
  - the host swap hook (`pluginHost.replace`).
- **Release pipeline is done.** `sysc-plugins` `main` has `tools/catalog` (`package`, `update`,
  `validate`), `.github/workflows/release.yml` (per-plugin tags `<dir>-v<version>`), `catalog.json`,
  `catalog-meta.json` and `docs/publishing.md`. `timer-v1.4.0` is released. It's the **only** catalog
  row, until sysc-572 releases the other nine.
- **Proven live on 2026-09-25:** a shell built from `main`, with no config overrides, reads the
  built-in `sysc` source from GitHub and installs Timer from the real release.
- **Current UI:** `internal/shell/popout_plugins.go` `pluginsTree`/`pluginCard` is a bare column of text
  lines and switches. Task 7 replaces it.

## Owner rules for how you work

- **Review once at the end.** Implement and test task by task, with no per-task reviewer. Then run one
  whole-branch spec and quality review on the most capable model, plus the separate visual review in
  Task 9.
- **Modified TDD is fine:** write the test, see it fail, then implement. Don't capture every
  intermediate output.
- **Pushes, tags, merges to `main`, and stopping `sysc-shell.service`** each need the owner's go-ahead
  when you reach them. Ask once per step, and don't assume an earlier approval carries over.
- **Never add AI attribution** anywhere: no `Co-Authored-By`, no "generated with" footer. This is an
  owner rule, not just the hook's.

## Machine traps (each one has cost real time)

- **`go test -race ./...` and uncapped repo-wide builds hard-lock this box.** A PreToolUse hook blocks
  them, and it matches the text anywhere in a command, including inside a quoted `bd` note. Run
  `-race` one package at a time, and repo-wide as `GOMAXPROCS=4 go test -count=1 -p 2 ./...`. For
  per-package race over a list, write `go list` to a file and loop over it.
- **`bd create`, `bd close` and `bd update` truncate `.beads/issues.jsonl`** (to 0–1 lines) far more
  often than not. The database is fine. After every bd write:
  ```bash
  n=$(wc -l < .beads/issues.jsonl); [ "$n" -lt <expected> ] && { sqlite3 .beads/beads.db "DELETE FROM export_hashes;" && bd export -o .beads/issues.jsonl; }
  ```
  Then check `wc -l`. Commit the tracker only from the primary checkout, and only after checking the
  line count.
- **The commit hook does substring matching.** It rejects AI-ish words and also `both`, `bottom`,
  `Hallmark`, `agent`, `passed`-style phrasing has tripped it. Write the message to a file, run
  `~/.git-hooks/commit-msg <file>` until it passes, then `git commit --no-verify -F <file>`.
  `--no-verify` also keeps the bd pre-commit hook from staging a truncated tracker into your commit.
- **gopls reports false "undefined" and "internal package" errors in worktrees.** Trust `go build` and
  `go test`, and never change code to satisfy gopls.
- **Other sessions share this repository.**
  - They hold uncommitted work in the primary checkout (currently `docs/plans/README.md`, untracked
    plans, `.tmp-thumbcheck/`) and push to `main` often.
  - Work only in your worktree. When registering a document, stage only your rows: build the index
    blob from `HEAD` plus your rows with `git hash-object` and `git update-index --cacheinfo`, and add
    the same rows to the working copy.
  - `git stash list` has parked work that isn't yours; leave it.
- **`sysc-plugins` primary checkout** has another session's uncommitted calendar work. Use a worktree
  from `origin/main` under `~/sysc-plugins/.worktrees/`. You can update its remote `main` with
  `git push origin HEAD:main` when it's a fast-forward, without touching that checkout.
- **Its CI is red** from a mini-docker gate that is already broken (sysc-519). The release workflow
  doesn't depend on CI. The "open catalog PR" step fails until the owner enables Actions-created PRs
  (sysc-520). When it fails, the catalog branch is still pushed; fast-forward it into `main` with owner
  approval.
- **Pre-existing test failures on `main`:** 4 battery/panel tests in `internal/shell` and about 17 tray
  tests in `tests/integration`. Your branch's repo-wide failure set must equal `main`'s. Diff them;
  don't eyeball.

## Live verification recipe (Task 9)

**Desktop:** DP-1, 3440×1440, scale 1.0; `sysc-shell.service` runs `~/.local/bin/sysc-shell`.
1. Back up `~/.config/sysc-shell/config.json`.
2. `export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1) WAYLAND_DISPLAY=wayland-1 XDG_RUNTIME_DIR=/run/user/1000`.
3. `systemctl --user stop sysc-shell`.
4. Run your build with `nohup` and store its pid.
5. Drive it with `<bin> ipc panel.open '{"panel":"plugin-store"}'` and `plugins.*`.
6. Capture with `grim -o DP-1`.
7. Kill the build **by pid, never `pkill -f`**, which matches your own shell.
8. Restore the config (check with `cmp`), then `systemctl --user start sysc-shell`, then confirm with
   `niri msg -j layers`.

Timer is symlinked into the user root as a local development copy. Its store listing therefore reads
"local copy". For the install path, move `~/.config/sysc-shell/plugins/timer` aside and restore it
afterwards.

**Laptop:** `ssh -p 7777 nomadx@192.168.0.64`. eDP-1, 1536×864 logical, scale 1.25. DMS and Noctalia are
installed there. Setup:
- `grim` takes **logical** coordinates for `-g`, and screenshots are 1920×1080 physical.
- `ydotool` needs `systemctl --user start ydotool`.
- **Calibration:** back up `~/.config/niri/config.kdl`. In its `mouse {}` block, set
  `accel-profile "flat"` and `accel-speed 0.0` (Niri reloads live; check with `niri validate`).
- A click at logical (x, y) is then `ydotool mousemove --absolute -x 0 -y 0`, then
  `ydotool mousemove -x X -y Y`, then `ydotool click 0xC0`. `~/refcap/click.sh PX PY` wraps this and
  takes **physical** pixels (×0.8).
- Restore the Niri config byte-identical afterwards.
- Keyboard input from `wtype` only moves through sidebars, and it closes layer-shell panels, so drive
  our panels by IPC.
- The owner watches sessions on the laptop's left pane. Crop screenshots to the window under test
  before committing them.

**Comparison sheets:** `magick ref.png ours.png +append compare/<state>.png`, committed under
`docs/plans/assets/2026-09-26-plugin-store-references/compare/`.

## Decisions already made (don't reopen them)

- **Tags and catalog PRs:** per-plugin tags; the release workflow opens a catalog PR and never pushes
  to `main`.
- **One rule set:** the catalog schema lives in the public `plugin/catalog` package; there is no second
  rule set.
- **Browse lives in a dedicated panel,** not inside Settings (U1).
- **READMEs:** a pinned `readme` URL, rendered as a markdown subset into existing nodes. No new `ui`
  primitive (U3).
- **Consent** is kept even though DMS has none, and it's pinned to the release on screen (`ReleaseRef`).
- **Updates are never automatic.** A daily check shows badges only; there's no auto-update switch (U4).
- **Removing a plugin keeps `Plugins.Enabled`.** A reinstall starts at once, and the consent block says
  so.
- **Ratings, related plugins and change diffs are deferred.** Don't build them.

## Tracker

- **sysc-508 (this work):** blocked by nothing. Its notes carry the owner's UI parity bar and three
  items carried forward:
  - pinned consent from the UI;
  - a shadowed row showing an available update;
  - the "will start immediately" consent line.
- **Close in Task 7:** the two open issues "Plugin cards do not use the settings row anatomy" and
  "Plugin setting rows do not use the settings pane's row anatomy". Re-find them by title, since IDs
  get remapped by other sessions.
- **Related, not in scope:** sysc-510 (community catalog repo, so the Suggested row stays inert),
  sysc-519, sysc-520, sysc-572 (release the other nine plugins; the grid is more convincing with them,
  so ask the owner whether to do it first).

## Done means

- All nine tasks committed on `feature/plugin-store-ui`.
- The repo-wide failure set equals `main`'s, and race runs are green for every touched package.
- The comparison sheets exist for every state in the design's verification bar, on both machines.
- The visual review and whole-branch review have run, and their fix wave has landed.
- The owner has approved the merge.
- sysc-508 is closed with the gate evidence in its notes.
