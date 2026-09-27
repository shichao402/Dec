#!/usr/bin/env python3
"""Generate the full platform icon set for Dec from the pure logo SVG.

Platform templates (all 1024x1024 canvases):

- macOS   : transparent corners, artwork scaled to 92% and optically
            lifted a touch; Apple's rounded-rect silhouette is painted
            by us with a subtle vertical gradient (system masks nothing).
- Windows : rounded-square cream plate, full-bleed-ish, artwork at 88%,
            corners transparent.
- iOS     : full-bleed square (no transparency), artwork at 72%.
- Android : full-bleed cream square for legacy launchers; adaptive
            foreground keeps artwork inside the 66% safe zone.

Outputs are written into client/src-tauri/icons/ (same filenames as
before, so tauri.conf.json needs no changes).
"""

import argparse
import math
import os
import struct
import subprocess
import sys
import tempfile
import zlib

import cairosvg
from PIL import Image, ImageDraw

CREAM = (236, 234, 222)          # #ECEADE warm cream plate
CREAM_LIGHT = (241, 238, 229)    # gradient top for macOS plate
INK = (35, 33, 30)               # #23211E artwork ink

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DEFAULT_SVG = os.path.join(REPO, "assets", "branding", "dec-logo.svg")
DEFAULT_OUT = os.path.join(REPO, "client", "src-tauri", "icons")


# ----------------------------------------------------------------------------
# canvas templates

def rounded_rect_mask(size, radius):
    mask = Image.new("L", (size, size), 0)
    d = ImageDraw.Draw(mask)
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=radius, fill=255)
    return mask


def canvas_macos(size):
    """Transparent corners; squircle-ish cream plate at 92% width.

    The plate silhouette mimics Apple's macOS icon shape (continuous
    corners). We fake the continuous curvature with a plain rounded
    rect; at icon sizes the difference is invisible.
    """
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    plate_inset = round(size * 0.04)          # plate occupies 92% of canvas
    plate_size = size - 2 * plate_inset
    radius = round(plate_size * 0.225)        # macOS corner ratio ~22.5%

    # subtle vertical gradient on the plate
    grad = Image.new("RGBA", (plate_size, plate_size))
    top, bottom = CREAM_LIGHT, CREAM
    for y in range(plate_size):
        t = y / max(1, plate_size - 1)
        r = round(top[0] + (bottom[0] - top[0]) * t)
        g = round(top[1] + (bottom[1] - top[1]) * t)
        b = round(top[2] + (bottom[2] - top[2]) * t)
        ImageDraw.Draw(grad).line([(0, y), (plate_size, y)], fill=(r, g, b, 255))

    mask = rounded_rect_mask(plate_size, radius)
    img.paste(grad, (plate_inset, plate_inset), mask)
    return img, plate_inset, plate_size


def canvas_windows(size):
    """Rounded-square cream plate, corners transparent, plate fills 100%."""
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    radius = round(size * 0.16)
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=radius,
                        fill=CREAM + (255,))
    return img, 0, size


def canvas_ios(size):
    """Full-bleed opaque square; the system applies the corner mask."""
    img = Image.new("RGBA", (size, size), CREAM + (255,))
    return img, 0, size


def canvas_android(size):
    """Full-bleed cream square for legacy launcher icons."""
    img = Image.new("RGBA", (size, size), CREAM + (255,))
    return img, 0, size


def canvas_android_foreground(size):
    """Adaptive-icon foreground: transparent bg, artwork in 66% safe zone."""
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    return img, 0, size


# ----------------------------------------------------------------------------
# artwork placement

def render_logo(size, svg_path):
    """Render the logo SVG to an RGBA image of the given pixel size."""
    png = cairosvg.svg2png(url=svg_path, output_width=size, output_height=size)
    import io
    return Image.open(io.BytesIO(png)).convert("RGBA")


def compose(template_img, plate_inset, plate_size, logo_img, artwork_ratio):
    """Paste the logo centered on the plate, artwork_ratio of plate width."""
    art = round(plate_size * artwork_ratio)
    logo = logo_img.resize((art, art), Image.LANCZOS)
    off = plate_inset + (plate_size - art) // 2
    template_img.alpha_composite(logo, (off, off))
    return template_img


# ----------------------------------------------------------------------------
# writers

def write_png(img, path):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    img.save(path, "PNG")


def write_ico(imgs, path):
    """imgs: list of RGBA PIL images (ascending sizes)."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    blobs = []
    for im in imgs:
        blobs.append(im.tobytes())  # placeholder, real encoding below
    # ICO with PNG-compressed entries (Vista+)
    header = struct.pack("<HHH", 0, 1, len(imgs))
    offset = 6 + 16 * len(imgs)
    entries = b""
    payload = b""
    for im in imgs:
        w, h = im.size
        b = io_png_bytes(im)
        dim = 0 if w >= 256 else w
        entries += struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32,
                               len(b), offset)
        offset += len(b)
        payload += b
    with open(path, "wb") as f:
        f.write(header + entries + payload)


def io_png_bytes(img):
    import io
    buf = io.BytesIO()
    img.save(buf, "PNG")
    return buf.getvalue()


def write_icns(imgs_by_size, path):
    """imgs_by_size: dict {pixel_size: PIL image}. Uses PNG payloads."""
    chunks = [
        ("icp4", 16), ("icp5", 32), ("icp6", 64),
        ("ic07", 128), ("ic08", 256), ("ic09", 512), ("ic10", 1024),
        ("ic11", 32), ("ic12", 64), ("ic13", 256), ("ic14", 1024),
    ]
    body = b""
    for typ, size in chunks:
        png = io_png_bytes(imgs_by_size[size])
        body += typ.encode() + struct.pack(">I", len(png) + 8) + png
    with open(path, "wb") as f:
        f.write(b"icns" + struct.pack(">I", len(body) + 8) + body)


# ----------------------------------------------------------------------------
# main

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--svg", default=DEFAULT_SVG)
    ap.add_argument("--out", default=DEFAULT_OUT)
    args = ap.parse_args()

    out = args.out
    logo_cache = {}

    def logo(size):
        if size not in logo_cache:
            logo_cache[size] = render_logo(size, args.svg)
        return logo_cache[size]

    # ---- macOS: icon.png (1024), icon.icns, 128, 256(128@2x), 32
    macos_art = 0.92
    macos_sizes = {16: "icp4", 32: "icp5", 64: "icp6", 128: "ic07",
                   256: "ic08", 512: "ic09", 1024: "ic10"}
    macos_imgs = {}
    for s in sorted(macos_sizes):
        canvas, inset, plate = canvas_macos(s)
        macos_imgs[s] = compose(canvas, inset, plate, logo(s), macos_art)
    # logo(s) renders the SVG at each target size directly, so the
    # artwork stays crisp at every size (no downscale blur).

    write_png(macos_imgs[1024], os.path.join(out, "icon.png"))
    write_png(macos_imgs[128], os.path.join(out, "128x128.png"))
    write_png(macos_imgs[256], os.path.join(out, "128x128@2x.png"))
    write_png(macos_imgs[32], os.path.join(out, "32x32.png"))
    write_png(macos_imgs[64], os.path.join(out, "64x64.png"))
    write_icns(macos_imgs, os.path.join(out, "icon.icns"))
    print("macOS set written")

    # ---- Windows: Square*Logo, StoreLogo, icon.ico
    win_art = 0.88
    win_targets = {
        "Square30x30Logo.png": 30, "Square44x44Logo.png": 44,
        "StoreLogo.png": 50, "Square71x71Logo.png": 71,
        "Square89x89Logo.png": 89, "Square107x107Logo.png": 107,
        "Square142x142Logo.png": 142, "Square150x150Logo.png": 150,
        "Square284x284Logo.png": 284, "Square310x310Logo.png": 310,
    }
    win_imgs = {}
    for name, s in win_targets.items():
        canvas, inset, plate = canvas_windows(s)
        img = compose(canvas, inset, plate, logo(s), win_art)
        win_imgs[s] = img
        write_png(img, os.path.join(out, name))
    ico_sizes = [16, 24, 32, 48, 64, 128, 256]
    ico_imgs = []
    for s in ico_sizes:
        canvas, inset, plate = canvas_windows(s)
        ico_imgs.append(compose(canvas, inset, plate, logo(s), win_art))
    write_ico(ico_imgs, os.path.join(out, "icon.ico"))
    print("Windows set written")

    # ---- iOS: full-bleed opaque
    ios_art = 0.72
    ios_targets = {
        "AppIcon-20x20@1x.png": 20, "AppIcon-20x20@2x.png": 40,
        "AppIcon-20x20@3x.png": 60, "AppIcon-29x29@1x.png": 29,
        "AppIcon-29x29@2x.png": 58, "AppIcon-29x29@3x.png": 87,
        "AppIcon-40x40@1x.png": 40, "AppIcon-40x40@2x.png": 80,
        "AppIcon-40x40@3x.png": 120, "AppIcon-60x60@2x.png": 120,
        "AppIcon-60x60@3x.png": 180, "AppIcon-76x76@1x.png": 76,
        "AppIcon-76x76@2x.png": 152, "AppIcon-83.5x83.5@2x.png": 167,
        "AppIcon-512@2x.png": 1024,
    }
    for name, s in ios_targets.items():
        canvas, inset, plate = canvas_ios(s)
        img = compose(canvas, inset, plate, logo(s), ios_art)
        write_png(img, os.path.join(out, "ios", name))
    print("iOS set written")

    # ---- Android: legacy + adaptive foreground
    android_art_legacy = 0.80
    android_art_fg = 0.62          # of full canvas, inside 66% safe zone
    densities = {"mdpi": 48, "hdpi": 72, "xhdpi": 96, "xxhdpi": 144,
                 "xxxhdpi": 192}
    for dpi, s in densities.items():
        d = os.path.join(out, "android", f"mipmap-{dpi}")
        # legacy launcher: cream full bleed
        canvas, inset, plate = canvas_android(s)
        img = compose(canvas, inset, plate, logo(s), android_art_legacy)
        write_png(img, os.path.join(d, "ic_launcher.png"))
        write_png(img, os.path.join(d, "ic_launcher_round.png"))
        # adaptive foreground: transparent, safe-zone artwork
        fgc, fi, fp = canvas_android_foreground(s)
        fgi = compose(fgc, fi, fp, logo(s), android_art_fg)
        write_png(fgi, os.path.join(d, "ic_launcher_foreground.png"))
    print("Android set written")

    print("done:", out)


if __name__ == "__main__":
    main()
