# sysc-shell agent guide

## Product constraints

- Use Go as the application language.
- Do not add C++, Rust, Lua, Luau, Qt, QML, or Quickshell.
- A narrow C-library boundary is acceptable for text or graphics when a measured requirement defeats the Go implementation. Ask before adding handwritten C or CGO.
- Support Niri first. Other compositors require a separate approved design.
- Do not implement a lock screen or a compositor.
- Do not preserve Noctalia or DMS configuration, plugin, or QML compatibility.
- Treat Noctalia v5 and DMS as behavior and architecture references.

## Engineering rules

- Stop at the first working rung: existing project code, Go standard library, native Linux service, pinned dependency, then new code.
- Keep Wayland types inside `internal/platform/wayland` and Niri wire types inside `internal/platform/niri`.
- Pin `github.com/Nomadcxx/sysc-wayland@v0.2.0`; do not import `dankgo` from shell code.
- Keep the Wayland dispatch loop on one goroutine. Other goroutines submit commands and receive immutable state updates through channels.
- Draw only after invalidation. Respect frame callbacks and buffer release events.
- Add one focused runnable check for non-trivial logic. Use table tests for pure layout and protocol code.
- Build first-party widgets in Go. Run third-party plugins as supervised processes; do not use Go binary plugins.
- Add UI primitives only for an approved shell component. Do not build a general application toolkit.
- Keep one repository until a component has a second real consumer and a stable API.
- Pin unstable dependencies by version or commit. Record the reason in the design or implementation plan.

## Tracking

- **bd (beads) is the only tracker.** Do not use markdown TODO lists, task lists in documents, or a
  session todo. Those are what lost the original Milestone 2 implementation plan.
- `bd ready` answers "what can I work on now"; `bd blocked` shows what is gated and by what. Prefer them
  over re-deriving state by reading documents.
- Claim work with `bd update <id> --status in_progress`, close it with `bd close <id> --reason "..."`.
  Record discovered work as `bd create "..." --deps discovered-from:<id>`.
- Commit `.beads/issues.jsonl` in the same commit as the code or documents it describes. The SQLite
  database is gitignored; the JSONL is the source of truth in git.
- **Status lives in bd, not in documents.** A design or plan states its decisions; bd states whether the
  work is done, in flight, or blocked. Do not duplicate status into a document header, where it drifts.
- Cross-repository gates are modelled as issues in this repository — `sysc-metrics` needing a release tag,
  for example — so a single graph holds every blocker.

## Designs and plans

- Every design, plan and handover is registered in `docs/plans/README.md`. Add a row in the same commit
  that adds a document.
- **Commit the plan before executing it.** Milestone 2's 16-task plan was executed and never committed; it
  no longer exists on any branch, and that implementation can no longer be reviewed against its own plan.
- **Land design and plan documents on `main` as docs-only commits, not on a milestone branch.** They
  describe intent, not shipped code, and they do not depend on the code. Holding them on a branch means no
  other branch can see them and they die if the branch is abandoned.
- Naming is `YYYY-MM-DD-<topic>[-<kind>].md`; the kinds are listed in `docs/plans/README.md`.

## Workflow

1. Read the approved design and the roadmap gate for the current milestone.
2. Work from the milestone implementation plan in a dedicated worktree.
3. Write the smallest failing check before implementation.
4. Run unit checks after each task and the live Niri gate before claiming a milestone.
5. Record measurements and unresolved hardware behavior in the milestone handoff.
6. Stop when the milestone exit gate passes. Do not pull later roadmap work forward.
