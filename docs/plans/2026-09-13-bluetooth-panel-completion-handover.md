# Bluetooth panel, widget, and page: completion handover

Date: 2026-09-14. This snapshot records the implementation, automated gates,
and live Niri/BlueZ observations for `sysc-155`. Leave it as written; later
corrections belong in bd.

## Shipped

- `services.Bluetooth` owns one system-bus BlueZ client, reduces immutable
  adapter/device snapshots, handles daemon loss/recovery, and exposes the
  power, scan, pair, connect, trust, disconnect, forget, and prompt actions.
- `KeyboardDisplay` implements the complete BlueZ pairing surface with one
  generation-matched prompt, bounded cancellation, fail-closed shutdown, and
  credential clearing.
- `Registry` owns the service and relay. The bar widget, `PanelBluetooth`, and
  the control-centre Bluetooth page consume the same cached state and
  `bluetoothBody`.
- Material glyphs and `bluetooth` configuration are wired into the bar. The
  IPC allowlist accepts `panel.open`, `panel.toggle`, and `panel.close` for
  Bluetooth.
- The final lifecycle correction keeps `RegisterAgent` ownership separate
  from default-agent readiness, so shutdown unregisters after a successful
  registration even when `RequestDefaultAgent` fails.

## Automated evidence

- `/usr/bin/go test -race -count=1 ./...` passed for every package.
- `/usr/bin/go vet ./...` passed.
- `gofmt -w .` followed by `test -z "$(gofmt -l .)"` passed.
- `git diff --exit-code -- go.mod go.sum` passed.
- Focused service, shell, IPC, pairing, and agent tests passed. The two final
  regressions caught and fixed the unregister lifecycle and Bluetooth IPC
  allowlist paths.

## Live Niri and BlueZ evidence

The final worktree build was deployed to the user service and matched the
installed binary:

```text
SHA-256 205c8735ab159db43fbd77842721d20fb46bff96209a0d6ce737ceca1e03b01a
service active: sysc-shell.service
```

The live compositor reported one output, `DP-1`, at 3440×1440, scale 1.0.
`niri msg -j layers` showed `sysc-shell:bar` and `sysc-shell-toast` at idle.
Opening the standalone panel and the control-centre Bluetooth destination
added one `sysc-shell-panel` layer on `DP-1` with exclusive keyboard input.
Closing the panel removed the panel and shield layers.

The running IPC server returned:

```text
panel.open {"panel":"bluetooth"} → {"id":1,"ok":true}
status → panels:["bluetooth"]
panel.open {"panel":"control-center","section":"bluetooth"} → {"id":1,"ok":true}
status → panels:["control-center"]
```

Both hosts rendered the same live BlueZ body: power, scan, Paired, Available,
and real device rows. The adapter reported `Powered: yes`, `Pairable: yes`,
and `Discovering: yes` while a Bluetooth body was open. After the final close,
the adapter reported `Discovering: no`, and the shell status returned
`panels:null`. No device state changed during the gate.

BlueZ exposed controller `1C:CE:51:34:11:14` and four existing devices. The
live run did not connect, pair, trust, or forget a device: the owner supplied
no disposable target or approved paired target. Unit tests cover those mutation
paths and all pairing prompt methods. The second-output matrix remains
unrunnable because this host has only `DP-1`.

An unrelated login-keyring authentication prompt appeared in the screenshots;
it did not come from sysc-shell.

## Tracker

The implementation and this completion snapshot land with the final
`sysc-155` state. The execution handover remains unchanged as the historical
commissioning document.
