# Niri hotkeys for sysc-shell panels

The compositor owns key bindings. The shell owns the panels. Add these to
`~/.config/niri/config.kdl` so Super+P, Super+M, and Super+X toggle the
clock, system-monitor, and session panels from anywhere. Media and brightness
keys ship with Tranche 4B's OSD.

```kdl
bind {
    Super+P { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"clock\"}"; }
    Super+M { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"system-monitor\"}"; }
    Super+X { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"session\"}"; }
}
```
