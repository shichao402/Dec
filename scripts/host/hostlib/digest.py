"""Deterministic host-tree digests."""

from __future__ import annotations

import hashlib
from pathlib import Path


def tree_sha256(directory: Path) -> str:
    digest = hashlib.sha256()
    paths = sorted(
        path for path in directory.rglob("*")
        if path.is_file() and "__pycache__" not in path.parts
    )
    if not paths:
        raise ValueError("scripts/host is empty")
    for path in paths:
        relative = path.relative_to(directory).as_posix()
        digest.update(relative.encode("utf-8"))
        digest.update(b"\0")
        digest.update(path.read_bytes().replace(b"\r\n", b"\n").replace(b"\r", b"\n"))
        digest.update(b"\0")
    return digest.hexdigest()
