# Niri blur

The frosted bar, and panels with **Blur behind panels** on, ask the compositor
to blur what lies behind them. The shell publishes the exact shape of each
surface, including the concave joints and screen-edge curves, through the
`ext-background-effect-v1` protocol. Niri draws the blur.

## Requirement

Niri **26.04 or later**. Earlier versions do not offer the protocol, and they
reject the `background-effect` and `blur` config below.

Without the protocol the shell still starts. A frosted bar paints as solid, and
panels with blur-behind fall back to a screen capture that the shell blurs
itself. The shell logs its answer once at startup:

```text
shell: the compositor blurs behind surfaces; frosted bars and panels blur
shell: the compositor offers no blur (ext-background-effect-v1, Niri 26.04 or later); frosted bars paint solid
```

No Niri config is needed for blur to work. The shell publishes its regions
whether or not a rule exists.

## Namespaces

| Surface | Namespace |
|---|---|
| Bar | `sysc-shell:bar` |
| Panels | `sysc-shell-panel` |

## Xray

By default Niri blurs with **xray**: a surface's blur shows the blurred
wallpaper, ignoring any windows beneath it. Niri computes that blur once and
reuses it, which makes it cheap.

- **The bar** only ever sits over the wallpaper, so xray is right for it. It
  needs no rule.
- **Panels** open over windows. With xray, a panel's blur shows the wallpaper
  instead of the window behind it. To blur the real content behind panels, turn
  xray off for their namespace. This costs more GPU time than xray.

```kdl
layer-rule {
    match namespace="^sysc-shell-panel$"
    background-effect {
        xray false
    }
}
```

## Tuning

Blur strength belongs to Niri, not to the shell; the protocol carries no
radius. The top-level `blur` section applies to all background blur. These
are Niri's defaults:

```kdl
blur {
    passes 3
    offset 3
    noise 0.02
    saturation 1.5
}
```

Raise `offset` before `passes`: `offset` is free, while each pass costs GPU
time. `appearance.blur-radius` in the shell's settings only drives the
screen-capture fallback.

## Known artefact

With xray on, the antialiased pixels along a rounded edge can show a faint
fringe of wallpaper colour. The blur region stops inside the painted edge,
but the partly covered edge pixels still blend with the xray backdrop.
Noctalia documents the same behaviour. Turning xray off for panels removes it
there.

## Choosing a style

Settings › Bar › Surface:

- **Style**: `frosted` (default), `solid` or `islands`. Islands paints no bar
  ground; each pill blurs on its own, and panels detach from the bar.
- **Shape**: `attached` (default) meets the screen edge and curves into its
  sides, while `floating` keeps the gap.

Settings › Bar › Frost sets the frosted ground's opacity and the pills'
opacity, both 40–100%.

References: Niri's [layer rules](https://niri-wm.github.io/niri/Configuration:-Layer-Rules.html),
[window effects](https://niri-wm.github.io/niri/Window-Effects.html) and
[`blur` section](https://niri-wm.github.io/niri/Configuration:-Miscellaneous.html).
