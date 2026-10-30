#!/usr/bin/env python3
"""由品牌 SVG 导出各尺寸位图。

这是 assets/branding/dec-logo.svg 的唯一渲染入口，保证所有尺寸图同源。

用法：
    python3 scripts/gen-brand-images.py [--check]

加 --check 时只按当前 SVG 重新渲染并与磁盘文件比对，不写入。
"""

from __future__ import annotations

import sys
from pathlib import Path

import cairosvg
from PIL import Image

ROOT = Path(__file__).resolve().parent.parent
SVG = ROOT / "assets" / "branding" / "dec-logo.svg"
OUT_DIR = ROOT / "assets" / "branding"

# 需要产出的尺寸。1024 是主图标，其余供安装包与界面使用。
TARGETS = {
    "dec-icon-1024.png": 1024,
    "dec-icon-512.png": 512,
    "dec-icon-256.png": 256,
    "dec-icon-128.png": 128,
}

# 品牌底色：米白。图标带这层底，与菜单栏里的透明单色版区分开。
BG = "#f2efe9"
# 主体墨色：近黑。
INK = "#22201e"


def render(size: int) -> bytes:
    """渲染成 PNG 字节流。"""
    return cairosvg.svg2png(
        url=str(SVG),
        output_width=size,
        output_height=size,
        background_color=BG,
    )

def main() -> int:
    check = "--check" in sys.argv

    if not SVG.exists():
        print(f"找不到 {SVG}", file=sys.stderr)
        return 1

    print(f"源文件 {SVG.relative_to(ROOT)}")
    print(f"尺寸   {SVG.stat().st_size} 字节")
    print()

    mismatch = 0
    for name, size in TARGETS.items():
        out = OUT_DIR / name
        data = render(size)
        if check:
            if out.exists() and out.read_bytes() == data:
                print(f"  {name:22s} 一致")
            else:
                print(f"  {name:22s} 不一致")
                mismatch += 1
        else:
            out.write_bytes(data)
            im = Image.open(out)
            px = im.convert("RGBA").load()
            # 统计非背景像素比例，确认图形确实画上去了
            total = size * size
            drawn = 0
            for y in range(size):
                for x in range(size):
                    r, g, b, _ = px[x, y]
                    if abs(r - 0xF2) > 24 or abs(g - 0xEF) > 24 or abs(b - 0xE9) > 24:
                        drawn += 1
            print(f"  {name:22s} {size:4d}x{size:<4d} {out.stat().st_size // 1024:5d} KB"
                  f"  墨迹 {drawn / total * 100:5.1f}%")

    if check:
        return 1 if mismatch else 0
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
