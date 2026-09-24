# Plugin Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver sysc-509: `sysc-plugins` publishes per-plugin, sha256-pinned release tarballs from tags, keeps a `catalog.json` that `sysc-shell`'s built-in `sysc` source reads, and gives authors a validator. The gate is a real install of Timer from a real release through the shell's default source.

**Architecture:** The catalog schema and its validation move out of `sysc-shell/internal/plugin/store` into a public `sysc-shell/plugin/catalog` package, beside `plugin/v1`. The shell's store and `sysc-plugins`' tooling then enforce one rule set. `sysc-plugins` gains one command, `tools/catalog`, with three verbs: `package` builds a reproducible release tarball, `update` rewrites `catalog.json` for a published tag, and `validate` checks the catalog in CI. A tag-triggered workflow builds both arches, publishes the GitHub release, and opens a pull request carrying the regenerated catalog.

**Tech Stack:** Go 1.26 standard library, GitHub Actions with the preinstalled `gh` CLI and `GITHUB_TOKEN`. No new module dependencies in either repository; `sysc-plugins` only moves its existing `sysc-shell` pin forward.

**Spec:** `docs/plans/2026-09-24-plugin-sources-design.md` (D2, D3, D10).

## Owner decisions (2026-09-25)

- **Tags are per plugin:** `<dir>-v<version>`, e.g. `timer-v1.4.0`, where `<dir>` is the directory under `plugins/`. Each plugin releases independently.
- **The workflow opens a PR** carrying the regenerated `catalog.json`. CI never pushes to `main`.

## Controller rulings

- **One owner for the catalog rules.** Schema types, `Decode`, per-row validation, the category set, the URL rule and `Newer` move to `sysc-shell/plugin/catalog`, a public package. `Resolve` stays in the store because it depends on the host's protocol ceiling. The alternative, `sysc-plugins` re-implementing the rules, would drift. Cost if wrong: one more public package in `sysc-shell` to keep stable.
- **Category and other listing metadata live in `catalog-meta.json`** at the `sysc-plugins` root, keyed by plugin id: `category`, `author`, and the optional `long_description`, `license`, `homepage` and `screenshot`. The manifest decoder is strict and must not grow listing fields. `name`, `description`, `version`, `protocol`, `capabilities` and `requires` come from each plugin's `manifest.json`, so they cannot disagree with the archive. Cost if wrong: one extra file for authors to edit.
- **The tarball is the plugin directory** minus `*.go`, `testdata/` and `bin/`, plus the built binary at the manifest's `exec` path, under a top-level directory named for the plugin id. It is deterministic: sorted entries, uid/gid 0, modes 0755/0644, and mtime set to the tagged commit's time. Cost if wrong: an asset missing a runtime file is caught by the live gate.
- **Older releases kept in the catalog:** the previous top-level release moves into `releases`, which is capped at the five newest. Cost if wrong: a very old shell loses its last compatible release; the cap can be raised later.

## Global Constraints

- `sysc-shell` work runs in a worktree off `main`. `sysc-plugins` work runs in a worktree off `origin/main`, never in the primary checkout, which holds another session's uncommitted calendar work.
- Gates are per package with `GOMAXPROCS=4`. Never combine `-race` with `./...`.
- Commit messages are screened with `~/.git-hooks/commit-msg <file>` and then committed with `--no-verify`. Never stage `.beads/issues.jsonl` from a worktree. No AI attribution trailer.
- Asset file name: `<id>-<version>-linux-<arch>.tar.gz`. Asset URL: `https://github.com/Nomadcxx/sysc-plugins/releases/download/<tag>/<file>`. Arches: `amd64`, `arm64`, built with `CGO_ENABLED=0`.
- The catalog schema and every limit are unchanged from the design: schema 1, 64 MiB assets, the closed category set, `https`-only links.
- Pushing a tag and merging to either repository's `main` are owner-approved steps; the tasks say where.

---

### Task 1 (sysc-shell): public `plugin/catalog` package

**Files:** create `plugin/catalog/catalog.go` and `plugin/catalog/catalog_test.go`; modify `internal/plugin/store/catalog.go`, `internal/plugin/store/store.go`, `internal/plugin/store/install.go`, and the store tests that reference the moved symbols.

**Interfaces produced:** in package `catalog`: `Schema`, `MaxCatalogBytes`, `MaxAssetBytes`, `CategoryOther`, `Categories`, `Asset`, `Screenshot`, `Requires`, `Release`, `Entry`, `RowError`, `Catalog`, `func Decode([]byte) (Catalog, error)`, `func (Entry) Validate() error`, `func CheckFetchURL(string) error`, `func IsHTTPS(string) bool`, `func Newer(a, b string) bool`, `func SameSet(a, b []string) bool`. Errors are plain `error` values wrapping a sentinel per kind: `ErrSchema`, `ErrInvalid`. The store maps them onto its `Kind` values (`KindSchema`, `KindCatalog`) in one place, where it calls `Decode`.

- [ ] Move the code verbatim apart from exporting the names above. `Resolve`, `Resolution` and `Compat` stay in the store and take `catalog.Entry`.
- [ ] Move the catalog decode/validate/`Newer` tests into `plugin/catalog/catalog_test.go`, asserting `errors.Is(err, catalog.ErrSchema)` and `catalog.ErrInvalid` where the store tests asserted kinds. `TestResolve` stays in the store.
- [ ] Add one store test proving the kind mapping: a schema-2 body refreshed through the store surfaces `KindSchema`.
- [ ] Gates: race for `./plugin/catalog`, `./internal/plugin/store`, `./internal/shell` (the store tests only); vet; gofmt; `go.mod`/`go.sum` unchanged.
- [ ] Commit `refactor(catalog): make the catalog schema a public package`. The owner approves the merge to `main` and the push, because `sysc-plugins` pins a pushed commit.

### Task 2 (sysc-plugins): pin and `tools/catalog package`

**Files:** `go.mod`/`go.sum` (move the `sysc-shell` pin to the Task 1 commit with `go get github.com/Nomadcxx/sysc-shell@<sha>`); create `tools/catalog/main.go`, `tools/catalog/package.go`, `tools/catalog/package_test.go`.

**Behaviour:** `go run ./tools/catalog package -plugin <dir> -arch <amd64|arm64> -out <dist>`.
1. Reads `plugins/<dir>/manifest.json`.
2. Builds `./cmd/sysc-plugin-<dir>` with `CGO_ENABLED=0 GOOS=linux GOARCH=<arch> go build -trimpath -ldflags=-buildid=`.
3. Writes `<dist>/<id>-<version>-linux-<arch>.tar.gz` and prints its path, size and sha256 as one JSON line.
   - Mtime comes from `SOURCE_DATE_EPOCH`, or failing that `git log -1 --format=%ct`.
   - Refuse when `cmd/sysc-plugin-<dir>` does not exist, or when the manifest's `exec` is not `bin/sysc-plugin-<dir>`.

- [ ] Tests (table): the archive's entries are sorted, uid/gid 0, modes 0755 for the binary and 0644 for others, one top-level `<id>/`, no `.go`, `testdata` or `bin/` source files; kdeconnect's `assets/*.png` and wallpaper-depth's `depth_helper.py` are included; packaging twice gives the same sha256; extracting it with `archive/tar` yields `<id>/manifest.json` and the `exec` file with its executable bit set. (`plugin.LoadManifest` is internal to `sysc-shell`, so Task 5's live install is the proof of the shell's own acceptance.)
- [ ] Commit `feat(tools): package reproducible plugin release archives`.

### Task 3 (sysc-plugins): `catalog-meta.json`, `tools/catalog update` and `validate`

**Files:** create `catalog-meta.json` (all ten plugins), `catalog.json` (initially `{"schema": 1, "plugins": []}`), `tools/catalog/update.go`, `tools/catalog/validate.go` and their tests.

**Categories for `catalog-meta.json`:** aiusage `monitoring`; calendar `productivity`; github-notifications `productivity`; kdeconnect `utilities`; mini-docker `system`; notes `productivity`; screen-recorder `media`; timer `productivity`; wallpaper-depth `appearance`; world-clock `utilities`. Author `Nomadcxx`, license `MIT` (matching `LICENSE`), homepage `https://github.com/Nomadcxx/sysc-plugins`.

**`update -tag <dir>-v<version> -dist <dir> [-now RFC3339]`:**
1. Check that the tag's version equals the manifest's.
2. Build the new top-level release from the manifest and the dist files, using the asset URL from Global Constraints.
3. If the plugin already has a row: move its old top-level release to the front of `releases` (dropping any entry with the same version), cap `releases` at 5, keep `added_at`, and set `updated_at` to now. For a new row, set `added_at = updated_at = now` and take the listing fields from `catalog-meta.json` and the manifest.
4. Write the catalog sorted by id with two-space indentation. Run `catalog.Decode` on the result and fail if any row is rejected.

**`validate [-community] [-fetch]`:** `catalog.Decode` must reject no rows, and every id must have a `catalog-meta.json` entry and a `plugins/<dir>` whose manifest agrees on name, description, protocol, capabilities and requires for the top-level release. `-community` also requires a screenshot. `-fetch` downloads each asset and screenshot and checks size and sha256, for the release workflow. Wire `validate` (without `-fetch`) into `ci.yml` after `validate-manifests`, and add `make catalog-validate`.

- [ ] Tests (table): first release creates a row; second release moves the old one into `releases`; the five-release cap; same-version re-release replaces rather than duplicates; tag/manifest version mismatch is refused; validate catches a manifest/catalog disagreement, a missing meta entry, and a missing screenshot under `-community`; `-fetch` against an `httptest` server catches a sha mismatch.
- [ ] Commit `feat(tools): maintain catalog.json from releases`.

### Task 4 (sysc-plugins): release workflow and authoring guide

**Files:** create `.github/workflows/release.yml` and `docs/publishing.md`; modify `README.md` with a short "Releasing and third-party catalogs" section linking the guide.

**Workflow:** triggered by `push: tags: ['*-v*']`, with permissions `contents: write` and `pull-requests: write`.
- Job **build**, a matrix over `amd64` and `arm64`: parse `<dir>` and `<version>` from the tag, run `go run ./tools/catalog package`, and upload the dist file as a workflow artifact.
- Job **release**, which needs **build**: download the artifacts, then `gh release create "$TAG" dist/* --title "$TAG" --notes "..." --verify-tag`.
- Job **catalog**, which needs **release**:
  1. Check out `main`.
  2. Run `go run ./tools/catalog update -tag "$TAG" -dist dist` and then `go run ./tools/catalog validate -fetch`.
  3. `git switch -c catalog/$TAG`, commit as `github-actions[bot]`, and push.
  4. `gh pr create --base main --title "catalog: $TAG" --body "Generated by the release workflow for $TAG."`

Actions stay at the majors already used in `ci.yml` (`actions/checkout@v4`, `actions/setup-go@v5`); upload/download use `actions/upload-artifact@v4` and `actions/download-artifact@v4`.

**`docs/publishing.md`** covers:
- the tag scheme;
- what goes in an asset;
- `catalog-meta.json` fields;
- running `make catalog-validate`;
- screenshot guidance (a representative state, any aspect ratio, at most 2 MiB and 1920×1080, required for the community catalog);
- how a third party publishes their own source: copy `tools/catalog` and the workflow, and keep `catalog.json` at the root of the default branch;
- that `git archive` output is accepted.

- [ ] Validate the workflow locally with `actionlint` if installed. Otherwise note in the report that it is unvalidated until the first tag.
- [ ] Commit `feat(ci): release plugins from per-plugin tags`.
- [ ] The owner approves merging the `sysc-plugins` branch to `main` and pushing.

### Task 5: first release and the live gate

- [ ] **Owner approval required** to push `timer-v1.4.0`. Push it, then watch the run to completion with `gh run watch`, or by polling the Actions API over HTTPS if `gh` is unauthenticated locally, and report the run URL.
- [ ] Confirm the release has both tarballs and that the catalog PR validates. **The owner merges the catalog PR.**
- [ ] Live gate on DP-1, with the owner's approval to stop `sysc-shell.service` as in tranche 1:
  - Back up the config.
  - Set Timer's user-root symlink aside.
  - Run the shell with no `plugins.sources` override, so it reads the built-in `sysc` source from GitHub.
  - `plugins.store` lists Timer 1.4.0 `available` from `sysc`.
  - `plugins.install {"source":"sysc","id":"org.sysc.timer"}` installs it; the process runs from `~/.local/share/sysc-shell/plugins/org.sysc.timer`; the bar screenshot shows the widget.
  - `plugins.remove` removes it.
  - Restore the config, the symlink and the service, and verify each.
- [ ] Record the gate on sysc-509 and close it. sysc-508 becomes ready.
