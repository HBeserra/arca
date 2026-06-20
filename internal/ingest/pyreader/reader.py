#!/usr/bin/env python3
"""
reader.py - StitchVault embroidery reader sidecar.

Reads ONE embroidery file with pyembroidery and writes a neutral JSON document to
stdout for the Go pyreader adapter to decode. This is the only place Python lives
in the pipeline; everything downstream (render, metadata, search) is Go.

Output JSON:
  {
    "format":   ".pes",
    "stitches": [[x_mm, y_mm, cmd_int], ...],
    "threads":  [{"r":255,"g":0,"b":0,"description":"","brand":"","catalog":""}, ...],
    "extras":   {"key": "value", ...}
  }

Conventions (MUST match internal/embroidery/embroidery.go):
  - coordinates in millimetres -- pyembroidery uses 1/10 mm, so divide by 10
  - Y axis points DOWN (image convention); if a sample renders vertically
    mirrored, flip INVERT_Y below -- this is the single axis-correction point.
  - command ints: Stitch=0 Jump=1 ColorChange=2 Trim=3 Stop=4 End=5 SequinEject=6
"""
import json
import os
import sys

INVERT_Y = False  # see module docstring


def const(pe, name, default):
    return getattr(pe, name, default)


def channel(thread, getter, shift):
    """Best-effort RGB channel extraction across pyembroidery versions."""
    fn = getattr(thread, getter, None)
    if callable(fn):
        try:
            return int(fn()) & 0xFF
        except Exception:
            pass
    color = getattr(thread, "color", 0) or 0
    try:
        return (int(color) >> shift) & 0xFF
    except Exception:
        return 0


def main(argv):
    if len(argv) < 2:
        sys.stderr.write("usage: reader.py <file>\n")
        return 2
    path = argv[1]

    try:
        import pyembroidery as pe
    except ImportError as e:
        sys.stderr.write("pyembroidery not installed: %s\n" % e)
        return 3

    pattern = pe.read(path)
    if pattern is None:
        sys.stderr.write("unsupported or unreadable file: %s\n" % path)
        return 4

    # pyembroidery base command -> our canonical int
    cmd_map = {
        const(pe, "STITCH", 0): 0,
        const(pe, "JUMP", 1): 1,
        const(pe, "COLOR_CHANGE", 5): 2,
        const(pe, "NEEDLE_SET", 9): 2,   # treat needle set as a colour change
        const(pe, "TRIM", 2): 3,
        const(pe, "STOP", 3): 4,
        const(pe, "END", 4): 5,
        const(pe, "SEQUIN_EJECT", 7): 6,
    }
    command_mask = const(pe, "COMMAND_MASK", 0xFF)

    stitches = []
    for st in pattern.stitches:
        x = st[0] / 10.0
        y = st[1] / 10.0
        if INVERT_Y:
            y = -y
        base = int(st[2]) & command_mask
        stitches.append([x, y, cmd_map.get(base, 0)])

    threads = []
    for t in getattr(pattern, "threadlist", []) or []:
        threads.append({
            "r": channel(t, "get_red", 16),
            "g": channel(t, "get_green", 8),
            "b": channel(t, "get_blue", 0),
            "description": str(getattr(t, "description", "") or ""),
            "brand": str(getattr(t, "brand", "") or ""),
            "catalog": str(getattr(t, "catalog_number", "") or ""),
        })

    extras = {}
    for k, v in (getattr(pattern, "extras", {}) or {}).items():
        try:
            extras[str(k)] = str(v)
        except Exception:
            pass

    json.dump({
        "format": os.path.splitext(path)[1].lower(),
        "stitches": stitches,
        "threads": threads,
        "extras": extras,
    }, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
