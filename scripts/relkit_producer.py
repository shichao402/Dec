#!/usr/bin/env python3
"""Canonical Relkit ArtifactSet producer for Dec."""

from __future__ import annotations

import base64
import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path
from typing import NoReturn


ROOT = Path(__file__).resolve().parents[1]
RUNTIME_COMPONENTS = ("dec-server", "dec-exec", "dec-host-setup")
TARGETS = {
    "linux-amd64": ("linux", "amd64"),
    "linux-arm64": ("linux", "arm64"),
    "windows-amd64": ("windows", "amd64"),
    "darwin-amd64": ("darwin", "amd64"),
    "darwin-arm64": ("darwin", "arm64"),
}


def fail(message: str) -> NoReturn:
    print(f"dec Relkit producer: {message}", file=sys.stderr)
    raise SystemExit(1)


def run(command: list[str], *, env: dict[str, str] | None = None) -> None:
    result = subprocess.run(
        command,
        cwd=ROOT,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    if result.stdout:
        print(result.stdout, file=sys.stderr, end="")
    if result.returncode != 0:
        fail(f"{command[0]} failed with status {result.returncode}")


def digest(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def artifact(
    artifact_id: str,
    path: Path,
    kind: str,
    target: str,
    selectors: dict[str, str],
    relations: dict[str, str] | None = None,
) -> dict[str, object]:
    value: dict[str, object] = {
        "id": artifact_id,
        "path": path.name,
        "kind": kind,
        "platform": target,
        "selectors": selectors,
        "sha256": digest(path),
        "size": path.stat().st_size,
    }
    if relations:
        value["relations"] = relations
    return value


def require_bootstrap(input_document: dict[str, object]) -> dict[str, object]:
    product = input_document["product"]
    values = input_document.get("runtimeBootstraps")
    value = values.get(product) if isinstance(values, dict) else None
    if not isinstance(value, dict):
        fail(f"missing verified runtime bootstrap for {product}")
    profile = value.get("profile")
    keys = value.get("trustedKeys")
    if (
        value.get("schema") != "relkit.runtime-bootstrap/1"
        or value.get("resolutionDigest") != input_document["resolutionDigest"]
        or not isinstance(profile, dict)
        or profile.get("product") != product
        or not isinstance(keys, list)
        or not keys
    ):
        fail(f"invalid verified runtime bootstrap for {product}")
    return value


def native_target() -> str:
    os_name = {"darwin": "darwin", "windows": "windows", "linux": "linux"}.get(
        platform.system().lower()
    )
    arch = {
        "x86_64": "amd64",
        "amd64": "amd64",
        "arm64": "arm64",
        "aarch64": "arm64",
    }.get(platform.machine().lower())
    return f"{os_name}-{arch}" if os_name and arch else "unsupported"


def release_code(version: str) -> int:
    parts = version.removeprefix("v").split(".")
    if len(parts) != 3 or not all(part.isdigit() for part in parts):
        fail(f"version {version} must be vMAJOR.MINOR.PATCH")
    value = int(parts[2])
    if value <= 0:
        fail(f"version {version} has no positive release code")
    return value


def main() -> None:
    try:
        input_document = json.load(sys.stdin)
    except (json.JSONDecodeError, UnicodeDecodeError) as error:
        fail(f"invalid producer input: {error}")
    if input_document.get("schema") != "relkit.artifact-producer-input/1":
        fail(f"unexpected schema {input_document.get('schema')}")
    if input_document.get("product") != "dec":
        fail(f"unexpected product {input_document.get('product')}")
    target = str(input_document.get("target", ""))
    if target not in TARGETS:
        fail(f"unsupported target {target}")
    version = str(input_document.get("version", ""))
    declared_version = json.loads((ROOT / "version.json").read_text(encoding="utf-8"))[
        "version"
    ]
    if version != declared_version:
        fail(f"version.json declares {declared_version}, requested {version}")

    bootstrap = require_bootstrap(input_document)
    output = Path(str(input_document["outputDirectory"]))
    output.mkdir(parents=True, exist_ok=True)
    runtime_input = output / ".runtime-input"
    runtime_input.mkdir()
    bootstrap_path = runtime_input / "dec.json"
    bootstrap_bytes = (
        json.dumps(bootstrap, ensure_ascii=False, separators=(",", ":")) + "\n"
    ).encode()
    bootstrap_path.write_bytes(bootstrap_bytes)
    bootstrap_base64 = base64.b64encode(bootstrap_bytes).decode("ascii")

    goos, goarch = TARGETS[target]
    child_env = os.environ.copy()
    child_env.update(
        {
            "GOOS": goos,
            "GOARCH": goarch,
            "CGO_ENABLED": "0",
            "DEC_RUNTIME_BOOTSTRAP": str(bootstrap_path),
            "DEC_RUNTIME_BOOTSTRAP_B64": bootstrap_base64,
            "RELKIT_OUTPUT_DIR": str(output),
        }
    )
    link_flags = (
        f"-X main.Version={version} "
        "-X github.com/shichao402/Dec/internal/update.runtimeBootstrapBase64="
        f"{bootstrap_base64}"
    )
    artifacts: list[dict[str, object]] = []
    core = os.environ.get("RELKIT_CORE", "").strip()
    if not core:
        fail("RELKIT_CORE was not supplied by the canonical wrapper")

    try:
        for component in RUNTIME_COMPONENTS:
            suffix = ".exe" if goos == "windows" else ""
            binary = output / f"{component}-{goos}-{goarch}{suffix}"
            run(
                [
                    "go",
                    "build",
                    "-trimpath",
                    "-ldflags",
                    link_flags,
                    "-o",
                    str(binary),
                    f"./cmd/{component}",
                ],
                env=child_env,
            )
            selectors = {
                "os": goos,
                "arch": goarch,
                "component": component,
                "audience": "runtime",
            }
            installer_id = f"{component}-{target}-installer"
            payload = output / f"{component}-{target}-payload.zip"
            tree = output / ".payload-tree" / component
            tree.mkdir(parents=True, exist_ok=True)
            shutil.copy2(binary, tree / f"{component}{suffix}")
            run([core, "payload", "build", "--tree", str(tree), "--out", str(payload)])
            artifacts.append(
                artifact(
                    installer_id,
                    binary,
                    "installer",
                    target,
                    selectors,
                )
            )
            artifacts.append(
                artifact(
                    f"{component}-{target}-payload",
                    payload,
                    "payload",
                    target,
                    selectors,
                    {"payloadOf": installer_id},
                )
            )

        if goos in {"windows", "darwin"}:
            if native_target() != target:
                fail(
                    f"Console {target} requires a native runner, current host is "
                    f"{native_target()}"
                )
            run(
                [sys.executable, "scripts/build-console.py"],
                env=child_env,
            )
            extension = "exe" if goos == "windows" else "dmg"
            console = output / f"dec-console-{goos}-{goarch}.{extension}"
            if not console.is_file():
                fail(f"Console build did not produce {console}")
            artifacts.append(
                artifact(
                    f"console-{target}-installer",
                    console,
                    "installer",
                    target,
                    {
                        "os": goos,
                        "arch": goarch,
                        "component": "console",
                        "audience": "user",
                    },
                )
            )
    finally:
        shutil.rmtree(runtime_input, ignore_errors=True)
        shutil.rmtree(output / ".payload-tree", ignore_errors=True)

    result = {
        "schema": "relkit.artifact-set/1",
        "resolutionDigest": input_document["resolutionDigest"],
        "product": input_document["product"],
        "version": version,
        "releaseCode": release_code(version),
        "channel": input_document["channel"],
        "mergeIdentity": {
            "product": input_document["product"],
            "version": version,
            "channel": input_document["channel"],
            "resolutionDigest": input_document["resolutionDigest"],
        },
        "artifacts": artifacts,
    }
    print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))


if __name__ == "__main__":
    main()
