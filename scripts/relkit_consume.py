#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Host-side bootstrap for relkit consume.

Copy this file verbatim to ``scripts/relkit_consume.py`` in the product repo —
same name, byte-identical, no host-local wrapper or renamed alias, so a drift
check (or a future relkit agent skill) can refresh it by comparing hashes.

Everything variable lives in ``scripts/relkit.lock.json``. Sparse cones, TLS
fallbacks, and ``go build`` live in the pinned SHA's ``scripts/consume.py``;
never add them here.

Chicken-egg: the first run has no ``third_party/relkit``, so the raw
``consume.py`` is downloaded once to bootstrap. Afterwards the checked-out copy
is always the one that runs — ``raw.githubusercontent.com`` is unreachable on
plenty of build networks, and the checkout is the artifact we can verify. When
syncing moves the checkout to another commit, ``consume.py`` changed underneath
us, so it is re-executed once to keep one SHA per run (ADR-007).
"""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any, Optional, Sequence
from urllib.error import URLError
from urllib.request import Request, urlopen

LOCK_SCHEMA = "relkit.consume/1"
CONSUME_REL = "scripts/consume.py"
REEXEC_ENV = "RELKIT_CONSUME_REEXEC"


def force_utf8_stdio() -> None:
    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is None:
            continue
        try:
            reconfigure(encoding="utf-8", errors="replace")
        except (ValueError, OSError):
            pass


def host_root(script_file: Optional[Path] = None) -> Path:
    here = (script_file or Path(__file__)).resolve()
    return here.parent.parent


def default_lock_path(root: Path) -> Path:
    return root / "scripts" / "relkit.lock.json"


def relkit_dir(root: Path) -> Path:
    return root / "third_party" / "relkit"


def argv_has_flag(argv: Sequence[str], name: str) -> bool:
    prefix = name + "="
    return any(item == name or item.startswith(prefix) for item in argv)


def prepare_argv(argv: Sequence[str], root: Path, lock_file: Optional[Path]) -> list[str]:
    out = list(argv)
    if not argv_has_flag(out, "--project-root"):
        out = ["--project-root", str(root), *out]
    if lock_file is not None and lock_file.is_file() and not argv_has_flag(out, "--lock"):
        out = ["--lock", str(lock_file), *out]
    return out


def load_lock(path: Path) -> dict[str, Any]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise RuntimeError(f"{path} is not a JSON object")
    schema = str(data.get("schema") or "")
    if schema and schema != LOCK_SCHEMA:
        raise RuntimeError(f"{path} schema {schema!r} is not {LOCK_SCHEMA}")
    return data


def lock_ref(lock: dict[str, Any]) -> str:
    override = (os.environ.get("RELKIT_REF") or "").strip()
    if override:
        return override
    commit = str(lock.get("commit") or "").strip()
    if commit:
        return commit
    return str(lock.get("channel") or "main")


def lock_git_urls(lock: dict[str, Any]) -> list[str]:
    raw: list[str] = []
    value = lock.get("url")
    if isinstance(value, list):
        raw.extend(str(item) for item in value)
    elif value:
        raw.append(str(value))
    env = (os.environ.get("RELKIT_URL") or "").strip()
    if env:
        raw.append(env)
    if "https://github.com/shichao402/relkit.git" not in raw:
        raw.append("https://github.com/shichao402/relkit.git")
    return raw


def strip_git_suffix(url: str) -> str:
    text = url.rstrip("/")
    if text.endswith(".git"):
        text = text[:-4]
    return text


def raw_consume_urls(git_url: str, ref: str) -> list[str]:
    """Best-effort raw file URLs for ``scripts/consume.py`` at ``ref``."""
    base = strip_git_suffix(git_url)
    path = CONSUME_REL
    urls: list[str] = []
    if "github.com/" in base:
        rest = base.split("github.com/", 1)[1]
        urls.append(f"https://raw.githubusercontent.com/{rest}/{ref}/{path}")
        urls.append(f"https://cdn.jsdelivr.net/gh/{rest}@{ref}/{path}")
    if "cnb.cool/" in base:
        urls.append(f"{base}/-/git/raw/{ref}/{path}")
    if "git.woa.com/" in base:
        urls.append(f"{base}/raw/{ref}/{path}")
    if not urls:
        urls.append(f"{base}/{path}")
    return urls


def _auth_request(url: str) -> Request:
    req = Request(url, method="GET")
    token = (os.environ.get("CNB_TOKEN") or "").strip()
    if token and "cnb.cool" in url:
        req.add_header("Authorization", f"Bearer {token}")
    return req


def download_bytes(urls: Sequence[str]) -> bytes:
    errors: list[str] = []
    for url in urls:
        try:
            with urlopen(_auth_request(url), timeout=60) as resp:
                data = resp.read()
            if b"def main(" in data and b"SPARSE_CONE_DIRS" in data:
                return data
            errors.append(f"{url}: not a consume.py")
        except (URLError, OSError, TimeoutError) as error:
            errors.append(f"{url}: {error}")
    raise RuntimeError(
        "cannot bootstrap relkit scripts/consume.py from lock URLs:\n"
        + "\n".join(errors)
    )


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def select_consume_py(root: Path, lock: dict[str, Any], ref: str) -> Path:
    override = (os.environ.get("RELKIT_CONSUME_PY") or "").strip()
    if override:
        path = Path(override)
        if not path.is_file():
            raise RuntimeError(f"RELKIT_CONSUME_PY is not a file: {path}")
        return path

    local = relkit_dir(root) / CONSUME_REL
    if local.is_file():
        return local

    urls: list[str] = []
    for git_url in lock_git_urls(lock):
        for item in raw_consume_urls(git_url, ref):
            if item not in urls:
                urls.append(item)
    data = download_bytes(urls)
    handle = tempfile.NamedTemporaryFile(
        prefix="relkit-consume-",
        suffix=".py",
        delete=False,
    )
    try:
        handle.write(data)
        handle.close()
        return Path(handle.name)
    except Exception:
        handle.close()
        Path(handle.name).unlink(missing_ok=True)
        raise


def main(argv: Optional[Sequence[str]] = None) -> int:
    force_utf8_stdio()
    args = list(sys.argv[1:] if argv is None else argv)
    root = host_root()
    lock_file = default_lock_path(root)
    lock: dict[str, Any] = {}
    if lock_file.is_file():
        lock = load_lock(lock_file)
    ref = lock_ref(lock)
    consume = select_consume_py(root, lock, ref)
    forwarded = prepare_argv(args, root, lock_file if lock_file.is_file() else None)
    print(f"relkit consume bootstrap → {consume} ref={ref}", flush=True)

    checkout = relkit_dir(root) / CONSUME_REL
    before = digest(checkout) if checkout.is_file() else ""
    code = subprocess.call([sys.executable, str(consume), *forwarded])
    if code != 0 or os.environ.get(REEXEC_ENV):
        return code
    if not checkout.is_file() or digest(checkout) == before:
        return code

    print(
        f"relkit consume: {CONSUME_REL} changed with the checkout; "
        "re-running it against the synced tree",
        flush=True,
    )
    env = {**os.environ, REEXEC_ENV: "1"}
    return subprocess.call(
        [sys.executable, str(checkout), *forwarded, "--skip-update"],
        env=env,
    )


if __name__ == "__main__":
    raise SystemExit(main())
