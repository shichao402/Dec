"""Implementation cluster: state."""

from __future__ import annotations

import argparse
import fnmatch
import hashlib
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import traceback
import zipfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Optional, Sequence
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse, urlunparse
from urllib.request import Request, urlopen

from .const import *
from .facets import (
    BY_NAME,
    TARGETS,
    components as registry_components,
    detected_components,
    updater_process_values,
)
from .digest import tree_sha256 as _tree_sha256
from .gates import GATES, run_gates
from . import runtime as _runtime


def _impl_force_utf8_stdio() -> None:
    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is not None:
            try:
                reconfigure(encoding="utf-8", errors="replace")
            except (ValueError, OSError):
                pass

def _impl_host_root(script_file: Optional[Path] = None) -> Path:
    if script_file is None:
        facade_file = getattr(_runtime.facade(), "__file__", __file__)
        script_file = Path(facade_file)
    path = script_file.resolve()
    parent = path.parent
    if parent.name == "host" and parent.parent.name == "scripts":
        return parent.parent.parent
    if parent.name == "scripts":
        return parent.parent
    return parent.parent

def _impl_host_scripts_dir(script_file: Optional[Path] = None) -> Path:
    if script_file is None:
        facade_file = getattr(_runtime.facade(), "__file__", __file__)
        script_file = Path(facade_file)
    return script_file.resolve().parent

def _impl_tree_sha256(directory: Path) -> str:
    try:
        return _tree_sha256(directory)
    except ValueError as error:
        raise Fail(str(error)) from error

def _impl_relkit_dir(root: Path) -> Path:
    return root / ".relkit"

def _impl_state_path(root: Path) -> Path:
    return relkit_dir(root) / "onboarding.json"

def _impl_cache_dir(root: Path) -> Path:
    return relkit_dir(root) / "cache"

def _impl_projection_path(root: Path) -> Path:
    return cache_dir(root) / "onboarding.md"

def _impl_local_path(root: Path) -> Path:
    return cache_dir(root) / "onboarding.local.json"

def _impl_empty_steps() -> dict[str, Any]:
    return {step: {"status": "unanswered", "value": None, "note": ""} for step in STEP_IDS}

def _impl_default_state(root: Path) -> dict[str, Any]:
    return {
        "schema": SCHEMA,
        "product": None,
        "steps": empty_steps(),
        "serve": {
            "sshHost": None,
            "sshPort": None,
            "configDir": DEFAULT_SERVE_DIR,
            "shareWith": None,
            "remoteVersion": None,
        },
        "agent": {
            "sshHost": None,
            "sshPort": None,
            "configPath": DEFAULT_AGENT_CONFIG,
            "remoteVersion": None,
        },
        "root": str(root),
    }

def _impl_load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))

def _impl_dump_json(data: dict[str, Any]) -> str:
    return json.dumps(data, indent=2, ensure_ascii=False) + "\n"

def _impl_load_state(root: Path) -> dict[str, Any]:
    path = state_path(root)
    if not path.is_file():
        return default_state(root)
    data = load_json(path)
    if data.get("schema") != SCHEMA:
        raise Fail(f"{path} must use schema {SCHEMA!r}")
    steps = empty_steps()
    raw = data.get("steps") or {}
    if isinstance(raw, dict):
        for key, value in raw.items():
            if key in steps and isinstance(value, dict):
                steps[key] = {
                    "status": value.get("status") or "unanswered",
                    "value": value.get("value"),
                    "note": value.get("note") or "",
                }
    data["steps"] = steps
    data.setdefault("serve", default_state(root)["serve"])
    data.setdefault("agent", default_state(root)["agent"])
    return data

def _impl_render_onboarding_md(state: dict[str, Any]) -> str:
    product = state.get("product") or "(unset)"
    lines = [
        "# relkit onboarding",
        "",
        "Generated from `.relkit/onboarding.json`. Do not edit.",
        "Regenerate: `python scripts/host/relkit_host.py status`.",
        "Reset: `python scripts/host/relkit_host.py onboard reset --yes`.",
        "",
        f"product: `{product}`",
        "",
        "| step | status | value | note |",
        "| --- | --- | --- | --- |",
    ]
    for step_id in STEP_IDS:
        item = (state.get("steps") or {}).get(step_id) or {}
        value = item.get("value")
        if value in (None, ""):
            value = "—"
        note = item.get("note") or "—"
        status = item.get("status") or "unanswered"
        lines.append(f"| `{step_id}` | {status} | {value} | {note} |")
    unresolved = next_unresolved_step(state) if state.get("steps") else None
    lines.extend(["", "## next", ""])
    if unresolved:
        lines.append(f"`{unresolved}`")
        lines.append("")
        lines.append(explain_step(unresolved))
    else:
        lines.append("no unresolved step; run verify")
    lines.append("")
    return "\n".join(lines)

def _impl_write_onboarding_md(root: Path, state: dict[str, Any]) -> None:
    cache_dir(root).mkdir(parents=True, exist_ok=True)
    projection_path(root).write_text(render_onboarding_md(state), encoding="utf-8")

def _impl_save_state(root: Path, data: dict[str, Any]) -> None:
    relkit_dir(root).mkdir(parents=True, exist_ok=True)
    state_path(root).write_text(dump_json(data), encoding="utf-8")
    write_onboarding_md(root, data)

def _impl_load_local(root: Path) -> dict[str, Any]:
    path = local_path(root)
    if not path.is_file():
        return {"schema": LOCAL_SCHEMA, "evidence": {}, "secretRefs": []}
    data = load_json(path)
    data.setdefault("schema", LOCAL_SCHEMA)
    data.setdefault("evidence", {})
    data.setdefault("secretRefs", [])
    return data

def _impl_save_local(root: Path, data: dict[str, Any]) -> None:
    cache_dir(root).mkdir(parents=True, exist_ok=True)
    local_path(root).write_text(dump_json(data), encoding="utf-8")

def _impl_ensure_gitignore(root: Path) -> None:
    path = root / ".gitignore"
    existing = path.read_text(encoding="utf-8") if path.is_file() else ""
    lines = existing.splitlines()
    missing = [item for item in GITIGNORE_RELKIT if item not in lines and item not in existing]
    if not missing:
        return
    with path.open("a", encoding="utf-8") as handle:
        if existing and not existing.endswith("\n"):
            handle.write("\n")
        for item in missing:
            handle.write(item + "\n")

def _impl_redact_text(text: str) -> str:
    text = re.sub(
        r"(export\s+(?:RELKIT_UPLOAD_TOKEN|RELKIT_SERVE_TOKEN|RELKIT_ADMIN_BOOTSTRAP)=)(['\"]?)([^'\"\s]+)\2",
        r"\1\2<redacted>\2",
        text,
    )
    text = re.sub(r"(Bearer\s+)\S+", r"\1<redacted>", text, flags=re.I)
    return text

def _impl_extract_export(name: str, text: str) -> Optional[str]:
    match = re.search(
        rf"export\s+{re.escape(name)}=(['\"]?)([^'\"\s]+)\1",
        text,
    )
    if not match:
        return None
    return match.group(2)

def _impl_set_step(
    state: dict[str, Any],
    step_id: str,
    status: str,
    value: Any = None,
    note: str = "",
    mark_later_stale: bool = False,
) -> None:
    if step_id not in STEP_IDS:
        raise Fail(f"unknown step {step_id}")
    if status not in STATUSES:
        raise Fail(f"unknown status {status}")
    previous = state["steps"][step_id]
    if value is None:
        value = previous.get("value")
    state["steps"][step_id] = {"status": status, "value": value, "note": note}
    if mark_later_stale:
        started = False
        for later in STEP_IDS:
            if later == step_id:
                started = True
                continue
            if started and state["steps"][later]["status"] in (
                "confirmed",
                "applied",
                "verified",
            ):
                state["steps"][later]["status"] = "stale"

_IMPLEMENTATIONS = {
    "force_utf8_stdio": _impl_force_utf8_stdio,
    "host_root": _impl_host_root,
    "host_scripts_dir": _impl_host_scripts_dir,
    "tree_sha256": _impl_tree_sha256,
    "relkit_dir": _impl_relkit_dir,
    "state_path": _impl_state_path,
    "cache_dir": _impl_cache_dir,
    "projection_path": _impl_projection_path,
    "local_path": _impl_local_path,
    "empty_steps": _impl_empty_steps,
    "default_state": _impl_default_state,
    "load_json": _impl_load_json,
    "dump_json": _impl_dump_json,
    "load_state": _impl_load_state,
    "render_onboarding_md": _impl_render_onboarding_md,
    "write_onboarding_md": _impl_write_onboarding_md,
    "save_state": _impl_save_state,
    "load_local": _impl_load_local,
    "save_local": _impl_save_local,
    "ensure_gitignore": _impl_ensure_gitignore,
    "redact_text": _impl_redact_text,
    "extract_export": _impl_extract_export,
    "set_step": _impl_set_step,
}

def _export(name: str):
    implementation = _IMPLEMENTATIONS[name]

    def exported(*args, **kwargs):
        facade = _runtime.facade()
        overlay = {
            key: value for key, value in vars(facade).items()
            if not key.startswith("__")
        }
        namespace = globals()
        missing = object()
        previous = {key: namespace.get(key, missing) for key in overlay}
        namespace.update(overlay)
        try:
            return implementation(*args, **kwargs)
        finally:
            for key, value in previous.items():
                if value is missing:
                    namespace.pop(key, None)
                else:
                    namespace[key] = value

    exported.__name__ = name
    exported.__qualname__ = name
    exported.__doc__ = implementation.__doc__
    exported.__wrapped__ = implementation
    return exported

for _name in _IMPLEMENTATIONS:
    globals()[_name] = _export(_name)

__all__ = tuple(_IMPLEMENTATIONS)
