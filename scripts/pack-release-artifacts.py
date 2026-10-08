#!/usr/bin/env python3
"""dec 的 release.packScript：把 dist/ 编组成 relkit.release-artifacts/2。

由 `relkit ci release` 调用（dec 的 GitHub Actions publish job）。
编组规则与旧 bash 尾段一一对应：

- dist/dec-*（非 console、非 manifest）按文件名后缀解析 os/arch，
  每个文件一组：install kind=binary + payload 目录（audience=runtime）。
- dist/dec-runtime-manifest.json 一组：install kind=blob（audience=runtime）。
- dist/dec-console-* 每个文件一组：install kind=installer（audience=user）。

结构化产物清单替代旧 bash 段里的字符串解析编组；stage 的
--payload filename= 逐组显式命名（旧段曾因缺省 filename 出现
多 payload 互相覆盖，见 onboarding ops.retrospect 备注）。
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

SCHEMA = "relkit.release-artifacts/2"
DEFAULT_MANIFEST = "dist/release-artifacts.json"

# 文件名后缀 → (os, arch)；与旧 bash 段的 case 分支一一对应。
# dmg 是 macOS 安装包扩展名（dec-console-darwin-*.dmg），历史上漏登记
# 导致两个 dmg 从未进入发布清单（Dec #22），此处显式补录。
TARGET_SUFFIXES = [
    ("-windows-amd64.exe", "windows", "amd64"),
    ("-linux-amd64", "linux", "amd64"),
    ("-linux-arm64", "linux", "arm64"),
    ("-darwin-amd64", "darwin", "amd64"),
    ("-darwin-arm64", "darwin", "arm64"),
    ("-darwin-amd64.dmg", "darwin", "amd64"),
    ("-darwin-arm64.dmg", "darwin", "arm64"),
]


def resolve_target(filename: str) -> tuple[str, str] | None:
    for suffix, os_name, arch in TARGET_SUFFIXES:
        if filename.endswith(suffix):
            return os_name, arch
    return None


def component_of(filename: str, os_name: str, arch: str) -> str:
    # dec-<component>-<os>-<arch>[.exe] → dec-<component>
    return filename[: -(len(os_name) + len(arch) + 2)]


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    dist = root / "dist"
    version = json.loads((root / "version.json").read_text(encoding="utf-8"))["version"]
    version = version.lstrip("v")

    groups: list[dict] = []
    payload_dir = dist / "payload"

    # 1. 运行时组件：install + payload 成组（audience=runtime）
    for path in sorted(dist.glob("dec-*")):
        if not path.is_file():
            continue
        name = path.name
        if name.startswith("dec-console-") or name == "dec-runtime-manifest.json":
            continue
        target = resolve_target(name)
        if target is None:
            raise SystemExit(
                "pack-release-artifacts: 无法解析产物目标平台: dist/%s\n"
                "  命名约定为 dec-*-<os>-<arch>[.exe|.dmg]，漏登记的扩展名会静默丢产物"
                "（Dec #22 教训），请扩充 TARGET_SUFFIXES。" % name
            )
        os_name, arch = target
        component = component_of(name, os_name, arch)
        install_name = f"{component}.exe" if os_name == "windows" else component
        group_payload = payload_dir / f"{component}-{os_name}-{arch}"
        group_payload.mkdir(parents=True, exist_ok=True)
        (group_payload / install_name).write_bytes(path.read_bytes())
        selectors = {
            "os": os_name,
            "arch": arch,
            "component": component,
            "audience": "runtime",
        }
        groups.append(
            {
                "selectors": selectors,
                "install": {
                    "path": f"dist/{name}",
                    "kind": "binary",
                    "filename": name,
                },
                "payloads": [
                    {
                        "path": f"dist/payload/{component}-{os_name}-{arch}",
                        "filename": f"dec-{version}-{component}-{os_name}-{arch}-payload.zip",
                        "selectors": selectors,
                    }
                ],
            }
        )

    # 2. 运行时清单 blob：install-only（audience=runtime）
    manifest_blob = dist / "dec-runtime-manifest.json"
    if manifest_blob.is_file():
        groups.append(
            {
                "selectors": {
                    "component": "manifest",
                    "audience": "runtime",
                },
                "install": {
                    "path": "dist/dec-runtime-manifest.json",
                    "kind": "blob",
                    "filename": "dec-runtime-manifest.json",
                },
                "payloads": [],
            }
        )

    # 3. Console 人面安装包：install-only（audience=user）
    for path in sorted(dist.glob("dec-console-*")):
        if not path.is_file():
            continue
        target = resolve_target(path.name)
        if target is None:
            raise SystemExit(
                "pack-release-artifacts: 无法解析 Console 安装包目标平台: dist/%s\n"
                "  命名约定为 dec-console-<os>-<arch>[.exe|.dmg]，漏登记的扩展名会静默丢产物"
                "（Dec #22 教训），请扩充 TARGET_SUFFIXES。" % path.name
            )
        os_name, arch = target
        groups.append(
            {
                "selectors": {
                    "os": os_name,
                    "arch": arch,
                    "component": "console",
                    "audience": "user",
                },
                "install": {
                    "path": f"dist/{path.name}",
                    "kind": "installer",
                },
                "payloads": [],
            }
        )

    if not groups:
        print("pack-release-artifacts: dist/ has no dec-* artifacts", file=sys.stderr)
        return 1

    doc = {
        "schema": SCHEMA,
        "version": version,
        "selectorGroups": groups,
        "archives": [],
    }
    out = root / DEFAULT_MANIFEST
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(doc, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"pack-release-artifacts: wrote {out.relative_to(root).as_posix()} groups={len(groups)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
