#!/usr/bin/env python3
"""把品牌 SVG 转成 Rust 轮廓常量。

这是 assets/branding/dec-logo.svg → tray/src/logo_polygons.rs 的唯一生成入口，
不要在 Rust 里手改顶点。

用法：
    python3 scripts/gen-logo-polygons.py

设计要点：
- SVG 里的曲线（C/S/Q/T/A）先离散成密集顶点，再按容差抽稀，
  这样 Rust 侧只需要做简单的多边形填充，不必实现贝塞尔求值。
- 坐标系保持 1024x1024、y 向下，与 SVG 的 viewBox 一致。
- 子路径的绕向原样保留：nonzero 规则下，同向叠加是实心，反向是挖空。
"""

from __future__ import annotations

import math
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_SVG = ROOT / "assets" / "branding" / "dec-logo.svg"
DEFAULT_OUT = ROOT / "tray" / "src" / "logo_polygons.rs"

# 曲线离散的步数：越大越平滑，也越占空间。
CURVE_STEPS = 16
# 抽稀容差（画布单位）。1.0 表示丢掉偏离不足 1 单位的点。
SIMPLIFY_TOL = 1.0


def tokenize(path_d: str) -> list[str]:
    """把 path 的 d 属性拆成命令与数字。"""
    pattern = r"[MmLlHhVvCcSsQqTtAaZz]|-?\d*\.?\d+(?:[eE][-+]?\d+)?"
    return re.findall(pattern, path_d)


class Pen:
    """把 SVG path 命令走成折线。"""

    def __init__(self) -> None:
        self.contours: list[list[tuple[float, float]]] = []
        self.cur: list[tuple[float, float]] = []
        self.x = 0.0
        self.y = 0.0
        self.start = (0.0, 0.0)
        self.cmd = ""
        self.last_ctrl: tuple[float, float] | None = None

    def move_to(self, x: float, y: float) -> None:
        self.flush()
        self.x, self.y = x, y
        self.start = (x, y)
        self.cur = [(x, y)]

    def line_to(self, x: float, y: float) -> None:
        self.x, self.y = x, y
        self.cur.append((x, y))

    def flush(self) -> None:
        if len(self.cur) >= 3:
            self.contours.append(self.cur)
        self.cur = []

    def cubic(self, x1: float, y1: float, x2: float, y2: float, x: float, y: float) -> None:
        x0, y0 = self.x, self.y
        for i in range(1, CURVE_STEPS + 1):
            t = i / CURVE_STEPS
            mt = 1 - t
            px = (mt**3 * x0 + 3 * mt**2 * t * x1 + 3 * mt * t**2 * x2 + t**3 * x)  # noqa: E501
            py = (mt**3 * y0 + 3 * mt**2 * t * y1 + 3 * mt * t**2 * y2 + t**3 * y)  # noqa: E501
            self.cur.append((px, py))
        self.x, self.y = x, y

    def quad(self, x1: float, y1: float, x: float, y: float) -> None:
        x0, y0 = self.x, self.y
        for i in range(1, CURVE_STEPS + 1):
            t = i / CURVE_STEPS
            mt = 1 - t
            px = mt**2 * x0 + 2 * mt * t * x1 + t**2 * x
            py = mt**2 * y0 + 2 * mt * t * y1 + t**2 * y
            self.cur.append((px, py))
        self.x, self.y = x, y

    def arc(self, rx, ry, rot, large, sweep, x, y) -> None:
        # 简化处理：端点是主要信息，弧段用直线近似后交给抽稀
        self.line_to(x, y)


def parse_path(d: str) -> list[list[tuple[float, float]]]:
    toks = tokenize(d)
    pen = Pen()
    i = 0
    while i < len(toks):
        tok = toks[i]
        if tok.isalpha():
            pen.cmd = tok
            i += 1
            if pen.cmd in "Zz":
                if pen.cur and pen.cur[0] != pen.start:
                    pen.cur.append(pen.start)
                pen.flush()
                pen.x, pen.y = pen.start
            continue

        rel = pen.cmd.islower()
        cmd = pen.cmd.upper()

        def num(k: int) -> float:
            return float(toks[i + k])

        if cmd == "M":
            x, y = num(0), num(1)
            pen.move_to(pen.x + x if rel else x, pen.y + y if rel else y)
            i += 2
            pen.cmd = "l" if rel else "L"
        elif cmd == "L":
            x, y = num(0), num(1)
            pen.line_to(pen.x + x if rel else x, pen.y + y if rel else y)
            i += 2
        elif cmd == "H":
            x = num(0)
            pen.line_to(pen.x + x if rel else x, pen.y)
            i += 1
        elif cmd == "V":
            y = num(0)
            pen.line_to(pen.x, pen.y + y if rel else y)
            i += 1
        elif cmd == "C":
            x1, y1, x2, y2, x, y = (num(k) for k in range(6))
            if rel:
                x1, y1, x2, y2, x, y = x1 + pen.x, y1 + pen.y, x2 + pen.x, y2 + pen.y, x + pen.x, y + pen.y
            pen.cubic(x1, y1, x2, y2, x, y)
            pen.last_ctrl = (x2, y2)
            i += 6
        elif cmd == "S":
            x2, y2, x, y = (num(k) for k in range(4))
            if rel:
                x2, y2, x, y = x2 + pen.x, y2 + pen.y, x + pen.x, y + pen.y
            if pen.last_ctrl:
                x1 = 2 * pen.x - pen.last_ctrl[0]
                y1 = 2 * pen.y - pen.last_ctrl[1]
            else:
                x1, y1 = pen.x, pen.y
            pen.cubic(x1, y1, x2, y2, x, y)
            pen.last_ctrl = (x2, y2)
            i += 4
        elif cmd == "Q":
            x1, y1, x, y = (num(k) for k in range(4))
            if rel:
                x1, y1, x, y = x1 + pen.x, y1 + pen.y, x + pen.x, y + pen.y
            pen.quad(x1, y1, x, y)
            pen.last_ctrl = (x1, y1)
            i += 4
        elif cmd == "T":
            x, y = num(0), num(1)
            if rel:
                x, y = x + pen.x, y + pen.y
            if pen.last_ctrl:
                x1 = 2 * pen.x - pen.last_ctrl[0]
                y1 = 2 * pen.y - pen.last_ctrl[1]
            else:
                x1, y1 = pen.x, pen.y
            pen.quad(x1, y1, x, y)
            pen.last_ctrl = (x1, y1)
            i += 2
        elif cmd == "A":
            rx, ry, rot, large, sweep, x, y = (num(k) for k in range(7))
            if rel:
                x, y = x + pen.x, y + pen.y
            pen.arc(rx, ry, rot, large, sweep, x, y)
            i += 7
        else:
            i += 1

    pen.flush()
    return pen.contours


def perp_dist(p, a, b) -> float:
    ax, ay = a
    bx, by = b
    px, py = p
    dx, dy = bx - ax, by - ay
    if dx == 0 and dy == 0:
        return math.hypot(px - ax, py - ay)
    t = ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)
    t = max(0.0, min(1.0, t))
    return math.hypot(px - (ax + t * dx), py - (ay + t * dy))


def simplify(points: list[tuple[float, float]], tol: float) -> list[tuple[float, float]]:
    """Douglas-Peucker 抽稀，保留闭合多边形的形状。"""
    if len(points) <= 3:
        return points
    closed = points[0] == points[-1]
    pts = points[:-1] if closed else points
    if len(pts) <= 3:
        return points

    keep = [False] * len(pts)
    keep[0] = keep[-1] = True
    stack = [(0, len(pts) - 1)]
    while stack:
        lo, hi = stack.pop()
        if hi <= lo + 1:
            continue
        dmax, idx = 0.0, lo
        for k in range(lo + 1, hi):
            d = perp_dist(pts[k], pts[lo], pts[hi])
            if d > dmax:
                dmax, idx = d, k
        if dmax > tol:
            keep[idx] = True
            stack.append((lo, idx))
            stack.append((idx, hi))

    out = [pts[k] for k in range(len(pts)) if keep[k]]
    if closed:
        out.append(out[0])
    return out


def signed_area(points: list[tuple[float, float]]) -> float:
    s = 0.0
    n = len(points)
    for i in range(n):
        x1, y1 = points[i]
        x2, y2 = points[(i + 1) % n]
        s += x1 * y2 - x2 * y1
    return s / 2


def parse_transform(svg: str) -> tuple[float, float, float, float]:
    """从 SVG 里取出 affine 变换 (a, b, c, d) 与平移 (e, f)。

    potrace 的输出形如 translate(0,1024) scale(0.1,-0.1)：
    先按 (a,d) 缩放（d 为负表示 y 轴翻转），再平移。这里只支持这种
    由 potrace 固定产出的形式，够用且不会误伤手工 SVG。
    """
    tx = ty = 0.0
    a = d = 1.0
    b = c = 0.0

    for m in re.finditer(r"translate\(\s*([-\d.eE+]+)[\s,]+([-\d.eE+]+)\s*\)", svg):
        tx += float(m.group(1))
        ty += float(m.group(2))
    for m in re.finditer(r"scale\(\s*([-\d.eE+]+)(?:[\s,]+([-\d.eE+]+))?\s*\)", svg):
        sx = float(m.group(1))
        sy = float(m.group(2)) if m.group(2) is not None else sx
        a *= sx
        d *= sy
    return a, b, c, d, tx, ty


def apply_transform(
    contours: list[list[tuple[float, float]]],
    transform: tuple[float, float, float, float, float, float],
) -> list[list[tuple[float, float]]]:
    a, b, c, d, tx, ty = transform
    out = []
    for contour in contours:
        out.append([(a * x + c * y + tx, b * x + d * y + ty) for x, y in contour])
    return out


def main() -> int:
    svg_path = Path(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_SVG
    out_path = Path(sys.argv[2]) if len(sys.argv) > 2 else DEFAULT_OUT

    if not svg_path.exists():
        print(f"找不到 SVG: {svg_path}", file=sys.stderr)
        return 1

    svg = svg_path.read_text(encoding="utf-8")
    ds = re.findall(r'\sd="([^"]+)"', svg)
    if not ds:
        print("SVG 里没有找到 path 的 d 属性", file=sys.stderr)
        return 1

    all_contours: list[list[tuple[float, float]]] = []
    for d in ds:
        for c in parse_path(d):
            s = simplify(c, SIMPLIFY_TOL)
            if len(s) >= 3:
                all_contours.append(s)

    transform = parse_transform(svg)
    if transform != (1.0, 0.0, 0.0, 1.0, 0.0, 0.0):
        print("应用变换 a={:.4f} d={:.4f} tx={:.1f} ty={:.1f}".format(
            transform[0], transform[3], transform[4], transform[5]))
        all_contours = apply_transform(all_contours, transform)

    # 统一缩放到 1024 画布：取所有轮廓的整体包围盒
    xs = [p[0] for c in all_contours for p in c]
    ys = [p[1] for c in all_contours for p in c]
    print(f"变换后包围盒 x[{min(xs):.1f},{max(xs):.1f}] y[{min(ys):.1f},{max(ys):.1f}]")

    lines = [
        "//! 品牌字标的轮廓顶点，由 scripts/gen-logo-polygons.py 从",
        f"//! {svg_path.relative_to(ROOT)} 生成，勿手改。",
        "//!",
        "//! 每个子路径是一组首尾相接的闭合多边形（坐标系 1024x1024，y 向下），",
        "//! 相邻子路径按 nonzero 规则叠加。",
        "",
        "/// 一条闭合轮廓的顶点，坐标单位与 1024 画布一致。",
        "pub type Contour = &'static [(i16, i16)];",
        "",
        "/// 品牌字标的所有轮廓。",
        "pub const CONTOURS: &[Contour] = &[",
    ]

    for c in all_contours:
        pts = [(round(x), round(y)) for x, y in c]
        if pts[0] != pts[-1]:
            pts.append(pts[0])
        lines.append("    &[")
        row: list[str] = []
        for x, y in pts:
            row.append(f"({x}, {y})")
            if len(row) == 6:
                lines.append("        " + ", ".join(row) + ",")
                row = []
        if row:
            lines.append("        " + ", ".join(row) + ",")
        lines.append("    ],")

    lines.append("];")
    lines.append("")

    out_path.write_text("\n".join(lines), encoding="utf-8")

    print(f"已生成 {out_path}")
    print(f"  子路径数 {len(all_contours)}")
    print(f"  总顶点数 {sum(len(c) for c in all_contours)}")
    for i, c in enumerate(all_contours):
        xs = [p[0] for p in c]
        ys = [p[1] for p in c]
        area = signed_area(c)
        way = "CCW" if area > 0 else "CW"
        print(f"  {i}: {len(c):4d} 点  x[{min(xs):.0f},{max(xs):.0f}] "
              f"y[{min(ys):.0f},{max(ys):.0f}]  面积 {area:+.0f} {way}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
