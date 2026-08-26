# sysc-shell Plan-Audit Handover

Date: 2026-08-27

## Assignment

Audit the `sysc-shell` architecture and plans before product implementation starts. Find factual errors, missing protocol requirements, unsafe sequencing, unnecessary work, and decisions that would force a rewrite later.

You may edit project documentation. Do not write Go code, generate protocol bindings, add dependencies, or start an implementation milestone.

The architecture baseline is commit `97b9249` (`docs: define sysc-shell architecture`). Work from the current branch that contains this handover. Read these files in full:

- `AGENTS.md`
- `README.md`
- `docs/prior-art.md`
- `docs/roadmap.md`
- `docs/plans/2026-08-26-sysc-shell-design.md`
- `docs/plans/2026-08-26-architectural-proof.md`
- `docs/plans/2026-08-26-development-orchestration.md`

Treat Noctalia v5 and DMS as behavior references. The product constraints remain fixed unless the owner changes them:

- Go-first;
- no C++, Rust, Lua, Luau, Qt, QML, or Quickshell;
- Niri first;
- no lock screen or compositor;
- no Noctalia or DMS compatibility contract;
- one repository until a component has a second consumer and a stable API.

## Authority and edit rules

Fix a documentation issue when current source or protocol evidence gives one clear answer. Examples include an invalid API call, omitted Wayland protocol, wrong event shape, impossible test step, unsafe resource lifetime, or contradictory milestone gate.

Do not make a product decision when two viable options have different user-visible, security, dependency, or long-term maintenance costs. Record those cases as decisions for the owner. Give a recommendation and the smallest useful set of alternatives.

Keep the proof narrow. Do not pull multi-output management, general controls, plugin implementation, GPU rendering, or broad service work into Milestone 1 unless the proof cannot validate its stated architecture without it.

Keep citations precise. For a repository finding, cite a file and heading or line. For upstream behavior, record the URL, commit or release, and relevant file, symbol, or protocol section. Label claims as verified, inferred, or unverified.

Keep research checkouts and generated experiments under `/tmp`. Do not create a module, dependency file, generated binding, or scratch artifact in this repository.

## Required audit passes

### 1. Dependency and toolchain pass

Verify that each pinned commit exists, licenses permit the planned use, module paths resolve, and the documented APIs match those commits. Check the installed Go version and whether the planned module directive and dependency versions work together.

Inspect `dankgo` rather than assuming that its generated types or dispatch model resemble `go-wayland`, `libwayland`, or another client. Confirm the scanner command, generated package API, socket ownership, event dispatch, request flushing, error propagation, and cancellation mechanics.

Inspect `go-text/typesetting` at the pin. Confirm the parsing, shaping, outline, metrics, direction, script, language, and rasterisation calls used by Task 3. Identify a redistribution-safe joined-script fixture or replace that step with a reproducible system-font gate.

### 2. Wayland protocol pass

Trace the complete request and event order for registry discovery, output identification, layer-surface creation, the initial empty commit, configure acknowledgement, buffer creation, attach, damage, frame callbacks, input, reconfigure, close, and shutdown.

Check every global's minimum version and whether the plan treats required and optional globals correctly. Verify protocol XML provenance and generation reproducibility.

Review object and memory lifetimes. A compositor may retain a `wl_buffer` after the surface reconfigures or starts shutting down. Confirm that the plan cannot reuse, unmap, close, or resize storage while a buffer remains busy.

### 3. Output, scale, and coordinate pass

Trace four coordinate spaces without collapsing them into one integer:

- Niri output identity;
- Wayland surface logical coordinates;
- fractional scale in 120ths;
- physical buffer pixels.

Verify rounding, viewport state, buffer dimensions, damage coordinates, pointer coordinates, exclusive-zone units, and reconfiguration behavior. Confirm the exact Niri-supported path for selecting a connector such as `DP-1` and obtaining the matching `wl_output` object.

### 4. Niri IPC pass

Validate Task 8 against the installed Niri version and the current upstream protocol. Check the initial request encoding, response framing, event envelope names, payloads, initial-state behavior, unknown-event handling, output naming, workspace identity, reconnect behavior, and cancellation.

Decide whether the event stream provides a complete initial workspace snapshot. If it does not, specify the smallest initial query plus event-stream reconciliation sequence that cannot lose an event between them.

### 5. UI, text, and renderer pass

Follow one state change from a Niri event or click through invalidation, layout, shaping, painting, frame scheduling, damage, and commit. Confirm that the packages agree on logical versus physical dimensions and that hit testing uses the same arranged geometry as painting.

Challenge types and interfaces that discard information or exist only to support a test fake. Keep an interface only when the platform boundary needs it. Check arithmetic overflow, stride validation, alpha premultiplication, clipping, glyph baselines, bidi behavior, font fallback assumptions, and deterministic test inputs.

### 6. Execution and acceptance pass

Check that each task can start from the previous commit, has a meaningful failing check, and ends with enough proof for the next task. Remove temporary work whose only purpose is to be deleted two tasks later when a smaller integration sequence covers the same risk.

Separate unit evidence from live Niri evidence. Make live procedures repeatable and safe for the operator's active session. Include restore steps for any scale or output configuration change.

### 7. Roadmap pass

Check whether the architectural proof establishes the contracts required by the per-output bar without building a miniature general-purpose toolkit. Review later milestones for hidden foundational work in accessibility, font discovery, image decoding, animation, clipboard handling, notifications, system tray behavior, plugin isolation, and configuration persistence.

Do not expand later milestones into implementation plans. Record missing design gates where the current roadmap would otherwise imply that a complex subsystem is routine.

## Questions the audit must answer

Rank each answer as `blocker`, `fix before Milestone 1`, `fix before affected milestone`, or `safe to defer`.

### Architectural proof

1. How will `--output DP-1` map a Niri connector name to a specific `wl_output`? Does the proof require `zxdg_output_manager_v1`, a Niri-to-registry correlation, or a different selection contract?
2. The planned `App.Configure(width, height, scale int)` represents scale as an integer. Can it preserve Niri fractional scale in 120ths and keep layout, paint, buffers, damage, and input consistent?
3. What exact buffer-size rounding rule should the proof use for a logical size multiplied by fractional scale? Which viewport source and destination requests are required?
4. Does `dankgo` expose the primitives needed for a correct poll loop without concurrent Wayland access? Detail read preparation, dispatch, flush, wakeup, cancellation, and fatal socket-error handling.
5. Which Wayland globals and versions does the proof require on Niri? Can it degrade to integer `wl_output.scale` when fractional-scale or viewporter is absent, or should their absence fail the proof with a named error?
6. Can a shared-memory pool or mapping be destroyed while a submitted buffer is still busy? Define reconfigure and shutdown ownership for old buffer generations.
7. Does the planned two-slot scheduler handle frame completion and buffer release arriving in either order, multiple invalidations, configure events during a pending frame, and a surface close?
8. Does the chosen `wl_shm` format and byte layout match host endianness and the compositor's advertised formats? Is `ARGB8888` guaranteed or must the client select from advertised formats?
9. How does the pointer path handle seat capability changes, enter and leave, fixed-point coordinates, button press versus release, and destruction of a removed pointer capability?
10. Does Niri's event stream send enough initial state for the first label? If not, how does the client avoid a race between an initial query and event subscription?
11. Do the Niri JSON fixtures and proposed structs match the installed and pinned Niri protocol? Which fields can change without breaking decoding?
12. Does `go-text/typesetting` at the chosen commit expose the exact outline and metric data assumed by Task 3? What licensed font proves joined-script shaping and rasterisation in CI?
13. Does the proof need bidi paragraph segmentation, or can it qualify one explicitly directed shaping run without claiming general bidi support?
14. Can the proof's UI tree paint at physical resolution while preserving logical bounds for layout and hit testing, or does the renderer need an explicit scale transform?
15. Is the `App` interface a real platform boundary or scaffolding for one production implementation and one fake? Would function callbacks or a concrete proof owner reduce lifecycle ambiguity?
16. The proof has a clickable button but requests no keyboard focus. Is pointer-only interaction acceptable for a non-production architecture probe, and what accessibility work must become a gate before interactive controls ship?
17. Does Task 7's temporary flat-color application save enough integration risk to justify code that Task 9 removes? If yes, define the smallest retained smoke path; if no, merge the integration sequence.
18. Can the live scale test run without risking the operator's active Niri configuration? Supply commands, backup or restore steps, and a non-mutating alternative when another scaled output already exists.

### Stable bar and later milestones

19. Which output events come from Wayland and which come from Niri? What source owns connector identity, scale, transform, mode, focused output, and hotplug reconciliation when the sources disagree or arrive in different orders?
20. When a bar exists on every output, what invariant prevents duplicate hosts during registry churn, reconnect, or output renaming?
21. What minimum font discovery and fallback strategy supports real bar text without turning the proof into a font-management project?
22. At which milestone must keyboard navigation, focus, screen-reader semantics, reduced motion, and high-contrast behavior become acceptance gates?
23. What image formats and decoders will icons, weather, tray items, notifications, and album art require? Which standard or existing Go packages cover them?
24. Does the plugin capability model limit only calls into the host, or does it promise OS-level filesystem, network, process, and D-Bus isolation? State the threat model before protocol design begins.
25. What prevents a plugin's rapid valid updates or deep node tree from exhausting CPU, memory, layout time, or redraw bandwidth after it passes message-size validation?
26. Which later features need protocols or services absent from the roadmap, such as data-control, primary selection, screencopy, activation, foreign-toplevel, idle-inhibit, notification D-Bus interfaces, StatusNotifierItem, or AT-SPI?
27. Does keeping gSlapper external require a stable supervision and IPC contract earlier than the wallpaper milestone suggests?
28. Which Noctalia or DMS behaviors may be copied as requirements, and which source-level adaptations require license notices or clean-room reimplementation?

## Deliverables

Create `docs/plans/2026-08-27-plan-audit-report.md` with:

1. an overall verdict: `ready`, `ready after listed fixes`, or `not ready`;
2. a table of findings with severity, evidence, affected plan section, and resolution;
3. answers to all 28 questions, combining answers only when one piece of evidence covers several;
4. documentation changes made during the audit;
5. unresolved owner decisions with a recommendation;
6. research that could not be completed and the exact reason;
7. a revised pre-implementation gate.

Edit the existing documents when the evidence supports one answer. Add the audit report to `README.md`. Do not turn the report into a second architecture document; keep settled detail in the owning plan and link to it.

## Verification and handoff

Stage the intended documentation before the final integrity check:

```bash
git add README.md docs
git diff --cached --check
git status --short
```

Verify every relative Markdown link and confirm that the repository still contains no product code, generated bindings, or dependency files.

Commit all audit documentation changes in one commit:

```bash
git commit -m "docs: audit sysc-shell plans"
```

Return:

- the verdict;
- blocker and pre-Milestone-1 findings;
- owner decisions still required;
- files changed;
- verification commands and results;
- the commit hash.
