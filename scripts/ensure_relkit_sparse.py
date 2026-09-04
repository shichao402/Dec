#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Sparse-checkout relkit into third_party/relkit.

Upstream: https://cnb.cool/shichao402/relkit

Cone paths cover the Go SDK (Dec import) plus sources needed to build cmd/relkit
for release staging. Root files (go.mod / go.sum / …) come with cone mode.

Pinned by default to verified full commit SHA on relkit main. Short SHAs are
rejected by most remotes as fetch refs, so the pin must be the full object id.
Override with --ref / RELKIT_REF.
Override clone URL with --url / RELKIT_URL. If CNB_TOKEN is set and the URL is
plain cnb.cool HTTPS, inject token auth automatically.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Optional, Sequence

DEFAULT_URL = "https://cnb.cool/shichao402/relkit.git"
DEFAULT_REF = "fd4676ae0e7498a7b61bd20eca1034b7d8b3a42d"

# Cone paths: Go SDK + CLI sources Dec needs.
SPARSE_CONE_DIRS = (
    "sdk",
    "api",
    "internal",
    "cmd/relkit",
    "embed",
    "version",
)


def project_root() -> Path:
    return Path(__file__).resolve().parent.parent


def relkit_dir(root: Path) -> Path:
    return root / "third_party" / "relkit"


def force_utf8_stdio() -> None:
    """GitHub Windows runner 的控制台编码是 cp1252，日志里的 `→` 与被转发的
    子进程输出会让 print 抛 UnicodeEncodeError，进而整步失败。"""
    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is None:
            continue
        try:
            reconfigure(encoding="utf-8", errors="replace")
        except (ValueError, OSError):
            pass


force_utf8_stdio()


def log(msg: str) -> None:
    print(msg, flush=True)


def run(
    command: Sequence[str],
    *,
    cwd: Path,
    timeout_seconds: int = 600,
    env: Optional[dict] = None,
) -> None:
    display = subprocess.list2cmdline([str(part) for part in command])
    log(f"$ {display}  (cwd={cwd})")
    result = subprocess.run(
        list(command),
        cwd=str(cwd),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=timeout_seconds,
        env={**os.environ, **(env or {})},
        check=False,
    )
    if result.stdout:
        for line in result.stdout.splitlines():
            log(line)
    if result.returncode != 0:
        err = ((result.stdout or "") + "\n" + (result.stderr or "")).strip()
        if err:
            log(err)
        raise RuntimeError(f"command failed ({result.returncode}): {display}")


def looks_like_commit(ref: str) -> bool:
    if len(ref) < 7:
        return False
    return all(ch in "0123456789abcdef" for ch in ref.lower())


def resolve_url(url: str) -> str:
    """Inject CNB_TOKEN into plain cnb.cool HTTPS URLs when present."""
    token = os.environ.get("CNB_TOKEN", "").strip()
    if not token:
        return url
    prefix = "https://cnb.cool/"
    if url.startswith(prefix) and "@" not in url.split("://", 1)[-1].split("/", 1)[0]:
        return f"https://cnb:{token}@cnb.cool/" + url[len(prefix) :]
    return url


def ensure_sparse_clone(
    root: Path,
    *,
    url: str,
    ref: str,
    allow_stale: bool = False,
) -> Path:
    dest = relkit_dir(root)
    dest.parent.mkdir(parents=True, exist_ok=True)
    url = resolve_url(url)
    can_reuse = (dest / ".git").exists()

    if not can_reuse:
        if dest.exists():
            shutil.rmtree(dest)
        log(f"sparse clone {url} → {dest}")
        clone_cmd = [
            "git",
            "clone",
            "--filter=blob:none",
            "--sparse",
            url,
            str(dest),
        ]
        if not looks_like_commit(ref):
            clone_cmd[4:4] = ["--branch", ref]
        run(clone_cmd, cwd=root, timeout_seconds=600)
        run(
            ["git", "sparse-checkout", "set", "--cone", *SPARSE_CONE_DIRS],
            cwd=dest,
        )
    else:
        log(f"update existing sparse checkout: {dest}")
        run(["git", "remote", "set-url", "origin", url], cwd=dest)
        run(
            ["git", "sparse-checkout", "set", "--cone", *SPARSE_CONE_DIRS],
            cwd=dest,
        )

    try:
        if looks_like_commit(ref):
            # Remotes usually reject `git fetch origin <sha>`. Fetch reachable
            # heads/tags, then check out the exact object id.
            run(
                [
                    "git",
                    "fetch",
                    "--filter=blob:none",
                    "--tags",
                    "origin",
                    "+refs/heads/*:refs/remotes/origin/*",
                ],
                cwd=dest,
                timeout_seconds=600,
            )
            run(["git", "checkout", "--force", ref], cwd=dest)
        else:
            run(
                ["git", "fetch", "--filter=blob:none", "--tags", "origin", ref],
                cwd=dest,
                timeout_seconds=600,
            )
            run(["git", "checkout", "--force", "FETCH_HEAD"], cwd=dest)
    except RuntimeError as error:
        if not (allow_stale and can_reuse):
            raise
        log(f"warn: fetch failed, reuse local HEAD: {error}")

    head = subprocess.check_output(
        ["git", "rev-parse", "HEAD"],
        cwd=str(dest),
        text=True,
        encoding="utf-8",
    ).strip()
    if looks_like_commit(ref) and not head.lower().startswith(ref.lower()):
        raise RuntimeError(f"checked out {head}, expected pin {ref}")
    log(f"relkit HEAD = {head} (requested {ref})")

    sdk_go = dest / "sdk" / "updater.go"
    if not sdk_go.is_file():
        raise RuntimeError(f"sparse checkout missing Go SDK: {sdk_go}")
    go_mod = dest / "go.mod"
    if not go_mod.is_file():
        raise RuntimeError(f"sparse checkout missing go.mod: {go_mod}")
    return dest


def build_cli(root: Path, dest: Path) -> Path:
    out = root / "relkit-bin"
    if os.name == "nt":
        out = root / "relkit-bin.exe"
    log(f"go build cmd/relkit → {out}")
    run(
        [
            "go",
            "build",
            "-trimpath",
            "-ldflags",
            "-s -w",
            "-o",
            str(out),
            "./cmd/relkit",
        ],
        cwd=dest,
        timeout_seconds=600,
        env={"CGO_ENABLED": "0"},
    )
    if not out.is_file():
        raise RuntimeError(f"go build did not produce {out}")
    if os.name != "nt":
        out.chmod(out.stat().st_mode | 0o111)
    log(f"relkit CLI ready: {out} ({out.stat().st_size} bytes)")
    return out


def create_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Sparse-checkout relkit into third_party/relkit",
    )
    parser.add_argument(
        "--url",
        default=os.environ.get("RELKIT_URL", DEFAULT_URL),
        help=f"relkit git URL (default {DEFAULT_URL})",
    )
    parser.add_argument(
        "--ref",
        default=os.environ.get("RELKIT_REF", DEFAULT_REF),
        help=f"branch/tag/commit (default {DEFAULT_REF}; env RELKIT_REF)",
    )
    parser.add_argument(
        "--sdk-only",
        action="store_true",
        help="only sync sources; do not build cmd/relkit",
    )
    parser.add_argument(
        "--build-cli",
        action="store_true",
        help="build cmd/relkit into ./relkit-bin (overrides --sdk-only)",
    )
    parser.add_argument(
        "--allow-stale",
        action="store_true",
        help="if local checkout exists, reuse HEAD when fetch fails (local only)",
    )
    return parser


def should_build_cli(args: argparse.Namespace) -> bool:
    if args.build_cli:
        return True
    if args.sdk_only:
        return False
    # Standalone default: build CLI so release staging works.
    return True


def main(argv: Optional[Sequence[str]] = None) -> int:
    try:
        args = create_parser().parse_args(argv)
        root = project_root()
        log("========== ensure_relkit_sparse ==========")
        log(f"url={args.url} ref={args.ref}")
        dest = ensure_sparse_clone(
            root,
            url=args.url,
            ref=args.ref,
            allow_stale=args.allow_stale,
        )
        if should_build_cli(args):
            build_cli(root, dest)
        else:
            log("skip CLI build (--sdk-only)")
        log(f"Go SDK path: {dest / 'sdk'}")
        log("relkit sparse ready")
        return 0
    except Exception as error:
        log(f"ensure_relkit_sparse failed: {error}")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
