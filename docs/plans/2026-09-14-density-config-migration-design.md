# Density configuration migration design

Date: 2026-09-14

## Problem

The Noctalia parity re-base replaced the 48 px `standard` density row with a
31 px `default` row. `MetricsFor` currently aliases `standard` to `default`, so
an existing sparse configuration that names neither a preset nor a density
silently shrinks its established bar from height/padding/spacing `48/6/4` to
`31/2/4` after an upgrade.

The sparse wire format does not itself distinguish an old omitted default from
a configuration newly written with the current default. The migration must
preserve that distinction without adding a schema version that an older binary
would reject or turning bar geometry into permanent explicit overrides.

## Decision

The theme package keeps `default` as the current 31 px reference row and
restores `standard` as a recognized, hidden compatibility row with the complete
pre-rebase metrics. `Densities` and the settings registry continue to expose
only `mini`, `compact`, `default`, `comfortable`, and `spacious`.

The `standard` preset continues to select `default`; it does not become the
compatibility mechanism. Consequently `Default()` and a missing configuration
file retain the intended 31 px bar.

At the configuration boundary, parsing an existing document with neither
`theme.preset` nor `theme.density` selects the hidden `standard` row before bar
geometry is derived. An explicit preset or density always wins. In particular,
`preset: "standard"` means the current standard preset and resolves to the
31 px `default` row, while `density: "standard"` deliberately requests the
legacy compatibility row.

The sparse writer records `preset: "standard"` when writing a configuration
whose standard preset still uses the `default` density. This semantically true
field is the durable provenance marker that prevents a newly written 31 px
configuration from being mistaken for an old sparse document on reload. A
loaded compatibility configuration differs from its preset by density, so the
writer records `density: "standard"` and preserves it across later writes.

## Why this boundary

Making the standard preset use the compatibility row would preserve upgrades
but also return fresh installations to 48 px and contradict the approved five
row parity model. A new schema version would distinguish generations cleanly,
but older binaries reject unknown fields. Explicit bar dimensions are accepted
by old binaries, but they pin geometry and stop later density changes from
deriving the bar.

Using the existing preset and density vocabulary keeps ownership coherent:
theme owns the two metric rows, config owns wire-generation provenance, and
the shell continues to consume one resolved composition.

## Compatibility and rollback

Older binaries accept both `preset: "standard"` and `density: "standard"`.
They may render a newly created current-default configuration at their own
48 px standard preset after rollback, but they do not reject or corrupt it.
Pre-rebase configurations retain their established geometry in both versions.

An old hand-written configuration that explicitly selected
`preset: "standard"` follows preset semantics and remains on the current
31 px row. This is intentional: only an omitted selector is treated as the
historical implicit default.

## Proof

Focused configuration and theme tests cover:

- `Default()` and a missing file resolving to `31/2/4`;
- an existing selector-free document resolving to `48/6/4`;
- explicit `preset: "standard"` resolving to the current row;
- explicit `density: "standard"` resolving to the compatibility row;
- writing and reloading both current and compatibility configurations without
  changing geometry; and
- the settings density list containing only the five current rows.

Wayland surface tests then assert the derived current and compatibility bar
heights. Existing preset-change tests continue to prove that the migration does
not pin bar geometry.
