#!/usr/bin/env python3
"""Authoring-time builder: SVG sources -> sysc-icons.ttf. Not invoked by go build."""

from pathlib import Path
import xml.etree.ElementTree as ET

from fontTools.fontBuilder import FontBuilder
from fontTools.misc.transform import Transform
from fontTools.pens.cu2quPen import Cu2QuPen
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.pens.transformPen import TransformPen
from fontTools.svgLib.path import parse_path

HERE = Path(__file__).resolve().parent
SVG_DIR = HERE / "svg"
OUT = HERE / "sysc-icons.ttf"

# Consecutive private-use codepoints from U+E000, matching iconfont.go.
GLYPHS = [
    ("uniE000", 0xE000, "clear-day"),
    ("uniE001", 0xE001, "partly-cloudy"),
    ("uniE002", 0xE002, "cloud"),
    ("uniE003", 0xE003, "fog"),
    ("uniE004", 0xE004, "rain"),
    ("uniE005", 0xE005, "snow"),
    ("uniE006", 0xE006, "heavy-snow"),
    ("uniE007", 0xE007, "thunderstorm"),
    ("uniE008", 0xE008, "battery-0"),
    ("uniE009", 0xE009, "battery-1"),
    ("uniE00A", 0xE00A, "battery-2"),
    ("uniE00B", 0xE00B, "battery-3"),
    ("uniE00C", 0xE00C, "battery-4"),
    ("uniE00D", 0xE00D, "battery-5"),
    ("uniE00E", 0xE00E, "battery-6"),
    ("uniE00F", 0xE00F, "battery-charging-0"),
    ("uniE010", 0xE010, "battery-charging-1"),
    ("uniE011", 0xE011, "battery-charging-2"),
    ("uniE012", 0xE012, "battery-charging-3"),
    ("uniE013", 0xE013, "battery-charging-4"),
    ("uniE014", 0xE014, "battery-charging-5"),
    ("uniE015", 0xE015, "battery-charging-6"),
    ("uniE016", 0xE016, "battery-critical"),
    ("uniE017", 0xE017, "cpu"),
    ("uniE018", 0xE018, "memory"),
    ("uniE019", 0xE019, "disk"),
    ("uniE01A", 0xE01A, "network"),
]

UPM = 1000
# 24px SVG -> ~800 font units, y-flipped so the icon sits on the baseline.
SCALE = 800 / 24
XFORM = Transform(SCALE, 0, 0, -SCALE, 100, 900)


def empty_glyph():
    return TTGlyphPen(None).glyph()


def glyph_from_svg(path: Path):
    root = ET.parse(path).getroot()
    ttpen = TTGlyphPen(None)
    cu = Cu2QuPen(ttpen, max_err=1.0)
    tpen = TransformPen(cu, XFORM)
    for el in root.iter():
        if el.tag.split("}")[-1] != "path":
            continue
        d = el.get("d")
        if d:
            parse_path(d, tpen)
    return ttpen.glyph()


def main():
    order = [".notdef"]
    cmap = {}
    glyf = {".notdef": empty_glyph()}
    for name, code, stem in GLYPHS:
        order.append(name)
        cmap[code] = name
        glyf[name] = glyph_from_svg(SVG_DIR / f"{stem}.svg")

    fb = FontBuilder(UPM, isTTF=True)
    fb.setupGlyphOrder(order)
    fb.setupCharacterMap(cmap)
    fb.setupGlyf(glyf)
    metrics = {name: (900, 50) for name in order}
    metrics[".notdef"] = (600, 0)
    fb.setupHorizontalMetrics(metrics)
    fb.setupHorizontalHeader(ascent=900, descent=-100)
    fb.setupNameTable(
        {
            "familyName": "sysc-icons",
            "styleName": "Regular",
            "uniqueFontIdentifier": "sysc-icons:1.000",
            "fullName": "sysc-icons",
            "psName": "sysc-icons",
            "version": "Version 1.000",
        }
    )
    fb.setupOS2(
        sTypoAscender=900,
        sTypoDescender=-100,
        usWinAscent=900,
        usWinDescent=100,
    )
    fb.setupPost()
    fb.save(OUT)
    print(f"wrote {OUT} ({OUT.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
