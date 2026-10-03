#!/usr/bin/env python3
"""
蓝盾制品 APP 跳转链接二维码生成脚本
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import struct
import sys
import urllib.request
import urllib.error
import urllib.parse
import zlib

import yaml

from auth import get_access_token


BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user"
MOBILE_APP_PACKAGE_SUFFIXES = (".apk", ".apks", ".ipa", ".hap")
ECC_CODEWORDS_PER_BLOCK = [
    -1, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28,
    30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30,
    30, 30, 30, 30, 30,
]
NUM_ERROR_CORRECTION_BLOCKS = [
    -1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9,
    9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25,
]
ALIGNMENT_PATTERN_POSITIONS = [
    [],
    [],
    [6, 18],
    [6, 22],
    [6, 26],
    [6, 30],
    [6, 34],
    [6, 22, 38],
    [6, 24, 42],
    [6, 26, 46],
    [6, 28, 50],
    [6, 30, 54],
    [6, 32, 58],
    [6, 34, 62],
    [6, 26, 46, 66],
    [6, 26, 48, 70],
    [6, 26, 50, 74],
    [6, 30, 54, 78],
    [6, 30, 56, 82],
    [6, 30, 58, 86],
    [6, 34, 62, 90],
    [6, 28, 50, 72, 94],
    [6, 26, 50, 74, 98],
    [6, 30, 54, 78, 102],
    [6, 28, 54, 80, 106],
    [6, 32, 58, 84, 110],
    [6, 30, 58, 86, 114],
    [6, 34, 62, 90, 118],
    [6, 26, 50, 74, 98, 122],
    [6, 30, 54, 78, 102, 126],
    [6, 26, 52, 78, 104, 130],
    [6, 30, 56, 82, 108, 134],
    [6, 34, 60, 86, 112, 138],
    [6, 30, 58, 86, 114, 142],
    [6, 34, 62, 90, 118, 146],
    [6, 30, 54, 78, 102, 126, 150],
    [6, 24, 50, 76, 102, 128, 154],
    [6, 28, 54, 80, 106, 132, 158],
    [6, 32, 58, 84, 110, 136, 162],
    [6, 26, 54, 82, 110, 138, 166],
    [6, 30, 58, 86, 114, 142, 170],
]


def get_auth_header(access_token: str | None = None) -> str:
    token = get_access_token(access_token)
    auth = {"access_token": token}
    username = os.environ.get("BK_CI_USERNAME")
    if username:
        auth["bk_username"] = username

    return json.dumps(auth, ensure_ascii=False)


def validate_mobile_app_package(path: str) -> None:
    normalized_path = urllib.parse.unquote(path).lower()
    if normalized_path.endswith(MOBILE_APP_PACKAGE_SUFFIXES):
        return

    allowed = "、".join(MOBILE_APP_PACKAGE_SUFFIXES)
    print(
        f"错误: APP 跳转二维码仅支持手机端 App 安装包制品（{allowed}），当前路径: {path}",
        file=sys.stderr,
    )
    sys.exit(1)


def get_app_download_url(
    project_id: str,
    artifactory_type: str,
    path: str,
    access_token: str | None = None,
) -> dict:
    params = urllib.parse.urlencode({
        "artifactoryType": artifactory_type,
        "path": path,
    })
    url = f"{BASE_URL}/projects/{project_id}/artifactories/app_download_url?{params}"

    req = urllib.request.Request(url)
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Bkapi-Authorization", get_auth_header(access_token))

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        body_text = e.read().decode("utf-8", errors="replace")
        print(f"HTTP 错误 {e.code}: {body_text}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"请求失败: {e.reason}", file=sys.stderr)
        sys.exit(1)

    if result.get("status") != 0:
        print(f"API 错误 [{result.get('status')}]: {result.get('message')}", file=sys.stderr)
        sys.exit(1)

    return result["data"]


def get_num_raw_data_modules(version: int) -> int:
    result = (16 * version + 128) * version + 64
    if version >= 2:
        num_align = version // 7 + 2
        result -= (25 * num_align - 10) * num_align - 55
        if version >= 7:
            result -= 36
    return result


def get_num_data_codewords(version: int) -> int:
    return (
        get_num_raw_data_modules(version) // 8
        - ECC_CODEWORDS_PER_BLOCK[version] * NUM_ERROR_CORRECTION_BLOCKS[version]
    )


def append_bits(bits: list[int], value: int, length: int) -> None:
    for i in reversed(range(length)):
        bits.append((value >> i) & 1)


def encode_text_to_codewords(text: str, version: int) -> list[int]:
    data = text.encode("utf-8")
    capacity_bits = get_num_data_codewords(version) * 8
    bits: list[int] = []

    append_bits(bits, 0b0100, 4)  # byte mode
    append_bits(bits, len(data), 8 if version <= 9 else 16)
    for byte in data:
        append_bits(bits, byte, 8)

    if len(bits) > capacity_bits:
        raise ValueError("文本内容过长，无法生成二维码")

    append_bits(bits, 0, min(4, capacity_bits - len(bits)))
    while len(bits) % 8 != 0:
        bits.append(0)

    pad_bytes = [0xEC, 0x11]
    pad_index = 0
    while len(bits) < capacity_bits:
        append_bits(bits, pad_bytes[pad_index % 2], 8)
        pad_index += 1

    return [
        sum(bits[i + j] << (7 - j) for j in range(8))
        for i in range(0, len(bits), 8)
    ]


def gf_multiply(x: int, y: int) -> int:
    result = 0
    while y:
        if y & 1:
            result ^= x
        x <<= 1
        if x & 0x100:
            x ^= 0x11D
        y >>= 1
    return result


def reed_solomon_divisor(degree: int) -> list[int]:
    result = [0] * (degree - 1) + [1]
    root = 1
    for _ in range(degree):
        for i in range(degree):
            result[i] = gf_multiply(result[i], root)
            if i + 1 < degree:
                result[i] ^= result[i + 1]
        root = gf_multiply(root, 0x02)
    return result


def reed_solomon_remainder(data: list[int], divisor: list[int]) -> list[int]:
    result = [0] * len(divisor)
    for byte in data:
        factor = byte ^ result.pop(0)
        result.append(0)
        for i, coef in enumerate(divisor):
            result[i] ^= gf_multiply(coef, factor)
    return result


def add_error_correction(data: list[int], version: int) -> list[int]:
    num_blocks = NUM_ERROR_CORRECTION_BLOCKS[version]
    ecc_len = ECC_CODEWORDS_PER_BLOCK[version]
    raw_codewords = get_num_raw_data_modules(version) // 8
    num_short_blocks = num_blocks - raw_codewords % num_blocks
    short_block_len = raw_codewords // num_blocks
    divisor = reed_solomon_divisor(ecc_len)
    blocks: list[list[int]] = []
    data_index = 0

    for block_index in range(num_blocks):
        data_len = short_block_len - ecc_len + (0 if block_index < num_short_blocks else 1)
        block_data = data[data_index:data_index + data_len]
        data_index += data_len
        block_ecc = reed_solomon_remainder(block_data, divisor)
        if block_index < num_short_blocks:
            block_data = block_data + [0]
        blocks.append(block_data + block_ecc)

    result = []
    for i in range(len(blocks[-1])):
        for j, block in enumerate(blocks):
            if i != short_block_len - ecc_len or j >= num_short_blocks:
                result.append(block[i])
    return result


def make_empty_matrix(version: int) -> tuple[list[list[bool]], list[list[bool]]]:
    size = version * 4 + 17
    return (
        [[False] * size for _ in range(size)],
        [[False] * size for _ in range(size)],
    )


def set_function_module(
    modules: list[list[bool]],
    is_function: list[list[bool]],
    x: int,
    y: int,
    value: bool,
) -> None:
    modules[y][x] = value
    is_function[y][x] = True


def draw_finder_pattern(
    modules: list[list[bool]],
    is_function: list[list[bool]],
    cx: int,
    cy: int,
) -> None:
    size = len(modules)
    for dy in range(-4, 5):
        for dx in range(-4, 5):
            x = cx + dx
            y = cy + dy
            if 0 <= x < size and 0 <= y < size:
                distance = max(abs(dx), abs(dy))
                set_function_module(modules, is_function, x, y, distance not in (2, 4))


def draw_alignment_pattern(
    modules: list[list[bool]],
    is_function: list[list[bool]],
    cx: int,
    cy: int,
) -> None:
    for dy in range(-2, 3):
        for dx in range(-2, 3):
            distance = max(abs(dx), abs(dy))
            set_function_module(modules, is_function, cx + dx, cy + dy, distance != 1)


def draw_function_patterns(
    modules: list[list[bool]],
    is_function: list[list[bool]],
    version: int,
) -> None:
    size = len(modules)
    draw_finder_pattern(modules, is_function, 3, 3)
    draw_finder_pattern(modules, is_function, size - 4, 3)
    draw_finder_pattern(modules, is_function, 3, size - 4)

    for i in range(size):
        if not is_function[6][i]:
            set_function_module(modules, is_function, i, 6, i % 2 == 0)
        if not is_function[i][6]:
            set_function_module(modules, is_function, 6, i, i % 2 == 0)

    positions = ALIGNMENT_PATTERN_POSITIONS[version]
    for y in positions:
        for x in positions:
            if (x == 6 and y == 6) or (x == 6 and y == size - 7) or (x == size - 7 and y == 6):
                continue
            draw_alignment_pattern(modules, is_function, x, y)

    for i in range(8):
        set_function_module(modules, is_function, 8, size - 1 - i, False)
        set_function_module(modules, is_function, size - 1 - i, 8, False)
    for i in range(9):
        if i != 6:
            set_function_module(modules, is_function, 8, i, False)
            set_function_module(modules, is_function, i, 8, False)
    set_function_module(modules, is_function, 8, size - 8, True)

    if version >= 7:
        for i in range(18):
            set_function_module(modules, is_function, size - 11 + i % 3, i // 3, False)
            set_function_module(modules, is_function, i // 3, size - 11 + i % 3, False)


def draw_codewords(
    modules: list[list[bool]],
    is_function: list[list[bool]],
    codewords: list[int],
) -> None:
    size = len(modules)
    bits = [(byte >> i) & 1 for byte in codewords for i in reversed(range(8))]
    bit_index = 0
    upward = True
    right = size - 1

    while right >= 1:
        if right == 6:
            right -= 1
        for vert in range(size):
            y = size - 1 - vert if upward else vert
            for x in (right, right - 1):
                if not is_function[y][x]:
                    modules[y][x] = bit_index < len(bits) and bits[bit_index] == 1
                    bit_index += 1
        upward = not upward
        right -= 2


def mask_bit(mask: int, x: int, y: int) -> bool:
    if mask == 0:
        return (x + y) % 2 == 0
    if mask == 1:
        return y % 2 == 0
    if mask == 2:
        return x % 3 == 0
    if mask == 3:
        return (x + y) % 3 == 0
    if mask == 4:
        return (x // 3 + y // 2) % 2 == 0
    if mask == 5:
        return (x * y) % 2 + (x * y) % 3 == 0
    if mask == 6:
        return ((x * y) % 2 + (x * y) % 3) % 2 == 0
    return ((x + y) % 2 + (x * y) % 3) % 2 == 0


def apply_mask(
    modules: list[list[bool]],
    is_function: list[list[bool]],
    mask: int,
) -> None:
    for y, row in enumerate(modules):
        for x, _ in enumerate(row):
            if not is_function[y][x] and mask_bit(mask, x, y):
                modules[y][x] = not modules[y][x]


def draw_format_bits(modules: list[list[bool]], mask: int) -> None:
    size = len(modules)
    data = (1 << 3) | mask  # error correction level L
    rem = data
    for _ in range(10):
        rem = (rem << 1) ^ ((rem >> 9) * 0x537)
    bits = ((data << 10) | rem) ^ 0x5412

    def bit(i: int) -> bool:
        return ((bits >> i) & 1) != 0

    for i in range(6):
        modules[i][8] = bit(i)
    modules[7][8] = bit(6)
    modules[8][8] = bit(7)
    modules[8][7] = bit(8)
    for i in range(9, 15):
        modules[8][14 - i] = bit(i)

    for i in range(8):
        modules[8][size - 1 - i] = bit(i)
    for i in range(8, 15):
        modules[size - 15 + i][8] = bit(i)
    modules[size - 8][8] = True


def draw_version_bits(modules: list[list[bool]], version: int) -> None:
    if version < 7:
        return
    size = len(modules)
    rem = version
    for _ in range(12):
        rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
    bits = (version << 12) | rem
    for i in range(18):
        bit = ((bits >> i) & 1) != 0
        modules[i // 3][size - 11 + i % 3] = bit
        modules[size - 11 + i % 3][i // 3] = bit


def get_penalty_score(modules: list[list[bool]]) -> int:
    size = len(modules)
    penalty = 0

    for row in modules:
        run_color = row[0]
        run_len = 1
        for color in row[1:]:
            if color == run_color:
                run_len += 1
                if run_len == 5:
                    penalty += 3
                elif run_len > 5:
                    penalty += 1
            else:
                run_color = color
                run_len = 1

    for x in range(size):
        run_color = modules[0][x]
        run_len = 1
        for y in range(1, size):
            color = modules[y][x]
            if color == run_color:
                run_len += 1
                if run_len == 5:
                    penalty += 3
                elif run_len > 5:
                    penalty += 1
            else:
                run_color = color
                run_len = 1

    for y in range(size - 1):
        for x in range(size - 1):
            color = modules[y][x]
            if modules[y][x + 1] == color and modules[y + 1][x] == color and modules[y + 1][x + 1] == color:
                penalty += 3

    pattern1 = [True, False, True, True, True, False, True, False, False, False, False]
    pattern2 = [False, False, False, False, True, False, True, True, True, False, True]
    for row in modules:
        for i in range(size - 10):
            if row[i:i + 11] == pattern1 or row[i:i + 11] == pattern2:
                penalty += 40
    for x in range(size):
        column = [modules[y][x] for y in range(size)]
        for i in range(size - 10):
            if column[i:i + 11] == pattern1 or column[i:i + 11] == pattern2:
                penalty += 40

    black = sum(1 for row in modules for cell in row if cell)
    total = size * size
    penalty += abs(black * 20 - total * 10) // total * 10
    return penalty


def make_qr_matrix(text: str) -> list[list[bool]]:
    data = text.encode("utf-8")
    version = None
    for candidate in range(1, 41):
        count_bits = 8 if candidate <= 9 else 16
        if 4 + count_bits + len(data) * 8 <= get_num_data_codewords(candidate) * 8:
            version = candidate
            break
    if version is None:
        raise ValueError("文本内容过长，无法生成二维码")

    data_codewords = encode_text_to_codewords(text, version)
    codewords = add_error_correction(data_codewords, version)
    base_modules, is_function = make_empty_matrix(version)
    draw_function_patterns(base_modules, is_function, version)
    draw_codewords(base_modules, is_function, codewords)

    best_modules = None
    best_score = None
    for mask in range(8):
        modules = [row[:] for row in base_modules]
        apply_mask(modules, is_function, mask)
        draw_format_bits(modules, mask)
        draw_version_bits(modules, version)
        score = get_penalty_score(modules)
        if best_score is None or score < best_score:
            best_modules = modules
            best_score = score

    return best_modules or base_modules


def png_chunk(chunk_type: bytes, data: bytes) -> bytes:
    return (
        struct.pack(">I", len(data))
        + chunk_type
        + data
        + struct.pack(">I", zlib.crc32(chunk_type + data) & 0xFFFFFFFF)
    )


def write_png_qrcode(text: str, output: Path, border: int = 4, scale: int = 8) -> None:
    modules = make_qr_matrix(text)
    size = len(modules)
    module_size = size + border * 2
    image_size = module_size * scale
    rows = []

    for pixel_y in range(image_size):
        module_y = pixel_y // scale - border
        row = bytearray([0])  # PNG filter type 0
        for pixel_x in range(image_size):
            module_x = pixel_x // scale - border
            is_black = (
                0 <= module_y < size
                and 0 <= module_x < size
                and modules[module_y][module_x]
            )
            row.append(0 if is_black else 255)
        rows.append(bytes(row))

    ihdr = struct.pack(">IIBBBBB", image_size, image_size, 8, 0, 0, 0, 0)
    png_data = b"".join([
        b"\x89PNG\r\n\x1a\n",
        png_chunk(b"IHDR", ihdr),
        png_chunk(b"IDAT", zlib.compress(b"".join(rows))),
        png_chunk(b"IEND", b""),
    ])

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_bytes(png_data)


def with_suffix_number(output: Path, number: int) -> Path:
    return output.with_name(f"{output.stem}_{number}{output.suffix}")


def default_qrcode_output_path(artifact_path: str) -> Path:
    artifact_name = urllib.parse.unquote(artifact_path.rstrip("/").split("/")[-1])
    normalized_name = artifact_name.lower()
    name = artifact_name
    ext_type = "app"
    for suffix in sorted(MOBILE_APP_PACKAGE_SUFFIXES, key=len, reverse=True):
        if normalized_name.endswith(suffix):
            name = artifact_name[:-len(suffix)]
            ext_type = suffix.lstrip(".")
            break

    safe_name = "".join("_" if char in '<>:"/\\|?*' else char for char in name).strip()
    safe_ext_type = "".join("_" if char in '<>:"/\\|?*' else char for char in ext_type).strip()
    if not safe_name:
        safe_name = "app"
    if not safe_ext_type:
        safe_ext_type = "app"
    return Path(f"{safe_name}_{safe_ext_type}.png")


def main():
    parser = argparse.ArgumentParser(
        description="生成蓝盾制品的 APP 跳转链接二维码 PNG 文件",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
artifactoryType 取值:
  PIPELINE    流水线仓库
  CUSTOM_DIR  自定义仓库

示例:
  python get_app_download_url.py \\
    --project-id myproject \\
    --artifactory-type PIPELINE \\
    --path /app-1.0.0.apk
        """,
    )
    parser.add_argument("--project-id", required=True, help="项目 ID")
    parser.add_argument(
        "--artifactory-type",
        required=True,
        choices=["PIPELINE", "CUSTOM_DIR"],
        help="制品仓库类型：PIPELINE 或 CUSTOM_DIR",
    )
    parser.add_argument("--path", required=True, help="文件完整路径（来自搜索结果的 fullPath）")
    parser.add_argument("--access-token", help="可选；本轮对话提供的访问令牌")
    parser.add_argument(
        "--output",
        help="二维码 PNG 输出路径（默认 {name}_{ext_type}.png，如 app-1.0.0_apk.png）",
    )

    args = parser.parse_args()

    validate_mobile_app_package(args.path)

    data = get_app_download_url(
        project_id=args.project_id,
        artifactory_type=args.artifactory_type,
        path=args.path,
        access_token=args.access_token,
    )

    urls = [data.get("url")]
    if data.get("url2"):
        urls.append(data["url2"])
    urls = [url for url in urls if url]
    if not urls:
        print("API 未返回可生成二维码的 APP 跳转链接", file=sys.stderr)
        sys.exit(1)

    output = Path(args.output) if args.output else default_qrcode_output_path(args.path)
    qrcodes = []
    for index, url in enumerate(urls, 1):
        output_path = output if index == 1 else with_suffix_number(output, index)
        write_png_qrcode(url, output_path)
        qrcodes.append({"file": str(output_path)})

    print(yaml.dump({"qrcodes": qrcodes}, allow_unicode=True, sort_keys=False, default_flow_style=False))


if __name__ == "__main__":
    main()
