# Contributing

The desktop shell for Niri: bars, panels, the launcher and the settings, drawn straight to Wayland.

This document says what a change has to clear and where the work is tracked.

The agent guide is `AGENTS.md`, and it carries the rules this document repeats.

## The commit gate

Every commit must pass these commands from the root of the tree:

```sh
gofmt -w . && test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=1 ./...
go build ./...
git diff --exit-code -- go.mod go.sum
```

The last one says the build did not rewrite `go.mod` or `go.sum`. A change to either is a change to the pins, and a pin change is a decision rather than a by-product of a build.

A gate script must record each step's real exit code. One trailing exit code can belong to a harmless
command and report success on a failing suite.

## The tracker

`bd` is the only tracker. Not a markdown list of tasks, not a list in a document, not a list in a
session. A task that lives in a document drifts from the tree and is lost with the document.

- `bd ready` answers what can be worked on now, and `bd blocked` shows what is gated and by what.
- Claim work with `bd update <id> --status in_progress` and close it with
  `bd close <id> --reason "..."`.
- Record work you discover as `bd create "..." --deps discovered-from:<id>`.

The tracker of this repository is here: `.beads/` holds the database and the export, and `bd` reads it from the primary checkout.

## Where plans live

Plans, designs and handovers are not in this repository. They are gitignored, and they live in the
primary checkout of the repository they govern, under `docs/plans/`. A worktree does not have a copy:
read and write them at the primary checkout's path.

- The name is `YYYY-MM-DD-<topic>[-<kind>].md`.
- Every one of them is registered in `docs/plans/README.md`.
- A plan is written before the work it governs starts.

## Releasing

The order is pull request, CI green, merge, tag. A tag cut on a side branch leaves the release commit
outside `main`, and CI never evaluates it.

Release notes are generated from the commit history: the release workflow creates the release with `gh release create --generate-notes`, and the notes of a release are the commits between the tags. There is no changelog file to maintain, because a changelog nobody maintains is worse than none: the documents of this organisation drifted once already.

## Before diagnosing a test timeout

Check the machine first: `uptime`, and look for processes an earlier run left behind. A goroutine dump
is the second tool, not the first.

## Settled-state checks

A test asserts on a settled state, not on a guess about when the work will be done. Wait for the
event that means the work finished: a channel the worker closes, a cached value the worker wrote,
a request the shell has already handled. A sleep that is long enough on a quiet machine is a
timeout waiting to happen on a busy one.

Two rules follow from that.

- When the work is done by another goroutine, wait for that goroutine. Close the stop channel and
  wait for the done channel, or read the cached value under the lock that guards it.
- When the test genuinely needs time to pass, use `testing/synctest`. The bubble owns the clock, so
  the assertion is on the virtual timeline and the test finishes in real time. A bubble has one
  restriction: a test inside it cannot call `t.Parallel()`.

A deadline is a backstop, never the assertion. When a wait does expire, the failure message has to
name the step that did not settle, so the next reader knows which one to fix.

## Check the tree you are changing

Confirm a constraint against `git show origin/main:AGENTS.md` before acting on it when the
working copy may lag. Verify every claim against the `origin/main` of the repository the
claim is about, never a stale local clone. A correction sourced from a checkout 26 commits behind made
a document more wrong.
