# Launcher mark

`sysc-aperture.png` is the owner-supplied nested-gates launcher mark, copied
byte-for-byte so the embedded asset and the source artwork stay identical.

| | |
|---|---|
| Source | `/home/nomadx/Pictures/sysc-aperture-c-nested-gates.png` |
| SHA-256 | `02f3a6246c193b06701e8d99d7cfbcb5b57136db943d2a67aca8740891827d3f` |
| Source size | 1024 x 1024, 8-bit RGBA, non-interlaced |
| Visible bounds | 768 x 768 at (128, 128) |

The transparent margin is part of the asset contract: the visible mark is
smaller than its canvas, and the bar sizes the box rather than the mark.

## Copying

```
cp /home/nomadx/Pictures/sysc-aperture-c-nested-gates.png \
   internal/render/icons/launcher/sysc-aperture.png
sha256sum internal/render/icons/launcher/sysc-aperture.png
```

`TestLauncherMarkAsset` pins the hash, dimensions, visible bounds, and
non-empty pixels, so a regenerated or re-encoded copy fails the check.
