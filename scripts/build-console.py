#!/usr/bin/env python3
"""Build one native Dec Console installer and normalize its release filename."""

from __future__ import annotations

import json
import argparse
import hashlib
import os
import platform
import re
import shutil
import subprocess
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CLIENT = ROOT / "client"
TAURI = CLIENT / "src-tauri"
DIST = ROOT / "dist"
RUNTIME_RESOURCES = TAURI / "resources" / "runtime"
RUNTIME_COMPONENTS = ("dec", "dec-server", "dec-mcp", "dec-exec")


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


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def clean_runtime_resources() -> None:
    RUNTIME_RESOURCES.mkdir(parents=True, exist_ok=True)
    for child in RUNTIME_RESOURCES.iterdir():
        if child.name == ".gitkeep":
            continue
        if child.is_dir():
            shutil.rmtree(child)
        else:
            child.unlink()


def prepare_runtime_resources(version: str, os_id: str, arch: str) -> None:
    clean_runtime_resources()
    platform_dir = RUNTIME_RESOURCES / f"{os_id}-{arch}"
    platform_dir.mkdir(parents=True)
    env = os.environ.copy()
    env.update({"GOOS": os_id, "GOARCH": arch, "CGO_ENABLED": "0"})
    release = f"v{version}"
    build_time = datetime.now(timezone.utc).strftime("%Y-%m-%d_%H:%M:%S")
    files: dict[str, str] = {}
    for component in RUNTIME_COMPONENTS:
        suffix = ".exe" if os_id == "windows" else ""
        filename = f"{component}{suffix}"
        package = "." if component == "dec" else f"./cmd/{component}"
        output = platform_dir / filename
        subprocess.run(
            [
                "go",
                "build",
                "-ldflags",
                f"-X main.Version={release} -X main.BuildTime={build_time}",
                "-o",
                str(output),
                package,
            ],
            cwd=ROOT,
            env=env,
            check=True,
        )
        files[filename] = sha256(output)
    manifest = {
        "version": release,
        "os": os_id,
        "arch": arch,
        "files": files,
    }
    (platform_dir / "runtime-manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--skip-deps",
        action="store_true",
        help="reuse existing client/node_modules instead of running npm ci",
    )
    parser.add_argument(
        "--prepare-runtime-only",
        action="store_true",
        help="build native runtime resources for tauri dev and keep them in place",
    )
    args = parser.parse_args()
    version = release_version()
    sync_console_version(version)
    os_id, arch, pattern, extension = target()
    if args.prepare_runtime_only:
        prepare_runtime_resources(version, os_id, arch)
        print(RUNTIME_RESOURCES / f"{os_id}-{arch}")
        return
    npm = "npm.cmd" if platform.system().lower() == "windows" else "npm"
    try:
        prepare_runtime_resources(version, os_id, arch)
        if not args.skip_deps:
            subprocess.run([npm, "ci"], cwd=CLIENT, check=True)
        subprocess.run([npm, "run", "tauri", "build"], cwd=CLIENT, check=True)
    finally:
        clean_runtime_resources()
    matches = sorted((TAURI / "target" / "release" / "bundle").glob(pattern))
    if len(matches) != 1:
        raise SystemExit(f"expected one Console installer matching {pattern}, got {matches}")
    DIST.mkdir(parents=True, exist_ok=True)
    output = DIST / f"dec-console-{os_id}-{arch}.{extension}"
    shutil.copy2(matches[0], output)
    print(output)


if __name__ == "__main__":
    main()
