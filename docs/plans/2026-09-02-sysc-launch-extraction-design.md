# sysc-launch Repository Extraction — Design

Date: 2026-09-02. Status lives in bd (`sysc-99`).

## Goal

Extract the launcher engine from `sysc-shell` into the empty public repository
`github.com/Nomadcxx/sysc-launch` without losing its commit history, tests,
usage data, or the merged Milestone 5 notification and tray implementation.
The first release also provides a narrow diagnostic CLI.

## Repository boundary

`sysc-launch` owns presentation-neutral launcher behavior:

- XDG desktop-entry scanning and exclusion policy
- Exec expansion, terminal policy, desktop actions, and Niri argv activation
- fzf field-weighted ranking and the capped Elephant-derived usage boost
- provider-prefix routing
- usage-history persistence
- collector and query-worker concurrency
- `cmd/sysc-launch`

`sysc-shell` continues to own every shell concern:

- the Niri layer-shell panel and its placement
- retained UI nodes, icons, painting, and the `OnPrimary` token
- panel keyboard, pointer, and IME projection
- shell IPC panel registration and the Super+Space binding
- process-wide interactive-root ownership

The public module is rooted at `github.com/Nomadcxx/sysc-launch` and uses
package name `launcher`. The shell imports that module directly. No
`sysc-shell/internal` package may appear in the extracted module.

## History-preserving extraction

Create the repository from a local clone of the merged shell history and use
`git filter-repo` to retain `internal/launcher`, renamed to the module root.
This preserves the launcher implementation commits and their original
Nomadcxx authorship while dropping unrelated shell and Milestone 5 files.
Follow-up commits add the module boundary, public API, CLI, README, and license.

A fresh-copy or squash extraction is rejected because it discards the
reviewable implementation sequence. A daemon/process boundary is rejected
because v1 needs no protocol, supervision, or second runtime owner.

The existing local safety refs
`safety/pre-launcher-main-20260902` and
`safety/launcher-v1-20260902` remain until the release tag and final shell pin
both verify.

## Public API

The module exports the smallest useful engine surface:

- `Entry`, `Action`, `Result`, and `Provider`
- `Service` and `ServiceConfig`
- `History`, `OpenHistory`, and `DefaultHistory`

Configuration injection fields use public function signatures so downstream
tests can supply scans, clocks, runners, path lookup, environment, and logging.
The service keeps its asynchronous latest-wins result channel for shell UI use.

`Result.Icon` and the deferred `ui.Node` closure are removed. Entries retain
presentation-neutral `IconName`, and provider overview entries retain glyph
metadata. The shell decides how either is painted. The current shell panel
does not consume `Result.Icon`, so this changes no visible v1 behavior.

## Usage-history continuity

The CLI default is:

`$XDG_STATE_HOME/sysc-launch/history.gob`

with the normal `$HOME/.local/state` fallback. `sysc-shell` explicitly opens
its existing path:

`$XDG_STATE_HOME/sysc-shell/launcher/history.gob`

No migration or shared file is introduced. Existing shell ranking history
continues unchanged, while the standalone diagnostic CLI owns independent
history.

## CLI

The binary has two commands:

- `sysc-launch query [QUERY]` emits JSON results to standard output.
- `sysc-launch launch DESKTOP_ID [ACTION_ID]` activates through
  `niri msg action spawn -- <argv>`.

It waits for the initial desktop snapshot before querying or activating.
Diagnostics go to standard error, invalid arguments and runtime failures exit
nonzero, and all waits are bounded. It adds no daemon and owns no Wayland
surface.

## Licensing and attribution

The repository carries GPL-3.0-only licensing because the usage-history
algorithm is ported from Elephant GPL-3. The source keeps its existing
attribution comment. Commit authorship remains
`Nomadcxx <noovie@gmail.com>` and commit messages contain no AI, Cursor, or
assistant attribution.

## Integration and release

Before publishing, use a temporary untracked `go.work` to test the local
`sysc-shell` and `sysc-launch` modules together. Do not commit a `replace`
directive.

After both repositories pass their normal and race suites:

1. Push `sysc-launch` main to the empty GitHub repository.
2. Create and push immutable tag `v0.1.0`.
3. Pin `github.com/Nomadcxx/sysc-launch@v0.1.0` in `sysc-shell`.
4. Remove `internal/launcher` and update shell imports.
5. Re-run shell tests, race tests, vet, and build without `go.work`.

Publishing the new repository and tag is part of this extraction.
`sysc-shell` remains local until the owner separately requests a push.

## Verification

The extracted package retains every launcher unit test and adds focused tests
for its public API, independent history paths, CLI JSON output, argument
validation, bounded waits, and launch errors. The shell retains panel
projection and interaction tests against the external package.

Completion requires:

- no deleted or modified Milestone 5 core file beyond dependency-driven import
  changes
- no `sysc-shell/internal` import in `sysc-launch`
- no committed `replace` directive or `go.work`
- clean `go test`, `go test -race`, `go vet`, and `go build` in both modules
- `v0.1.0` resolvable from GitHub
- both local safety refs still present
