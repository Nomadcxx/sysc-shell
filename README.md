# sysc-shell

A desktop shell for [Niri](https://github.com/YaLTeR/niri), written in Go. Bars, panels, a launcher,
notifications, a system tray, clipboard history and plugins, drawn straight to Wayland with no Qt,
QML, GTK or Quickshell underneath.

## Features

- **Bars**: one per output, with workspaces, window title, clock, weather, CPU, memory, temperature,
  GPU, disk, network, battery and the system tray
- **Launcher**: fuzzy app search that learns what you open, desktop actions, a calculator and emoji
- **Control centre**: network, Bluetooth, audio, media players, weather and a calendar in one panel
- **Notifications**: popups and a history centre, fed by [sysc-notify](https://github.com/Nomadcxx/sysc-notify)
- **Clipboard**: browse, restore and pin history kept by [sysc-clipboard](https://github.com/Nomadcxx/sysc-clipboard)
- **System monitor**: live gauges, and a process view grouped by application
- **Session panel**: log out, suspend, reboot and power off, with battery status and power profiles
- **OSD**: volume and brightness
- **Theming**: Material 3 colours from your wallpaper through matugen, applied to the shell and, with
  templates, to your other apps
- **Wallpaper**: set it from the shell with awww, swaybg or gSlapper
- **Frosted glass**: blurred bars and panels on Niri 26.04 and later
- **Plugins**: a plugin manager and store. The official plugins live in
  [sysc-plugins](https://github.com/Nomadcxx/sysc-plugins)

Only Niri is supported. There is no lock screen built in. The session panel shows a Lock button once
you tell it which locker to run:

```json
{ "session": { "locker": "swaylock -f" } }
```

## Installation

**Requires:** Go 1.26.4+ and Niri, started as a session (`niri-session`, which is what display managers
run) so systemd knows about your graphical session.

**Optional:** `matugen` for wallpaper colours, `awww`, `swaybg` or `gSlapper` for wallpapers, and
`wl-clipboard`.

### Build from Source

```bash
git clone https://github.com/Nomadcxx/sysc-shell
cd sysc-shell
go build -o ~/.local/bin/sysc-shell ./cmd/sysc-shell
```

### Run it as a user service

```bash
install -Dm644 packaging/systemd/sysc-shell.service ~/.config/systemd/user/sysc-shell.service
systemctl --user daemon-reload
systemctl --user enable --now sysc-shell.service
```

The service starts with your Niri session and restarts the shell if it crashes. If your Niri config
already has a `spawn-at-startup` line for sysc-shell, remove it, or you'll get two shells. More detail
in [packaging/systemd](packaging/systemd/README.md).

### The daemons

Notifications, the tray and clipboard history each run as their own small daemon, so a crash in one
doesn't take the bar with it, and restarting the shell loses nothing. The shell works without them;
you just don't get that feature.

**Notifications:** follow the [sysc-notify README](https://github.com/Nomadcxx/sysc-notify#installation).
Stop mako, dunst or swaync first.

**Clipboard history:** follow the [sysc-clipboard README](https://github.com/Nomadcxx/sysc-clipboard#installation).

**System tray:**

```bash
GOBIN="$HOME/.local/bin" go install github.com/Nomadcxx/sysc-tray/cmd/sysc-tray@v0.1.0-rc.3

cat > ~/.config/systemd/user/sysc-tray.service <<'EOF'
[Unit]
Description=sysc tray presenter

[Service]
ExecStart=%h/.local/bin/sysc-tray
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable --now sysc-tray.service
```

### Plugins

```bash
git clone https://github.com/Nomadcxx/sysc-plugins
cd sysc-plugins
make install
```

Then enable them from the plugin manager. See [sysc-plugins](https://github.com/Nomadcxx/sysc-plugins)
for what each one needs.

## Usage

Everything opens from the bar. To open panels from the keyboard, bind `sysc-shell ipc` in your Niri
config:

```kdl
binds {
    Super+Space { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"launcher\"}"; }
    Super+X     { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"session\"}"; }
    Super+Comma { spawn "sysc-shell" "ipc" "panel.toggle" "{\"panel\":\"settings\"}"; }
    XF86AudioRaiseVolume allow-when-locked { spawn "sysc-shell" "ipc" "osd.step" "{\"kind\":\"audio\",\"action\":\"up\"}"; }
}
```

The panels you can name are `launcher`, `control-center`, `notifications`, `clipboard`, `clock`,
`weather`, `system-monitor`, `session`, `power`, `settings`, `wallpaper`, `audio`, `network`,
`bluetooth` and `plugin`. The full set of bindings, including brightness and mute, is in
[docs/niri-hotkeys.md](docs/niri-hotkeys.md).

From a terminal:

```bash
sysc-shell ipc status
sysc-shell ipc panel.toggle '{"panel":"control-center"}'
```

Settings are in the settings panel and saved to `~/.config/sysc-shell/config.json`. Logs go to
`journalctl --user -u sysc-shell`.

## Documentation

- [Niri hotkeys](docs/niri-hotkeys.md)
- [Blur on Niri](docs/niri-blur.md)
- [Metrics widgets and Intel GPU usage](docs/metrics-widgets.md)
- [Running under systemd](packaging/systemd/README.md)
- [Development](docs/development.md)

## License

BSD-3-Clause. [NOTICE](NOTICE) covers the app-theming template catalog.

---

<a href="https://github.com/Nomadcxx"><img src="https://raw.githubusercontent.com/Nomadcxx/Nomadcxx/main/assets/rama-mark.svg" height="22" alt="RAMA"></a> — terminal-native tooling for the linux desktop.
[More projects →](https://github.com/Nomadcxx) · [Sponsor](https://github.com/sponsors/Nomadcxx) ❤️
