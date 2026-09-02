# Niri hotkeys for sysc-shell panels

The compositor owns key bindings. The shell owns the panels. Add these to
`~/.config/niri/config.kdl` so Super+P, Super+M, Super+X, Super+Comma, and
Super+Space toggle the clock, system-monitor, session, settings, and launcher
panels from anywhere. Super+X still toggles `session`; that surface now
includes battery status and power profiles. Media and brightness keys step
the matching service and show the OSD.

```kdl
bind {
    Super+Space { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"launcher\"}"; }
    Super+P { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"clock\"}"; }
    Super+M { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"system-monitor\"}"; }
    Super+X { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"session\"}"; }
    Super+Comma { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"settings\"}"; }
    XF86AudioRaiseVolume allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" "{\"kind\":\"audio\",\"action\":\"up\"}"; }
    XF86AudioLowerVolume allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" "{\"kind\":\"audio\",\"action\":\"down\"}"; }
    XF86AudioMute        allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" "{\"kind\":\"audio\",\"action\":\"mute\"}"; }
    XF86MonBrightnessUp  allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" "{\"kind\":\"brightness\",\"action\":\"up\"}"; }
    XF86MonBrightnessDown allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" "{\"kind\":\"brightness\",\"action\":\"down\"}"; }
}
```
