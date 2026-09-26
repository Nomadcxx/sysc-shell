#!/usr/bin/env python3
"""Authoring-time generator: parametric cat poses -> svg/cat-*.svg. Not invoked by go build.

The cat is an original drawing for sysc-shell. Every pose is built from the
same geometric parts (torso masses, head, ears, tail, jointed legs), posed
per act -- sleeping, sitting, grooming, scratching, stretching, walking,
galloping -- then unioned into one silhouette so the glyph fills as a single
contour set with the eye cut out as a hole.

Requires shapely (tested with 2.1.2). Run it, then build.py.
"""

import math
import sys
from pathlib import Path

from shapely.geometry import LineString, Point, Polygon
from shapely.geometry.polygon import orient as shapely_orient
from shapely.ops import unary_union
from shapely import affinity

GRID = 24.0


def seg(a, b, w0, w1=None):
    """A tapered limb segment from a to b with round ends."""
    if w1 is None:
        w1 = w0
    ca = Point(a).buffer(w0 / 2, quad_segs=16)
    cb = Point(b).buffer(w1 / 2, quad_segs=16)
    return unary_union([ca, cb]).convex_hull


def chain(origin, angles, lengths, widths):
    """Forward kinematics: absolute angles in degrees, 0 = straight down,
    positive swings toward +x (the direction the cat faces)."""
    pts = [origin]
    x, y = origin
    for a, l in zip(angles, lengths):
        r = math.radians(a)
        x += math.sin(r) * l
        y += math.cos(r) * l
        pts.append((x, y))
    parts = []
    for i in range(len(angles)):
        parts.append(seg(pts[i], pts[i + 1], widths[i], widths[i + 1]))
    return unary_union(parts), pts


def ellipse(cx, cy, rx, ry, rot=0.0):
    e = affinity.scale(Point(0, 0).buffer(1, quad_segs=32), rx, ry)
    e = affinity.rotate(e, rot, origin=(0, 0))
    return affinity.translate(e, cx, cy)


def tail(root, angles, lengths, w0, w1):
    shape, _ = chain(root, angles, lengths, [w0] + [w0 + (w1 - w0) * (i + 1) / len(angles) for i in range(len(angles))])
    return shape


def head(hx, hy, tilt=0.0, eye="open"):
    """Skull, muzzle and ears around (hx, hy), turned by tilt degrees, and
    the eye to cut out of the finished silhouette: an open eye is a hole, a
    closed one a lid-shaped slit, and "none" leaves the head solid."""
    skull = ellipse(hx, hy, 2.75, 2.5)
    muzzle = ellipse(hx + 1.85, hy + 0.85, 1.15, 0.95)
    ear1 = Polygon([(hx - 2.3, hy - 0.8), (hx - 1.75, hy - 4.3), (hx - 0.05, hy - 2.0)])
    ear2 = Polygon([(hx - 0.2, hy - 2.0), (hx + 1.05, hy - 4.45), (hx + 2.25, hy - 0.85)])
    shape = unary_union([skull, muzzle, ear1.buffer(0.25, join_style=1), ear2.buffer(0.25, join_style=1)])
    if eye == "open":
        hole = ellipse(hx + 0.85, hy - 0.1, 0.45, 0.55)
    elif eye == "closed":
        hole = LineString([(hx + 0.2, hy - 0.1), (hx + 0.85, hy + 0.3), (hx + 1.5, hy - 0.1)]).buffer(0.22, cap_style=1, join_style=1)
    else:
        hole = Polygon()
    rot = lambda g: affinity.rotate(g, tilt, origin=(hx, hy))
    return rot(shape), rot(hole)


def run_pose(p):
    """A standing or moving cat; p is a dict of pose parameters."""
    hip = (p["hip_x"], p["hip_y"])
    chest = (p["chest_x"], p["chest_y"])
    # Torso: hip and chest masses bridged by a hull, with a raised mid mass
    # for the arch of the back.
    hipm = ellipse(hip[0], hip[1], 3.0, 2.8, p.get("hip_rot", 0))
    chestm = ellipse(chest[0], chest[1], 3.2, 3.0, p.get("chest_rot", 0))
    midx = (hip[0] + chest[0]) / 2
    midy = (hip[1] + chest[1]) / 2 - p.get("arch", 0.0)
    midm = ellipse(midx, midy, 2.7, 2.5)
    torso = unary_union([hipm, chestm, midm])
    torso = unary_union([torso, unary_union([hipm, midm]).convex_hull,
                         unary_union([midm, chestm]).convex_hull])

    # Head, bridged to the chest by a neck.
    hx, hy = p["head_x"], p["head_y"]
    tilt = p.get("head_tilt", 0.0)
    headm, eye = head(hx, hy, tilt, p.get("eye", "open"))
    neck = unary_union([ellipse(chest[0] + 0.8, chest[1] - 0.6, 2.2, 2.2), ellipse(hx - 0.6, hy + 0.6, 1.7, 1.7)]).convex_hull

    # Legs. Front: upper arm, forearm, paw. Hind: thigh, shin, foot.
    parts = [torso, headm, neck]
    for leg in ("ff", "fn"):  # far, near front
        a = p[leg]
        shape, _ = chain((chest[0] + 0.6, chest[1] + 1.0), a, [3.2, 3.1, 1.2], [2.7, 1.75, 1.5, 1.45])
        parts.append(shape)
    for leg in ("hf", "hn"):
        a = p[leg]
        shape, _ = chain((hip[0] - 0.2, hip[1] + 0.5), a, [3.3, 3.0, 1.7], [3.4, 1.85, 1.55, 1.45])
        parts.append(shape)
    parts.append(tail((hip[0] - 2.2, hip[1] - 1.0), p["tail"], [2.4, 2.3, 2.2, 2.0], 1.75, 1.3))

    # The eye is a hole: it reads at panel sizes and closes up cleanly in
    # the bar, where it is smaller than a pixel.
    return unary_union(parts).difference(eye)


# Rotary gallop, six keyframes. Leg angle triples are absolute segment
# angles: 0 is straight down, positive reaches forward. The glyphs are not
# these keyframes but RUN_STEPS poses sampled from a periodic spline through
# them, so each pose is a small, even move from the last.
BASE = dict(hip_x=7.2, hip_y=12.6, chest_x=14.2, chest_y=12.4, head_x=18.6, head_y=9.2)

FRAMES = [
    # 0: gathered suspension -- all four feet bunched under the belly, back
    # arched high.
    dict(BASE, hip_x=7.9, chest_x=13.6, arch=1.3, hip_y=12.9, chest_y=12.9, head_y=9.8, head_x=18.0, head_tilt=6,
         ff=[-35, 10, 60], fn=[-50, -5, 45],
         hf=[40, -20, 30], hn=[55, -5, 45],
         tail=[-95, -130, -165, -195]),
    # 1: hind feet strike and plant; fronts start reaching.
    dict(BASE, arch=0.6, head_y=9.3,
         ff=[10, 35, 80], fn=[-5, 25, 70],
         hf=[25, -25, 15], hn=[10, -30, 5],
         tail=[-100, -135, -165, -190]),
    # 2: hind push, fronts reaching far forward.
    dict(BASE, arch=-0.1, hip_y=12.2, chest_y=11.8, head_y=8.9, head_tilt=-4,
         ff=[40, 60, 95], fn=[25, 50, 90],
         hf=[-10, -45, -20], hn=[-30, -60, -40],
         tail=[-105, -135, -160, -180]),
    # 3: extended suspension -- body long and flat, legs at full stretch.
    dict(BASE, hip_x=6.4, chest_x=14.9, head_x=19.3, arch=-0.4, hip_y=12.0, chest_y=12.0, head_y=9.0, head_tilt=-6,
         ff=[55, 70, 100], fn=[45, 65, 100],
         hf=[-45, -70, -85], hn=[-60, -80, -95],
         tail=[-110, -135, -155, -175]),
    # 4: front feet strike, hinds swinging forward.
    dict(BASE, arch=0.1, hip_y=12.4, chest_y=12.7, head_y=9.6, head_tilt=4,
         ff=[20, 5, 50], fn=[35, 20, 70],
         hf=[-20, -50, -30], hn=[5, -30, 0],
         tail=[-105, -135, -165, -185]),
    # 5: fronts push back under the chest, hinds reach under the belly.
    dict(BASE, hip_x=7.6, chest_x=13.9, arch=0.8, hip_y=12.8, chest_y=12.8, head_y=9.7, head_x=18.3, head_tilt=5,
         ff=[-15, -30, 10], fn=[-25, -40, 0],
         hf=[30, -5, 30], hn=[45, 0, 40],
         tail=[-100, -130, -165, -190]),
]


# RUN_STEPS is how many poses one stride is cut into. Twelve keeps a sprint
# at a stride every 0.4 s to thirty poses a second, the most the host is
# asked to paint for the cat.
RUN_STEPS = 12


def catmull_rom(p0, p1, p2, p3, t):
    t2, t3 = t * t, t * t * t
    return 0.5 * (2 * p1 + (p2 - p0) * t + (2 * p0 - 5 * p1 + 4 * p2 - p3) * t2 + (3 * p1 - p0 - 3 * p2 + p3) * t3)


def inbetween(keys, t):
    """The pose at t keyframes into the cycle, on a closed Catmull-Rom spline
    through every parameter, so velocity is continuous across the seam."""
    n = len(keys)
    i = int(math.floor(t)) % n
    f = t - math.floor(t)
    k0, k1, k2, k3 = keys[(i - 1) % n], keys[i], keys[(i + 1) % n], keys[(i + 2) % n]
    out = {}
    for name in set(k0) | set(k1) | set(k2) | set(k3):
        vals = [k.get(name, 0.0) for k in (k0, k1, k2, k3)]
        if isinstance(vals[1], list):
            out[name] = [catmull_rom(*(v[j] for v in vals), f) for j in range(len(vals[1]))]
        else:
            out[name] = catmull_rom(*vals, f)
    return out


def run_poses():
    return [run_pose(inbetween(FRAMES, i * len(FRAMES) / RUN_STEPS)) for i in range(RUN_STEPS)]


def sleep_pose(breath, z):
    """A loaf curled on the baseline with its tail wrapped under. breath,
    zero through one, swells the flank; z holds the two z marks, which rise
    and grow through the cycle."""
    body = ellipse(11.2, 17.6 - 0.25 * breath, 7.4, 3.9 + 0.3 * breath)
    haunch = ellipse(8.0, 16.4 - 0.3 * breath, 4.3 + 0.1 * breath, 4.4 + 0.35 * breath)
    hx, hy = 16.3, 16.6
    skull = ellipse(hx, hy, 2.8, 2.45)
    ear1 = Polygon([(hx - 2.2, hy - 1.0), (hx - 1.6, hy - 4.0), (hx - 0.1, hy - 1.9)]).buffer(0.25, join_style=1)
    ear2 = Polygon([(hx - 0.1, hy - 1.9), (hx + 1.2, hy - 4.2), (hx + 2.2, hy - 1.0)]).buffer(0.25, join_style=1)
    paws = ellipse(17.3, 20.3, 3.3, 1.35)
    tail_shape = tail((4.2, 19.3), [55, 80, 95, 100], [2.8, 3.2, 3.2, 2.6], 1.8, 1.3)
    cat = unary_union([body, haunch, skull, ear1, ear2, paws, tail_shape])
    # Closed eye: a thin arc cut into the head.
    lid = LineString([(hx - 0.2, hy + 0.1), (hx + 0.6, hy + 0.6), (hx + 1.4, hy + 0.1)]).buffer(0.26, cap_style=1, join_style=1)
    cat = cat.difference(lid)
    return unary_union([cat] + [zglyph(zx, zy, s) for zx, zy, s in z])


def zglyph(x, y, s):
    w = 0.34 * s + 0.2
    return LineString([(x, y), (x + s, y), (x, y + s), (x + s, y + s)]).buffer(w / 2, cap_style=2, join_style=2)


# The breathing cycle: inhale, hold, exhale, rest. The z marks drift up
# and swell as the breath comes in, and settle back on the way out.
SLEEP = [
    (0.0, [(18.0, 6.4, 2.4), (21.0, 2.6, 1.6)]),
    (0.6, [(18.2, 5.9, 2.6), (21.2, 2.0, 1.6)]),
    (1.0, [(18.4, 5.4, 2.8), (21.4, 1.4, 1.5)]),
    (0.5, [(18.2, 5.9, 2.6), (21.2, 2.0, 1.6)]),
]


def sit_pose(p):
    """A cat sitting upright, facing right: haunch on the floor, chest up,
    forelegs straight to the floor, tail along the ground."""
    lean = p.get("lean", 0.0)
    hx0, hy0 = 8.6, 17.4
    haunch = ellipse(hx0, hy0, 4.3, 3.9)
    cx, cy = 12.4 + lean, 12.9
    chest = ellipse(cx, cy, 2.6, 3.6, -18 + lean * 6)
    torso = unary_union([haunch, chest, unary_union([haunch, chest]).convex_hull])
    hx, hy = p.get("head_x", 14.0) + lean, p.get("head_y", 7.4)
    headm, eye = head(hx, hy, p.get("head_tilt", 0.0), p.get("eye", "open"))
    neck = unary_union([ellipse(cx + 0.4, cy - 1.6, 2.0, 2.0), ellipse(hx - 0.5, hy + 0.9, 1.7, 1.7)]).convex_hull
    parts = [torso, headm, neck]
    shoulder = (cx + 0.9, cy + 1.2)
    for leg in ("ff", "fn"):
        shape, _ = chain(shoulder, p[leg], [3.6, 3.4, 1.1], [2.4, 1.7, 1.5, 1.45])
        parts.append(shape)
    # Folded hind foot in front of the haunch.
    if "hn" in p:
        # The raised hind leg pivots at the hip joint, high on the haunch.
        shape, _ = chain((hx0 + 1.6, hy0 - 1.8), p["hn"], [3.6, 3.8, 2.0], [3.0, 1.8, 1.55, 1.45])
        parts.append(shape)
    else:
        parts.append(ellipse(hx0 + 3.1, 20.9, 1.9, 0.85))
    parts.append(tail((hx0 - 3.0, 20.0), p["tail"], [2.3, 2.3, 2.2, 2.0], 1.75, 1.3))
    return unary_union(parts).difference(eye)


# The tail lies back along the floor and its tip curls up and swishes.
SIT_TAIL = [
    [-92, -100, -125, -160],
    [-92, -104, -140, -180],
    [-92, -108, -150, -195],
]
UP = [2, 0, 90]


# The catalogue holds each distinct pose once. A cycle that returns through
# a pose -- the tail swinging back, a second lick -- repeats its name in the
# plugin's frame list, which costs nothing in the font.


def sit_frames():
    # Three tail positions for the swish, then the middle one blinking, so a
    # cycle can blink once every few swishes rather than on every pass.
    poses = [dict(ff=UP, fn=[-2, 0, 90], tail=t) for t in SIT_TAIL]
    poses.append(dict(poses[1], eye="closed"))
    return [sit_pose(p) for p in poses]


def groom_frames():
    # Paw up to the mouth, lick twice, then wash over the ear and back.
    lick_a = dict(ff=UP, fn=[95, 175, 150], head_x=14.3, head_y=7.9, head_tilt=10, eye="closed", tail=SIT_TAIL[0])
    lick_b = dict(ff=UP, fn=[100, 180, 160], head_x=14.2, head_y=8.3, head_tilt=16, eye="closed", tail=SIT_TAIL[1])
    wash_a = dict(ff=UP, fn=[125, 192, 200], head_x=13.8, head_y=9.0, head_tilt=24, eye="closed", tail=SIT_TAIL[2])
    wash_b = dict(ff=UP, fn=[135, 186, 205], head_x=13.7, head_y=9.2, head_tilt=28, eye="closed", tail=SIT_TAIL[1])
    return [sit_pose(dict(ff=UP, fn=[60, 150, 120], head_tilt=6, tail=SIT_TAIL[0])),
            sit_pose(lick_a), sit_pose(lick_b), sit_pose(wash_a), sit_pose(wash_b)]


def scratch_frames():
    # Lean back on the haunch, head cocked down to the raised hind foot,
    # which rakes behind the ear.
    out = []
    # Leg lifting, then the two ends of the rake.
    rakes = [(120, 170, 195), (150, 190, 215), (160, 200, 230)]
    for i, (a, b, c) in enumerate(rakes):
        out.append(sit_pose(dict(ff=[8, 2, 90], fn=[2, 0, 90], hn=[a, b, c], lean=0.4,
                                 head_x=13.9, head_y=8.6, head_tilt=-24, eye="closed" if i else "open",
                                 tail=SIT_TAIL[i])))
    return out


def stretch_frames():
    stand = dict(hip_x=7.2, hip_y=12.6, chest_x=14.2, chest_y=12.6, head_x=18.6, head_y=9.0,
                 ff=[5, 0, 90], fn=[-5, 0, 90], hf=[10, -10, 20], hn=[0, -15, 20],
                 tail=[-120, -150, -175, -195])
    bow = dict(hip_x=7.4, hip_y=11.4, chest_x=14.6, chest_y=15.2, head_x=19.2, head_y=14.0, head_tilt=-8, arch=-0.6,
               ff=[72, 85, 95], fn=[64, 80, 95], hf=[0, -8, 30], hn=[-8, -15, 25],
               tail=[-130, -160, -185, -200])
    deep = dict(bow, chest_y=16.2, head_y=15.2, head_x=19.6, ff=[80, 90, 95], fn=[74, 86, 95], tail=[-140, -170, -195, -205])
    yawn = dict(deep, head_y=13.8, head_tilt=-24, eye="closed")
    return [run_pose(stand), run_pose(bow), run_pose(deep), run_pose(yawn)]


# WALK_STEPS poses cover half a stride.
WALK_STEPS = 8


def walk_frames():
    out = []
    for i in range(WALK_STEPS):
        # A walk's silhouette repeats every half stride -- near and far
        # legs trade places and look the same -- so the poses sample half a
        # stride and one pass of the cycle is one step.
        th = math.pi * i / WALK_STEPS
        legs = {}
        # Lateral-sequence walk: hind left, fore left, hind right, fore right.
        for leg, phase in (("hn", 0.0), ("fn", 0.25), ("hf", 0.5), ("ff", 0.75)):
            a = th + 2 * math.pi * phase
            swing = max(0.0, math.cos(a))  # leg travelling forward
            upper = 24 * math.sin(a)
            if leg[0] == "f":
                legs[leg] = [upper, upper - 45 * swing, 90 - 30 * swing]
            else:
                legs[leg] = [upper + 8, upper - 25 - 30 * swing, 25 + 30 * swing]
        bob = 0.25 * math.cos(2 * th)
        tail_sway = 8 * math.sin(th)
        out.append(run_pose(dict(hip_x=7.2, hip_y=12.2 + bob, chest_x=14.2, chest_y=12.2 + bob,
                                 head_x=18.6, head_y=8.8 + bob, head_tilt=0, arch=0.2, **legs,
                                 tail=[-115 + tail_sway, -140 + tail_sway, -165 + tail_sway, -185 + tail_sway])))
    return out


def polys(geom):
    if geom.geom_type == "Polygon":
        return [geom]
    return list(geom.geoms)


def to_path(geom):
    out = []
    for poly in orient(geom):
        poly = poly.simplify(0.01)
        rings = [poly.exterior] + list(poly.interiors)
        for ring in rings:
            coords = list(ring.coords)
            d = "M" + " L".join(f"{x:.3f} {y:.3f}" for x, y in coords[:-1]) + " Z"
            out.append(d)
    return " ".join(out)


def orient(geom):
    """Exteriors counter-clockwise in the raw SVG numbers and holes the
    other way, so after build.py's y-flip outers wind clockwise, the
    TrueType convention, and every hole winds against its outer."""
    return [shapely_orient(p, sign=1.0) for p in polys(geom)]


def svg(geom, title):
    d = to_path(geom)
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">'
            f"<title>{title}</title><path d=\"{d}\"/></svg>\n")


# Each cycle shares one fit so the cat does not jitter between frames:
# the union of every pose is scaled into the grid with a margin and centred
# on it. The host centres an icon's box, not its ink, so a cat that sat on a
# baseline would ride low in the bar.
MARGIN = 0.4


def fit(shapes):
    minx = min(g.bounds[0] for g in shapes)
    miny = min(g.bounds[1] for g in shapes)
    maxx = max(g.bounds[2] for g in shapes)
    maxy = max(g.bounds[3] for g in shapes)
    s = min((GRID - 2 * MARGIN) / (maxx - minx), (GRID - 2 * MARGIN) / (maxy - miny))
    dx = (GRID - (maxx - minx) * s) / 2 - minx * s
    dy = (GRID - (maxy - miny) * s) / 2 - miny * s
    return [affinity.translate(affinity.scale(g, s, s, origin=(0, 0)), dx, dy) for g in shapes]


# ACTS is every cycle the catalogue carries, in codepoint order. The
# plugin picks an act by the machine's load and, while idle, by chance;
# the host steps through the chosen act's poses.
ACTS = [
    ("sleep", lambda: [sleep_pose(b, z) for b, z in SLEEP]),
    ("sit", sit_frames),
    ("groom", groom_frames),
    ("scratch", scratch_frames),
    ("stretch", stretch_frames),
    ("walk", walk_frames),
    ("run", run_poses),
]


def frames():
    """Every pose of every act under one shared fit, so the cat keeps its
    size and its floor line when the plugin switches from one act to the
    next."""
    names, shapes = [], []
    for act, build in ACTS:
        poses = build()
        names += [f"cat-{act}-{i}" for i in range(len(poses))]
        shapes += poses
    return dict(zip(names, fit(shapes)))


def main():
    dest = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parent / "svg"
    for name, geom in frames().items():
        minx, miny, maxx, maxy = geom.bounds
        if minx < 0 or miny < 0 or maxx > GRID or maxy > GRID:
            raise SystemExit(f"{name} leaves the 24 grid: {geom.bounds}")
        (dest / f"{name}.svg").write_text(svg(geom, name))
        print(f"wrote {name}.svg")


if __name__ == "__main__":
    main()
