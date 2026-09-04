# Running sysc-shell from systemd

`niri --session` starts `graphical-session.target` and exports
`WAYLAND_DISPLAY` and `NIRI_SOCKET` into the systemd user environment, so a
user unit ordered after that target starts with everything the shell needs.

Prefer this over niri's `spawn-at-startup`:

- a crash is restarted, instead of leaving the session with no bar until the
  user notices and runs the binary by hand;
- `journalctl --user -u sysc-shell` holds the output, instead of it going to
  whatever niri's stdout happens to be;
- `systemctl --user restart sysc-shell` is one command during development;
- startup is ordered against the session rather than racing it.

## Install

    install -Dm644 packaging/systemd/sysc-shell.service \
      ~/.config/systemd/user/sysc-shell.service
    systemctl --user daemon-reload
    systemctl --user enable --now sysc-shell.service

Then remove niri's own launch, or the session starts two shells:

    # config.kdl
    - spawn-at-startup "/home/nomadx/.local/bin/sysc-shell"

Keybinds still `spawn` the binary in `ipc` mode; those are one-shot clients
and are unaffected.

## Check

    systemctl --user status sysc-shell
    journalctl --user -u sysc-shell -b
