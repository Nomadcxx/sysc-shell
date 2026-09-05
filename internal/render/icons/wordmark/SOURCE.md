# SYSC wordmark

`sysc-mark.png` is a single-channel alpha master of the SYSC brand mark,
downsampled from the canonical logo so the renderer only ever scales *down*.

| | |
|---|---|
| Source | `sysc-greet/assets/logo.png` at `ccfcdde` |
| Source size | 828 x 114 after trimming transparent margins |
| Master size | 930 x 128, Lanczos |
| Aspect | 7.265625, mirrored in `render.WordmarkAspect` |

Alpha only: the mark carries no colour of its own and is tinted with the
theme's accent at paint time, so it follows the palette like every other piece
of chrome.

## Regenerating

```
python3 - <<'PY'
from PIL import Image
a = Image.open('sysc-greet/assets/logo.png').split()[-1]
a = a.crop(a.getbbox())
H = 128; W = round(a.size[0] * H / a.size[1])
a.resize((W, H), Image.LANCZOS).convert('L').save('sysc-mark.png', optimize=True)
PY
```

If the master's aspect changes, update `WordmarkAspect` with it —
`TestWordmarkAspectMatchesTheAsset` fails otherwise.
