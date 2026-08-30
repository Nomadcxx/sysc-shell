# Milestone 5 Design and Plan Audit Handover

Date: 2026-08-30

## Assignment

Audit the Milestone 5 Tranche 5A and 5B designs and implementation plans before product work starts.
Test their contracts against the existing shell architecture, the `sysc-notify` and `sysc-tray` service
designs, Niri behavior, and the approved Milestone 4 plans.

The auditor may edit M5 design and plan documents to correct verified defects. Do not implement shell,
notification-service, or tray-service product code during this audit.

Produce one report:

`/home/nomadx/sysc-shell/docs/plans/2026-08-30-notifications-and-tray-audit-report.md`

## Repository and workspace

- Primary checkout: `/home/nomadx/sysc-shell`
- Integration branch: `main`
- M5 source worktree:
  `/home/nomadx/.config/superpowers/worktrees/sysc-shell/milestone/notifications-tray`
- Source branch: `milestone/notifications-tray`

The source branch's documentation commit reached `main` through a cherry-pick, so the branch and `main`
have different commit identities. Create a fresh audit worktree from `main`; do not reset or reuse the
source worktree for audit commits.

Suggested setup:

```bash
cd /home/nomadx/sysc-shell
git status --short
git worktree add \
  /home/nomadx/.config/superpowers/worktrees/sysc-shell/audit/milestone-5 \
  -b audit/milestone-5 main
```

Preserve unexpected changes in the primary checkout. Do not run `bd` from an audit worktree. Record audit
findings in the report; the primary operator will add required issue edges after reviewing the verdict.

## Documents to audit

Read these shell documents in order:

1. Prior art:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-notifications-and-tray-prior-art.md`
2. Decisions D1 through D14:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-notifications-and-tray-research.md`
3. Tranche 5A design:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-notifications-foundation-design.md`
4. Tranche 5A plan, 13 tasks:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-notifications-foundation.md`
5. Notification persistence addendum:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-sysc-notify-persistence-design.md`
6. Tranche 5B design:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-tray-foundation-design.md`
7. Tranche 5B plan, 8 tasks:
   `/home/nomadx/sysc-shell/docs/plans/2026-08-30-tray-foundation.md`

Compare them with:

- `/home/nomadx/sysc-notify/docs/plans/2026-08-27-sysc-notify-design.md`
- `/home/nomadx/sysc-notify/docs/plans/2026-08-30-sysc-notify-persistence-design.md`
- `/home/nomadx/sysc-notify/docs/roadmap.md`
- `/home/nomadx/sysc-tray/docs/plans/2026-08-27-sysc-tray-design.md`
- `/home/nomadx/sysc-tray/docs/roadmap.md`
- `/home/nomadx/sysc-shell/docs/plans/2026-08-30-panel-foundation-design.md`
- `/home/nomadx/sysc-shell/docs/plans/2026-08-30-panel-foundation.md`
- `/home/nomadx/sysc-shell/docs/plans/2026-08-30-settings-osd-theme-catalog-design.md`
- `/home/nomadx/sysc-shell/docs/plans/2026-08-30-settings-osd-theme-catalog.md`
- `/home/nomadx/sysc-shell/docs/roadmap.md`

Use current source only to validate named APIs and ownership. M4 product code does not exist yet, so an
M5 snippet cannot claim compilation proof against future M4 symbols.

## Fixed project constraints

- Linux and Niri form the supported platform.
- Go owns project code. C++, Rust, Lua, Luau, Qt, QML, and Quickshell stay out.
- One goroutine owns the Wayland connection and every Wayland proxy.
- `wl_shm` remains the renderer until measured evidence supports another path.
- The shell acts as a presentation client for service-owned notification and tray sockets.
- `sysc-shell/ipc.v1.sock` remains the inbound CLI and hotkey server.
- The shell consumes tagged service releases; plans must not use local module replacements.
- Service absence, restart, malformed input, or a slow shell must not block D-Bus service ownership.
- M2 hardware qualification remains deferred and does not block design or implementation work.

Treat owner decisions D1 through D14 as fixed unless two decisions contradict each other or require an
unapproved security, dependency, or ownership change. Report such conflicts instead of selecting a new
product direction.

## Audit method

For each design decision and implementation task:

1. Trace the producer, transport, state owner, consumer, and failure path.
2. Compare every named type, method, file, dependency, and command with its source document or current
   repository state.
3. Check task ordering, TDD red-green steps, commit boundaries, and exit evidence.
4. Classify findings as Blocking, Significant, or Minor.
5. Cite each finding with a document path and line number.

Use these verdicts:

- **Proceed**: plans can execute after their recorded prerequisites.
- **Proceed after corrections**: named document fixes close the findings.
- **Redesign required**: an ownership, protocol, security, or platform assumption fails.

## Cross-repository contract audit

Prove that each service and shell plan agrees on:

- socket ownership, path, permissions, peer credentials, and version handshake;
- framing, message size, field types, optional fields, and unknown-field behavior;
- snapshot shape, sequence numbering, delta ordering, gap detection, and resync;
- reconnect backoff, cancellation, backpressure, and slow-client removal;
- stable notification and tray-item identity;
- capability negotiation for history, reply, images, menus, and future fields;
- service behavior with zero presentation clients;
- release and tagging prerequisites.

Check whether the persistence addendum changes the `sysc-notify` protocol version or capability set. The
audit must name the required service implementation plan if the current service repository lacks one.

Check the shell plans for invented wire fields. A field absent from the service design needs either a
service-design amendment or removal from the shell plan.

## Tranche 5A audit questions

### Image and icon trust boundaries

- Do PNG, JPEG, and raw `image-data` decoders cap encoded bytes, dimensions, stride, decoded memory, and
  work before allocation?
- Does raw image validation prevent integer overflow, negative dimensions, invalid row stride, and
  mismatched channel data?
- Does icon-theme lookup reject path traversal and unsafe symlink escapes while following freedesktop
  inheritance rules?
- Do cache keys include source identity, size, scale, and theme state without retaining unbounded images?
- Does the plan use the existing `golang.org/x/image` dependency without adding another decoder stack?

### Notification content and actions

- Does markup sanitization define its accepted subset and escape rejected elements and attributes?
- Do summary, body, action labels, reply text, and URLs have byte and display bounds?
- Do action invocation, dismissal, close reasons, inline reply, and default action match the service
  protocol?
- Does PID lineage focus a window only when one unambiguous Niri candidate exists?
- Can `/proc` races, PID reuse, missing processes, or permission failures change a notification action?

### Lifetime and surfaces

- Does one toast surface per output preserve output-global identity across hotplug and connector reuse?
- Do queue overflow, hover pause, DND, Critical urgency, replacement, and center-open state define timer
  ownership and expiry transitions?
- Does expiry use monotonic duration behavior and avoid one timer or goroutine per card where a process
  scheduler suffices?
- Do IPC goroutines send immutable state to the shell without touching Wayland proxies?
- Does inline reply request `OnDemand` keyboard only while the text field owns focus, then release it on
  submit, cancel, dismissal, output removal, and service loss?
- Do invalidation rules prevent a continuous frame loop while countdown and swipe animation remain
  correct under reduced motion?

### Persistence and privacy

- Does `sysc-notify` own atomic writes, recovery from corrupt state, retention, cap enforcement, and PNG
  sidecar garbage collection?
- Do transient and private notifications stay off disk according to the freedesktop hints?
- Do state directories and files use 0700 and 0600 without a permission-widening race?
- Does the snapshot distinguish active state from closed history after a service restart?
- Does the plan specify migration or rejection for future persistence schema versions?

## Tranche 5B audit questions

### Tray transport and identity

- Does the shell key items by unique bus owner and object path, not a reusable well-known name?
- Do registration, property deltas, owner loss, reconnect, and replacement preserve one item instance?
- Does the service own StatusNotifierWatcher and DBusMenu D-Bus work while the shell owns presentation?
- Do menu requests carry correlation and cancellation so a stale response cannot open for a removed item?

### Menu and popup safety

- Does Task 1 prove that Niri accepts an `xdg_popup` parented to the layer-shell bar before popup product
  code starts?
- Does the fallback use the approved M4 panel and menu vocabulary without creating a second menu system?
- Do menu decoding and rendering cap tree depth, item count, string size, icon size, and update rate?
- Do cycle detection and invalid parent IDs reject malformed DBusMenu trees?
- Do popup configure, grab serial, dismissal, output removal, and scale changes stay on the Wayland owner
  goroutine?
- Does keyboard navigation reach each enabled menu item and restore focus after dismissal?

### Bar and overflow behavior

- Does layout remain per output under mixed scale, transform, hotplug, and constrained width?
- Does overflow move items without duplicating activation targets or losing attention state?
- Do normal, attention, and overlay icons define composition order, scale, clipping, and cache bounds?
- Do left, middle, right, and wheel actions match the service contract and preserve event coordinates?
- Does the overflow drawer reuse M4 panel single-instance and accessibility rules?

## Dependency and sequencing audit

Confirm the plans encode these gates without relying on prose alone:

- 5A needs M4 panel and auxiliary-surface machinery plus the 4B text field for inline reply.
- 5A needs a tagged `sysc-notify` release with the agreed snapshot and persistence capabilities.
- 5B needs 5A's `internal/icons`, M4 panels, and a tagged `sysc-tray` release.
- 5B Task 1 gates its popup path; a failed live probe selects the named fallback before Task 6.

Check whether 5A's 13 tasks or 5B's 8 tasks contain task-sized units with multiple independent commit
boundaries. Recommend a split when a task cannot support a useful red-green cycle or focused review.

Identify shared-file conflicts between 5A, 5B, unfinished M4 work, and the service repositories. Each
cross-repository change needs its own plan, tests, release gate, and merge order.

## Test and evidence audit

Require tests for:

- hostile and truncated wire messages;
- reconnect with a sequence gap and snapshot replacement;
- service loss during action, reply, menu request, and image decode;
- bounded image, icon, markup, notification, menu, and queue inputs;
- output add, remove, scale, transform, and reconnect;
- keyboard focus transitions and pointer routing;
- reduced-motion behavior and idle rendering;
- persistence crash recovery and file permissions;
- popup probe success and fallback selection.

The live gate must cover two outputs where output-specific behavior matters. Keep host names, connector
names, notification content, screenshots, and machine measurements outside Git.

## Scope and dependency audit

Reject plans that introduce:

- a second Wayland owner or Niri connection;
- shell ownership of D-Bus notification or StatusNotifierWatcher names;
- local module replacements or untagged service dependencies;
- a general UI, plugin, service-registry, or dependency-injection framework;
- shell-side notification persistence;
- XEmbed tray support;
- C++, Rust, Lua, Qt, QML, or Quickshell;
- new dependencies for behavior covered by stdlib, `x/image`, `x/sys`, or pinned `sysc-wayland`.

Flag parity features whose cost exceeds a tranche boundary. Preserve D1's full-parity destination while
splitting execution when review and rollback need smaller units.

## Required output

The audit report must include:

1. Verdict for 5A and 5B separately.
2. Findings ordered by severity, each with path:line evidence, impact, and exact correction.
3. A cross-repository contract table showing the owner and source document for each wire field and
   behavior.
4. A dependency graph and merge order for `sysc-notify`, 5A, `sysc-tray`, and 5B.
5. Plan task-count and file-overlap findings.
6. Missing automated and live evidence.
7. Edits made during the audit, with commit hashes.
8. Questions that require owner decisions.

Apply mechanical and unambiguous documentation fixes in the audit branch. Leave disputed product choices
in the report. Do not start M5 implementation.
