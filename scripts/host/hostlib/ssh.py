"""Implementation cluster: ssh."""

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


def _impl__expand_ssh_path(raw: str) -> Path:
    return Path(os.path.expanduser(os.path.expandvars(raw.strip().strip("\"'"))))

def _impl__ssh_include_paths(raw: str) -> list[Path]:
    expanded = _expand_ssh_path(raw)
    name = expanded.name
    if any(ch in name for ch in "*?["):
        parent = expanded.parent
        if not parent.is_dir():
            return []
        return sorted(path for path in parent.glob(name) if path.is_file())
    return [expanded]

def _impl_parse_ssh_config(
    config_path: Optional[Path] = None,
    *,
    _seen: Optional[set[Path]] = None,
) -> tuple[list[str], list[str]]:
    exact, patterns, _ports = parse_ssh_config_ports(config_path, _seen=_seen)
    return exact, patterns

def _impl_parse_ssh_config_ports(
    config_path: Optional[Path] = None,
    *,
    _seen: Optional[set[Path]] = None,
) -> tuple[list[str], list[str], list[tuple[list[str], int]]]:
    """Host names plus the Port each Host block declares.

    Ports usually live in an Include (devcloud writes Port 36000 there), so a
    bare host name in onboarding state says nothing about the real port.
    """
    path = (config_path or (Path.home() / ".ssh" / "config")).resolve()
    seen = _seen if _seen is not None else set()
    if path in seen or not path.is_file():
        return [], [], []
    seen.add(path)
    exact: list[str] = []
    patterns: list[str] = []
    ports: list[tuple[list[str], int]] = []
    current: list[str] = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        lower = stripped.lower()
        if lower.startswith("include "):
            for item in stripped.split()[1:]:
                for nested in _ssh_include_paths(item):
                    more_exact, more_patterns, more_ports = parse_ssh_config_ports(
                        nested, _seen=seen
                    )
                    for name in more_exact:
                        if name not in exact:
                            exact.append(name)
                    for name in more_patterns:
                        if name not in patterns:
                            patterns.append(name)
                    ports.extend(more_ports)
            continue
        if lower.startswith("port ") and current:
            try:
                ports.append((list(current), int(stripped.split()[1])))
            except (IndexError, ValueError):
                pass
            continue
        if not lower.startswith("host "):
            continue
        current = stripped.split()[1:]
        for token in current:
            if any(ch in token for ch in "*?!"):
                if token not in patterns:
                    patterns.append(token)
            elif token not in exact:
                exact.append(token)
    return exact, patterns, ports

def _impl_list_ssh_hosts(config_path: Optional[Path] = None) -> list[str]:
    exact, _patterns = parse_ssh_config(config_path)
    return exact

def _impl_ssh_host_port(name: str, config_path: Optional[Path] = None) -> Optional[int]:
    """Port ssh would pick for this Host, or None when the default 22 applies."""
    if not name:
        return None
    _exact, _patterns, ports = parse_ssh_config_ports(config_path)
    for tokens, port in ports:
        for token in tokens:
            if token == name or (
                any(ch in token for ch in "*?") and fnmatch.fnmatch(name, token)
            ):
                return port
    return None

def _impl_ssh_target(host: str, port: Optional[int] = None) -> str:
    """host:port for logs and errors; never guess silently between 22 and 36000."""
    resolved = port if port is not None else ssh_host_port(host)
    return f"{host}:{resolved}" if resolved else f"{host}:22 (ssh default)"

def _impl_ssh_host_allowed(name: str, config_path: Optional[Path] = None) -> bool:
    exact, patterns = parse_ssh_config(config_path)
    if name in exact:
        return True
    for pattern in patterns:
        if pattern == "*":
            continue
        if fnmatch.fnmatch(name, pattern):
            return True
    return False

def _impl_ssh_host_recommend(
    config_path: Optional[Path] = None, *, root: Optional[Path] = None
) -> str:
    exact, patterns, ports = parse_ssh_config_ports(config_path)

    def with_port(name: str) -> str:
        for tokens, port in ports:
            if name in tokens:
                return f"{name} (port {port})"
        return name

    bits: list[str] = []
    if exact:
        bits.append("candidates: " + ", ".join(with_port(item) for item in exact))
    useful = [item for item in patterns if item != "*"]
    if useful:
        bits.append("patterns: " + ", ".join(with_port(item) for item in useful))
    if root is not None:
        matched = ssh_inventory(root, config_path).get("matched") or []
        if matched:
            bits.append(
                "matched: "
                + ", ".join(
                    f"{item['host']} via {item['via']}" for item in matched
                )
            )
    return "; ".join(bits) if bits else "no Host entries in ~/.ssh/config"

def _impl_default_ssh_config_path() -> Path:
    return Path.home() / ".ssh" / "config"

def _impl_parse_known_host_names(path: Path) -> list[str]:
    if not path.is_file():
        return []
    names: list[str] = []
    try:
        lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
    except OSError:
        return []
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or stripped.startswith("|"):
            continue
        parts = stripped.split()
        if not parts:
            continue
        hostfield = parts[1] if parts[0].startswith("@") and len(parts) > 1 else parts[0]
        for token in hostfield.split(","):
            token = token.strip()
            if token.startswith("[") and "]:" in token:
                token = token[1 : token.index("]:")]
            if not token or token.startswith("|") or "*" in token or "?" in token:
                continue
            if token not in names:
                names.append(token)
    return names

def _impl_collect_json_hosts(value: Any, into: list[str]) -> None:
    if isinstance(value, dict):
        for item in value.values():
            collect_json_hosts(item, into)
        return
    if isinstance(value, list):
        for item in value:
            collect_json_hosts(item, into)
        return
    if not isinstance(value, str) or "://" not in value:
        return
    try:
        parsed = urlparse(value)
    except ValueError:
        return
    host = (parsed.hostname or "").strip()
    if host and host not in into:
        into.append(host)

def _impl_ssh_inventory(
    root: Path, config_path: Optional[Path] = None
) -> dict[str, Any]:
    path = (config_path or default_ssh_config_path()).resolve()
    exact, patterns, ports = parse_ssh_config_ports(path)
    useful = [item for item in patterns if item != "*"]
    sources: list[str] = []
    for name in exact:
        sources.append(name)
    known_files = [Path.home() / ".ssh" / "known_hosts"]
    known_dir = Path.home() / ".ssh" / "known_hosts.d"
    if known_dir.is_dir():
        known_files.extend(sorted(p for p in known_dir.iterdir() if p.is_file()))
    known: list[str] = []
    for known_path in known_files:
        for name in parse_known_host_names(known_path):
            if name not in known:
                known.append(name)
    config_hosts: list[str] = []
    relkit_json = root / "relkit.json"
    if relkit_json.is_file():
        try:
            collect_json_hosts(load_json(relkit_json), config_hosts)
        except (OSError, json.JSONDecodeError, TypeError):
            config_hosts = []
    matched: list[dict[str, str]] = []
    seen: set[str] = set()
    for name, via in (
        *[(item, "relkit.json") for item in config_hosts],
        *[(item, "known_hosts") for item in known],
    ):
        if name in seen or name in exact:
            continue
        for pattern in useful:
            if fnmatch.fnmatch(name, pattern):
                matched.append({"host": name, "via": via, "pattern": pattern})
                seen.add(name)
                break
    return {
        "config": str(path),
        "exact": exact,
        "patterns": useful,
        "matched": matched,
        "ports": [
            {"hosts": tokens, "port": port} for tokens, port in ports
        ],
    }

def _impl_print_ssh_inventory(inventory: dict[str, Any]) -> None:
    print(f"SSH config: {inventory.get('config')}")
    index = 1
    for name in inventory.get("exact") or []:
        print(f"  {index}. {name} (exact Host)")
        index += 1
    for item in inventory.get("matched") or []:
        print(
            f"  {index}. {item['host']} "
            f"(matches {item['pattern']} via {item['via']})"
        )
        index += 1
    patterns = inventory.get("patterns") or []
    if patterns:
        print("  patterns (not sshable aliases): " + ", ".join(patterns))
    if index == 1 and not patterns:
        print("  (no Host entries)")

def _impl_ssh_argv(host: str, port: Optional[int]) -> list[str]:
    argv = ["ssh", "-o", "BatchMode=yes"]
    if port:
        argv.extend(["-p", str(port)])
    argv.append(host)
    return argv

def _impl_ssh_run(
    host: str,
    remote: Sequence[str],
    *,
    timeout: int = 60,
    port: Optional[int] = None,
) -> subprocess.CompletedProcess[str]:
    if not host:
        raise Fail("SSH Host is required")
    resolved = port if port is not None else ssh_host_port(host)
    target = ssh_target(host, resolved)
    # ssh joins argv with spaces and the far side re-parses it, so an argument
    # holding a space would silently split into extra words.
    argv = [*ssh_argv(host, resolved), "--", *(shlex.quote(arg) for arg in remote)]
    try:
        result = subprocess.run(
            argv,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
            check=False,
        )
    except FileNotFoundError as error:
        raise Fail("ssh is not installed") from error
    except subprocess.TimeoutExpired as error:
        raise Fail(f"SSH to {target} timed out") from error
    if result.returncode != 0:
        stderr = redact_text((result.stderr or result.stdout or "").strip())
        raise Fail(
            f"SSH to {target} failed (exit {result.returncode}). "
            f"Stop. Do not switch credentials. {stderr}"
        )
    return result

def _impl_ssh_write(
    host: str,
    remote_path: str,
    data: bytes,
    *,
    timeout: int = 60,
    port: Optional[int] = None,
) -> None:
    if not host:
        raise Fail("SSH Host is required")
    if not remote_path.startswith("/"):
        raise Fail("remote path must be absolute")
    resolved = port if port is not None else ssh_host_port(host)
    target = ssh_target(host, resolved)
    argv = [*ssh_argv(host, resolved), "--", "sudo", "tee", shlex.quote(remote_path)]
    try:
        result = subprocess.run(
            argv,
            input=data,
            capture_output=True,
            timeout=timeout,
            check=False,
        )
    except FileNotFoundError as error:
        raise Fail("ssh is not installed") from error
    except subprocess.TimeoutExpired as error:
        raise Fail(f"SSH to {target} timed out") from error
    if result.returncode != 0:
        stderr = redact_text((result.stderr or b"").decode("utf-8", "replace").strip())
        raise Fail(
            f"SSH write to {target} failed (exit {result.returncode}). "
            f"Stop. Do not switch credentials. {stderr}"
        )

_IMPLEMENTATIONS = {
    "_expand_ssh_path": _impl__expand_ssh_path,
    "_ssh_include_paths": _impl__ssh_include_paths,
    "parse_ssh_config": _impl_parse_ssh_config,
    "parse_ssh_config_ports": _impl_parse_ssh_config_ports,
    "list_ssh_hosts": _impl_list_ssh_hosts,
    "ssh_host_port": _impl_ssh_host_port,
    "ssh_target": _impl_ssh_target,
    "ssh_host_allowed": _impl_ssh_host_allowed,
    "ssh_host_recommend": _impl_ssh_host_recommend,
    "default_ssh_config_path": _impl_default_ssh_config_path,
    "parse_known_host_names": _impl_parse_known_host_names,
    "collect_json_hosts": _impl_collect_json_hosts,
    "ssh_inventory": _impl_ssh_inventory,
    "print_ssh_inventory": _impl_print_ssh_inventory,
    "ssh_argv": _impl_ssh_argv,
    "ssh_run": _impl_ssh_run,
    "ssh_write": _impl_ssh_write,
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
