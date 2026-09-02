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

Result: **8,184 bytes, 43 glyphs** (19 icons, the letters and underscore that
spell them, and `.notdef`).

### Inventory

```
lock logout bedtime restart_alt power_settings_new
speed balance energy_savings_leaf check
close chevron_left chevron_right
search settings notifications do_not_disturb_on
volume_up volume_off brightness_high
```

`materialfont.go` accepts exactly these names and rejects anything else. Adding
one means editing `ICONS` in `build.py`, rebuilding, and committing the larger
font deliberately.

## Reproducing

```bash
python3 internal/render/icons/material/build.py /path/to/MaterialSymbolsRounded.ttf
sha256sum internal/render/icons/material/material-symbols-rounded.ttf
git diff --exit-code -- internal/render/icons/material/material-symbols-rounded.ttf
```

The head table's creation and modification timestamps are pinned, so a rebuild
from the same source reproduces the committed file byte for byte:

```
204ecc8e8b4f5657fbdd5ee5c5ea6668fdf1d0e8d576875230c56dc3c42debcc
```

Built with fontTools 4.63.0.
