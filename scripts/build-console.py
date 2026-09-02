#!/usr/bin/env python3
"""Build one native Dec Console installer and normalize its release filename."""

from __future__ import annotations

import json
import argparse
import platform
import re
import shutil
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CLIENT = ROOT / "client"
TAURI = CLIENT / "src-tauri"
DIST = ROOT / "dist"


def release_version() -> str:
    value = json.loads((ROOT / "version.json").read_text(encoding="utf-8"))["version"]
    if not re.fullmatch(r"v\d+\.\d+\.\d+", value):
        raise SystemExit(f"invalid release version: {value}")
    return value[1:]


def sync_console_version(version: str) -> None:
    cargo = TAURI / "Cargo.toml"
    text = cargo.read_text(encoding="utf-8")
    text, count = re.subn(
        r'(?m)^(version = ")[^"]+(")$',
        rf"\g<1>{version}\2",
        text,
        count=1,
    )
    if count != 1:
        raise SystemExit("cannot update Cargo.toml package version")
    cargo.write_text(text, encoding="utf-8")

    conf_path = TAURI / "tauri.conf.json"
    conf = json.loads(conf_path.read_text(encoding="utf-8"))
    conf["version"] = version
    conf_path.write_text(
        json.dumps(conf, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def target() -> tuple[str, str, str, str]:
    os_name = platform.system().lower()
    machine = platform.machine().lower()
    os_map = {"windows": "windows", "darwin": "darwin", "linux": "linux"}
    arch_map = {
        "amd64": "amd64",
        "x86_64": "amd64",
        "arm64": "arm64",
        "aarch64": "arm64",
    }
    if os_name not in os_map or machine not in arch_map:
        raise SystemExit(f"unsupported native build target: {os_name}/{machine}")
    os_id, arch = os_map[os_name], arch_map[machine]
    if os_id == "windows":
        return os_id, arch, "nsis/*-setup.exe", "exe"
    if os_id == "darwin":
        return os_id, arch, "dmg/*.dmg", "dmg"
    return os_id, arch, "appimage/*.AppImage", "AppImage"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--skip-deps",
        action="store_true",
        help="reuse existing client/node_modules instead of running npm ci",
    )
    args = parser.parse_args()
    version = release_version()
    sync_console_version(version)
    npm = "npm.cmd" if platform.system().lower() == "windows" else "npm"
    if not args.skip_deps:
        subprocess.run([npm, "ci"], cwd=CLIENT, check=True)
    subprocess.run([npm, "run", "tauri", "build"], cwd=CLIENT, check=True)

    os_id, arch, pattern, extension = target()
    matches = sorted((TAURI / "target" / "release" / "bundle").glob(pattern))
    if len(matches) != 1:
        raise SystemExit(f"expected one Console installer matching {pattern}, got {matches}")
    DIST.mkdir(parents=True, exist_ok=True)
    output = DIST / f"dec-console-{os_id}-{arch}.{extension}"
    shutil.copy2(matches[0], output)
    print(output)


if __name__ == "__main__":
    main()
