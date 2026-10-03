# sysc-shell toast-host & Wayland crash audit report

- **Date:** 2026-10-03
- **Target:** `github.com/Nomadcxx/sysc-shell`
- **Revision audited:** `d7f4ff92` (`main`); deployed build `818717c0` (`test/desktop-center-env-20261002`)
- **Trigger:** intermittent shell exit under rapid-succession notification bursts (mainly the `ai-usage` plugin)
- **Companion:** `sysc-notify` presenter/history audit (separate PR)

## 0. Evidence → Finding → Path

| Evidence | Finding | Path |
|---|---|---|
| `wl_display.error: object=1 code=1: invalid arguments for wl_compositor#6.create_region` | **F1** The fatal is a **server-rejected `create_region` `new_id`**, not a shell panic and not a daemon panic | `internal/platform/wayland/regions.go:82` `applyInputRects` |
| `go.mod` on deployed branch pins `sysc-wayland v0.3.1`; `main` pins `v0.2.2`; 8 unreleased decoder fixes sit between `v0.3.1` and `sysc-wayland@main` | **F2 (root-cause lead)** v0.3.1's client object-ID state can desync on a mis-decoded event, so a later `create_region` gets a bad `new_id` | `sysc-wayland` `baedfe9`…`b31ea23` |
| `internal/platform/wayland/client.go:1302` `dispatchAll` returns `Dispatch()` errors → `run` returns `o.fatal` → `os.Exit(1)` | **F3** A `wl_display.error` is **fatal by construction**; there is no `recover` | `internal/platform/wayland/client.go:1303`, `:266` `fail` |
| `aux_surface.go:125` `handleAux` default branch `o.fail(err)` for a no-Reply error | **F4** Non-stale aux errors with no reply are promoted to process-fatal; only `open` is contained | `internal/platform/wayland/aux_surface.go:125` |
| `regions.go:82` — `input.Destroy()` is skipped when `Add`/`SetInputRegion` errors | **F5** Region leak on the error path | `internal/platform/wayland/regions.go:82-92` |
| New regression test, 300-note burst + churn + output loss + resync + DND | **F6** In-bounds contract holds; the toast host is **not** the source of bad rects | `internal/shell/toastburst_audit_test.go` |

## 1. Crash signature

```
wl_display.error: object=1 code=1: invalid arguments for wl_compositor#6.create_region
destroy buffer 0/1: invalid arguments for wl_compositor#6.create_region
destroy pool: invalid arguments for wl_compositor#6.create_region
wayland: shutdown roundtrip: unable to get sync callback: invalid arguments for wl_compositor#6.create_region
```

- **No coredump, no Go panic.** The process exits `status=1`, i.e. a returned error, not a signal.
- The repeated identical lines are `ctx.fatalErr` cached after the **first** real error; the first `create_region` line is the trigger, the rest is the shutdown cascade.
- Error-string origin: `invalid arguments for %s#%u.%s` lives in `/usr/lib/libwayland-server.so.0.26.0` at offset `0xf868`, inside `wl_client_connection_data` — a demarshal/argument-validation failure on the **server** side.
- `code=1` = `WL_DISPLAY_ERROR_INVALID_METHOD`.

The shell dies because the compositor rejected the request the shell sent, and the shell has no containment for a display-level error.

## 2. Why the request is invalid — root-cause lead

`create_region` is sent by `applyInputRects` (`internal/platform/wayland/regions.go:82`). On the wire:

```
compositor.create_region(new_id) = 8-byte header + 4-byte new_id
```

The `new_id` comes from `Context.Register`, a monotonic `currentID` that skips IDs still present in the `objects` map. In `sysc-wayland v0.3.1` that map cannot reuse a live ID and `DeleteID` only runs on a server `delete_id` event. So a **correctly-decoded** stream cannot produce a duplicate `new_id`.

Therefore the corruption must come from a **mis-decoded inbound event that desyncs the object map / byte stream**. The deployed `v0.3.1` predates exactly this class of fix on `sysc-wayland` `main`:

| Commit | Fix |
|---|---|
| `011085c` | regenerate validated event decoders |
| `4541638` | validate event field lengths |
| `de0a543` | regenerate registry lifecycle |
| `d671af5` | mark destroyed registry zombie |
| `e7c6699` / `0daf30b` | pad event array offsets |
| `83b65df` | close parsed FDs on error |
| `baedfe9` | honor Wayland connection env |
| `b31ea23` / `b880999` | **register event `new_id` before the nil-handler return** |

`b31ea23` is the most direct match: *"A nil event handler returned before decoding new_id arguments, so server-allocated objects … were never registered. Later events on that id fatal the connection."* That is the same failure mode — an object-ID bookkeeping error that later surfaces as a rejected request.

**Status:** leading hypothesis, not yet proven end-to-end. Final proof needs a `WAYLAND_DEBUG=1` capture or a minimal client repro that shows the object map diverging from the server's.

## 3. Containment gaps (independent of root cause)

Even with the root cause fixed, the shell should not die on one bad downgraded request:

- **F3 — display error is fatal by construction.** `dispatchAll` (`client.go:1302`) returns `Dispatch()` errors, and a `wl_display.error` is delivered there. There is no recovery. This is *correct* per the Wayland protocol — after a display error the connection is dead — but it means any client-side desync is a hard process exit.
- **F4 — `handleAux` default branch promotes to fatal.** Errors that are not `errOutputGone`/`errAuxNotOpen` and arrive with no `Reply` call `o.fail(err)` (`aux_surface.go:125`), which exits the process. Only the `open` path is contained. A `create_region` failure surfaces as a `wl_display.error`, so it bypasses `failUnit`/`openAux` containment entirely.
- **F5 — region leak.** `applyInputRects` skips `input.Destroy()` when `Add` or `SetInputRegion` fails.

## 4. Ruled out: the toast host's region math

A regression test drives the host through every shape the burst produces — a 300-note snapshot, 300 churn deltas, output loss/return, a daemon resync (disconnect + fresh snapshot), and DND on/off — and checks every emitted input rect against the real output size:

```sh
cd sysc-shell
go test -race -count=1 -run TestToast ./internal/shell/
```

Both tests pass. No empty, negative-origin, or out-of-bounds rect is emitted at any stage. The rects the host feeds to `CreateRegion` are within bounds, so the rejected `create_region` is **not** caused by bad rectangle values — it is the `new_id` itself.

## 5. Verdict

1. The crash is a **shell-side Wayland protocol failure**, not a `sysc-notify` panic and not a shell logic panic.
2. The `create_region` `new_id` is corrupt because the v0.3.1 client's object-ID state desynced; the unreleased `sysc-wayland` decoder fixes are the leading cause.
3. The toast host's rect logic is exonerated.
4. `sysc-notify` contributed only by tearing down slow presenters under bursts (fixed in the companion PR).

## 6. Remediation

1. **Upgrade `sysc-wayland`** on the deployed branch to a revision containing `b31ea23` (ideally tag a `v0.3.2`). This is the primary fix.
2. **Contain aux errors** so a single surface failure cannot exit the process (`handleAux` default branch).
3. **Fix the region leak** in `applyInputRects` (`defer input.Destroy()` once created).
4. Ship the burst regression test to lock in the in-bounds contract.

## 7. Reproduction

```sh
cd sysc-shell
go test -race -count=1 -run TestToast ./internal/shell/
```

## 8. Follow-up

- Capture `WAYLAND_DEBUG=1` around a repro to confirm the object-ID divergence described in §2.
- Decide whether `sysc-notify`'s `MaxPresenterQueueMessages` should also be raised; the resync fix makes it non-urgent.
