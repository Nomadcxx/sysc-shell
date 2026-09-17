#!/usr/bin/env python3
"""Emit the dimensioned mockups for the 2026-09-17 radial design commission.

Every geometric constant here is a measured value recorded in measurements.md,
taken from the real layout engine and the real font stack at source revision
4119ce5. Editing a number here without re-measuring makes the artifacts lie, so
the constants are named after the code that owns them.

Usage:  python3 generate.py            # writes the .svg files beside this script
"""

import math
import os

OUT = os.path.dirname(os.path.abspath(__file__))

# --- Tokens owned by internal/theme/profile.go -----------------------------
MARGIN_XXS, MARGIN_XS, MARGIN_S, MARGIN_M, MARGIN_L = 2, 4, 6, 9, 13
CARD_PADDING = 9          # theme.Metrics.CardPadding, constant at every density
CARD_RADIUS = 12          # render.Style.CardRadius

# --- Bounds owned by internal/shell/controlcenter_pages.go -----------------
CARD_W, CARD_H = 356, 88  # ccLeftColumnW, ccCardH
INTERIOR_W = CARD_W - 2 * CARD_PADDING   # 338
INTERIOR_H = CARD_H - 2 * CARD_PADDING   # 70

# --- Measured text metrics, Inter, scale 1.0, font scale 100% --------------
TITLE_H, CAPTION_H, BODY_H = 22, 15, 19
TITLE_SIZE, CAPTION_SIZE, BODY_SIZE = 17, 12, 15

# --- Gauge geometry --------------------------------------------------------
RING = 40                 # the established Home diameter
RING_SMALL = 22           # the rejected diameter, and the bar's
STROKE = 2                # paintRadialGauge: Scale120.Physical(2)
SLOT = (INTERIOR_W - 3 * MARGIN_M) // 4   # 77

# --- Resolved palette, Theme.PanelStyle() at the default theme -------------
BG = "#1d2025"
CARD = "#3a4149"
TRACK = "#9aa0a6"
ACCENT = "#0080ff"
SECONDARY = "#bec6dc"
ERROR = "#ff5449"
AMBER = "#ffb300"
TEXT = "#e6e6e6"
SUBTLE = "#9aa0a6"

# --- Annotation palette (documentation chrome, not shell tokens) -----------
PAPER = "#f4f2ee"
INK = "#1c1a17"
QUIET = "#8a827a"
BAD = "#b91c1c"
GOOD = "#0f766e"
WARN = "#a16207"
MONO = "IBM Plex Mono, DejaVu Sans Mono, monospace"
SANS = "IBM Plex Sans, DejaVu Sans, sans-serif"
SHELL_FONT = "Inter, DejaVu Sans, sans-serif"


def lerp(a, b, t):
    return tuple(round(x + (y - x) * t) for x, y in zip(a, b))


def hexof(rgb):
    return "#%02x%02x%02x" % rgb


RGB = {
    "accent": (0x00, 0x80, 0xFF),
    "secondary": (0xBE, 0xC6, 0xDC),
    "amber": (0xFF, 0xB3, 0x00),
    "error": (0xFF, 0x54, 0x49),
}


def contrast_ratio(a, b):
    def lin(c):
        c /= 255
        return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4

    def lum(c):
        return 0.2126 * lin(c[0]) + 0.7152 * lin(c[1]) + 0.0722 * lin(c[2])

    la, lb = lum(a), lum(b)
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)


def ensure_contrast(fg, bg, target):
    """theme.EnsureContrast, reproduced. It walks the foreground toward black or
    white until the ratio is met, which is why the warning amber is painted as a
    dark brown rather than as the amber the source names."""
    if contrast_ratio(fg, bg) >= target:
        return fg
    black, white = (0, 0, 0), (255, 255, 255)
    endpoint = white if contrast_ratio(white, bg) > contrast_ratio(black, bg) else black
    if contrast_ratio(endpoint, bg) < target:
        return endpoint
    lo, hi = 0.0, 1.0
    for _ in range(24):
        mid = (lo + hi) / 2
        if contrast_ratio(lerp(fg, endpoint, mid), bg) >= target:
            hi = mid
        else:
            lo = mid
    return lerp(fg, endpoint, hi)


# radialWarningAmber: the amber is forced to 3:1 against the track before use.
TRACK_RGB = (0x9A, 0xA0, 0xA6)
WARNING_AMBER = ensure_contrast(RGB["amber"], TRACK_RGB, 3)


def thermal_end(celsius):
    """radialArcColor's thermal ramp, reproduced with the adjusted amber."""
    if celsius < 60:
        return RGB["accent"]
    if celsius < 75:
        return lerp(RGB["accent"], WARNING_AMBER, (celsius - 60) / 15)
    if celsius < 85:
        return lerp(WARNING_AMBER, RGB["error"], (celsius - 75) / 10)
    return RGB["error"]


def grad(gid, end_hex):
    return (
        f'<linearGradient id="{gid}" x1="0" y1="0" x2="1" y2="1">'
        f'<stop offset="0" stop-color="{ACCENT}"/>'
        f'<stop offset="1" stop-color="{end_hex}"/></linearGradient>'
    )


def ring(x, y, size, fraction, gid, absent=False):
    """One radial gauge. The arc starts at twelve o'clock and runs clockwise,
    with round caps, exactly as paintRadialGauge draws it."""
    r = (size - STROKE) / 2
    cx, cy = x + size / 2, y + size / 2
    circumference = 2 * math.pi * r
    out = [
        f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="none" '
        f'stroke="{TRACK}" stroke-width="{STROKE}"/>'
    ]
    if not absent and fraction > 0:
        dash = circumference * min(fraction, 1.0)
        out.append(
            f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="none" stroke="url(#{gid})" '
            f'stroke-width="{STROKE}" stroke-linecap="round" '
            f'stroke-dasharray="{dash:.2f} {circumference:.2f}" '
            f'transform="rotate(-90 {cx} {cy})"/>'
        )
    return "".join(out)


def centred(x, y, size, text, px, colour=TEXT, weight=600):
    """Text centred in a size x size box, on the box's true centre row."""
    return (
        f'<text x="{x + size / 2}" y="{y + size / 2}" text-anchor="middle" '
        f'dominant-baseline="central" font-family="{SHELL_FONT}" font-size="{px}" '
        f'font-weight="{weight}" fill="{colour}">{text}</text>'
    )


def label(x, y, text, px=CAPTION_SIZE, colour=SUBTLE, anchor="middle",
          family=SHELL_FONT, weight=400):
    return (
        f'<text x="{x}" y="{y}" text-anchor="{anchor}" font-family="{family}" '
        f'font-size="{px}" font-weight="{weight}" fill="{colour}">{text}</text>'
    )


def note(x, y, text, px=11, colour=QUIET, anchor="start", family=MONO, weight=400):
    return label(x, y, text, px, colour, anchor, family, weight)


def card(x, y, w, h, fill=CARD):
    return (f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{CARD_RADIUS}" '
            f'fill="{fill}"/>')


def dim_v(x, y0, y1, text, colour=QUIET, side="right"):
    """A vertical dimension line, labelled away from the drawing."""
    tx, anchor = (x + 6, "start") if side == "right" else (x - 6, "end")
    return (
        f'<line x1="{x}" y1="{y0}" x2="{x}" y2="{y1}" stroke="{colour}" '
        f'stroke-width="1"/>'
        f'<line x1="{x - 3}" y1="{y0}" x2="{x + 3}" y2="{y0}" stroke="{colour}" stroke-width="1"/>'
        f'<line x1="{x - 3}" y1="{y1}" x2="{x + 3}" y2="{y1}" stroke="{colour}" stroke-width="1"/>'
        + note(tx, (y0 + y1) / 2 + 3.5, text, 10, colour, anchor=anchor)
    )


def dim_h(y, x0, x1, text, colour=QUIET):
    return (
        f'<line x1="{x0}" y1="{y}" x2="{x1}" y2="{y}" stroke="{colour}" stroke-width="1"/>'
        f'<line x1="{x0}" y1="{y - 3}" x2="{x0}" y2="{y + 3}" stroke="{colour}" stroke-width="1"/>'
        f'<line x1="{x1}" y1="{y - 3}" x2="{x1}" y2="{y + 3}" stroke="{colour}" stroke-width="1"/>'
        + note((x0 + x1) / 2, y - 5, text, 10, colour, anchor="middle")
    )


def svg(w, h, body, defs="", background=PAPER):
    return (
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" '
        f'viewBox="0 0 {w} {h}" font-kerning="normal">\n'
        f'<defs>{defs}</defs>\n'
        f'<rect width="{w}" height="{h}" fill="{background}"/>\n'
        f'{body}\n</svg>\n'
    )


def heading(x, y, kicker, kicker_colour, title, sub):
    out = [note(x, y, kicker, 11, kicker_colour)]
    out.append(label(x, y + 26, title, 26, INK, anchor="start", family=SANS, weight=600))
    if sub:
        out.append(label(x, y + 46, sub, 12.5, "#56504a", anchor="start", family=SANS))
    return "".join(out)


# --- The four Home slots, in the fixed commission order --------------------
SLOTS = [
    ("CPU", "42%", 0.42, "gA", 14),
    ("Memory", "25%", 0.25, "gA", 14),
    ("Temp", "65°", 0.65, "gT", 13),
    ("GPU", "70%", 0.70, "gA", 14),
]

DEFS = (
    grad("gA", SECONDARY)
    + grad("gT", hexof(thermal_end(65)))
    + grad("gW", hexof(thermal_end(78)))
    + grad("gE", hexof(thermal_end(92)))
)


def home_card(ox, oy, slots=SLOTS, absent=()):
    """The recommended composition: four rings in one row, value inside the
    ring, caption beneath, no card title. Content is 57 tall in a 70 interior,
    so it is centred and 13px of residual sits evenly above and below."""
    content_h = RING + MARGIN_XXS + CAPTION_H
    top = oy + CARD_PADDING + (INTERIOR_H - content_h) // 2
    out = [card(ox, oy, CARD_W, CARD_H)]
    for i, (name, value, frac, gid, px) in enumerate(slots):
        sx = ox + CARD_PADDING + i * (SLOT + MARGIN_M)
        rx = sx + (SLOT - RING) / 2
        gone = name in absent
        out.append(ring(rx, top, RING, frac, gid, absent=gone))
        out.append(centred(rx, top, RING, "—" if gone else value, px))
        out.append(label(sx + SLOT / 2, top + RING + MARGIN_XXS + 11, name))
    return "".join(out), top, content_h


# ---------------------------------------------------------------------------
# 1. The recommended option, dimensioned
# ---------------------------------------------------------------------------
def option_b():
    W, H = 900, 440
    ox, oy = 150, 150
    body, top, content_h = home_card(ox, oy)
    out = [
        heading(48, 44, "OPTION B · RECOMMENDED", GOOD,
                "Four 40px rings in one row",
                "The only composition measured that holds a 40px ring inside the existing 88px System card."),
        f'<rect x="{ox - 24}" y="{oy - 24}" width="{CARD_W + 48}" height="{CARD_H + 48}" rx="10" fill="{BG}"/>',
        body,
        # padded interior
        f'<rect x="{ox + CARD_PADDING}" y="{oy + CARD_PADDING}" width="{INTERIOR_W}" '
        f'height="{INTERIOR_H}" fill="none" stroke="{GOOD}" stroke-width="1" '
        f'stroke-dasharray="3 3" opacity=".7"/>',
        dim_h(oy - 36, ox, ox + CARD_W, f"{CARD_W} card width"),
        dim_v(ox + CARD_W + 34, oy, oy + CARD_H, f"{CARD_H} card"),
        dim_v(ox + CARD_W + 96, oy + CARD_PADDING, oy + CARD_PADDING + INTERIOR_H,
              f"{INTERIOR_H} interior"),
        dim_v(ox - 40, top, top + content_h, f"{content_h} content", GOOD, side="left"),
        dim_h(oy + CARD_H + 42, ox + CARD_PADDING,
              ox + CARD_PADDING + SLOT, f"{SLOT} slot"),
        dim_h(oy + CARD_H + 42, ox + CARD_PADDING + SLOT,
              ox + CARD_PADDING + SLOT + MARGIN_M, f"{MARGIN_M}"),
        note(48, 330, "VERTICAL BUDGET", 10, QUIET),
        note(48, 348, f"ring {RING}  +  gap {MARGIN_XXS}  +  caption {CAPTION_H}"
                      f"   =   {content_h} of {INTERIOR_H}", 11, INK),
        note(48, 366, f"{INTERIOR_H - content_h} residual, split above and below by centring", 11, QUIET),
        note(470, 330, "HOLDS AT EVERY SUPPORTED FONT SCALE", 10, QUIET),
        note(470, 348, "75% 54/70    100% 57/70    125% 61/70    150% 65/70", 11, GOOD),
        note(470, 366, "200% 72/70 — the page already fails at this scale", 11, QUIET),
        note(48, 404, "Ring interior type scales with the diameter: 14px value, 13px for a "
                      "three-character temperature.", 11, WARN),
    ]
    return svg(W, H, "".join(out), DEFS)


# ---------------------------------------------------------------------------
# 2. What is on main today
# ---------------------------------------------------------------------------
def rejected():
    W, H = 900, 420
    ox, oy = 150, 132
    out = [heading(48, 44, "REJECTED · ON MAIN TODAY", BAD,
                   "22px rings, two rows",
                   "The second row paints outside the card at the default font scale.")]
    out.append(f'<rect x="{ox - 24}" y="{oy - 24}" width="{CARD_W + 48}" '
               f'height="{CARD_H + 76}" rx="10" fill="{BG}"/>')
    out.append(card(ox, oy, CARD_W, CARD_H))
    cx = ox + CARD_PADDING
    cy = oy + CARD_PADDING
    out.append(label(cx, cy + 16, "System", TITLE_SIZE, TEXT, anchor="start", weight=600))
    rows = [[("CPU", "42%", 0.42, "gA"), ("Memory", "25%", 0.25, "gA")],
            [("CPU temperature", "65°C", 0.65, "gT"), ("GPU", "70%", 0.70, "gA")]]
    y = cy + TITLE_H + MARGIN_XS
    for row in rows:
        x = cx
        for name, value, frac, gid in row:
            out.append(ring(x, y, RING_SMALL, frac, gid))
            if gid == "gT":
                out.append(centred(x, y, RING_SMALL, "65°", 8))
            else:
                out.append(f'<rect x="{x + 7.5}" y="{y + 7.5}" width="7" height="7" '
                           f'rx="1" fill="none" stroke="{TEXT}" stroke-width="1"/>')
            tx = x + RING_SMALL + MARGIN_M
            out.append(label(tx, y + 15, name, CAPTION_SIZE, SUBTLE, anchor="start"))
            adv = len(name) * 6.6 + MARGIN_XXS
            out.append(label(tx + adv, y + 16, value, BODY_SIZE, TEXT, anchor="start"))
            x += 179
        y += RING_SMALL + MARGIN_XS
    # the padded bound and the spill past it
    out.append(f'<rect x="{cx}" y="{cy}" width="{INTERIOR_W}" height="{INTERIOR_H}" '
               f'fill="none" stroke="{ERROR}" stroke-width="1" stroke-dasharray="3 3"/>')
    out.append(f'<rect x="{ox}" y="{oy + CARD_H - CARD_PADDING}" width="{CARD_W}" height="4" '
               f'fill="{ERROR}" opacity=".35"/>')
    out.append(note(ox, oy + CARD_H + 24, "the second row ends 4px below the padded bound",
                    10, "#ff9a92"))
    out.append(note(48, 300, "MEASURED OVERFLOW · real font metrics", 10, QUIET))
    for i, (scale, content, verdict, colour) in enumerate([
            ("75%", "69 / 70", "fits", GOOD),
            ("100%", "74 / 70", "overflows 4px", BAD),
            ("125%", "83 / 70", "overflows 13px", BAD),
            ("150%", "99 / 70", "overflows 29px", BAD),
            ("200%", "—", "layout returns an error", BAD)]):
        yy = 320 + i * 17
        out.append(note(48, yy, scale, 11, INK))
        out.append(note(110, yy, content, 11, INK))
        out.append(note(190, yy, verdict, 11, colour))
    out.append(note(400, 300, "WHY THE GUARD TEST DOES NOT CATCH IT", 10, QUIET))
    out.append(note(400, 320, "TestControlCentreHomeSystemGaugesFitInsideCardBounds", 10, INK))
    out.append(note(400, 337, "measures every string as 16px tall. The real title is 22px,", 11, INK))
    out.append(note(400, 354, "and the comment above ccResourceRowH derives its fit from", 11, INK))
    out.append(note(400, 371, "that same wrong 16.", 11, INK))
    return svg(W, H, "".join(out), DEFS)


# ---------------------------------------------------------------------------
# 3. Option A: 40px two-by-two inside the existing card
# ---------------------------------------------------------------------------
def option_a():
    W, H = 900, 420
    ox, oy = 150, 140
    out = [heading(48, 44, "OPTION A · DOES NOT FIT", BAD,
                   "Four 40px rings, two by two",
                   "Needs 110px of interior against the 70px the card has.")]
    out.append(f'<rect x="{ox - 24}" y="{oy - 24}" width="{CARD_W + 48}" '
               f'height="{CARD_H + 96}" rx="10" fill="{BG}"/>')
    out.append(card(ox, oy, CARD_W, CARD_H))
    cx, cy = ox + CARD_PADDING, oy + CARD_PADDING
    out.append(label(cx, cy + 16, "System", TITLE_SIZE, TEXT, anchor="start", weight=600))
    rows = [[("CPU", "42%", 0.42, "gA"), ("Memory", "25%", 0.25, "gA")],
            [("CPU temp", "65°C", 0.65, "gT"), ("GPU", "70%", 0.70, "gA")]]
    y = cy + TITLE_H + MARGIN_XS
    for row in rows:
        x = cx
        for name, value, frac, gid in row:
            out.append(ring(x, y, RING, frac, gid))
            if gid == "gT":
                out.append(centred(x, y, RING, "65°", 8))
            tx = x + RING + MARGIN_M
            out.append(label(tx, y + 14, name, CAPTION_SIZE, SUBTLE, anchor="start"))
            out.append(label(tx, y + 33, value, BODY_SIZE, TEXT, anchor="start"))
            x += 164 + MARGIN_M
        y += RING + MARGIN_XS
    out.append(f'<rect x="{cx}" y="{cy}" width="{INTERIOR_W}" height="{INTERIOR_H}" '
               f'fill="none" stroke="{ERROR}" stroke-width="1" stroke-dasharray="3 3"/>')
    out.append(f'<rect x="{ox}" y="{oy + CARD_H - CARD_PADDING}" width="{CARD_W}" height="40" '
               f'fill="{ERROR}" opacity=".2"/>')
    out.append(f'<line x1="{ox}" y1="{oy + CARD_H - CARD_PADDING}" x2="{ox + CARD_W}" '
               f'y2="{oy + CARD_H - CARD_PADDING}" stroke="{ERROR}" stroke-width="1"/>')
    out.append(note(ox + 6, oy + CARD_H + 30, "40px outside the card", 10, "#ff9a92"))
    out.append(note(48, 310, "CARD HEIGHT THIS COMPOSITION NEEDS", 10, QUIET))
    for i, (scale, content, need) in enumerate([
            ("75%", "105", "123"), ("100%", "110", "128"),
            ("125%", "115", "133"), ("150%", "121", "139")]):
        yy = 330 + i * 17
        out.append(note(48, yy, scale, 11, INK))
        out.append(note(110, yy, content, 11, INK))
        out.append(note(175, yy, f"card {need}", 11, BAD))
    out.append(note(330, 310, "AND THE RING GAINS NOTHING", 10, QUIET))
    out.append(note(330, 330, "paintRadialGauge draws an 11px glyph and a 2px stroke", 11, INK))
    out.append(note(330, 347, "whatever the diameter, so a 40px ring reads emptier", 11, INK))
    out.append(note(330, 364, "than the 22px one it replaces.", 11, INK))
    out.append(note(330, 388, "Keeping this composition means growing the card: option C.", 11, WARN))
    return svg(W, H, "".join(out), DEFS)


# ---------------------------------------------------------------------------
# 4. Option C: the grown card
# ---------------------------------------------------------------------------
def option_c():
    W, H = 900, 420
    ox, oy = 150, 132
    grown_h = 128
    out = [heading(48, 44, "OPTION C · VIABLE, AT A COST", WARN,
                   "Grow the System card to 128px",
                   "Keeps the card title and the label-beside-ring reading. Home begins to scroll.")]
    out.append(f'<rect x="{ox - 24}" y="{oy - 24}" width="{CARD_W + 48}" '
               f'height="{grown_h + 48}" rx="10" fill="{BG}"/>')
    out.append(card(ox, oy, CARD_W, grown_h))
    cx, cy = ox + CARD_PADDING, oy + CARD_PADDING
    out.append(label(cx, cy + 16, "System", TITLE_SIZE, TEXT, anchor="start", weight=600))
    rows = [[("CPU", "42%", 0.42, "gA"), ("Memory", "25%", 0.25, "gA")],
            [("CPU temp", "65°C", 0.65, "gT"), ("GPU", "70%", 0.70, "gA")]]
    y = cy + TITLE_H + MARGIN_XS
    for row in rows:
        x = cx
        for name, value, frac, gid in row:
            out.append(ring(x, y, RING, frac, gid))
            if gid == "gT":
                out.append(centred(x, y, RING, "65°", 8))
            tx = x + RING + MARGIN_M
            out.append(label(tx, y + 14, name, CAPTION_SIZE, SUBTLE, anchor="start"))
            out.append(label(tx, y + 33, value, BODY_SIZE, TEXT, anchor="start"))
            x += 164 + MARGIN_M
        y += RING + MARGIN_XS
    out.append(dim_v(ox + CARD_W + 34, oy, oy + grown_h, f"{grown_h} card"))
    out.append(note(48, 310, "WHAT MOVES", 10, QUIET))
    for i, (name, before, after) in enumerate([
            ("ccCardH (System only)", "88", "128"),
            ("left column", "182", "222"),
            ("ccPageH", "480", "520"),
            ("scroll viewport", "480", "480 unchanged")]):
        yy = 330 + i * 17
        out.append(note(48, yy, name, 11, INK))
        out.append(note(230, yy, before, 11, QUIET))
        out.append(note(270, yy, "→", 11, QUIET))
        out.append(note(290, yy, after, 11, INK))
    out.append(note(430, 310, "THE CONSEQUENCE IS SCROLL, NOT CLIPPING", 10, QUIET))
    out.append(note(430, 330, "Home already sits in a KindScroll viewport of exactly 480px", 11, INK))
    out.append(note(430, 347, "inside a 700x564 panel, with 2px of slack. The panel does not", 11, INK))
    out.append(note(430, 364, "resize and no other Control Centre section is touched — each", 11, INK))
    out.append(note(430, 381, "carries its own page height.", 11, INK))
    return svg(W, H, "".join(out), DEFS)


# ---------------------------------------------------------------------------
# 5. The state sheet
# ---------------------------------------------------------------------------
STATES = [
    ("NORMAL", "a reading on every selector",
     [("CPU", "42%", .42, "gA", 14), ("Memory", "25%", .25, "gA", 14),
      ("Temp", "65°", .65, "gT", 13), ("GPU", "70%", .70, "gA", 14)], ()),
    ("VALID ZERO", "zero is a reading, not an absence",
     [("CPU", "0%", 0, "gA", 14), ("Memory", "0%", 0, "gA", 14),
      ("Temp", "0°", 0, "gT", 13), ("GPU", "0%", 0, "gA", 14)], ()),
    ("THREE-DIGIT MAXIMUM", "the widest value each ring must hold",
     [("CPU", "100%", 1, "gA", 12), ("Memory", "100%", 1, "gA", 12),
      ("Temp", "100°", 1, "gE", 12), ("GPU", "100%", 1, "gA", 12)], ()),
    ("ALL UNAVAILABLE", "no lease, failed read, or panel just opened",
     [("CPU", "—", 0, "gA", 14), ("Memory", "—", 0, "gA", 14),
      ("Temp", "—", 0, "gT", 14), ("GPU", "—", 0, "gA", 14)],
     ("CPU", "Memory", "Temp", "GPU")),
    ("GPU UNAVAILABLE", "usage invalid; the other three are untouched",
     [("CPU", "42%", .42, "gA", 14), ("Memory", "25%", .25, "gA", 14),
      ("Temp", "65°", .65, "gT", 13), ("GPU", "—", 0, "gA", 14)], ("GPU",)),
    ("GPU IDENTITY AMBIGUOUS", "multi-GPU snapshot cannot distinguish devices",
     [("CPU", "42%", .42, "gA", 14), ("Memory", "25%", .25, "gA", 14),
      ("Temp", "65°", .65, "gT", 13), ("GPU", "—", 0, "gA", 14)], ("GPU",)),
    ("TEMPERATURE WARNING", "75–85°C ramps amber toward the error role",
     [("CPU", "61%", .61, "gA", 14), ("Memory", "48%", .48, "gA", 14),
      ("Temp", "78°", .78, "gW", 13), ("GPU", "88%", .88, "gA", 14)], ()),
    ("TEMPERATURE CRITICAL", "above 85°C the arc ends at the error role",
     [("CPU", "94%", .94, "gA", 14), ("Memory", "71%", .71, "gA", 14),
      ("Temp", "92°", .92, "gE", 13), ("GPU", "97%", .97, "gA", 14)], ()),
]


def state_sheet():
    cols, colw, rowh = 2, 450, 150
    W = 60 + cols * colw
    H = 130 + ((len(STATES) + cols - 1) // cols) * rowh + 44
    out = [heading(48, 44, "STATE SHEET · RECOMMENDED COMPOSITION", INK,
                   "Every state, one geometry",
                   "Only the arc and the value change, so nothing reflows when a reading drops out.")]
    for i, (name, sub, slots, absent) in enumerate(STATES):
        ox = 48 + (i % cols) * colw
        oy = 128 + (i // cols) * rowh
        out.append(note(ox, oy, name, 10.5, INK))
        out.append(label(ox, oy + 15, sub, 11, QUIET, anchor="start", family=SANS))
        out.append(f'<rect x="{ox}" y="{oy + 24}" width="{CARD_W + 20}" '
                   f'height="{CARD_H + 20}" rx="8" fill="{BG}"/>')
        body, _, _ = home_card(ox + 10, oy + 34, slots, absent)
        out.append(body)
    foot = 128 + ((len(STATES) + cols - 1) // cols) * rowh + 6
    out.append(note(48, foot, "THE TWO GPU STATES ARE DELIBERATELY IDENTICAL ON SCREEN", 10, WARN))
    out.append(label(48, foot + 17,
                     "Both render unavailable, because the shell knows a usage it cannot attribute to a device. "
                     "They differ only in the accessible name and tooltip:",
                     11, "#3a3530", anchor="start", family=SANS))
    out.append(label(48, foot + 32,
                     "\u201cGPU usage: unavailable\u201d against \u201cGPU usage: device identity ambiguous\u201d. "
                     "Nothing in the geometry may imply a list position is being measured.",
                     11, "#3a3530", anchor="start", family=SANS))
    return svg(W, H, "".join(out), DEFS)


# ---------------------------------------------------------------------------
# 6. The same metrics across the three surfaces
# ---------------------------------------------------------------------------
def surfaces():
    W, H = 980, 560
    out = [heading(48, 44, "CARRYING THE DESIGN ACROSS SURFACES", INK,
                   "The same four metrics, three places",
                   "A ring is a glance and a graph is a history. What they share is the reading contract.")]

    # bar
    out.append(note(48, 126, "BAR WIDGET · 22px ring", 10, QUIET))
    out.append(f'<rect x="48" y="136" width="150" height="49" rx="8" fill="{BG}"/>')
    out.append(f'<rect x="60" y="148" width="126" height="25" rx="12" fill="{CARD}"/>')
    for i, (frac, gid) in enumerate([(.42, "gA"), (.25, "gA"), (.65, "gT")]):
        x = 66 + i * 28
        out.append(ring(x, 149.5, RING_SMALL, frac, gid))
        if gid == "gT":
            out.append(centred(x, 149.5, RING_SMALL, "65°", 8))
    out.append(label(48, 204, "Identity by glyph, value by tooltip —", 11, "#56504a",
                     anchor="start", family=SANS))
    out.append(label(48, 219, "the one sanctioned exception to P1.", 11, "#56504a",
                     anchor="start", family=SANS))

    # home
    out.append(note(260, 126, "CONTROL CENTRE HOME · 40px ring · approved", 10, GOOD))
    out.append(f'<rect x="260" y="136" width="{CARD_W + 24}" height="{CARD_H + 24}" rx="8" fill="{BG}"/>')
    body, _, _ = home_card(272, 148)
    out.append(body)
    out.append(label(260, 204, "Four metrics at a glance. The ring is the mark, the value lives",
                     11, "#56504a", anchor="start", family=SANS))
    out.append(label(260, 219, "inside it, the caption names it.", 11, "#56504a",
                     anchor="start", family=SANS))

    # console
    out.append(note(48, 262, "SYSTEM MONITOR CONSOLE · graph card", 10, QUIET))
    out.append(f'<rect x="48" y="272" width="646" height="128" rx="8" fill="{BG}"/>')
    for j, (title, value, extra, extra_col, pts) in enumerate([
            ("CPU", "42%", "65°C", TEXT,
             "0,44 24,38 48,41 72,28 96,33 120,20 144,26 168,14 192,22 216,11 240,18 264,9 285,15"),
            ("GPU", "70%", "NVIDIA 2b:00.0", SUBTLE,
             "0,30 24,26 48,31 72,18 96,24 120,12 144,19 168,8 192,16 216,6 240,13 264,4 285,10")]):
        cx0 = 60 + j * 311
        out.append(card(cx0, 284, 303, 104))
        out.append(label(cx0 + 9, 284 + 9 + 16, title, TITLE_SIZE, TEXT, anchor="start", weight=600))
        gy = 284 + 9 + TITLE_H + MARGIN_XS
        out.append(f'<g transform="translate({cx0 + 9},{gy})">'
                   f'<polyline points="{pts}" fill="none" stroke="{ACCENT}" stroke-width="1.5"/>'
                   f'</g>')
        out.append(label(cx0 + 9, gy + 52 + 14, value, BODY_SIZE, TEXT, anchor="start"))
        out.append(label(cx0 + 9 + 46, gy + 52 + 14, extra, BODY_SIZE, extra_col, anchor="start"))
    out.append(label(48, 418, "History, not glance. Temperature rides the CPU card's legend as a "
                              "second chip rather than claiming its own card.",
                     11, "#56504a", anchor="start", family=SANS))

    out.append(note(48, 452, "THE CONTRACT EVERY SURFACE KEEPS", 10, QUIET))
    principles = [
        ("P1 names itself", "A visible label carries identity — never colour, ring or position alone. Bar excepted: glyph plus tooltip."),
        ("P2 value with its mark", "Inside the ring on Home; in the legend under its own graph on the console."),
        ("P3 geometry survives", "Unavailable keeps the box and draws a dash. No stale value, no fabricated zero, no reflow."),
        ("P4 zero is a reading", "0% and 0°C render as themselves and never as the unavailable dash."),
        ("P5 one GPU identity", "selectGPU is the only chooser. Ambiguous devices go unavailable, never a list position."),
        ("P6 text scales", "Interior type derives from the box it sits in. No fixed 8px in a container whose size varies."),
    ]
    for i, (key, text) in enumerate(principles):
        yy = 472 + i * 15
        out.append(note(48, yy, key, 10, "#9a3412"))
        out.append(label(190, yy, text, 11, "#3a3530", anchor="start", family=SANS))
    return svg(W, H, "".join(out), DEFS)


FILES = {
    "option-b-40px-row-recommended.svg": option_b,
    "option-a-40px-grid.svg": option_a,
    "option-c-128px-card.svg": option_c,
    "rejected-22px-current.svg": rejected,
    "state-sheet.svg": state_sheet,
    "surfaces.svg": surfaces,
}

if __name__ == "__main__":
    for name, fn in FILES.items():
        path = os.path.join(OUT, name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(fn())
        print("wrote", name)
