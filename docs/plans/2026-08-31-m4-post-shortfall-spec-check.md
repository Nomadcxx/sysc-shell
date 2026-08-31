# M4 Post-Shortfall Spec Check

Date: 2026-08-31
Commit: polish on `fix/m4-polish` (after `b4c4bc6` closed sysc-38–40)
Documents: 4A D1–D13, 4B D1–D10, previous sweeps, `2026-08-31-m4-spec-shortfall-corrections.md`

Live Niri matrices remain owner-deferred. M3 defects `sysc-31`–`sysc-35` stay out of scope.

## Check of sysc-38 / 39 / 40

| Issue | Spec | Landed at `b4c4bc6` | After this polish |
|---|---|---|---|
| sysc-38 | D5 standing detection; D7 glyph+label+bar, fade+slide | Process-lifetime leases; `Show` drops `mu` before aux; `set-mute`; reveal ticks; chrome is an accent square + dash track + bar | Hide also drops `mu` before aux. Chrome is still geometric stand-ins, not icon-font + shaped text. |
| sysc-39 | D9 XDG HookWrite, kitty SIGUSR1, ours-only, single-flight supersede | XDG paths, `/proc` SIGUSR1, first error returned | Supersede now reruns the **latest** home/enabled/tokens, not the in-flight call. |
| sysc-40 | D2 live widgets + item lists; virtual list consumer | `DefaultFor(cfg)`, `bar.items.*`, `KindVirtualList`+`Item` | `Focusables` walks `Item`; settings no longer pre-fills `Children`. |

## Verdict

4A still holds. 4B's three filed shortfalls hold. Remaining spec gaps are ceilings or operator surface, not reopen of 38–40:

- **D7 label/glyph** is painted as rounded rects, not `render.TextRenderer` / icon font. Tests prove regions, not glyphs. Accept until a text consumer is wired into OSD.
- **D5 prose** still says `SetMute` via `set-volume`; the quality finding and the code use `set-mute`. The code is the live contract.
- **Live** `wpctl` from a terminal, niri include hot-reload, and keyboard fall-through stay unrun. `sysc-3` stays open.
- **`status`** now includes version, open panels, matugen presence, audio/brightness, and template enables (4A D12 + 4B IPC).

Scheme remains a free string. High-contrast is under Accessibility (4A) and duplicated as a generation flag. Not defects.

`sysc-12` is Milestone 7 research, not a 4A/4B ship item.
