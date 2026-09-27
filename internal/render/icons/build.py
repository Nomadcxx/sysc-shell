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
    ("uniE01B", 0xE01B, "camera"),
    ("uniE01C", 0xE01C, "camera-off"),
    ("uniE01D", 0xE01D, "record"),
    ("uniE01E", 0xE01E, "stop"),
    ("uniE01F", 0xE01F, "replay"),
    ("uniE020", 0xE020, "notifications"),
    ("uniE021", 0xE021, "notifications-off"),
    ("uniE022", 0xE022, "close"),
    ("uniE023", 0xE023, "schedule"),
    ("uniE024", 0xE024, "ghost"),
    ("uniE025", 0xE025, "sysmon-cpu"),
    ("uniE026", 0xE026, "sysmon-memory"),
    ("uniE027", 0xE027, "sysmon-gpu"),
    ("uniE028", 0xE028, "clear-night"),
    ("uniE029", 0xE029, "partly-cloudy-night"),
    ("uniE02A", 0xE02A, "thermometer"),
    ("uniE02B", 0xE02B, "wind"),
    ("uniE02C", 0xE02C, "humidity"),
    ("uniE02D", 0xE02D, "sunrise"),
    ("uniE02E", 0xE02E, "sunset"),
    ("uniE02F", 0xE02F, "elevation"),
    ("uniE030", 0xE030, "ai-usage"),
    ("uniE031", 0xE031, "smartphone"),
    ("uniE032", 0xE032, "phonelink-off"),
    ("uniE033", 0xE033, "tablet"),
    ("uniE034", 0xE034, "laptop"),
    ("uniE035", 0xE035, "desktop-windows"),
    ("uniE036", 0xE036, "tv"),
    ("uniE037", 0xE037, "devices"),
    ("uniE038", 0xE038, "phone-in-talk"),
    ("uniE039", 0xE039, "folder-open"),
    ("uniE03A", 0xE03A, "content-paste"),
    ("uniE03B", 0xE03B, "share"),
    ("uniE03C", 0xE03C, "sms"),
    ("uniE03D", 0xE03D, "notifications-active"),
    ("uniE03E", 0xE03E, "refresh"),
    ("uniE03F", 0xE03F, "5g"),
    ("uniE040", 0xE040, "4g-mobiledata"),
    ("uniE041", 0xE041, "3g-mobiledata"),
    ("uniE042", 0xE042, "g-mobiledata"),
    ("uniE043", 0xE043, "signal-cellular-null"),
    ("uniE044", 0xE044, "signal-cellular-1-bar"),
    ("uniE045", 0xE045, "signal-cellular-2-bar"),
    ("uniE046", 0xE046, "signal-cellular-3-bar"),
    ("uniE047", 0xE047, "signal-cellular-4-bar"),
    ("uniE048", 0xE048, "sysmon-gpu"),
    ("uniE049", 0xE049, "cross"),
]

# The cat's acts follow the cellular set, pose by pose, in the order
# iconfont.go's catActs lists them. cat.py draws the SVGs.
CAT_ACTS = [("sleep", 4), ("sit", 4), ("groom", 5), ("scratch", 3), ("stretch", 4), ("walk", 8), ("run", 12)]
_next = 0xE04A
for _act, _poses in CAT_ACTS:
    for _i in range(_poses):
        GLYPHS.append((f"uni{_next:04X}", _next, f"cat-{_act}-{_i}"))
        _next += 1

# The Docker whale closes the font after the cat band, for the mini-docker
# plugin's bar pill.
GLYPHS.append(("uniE072", 0xE072, "docker"))

UPM = 1000
# 24px SVG -> font units, y-flipped so the icon sits on the baseline.
#
# The box is 1.2em so a filled metric glyph paints at ~17px next to 14px body
# text, matching Noctalia's baseGlyphSize 16 vs fontSizeBody 14. Advance must
# cover that box: a 900-unit advance clipped the 1200-unit outlines and the
# icons read as stretched slabs.
BOX = 1200
SCALE = BOX / 24
SIDE = 0
XFORM = Transform(SCALE, 0, 0, -SCALE, SIDE, 1100)
ADVANCE = BOX


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
    metrics = {name: (ADVANCE, SIDE) for name in order}
    metrics[".notdef"] = (600, 0)
    fb.setupHorizontalMetrics(metrics)
    fb.setupHorizontalHeader(ascent=1100, descent=-100)
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
        sTypoAscender=1100,
        sTypoDescender=-100,
        usWinAscent=1100,
        usWinDescent=100,
    )
    fb.setupPost()
    fb.save(OUT)
    print(f"wrote {OUT} ({OUT.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
