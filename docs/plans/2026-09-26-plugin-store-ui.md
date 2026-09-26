# Plugin Store UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver sysc-508, the plugin store's user interface. That is a dedicated store panel (grid, filters, detail view with README and consent) and a redesigned Settings → Plugins page (Installed and Sources), at parity with the captured DMS and Noctalia references.

**Architecture:** The store worker gains an update channel, a daily check, and an on-demand media cache (screenshots and READMEs). The shell relays updates under `Registry.mu` the way the wallpaper picker does. The UI splits into pure, table-tested view models (`browseListings`, card and detail models, the markdown subset) and thin tree builders over existing `ui` kinds, so every state can be tested headless before it is checked live.

**Tech Stack:** Go 1.26 standard library; existing `internal/ui` kinds, the `icons.Worker` image decode path, and the panel host. No new module dependencies and no new `ui` primitive.

**Specs:** `docs/plans/2026-09-24-plugin-sources-design.md` (D5–D9) as amended by `docs/plans/2026-09-26-plugin-store-ui-design.md` (U1–U5). References are in `docs/plans/assets/2026-09-26-plugin-store-references/`.

## Global Constraints

- Work in a worktree off `main`: `/home/nomadx/.config/superpowers/worktrees/sysc-shell/feature/plugin-store-ui`, branch `feature/plugin-store-ui`. Run `bd` from the primary checkout only.
- Gates are per package with `GOMAXPROCS=4`. Never combine `-race` with `./...`. The repo-wide form is `GOMAXPROCS=4 go test -count=1 -p 2 ./...`, and its known failures (battery tests, tray integration) must match `main`'s.
- Commits: write the message to a file, screen it with `~/.git-hooks/commit-msg <file>`, then commit with `--no-verify`. Never stage `.beads/issues.jsonl` from the worktree. No AI attribution anywhere.
- Store I/O never runs on the Wayland owner and never under `Registry.mu`. Tree builders read only snapshots.
- Every tree builder gets a headless render test. The render package already renders pages headless, so read how `popout_wallpaper_test.go` and the settings tests do it and follow the same pattern.
- Panel IPC name: `plugin-store`. Target size is 1280×820, clamped by `Placement.FittedSize`. Grid column count is `max(1, floor(bodyWidth/300))`.
- Size caps: README 256 KiB; screenshot 2 MiB (unchanged).
- Media fetches are https, or http to loopback only, and every file is sha256-pinned from the catalog, as in `plugin/catalog`.
- The live checks stop the owner's `sysc-shell.service` on each machine and restore it afterwards: the desktop, and the laptop over `ssh -p 7777 nomadx@192.168.0.64`. `ydotool` drives clicks on the laptop; the calibration recipe is in Task 9.

---

### Task 1: `readme` in the catalog (sysc-shell + sysc-plugins)

- **sysc-shell** `plugin/catalog`: add `Readme *Screenshot` with JSON tag `readme`. It has the same shape and validation as the screenshot field: https or loopback http, and a sha256. Add `MaxReadmeBytes = 256 << 10`. Tests cover a valid readme, an http readme, and a bad sha.
- **sysc-plugins** `tools/catalog update`: when `plugins/<dir>/README.md` exists at the tagged commit, set `readme` to `https://raw.githubusercontent.com/Nomadcxx/sysc-plugins/<tag>/plugins/<dir>/README.md` with that file's sha256. `validate -fetch` checks it. Move the `sysc-shell` pin to the Task 1 commit. Tests use a table with and without a README.
- The owner approves pushing both repositories (`sysc-plugins` pins a pushed commit).
- [ ] Commits: `feat(catalog): an optional pinned readme per plugin` (sysc-shell) and `feat(tools): pin each plugin's README in the catalog` (sysc-plugins).

### Task 2: store updates, the daily check, pinned consent, and the media cache (sysc-shell `internal/plugin/store`)

- **Updates.** Add `(*Store).Updates() <-chan State`. It is buffered 1 with latest-wins: a send replaces an unread snapshot. It is published after every `rebuild` and every busy change.
- **Daily check.** `Run` owns a `time.Ticker` (interval `Options.CheckInterval`, default 24h) that enqueues a refresh. Test it with a short interval.
- **Pinned consent.**
  - `Install(source, id string, want ReleaseRef)` and `Update(id string, want ReleaseRef, confirmed bool)`, where `ReleaseRef{Version, SHA256 string}` is what the UI showed.
  - An empty `ReleaseRef` keeps the current enqueue-time pinning, which IPC uses.
  - A mismatch with the current resolution fails with `KindConsent`.
  - Update the IPC handler to pass `version`/`sha256` params when given.
- **Media cache.**
  - `(*Store).Want(keys []MediaKey)` with `MediaKey{URL, SHA256 string; Max int64}` records what the UI currently needs.
  - The worker fetches missing items one at a time into `$XDG_CACHE_HOME/sysc-shell/media/<sha256>` via `Download`.
  - `State.Media map[string]MediaState` is keyed by sha256, with `MediaState{Path string; Err error}`.
  - `Want` replaces the previous wish list, so a closed detail view stops its README fetch.
  - Tests: hit, miss, sha mismatch, oversize, and a replaced wish list.
- **Shadowed rows.** A shadowed row whose managed copy has an update carries `Listing.UpdateAvailable bool` alongside the `StatusShadowed` status (final review #8).
- [ ] Commit `feat(store): updates channel, daily check, pinned consent and media cache`.

### Task 3: store view model (sysc-shell `internal/shell/pluginstore_model.go`)

These are pure functions with table tests and no `ui` imports:

- `type BrowseQuery struct{ Text, Source, Category string; HideInstalled bool; Sort SortKey; Desc bool }` with `SortKey` constants `SortName`, `SortUpdated`, `SortAdded`, `SortCategory`, `SortInstalledFirst`, and `func (SortKey) Next() SortKey` for the cycling chip.
- `func browseListings(ls []store.Listing, q BrowseQuery) []store.Listing`.
  - Text matching is case-insensitive over name, author and description.
  - Ties break on name, then `source/id`.
  - A missing timestamp sorts last whichever direction is chosen.
- `type Card struct` holds title, byline ("v1.4.0 · author"), description, badges ([]Badge{Label, Tone}), screenshot key (a `MediaKey` or zero), glyph, and an action (`ActionInstall`, `ActionInstalled`, `ActionUpdate`, `ActionDisabled`, plus a reason). Built by `func cardFor(l store.Listing, media map[string]store.MediaState) Card`. Badge rules:
  - `sysc` → Official;
  - `community` → Community;
  - any other source → its name;
  - deprecated, held back, and local override add their own badges.
- `type Detail struct`, built by `func detailFor(l store.Listing, media map[string]store.MediaState, readme string) Detail`, carries everything U2's detail view shows, including the consent lines:
  - source, version, catalog commit, sha256 prefix;
  - capabilities and required commands;
  - "runs as your user with full file and network access";
  - "will start immediately" when the id is in `Plugins.Enabled`.
- `func categoryGlyph(category string) string`: one Material Symbols name per category, taken from the pinned subset and asserted by the icon inventory test the way `settingsSectionIcons` is.
- [ ] Commit `feat(shell): plugin store view model`.

### Task 4: markdown subset renderer (sysc-shell `internal/shell/markdown.go`)

- `func markdownNodes(src string, width int, m theme.Metrics) []*ui.Node` implements exactly U3's subset.
- **Blocks:** heading → bold text at a larger size tier; paragraph → wrapping text; list → rows of bullet plus text; code block → a column of monospace or subtle-tone lines inside a filled container; table → rows of fixed-width cells; rule → separator.
- **Inline:** `code` and **bold** become runs. If the text node has no run support, bold drops to plain text and inline code keeps its backticks. Check `ui.Node` first and record which case applied in the report. Links render as `text (url)`. Images and HTML are dropped.
- Input is capped at `MaxReadmeBytes`, and output at 400 nodes, ending in a "README truncated" line.
- Table tests cover each block type, mixed documents, unterminated fences, a 300-row table (truncation), and garbage input (never panics).
- Add a fuzz test `FuzzMarkdownNodes` that runs for 10 s in the gate.
- [ ] Commit `feat(shell): render a markdown subset for plugin READMEs`.

### Task 5: the store panel, grid and chrome (sysc-shell)

- **Panel.** Add `PanelPluginStore` to the `PanelID` list, `panelTargetSize` (1280×820), the panel tree switch, `knownPanels` in `internal/ipc/server.go` (`"plugin-store"`), and the panel-name mapping. Follow how `PanelWallpaper` is wired throughout. Search for every `PanelWallpaper` reference and mirror it where it applies.
- **Tree.** `pluginStoreTree(r, h)` builds the U2 header, search row, chip row and grid from `browseListings` + `cardFor`.
  - The search field uses the wallpaper search pattern.
  - Chips are toggle buttons; the category chip opens a `KindMenu`.
  - Cards are keyed `store-card:<source>/<id>`.
  - Screenshots show as `KindImage` from the media path decoded through the existing `icons.Worker`, as the plugin host already decodes plugin images. Until decoded, a card shows the glyph placeholder.
- **Panel state** lives in `PanelHost` fields: the query, the selected key, and the detail key. Selection survives a rebuild by key.
- **States:** loading (the first refresh is in flight and nothing is cached); empty ("No plugins match", with Clear filters); all sources failed (a banner with the first error and Retry); stale (a banner naming the stale sources).
- **Keyboard:** arrows move the selection across the grid, clamped at the edges, with no wrap; PgUp/PgDn move a screen; Enter opens the detail view; `/` focuses search; Esc clears the search, then closes the panel. The search field keeps its own keys while focused. Model this on `wallpaperKeyPress`.
- **Want list:** after each rebuild, the panel calls `store.Want` with the screenshot keys of the visible cards plus the next row.
- **Tests:** headless renders of every state at 1280×820 and at 1229×691 (the laptop's logical size clamped); keyboard movement at the grid edges; selection surviving a filter change.
- [ ] Commit `feat(shell): plugin store panel with search, filters and grid`.

### Task 6: detail view, consent and links (sysc-shell)

- **Detail view.** `pluginStoreDetail(r, h, key)` follows U2's detail layout. It requests the README (a `Want` covering both the screenshot and the readme) and renders it via `markdownNodes` once it is cached. Until then it shows `long_description`, then `description`.
- **Primary action.**
  - Install and Update expand the consent block, whose Confirm calls `store.Install` or `store.Update` with the `ReleaseRef` shown.
  - Remove expands an inline confirmation.
  - Errors render on the detail view's own error line.
- **Links.** `openURL(url string) error` runs `xdg-open` detached (`exec.Command` + `Start`, reaped in a goroutine). It only accepts `https` URLs that the catalog already validated, and never runs under `Registry.mu`. It's a package var so tests can replace it. It is the first URL opener in the shell.
- **Tests:** detail renders with a README, without one, with consent open (including the "will start immediately" line), and with an error; Confirm sends the displayed `ReleaseRef`; an http link is refused.
- [ ] Commit `feat(shell): plugin detail view with README, consent and links`.

### Task 7: Settings → Plugins rebuilt (sysc-shell `popout_plugins.go`)

- Replace `pluginsTree` and `pluginCard` with the U4 page: an Installed · Sources segment, plus Browse plugins, which opens `PanelPluginStore`.
- **Installed.**
  - Rows use the settings row anatomy that `settingsSectionColumn` uses: icon, name, badge, version, description, Requires, provenance, switch.
  - Keep the plugin settings controls (`pluginSettingRow`), shown inline under a disclosure.
  - Row actions: update, roll back, remove with confirmation, retry, and stderr as today.
  - Updates (N) plus Update all. Update all runs the no-consent updates in sequence and lists the plugins that need consent with a Review button, which opens their store detail.
- **Sources.** Rows as described in U4. Add source is two text fields and Add, which expands the D5 warning with Confirm and Cancel. The Suggested community row is dimmed until tranche 3b (sysc-510) publishes the repository, and then reads "Not published yet". The local directory and Rescan also live here.
- **Adding a source** writes `Plugins.Sources` through the existing config write path (`writeConfig`), then calls `store.Refresh()`.
- **Tests:** headless renders of Installed (managed, local override, unlisted, error, updates), Sources (fresh, stale, add-warning), and Update all with a mixed consent set.
- Close the two row-anatomy bd issues in this task's tracking step.
- [ ] Commit `feat(shell): settings plugins page with installed and sources`.

### Task 8: relay and wiring (sysc-shell)

- `relayPluginStore(st *store.Store)`, modelled on `relayWallpaper`, reads `Updates()` and, under `r.mu`, rebuilds the Settings host (when it shows Plugins) and the store panel host, then publishes surfaces. Start it from `BindPluginStore`.
- `main.go`: set `CheckInterval` (24h) and start the relay. Remove any remaining IPC-only assumptions.
- **Tests:** a fake store snapshot rebuilds an open store panel; a closed panel costs nothing.
- [ ] Commit `feat(shell): relay plugin store updates to the panels`.

### Task 9: live verification and visual review

- **Laptop setup.**
  - Calibrate `ydotool`: temporarily set the Niri mouse block to `accel-profile "flat"` and `accel-speed 0.0` (back up and restore the config).
  - Clicking at logical (x, y) is: `ydotool mousemove --absolute -x 0 -y 0`, then `ydotool mousemove -x X -y Y`.
  - On the 1.25 output, physical pixels ×0.8 give logical coordinates.
  - The helper script is `~/refcap/click.sh PX PY`.
- **Both machines:**
  1. Stop `sysc-shell.service` and run the branch build.
  2. Capture every state named in the design's verification bar.
  3. Build one side-by-side sheet per state, reference on the left and ours on the right, with `magick +append`, under `docs/plans/assets/2026-09-26-plugin-store-references/compare/`.
  4. Restore the service and verify.
- **Visual and interaction review.** One pass on the most capable model over the comparison sheets and the tree builders: spacing, type scale, focus order, keyboard paths, and truncation at scale 1.25. Findings go to one fix wave. Then the whole-branch code review, per the owner's review-once rule.
- **Record and close.** Record the gate on sysc-508 and close it.
