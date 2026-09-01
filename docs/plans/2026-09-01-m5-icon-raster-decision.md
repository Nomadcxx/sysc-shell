# Milestone 5 Icon Raster Decision

Status: decided 2026-09-01, applies to Milestone 5
Plan: [notifications foundation](2026-08-30-notifications-foundation.md), Task 3
Design: [notifications and tray integration](2026-08-31-notifications-and-tray-integration-design.md)

## Decision

Milestone 5 decodes **raster icons only**. No SVG rasterizer is pinned.

Task 3 required selecting and recording a bounded pure-Go SVG dependency before implementation, or
stopping. Neither was chosen blindly: the maintainer selected raster-only after review.

## Why

Both wire formats already carry raster data. `sysc-notify` sends PNG in `protocol.Image`; `sysc-tray`
sends ARGB32 pixmaps. SVG matters only when resolving an icon *name* through an XDG theme.

The candidate parsers are unattractive for this job. `srwiley/oksvg` with `rasterx` is the usual pure-Go
choice, but neither enforces resource limits, so the shell would need to wrap them in its own caps to
meet the integration design's bounds, and both are lightly maintained. That is new third-party parsing
of untrusted files from disk, on the shell's own process, for a case the wire formats mostly cover.

## What this costs

A theme that ships **only** SVG for a name resolves to nothing, and the caller draws its own placeholder.
Papirus is the common example. Themes that ship PNG alongside SVG, including hicolor and Adwaita, are
unaffected.

This deviates from one line of the `sysc-tray` v0.1 plan's qualification matrix, "theme SVG and pixmap
fallback". The pixmap half is covered; the SVG half is deliberately not, and the qualification record
must say so rather than quietly pass.

## Where it lives

`internal/icons` is the single shared resolver and decode worker. Notifications and the tray both use it;
neither has an icon path of its own.

`Resolver.Resolve` accepts `.png` and `.xpm`, walks theme inheritance with a cycle guard, and prefers an
exact size, then the smallest icon at least as large, then the largest available. A `scalable/`
directory reports size zero because it holds nothing this resolver can read.

Adding SVG later means extending `rasterExtensions` and `Resolve`, plus a rasterizer in the worker. It
does not mean a second icon path.

## Bounds

| Resource | Limit |
|---|---:|
| Pending decode jobs | 32 |
| Icon file | 8 MiB |
| Decoded source edge | 4096 |
| Cache | 256 entries / 32 MiB |

Dimensions are checked from the image header before the decoder allocates, so a hostile header cannot
force a large allocation. A malformed or missing icon publishes no raster and is not cached; the node
keeps the box it measured and the caller draws its placeholder.
