#!/usr/bin/env python3
"""Authoring-time builder: pinned Material Symbols Rounded -> a static subset.

Not invoked by `go build`. The committed TTF is the artefact; this script exists
so the subset can be reproduced and audited byte for byte. Run:

    python3 internal/render/icons/material/build.py /path/to/MaterialSymbolsRounded.ttf

See SOURCE.md for provenance.
"""

import hashlib
import sys
from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer

HERE = Path(__file__).resolve().parent
OUT = HERE / "material-symbols-rounded.ttf"

# The upstream file this subset is cut from. Verified before anything is read as
# a font, so a substituted or truncated download fails loudly rather than
# producing a plausible-looking font with the wrong shapes.
SOURCE_SHA256 = "c4416e02739ed6865e3218c19dcd62c5a88fb97b8bcc445f24ae8017d11cc2d0"
SOURCE_COMMIT = "84ccef280841abfac506afc4ad4a2782f6d0a1d0"

# One weight, filled, no grade, at the 24 px optical size the chrome draws at.
# Pinning every axis leaves a static font with no fvar.
AXES = {"FILL": 1, "wght": 400, "GRAD": 0, "opsz": 24}

# The exact inventory the shell may name. Adding a glyph here is a deliberate
# act: the subset grows, and materialfont.go must accept the name too.
ICONS = [
    "lock",
    "logout",
    "bedtime",
    "restart_alt",
    "power_settings_new",
    "speed",
    "balance",
    "energy_savings_leaf",
    "check",
    "close",
    "chevron_left",
    "chevron_right",
    "search",
    "settings",
    "notifications",
    "do_not_disturb_on",
    "volume_up",
    "volume_off",
    "brightness_high",
]

# Material Symbols addresses a glyph by typing its name, so the letters and the
# underscore have to survive subsetting for the ligature to have inputs.
LIGATURE_FEATURES = ["rlig", "rclt"]

# A fixed head timestamp. TrueType records creation and modification dates, and
# leaving them live would change the file's hash on every rebuild.
EPOCH = 0


def verify(path: Path) -> None:
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if digest != SOURCE_SHA256:
        raise SystemExit(
            f"source sha256 {digest} does not match the pinned "
            f"{SOURCE_SHA256} for commit {SOURCE_COMMIT}"
        )


def build(source: Path) -> None:
    verify(source)
    font = TTFont(source)
    font = instancer.instantiateVariableFont(font, AXES, inplace=True, updateFontNames=False)
    if "fvar" in font:
        raise SystemExit("instancing left an fvar table; the result is not static")

    options = subset.Options()
    options.layout_features = LIGATURE_FEATURES
    options.glyph_names = True
    # Without this, retaining the letters drags in every ligature they can
    # begin -- which is all 6000-odd icons, and a 1.4 MB font.
    options.layout_closure = False
    options.drop_tables += ["DSIG"]
    options.recalc_timestamp = False

    text = "".join(sorted({c for name in ICONS for c in name}))
    subsetter = subset.Subsetter(options=options)
    subsetter.populate(glyphs=ICONS, text=text)
    subsetter.subset(font)

    for name in ICONS:
        if name not in font.getGlyphOrder():
            raise SystemExit(f"subset dropped {name!r}")
    if "GSUB" not in font:
        raise SystemExit("subset dropped GSUB; ligature lookup by name would stop working")

    font.recalcTimestamp = False
    font["head"].created = EPOCH
    font["head"].modified = EPOCH
    font.save(OUT)
    print(f"{OUT}: {OUT.stat().st_size} bytes, {len(font.getGlyphOrder())} glyphs")
    print(f"sha256 {hashlib.sha256(OUT.read_bytes()).hexdigest()}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    build(Path(sys.argv[1]))
