# Night light

Night light changes the colour temperature of each Niri output through
`wlr-gamma-control-unstable-v1`. Open **Settings → Night Light** to
choose a schedule and temperature.

| Mode | Behaviour |
|---|---|
| Off | Leaves the compositor's colour tables alone. The Control Centre tile can turn night light on until you toggle it off or restart the shell. |
| Sunset | Fades from the day temperature to the night temperature around local sunset, then back around sunrise. Set a city or coordinates under Weather. |
| Custom | Uses the configured start and end times. Times may cross midnight. |
| Always | Holds the night temperature. The tile can turn it off until you toggle it again or restart the shell. |

The transition is centred on each schedule boundary and runs in mired space.
The shell recalculates it from the clock, so restarting during a fade returns
to the temperature for that time. Tile toggles and the `nightlight.set` and
`nightlight.toggle` methods fade over one second; **Reduced motion** makes them
immediate.

The Control Centre shows a **Night** tile when an output supports gamma
control. If another colour program owns it, the tile and Settings show the
failure reason. Stop that program, then toggle the tile to retry.

The configuration block is named `night-light`. These are its defaults:

```json
{
  "night-light": {
    "mode": "off",
    "night_kelvin": 4000,
    "day_kelvin": 6500,
    "transition_minutes": 30,
    "start": "20:00",
    "end": "07:00"
  }
}
```

Use `sysc-shell ipc` for scripts and key bindings:

| Method | Parameters | Result |
|---|---|---|
| `nightlight.status` | none | Current mode, temperature, target, next change, availability, reason, and override state. |
| `nightlight.set` | `{"on":true}` or `{"on":false}` | Sets an override until the next schedule boundary. In Off or Always mode, it lasts until toggled or the shell restarts. |
| `nightlight.toggle` | none | Toggles the override with the same lifetime as `nightlight.set`. |
| `nightlight.temperature` | `{"kelvin":3500}` | Saves the night temperature. Values range from 2500 to 6000 K. |

For example, run `sysc-shell ipc nightlight.status` or
`sysc-shell ipc nightlight.temperature '{"kelvin":3500}'`.
