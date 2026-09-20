"""Implementation cluster: reconcile."""

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


def _impl_claim_remote_registration(
    state: dict[str, Any],
    step_id: str,
    expected: Optional[str],
    products: Sequence[str],
    *,
    note: str,
    missing: str,
    drift: list[str],
) -> None:
    """Remote listing is the evidence. A stale local status must not hide it."""
    if not expected:
        return
    if expected in products:
        set_step(state, step_id, "verified", expected, note)
    elif state["steps"][step_id]["status"] != "unanswered":
        drift.append(missing)
        state["steps"][step_id]["status"] = "drift"

def _impl_reconcile_signing_profile(
    state: dict[str, Any],
    host: str,
    config_path: str,
    product: str,
    drift: list[str],
    expected: Optional[dict[str, Any]] = None,
) -> bool:
    """The machine publish profile names the key id; the key itself never leaves the host."""
    profile = f"{Path(config_path).parent.as_posix()}/products/{product}.json"
    try:
        raw = ssh_run(host, ["sudo", "cat", profile]).stdout
    except Fail:
        return False
    try:
        profile_data = json.loads(raw)
    except json.JSONDecodeError:
        drift.append(f"{profile} is not JSON")
        return False
    if not isinstance(profile_data, dict):
        drift.append(f"{profile} is not a JSON object")
        return False
    if expected is not None and profile_data != expected:
        drift.append(f"{profile} does not match the provisioned publish profile")
        return False
    signing = profile_data.get("signing") or {}
    remote_key = str(signing.get("keyId") or "").strip()
    if not remote_key:
        drift.append(f"{profile} has no signing.keyId")
        return False
    local_key = str(state["steps"]["signing.keys"].get("value") or "").strip()
    if local_key and local_key != remote_key:
        drift.append(f"{profile} signs with {remote_key}; local state says {local_key}")
        state["steps"]["signing.keys"]["status"] = "drift"
        return False
    set_step(state, "signing.keys", "verified", remote_key, "publish profile on agent host")
    return True

def _impl_updater_sidecar_name() -> str:
    target = "windows-amd64" if os.name == "nt" else "linux-amd64"
    return BY_NAME["updater"].install_name(target)

def _impl_reconcile_sidecar_layout(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    src = root / "tools" / "bin" / updater_sidecar_name()
    if not src.is_file():
        return

    config_path = root / "relkit.json"
    if config_path.is_file():
        try:
            config = load_json(config_path)
        except (OSError, json.JSONDecodeError, TypeError) as error:
            drift.append(f"cannot read relkit.json sidecar config: {error}")
            state["steps"]["sidecar.layout"]["status"] = "drift"
            return
        sidecar = config.get("sidecar") or {}
        if not isinstance(sidecar, dict):
            drift.append("relkit.json sidecar must be an object")
            state["steps"]["sidecar.layout"]["status"] = "drift"
            return
        pack_script = sidecar.get("packScript")
        if pack_script is not None:
            if not isinstance(pack_script, str) or not pack_script.strip():
                drift.append("relkit.json sidecar.packScript must be a non-empty string")
                state["steps"]["sidecar.layout"]["status"] = "drift"
                return
            pack = root / pack_script
            if not pack.is_file():
                drift.append(f"sidecar.packScript is missing: {pack.as_posix()}")
                state["steps"]["sidecar.layout"]["status"] = "drift"
                return
            text = pack.read_text(encoding="utf-8")
            if "relkit-updater" not in text or "tools/bin" not in text:
                drift.append(
                    "sidecar.packScript must reference relkit-updater and tools/bin: "
                    f"{pack.as_posix()}"
                )
                state["steps"]["sidecar.layout"]["status"] = "drift"
                return

    set_step(
        state,
        "sidecar.layout",
        "verified",
        "tools/bin",
        f"lock-pinned updater present: {src.as_posix()}",
    )

def _impl_github_release_workflow(root: Path) -> Optional[Path]:
    workflows = root / ".github" / "workflows"
    if not workflows.is_dir():
        return None
    for path in sorted(workflows.glob("*.yml")) + sorted(workflows.glob("*.yaml")):
        try:
            text = path.read_text(encoding="utf-8")
        except OSError:
            continue
        if github_workflow_publishes_relkit(text):
            return path
    return None

def _impl_github_workflow_publishes_relkit(text: str) -> bool:
    if "relkit_host.py" in text and (
        "release --execute" in text or "RELKIT_RELEASE_VIA_CI" in text
    ):
        return True
    if "relkit_host.py" in text and "install" in text and (
        "cas-put" in text or " stage " in text or "stage " in text
    ):
        return True
    return False

def _impl_reconcile_pack_ci(root: Path, state: dict[str, Any]) -> None:
    workflow = github_release_workflow(root)
    if workflow is not None:
        set_step(
            state,
            "pack.ci",
            "confirmed",
            workflow.relative_to(root).as_posix(),
            "GitHub Actions publishes lock-pinned relkit artifacts; token stays in CI secrets",
        )
        return
    if state["steps"]["pack.ci"]["status"] == "unanswered":
        return
    yaml_dev = root / "ci" / "build_dev.yaml"
    yaml_stable = root / "ci" / "build_stable.yaml"
    win_release = root / "scripts" / "ci_win_release.cmd"
    config_path = root / "relkit.json"
    if not (yaml_dev.is_file() and yaml_stable.is_file() and win_release.is_file()):
        return
    try:
        config = load_json(config_path) if config_path.is_file() else {}
    except (OSError, json.JSONDecodeError):
        return
    release = config.get("release") if isinstance(config, dict) else None
    pack_script = (
        str(release.get("packScript") or "").strip()
        if isinstance(release, dict)
        else ""
    )
    if not pack_script:
        return
    text = win_release.read_text(encoding="utf-8")
    if "relkit_host.py" not in text or "ci release" not in text:
        return
    pack_path = root / pack_script
    if not pack_path.is_file():
        return
    set_step(
        state,
        "pack.ci",
        "confirmed",
        "ci/build_dev.yaml,ci/build_stable.yaml",
        "BK-CI PAC; entry relkit_host.py ci release; packScript declared in relkit.json",
    )

def _impl_reconcile_fake_stage(root: Path, state: dict[str, Any]) -> None:
    if state["steps"]["fake.release"]["status"] == "unanswered":
        return
    # fake.release proves the stage -> simulate wiring once. Its dummy staged
    # tree is disposable cache and may be replaced by a newer real release.
    # A different live staged version is therefore not evidence that wiring
    # regressed, and must not demote a verified gate.
    if state["steps"]["fake.release"]["status"] == "verified":
        return
    staged = cache_dir(root) / "staged"
    if not staged.is_dir():
        return
    preferred = str(state["steps"]["fake.release"].get("value") or "")
    candidates = [
        path.name
        for path in sorted(staged.iterdir())
        if path.is_dir() and (path / "staged.pb").is_file()
    ]
    if not candidates:
        return
    version = preferred if preferred in candidates else candidates[-1]
    set_step(
        state,
        "fake.release",
        "applied",
        version,
        "stage present; publish waits for pack.ci",
    )

def _impl_clear_stale_staged_trees(root: Path, current_version: str) -> list[str]:
    """Delete disposable staged caches that are not the release being published."""
    staged = cache_dir(root) / "staged"
    if not staged.is_dir():
        return []
    removed: list[str] = []
    for path in sorted(staged.iterdir()):
        if not path.is_dir() or path.name == current_version:
            continue
        shutil.rmtree(path)
        removed.append(path.name)
    return removed

def _impl_reconcile(root: Path, state: dict[str, Any], *, write: bool) -> dict[str, Any]:
    drift: list[str] = []
    unconfirmed: list[str] = []
    via_ci = os.environ.get("RELKIT_RELEASE_VIA_CI") == "1"
    topology = publish_topology(root)
    remote_relevant = topology.get("mode") != "direct"
    serve = state.get("serve") or {}
    host = serve.get("sshHost")
    port = serve.get("sshPort") or ssh_host_port(host or "")
    config_dir = serve.get("configDir") or DEFAULT_SERVE_DIR
    if via_ci:
        # CI 构建机到不了发布机的 SSH 端口；真发走 agent HTTP + RELKIT_UPLOAD_TOKEN。
        print("relkit: CI publish skips SSH probes to the serve/agent host")
    elif host and remote_relevant:
        try:
            version = ssh_run(host, [SERVE_BIN, "-version"], port=port).stdout.strip()
            serve["remoteVersion"] = version
            serve["sshPort"] = port
            listed = ssh_run(
                host,
                ["sudo", SERVE_BIN, "init", "-out", str(config_dir), "-list-products"],
                port=port,
            ).stdout
            products = parse_list_products(listed)
            expected = None
            try:
                expected = product_id(state)
            except Fail:
                expected = None
            local = load_local(root)
            local["evidence"]["serve.list-products"] = redact_text(listed)
            local["evidence"]["serve.version"] = version
            save_local(root, local)
            # Reaching the unit through this Host and config dir is the evidence.
            set_step(state, "ssh.host", "verified", host, f"{ssh_target(host, port)} {version}")
            set_step(state, "ssh.config_dir", "verified", str(config_dir), "unit answered here")
            claim_remote_registration(
                state,
                "serve.register",
                expected,
                products,
                note="listed on remote",
                missing=f"serve {host} does not list {expected}; local state says registered",
                drift=drift,
            )
        except Fail as error:
            unconfirmed.append(str(error))
    elif not via_ci and remote_relevant and state["steps"]["serve.register"]["status"] in (
        "applied",
        "verified",
    ):
        unconfirmed.append("serve.register is applied but ssh.host is empty")

    agent = state.get("agent") or {}
    agent_host = agent.get("sshHost") or host
    agent_port = agent.get("sshPort") or ssh_host_port(agent_host or "")
    config_path = agent.get("configPath") or DEFAULT_AGENT_CONFIG
    if agent_host and not via_ci and remote_relevant:
        try:
            version = ssh_run(
                agent_host, [AGENT_BIN, "-version"], port=agent_port
            ).stdout.strip()
            agent["remoteVersion"] = version
            agent["sshPort"] = agent_port
            listed = ssh_run(
                agent_host,
                ["sudo", AGENT_BIN, "init", "-config", str(config_path), "-list-products"],
                port=agent_port,
            ).stdout
            products = parse_agent_products(listed)
            local = load_local(root)
            local["evidence"]["agent.list-products"] = redact_text(listed)
            local["evidence"]["agent.version"] = version
            save_local(root, local)
            expected = None
            try:
                expected = product_id(state)
            except Fail:
                expected = None
            claim_remote_registration(
                state,
                "agent.register",
                expected,
                products,
                note="listed on remote",
                missing=f"agent {agent_host} does not list {expected}; local state says registered",
                drift=drift,
            )
            if expected and expected in products:
                reconcile_signing_profile(state, agent_host, config_path, expected, drift)
        except Fail as error:
            unconfirmed.append(str(error))

    lock_path = root / "scripts" / "relkit.lock.json"
    if lock_path.is_file():
        try:
            lock = load_json(lock_path)
            if lock.get("schema") != LOCK_SCHEMA:
                drift.append(f"{lock_path} is not {LOCK_SCHEMA}")
            pinned = str(lock.get("hostScriptsSha256") or "").lower()
            actual = tree_sha256(root / "scripts" / "host")
            if not re.fullmatch(r"[0-9a-f]{64}", pinned):
                drift.append("lock has no valid hostScriptsSha256")
            elif pinned != actual:
                drift.append("scripts/host tree does not match lock hostScriptsSha256")
            else:
                set_step(state, "consume.lock", "verified", lock.get("release"), "lock present")
        except (OSError, json.JSONDecodeError) as error:
            unconfirmed.append(f"cannot read lock: {error}")

    apply_topology_to_state(root, state)
    reconcile_local_signing(root, state)
    reconcile_sidecar_layout(root, state, drift)
    reconcile_pack_ci(root, state)
    reconcile_fake_stage(root, state)
    run_gates(root, state, drift)

    if write:
        save_state(root, state)
    report = dict(state)
    report["drift"] = drift
    report["unconfirmed"] = unconfirmed
    return report

_IMPLEMENTATIONS = {
    "claim_remote_registration": _impl_claim_remote_registration,
    "reconcile_signing_profile": _impl_reconcile_signing_profile,
    "updater_sidecar_name": _impl_updater_sidecar_name,
    "reconcile_sidecar_layout": _impl_reconcile_sidecar_layout,
    "github_release_workflow": _impl_github_release_workflow,
    "github_workflow_publishes_relkit": _impl_github_workflow_publishes_relkit,
    "reconcile_pack_ci": _impl_reconcile_pack_ci,
    "reconcile_fake_stage": _impl_reconcile_fake_stage,
    "clear_stale_staged_trees": _impl_clear_stale_staged_trees,
    "reconcile": _impl_reconcile,
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
