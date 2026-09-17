# Material Symbols Rounded subset

`material-symbols-rounded.ttf` is a static, subsetted cut of Google's Material
Symbols Rounded variable font. It is committed rather than fetched or generated
during `go build`: the build stays offline and reproducible, and a font is
deterministic once authored.

## Upstream

| | |
|---|---|
| Project | [google/material-design-icons](https://github.com/google/material-design-icons) |
| Commit | `84ccef280841abfac506afc4ad4a2782f6d0a1d0` |
| Path | `variablefont/MaterialSymbolsRounded[FILL,GRAD,opsz,wght].ttf` |
| Size | 15,090,976 bytes |
| SHA-256 | `c4416e02739ed6865e3218c19dcd62c5a88fb97b8bcc445f24ae8017d11cc2d0` |
| Licence | Apache-2.0, copied verbatim to `LICENSE` |

Download URL, with the bracket characters percent-encoded:

```
https://raw.githubusercontent.com/google/material-design-icons/84ccef280841abfac506afc4ad4a2782f6d0a1d0/variablefont/MaterialSymbolsRounded%5BFILL%2CGRAD%2Copsz%2Cwght%5D.ttf
```

`build.py` verifies that SHA-256 before it reads the file as a font, so a
truncated or substituted download fails loudly instead of yielding a plausible
font with the wrong shapes.

## Instanced axes

| Axis | Value | Why |
|---|---|---|
| `FILL` | 1 | Filled symbols read at chrome sizes; outlines thin out. |
| `wght` | 400 | Matches the shell's body text weight. |
| `GRAD` | 0 | No optical grade correction. |
| `opsz` | 24 | The size the catalogue's chrome icons draw at. |

Every axis is pinned, so the result carries no `fvar` and is a plain static TTF.

## Subset

Material Symbols addresses a glyph by typing its name, so the letters and the
underscore are retained alongside the icon glyphs and the `rlig`/`rclt` lookups
that join them. Layout closure is disabled during subsetting: leaving it on lets
the retained letters reach every ligature they could begin, which is all
6,605 glyphs and a 1.4 MB file.

Result: **26,204 bytes, 114 glyphs** (83 shell names, the letters and underscore
that spell them, and `.notdef`).

### Inventory

```
lock logout bedtime restart_alt power_settings_new
speed balance energy_savings_leaf check
content_paste content_copy
close chevron_left chevron_right
search settings notifications do_not_disturb_on
volume_up volume_off brightness_high
delete schedule
mic mic_off graphic_eq headphones
home music_note play_arrow pause skip_next skip_previous repeat repeat_one shuffle desktop_windows
wifi bluetooth cloud calendar_month
battery_full coffee wallpaper person
sunny partly_cloudy_day rainy thunderstorm weather_snowy foggy
signal_wifi_0_bar network_wifi_1_bar network_wifi_2_bar
network_wifi_3_bar signal_wifi_4_bar wifi_off lan visibility visibility_off
bluetooth_disabled bluetooth_connected keyboard mouse smartphone speaker
devices_other
palette description toolbar widgets web_asset display_settings apps extension
accessibility_new folder_open expand_more add remove
drag_indicator tune link_off
```

`materialfont.go` accepts exactly these names and rejects anything else. Adding
one means editing `ICONS` in `build.py`, rebuilding, and committing the larger
font deliberately. The pinned source renamed `smartphone`; the builder retains
`phone_bluetooth_speaker` for that public shell name and the Go renderer uses
the same alias.

## Reproducing

```bash
python3 internal/render/icons/material/build.py /path/to/MaterialSymbolsRounded.ttf
sha256sum internal/render/icons/material/material-symbols-rounded.ttf
git diff --exit-code -- internal/render/icons/material/material-symbols-rounded.ttf
```

The head table's creation and modification timestamps are pinned, so a rebuild
from the same source reproduces the committed file byte for byte:

```
1cfa6e87320c4777780004f682e7c4d4fac5969e118f407bab86b437b99ab28d
```

Built with fontTools 4.65.0.
