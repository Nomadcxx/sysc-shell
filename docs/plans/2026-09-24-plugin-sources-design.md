# Plugin sources, catalog, and installer — design

Issue: `sysc-506`. Owner-approved in brainstorming on 2026-09-24.

## Purpose

Milestone 6 shipped a plugin host and a local manager, and its design (D7) deferred "a catalog,
installer, updater, and removal UI". Users still install plugins by running `make install` in a
`sysc-plugins` checkout, which symlinks each plugin directory into `~/.config/sysc-shell/plugins`.

This slice lets a user browse plugins from git-hosted sources in Settings, then install, update, roll
back, and remove them. It covers first-party `sysc-plugins`, a community catalog, and any third-party
repository with the right shape. Anyone can publish a source.

## Prior art

Checked against the code and repositories, not from memory.

- **Noctalia v5** (`~/noctalia/src/scripting/plugin_catalog.*`, `plugin_git.cpp`,
  `config_types.cpp`). A source is `{kind: git|path, name, location, enabled}`. Two git sources are
  built in: `noctalia-dev/official-plugins` and `community-plugins`. A source repo carries a
  `catalog.toml`, read with `git clone --filter=blob:none --no-checkout` and `git show`. Rows carry a
  plugin API level and optional older `[[plugin.release]]` revisions pinned to a commit. The host picks
  the newest revision its API level supports and marks the row "held back" when the tip is too new.
  Plugins are opt-in through an `enabled` list. Adding a source requires no consent.
- **DMS** (`AvengeMedia/dms-plugin-registry`). A central repo holds one JSON file per plugin:
  `id`, `name`, `repo`, optional `path`, `category`, `capabilities`, `dependencies`, `compositors`,
  `distro`, and a required `screenshot`. Authors submit by pull request. Installing clones `repo`.
  Categories are documented as open ("monitoring, utilities, appearance, system, etc.") and have
  drifted: `utility` beside `utilities`, `network` beside `networking`, a capitalised `Stock`.

Neither needs a build step, because their plugins are interpreted QML or Luau: installing means
copying files at a pinned revision. sysc-shell plugins are compiled Go executables. A git repository
gives us source, and building on the user's machine would make every user install a Go toolchain.
Committing binaries into git would bloat every source's history. **This design borrows Noctalia's
source and catalog model and replaces the payload with pinned release assets.**

## Decisions

### D1. A source is a git repository with a `catalog.json` at its root

`config.Plugins` gains `Sources []PluginSource`, each `{Name, URL, Enabled}`. The config file is the
record of user intent, as in Noctalia.

- `sysc` → `https://github.com/Nomadcxx/sysc-plugins` is built in. It can be disabled and cannot be
  removed. The config writer stores only a difference from that default.
- The host reads a source with `git clone --filter=blob:none --no-checkout` into
  `$XDG_CACHE_HOME/sysc-shell/sources/<name>`, then `git fetch` and
  `git show origin/HEAD:catalog.json`. Any git host works. `git` becomes a runtime dependency of the
  store only; a missing `git` is a per-source error, not a shell failure.
- Source names match `^[a-z0-9][a-z0-9-]{0,31}$`, because a name is a directory component.
- The commit the catalog was read from is recorded with every install as provenance.

### D2. Catalog schema 1

```json
{
  "schema": 1,
  "plugins": [{
    "id": "org.sysc.timer",
    "name": "Timer",
    "author": "Nomadcxx",
    "description": "Countdown and stopwatch in the bar.",
    "long_description": "Plain text shown on the detail view.",
    "category": "productivity",
    "license": "MIT",
    "homepage": "https://github.com/Nomadcxx/sysc-plugins",
    "screenshot": {"url": "https://…/timer.png", "sha256": "…"},
    "added_at": "2026-09-01T00:00:00Z",
    "updated_at": "2026-09-24T00:00:00Z",
    "deprecated": false,
    "version": "1.4.0",
    "release_notes": "https://github.com/Nomadcxx/sysc-plugins/releases/tag/timer-v1.4.0",
    "protocol": {"major": 1, "minor": 6},
    "capabilities": ["panels"],
    "requires": {"commands": []},
    "assets": {
      "linux-amd64": {"url": "https://…/timer-1.4.0-linux-amd64.tar.gz", "sha256": "…", "size": 4812345},
      "linux-arm64": {"url": "https://…/timer-1.4.0-linux-arm64.tar.gz", "sha256": "…", "size": 4700000}
    },
    "releases": [
      {"version": "1.3.2", "protocol": {"major": 1, "minor": 5}, "capabilities": ["panels"],
       "requires": {"commands": []}, "assets": {"linux-amd64": {"url": "…", "sha256": "…", "size": 4700000}}}
    ]
  }]
}
```

- **Resolution.** The candidates are the top-level release and every entry in `releases`. The host
  picks the newest version whose protocol has the host's major version and a minor no higher than the
  host's, and that carries an asset for `linux-<runtime.GOARCH>`. When the top-level release is not
  picked, the row is **held back**. When nothing qualifies, it is **incompatible** and names the
  protocol it needs.
- **The decoder is lenient.** Unknown fields are ignored, which is the opposite of the strict
  `plugin/v1` wire decode. This keeps ratings, download counts, and README links additive. A
  breaking change bumps `schema`, and a source with an unknown schema shows "needs a newer shell".
- **Category** is required and closed: `utilities`, `monitoring`, `system`, `appearance`,
  `productivity`, `media`, `audio`, `networking`, `weather`, `finance`, `social`. This is the DMS
  working set with its duplicate spellings merged. `tools/validate-catalog` rejects other values. The
  host shows an unknown category as **Other**, so a newer catalog never breaks an older shell.
- **Screenshot** is required in the community catalog (D10) and optional elsewhere.
- **Required fields:** `id`, `name`, `author`, `description`, `category`, `version`, `protocol`,
  `assets`. **Optional:** `long_description` (plain text, no markup), `license`, `homepage`,
  `release_notes`, `added_at` and `updated_at` (RFC 3339; a missing value sorts last), and
  `deprecated`. The authoring workflow (D10) maintains `added_at` and `updated_at`.
- `homepage` and `release_notes` must be `https` URLs, or the row is rejected.
- **Capabilities and requires live in the catalog**, so consent (D5) can show them before download.
- The asset `sha256` pins content. Releases carry no commit field: with binary assets it would pin
  nothing the hash does not.

### D3. Asset archive shape

An asset is a `.tar.gz` containing exactly one top-level directory whose name is the plugin id. Inside
is `manifest.json` plus whatever the manifest's `exec` names. That makes it an ordinary plugin
directory under the existing M6 schema.

### D4. Managed root and the local override

The installer owns `$XDG_DATA_HOME/sysc-shell/plugins/`:

```
plugins/
  <id>/               active version, an ordinary plugin directory
  .prev/<id>/         one previous version, for rollback
  .staging/<random>/  in-flight work; removed at startup
  installed.json      id → {source, version, catalog_commit, sha256, installed_at}
```

- Discovery gains `SourceManaged` beside `SourceUser` and `SourceSystem`, and skips dot-directories.
- The installer never writes to `~/.config/sysc-shell/plugins`, which stays hand-managed. Existing
  symlinked development installs keep working unchanged.
- **One exception to M6's neither-wins rule:** when a **user** candidate and a **managed** candidate
  declare the same id, the user candidate wins. The managed one is kept as `Shadowed`, carrying the
  overriding path, and the manager shows a "local override" badge. Every other collision still rejects
  every side: two user copies, user against system, managed against system.
- `Plugins.Enabled` and plugin settings are keyed by id, so they carry over when a user switches
  between a local and a managed copy.

### D5. Trust and consent

Plugins are native processes running as the user with no sandbox. Manifest capabilities govern host
calls, not access to files or the network. Consent is asked where the risk changes:

- **Adding a source in Settings** shows a warning that its plugins run as the user with full file and
  network access, and requires confirmation before anything is fetched or saved. A source written into
  the config file by hand is trusted, since editing the file is the consent.
- **Installing** shows the source, version, catalog commit, sha256 prefix, capabilities, and required
  commands, and needs a confirm. An installed plugin starts **disabled**, as local plugins do today;
  running it is a separate enable.
- **Updating** asks again only when capabilities or required commands differ from the installed
  manifest. Otherwise it proceeds on the user's Update action with no further sheet.

### D6. Install, update, rollback, remove

Install and update share one path:

1. Resolve the release from the cached catalog (D2) and pass D5.
2. Download with `net/http` into `.staging/`. Stop past the declared `size` or the 64 MiB hard cap,
   then compare the sha256.
3. Extract defensively. Only regular files and directories are allowed: no symlinks, hardlinks,
   device files, absolute paths, or `..`. Caps are 256 entries and 128 MiB extracted. Permission bits
   are masked to 0755 for directories and executables, 0644 for other files.
4. Validate with the existing `plugin.LoadManifest`. The id, version, capabilities, and required
   commands must equal the catalog row. `exec` must resolve to a file inside the directory with its
   executable bit set.
5. Swap:
   1. stop the plugin if it is running;
   2. move `<id>` to `.prev/<id>`, replacing any older `.prev` copy;
   3. rename the staged directory to `<id>`;
   4. write `installed.json` atomically, as a temp file plus rename;
   5. call the existing `rescan()`, which restarts the plugin if it is enabled.

   A failure before the final rename restores `.prev/<id>` and leaves the previous version running.

**Rollback** swaps `<id>` with `.prev/<id>` and rescans. **Remove** deletes both directories and the
`installed.json` entry. It leaves `Plugins.Enabled` and the plugin's settings untouched, per the
existing rule that an absent plugin keeps its configuration.

A corrupt `installed.json` is rebuilt from the manifests on disk with provenance "unknown", and a
warning is logged. Nothing is deleted.

### D7. The store worker

All git, network, and disk work runs on one store goroutine. It never runs on the Wayland owner and
never holds `Registry.mu`. Operations are queued and run one at a time. The worker publishes immutable
`StoreState` snapshots to the shell, as other relays do. A snapshot holds:

- per source: its catalog, last fetch time, and last error, including "stale" when a fetch failed and
  the last good catalog is still shown;
- per plugin: a `Listing` (D9) and its status, one of *not installed*, *installed*, *update
  available*, *held back*, *incompatible*, *shadowed by a local copy*, or *local only*;
- the operation in flight and its progress.

A failed operation is recorded on the listing or source that caused it and never touches a running
plugin or the plugin tree.

### D8. Updates are checked, never applied

The worker fetches every enabled source at startup and every 24 hours on a `time.Ticker` it owns.
(`internal/plugin/schedule.go` is the 30 Hz view-publish gate, not a periodic scheduler; the approved
text named it in error, corrected 2026-09-24 with the tranche 1 plan.) A newer compatible release sets *update available*. Nothing is installed without
the user's Update action. The built-in `sysc` source is enabled by default, so a default install makes
this daily request.

### D9. Settings UI, at near parity with Noctalia and DMS

The Plugins page stays inside Settings (900×760; the body column is about 790 px), as DMS keeps its
browser inside settings. It gains a segmented control, **Installed · Browse · Sources**, built from
existing kinds only (`KindSegmented`, `KindTextField`, `KindScroll`, `KindImage`, `KindToggle`,
`KindButton`, `KindMenu`). No new `ui` primitive is added.

Parity was measured against Noctalia's `plugin_store_content.cpp`, `plugin_store_tile.cpp` and
`settings_content_plugins.cpp`, and DMS's `PluginBrowser.qml`, `PluginsHubHeader.qml`,
`PluginUpdatesDialog.qml` and `PluginsManageTab.qml`. Everything both offer is in scope, apart from
the deferrals listed at the end of this section.

**Installed**
- Keeps `pluginCard`. Adds a provenance line ("from sysc · v1.4.0", "local · <path>", "local override
  (managed v1.4.0 hidden)") and the source badge.
- Managed plugins get **Update to vX** (when available), **Roll back to vY** (when `.prev` exists), and
  **Remove**. Remove asks for inline confirmation and states that settings are kept.
- A header row shows **Updates (N)** and **Update all**. Update all runs queued updates one at a time
  (D7). Plugins whose capabilities or required commands changed are held out of the batch and listed
  for individual consent (D5).
- *Deprecated* and *held back* show as status chips on the card.

**Browse**
- **Filter bar:** a search field (name, author, description), a category menu ("All" plus the
  categories present under the current source filter), a source menu (shown when more than one source
  is enabled), a **Hide installed** toggle, a sort menu, and a result count.
- **Sort:** name, recently updated, recently added, category, installed first. Ascending or descending
  where meaningful. Ties break on name, then `source/id`.
- **Grid:** two columns of cards. Each card has a 16:9 screenshot slot that letterboxes rather than
  crops, then name, author, version, category, the source badge, capability chips, and one status
  action: **Install**, **Installed**, **Update**, **Held back (needs protocol 1.x)**,
  **Incompatible**, or **Local copy at <path>**.
- **Detail view:** opened by clicking a card or pressing Enter, and closed with Back or Esc. It shows a
  large screenshot, the name and badges, `long_description` (or `description` when there is none),
  author, version, category, license, capabilities, required commands, source and catalog commit, and
  **Homepage** and **Release notes** links. Install, update, and remove act here with the D5 consent
  block inline.
- **Keyboard:** arrow keys move the selection across the grid, Page Up/Down move a screen, Enter
  opens the detail view, Esc closes it. The search field keeps its own keys while focused.

**Sources**
- Each row shows name, URL, last fetched (or "stale: <reason>"), plugin count, an enabled toggle,
  **Refresh**, and **Remove** (not offered for `sysc`).
- **Add source** takes a name and a URL, then shows the D5 warning.
- A **Suggested** row offers `sysc-community-plugins` through the same confirmation, worded to say its
  plugins come from many authors and were reviewed only at submission.
- The local plugin directory and the rescan action from the M6 manager move here.

**Badges:** **Official** for `sysc`, **Community** for `sysc-community-plugins`, the source name for
any other source.

**States**
- *Loading:* the first fetch of any source.
- *Empty:* "No plugins match" with a clear-filters action.
- *All sources failed:* a page banner with the first error and **Retry**.
- *No screenshot:* "No screenshot provided".
- Per-row errors render on the row that caused them ("sha256 mismatch", "catalog: unknown schema 2",
  "git not found on PATH"), never only as a toast. A row's last error stays until the next operation on
  that row.

**Screenshots** are fetched by the store worker only while Browse is open, visible cards first, one at
a time. Each is capped at 2 MiB, checked against its sha256, and cached at
`$XDG_CACHE_HOME/sysc-shell/screenshots/<sha256>`. They render through the existing `KindImage`,
`FileResolver`, and `icons.Worker` path from wire minor 5. A failed or mismatched image shows the
placeholder. It never blocks the card and is never an install error.

**Links** open by spawning `xdg-open` detached, off the Wayland owner and without holding
`Registry.mu`. The shell has no URL-open path today, and this slice adds the only one. Only the
`https` URLs D2 admits reach it.

**Structure**
- The store resolves every catalog row into an immutable `Listing` holding every decoded field and its
  status. The UI never reads catalog JSON.
- Filtering and sorting are one pure function,
  `browseListings([]Listing, BrowseQuery{Text, Category, Source, HideInstalled, Sort, Desc}) []Listing`,
  with table tests, separate from the tree builders.
- Each card and the detail view are keyed by `<source>/<id>`. The selection survives a rebuild by
  key, so a filter change keeps the selected plugin when it is still shown (Noctalia's rule).

**Deferred:**
- *Ratings* need a vote service. They become one `Listing` field and one sort option.
- *README rendering* needs a markdown renderer the shell does not have. `long_description` covers the
  detail view until then.
- *"View Changes" diffs* need host-specific compare URLs. `release_notes` is the stand-in.
- *Related plugins* have no data source.

### D10. Community catalog and authoring kit

- **`sysc-community-plugins`** is a repository that is itself a valid source. Its `catalog.json` rows
  point at release assets in each author's own repository. Authors are listed by pull request. CI
  validates every row: asset URLs resolve, sizes and sha256 match, the archive passes the D6 checks,
  the manifest matches the row, the category is in the closed set, and the screenshot is reachable,
  PNG or JPEG, at most 2 MiB and 1920×1080, and matches its sha256. It is offered in Settings as a
  suggested source, not a built-in one.
- **The authoring kit** lives in `sysc-plugins`:
  - a reusable GitHub Actions workflow that builds each plugin for `linux-amd64` and `linux-arm64`,
    packages the D3 tarballs, attaches them to the tagged release, and rewrites `catalog.json` with
    the new versions, sizes, and hashes;
  - `tools/validate-catalog`, the same checks CI runs;
  - a short authoring guide, including the screenshot guidance: capture the plugin in a
    representative state, any aspect ratio.

  A new author copies the workflow, tags a release, and has a valid personal source.

## Error model

Store operations fail with a typed error naming the cause: source unreachable, git missing, git
timeout (60 s per invocation), schema unsupported, no asset for this arch, size exceeded, sha256
mismatch, archive rejected (with the offending entry), manifest mismatch (with the differing field),
or disk failure. Errors attach to a source or a listing (D7) and render on that row (D9).

## Testing

Table tests unless noted:

- catalog decode and resolution: compatible, held back, incompatible, no asset for this arch, unknown
  fields ignored, unknown category → Other, unknown schema;
- the extractor against hostile archives: `..`, absolute paths, symlinks, hardlinks, device files,
  oversized entries, entry-count overflow;
- install, update, rollback, and remove against an `httptest` server and a real `git init` fixture
  repository in a temp directory, with a failure injected at each swap step and the previous version
  asserted intact;
- `rejectDuplicates` with the user-over-managed rule and every still-rejected pair;
- `browseListings` over every `BrowseQuery` field, including tie ordering and missing timestamps;
- the Update all batch holding out capability changes;
- selection surviving a filter change by key, and keyboard movement at grid edges;
- the screenshot cache: hit, miss, and sha256 mismatch.

**Live Niri gate.** From Settings: add the community source, install Timer from a real `sysc-plugins`
release, enable it, and assert it is mapped with `niri msg -j layers`. Then update, roll back, and
remove it. Separately, confirm that a symlinked local copy shows the override badge and runs instead of
the managed copy.

## Delivery

1. **Store core.** `internal/plugin/store`: catalog, git fetch, download, extract, install, rollback,
   remove, `installed.json`. Also the `SourceManaged` root with the override rule, and
   `config.Plugins.Sources`. No UI; driven through the existing IPC socket.
2. **Settings UI.** The three segments, detail view, filters and sort, Update all, consent blocks,
   screenshot cache and cards, keyboard navigation, the link opener, and the 24-hour check.
3. **Cross-repository**, modelled as issues in this repository: `sysc-plugins` gets `catalog.json`,
   the release workflow, `tools/validate-catalog`, and its first tagged release. Then create
   `sysc-community-plugins` with its CI validator. The live gate needs a real release, so this blocks
   the gate but not the code.

## Out of scope

Ratings, README rendering, change diffs, related plugins, a plugin sandbox, signature verification beyond catalog-pinned
sha256, building from source, non-git (path or HTTP-only) sources, installer hooks, automatic updates,
and any DMS or Noctalia plugin, catalog, or configuration compatibility.
