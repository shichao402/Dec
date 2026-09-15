"""Implementation cluster: remote."""

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


def _impl_ssh_path_exists(host: str, remote_path: str) -> bool:
    if not remote_path.startswith("/"):
        raise Fail("remote path must be absolute")
    script = (
        f"if sudo test -f {shlex.quote(remote_path)}; then echo exists; else echo missing; fi"
    )
    out = ssh_run(host, ["bash", "-lc", script]).stdout.strip().splitlines()
    return bool(out) and out[-1] == "exists"

def _impl_agent_profile_path(config_path: str, product: str) -> str:
    return f"{Path(config_path).parent.as_posix()}/products/{product}.json"

def _impl_rewrite_agent_backend_url(url: str) -> str:
    parsed = urlparse(url)
    if parsed.hostname != AGENT_ORIGIN_HOST:
        return url
    if parsed.scheme == "http" and parsed.port == 8080:
        return url
    return urlunparse(
        ("http", AGENT_ORIGIN_NETLOC, parsed.path, parsed.params, parsed.query, parsed.fragment)
    )

def _impl_apply_agent_backend_urls(backend: dict[str, Any]) -> None:
    """Clients download via baseUrl; the agent uploads via origin uploadUrl."""
    base = backend.get("baseUrl")
    if not isinstance(base, str) or not base:
        return
    origin = rewrite_agent_backend_url(base)
    upload = backend.get("uploadUrl")
    if isinstance(upload, str) and upload:
        backend["uploadUrl"] = rewrite_agent_backend_url(upload)
    else:
        backend["uploadUrl"] = origin

def _impl_extract_publish_profile(machine: dict[str, Any]) -> dict[str, Any]:
    signing = dict(machine.get("signing") or {})
    profile_signing: dict[str, Any] = {
        "keyId": signing.get("keyId"),
        "privateKeyPath": signing.get("privateKeyPath"),
    }
    if signing.get("privateKeyEnv"):
        profile_signing["privateKeyEnv"] = signing["privateKeyEnv"]
    # Site blurbs travel in the staged release-policy.json. A profile carries
    # only the Makers token env name; anything else here is an unknown field
    # the agent refuses to parse.
    profile_site: dict[str, Any] = {}
    token_env = ((machine.get("site") or {}).get("makers") or {}).get("tokenEnv")
    if token_env:
        profile_site["makers"] = {"tokenEnv": token_env}
    profile: dict[str, Any] = {
        "product": machine.get("product"),
        "signing": profile_signing,
        "backends": machine.get("backends") or {},
        "publishTo": list(machine.get("publishTo") or []),
    }
    directory_publish_to = (machine.get("directory") or {}).get("publishTo")
    if directory_publish_to:
        profile["directory"] = {"publishTo": list(directory_publish_to)}
    if profile_site:
        profile["site"] = profile_site
    return profile

def _impl_machine_publish_config(root: Path, product: str, key_id: str, private_relpath: str) -> dict[str, Any]:
    config_path = root / "relkit.json"
    if not config_path.is_file():
        raise Fail(f"missing {config_path}")
    cfg = load_json(config_path)
    backends = json.loads(json.dumps(cfg.get("backends") or {}))
    if not backends:
        raise Fail("relkit.json has no backends for the machine publish profile")
    for backend in backends.values():
        if isinstance(backend, dict):
            apply_agent_backend_urls(backend)
    signing = dict(cfg.get("signing") or {})
    signing["keyId"] = key_id
    signing["privateKeyPath"] = private_relpath
    return {
        "product": product,
        "defaultChannel": cfg.get("defaultChannel") or "stable",
        "channels": list(cfg.get("channels") or ["stable"]),
        "codeStrategy": cfg.get("codeStrategy") or "version-build",
        "retainVersions": cfg.get("retainVersions") or 1,
        "backends": backends,
        "publishTo": list(cfg.get("publishTo") or backends.keys()),
        "directory": {"publishTo": list((cfg.get("directory") or {}).get("publishTo") or cfg.get("publishTo") or backends.keys())},
        "signing": signing,
        "site": cfg.get("site") or {},
    }

def _impl_parse_list_products(text: str) -> list[str]:
    products: list[str] = []
    in_list = False
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("uploadTokens") or stripped.startswith("products"):
            in_list = True
            continue
        if not in_list:
            continue
        if not stripped or stripped.lower().endswith("none"):
            continue
        first = stripped.split()[0]
        if first in ("config", "operator"):
            continue
        for item in first.split(","):
            if item and item not in products:
                products.append(item)
    if not products:
        for match in re.finditer(r"^\s+([A-Za-z0-9._,-]+)\s+\S+", text, re.M):
            blob = match.group(1)
            if blob in ("config", "uploadTokens", "products", "operator"):
                continue
            for item in blob.split(","):
                if item and item not in products:
                    products.append(item)
    return products

def _impl_publish_topology(root: Path) -> dict[str, Any]:
    """Describe the configured publish path without guessing operator intent."""
    path = root / "relkit.json"
    if not path.is_file():
        return {
            "mode": "unknown",
            "publishTo": [],
            "backendTypes": {},
            "agentUrl": None,
            "tokenRequired": None,
            "serveRequired": None,
            "reason": "relkit.json is absent",
        }
    try:
        config = load_json(path)
    except (OSError, json.JSONDecodeError, TypeError) as error:
        return {
            "mode": "unknown",
            "publishTo": [],
            "backendTypes": {},
            "agentUrl": None,
            "tokenRequired": None,
            "serveRequired": None,
            "reason": f"relkit.json is unreadable: {error}",
        }
    backends = config.get("backends")
    backends = backends if isinstance(backends, dict) else {}
    publish_to = config.get("publishTo")
    selected = (
        [str(item) for item in publish_to]
        if isinstance(publish_to, list)
        else [str(item) for item in backends]
    )
    backend_types = {
        name: str((backends.get(name) or {}).get("type") or "")
        for name in selected
        if isinstance(backends.get(name), dict)
    }
    agent_url = str((config.get("agent") or {}).get("url") or "").strip() or None
    gateway_types = {"relkit-compatible", "intranet-relkit-compatible"}
    gateway_backends = [
        name for name, kind in backend_types.items() if kind in gateway_types
    ]
    if agent_url:
        mode = "agent"
        token_required: Optional[bool] = True
        serve_required: Optional[bool] = False
        reason = "relkit.json agent.url routes release through relkit-agent"
    elif gateway_backends:
        mode = "gateway" if len(gateway_backends) == len(selected) else "mixed"
        token_required = True
        serve_required = True
        reason = "publishTo includes a relkit-compatible backend"
    elif selected and len(backend_types) == len(selected):
        mode = "direct"
        token_required = False
        serve_required = False
        reason = "publishTo contains only direct backends"
    else:
        mode = "unknown"
        token_required = None
        serve_required = None
        reason = "publishTo cannot be resolved to configured backends"
    return {
        "mode": mode,
        "publishTo": selected,
        "backendTypes": backend_types,
        "agentUrl": agent_url,
        "tokenRequired": token_required,
        "serveRequired": serve_required,
        "reason": reason,
    }

def _impl__version_tuple(raw: str) -> Optional[tuple[int, int, int]]:
    match = re.search(r"(\d+)\.(\d+)\.(\d+)", raw)
    if not match:
        return None
    return tuple(int(item) for item in match.groups())

def _impl_remote_inventory(
    root: Path, state: dict[str, Any], topology: dict[str, Any]
) -> dict[str, Any]:
    """Read a configured remote without mutating it or exposing token bytes."""
    use_agent = topology.get("mode") == "agent"
    role = "agent" if use_agent else "serve"
    config = (state.get(role) or {}) if role == "agent" else (state.get("serve") or {})
    host = str(config.get("sshHost") or "")
    if use_agent and not host:
        host = str((state.get("serve") or {}).get("sshHost") or "")
    port = config.get("sshPort") or ssh_host_port(host)
    on_route = bool(
        topology.get("tokenRequired")
        and ((use_agent and topology.get("agentUrl")) or topology.get("serveRequired"))
    )
    result: dict[str, Any] = {
        "configured": bool(host),
        "onPublishRoute": on_route,
        "role": role,
        "host": host or None,
        "port": port,
        "available": False,
        "binary": AGENT_BIN if use_agent else SERVE_BIN,
        "version": None,
        "serviceActive": None,
        "serviceEnabled": None,
        "operatorTokenPresent": None,
        "products": [],
        "error": None,
    }
    if not host:
        result["error"] = "no confirmed SSH host"
        return result
    binary = AGENT_BIN if use_agent else SERVE_BIN
    unit = "relkit-agent" if use_agent else "relkit-serve"
    try:
        version = ssh_run(host, [binary, "-version"], port=port).stdout.strip()
        status = ssh_run(
            host,
            [
                "bash",
                "-lc",
                (
                    f"systemctl is-active {shlex.quote(unit)} 2>/dev/null || true; "
                    f"systemctl is-enabled {shlex.quote(unit)} 2>/dev/null || true"
                ),
            ],
            port=port,
        ).stdout.splitlines()
        if use_agent:
            config_path = str(config.get("configPath") or DEFAULT_AGENT_CONFIG)
            listed = ssh_run(
                host,
                [
                    "sudo",
                    AGENT_BIN,
                    "init",
                    "-config",
                    config_path,
                    "-list-products",
                ],
                port=port,
            ).stdout
            products = parse_agent_products(listed)
        else:
            config_dir = str(config.get("configDir") or DEFAULT_SERVE_DIR)
            listed = ssh_run(
                host,
                [
                    "sudo",
                    SERVE_BIN,
                    "init",
                    "-out",
                    config_dir,
                    "-list-products",
                ],
                port=port,
            ).stdout
            products = parse_list_products(listed)
        result.update(
            {
                "available": True,
                "version": version,
                "serviceActive": status[0].strip() if status else "unknown",
                "serviceEnabled": status[1].strip() if len(status) > 1 else "unknown",
                "operatorTokenPresent": bool(
                    re.search(r"(?mi)^\s*operator\s+\S+", listed)
                ),
                "products": products,
            }
        )
    except Fail as error:
        result["error"] = str(error)
    lock_path = root / "scripts" / "relkit.lock.json"
    if lock_path.is_file():
        try:
            lock_release = str(load_json(lock_path).get("release") or "")
        except (OSError, json.JSONDecodeError, TypeError):
            lock_release = ""
        remote_version = _version_tuple(str(result.get("version") or ""))
        locked_version = _version_tuple(lock_release)
        result["lockRelease"] = lock_release or None
        result["versionRelation"] = (
            "behind"
            if remote_version and locked_version and remote_version < locked_version
            else "current-or-newer"
            if remote_version and locked_version
            else "unknown"
        )
    return result

def _impl_decision_evidence(root: Path, state: dict[str, Any]) -> dict[str, Any]:
    topology = publish_topology(root)
    remote = remote_inventory(root, state, topology)
    implications: list[str] = []
    blocked: dict[str, str] = {}
    applicable = {step: True for step in STEP_IDS}
    if topology.get("mode") == "direct":
        implications.append(
            "release publishes directly to configured backends; serve/agent product tokens are not on this route"
        )
        for step in (
            "ssh.host",
            "ssh.config_dir",
            "token.isolation",
            "serve.register",
            "agent.register",
        ):
            applicable[step] = False
    if remote.get("available"):
        products = list(remote.get("products") or [])
        if products:
            implications.append(
                "share-with is limited to remote products: " + ", ".join(products)
            )
        else:
            implications.append(
                "the remote has no product token to inherit; operator credentials are not product tokens"
            )
        if remote.get("versionRelation") == "behind":
            message = (
                f"remote {remote.get('role')} {remote.get('version')} is behind "
                f"lock {remote.get('lockRelease')}"
            )
            implications.append(message)
            if remote.get("onPublishRoute"):
                blocked["token.isolation"] = message + "; upgrade the remote first"
    elif topology.get("tokenRequired"):
        blocked["token.isolation"] = (
            "live remote inventory is required before choosing token isolation: "
            + str(remote.get("error") or "unavailable")
        )
    return {
        "topology": topology,
        "remote": remote,
        "applicable": applicable,
        "implications": implications,
        "blockedDecisions": blocked,
    }

def _impl_parse_agent_products(text: str) -> list[str]:
    products: list[str] = []
    in_products = False
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("products"):
            in_products = True
            if stripped.lower().endswith("none"):
                return []
            continue
        if not in_products:
            continue
        if not stripped:
            continue
        name = stripped.split()[0]
        if name not in products:
            products.append(name)
    return products

def _impl_require_execute(args: argparse.Namespace, action: str) -> None:
    if not getattr(args, "execute", False):
        raise Fail(f"refusing to {action} without --execute")

def _impl_require_restart_flag(args: argparse.Namespace) -> None:
    if not getattr(args, "restart", False):
        raise Fail(
            "config changed but the process was not restarted. "
            "Pass --restart only after the user said the machine may restart."
        )

def _impl_write_secret(root: Path, token: str, *, note: Path = SECRET_NOTE) -> Path:
    path = root / note
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(f"export {TOKEN_ENV}='{token}'\n", encoding="utf-8")
    try:
        path.chmod(0o600)
    except OSError:
        pass
    local = load_local(root)
    ref = str(note).replace("\\", "/")
    if ref not in local["secretRefs"]:
        local["secretRefs"].append(ref)
    save_local(root, local)
    return path

def _impl_archive_secret(root: Path) -> None:
    path = root / SECRET_NOTE
    if not path.is_file():
        return
    archived = path.with_name(path.name + ".revoked")
    path.replace(archived)

def _impl_relkit_bin(root: Path, explicit: Optional[str] = None) -> Path:
    if explicit:
        path = Path(explicit)
        if not path.is_file():
            raise Fail(f"relkit binary not found: {path}")
        return path
    env = os.environ.get("RELKIT_BIN")
    if env:
        path = Path(env)
        if path.is_file():
            return path
    tools = root / "tools" / "bin"
    for name in ("relkit.exe", "relkit", "relkit-linux-amd64"):
        candidate = tools / name
        if candidate.is_file():
            return candidate
    found = shutil.which("relkit")
    if found:
        return Path(found)
    raise Fail("relkit CLI is not installed; run install first or pass --bin")

def _impl_cmd_status(root: Path, as_json: bool = False) -> int:
    state = load_state(root)
    report = reconcile(root, state, write=True)
    if as_json:
        print(dump_json(report))
        return 0
    print(f"schema {report['schema']}")
    print(f"product {report.get('product') or '(unset)'}")
    print("steps")
    for step_id in STEP_IDS:
        item = report["steps"][step_id]
        value = item.get("value")
        suffix = "" if value in (None, "") else f"  {value}"
        extra = ""
        if item.get("note"):
            extra = f"  ({item['note']})"
        print(f"  {step_id:18} {item['status']}{suffix}{extra}")
    write_onboarding_md(root, report)
    unresolved = next_unresolved_step(report)
    if unresolved:
        print("next")
        print_decision(root, report, unresolved)
    drift = report.get("drift") or []
    if drift:
        print("drift")
        for item in drift:
            print(f"  {item}")
    unconfirmed = report.get("unconfirmed") or []
    if unconfirmed:
        print("unconfirmed")
        for item in unconfirmed:
            print(f"  {item}")
    return 1 if drift else 0

def _impl_agent_token_path(product: str, share_with: Optional[str] = None) -> str:
    owner = (share_with or product or "").strip()
    if not owner:
        raise Fail("token owner product id is required")
    return "/etc/relkit-agent/tokens/" + owner + ".token"

def _impl_chown_serve_product_token(
    host: str,
    config_dir: str,
    product: str,
    share_with: Optional[str] = None,
) -> None:
    token_path = serve_token_path(config_dir, product, share_with)
    ssh_run(host, ["sudo", "chown", "relkit:relkit", token_path])
    ssh_run(host, ["sudo", "chmod", "0600", token_path])
    print(f"chown relkit:relkit {token_path}")

def _impl_cmd_serve_list(root: Path) -> int:
    state = load_state(root)
    host = (state.get("serve") or {}).get("sshHost")
    config_dir = (state.get("serve") or {}).get("configDir") or DEFAULT_SERVE_DIR
    if not host:
        print("no ssh.host recorded")
        print(ssh_host_recommend())
        print("set it with: onboard set ssh.host <Host>")
        return 1
    out = ssh_run(
        host,
        ["sudo", SERVE_BIN, "init", "-out", str(config_dir), "-list-products"],
    )
    print(redact_text(out.stdout))
    return 0

def _impl_cmd_serve_add(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "serve add")
    state = load_state(root)
    product = args.product or product_id(state)
    host = args.host or (state.get("serve") or {}).get("sshHost")
    if not host:
        raise Fail("pass --host or onboard set ssh.host first")
    config_dir = args.config_dir or (state.get("serve") or {}).get("configDir") or DEFAULT_SERVE_DIR
    share_with = args.share_with or (state.get("serve") or {}).get("shareWith")
    remote = [
        "sudo",
        SERVE_BIN,
        "init",
        "-out",
        str(config_dir),
        "-product",
        product,
    ]
    if share_with:
        remote.extend(["-share-with", share_with])
    print(f"SSH {ssh_target(host)} (config {config_dir}) product {product}")
    result = ssh_run(host, remote)
    stdout = result.stdout or ""
    token = extract_export(TOKEN_ENV, stdout) or extract_export("RELKIT_SERVE_TOKEN", stdout)
    print(redact_text(stdout))
    if token:
        write_secret(root, token)
        print(f"token written to project note {SECRET_NOTE.as_posix()} (plaintext not printed)")
    elif share_with:
        print("share-with does not print a token")
    else:
        raise Fail("init did not print a token; not guessing")
    chown_serve_product_token(host, config_dir, product, share_with)
    state["product"] = product
    state["serve"]["sshHost"] = host
    state["serve"]["sshPort"] = ssh_host_port(host)
    state["serve"]["configDir"] = config_dir
    state["serve"]["shareWith"] = share_with
    set_step(state, "ssh.host", "verified", host, ssh_target(host))
    set_step(state, "ssh.config_dir", "verified", config_dir)
    set_step(state, "product.id", "verified", product)
    set_step(
        state,
        "token.isolation",
        "verified",
        f"share-with:{share_with}" if share_with else "exclusive",
    )
    set_step(state, "serve.register", "applied", product, "waiting for restart" if not args.restart else "")
    save_state(root, state)
    if args.restart:
        ssh_run(host, ["sudo", "systemctl", "restart", "relkit-serve"])
        set_step(state, "serve.register", "applied", product, "restarted")
        save_state(root, state)
        print("relkit-serve restarted")
    else:
        print("not restarted; old tokens still work until --restart")
    return 0

def _impl_cmd_serve_restart(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "serve restart")
    require_restart_flag(args)
    state = load_state(root)
    host = args.host or (state.get("serve") or {}).get("sshHost")
    if not host:
        raise Fail("pass --host or onboard set ssh.host first")
    product = None
    try:
        product = product_id(state)
    except Fail:
        product = state["steps"]["serve.register"].get("value")
    config_dir = (state.get("serve") or {}).get("configDir") or DEFAULT_SERVE_DIR
    share_with = (state.get("serve") or {}).get("shareWith")
    if product:
        chown_serve_product_token(host, str(config_dir), str(product), share_with)
    ssh_run(host, ["sudo", "systemctl", "reset-failed", "relkit-serve"])
    ssh_run(host, ["sudo", "systemctl", "restart", "relkit-serve"])
    if product:
        set_step(state, "serve.register", "applied", product, "restarted")
        save_state(root, state)
    print("relkit-serve restarted")
    return 0

def _impl_cmd_agent_restart(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "agent restart")
    require_restart_flag(args)
    state = load_state(root)
    host = args.host or (state.get("agent") or {}).get("sshHost") or (state.get("serve") or {}).get("sshHost")
    if not host:
        raise Fail("pass --host or onboard set ssh.host first")
    product = args.product or None
    if not product:
        try:
            product = product_id(state)
        except Fail:
            product = state["steps"]["agent.register"].get("value")
    if product:
        root_dir = args.root_path or f"/srv/relkit/{product}"
        share_with = (state.get("serve") or {}).get("shareWith") or getattr(
            args, "share_with", None
        )
        token_path = agent_token_path(str(product), share_with)
        ssh_run(host, ["sudo", "chown", "-R", "relkit:relkit", str(root_dir)])
        ssh_run(host, ["sudo", "chown", "relkit:relkit", token_path])
        print(f"chown relkit:relkit {root_dir} and {token_path}")
    ssh_run(host, ["sudo", "systemctl", "restart", "relkit-agent"])
    if product:
        set_step(state, "agent.register", "applied", product, "restarted")
        save_state(root, state)
    print("relkit-agent restarted")
    return 0

def _impl_cmd_serve_rotate(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "serve rotate")
    state = load_state(root)
    product = args.product or product_id(state)
    host = args.host or (state.get("serve") or {}).get("sshHost")
    config_dir = args.config_dir or (state.get("serve") or {}).get("configDir") or DEFAULT_SERVE_DIR
    if not host:
        raise Fail("pass --host or onboard set ssh.host first")
    result = ssh_run(
        host,
        [
            "sudo",
            SERVE_BIN,
            "init",
            "-out",
            str(config_dir),
            "-product",
            product,
            "-token-only",
        ],
    )
    stdout = result.stdout or ""
    token = extract_export(TOKEN_ENV, stdout) or extract_export("RELKIT_SERVE_TOKEN", stdout)
    print(redact_text(stdout))
    if not token:
        raise Fail("rotate did not print a token")
    write_secret(root, token)
    print(f"new token written to {SECRET_NOTE.as_posix()} (plaintext not printed)")
    print("deliver this to publishers before --restart")
    if args.restart:
        ssh_run(host, ["sudo", "systemctl", "restart", "relkit-serve"])
        print("relkit-serve restarted; previous token is invalid")
    else:
        print("not restarted; previous token still works")
    return 0

def _impl_cmd_serve_remove(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "serve remove")
    state = load_state(root)
    product = args.product or product_id(state)
    host = args.host or (state.get("serve") or {}).get("sshHost")
    config_dir = args.config_dir or (state.get("serve") or {}).get("configDir") or DEFAULT_SERVE_DIR
    if not host:
        raise Fail("pass --host or onboard set ssh.host first")
    if args.restart:
        ssh_run(host, ["sudo", "systemctl", "restart", "relkit-serve"])
    result = ssh_run(
        host,
        [
            "sudo",
            SERVE_BIN,
            "init",
            "-out",
            str(config_dir),
            "-product",
            product,
            "-remove",
        ],
    )
    print(redact_text(result.stdout or ""))
    archive_secret(root)
    set_step(state, "serve.register", "stale", product, "removed remotely")
    save_state(root, state)
    print(f"archived local note {SECRET_NOTE.as_posix()}.revoked if it existed")
    if not args.restart:
        print("pass --restart as soon as possible so the revoked id stops being writable")
    return 0

def _impl_cmd_agent_list(root: Path) -> int:
    state = load_state(root)
    host = (state.get("agent") or {}).get("sshHost") or (state.get("serve") or {}).get("sshHost")
    config_path = (state.get("agent") or {}).get("configPath") or DEFAULT_AGENT_CONFIG
    if not host:
        raise Fail("no agent/serve ssh host recorded")
    out = ssh_run(
        host,
        ["sudo", AGENT_BIN, "init", "-config", str(config_path), "-list-products"],
    )
    print(redact_text(out.stdout))
    return 0

def _impl_cmd_agent_add(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "agent add")
    state = load_state(root)
    product = args.product or product_id(state)
    host = args.host or (state.get("agent") or {}).get("sshHost") or (state.get("serve") or {}).get("sshHost")
    config_path = args.config or (state.get("agent") or {}).get("configPath") or DEFAULT_AGENT_CONFIG
    if not host:
        raise Fail("pass --host")
    share_with = args.share_with or (state.get("serve") or {}).get("shareWith")
    remote = [
        "sudo",
        AGENT_BIN,
        "init",
        "-config",
        str(config_path),
        "-product",
        product,
    ]
    if share_with:
        remote.extend(["-share-with", share_with])
    if args.root_path:
        remote.extend(["-root", args.root_path])
    result = ssh_run(host, remote)
    print(redact_text(result.stdout or ""))
    token = extract_export(TOKEN_ENV, result.stdout or "")
    if token:
        write_secret(root, token, note=AGENT_SECRET_NOTE)
        print(f"agent token written to {AGENT_SECRET_NOTE.as_posix()} (plaintext not printed)")
    elif share_with:
        print("share-with does not print a token")
    state["agent"]["sshHost"] = host
    state["agent"]["sshPort"] = ssh_host_port(host)
    state["agent"]["configPath"] = config_path
    set_step(state, "agent.register", "applied", product)
    save_state(root, state)
    if args.restart:
        ssh_run(host, ["sudo", "systemctl", "restart", "relkit-agent"])
        print("relkit-agent restarted")
    else:
        print("not restarted; pass --restart after the user allows it")
    return 0

def _impl_cmd_agent_provision(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "agent provision")
    state = load_state(root)
    product = args.product or product_id(state)
    host_name = args.host or (state.get("agent") or {}).get("sshHost") or (state.get("serve") or {}).get("sshHost")
    config_path = args.config or (state.get("agent") or {}).get("configPath") or DEFAULT_AGENT_CONFIG
    product_root = args.root_path or f"/srv/relkit/{product}"
    if not host_name:
        raise Fail("pass --host")
    key_id = str(state["steps"]["signing.keys"].get("value") or "k1")
    private_rel = f".relkit-keys/{key_id}.private.pb"
    private_local = root / private_rel
    if not private_local.is_file():
        raise Fail(f"missing {private_rel}; generate keys on this repo first")
    machine = machine_publish_config(root, product, key_id, private_rel)
    publish_profile = extract_publish_profile(machine)
    profile_path = agent_profile_path(str(config_path), product)
    if ssh_path_exists(host_name, profile_path):
        ssh_write(host_name, profile_path, dump_json(publish_profile).encode("utf-8"))
        ssh_run(host_name, ["sudo", "chmod", "644", profile_path])
        ssh_run(host_name, ["sudo", "chown", "relkit:relkit", profile_path])
        print(f"updated {profile_path}")
    else:
        keys_dir = f"{product_root}/.relkit-keys"
        remote_json = f"{product_root}/relkit.json"
        remote_private = f"{product_root}/{private_rel}"
        ssh_run(host_name, ["sudo", "mkdir", "-p", keys_dir])
        ssh_write(host_name, remote_json, dump_json(machine).encode("utf-8"))
        ssh_write(host_name, remote_private, private_local.read_bytes())
        ssh_run(host_name, ["sudo", "chmod", "640", remote_json])
        ssh_run(host_name, ["sudo", "chmod", "600", remote_private])
        ssh_run(host_name, ["sudo", "chown", "-R", "relkit:relkit", product_root])
        result = ssh_run(
            host_name,
            [
                "sudo",
                AGENT_BIN,
                "init",
                "-config",
                str(config_path),
                "-product",
                product,
                "-migrate-profile",
            ],
        )
        print(redact_text(result.stdout or "").strip())
        print(f"wrote {private_rel} under {product_root} (bytes not printed)")
    drift: list[str] = []
    if not reconcile_signing_profile(
        state,
        host_name,
        config_path,
        product,
        drift,
        expected=publish_profile,
    ):
        detail = "\n  ".join(drift) if drift else "cannot read the remote profile back"
        raise Fail("agent provision wrote a profile but could not verify it:\n  " + detail)
    save_state(root, state)
    if args.restart:
        ssh_run(host_name, ["sudo", "systemctl", "restart", "relkit-agent"])
        print("relkit-agent restarted")
    else:
        print("not restarted; pass --restart after the user allows it")
    return 0

def _impl_cmd_agent_remove(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "agent remove")
    state = load_state(root)
    product = args.product or product_id(state)
    host = args.host or (state.get("agent") or {}).get("sshHost") or (state.get("serve") or {}).get("sshHost")
    config_path = args.config or (state.get("agent") or {}).get("configPath") or DEFAULT_AGENT_CONFIG
    if not host:
        raise Fail("pass --host")
    result = ssh_run(
        host,
        [
            "sudo",
            AGENT_BIN,
            "init",
            "-config",
            str(config_path),
            "-product",
            product,
            "-remove",
        ],
    )
    print(redact_text(result.stdout or ""))
    set_step(state, "agent.register", "stale", product, "removed remotely")
    save_state(root, state)
    return 0

def _impl_run_relkit(root: Path, binary: Path, argv: Sequence[str]) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        [str(binary), *argv],
        cwd=str(root),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    text = redact_text((result.stdout or "") + (result.stderr or ""))
    if text.strip():
        print(text.strip())
    if result.returncode != 0:
        raise Fail(text.strip() or f"relkit {' '.join(argv)} failed")
    return result

_IMPLEMENTATIONS = {
    "ssh_path_exists": _impl_ssh_path_exists,
    "agent_profile_path": _impl_agent_profile_path,
    "rewrite_agent_backend_url": _impl_rewrite_agent_backend_url,
    "apply_agent_backend_urls": _impl_apply_agent_backend_urls,
    "extract_publish_profile": _impl_extract_publish_profile,
    "machine_publish_config": _impl_machine_publish_config,
    "parse_list_products": _impl_parse_list_products,
    "publish_topology": _impl_publish_topology,
    "_version_tuple": _impl__version_tuple,
    "remote_inventory": _impl_remote_inventory,
    "decision_evidence": _impl_decision_evidence,
    "parse_agent_products": _impl_parse_agent_products,
    "require_execute": _impl_require_execute,
    "require_restart_flag": _impl_require_restart_flag,
    "write_secret": _impl_write_secret,
    "archive_secret": _impl_archive_secret,
    "relkit_bin": _impl_relkit_bin,
    "cmd_status": _impl_cmd_status,
    "agent_token_path": _impl_agent_token_path,
    "chown_serve_product_token": _impl_chown_serve_product_token,
    "cmd_serve_list": _impl_cmd_serve_list,
    "cmd_serve_add": _impl_cmd_serve_add,
    "cmd_serve_restart": _impl_cmd_serve_restart,
    "cmd_agent_restart": _impl_cmd_agent_restart,
    "cmd_serve_rotate": _impl_cmd_serve_rotate,
    "cmd_serve_remove": _impl_cmd_serve_remove,
    "cmd_agent_list": _impl_cmd_agent_list,
    "cmd_agent_add": _impl_cmd_agent_add,
    "cmd_agent_provision": _impl_cmd_agent_provision,
    "cmd_agent_remove": _impl_cmd_agent_remove,
    "run_relkit": _impl_run_relkit,
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
