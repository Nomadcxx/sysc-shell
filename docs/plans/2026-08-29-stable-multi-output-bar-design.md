# Stable Multi-Output Bar Design

Date: 2026-08-29
Status: Owner-approved

## Goal

Milestone 2 gives each ready Niri output one stable layer-shell bar. The shell must handle startup,
hotplug, unplug, reconnect, mixed scale, pointer focus, configuration reload, and shutdown without
sharing output-scoped state.

This milestone keeps the software-rendered `wl_shm` path. It does not add widgets, a plugin API, GPU
rendering, tray support, notifications, audio, MPRIS, or a lock screen.

## Reference geometry

DankMaterialShell and Noctalia v4 provide the visual and dimensional reference. The default tokens are:

- nominal height: 48 logical pixels;
- outer gap: 4 logical pixels;
- painted body: `48 - 2*4 = 40` logical pixels;
- layer-surface height and exclusive zone: `4 + 40 = 44` logical pixels;
- corner radius: 12 logical pixels.

The nominal 48px token does not reach Wayland. Niri configures a 44px-high surface and reserves 44px.
The renderer leaves the top and side gap transparent, then paints the 40px body with rounded corners.
The input region covers the whole surface so the screen edge remains clickable.

## Ownership

One goroutine owns `wl_display` and every Wayland proxy. `owner` owns shared globals, the seat, pointer
focus, the accepted configuration, and the host set.

Each `OutputHost` owns:

- its registry global identity and `wl_output` metadata;
- its resolved `config.Bar` policy;
- whether the configured background is opaque;
- its surface, layer role, viewport and fractional-scale proxy;
- configure and scale state, buffers, frame callback and scheduler;
- its shell callbacks and bounded close-recovery state.

The connector selects configuration but never identifies a host. A reconnected connector receives a new
registry global and a new host.

## Startup and output lifecycle

`wayland.Run` receives the complete validated initial configuration. A host becomes ready after
`wl_output.done` and a non-empty connector name.

The readiness transition resolves the connector policy and stores it on the host. A disabled policy
leaves the host idle without creating a surface. An enabled policy builds the shell model, creates the
layer surface, applies the host geometry, commits without a buffer, and waits for configure.

Hotplug uses the same readiness transition. Unplug marks the host dead before destroying its proxies and
removes its shell model. Layer-surface close recovery reuses the stored host policy and shell callbacks.

## Configuration reload

SIGHUP wakes the Wayland owner. The owner reads and validates the candidate, resolves a policy for each
live connector, and asks the shell registry to prepare replacement bar models for enabled outputs. The
registry builds every replacement before changing live state.

The owner then applies the prepared policies while no Wayland dispatch or rendering can interleave:

1. disable roles whose candidate policy disables them;
2. update geometry on mapped roles;
3. create roles that the candidate enables;
4. replace each host's policy and shell callbacks;
5. commit the prepared registry models and accepted configuration.

Parse, resolution, or model-construction errors reject the candidate without changing live state.
Wayland request errors are fatal because the process cannot roll back requests already sent to the
compositor. Shutdown and restart provide a clean recovery path instead of continuing with mixed state.

Reload may reset fixture-only interaction state such as the demo meter toggle. Workspace labels survive
because the registry owns them independently from bar models.

## Rendering and regions

The shell computes the logical body from the configured gap during configure. The renderer converts the
body and radius to physical buffer coordinates with the host's `scale120` value.

Each paint clears the reused shared-memory buffer to transparent, fills the rounded body, then draws its
children. Clearing prevents pixels from an older configuration or larger body surviving in the gap.

The opaque region is conservative. An opaque background declares the body minus its corner squares;
a translucent background declares no opaque region. The renderer and region builder use the same host
radius and body geometry.

## Error handling

- Invalid startup configuration stops before Wayland connection.
- Invalid reload candidates keep the accepted configuration and print the field-specific error.
- A host-scoped bind failure leaves other outputs running.
- A protocol or request failure after reload application begins stops the shell.
- Shutdown reports joined cleanup errors after releasing child proxies before parents.

## Automated proof

Focused tests must cover:

- disabled and overridden policies at readiness;
- two hosts retaining different geometry;
- hotplug after a reload using the accepted connector policy;
- reload replacing existing bar models and callbacks;
- rejected candidates leaving registry and host state unchanged;
- transparent gap, rounded corners, translucent backgrounds and fractional scale;
- existing lifecycle, pointer, layout, text, buffer and cleanup behavior under `-race`.

The automated gate is:

```bash
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
go build ./cmd/sysc-shell
gofmt -l .
```

## Live qualification

Automated tests cannot qualify compositor behavior. The live Niri matrix in
`tests/integration/README.md` remains the milestone exit gate. It requires two outputs, physical hotplug,
one rotatable output, mixed scale, pointer input, exclusive-zone checks, reload, restart, and a 60-minute
idle run.

The branch may merge after the automated gate and local review pass. The project must record Milestone 2
as unqualified until the live matrix passes.

## Stop condition

Stop after one correct bar exists on each enabled output and the reload, rendering, lifecycle and
automated contracts pass. Defer widget and service work to later milestones.
