"""Implementation cluster: inspect."""

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
from .gates import GATES, run_gates, unused_publish_channels
from . import runtime as _runtime


def _impl_inspect_path(root: Path) -> Path:
    return cache_dir(root) / "inspect.json"

def _impl_inventory_path(root: Path) -> Path:
    return cache_dir(root) / "inventory.json"

def _impl_ops_journal_path(root: Path) -> Path:
    return cache_dir(root) / "ops-journal.jsonl"

def _impl_onboarding_ignored(root: Path) -> Optional[bool]:
    """Whether git ignores the decision record; None when git cannot answer.

    ensure_gitignore only appends lines, so a repo that already excluded the
    whole .relkit/ tree would silently keep onboarding.json uncommittable.
    """
    try:
        result = subprocess.run(
            ["git", "check-ignore", "-q", ".relkit/onboarding.json"],
            cwd=str(root),
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )
    except (FileNotFoundError, OSError, subprocess.TimeoutExpired):
        return None
    if result.returncode == 0:
        return True
    if result.returncode == 1:
        return False
    return None

def _release_tuple(value: Any) -> Optional[tuple[int, ...]]:
    match = re.fullmatch(r"v?(\d+)\.(\d+)\.(\d+)", str(value or "").strip())
    if not match:
        return None
    return tuple(int(item) for item in match.groups())

def _impl_upstream_latest_release(root: Path) -> str:
    """Newest published release tag, or "" when it cannot be resolved.

    Anonymous api.github.com allows 60 calls an hour and does not share that
    budget with release downloads, so the tag comes from the redirect that
    /releases/latest answers with. A network failure must leave inspect usable,
    so it degrades to an unknown relation instead of a finding.

    The TTL cache is also invalidated when the pinned lock.release is newer than
    the cached latest: a just-published tag can otherwise look stale for up to
    an hour even though the lock already advanced.
    """
    cached_path = cache_dir(root) / "upstream-latest.json"
    now = datetime.now(timezone.utc).timestamp()
    try:
        cached = load_json(cached_path)
        if now - float(cached.get("checkedAt") or 0) < UPSTREAM_LATEST_TTL:
            cached_release = str(cached.get("release") or "")
            lock_path = root / "scripts" / "relkit.lock.json"
            try:
                locked = _release_tuple(load_json(lock_path).get("release"))
                newest_cached = _release_tuple(cached_release)
                if locked and newest_cached and locked > newest_cached:
                    pass  # lock already past cache; refetch
                else:
                    return cached_release
            except (OSError, json.JSONDecodeError, TypeError, ValueError):
                return cached_release
    except (OSError, json.JSONDecodeError, TypeError, ValueError):
        pass
    url = f"https://github.com/{GITHUB_REPO}/releases/latest"
    try:
        with urlopen(
            Request(url, headers={"User-Agent": "relkit-host"}), timeout=15
        ) as response:
            resolved = urlparse(response.geturl()).path.rsplit("/", 1)[-1]
    except (HTTPError, URLError, TimeoutError, OSError, ValueError):
        return ""
    if not _release_tuple(resolved):
        return ""
    try:
        cache_dir(root).mkdir(parents=True, exist_ok=True)
        cached_path.write_text(
            dump_json({"checkedAt": now, "release": resolved}), encoding="utf-8"
        )
    except OSError:
        pass
    return resolved

def _impl_lock_currency(root: Path, lock_release: Any) -> dict[str, Any]:
    """Compare the pinned release against the newest published one."""
    latest = upstream_latest_release(root)
    locked = _release_tuple(lock_release)
    newest = _release_tuple(latest)
    if locked and newest:
        relation = "behind" if locked < newest else "current-or-newer"
    else:
        relation = "unknown"
    return {"latestRelease": latest or None, "releaseRelation": relation}

def _impl_env_inspect_report(root: Path) -> dict[str, Any]:
    findings: list[dict[str, str]] = []
    facts: dict[str, Any] = {
        "stack": detect_stack(root),
        "relkit.json": None,
        "backends": {},
        "version": {},
        "lock": None,
        "onboarding": None,
        "ssh": ssh_inventory(root),
    }
    relkit_json = root / "relkit.json"
    if relkit_json.is_file():
        try:
            data = load_json(relkit_json)
        except (OSError, json.JSONDecodeError, TypeError) as error:
            findings.append(
                {
                    "severity": "error",
                    "code": "relkit-json-unreadable",
                    "detail": str(error),
                }
            )
            data = {}
        facts["relkit.json"] = True
        facts["product"] = data.get("product")
        backends = data.get("backends") if isinstance(data.get("backends"), dict) else {}
        facts["backends"] = {
            name: (backend or {}).get("type")
            for name, backend in backends.items()
            if isinstance(backend, dict)
        }
        site = data.get("site") if isinstance(data.get("site"), dict) else {}
        facts["siteTitle"] = site.get("title")
        if "makers" in site:
            findings.append(
                {
                    "severity": "error",
                    "code": "product-site-makers",
                    "detail": "site.makers belongs in relkit-agent.json; product relkit.json may only carry title/description/homepage",
                }
            )
        if site and not str(site.get("title") or "").strip():
            findings.append(
                {
                    "severity": "error",
                    "code": "missing-site-title",
                    "detail": "relkit.json site.title is required for the human release catalog",
                }
            )
        for name, kind in facts["backends"].items():
            if kind in STALE_BACKEND_TYPES:
                findings.append(
                    {
                        "severity": "error",
                        "code": "stale-backend-type",
                        "detail": f"backends.{name}.type={kind}",
                    }
                )
        unused, queried = unused_publish_channels(root)
        if unused:
            facts["clientChannels"] = sorted(queried)
            facts["unusedPublishChannels"] = sorted(unused)
            findings.append(
                {
                    "severity": "warning",
                    "code": "publish-channel-no-client-consumer",
                    "detail": (
                        "publish channel "
                        + ", ".join(sorted(unused))
                        + " is not queried by any client "
                        + f"(host source queries: {', '.join(sorted(queried))}); "
                        "installed clients will not see those releases"
                    ),
                }
            )
    else:
        facts["relkit.json"] = False
        findings.append(
            {
                "severity": "warning",
                "code": "missing-relkit-json",
                "detail": "relkit.json is absent (fresh repo)",
            }
        )
    version_json = root / "VERSION.json"
    version_legacy = root / "VERSION"
    facts["version"] = {
        "VERSION.json": version_json.is_file(),
        "VERSION": version_legacy.is_file(),
    }
    if version_legacy.is_file() and not version_json.is_file():
        findings.append(
            {
                "severity": "warning",
                "code": "legacy-VERSION",
                "detail": "VERSION exists without VERSION.json",
            }
        )
    lock_path = root / "scripts" / "relkit.lock.json"
    if lock_path.is_file():
        try:
            lock = load_json(lock_path)
            expected = str(lock.get("hostScriptsSha256") or "").lower()
            host_dir = root / "scripts" / "host"
            actual = (
                tree_sha256(host_dir)
                if (host_dir / "relkit_host.py").is_file()
                else ""
            )
            currency = lock_currency(root, lock.get("release"))
            facts["lock"] = {
                "release": lock.get("release"),
                "hostScriptsMatch": bool(expected) and expected == actual,
                **currency,
            }
            if currency["releaseRelation"] == "behind":
                findings.append(
                    {
                        "severity": "warning",
                        "code": "lock-behind-upstream",
                        "detail": (
                            f"lock {lock.get('release')} is behind upstream "
                            f"{currency['latestRelease']}; confirm the intent to "
                            f"upgrade or to stay before other ops"
                        ),
                    }
                )
            if expected and actual and expected != actual:
                findings.append(
                    {
                        "severity": "warning",
                        "code": "host-scripts-drift",
                        "detail": "scripts/host hash != lock hostScriptsSha256",
                    }
                )
        except (OSError, json.JSONDecodeError, Fail, TypeError) as error:
            findings.append(
                {
                    "severity": "warning",
                    "code": "lock-unreadable",
                    "detail": str(error),
                }
            )
    ignored = onboarding_ignored(root)
    facts["onboardingIgnored"] = ignored
    if ignored:
        findings.append(
            {
                "severity": "error",
                "code": "onboarding-json-ignored",
                "detail": (
                    "gitignore excludes .relkit/onboarding.json, so the decision "
                    "record cannot be committed; a parent exclusion cannot be "
                    "undone per-file, so use '.relkit/*' plus "
                    "'!.relkit/onboarding.json'"
                ),
            }
        )
    onboard = state_path(root)
    facts["onboarding"] = onboard.is_file()
    return {
        "schema": INSPECT_SCHEMA,
        "root": str(root),
        "facts": facts,
        "findings": findings,
    }

def _impl_print_env_inspect(report: dict[str, Any]) -> None:
    facts = report.get("facts") or {}
    print("env.inspect facts")
    print(f"  relkit.json: {facts.get('relkit.json')}")
    backends = facts.get("backends") or {}
    if backends:
        print("  backends: " + ", ".join(f"{k}={v}" for k, v in backends.items()))
    version = facts.get("version") or {}
    print(
        "  VERSION.json="
        + str(version.get("VERSION.json"))
        + " VERSION="
        + str(version.get("VERSION"))
    )
    lock = facts.get("lock")
    if lock:
        print(f"  lock: {lock}")
    print(f"  onboarding.json: {facts.get('onboarding')}")
    findings = report.get("findings") or []
    if not findings:
        print("  findings: none")
        return
    print("  findings:")
    for item in findings:
        print(f"    [{item['severity']}] {item['code']}: {item['detail']}")

def _impl_apply_env_inspect(root: Path, state: dict[str, Any], report: dict[str, Any]) -> None:
    cache_dir(root).mkdir(parents=True, exist_ok=True)
    inspect_path(root).write_text(dump_json(report), encoding="utf-8")
    errors = [
        item for item in report.get("findings") or [] if item.get("severity") == "error"
    ]
    if errors:
        detail = "; ".join(item["detail"] for item in errors)
        set_step(state, "env.inspect", "blocked", "findings", detail)
        return
    set_step(state, "env.inspect", "verified", "clean", "no error findings")

def _impl_cmd_onboard_inspect(root: Path) -> int:
    state = load_state(root)
    report = env_inspect_report(root)
    apply_env_inspect(root, state, report)
    save_state(root, state)
    print_env_inspect(report)
    errors = [
        item for item in report.get("findings") or [] if item.get("severity") == "error"
    ]
    if errors:
        codes = sorted({str(item.get("code") or "env-inspect-blocked") for item in errors})
        raise Fail(
            "env.inspect blocked; clean error findings then rerun onboard inspect",
            code=codes[0] if len(codes) == 1 else "env-inspect-blocked",
        )
    print("env.inspect verified")
    return 0

def _impl_append_ops_journal(root: Path, argv: Sequence[str], error: Fail) -> None:
    if error.code == "unclassified":
        return
    cache_dir(root).mkdir(parents=True, exist_ok=True)
    entry = {
        "schema": OPS_JOURNAL_SCHEMA,
        "ts": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "cmd": " ".join(str(item) for item in argv),
        "code": error.code,
        "message": redact_text(str(error)),
    }
    with ops_journal_path(root).open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(entry, ensure_ascii=False) + "\n")

def _impl_load_ops_journal(root: Path) -> list[dict[str, Any]]:
    path = ops_journal_path(root)
    if not path.is_file():
        return []
    rows: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(item, dict):
            rows.append(item)
    return rows

_IMPLEMENTATIONS = {
    "inspect_path": _impl_inspect_path,
    "inventory_path": _impl_inventory_path,
    "ops_journal_path": _impl_ops_journal_path,
    "onboarding_ignored": _impl_onboarding_ignored,
    "upstream_latest_release": _impl_upstream_latest_release,
    "lock_currency": _impl_lock_currency,
    "env_inspect_report": _impl_env_inspect_report,
    "print_env_inspect": _impl_print_env_inspect,
    "apply_env_inspect": _impl_apply_env_inspect,
    "cmd_onboard_inspect": _impl_cmd_onboard_inspect,
    "append_ops_journal": _impl_append_ops_journal,
    "load_ops_journal": _impl_load_ops_journal,
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
