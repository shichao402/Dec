#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Product-repo ops gate for relkit.

Copy the whole ``scripts/host/`` tree byte-for-byte. This file is the only
command people and agents run in a product repo. It never writes serve/agent
JSON; machine config is owned by the binaries' ``init`` (internal).
"""

from __future__ import annotations

import argparse
import fnmatch
import hashlib
import inspect
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

SCHEMA = "relkit.onboarding/1"
LOCAL_SCHEMA = "relkit.onboarding.local/1"
LOCK_SCHEMA = "relkit.consume/2"
DEFAULT_SERVE_DIR = "/etc/relkit-serve"
DEFAULT_AGENT_CONFIG = "/etc/relkit-agent/relkit-agent.json"
AGENT_ORIGIN_HOST = "update.devcloud.woa.com"
AGENT_ORIGIN_NETLOC = "update.devcloud.woa.com:8080"
GITHUB_REPO = os.environ.get("RELKIT_RELEASE_REPO", "shichao402/relkit")
TOKEN_ENV = "RELKIT_UPLOAD_TOKEN"
SECRET_NOTE = Path(".secrets") / "project" / "relkit-upload-token"
AGENT_SECRET_NOTE = Path(".secrets") / "project" / "relkit-agent-upload-token"
GITIGNORE_RELKIT = (
    ".relkit/cache/",
    ".relkit-keys/*.private.pb",
    # install puts python entrypoints in scripts/host; running them leaves
    # bytecode caches that would otherwise read as product repo changes.
    "__pycache__/",
)
PUBLISH_PROTOCOL_FALLBACK = 2
UPDATER_IPC_FALLBACK = 1
UPDATER_PROCESS_VALUES = ("rust", "node", "dart", "go", "other")
UPDATER_PROCESS_EXPLAIN = (
    "谁调用 Updater.open。只记封闭词，不要另写决策备忘。"
    " rust/node/dart/go：该语言 facade 直连 sidecar。"
    " 给 WebView 只用对应 SDK 的 JSON 投影（Rust：check_result_to_json）。"
    " 禁止再开工一份 CheckResult / UpdateAvailable 手写 DTO。"
    " 缺 releaseNotesMarkdown 这类键是投影器或手写层 bug，不是旧 updater；"
    " 禁止 serde(default) / Option 吞掉。"
    " other：先口头说明再记；说明里若出现第二套 JSON 形状，当场拦住。"
)

STATUSES = (
    "unanswered",
    "confirmed",
    "applied",
    "verified",
    "blocked",
    "stale",
    "drift",
)

STEP_IDS = (
    "repo.root",
    "env.inspect",
    "product.id",
    "updater.process",
    "channel.ssot",
    "backend.kind",
    "ssh.host",
    "ssh.config_dir",
    "token.isolation",
    "serve.register",
    "agent.register",
    "signing.keys",
    "consume.lock",
    "sidecar.layout",
    "fake.release",
    "pack.ci",
    "ops.retrospect",
)

REQUIRED_FOR_RELEASE = STEP_IDS
DECISION_STEPS = (
    "repo.root",
    "env.inspect",
    "product.id",
    "updater.process",
    "channel.ssot",
    "backend.kind",
    "ssh.host",
    "ssh.config_dir",
    "token.isolation",
)
ACTION_STEPS = tuple(step for step in STEP_IDS if step not in DECISION_STEPS)
STALE_BACKEND_TYPES = frozenset({"http-put", "local"})
INSPECT_SCHEMA = "relkit.inspect/1"
OPS_JOURNAL_SCHEMA = "relkit.ops-journal/1"
QUESTION_BATCH_SCHEMA = "relkit.onboarding-questions/1"
ANSWER_BATCH_SCHEMA = "relkit.onboarding-answers/1"
ONBOARD_INTENTS = ("fresh", "reconfigure", "upgrade")
BATCH_DECISION_STEPS = tuple(
    step for step in DECISION_STEPS if step not in ("repo.root", "env.inspect")
)
DIGESTED_ISSUE_CODES = frozenset(
    {
        "share-with-token-chown",
        "list-products-comma",
        "updater-process-rust-shell",
        "onboarding-gitignore-split",
        "sidecar-mjs-hardcode",
        "fake-stage-version-demote",
        "stale-backend-type",
        "env-inspect-blocked",
        "env-inspect-set-refused",
        "env-inspect-required",
        "fake-verify-missing-stage",
        "fake-verify-no-version",
        "onboard-batch-intent-invalid",
        "onboard-batch-intent-conflict",
        "onboard-answer-step-invalid",
        "onboard-answer-shape-invalid",
        "onboard-answer-unreadable",
        "onboard-answer-schema-invalid",
        "onboard-answer-revision-conflict",
        "onboard-answer-conflict",
        "lock-v1-no-migration-path",
        "v2-no-go-sdk-artifact",
        "onboarding-json-ignored",
        "sdk-readonly-backup-rmtree",
        "host-scripts-pycache-untracked",
    }
)

EXPLAIN_TEXTS = {
    "repo.root": "Confirm the product repository root and languages. No mutation.",
    "env.inspect": (
        "Dump existing wiring before any product decision. "
        "Re-run onboard inspect after cleanup; error findings block product.id."
    ),
    "product.id": "Stable product id used by serve/agent tokens and relkit.json.",
    "updater.process": UPDATER_PROCESS_EXPLAIN,
    "channel.ssot": "VERSION.json is the version SSOT; host.py/CI call relkit version, people do not.",
    "backend.kind": "Where bits live. Intranet products share a serve host with a new product id.",
    "ssh.host": (
        "OpenSSH Host from ~/.ssh/config plus Include files. "
        "This script lists exact names, glob patterns, and matching hostnames; "
        "it never picks one."
    ),
    "ssh.config_dir": "Directory the running unit actually reads (journal config: line).",
    "token.isolation": "exclusive mints a new token. share-with must be explicit.",
    "serve.register": "SSH to the already-installed binary's init. This script does not edit JSON.",
    "agent.register": "Register a publish profile for this product on the agent host.",
    "signing.keys": "Keygen + embed public keys. Private keys are not committed.",
    "consume.lock": "relkit.consume/2 lock pins release URLs, hashes, and scripts/host tree hash.",
    "sidecar.layout": "relkit-updater binary layout the host pack must keep.",
    "fake.release": (
        "host.py fake verify stages a dummy zip if needed, then simulate. "
        "Do not call relkit.exe stage by hand."
    ),
    "pack.ci": "Real artifacts and CI. Optional for the first wiring, required for shipping.",
    "ops.retrospect": (
        "Final mechanical ops check. Agents must run relkit_host.py retrospect "
        "before claiming onboarding is complete."
    ),
}


class Fail(Exception):
    def __init__(self, message: str, *, code: str = "unclassified") -> None:
        super().__init__(message)
        self.code = code


def force_utf8_stdio() -> None:
    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is not None:
            try:
                reconfigure(encoding="utf-8", errors="replace")
            except (ValueError, OSError):
                pass


def host_root(script_file: Optional[Path] = None) -> Path:
    path = (script_file or Path(__file__)).resolve()
    parent = path.parent
    if parent.name == "host" and parent.parent.name == "scripts":
        return parent.parent.parent
    if parent.name == "scripts":
        return parent.parent
    return parent.parent


def host_scripts_dir(script_file: Optional[Path] = None) -> Path:
    return (script_file or Path(__file__)).resolve().parent


def tree_sha256(directory: Path) -> str:
    digest = hashlib.sha256()
    for name in ("relkit_consume.py", "relkit_host.py"):
        path = directory / name
        if not path.is_file():
            raise Fail(f"scripts/host is missing {name}")
        digest.update(name.encode("utf-8"))
        digest.update(b"\0")
        # windows-2016 checkout 常是 CRLF；hash 按 LF 算，避免 lock drift。
        digest.update(path.read_bytes().replace(b"\r\n", b"\n").replace(b"\r", b"\n"))
        digest.update(b"\0")
    return digest.hexdigest()


def relkit_dir(root: Path) -> Path:
    return root / ".relkit"


def state_path(root: Path) -> Path:
    return relkit_dir(root) / "onboarding.json"


def cache_dir(root: Path) -> Path:
    return relkit_dir(root) / "cache"


def projection_path(root: Path) -> Path:
    return cache_dir(root) / "onboarding.md"


def local_path(root: Path) -> Path:
    return cache_dir(root) / "onboarding.local.json"


def empty_steps() -> dict[str, Any]:
    return {step: {"status": "unanswered", "value": None, "note": ""} for step in STEP_IDS}


def default_state(root: Path) -> dict[str, Any]:
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


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def dump_json(data: dict[str, Any]) -> str:
    return json.dumps(data, indent=2, ensure_ascii=False) + "\n"


def load_state(root: Path) -> dict[str, Any]:
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


def render_onboarding_md(state: dict[str, Any]) -> str:
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


def write_onboarding_md(root: Path, state: dict[str, Any]) -> None:
    cache_dir(root).mkdir(parents=True, exist_ok=True)
    projection_path(root).write_text(render_onboarding_md(state), encoding="utf-8")


def save_state(root: Path, data: dict[str, Any]) -> None:
    relkit_dir(root).mkdir(parents=True, exist_ok=True)
    state_path(root).write_text(dump_json(data), encoding="utf-8")
    write_onboarding_md(root, data)


def load_local(root: Path) -> dict[str, Any]:
    path = local_path(root)
    if not path.is_file():
        return {"schema": LOCAL_SCHEMA, "evidence": {}, "secretRefs": []}
    data = load_json(path)
    data.setdefault("schema", LOCAL_SCHEMA)
    data.setdefault("evidence", {})
    data.setdefault("secretRefs", [])
    return data


def save_local(root: Path, data: dict[str, Any]) -> None:
    cache_dir(root).mkdir(parents=True, exist_ok=True)
    local_path(root).write_text(dump_json(data), encoding="utf-8")


def ensure_gitignore(root: Path) -> None:
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


def redact_text(text: str) -> str:
    text = re.sub(
        r"(export\s+(?:RELKIT_UPLOAD_TOKEN|RELKIT_SERVE_TOKEN|RELKIT_ADMIN_BOOTSTRAP)=)(['\"]?)([^'\"\s]+)\2",
        r"\1\2<redacted>\2",
        text,
    )
    text = re.sub(r"(Bearer\s+)\S+", r"\1<redacted>", text, flags=re.I)
    return text


def extract_export(name: str, text: str) -> Optional[str]:
    match = re.search(
        rf"export\s+{re.escape(name)}=(['\"]?)([^'\"\s]+)\1",
        text,
    )
    if not match:
        return None
    return match.group(2)


def set_step(
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


def step_value(state: dict[str, Any], step_id: str) -> Any:
    return state["steps"][step_id].get("value")


def product_id(state: dict[str, Any]) -> str:
    value = state.get("product") or step_value(state, "product.id")
    if not value:
        raise Fail("product.id is unanswered; run onboard set product.id <id>")
    return str(value)


def detect_stack(root: Path) -> dict[str, Any]:
    rust_manifests = list(root.glob("Cargo.toml"))
    if not rust_manifests:
        rust_manifests = list(root.glob("src-tauri/Cargo.toml"))
    if not rust_manifests:
        rust_manifests = list(root.glob("packages/*/src-tauri/Cargo.toml"))
    rust = bool(rust_manifests)
    node = (root / "package.json").is_file()
    dart = (root / "pubspec.yaml").is_file()
    go = (root / "go.mod").is_file()
    kind = []
    if rust:
        kind.append("rust")
    if node:
        kind.append("node")
    if dart:
        kind.append("dart")
    if go:
        kind.append("go")
    updater = None
    if rust and node:
        updater = "choose rust or node (which process calls Updater.open)"
    elif rust:
        updater = "rust"
    elif node:
        updater = "node"
    elif dart:
        updater = "dart"
    elif go:
        updater = "go"
    return {"languages": kind, "updater": updater, "root": str(root)}


def recommendations(root: Path, state: dict[str, Any]) -> dict[str, str]:
    stack = detect_stack(root)
    relkit_json = root / "relkit.json"
    product = None
    if relkit_json.is_file():
        try:
            product = load_json(relkit_json).get("product")
        except (OSError, json.JSONDecodeError, TypeError):
            product = None
    return {
        "repo.root": f"{root}  stack={','.join(stack['languages']) or 'unknown'}",
        "env.inspect": "run onboard inspect; error findings must be cleaned first",
        "product.id": str(product or re.sub(r"[^a-z0-9._-]+", "-", root.name.lower()).strip("-")),
        "updater.process": stack["updater"] or "ask the user which process owns Updater.open",
        "channel.ssot": (
            "migrate VERSION to VERSION.json via relkit version"
            if (root / "VERSION").is_file() and not (root / "VERSION.json").is_file()
            else "VERSION.json via relkit version"
        ),
        "backend.kind": "choose: intranet-relkit-compatible / s3-compatible / static-http",
        "ssh.host": ssh_host_recommend(),
        "ssh.config_dir": DEFAULT_SERVE_DIR + " (confirm against journal config: line)",
        "token.isolation": "exclusive (pass --share-with only when the user names an existing id)",
        "serve.register": "relkit_host.py serve add --execute after ssh.host is confirmed",
        "agent.register": "relkit_host.py agent add --execute after serve.register",
        "signing.keys": "relkit keygen on this repo; private key stays out of git",
        "consume.lock": "relkit_host.py install after scripts/relkit.lock.json is pinned",
        "sidecar.layout": "tools/bin updater sidecar next to the process that calls Updater.open",
        "fake.release": "relkit_host.py fake verify (stages dummy zip, then simulate)",
        "pack.ci": "product packaging + CI calling this script's release --execute",
        "ops.retrospect": "run relkit_host.py retrospect and require exit code 0",
    }


def recommend(root: Path, state: dict[str, Any], step_id: str) -> str:
    return recommendations(root, state)[step_id]


def explain_step(step_id: str) -> str:
    if step_id not in EXPLAIN_TEXTS:
        raise Fail(f"unknown step {step_id}")
    return EXPLAIN_TEXTS[step_id]


def choice_hint(root: Path, step_id: str) -> str:
    hints = {
        "product.id": "稳定的小写 ID，例如 loom；发布后不要随意改",
        "updater.process": " / ".join(UPDATER_PROCESS_VALUES),
        "channel.ssot": "migrate:VERSION->VERSION.json / VERSION.json / custom",
        "backend.kind": "intranet-relkit-compatible / s3-compatible / static-http",
        "env.inspect": "clean error findings then onboard inspect; no typed confirmation",
        "ssh.host": ssh_host_recommend(root=root),
        "ssh.config_dir": "运行中服务日志实际打印的配置目录",
        "token.isolation": "exclusive / share-with:<existing-product>",
    }
    return hints.get(step_id, "")


def recommended_value(root: Path, state: dict[str, Any], step_id: str) -> Optional[str]:
    stack = detect_stack(root)
    values = {
        "product.id": re.sub(r"[^a-z0-9._-]+", "-", root.name.lower()).strip("-"),
        "updater.process": (
            None
            if "rust" in stack["languages"] and "node" in stack["languages"]
            else stack["updater"]
        ),
        "channel.ssot": (
            "migrate:VERSION->VERSION.json"
            if (root / "VERSION").is_file() and not (root / "VERSION.json").is_file()
            else "VERSION.json"
        ),
        "ssh.config_dir": DEFAULT_SERVE_DIR,
        "token.isolation": "exclusive",
    }
    value = values.get(step_id)
    return str(value) if value else None


def decision_options(root: Path, step_id: str) -> list[str]:
    if step_id == "updater.process":
        return list(UPDATER_PROCESS_VALUES)
    if step_id == "channel.ssot":
        options = ["VERSION.json", "custom"]
        if (root / "VERSION").is_file() and not (root / "VERSION.json").is_file():
            options.insert(0, "migrate:VERSION->VERSION.json")
        return options
    if step_id == "backend.kind":
        return ["intranet-relkit-compatible", "s3-compatible", "static-http"]
    if step_id == "ssh.host":
        inventory = ssh_inventory(root)
        values = list(inventory.get("exact") or [])
        for item in inventory.get("matched") or []:
            host = str(item.get("host") or "")
            if host and host not in values:
                values.append(host)
        return values
    if step_id == "token.isolation":
        return ["exclusive", "share-with:<existing-product>"]
    return []


def question_batch_revision(
    root: Path, state: dict[str, Any], intent: str
) -> str:
    inspection = env_inspect_report(root)
    material = {
        "intent": intent,
        "inspect": inspection,
        "decisions": {
            step: {
                "status": state["steps"][step]["status"],
                "value": state["steps"][step]["value"],
            }
            for step in BATCH_DECISION_STEPS
        },
    }
    encoded = json.dumps(
        material, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def build_question_batch(
    root: Path, state: dict[str, Any], intent: str
) -> dict[str, Any]:
    if intent not in ONBOARD_INTENTS:
        raise Fail(
            "intent must be " + " / ".join(ONBOARD_INTENTS),
            code="onboard-batch-intent-invalid",
        )
    if state["steps"]["env.inspect"]["status"] != "verified":
        raise Fail(
            "env.inspect is not verified; run onboard inspect before batching questions",
            code="env-inspect-required",
        )
    existing_product = str(state.get("product") or "")
    inspection = env_inspect_report(root)
    configured_product = str(
        (inspection.get("facts") or {}).get("product") or ""
    )
    if intent == "fresh" and (existing_product or configured_product):
        found = existing_product or configured_product
        raise Fail(
            f"fresh conflicts with existing product.id={found}; "
            "use reconfigure or upgrade",
            code="onboard-batch-intent-conflict",
        )
    if intent == "upgrade":
        selected = [
            step
            for step in BATCH_DECISION_STEPS
            if state["steps"][step]["status"]
            in ("unanswered", "stale", "blocked", "drift")
        ]
    else:
        selected = list(BATCH_DECISION_STEPS)
    questions: list[dict[str, Any]] = []
    for step in selected:
        questions.append(
            {
                "id": step,
                "prompt": explain_step(step),
                "hint": choice_hint(root, step),
                "options": decision_options(root, step),
                "recommended": recommended_value(root, state, step),
                "current": state["steps"][step]["value"],
            }
        )
    return {
        "schema": QUESTION_BATCH_SCHEMA,
        "intent": intent,
        "revision": question_batch_revision(root, state, intent),
        "inspection": inspection,
        "questions": questions,
        "answerSchema": ANSWER_BATCH_SCHEMA,
        "answerShape": {
            "schema": ANSWER_BATCH_SCHEMA,
            "intent": intent,
            "revision": "<copy revision>",
            "answers": {item["id"]: "<value>" for item in questions},
        },
    }


def next_unresolved_step(state: dict[str, Any]) -> Optional[str]:
    return next(
        (
            step
            for step in STEP_IDS
            if state["steps"][step]["status"] in ("unanswered", "stale", "blocked", "drift")
        ),
        None,
    )


def print_decision(root: Path, state: dict[str, Any], step_id: str) -> None:
    print(f"\n待决策: {step_id}")
    print(f"说明: {explain_step(step_id)}")
    print(f"推荐: {recommend(root, state, step_id)}")
    hint = choice_hint(root, step_id)
    if hint:
        print(f"可选: {hint}")
    if step_id == "ssh.host":
        print_ssh_inventory(ssh_inventory(root))
    if step_id == "env.inspect":
        print_env_inspect(env_inspect_report(root))


def normalize_interactive_value(step_id: str, raw: str) -> tuple[str, Optional[str]]:
    value = raw.strip()
    if step_id == "token.isolation" and value.startswith("share-with:"):
        shared = value.split(":", 1)[1].strip()
        if not shared:
            raise Fail("share-with 后必须给已有 product id")
        return "share-with", shared
    return value, None


def run_interactive_wizard(
    root: Path,
    *,
    input_fn: Callable[[str], str] = input,
) -> int:
    """Ask every policy question; never silently accept a recommendation."""
    while True:
        state = load_state(root)
        step_id = next_unresolved_step(state)
        if step_id is None:
            print("开箱状态没有未决项。运行 verify 对账后再 release。")
            return 0
        if step_id in ACTION_STEPS:
            print_decision(root, state, step_id)
            print("这是执行步骤，不用文字确认冒充完成。")
            print("运行上面建议的显式子命令；有远端写入时还需 --execute，重启还需 --restart。")
            return 0
        if step_id == "env.inspect":
            print_decision(root, state, step_id)
            cmd_onboard_inspect(root)
            continue
        print_decision(root, state, step_id)
        answer = input_fn("输入选择；r=采用推荐，?=重看说明，q=保存并退出: ").strip()
        if answer.lower() == "q":
            print("已保存当前进度。")
            return 0
        if answer == "?":
            continue
        if answer.lower() == "r":
            selected = recommended_value(root, state, step_id)
            if selected is None:
                print("这一项没有可安全代选的单值推荐，请输入你的选择。")
                continue
            answer = selected
        if not answer:
            print("空输入不会确认。")
            continue
        value, share_with = normalize_interactive_value(step_id, answer)
        cmd_onboard_set(root, step_id, value, share_with)


def _expand_ssh_path(raw: str) -> Path:
    return Path(os.path.expanduser(os.path.expandvars(raw.strip().strip("\"'"))))


def _ssh_include_paths(raw: str) -> list[Path]:
    expanded = _expand_ssh_path(raw)
    name = expanded.name
    if any(ch in name for ch in "*?["):
        parent = expanded.parent
        if not parent.is_dir():
            return []
        return sorted(path for path in parent.glob(name) if path.is_file())
    return [expanded]


def parse_ssh_config(
    config_path: Optional[Path] = None,
    *,
    _seen: Optional[set[Path]] = None,
) -> tuple[list[str], list[str]]:
    exact, patterns, _ports = parse_ssh_config_ports(config_path, _seen=_seen)
    return exact, patterns


def parse_ssh_config_ports(
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


def list_ssh_hosts(config_path: Optional[Path] = None) -> list[str]:
    exact, _patterns = parse_ssh_config(config_path)
    return exact


def ssh_host_port(name: str, config_path: Optional[Path] = None) -> Optional[int]:
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


def ssh_target(host: str, port: Optional[int] = None) -> str:
    """host:port for logs and errors; never guess silently between 22 and 36000."""
    resolved = port if port is not None else ssh_host_port(host)
    return f"{host}:{resolved}" if resolved else f"{host}:22 (ssh default)"


def ssh_host_allowed(name: str, config_path: Optional[Path] = None) -> bool:
    exact, patterns = parse_ssh_config(config_path)
    if name in exact:
        return True
    for pattern in patterns:
        if pattern == "*":
            continue
        if fnmatch.fnmatch(name, pattern):
            return True
    return False


def ssh_host_recommend(
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


def default_ssh_config_path() -> Path:
    return Path.home() / ".ssh" / "config"


def parse_known_host_names(path: Path) -> list[str]:
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


def collect_json_hosts(value: Any, into: list[str]) -> None:
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


def ssh_inventory(
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


def print_ssh_inventory(inventory: dict[str, Any]) -> None:
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


def inspect_path(root: Path) -> Path:
    return cache_dir(root) / "inspect.json"


def ops_journal_path(root: Path) -> Path:
    return cache_dir(root) / "ops-journal.jsonl"


def onboarding_ignored(root: Path) -> Optional[bool]:
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


def env_inspect_report(root: Path) -> dict[str, Any]:
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
        for name, kind in facts["backends"].items():
            if kind in STALE_BACKEND_TYPES:
                findings.append(
                    {
                        "severity": "error",
                        "code": "stale-backend-type",
                        "detail": f"backends.{name}.type={kind}",
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
            facts["lock"] = {
                "release": lock.get("release"),
                "hostScriptsMatch": bool(expected) and expected == actual,
            }
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


def print_env_inspect(report: dict[str, Any]) -> None:
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


def apply_env_inspect(root: Path, state: dict[str, Any], report: dict[str, Any]) -> None:
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


def cmd_onboard_inspect(root: Path) -> int:
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


def append_ops_journal(root: Path, argv: Sequence[str], error: Fail) -> None:
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


def load_ops_journal(root: Path) -> list[dict[str, Any]]:
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


def dummy_stage_zip(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("DUMMY.txt", "relkit fake.release dummy\n")


def stage_dummy_release(root: Path, version: str, binary: Path) -> None:
    dummy = cache_dir(root) / "dummy-fake"
    dummy.mkdir(parents=True, exist_ok=True)
    windows = dummy / "dummy-windows-x64.zip"
    macos = dummy / "dummy-macos.zip"
    dummy_stage_zip(windows)
    dummy_stage_zip(macos)
    run_relkit(
        root,
        binary,
        [
            "stage",
            version,
            "--add",
            str(windows),
            "os=windows,arch=x64,meta.layout=wholeRoot",
            "--add",
            str(macos),
            "os=macos,meta.layout=wholeRoot",
        ],
    )


def ssh_argv(host: str, port: Optional[int]) -> list[str]:
    argv = ["ssh", "-o", "BatchMode=yes"]
    if port:
        argv.extend(["-p", str(port)])
    argv.append(host)
    return argv


def ssh_run(
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


def ssh_write(
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


def ssh_path_exists(host: str, remote_path: str) -> bool:
    if not remote_path.startswith("/"):
        raise Fail("remote path must be absolute")
    script = (
        f"if sudo test -f {shlex.quote(remote_path)}; then echo exists; else echo missing; fi"
    )
    out = ssh_run(host, ["bash", "-lc", script]).stdout.strip().splitlines()
    return bool(out) and out[-1] == "exists"


def agent_profile_path(config_path: str, product: str) -> str:
    return f"{Path(config_path).parent.as_posix()}/products/{product}.json"


def rewrite_agent_backend_url(url: str) -> str:
    parsed = urlparse(url)
    if parsed.hostname != AGENT_ORIGIN_HOST:
        return url
    if parsed.scheme == "http" and parsed.port == 8080:
        return url
    return urlunparse(
        ("http", AGENT_ORIGIN_NETLOC, parsed.path, parsed.params, parsed.query, parsed.fragment)
    )


def apply_agent_backend_urls(backend: dict[str, Any]) -> None:
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


def extract_publish_profile(machine: dict[str, Any]) -> dict[str, Any]:
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


def machine_publish_config(root: Path, product: str, key_id: str, private_relpath: str) -> dict[str, Any]:
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


def parse_list_products(text: str) -> list[str]:
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


def parse_agent_products(text: str) -> list[str]:
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


def require_execute(args: argparse.Namespace, action: str) -> None:
    if not getattr(args, "execute", False):
        raise Fail(f"refusing to {action} without --execute")


def require_restart_flag(args: argparse.Namespace) -> None:
    if not getattr(args, "restart", False):
        raise Fail(
            "config changed but the process was not restarted. "
            "Pass --restart only after the user said the machine may restart."
        )


def write_secret(root: Path, token: str, *, note: Path = SECRET_NOTE) -> Path:
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


def archive_secret(root: Path) -> None:
    path = root / SECRET_NOTE
    if not path.is_file():
        return
    archived = path.with_name(path.name + ".revoked")
    path.replace(archived)


def relkit_bin(root: Path, explicit: Optional[str] = None) -> Path:
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


def cmd_status(root: Path, as_json: bool = False) -> int:
    state = load_state(root)
    report = reconcile(root, state, write=False)
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


def claim_remote_registration(
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


def reconcile_signing_profile(
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


def updater_sidecar_name() -> str:
    return "relkit-updater.exe" if os.name == "nt" else "relkit-updater"


def reconcile_sidecar_layout(root: Path, state: dict[str, Any], drift: list[str]) -> None:
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


def reconcile_pack_ci(root: Path, state: dict[str, Any]) -> None:
    if state["steps"]["pack.ci"]["status"] == "unanswered":
        return
    yaml_dev = root / "ci" / "build_dev.yaml"
    yaml_stable = root / "ci" / "build_stable.yaml"
    entry = root / "scripts" / "ci_release.mjs"
    if not (yaml_dev.is_file() and yaml_stable.is_file() and entry.is_file()):
        return
    text = entry.read_text(encoding="utf-8")
    if "relkit_host.py" not in text or "release" not in text:
        return
    set_step(
        state,
        "pack.ci",
        "confirmed",
        "ci/build_dev.yaml,ci/build_stable.yaml",
        "BK-CI PAC; entry scripts/ci_release.mjs; CI holds agent token only",
    )


def reconcile_fake_stage(root: Path, state: dict[str, Any]) -> None:
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


def clear_stale_staged_trees(root: Path, current_version: str) -> list[str]:
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


def reconcile(root: Path, state: dict[str, Any], *, write: bool) -> dict[str, Any]:
    drift: list[str] = []
    unconfirmed: list[str] = []
    via_ci = os.environ.get("RELKIT_RELEASE_VIA_CI") == "1"
    serve = state.get("serve") or {}
    host = serve.get("sshHost")
    port = serve.get("sshPort") or ssh_host_port(host or "")
    config_dir = serve.get("configDir") or DEFAULT_SERVE_DIR
    if via_ci:
        # CI 构建机到不了发布机的 SSH 端口；真发走 agent HTTP + RELKIT_UPLOAD_TOKEN。
        print("relkit: CI publish skips SSH probes to the serve/agent host")
    elif host:
        try:
            version = ssh_run(host, ["relkit-serve", "-version"], port=port).stdout.strip()
            serve["remoteVersion"] = version
            serve["sshPort"] = port
            listed = ssh_run(
                host,
                ["sudo", "relkit-serve", "init", "-out", str(config_dir), "-list-products"],
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
    elif not via_ci and state["steps"]["serve.register"]["status"] in (
        "applied",
        "verified",
    ):
        unconfirmed.append("serve.register is applied but ssh.host is empty")

    agent = state.get("agent") or {}
    agent_host = agent.get("sshHost") or host
    agent_port = agent.get("sshPort") or ssh_host_port(agent_host or "")
    config_path = agent.get("configPath") or DEFAULT_AGENT_CONFIG
    if agent_host and not via_ci:
        try:
            version = ssh_run(
                agent_host, ["relkit-agent", "-version"], port=agent_port
            ).stdout.strip()
            agent["remoteVersion"] = version
            agent["sshPort"] = agent_port
            listed = ssh_run(
                agent_host,
                ["sudo", "relkit-agent", "init", "-config", str(config_path), "-list-products"],
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

    reconcile_sidecar_layout(root, state, drift)
    reconcile_pack_ci(root, state)
    reconcile_fake_stage(root, state)

    if write:
        save_state(root, state)
    report = dict(state)
    report["drift"] = drift
    report["unconfirmed"] = unconfirmed
    return report


def cmd_onboard_start(root: Path, *, interactive: bool = False) -> int:
    ensure_gitignore(root)
    state = load_state(root)
    stack = detect_stack(root)
    set_step(state, "repo.root", "verified", ".", ",".join(stack["languages"]))
    report = env_inspect_report(root)
    apply_env_inspect(root, state, report)
    save_state(root, state)
    print("已检测仓库事实；尚未替你确认任何策略。")
    print(f"仓根: {root}")
    print(f"技术栈: {','.join(stack['languages']) or 'unknown'}")
    print_env_inspect(report)
    if interactive:
        return run_interactive_wizard(root)
    unresolved = next_unresolved_step(state)
    if unresolved:
        print_decision(root, state, unresolved)
        if unresolved == "env.inspect":
            print("非交互模式: 清理 error findings 后运行 onboard inspect")
        else:
            print(f"非交互模式写入: onboard set {unresolved} <你的选择>")
    return 0


def cmd_onboard_resume(root: Path, *, interactive: bool = False) -> int:
    state = load_state(root)
    if interactive:
        return run_interactive_wizard(root)
    step_id = next_unresolved_step(state)
    if step_id:
        print_decision(root, state, step_id)
        if step_id == "env.inspect":
            print("非交互模式: 清理 error findings 后运行 onboard inspect")
        else:
            print(f"非交互模式写入: onboard set {step_id} <你的选择>")
        return 0
    print("开箱状态没有未决项。运行 verify 对账。")
    return 0


def cmd_onboard_explain(step_id: str) -> int:
    print(explain_step(step_id))
    return 0


def cmd_onboard_reset(root: Path, *, yes: bool = False) -> int:
    path = relkit_dir(root)
    if not yes:
        raise Fail("onboard reset deletes .relkit/; pass --yes")
    if path.exists():
        shutil.rmtree(path)
    print(f"deleted {path.as_posix()}")
    print("product onboarding is unanswered; no extra markdown was kept")
    return 0


def apply_decision_to_state(
    root: Path,
    state: dict[str, Any],
    step_id: str,
    value: Optional[str],
    share_with: Optional[str],
    note: Optional[str] = None,
) -> None:
    if step_id not in BATCH_DECISION_STEPS:
        raise Fail(
            f"{step_id} is not a human decision step",
            code="onboard-answer-step-invalid",
        )
    previous_note = str(state["steps"].get(step_id, {}).get("note") or "")
    kept_note = previous_note if note is None else note
    if step_id == "product.id":
        if state["steps"]["env.inspect"]["status"] != "verified":
            raise Fail(
                "env.inspect is not verified; run onboard inspect after cleanup",
                code="env-inspect-required",
            )
        if not value:
            raise Fail("onboard set product.id needs an id")
        state["product"] = value
        set_step(state, step_id, "confirmed", value, kept_note, mark_later_stale=True)
    elif step_id == "ssh.host":
        if not value:
            raise Fail("onboard set ssh.host needs a Host name from ~/.ssh/config")
        if not ssh_host_allowed(value):
            raise Fail(f"{value} does not match ~/.ssh/config. {ssh_host_recommend()}")
        state["serve"]["sshHost"] = value
        state["serve"]["sshPort"] = ssh_host_port(value)
        set_step(
            state,
            step_id,
            "confirmed",
            value,
            kept_note or ssh_target(value),
            mark_later_stale=True,
        )
    elif step_id == "ssh.config_dir":
        if not value:
            raise Fail("onboard set ssh.config_dir needs the live config directory")
        state["serve"]["configDir"] = value
        set_step(state, step_id, "confirmed", value, kept_note, mark_later_stale=True)
    elif step_id == "token.isolation":
        if value in (None, "exclusive"):
            state["serve"]["shareWith"] = None
            set_step(state, step_id, "confirmed", "exclusive", kept_note, mark_later_stale=True)
        elif value == "share-with":
            if not share_with:
                raise Fail("share-with requires --share-with <existing-id>")
            state["serve"]["shareWith"] = share_with
            set_step(
                state,
                step_id,
                "confirmed",
                f"share-with:{share_with}",
                kept_note,
                mark_later_stale=True,
            )
        else:
            raise Fail("token.isolation is exclusive or share-with")
    elif step_id == "updater.process":
        if not value:
            raise Fail("onboard set updater.process needs a process name")
        if value not in UPDATER_PROCESS_VALUES:
            raise Fail("updater.process must be " + " / ".join(UPDATER_PROCESS_VALUES))
        set_step(state, step_id, "confirmed", value, kept_note, mark_later_stale=True)
    elif step_id == "backend.kind":
        allowed = ("intranet-relkit-compatible", "s3-compatible", "static-http")
        if value not in allowed:
            raise Fail("backend.kind must be " + " / ".join(allowed))
        set_step(
            state,
            step_id,
            "confirmed",
            value,
            kept_note,
            mark_later_stale=True,
        )
    else:
        if value is None:
            raise Fail(f"onboard set {step_id} needs a value")
        set_step(state, step_id, "confirmed", value, kept_note, mark_later_stale=True)


def cmd_onboard_set(
    root: Path,
    step_id: str,
    value: Optional[str],
    share_with: Optional[str],
    note: Optional[str] = None,
) -> int:
    if step_id == "env.inspect":
        raise Fail(
            "env.inspect is written by onboard inspect, not onboard set",
            code="env-inspect-set-refused",
        )
    state = load_state(root)
    apply_decision_to_state(root, state, step_id, value, share_with, note)
    save_state(root, state)
    print(f"recorded {step_id}={state['steps'][step_id]['value']} status=confirmed")
    print("no remote mutation happened")
    return 0


def cmd_onboard_questions(root: Path, intent: str, as_json: bool = False) -> int:
    batch = build_question_batch(root, load_state(root), intent)
    if as_json:
        print(dump_json(batch), end="")
        return 0
    print(f"批量决策 intent={intent} revision={batch['revision']}")
    for index, item in enumerate(batch["questions"], start=1):
        print(f"{index}. {item['id']}: {item['prompt']}")
        if item["options"]:
            print("   可选: " + " / ".join(item["options"]))
        if item["recommended"]:
            print(f"   推荐: {item['recommended']}")
        if item["current"]:
            print(f"   当前: {item['current']}")
    print("用 onboard apply --answers <json-file> 原子提交整批答案。")
    return 0


def _answer_value(
    step_id: str, raw: Any
) -> tuple[Optional[str], Optional[str], Optional[str]]:
    if isinstance(raw, str):
        value, share_with = normalize_interactive_value(step_id, raw)
        return value, share_with, None
    if not isinstance(raw, dict):
        raise Fail(
            "answer must be a string or object with value/note",
            code="onboard-answer-shape-invalid",
        )
    value_raw = raw.get("value")
    if not isinstance(value_raw, str):
        raise Fail(
            "answer object needs string value",
            code="onboard-answer-shape-invalid",
        )
    value = value_raw.strip()
    share_with = raw.get("shareWith")
    if share_with is not None and not isinstance(share_with, str):
        raise Fail(
            "answer shareWith must be a string",
            code="onboard-answer-shape-invalid",
        )
    if value.startswith("share-with:"):
        value, inline_share = normalize_interactive_value("token.isolation", value)
        share_with = inline_share
    note = raw.get("note")
    if note is not None and not isinstance(note, str):
        raise Fail(
            "answer note must be a string",
            code="onboard-answer-shape-invalid",
        )
    return value, share_with, note


def cmd_onboard_apply(root: Path, answers_path: Path) -> int:
    resolved_answers = (
        answers_path.resolve()
        if answers_path.is_absolute()
        else (root / answers_path).resolve()
    )
    try:
        payload = load_json(resolved_answers)
    except (OSError, json.JSONDecodeError, TypeError) as error:
        raise Fail(
            f"cannot read answer batch: {error}",
            code="onboard-answer-unreadable",
        ) from error
    if payload.get("schema") != ANSWER_BATCH_SCHEMA:
        raise Fail(
            f"answer batch must use {ANSWER_BATCH_SCHEMA}",
            code="onboard-answer-schema-invalid",
        )
    intent = str(payload.get("intent") or "")
    state = load_state(root)
    batch = build_question_batch(root, state, intent)
    if payload.get("revision") != batch["revision"]:
        raise Fail(
            "repository facts or decisions changed after questions were generated; "
            "regenerate the batch and ask only the changed questions",
            code="onboard-answer-revision-conflict",
        )
    answers = payload.get("answers")
    if not isinstance(answers, dict):
        raise Fail(
            "answer batch needs an answers object",
            code="onboard-answer-shape-invalid",
        )
    expected = [item["id"] for item in batch["questions"]]
    missing = [step for step in expected if step not in answers]
    unknown = [str(step) for step in answers if step not in expected]
    conflicts: list[str] = []
    if missing:
        conflicts.append("missing: " + ", ".join(missing))
    if unknown:
        conflicts.append("not asked: " + ", ".join(unknown))
    candidate = json.loads(json.dumps(state))
    if not conflicts:
        for step in expected:
            try:
                value, share_with, note = _answer_value(step, answers[step])
                apply_decision_to_state(
                    root, candidate, step, value, share_with, note
                )
            except Fail as error:
                conflicts.append(f"{step}: {error}")
    product = str(candidate.get("product") or "")
    shared = str(candidate.get("serve", {}).get("shareWith") or "")
    if shared and shared == product:
        conflicts.append("token.isolation: cannot share with the same product.id")
    if conflicts:
        raise Fail(
            "answer batch rejected atomically:\n  " + "\n  ".join(conflicts),
            code="onboard-answer-conflict",
        )
    save_state(root, candidate)
    print(f"applied {len(expected)} decisions atomically; no remote mutation happened")
    return 0


def import_consume() -> Any:
    scripts = host_scripts_dir()
    if str(scripts) not in sys.path:
        sys.path.insert(0, str(scripts))
    import relkit_consume

    return relkit_consume


def consume_components(root: Path) -> list[str]:
    stack = detect_stack(root)
    components = ["cli", "updater"]
    if "dart" in stack["languages"]:
        components.insert(0, "sdk-dart")
    if "rust" in stack["languages"]:
        components.insert(0, "sdk-rust")
    if "go" in stack["languages"]:
        components.insert(0, "sdk-go")
    return components


def require_host_scripts_match_lock(root: Path) -> dict[str, Any]:
    lock_path = root / "scripts" / "relkit.lock.json"
    if not lock_path.is_file():
        raise Fail(f"missing {lock_path}")
    lock = load_json(lock_path)
    if lock.get("schema") != LOCK_SCHEMA:
        raise Fail(f"{lock_path} must use {LOCK_SCHEMA}")
    expected = str(lock.get("hostScriptsSha256") or "").lower()
    actual = tree_sha256(root / "scripts" / "host")
    if not re.fullmatch(r"[0-9a-f]{64}", expected) or expected != actual:
        raise Fail(
            "scripts/host does not match relkit.lock.json after install; "
            "do not start the product build"
        )
    return lock


def lock_artifact_names(root: Path) -> set[str]:
    """Artifact keys the lock pins, or an empty set when the lock is unusable."""
    path = root / "scripts" / "relkit.lock.json"
    if not path.is_file():
        return set()
    try:
        artifacts = load_json(path).get("artifacts")
    except (OSError, json.JSONDecodeError, AttributeError):
        return set()
    return set(artifacts) if isinstance(artifacts, dict) else set()


def cmd_install(root: Path, consume_argv: Sequence[str]) -> int:
    consume = import_consume()
    extra = list(consume_argv)
    if not any(item == "--component" for item in extra):
        pinned = lock_artifact_names(root)
        for component in consume_components(root):
            # A release that predates an SDK attachment simply has nothing to
            # install; the lock stays the single source of truth either way.
            if component.startswith("sdk-") and pinned and component not in pinned:
                print(f"relkit: {component} is not pinned by the lock; skipping")
                continue
            extra.extend(["--component", component])
    code = consume.main(["install", "--project-root", str(root), *extra])
    if code == 0:
        lock = require_host_scripts_match_lock(root)
        state = load_state(root)
        set_step(state, "consume.lock", "verified", lock.get("release"), "lock installed")
        save_state(root, state)
    return code


def int_window(raw: Any, fallback: int) -> dict[str, int]:
    try:
        minimum = int((raw or {}).get("min") or fallback)
        maximum = int((raw or {}).get("max") or minimum)
    except (AttributeError, TypeError, ValueError):
        return {"min": fallback, "max": fallback}
    return {"min": minimum, "max": maximum}


def build_release_lock(
    previous: dict[str, Any],
    *,
    release: str,
    commit: str,
    consumer_hash: str,
    host_tree_hash: str,
    manifest: dict[str, Any],
    base: str,
    sums: dict[str, str],
) -> dict[str, Any]:
    """Build a whole relkit.consume/2 lock out of one immutable release.

    Constructing instead of patching is what lets a relkit.consume/1 repo run
    upgrade directly: every pinned value is restated by the release, so the old
    lock's shape is never a precondition. Only the updater IPC window carries
    over, because no release attachment declares it.
    """
    artifacts: dict[str, Any] = {}
    rewrite_lock_artifacts(artifacts, base, sums)
    return {
        "schema": LOCK_SCHEMA,
        "release": release,
        "commit": commit,
        "consumerSha256": consumer_hash,
        "hostScriptsSha256": host_tree_hash,
        "protocol": int_window(
            {
                "min": manifest.get("minProtocol"),
                "max": manifest.get("maxProtocol"),
            },
            PUBLISH_PROTOCOL_FALLBACK,
        ),
        "updaterIpc": int_window(previous.get("updaterIpc"), UPDATER_IPC_FALLBACK),
        "artifacts": artifacts,
    }


def cmd_upgrade(root: Path, release: str) -> int:
    if not re.fullmatch(r"v\d+\.\d+\.\d+", release):
        raise Fail("upgrade expects vX.Y.Z")
    lock_path = root / "scripts" / "relkit.lock.json"
    previous: dict[str, Any] = {}
    if lock_path.is_file():
        try:
            loaded = load_json(lock_path)
        except (OSError, json.JSONDecodeError) as error:
            raise Fail(
                f"{lock_path} exists but is not readable JSON: {error}",
                code="lock-unreadable",
            ) from error
        if isinstance(loaded, dict):
            previous = loaded
    base = f"https://github.com/{GITHUB_REPO}/releases/download/{release}"
    # Commit 只从同目录的 immutable 附件读。不要打 api.github.com：
    # 匿名 REST 每小时 60 次，和 Releases 下载不共用配额。
    manifest = release_manifest(base)
    commit = release_commit(manifest, base)
    host_tree_hash = str(manifest.get("hostScriptsSha256") or "").strip().lower()
    consumer_hash = str(manifest.get("consumerSha256") or "").strip().lower()
    if not re.fullmatch(r"[0-9a-f]{64}", host_tree_hash):
        raise Fail(f"{base}/manifest.json has no valid hostScriptsSha256")
    if not re.fullmatch(r"[0-9a-f]{64}", consumer_hash):
        raise Fail(f"{base}/manifest.json has no valid consumerSha256")
    sums_text = http_get(f"{base}/SHA256SUMS")
    sums = parse_sha256sums(sums_text)
    if "relkit-host-scripts.zip" not in sums:
        raise Fail("SHA256SUMS has no relkit-host-scripts.zip")
    lock = build_release_lock(
        previous,
        release=release,
        commit=commit,
        consumer_hash=consumer_hash,
        host_tree_hash=host_tree_hash,
        manifest=manifest,
        base=base,
        sums=sums,
    )
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    lock_path.write_text(dump_json(lock), encoding="utf-8")
    previous_schema = str(previous.get("schema") or "none")
    if previous_schema != LOCK_SCHEMA:
        print(f"rewrote {lock_path} from {previous_schema} to {LOCK_SCHEMA}")
    print(f"updated {lock_path} to {release}")
    return cmd_install(root, [])


def http_get(url: str) -> str:
    request = Request(url, headers={"User-Agent": "relkit-host"})
    try:
        with urlopen(request, timeout=30) as response:
            return response.read().decode("utf-8")
    except (HTTPError, URLError, TimeoutError, OSError) as error:
        raise Fail(f"GET {url} failed: {error}") from error


def parse_sha256sums(text: str) -> dict[str, str]:
    mapping: dict[str, str] = {}
    for line in text.splitlines():
        parts = line.split()
        if len(parts) >= 2:
            mapping[parts[-1].lstrip("*")] = parts[0].lower()
    return mapping


def release_manifest(base: str) -> dict[str, Any]:
    """Read immutable build metadata from the release CDN, never the REST API."""
    raw = http_get(f"{base}/manifest.json")
    try:
        manifest = json.loads(raw)
    except json.JSONDecodeError as error:
        raise Fail(f"{base}/manifest.json is not JSON: {error}") from error
    if not isinstance(manifest, dict):
        raise Fail(f"{base}/manifest.json is not a JSON object")
    return manifest


def release_commit(manifest: dict[str, Any], base: str) -> str:
    commit = str(manifest.get("commit") or "").strip().lower()
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise Fail(
            f"{base}/manifest.json must pin a 40-char commit; refusing to keep the previous lock commit"
        )
    return commit


def release_commit_from_manifest(base: str) -> str:
    """Compatibility helper used by callers that only need the commit."""
    return release_commit(release_manifest(base), base)


def rewrite_lock_artifacts(artifacts: dict[str, Any], base: str, sums: dict[str, str]) -> None:
    host_scripts = "relkit-host-scripts.zip"
    if host_scripts in sums:
        artifacts["host-scripts"] = {
            "url": f"{base}/{host_scripts}",
            "sha256": sums[host_scripts],
        }
    dart = "relkit-sdk-dart.zip"
    if dart in sums:
        artifacts["sdk-dart"] = {"url": f"{base}/{dart}", "sha256": sums[dart]}
    rust = "relkit-sdk-rust.zip"
    if rust in sums:
        artifacts["sdk-rust"] = {"url": f"{base}/{rust}", "sha256": sums[rust]}
    go = "relkit-sdk-go.zip"
    if go in sums:
        artifacts["sdk-go"] = {"url": f"{base}/{go}", "sha256": sums[go]}
    cli = artifacts.setdefault("cli", {})
    updater = artifacts.setdefault("updater", {})
    mapping = {
        ("cli", "linux-amd64"): "relkit-linux-amd64",
        ("cli", "linux-arm64"): "relkit-linux-arm64",
        ("cli", "windows-amd64"): "relkit-windows-amd64.exe",
        ("cli", "darwin-amd64"): "relkit-darwin-amd64",
        ("cli", "darwin-arm64"): "relkit-darwin-arm64",
        ("updater", "linux-amd64"): "relkit-updater-linux-amd64",
        ("updater", "linux-arm64"): "relkit-updater-linux-arm64",
        ("updater", "windows-amd64"): "relkit-updater-windows-amd64.exe",
        ("updater", "darwin-amd64"): "relkit-updater-darwin-amd64",
        ("updater", "darwin-arm64"): "relkit-updater-darwin-arm64",
    }
    buckets = {"cli": cli, "updater": updater}
    for (component, target), filename in mapping.items():
        if filename not in sums:
            continue
        buckets[component][target] = {
            "url": f"{base}/{filename}",
            "sha256": sums[filename],
        }


def relkit_config(root: Path) -> dict[str, Any]:
    path = root / "relkit.json"
    if not path.is_file():
        raise Fail("relkit.json not found; run keys gen --execute first")
    try:
        data = json.loads(path.read_text(encoding="utf-8-sig"))
    except json.JSONDecodeError as exc:
        raise Fail(f"relkit.json is not valid JSON: {exc}") from exc
    if not isinstance(data, dict):
        raise Fail("relkit.json must be a JSON object")
    return data


def agent_base_url(root: Path) -> Optional[str]:
    url = str((relkit_config(root).get("agent") or {}).get("url") or "").strip()
    return url or None


def publish_protocol_window(root: Path) -> tuple[int, int]:
    """Publisher handshake窗口。lock 钉的 CLI 与 agent 是同一份契约。"""
    lock = root / "scripts" / "relkit.lock.json"
    if lock.is_file():
        try:
            protocol = json.loads(lock.read_text(encoding="utf-8")).get("protocol")
            minimum = int((protocol or {}).get("min") or PUBLISH_PROTOCOL_FALLBACK)
            maximum = int((protocol or {}).get("max") or minimum)
            return minimum, maximum
        except (OSError, TypeError, ValueError, AttributeError, json.JSONDecodeError):
            pass
    return PUBLISH_PROTOCOL_FALLBACK, PUBLISH_PROTOCOL_FALLBACK


def sanitize_upload_token(raw: str) -> str:
    token = raw.replace("\ufeff", "").strip()
    if len(token) >= 2 and token[0] == token[-1] and token[0] in "'\"":
        token = token[1:-1].strip()
    return token.replace("\r", "").replace("\n", "").strip()


def upload_token(root: Path, note: Path) -> str:
    token = sanitize_upload_token(os.environ.get(TOKEN_ENV, ""))
    if "${{" in token or "secretKey" in token:
        raise Fail(
            f"{TOKEN_ENV} looks unsubstituted by the pipeline; "
            "the CI GUI must inject the product agent Bearer"
        )
    if token:
        print(f"{TOKEN_ENV} length={len(token)}")
        return token
    path = root / note
    if path.is_file():
        found = extract_export(TOKEN_ENV, path.read_text(encoding="utf-8"))
        if found:
            return sanitize_upload_token(found)
    raise Fail(
        f"{TOKEN_ENV} is unset. CI injects it as a pipeline secret; "
        f"locally it lives in {str(note).replace(chr(92), '/')}"
    )


def publish_via_agent(
    root: Path,
    binary: Path,
    *,
    product: str,
    version: str,
    url: str,
    execute: bool,
) -> int:
    """CI 侧发布：传 CAS + 瘦 staged 树，再让发布机持钥写 index。"""
    staged = cache_dir(root) / "staged" / version
    if not staged.is_dir():
        raise Fail(f"no staged tree for {version}; run relkit stage first ({staged})")
    publish = url.rstrip("/") + "/publish"
    minimum, maximum = publish_protocol_window(root)
    print(f"agent {url}")
    print(f"staged {staged}")
    if not execute:
        print(f"plan: relkit cas-put --version {version} --product {product}")
        print(f"plan: POST {publish}")
        print("pass --execute to upload and publish; the signing key stays on the publish host")
        return 0
    token = upload_token(root, AGENT_SECRET_NOTE)
    env = dict(os.environ)
    env[TOKEN_ENV] = token
    env["RELKIT_AGENT_URL"] = url
    result = subprocess.run(
        [str(binary), "cas-put", "--version", version, "--product", product],
        cwd=str(root),
        env=env,
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
        detail = text.strip() or f"exit {result.returncode}"
        raise Fail(
            "relkit cas-put failed\n"
            + detail
            + (
                f"\n{TOKEN_ENV} is not accepted as the agent Bearer for this product"
                if "401" in detail
                else ""
            )
        )
    staged_sha = re.search(r"sha256=([0-9a-f]{64})", text)
    if not staged_sha:
        raise Fail("cas-put did not report the staged sha256")
    payload = json.dumps(
        {"product": product, "version": version, "stagedSha256": staged_sha.group(1)}
    ).encode("utf-8")
    request = Request(
        publish,
        data=payload,
        method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-Relkit-Publish-Protocol": str(minimum),
            "X-Relkit-Publish-Protocol-Min": str(minimum),
            "X-Relkit-Publish-Protocol-Max": str(maximum),
            "X-Relkit-Version": "relkit-host",
        },
    )
    print(f"POST {publish}")
    try:
        with urlopen(request, timeout=600) as response:
            body = response.read().decode("utf-8", "replace")
    except HTTPError as exc:
        detail = redact_text(exc.read().decode("utf-8", "replace")).strip()
        raise Fail(f"agent publish failed: HTTP {exc.code} {publish}: {detail or exc.reason}") from exc
    except URLError as exc:
        raise Fail(f"agent publish failed: {publish}: {exc.reason}") from exc
    if body.strip():
        print(redact_text(body).strip())
    return 0


def release_incomplete_steps(state: dict[str, Any], *, via_ci: bool = False) -> list[str]:
    missing: list[str] = []
    for step in REQUIRED_FOR_RELEASE:
        if step == "ops.retrospect" and via_ci:
            continue
        status = state["steps"][step]["status"]
        allowed = ("confirmed", "verified") if step in DECISION_STEPS else ("verified",)
        if step == "pack.ci" and via_ci:
            allowed = ("confirmed", "verified")
        if status not in allowed:
            missing.append(step)
    return missing


def cmd_release(root: Path, args: argparse.Namespace) -> int:
    state = load_state(root)
    report = reconcile(root, state, write=True)
    if report.get("drift"):
        raise Fail("release refused: unresolved drift\n  " + "\n  ".join(report["drift"]))
    if report.get("unconfirmed"):
        raise Fail(
            "release refused: cannot confirm remote state\n  "
            + "\n  ".join(report["unconfirmed"])
        )
    via_ci = os.environ.get("RELKIT_RELEASE_VIA_CI") == "1"
    missing = release_incomplete_steps(state, via_ci=via_ci)
    if missing:
        raise Fail("release refused: incomplete steps: " + ", ".join(missing))
    binary = relkit_bin(root)
    version_argv = [str(binary), "version", "get"]
    current = subprocess.run(
        version_argv,
        cwd=str(root),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    if current.returncode != 0:
        raise Fail(redact_text(current.stderr or current.stdout or "relkit version failed"))
    version = (current.stdout or "").strip().splitlines()[-1] if current.stdout else ""
    print(f"relkit {binary}")
    print(f"version {version}")
    agent = agent_base_url(root)
    if agent:
        if args.execute and not via_ci:
            raise Fail(
                "agent publish is CI-only; scripts/ci_release.mjs must set "
                "RELKIT_RELEASE_VIA_CI=1"
            )
        if args.execute:
            removed = clear_stale_staged_trees(root, version)
            if removed:
                print("removed stale staged caches: " + ", ".join(removed))
        return publish_via_agent(
            root,
            binary,
            product=product_id(state),
            version=version,
            url=agent,
            execute=bool(args.execute),
        )
    print("dry-run publish")
    dry = subprocess.run(
        [str(binary), "publish", "--dry-run"],
        cwd=str(root),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    print(redact_text(dry.stdout or dry.stderr or ""))
    if dry.returncode != 0:
        raise Fail("relkit publish --dry-run failed")
    if not args.execute:
        print("pass --execute to publish for real; index write still happens only then")
        return 0
    print("publishing (index is written last)")
    real = subprocess.run(
        [str(binary), "publish"],
        cwd=str(root),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    print(redact_text(real.stdout or real.stderr or ""))
    if real.returncode != 0:
        raise Fail("relkit publish failed")
    print("verify")
    verify = subprocess.run(
        [str(binary), "verify"],
        cwd=str(root),
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
    )
    print(redact_text(verify.stdout or verify.stderr or ""))
    if verify.returncode != 0:
        raise Fail("relkit verify failed after publish")
    return 0


def cmd_fake_verify(root: Path, version: Optional[str]) -> int:
    binary = relkit_bin(root)
    resolved = version
    if not resolved:
        current = run_relkit(root, binary, ["version", "get"])
        resolved = (current.stdout or "").strip().splitlines()[-1]
    if not resolved:
        raise Fail("fake verify needs a version", code="fake-verify-no-version")
    staged = cache_dir(root) / "staged" / resolved
    if not (staged / "staged.pb").is_file():
        print(f"no staged tree for {resolved}; staging dummy zip")
        stage_dummy_release(root, resolved, binary)
    run_relkit(root, binary, ["simulate", "--with-staged", resolved, "--from", "all"])
    state = load_state(root)
    set_step(
        state,
        "fake.release",
        "verified",
        resolved,
        "stage+simulate verified; publish remains CI-only",
    )
    save_state(root, state)
    print(f"verified fake.release={resolved}")
    return 0


def serve_token_path(
    config_dir: str, product: str, share_with: Optional[str] = None
) -> str:
    """On-disk token after init. -share-with keeps the existing owner's file."""
    owner = (share_with or product or "").strip()
    if not owner:
        raise Fail("token owner product id is required")
    return str(Path(config_dir) / "tokens" / f"{owner}.token").replace("\\", "/")


def agent_token_path(product: str, share_with: Optional[str] = None) -> str:
    owner = (share_with or product or "").strip()
    if not owner:
        raise Fail("token owner product id is required")
    return "/etc/relkit-agent/tokens/" + owner + ".token"


def chown_serve_product_token(
    host: str,
    config_dir: str,
    product: str,
    share_with: Optional[str] = None,
) -> None:
    token_path = serve_token_path(config_dir, product, share_with)
    ssh_run(host, ["sudo", "chown", "relkit:relkit", token_path])
    ssh_run(host, ["sudo", "chmod", "0600", token_path])
    print(f"chown relkit:relkit {token_path}")


def cmd_serve_list(root: Path) -> int:
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
        ["sudo", "relkit-serve", "init", "-out", str(config_dir), "-list-products"],
    )
    print(redact_text(out.stdout))
    return 0


def cmd_serve_add(root: Path, args: argparse.Namespace) -> int:
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
        "relkit-serve",
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


def cmd_serve_restart(root: Path, args: argparse.Namespace) -> int:
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


def cmd_agent_restart(root: Path, args: argparse.Namespace) -> int:
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


def cmd_serve_rotate(root: Path, args: argparse.Namespace) -> int:
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
            "relkit-serve",
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


def cmd_serve_remove(root: Path, args: argparse.Namespace) -> int:
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
            "relkit-serve",
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


def cmd_agent_list(root: Path) -> int:
    state = load_state(root)
    host = (state.get("agent") or {}).get("sshHost") or (state.get("serve") or {}).get("sshHost")
    config_path = (state.get("agent") or {}).get("configPath") or DEFAULT_AGENT_CONFIG
    if not host:
        raise Fail("no agent/serve ssh host recorded")
    out = ssh_run(
        host,
        ["sudo", "relkit-agent", "init", "-config", str(config_path), "-list-products"],
    )
    print(redact_text(out.stdout))
    return 0


def cmd_agent_add(root: Path, args: argparse.Namespace) -> int:
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
        "relkit-agent",
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


def cmd_agent_provision(root: Path, args: argparse.Namespace) -> int:
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
                "relkit-agent",
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


def cmd_agent_remove(root: Path, args: argparse.Namespace) -> int:
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
            "relkit-agent",
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


def run_relkit(root: Path, binary: Path, argv: Sequence[str]) -> subprocess.CompletedProcess[str]:
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


def cmd_keys_gen(root: Path, args: argparse.Namespace) -> int:
    require_execute(args, "keys gen")
    ensure_gitignore(root)
    state = load_state(root)
    product = args.product or product_id(state)
    key_id = args.key_id or "k1"
    out_dir = args.out or ".relkit-keys"
    binary = relkit_bin(root, getattr(args, "bin", None))
    config = root / "relkit.json"
    if not config.is_file():
        run_relkit(root, binary, ["init", "--product", product])
    plain = root / "VERSION"
    if plain.is_file():
        raw = plain.read_text(encoding="utf-8").strip().splitlines()[0].strip()
        if raw:
            run_relkit(root, binary, ["version", "set", raw])
            set_step(
                state,
                "channel.ssot",
                "applied",
                "migrate:VERSION->VERSION.json",
                f"VERSION.json={raw}",
            )
    run_relkit(
        root,
        binary,
        ["keygen", "--key-id", key_id, "--out", out_dir, "--update-config"],
    )
    pub = Path(out_dir) / f"{key_id}.public.pb"
    set_step(state, "signing.keys", "applied", key_id, pub.as_posix())
    save_state(root, state)
    print(f"recorded signing.keys={key_id}; private key is gitignored")
    return 0


def routing_help() -> str:
    return """relkit_host.py — product-repo ops gate. Prints routing only; confirms nothing.

  onboard start|resume [--interactive|--non-interactive]
  onboard inspect|questions|apply|explain|set|reset
  retrospect
  install
  upgrade vX.Y.Z
  fake verify [--version X.Y.Z+N]
  release [--execute]
  serve list|add|restart|rotate|remove
  agent list|add|provision|restart|remove
  keys gen
  status [--json]
  verify [--all]

CI must name a subcommand. Mutations need --execute. Restarts need --restart.

Empty-machine install / binary replace lives in the relkit repo:
  python deploy/relkit.py build|install|upgrade
"""


def _retrospect_skill_paths(root: Path) -> list[Path]:
    candidates = (
        root / "skills" / "relkit-ops" / "SKILL.md",
        root / ".cursor" / "skills" / "relkit-ops" / "SKILL.md",
        root / ".cursor" / "skills" / "dec-relkit-ops" / "SKILL.md",
        root / ".codebuddy" / "skills" / "relkit-ops" / "SKILL.md",
    )
    return [path for path in candidates if path.is_file()]


def _retrospect_item(
    outcome: str,
    check: str,
    path: str,
    expected: str,
    actual: str,
) -> dict[str, str]:
    return {
        "outcome": outcome,
        "check": check,
        "path": path,
        "expected": expected,
        "actual": actual,
    }


def retrospect_report(root: Path) -> dict[str, Any]:
    """Classify every mechanical ops-contract check without prompting or mutation."""
    items: list[dict[str, str]] = []
    host_path = "scripts/host/relkit_host.py"

    def check(
        check_id: str,
        path: str,
        expected: str,
        actual: str,
        passed: bool,
    ) -> None:
        items.append(
            _retrospect_item(
                "landed" if passed else "todo",
                check_id,
                path,
                expected,
                actual,
            )
        )

    recommendation_keys = set(recommendations(root, default_state(root)))
    step_ids = set(STEP_IDS)
    check(
        "step-explanations",
        host_path,
        "EXPLAIN_TEXTS keys exactly match STEP_IDS",
        f"STEP_IDS={sorted(step_ids)}; EXPLAIN_TEXTS={sorted(EXPLAIN_TEXTS)}",
        step_ids == set(EXPLAIN_TEXTS),
    )
    check(
        "step-recommendations",
        host_path,
        "recommendations keys exactly match STEP_IDS",
        f"STEP_IDS={sorted(step_ids)}; recommendations={sorted(recommendation_keys)}",
        step_ids == recommendation_keys,
    )
    for step_id in STEP_IDS:
        try:
            explain_step(step_id)
            recommend(root, default_state(root), step_id)
        except (Fail, KeyError) as error:
            check(
                f"step-entry:{step_id}",
                host_path,
                f"{step_id} has callable explain and recommend entries",
                f"entry failed: {error}",
                False,
            )

    expected_ignores = {
        ".relkit/cache/",
        ".relkit-keys/*.private.pb",
        "__pycache__/",
    }
    check(
        "gitignore-contract",
        host_path,
        f"GITIGNORE_RELKIT={sorted(expected_ignores)}",
        f"GITIGNORE_RELKIT={sorted(GITIGNORE_RELKIT)}",
        set(GITIGNORE_RELKIT) == expected_ignores,
    )
    with tempfile.TemporaryDirectory() as raw:
        probe = Path(raw)
        ensure_gitignore(probe)
        ignore = (probe / ".gitignore").read_text(encoding="utf-8").splitlines()
        check(
            "cache-ignore",
            host_path,
            "ensure_gitignore writes .relkit/cache/",
            f"generated lines={ignore}",
            ".relkit/cache/" in ignore,
        )
        check(
            "onboarding-tracked",
            host_path,
            "ensure_gitignore does not ignore .relkit/onboarding.json",
            f"generated lines={ignore}",
            not any("onboarding.json" in line for line in ignore),
        )

    routes = routing_help()
    canonical_deploy = "python deploy/relkit.py build|install|upgrade"
    check(
        "canonical-deploy-route",
        host_path,
        f"routing_help contains {canonical_deploy!r}",
        "canonical deploy route present" if canonical_deploy in routes else "canonical deploy route missing",
        canonical_deploy in routes,
    )
    forbidden_routes = ("scripts/relkit_host.py", "deploy/relkit.py serve", "deploy/relkit.py agent")
    found_forbidden = [item for item in forbidden_routes if item in routes]
    check(
        "canonical-product-routes",
        host_path,
        f"routing_help excludes {list(forbidden_routes)}",
        f"forbidden routes found={found_forbidden}",
        not found_forbidden,
    )

    check(
        "updater-process-vocabulary",
        host_path,
        "UPDATER_PROCESS_VALUES includes rust and excludes rust-shell",
        f"UPDATER_PROCESS_VALUES={list(UPDATER_PROCESS_VALUES)}",
        "rust" in UPDATER_PROCESS_VALUES and "rust-shell" not in UPDATER_PROCESS_VALUES,
    )

    upgrade_source = inspect.getsource(cmd_upgrade)
    upgrade_builds_lock = (
        "build_release_lock(" in upgrade_source
        and "must use" not in upgrade_source
    )
    check(
        "lock-bootstrap-from-release",
        host_path,
        "cmd_upgrade builds the whole lock from the release, so a v1 lock upgrades directly",
        (
            "upgrade constructs the lock from release metadata"
            if upgrade_builds_lock
            else "upgrade still requires the previous lock to already be relkit.consume/2"
        ),
        upgrade_builds_lock,
    )

    with tempfile.TemporaryDirectory() as raw:
        probe = Path(raw)
        (probe / "go.mod").write_text("module example.test\n", encoding="utf-8")
        go_components = consume_components(probe)
        check(
            "go-sdk-component",
            host_path,
            "a Go host repo installs the sdk-go artifact",
            f"consume_components={go_components}",
            "sdk-go" in go_components,
        )
    probe_artifacts: dict[str, Any] = {}
    rewrite_lock_artifacts(
        probe_artifacts,
        "https://example.test/download",
        {"relkit-sdk-go.zip": "0" * 64},
    )
    check(
        "go-sdk-lock-entry",
        host_path,
        "rewrite_lock_artifacts pins relkit-sdk-go.zip",
        f"artifacts={sorted(probe_artifacts)}",
        "sdk-go" in probe_artifacts,
    )

    inspect_source = inspect.getsource(env_inspect_report)
    detects_ignored_record = "onboarding-json-ignored" in inspect_source
    check(
        "onboarding-record-committable",
        host_path,
        "env.inspect fails when gitignore excludes .relkit/onboarding.json",
        (
            "inspect reports onboarding-json-ignored"
            if detects_ignored_record
            else "inspect never checks whether the decision record is ignored"
        ),
        detects_ignored_record,
    )

    consume_path = "scripts/host/relkit_consume.py"
    consume_module = import_consume()
    replace_source = inspect.getsource(consume_module.safe_extract_sdk)
    forces_removal = (
        "remove_tree(" in replace_source
        and "shutil.rmtree(" not in replace_source
    )
    check(
        "readonly-tree-replacement",
        consume_path,
        "SDK replacement deletes read-only trees such as a sparse checkout's .git",
        (
            "replacement routes deletions through remove_tree"
            if forces_removal
            else "replacement calls shutil.rmtree directly and fails on read-only files"
        ),
        forces_removal,
    )

    sidecar_source = inspect.getsource(reconcile_sidecar_layout)
    sidecar_is_config_driven = (
        'sidecar.get("packScript")' in sidecar_source
        and "root / pack_script" in sidecar_source
    )
    check(
        "generic-sidecar-verification",
        host_path,
        "reconcile_sidecar_layout resolves packScript from relkit.json",
        (
            "packScript is resolved from relkit.json"
            if sidecar_is_config_driven
            else "config-driven sidecar.packScript resolution is missing"
        ),
        sidecar_is_config_driven,
    )

    if not callable(clear_stale_staged_trees):
        check(
            "stale-stage-cleanup",
            host_path,
            "clear_stale_staged_trees is callable and removes only stale trees",
            "clear_stale_staged_trees is not callable",
            False,
        )
    else:
        with tempfile.TemporaryDirectory() as raw:
            probe = Path(raw)
            stale = cache_dir(probe) / "staged" / "old"
            current = cache_dir(probe) / "staged" / "current"
            stale.mkdir(parents=True)
            current.mkdir(parents=True)
            removed = clear_stale_staged_trees(probe, "current")
            passed = removed == ["old"] and not stale.exists() and current.is_dir()
            check(
                "stale-stage-cleanup",
                host_path,
                "remove old and preserve current staged tree",
                (
                    f"removed={removed}; old_exists={stale.exists()}; "
                    f"current_exists={current.is_dir()}"
                ),
                passed,
            )

    skill_paths = _retrospect_skill_paths(root)
    if not skill_paths:
        items.append(
            _retrospect_item(
                "skipped",
                "skill-gate",
                "skills/relkit-ops/SKILL.md",
                "installed relkit-ops skill requires relkit_host.py retrospect",
                "not applicable: no relkit-ops skill is installed in this repository",
            )
        )
    for skill_path in skill_paths:
        text = skill_path.read_text(encoding="utf-8")
        display_path = skill_path.relative_to(root).as_posix()
        requires_command = "relkit_host.py retrospect" in text
        check(
            f"skill-command:{display_path}",
            display_path,
            "skill requires relkit_host.py retrospect",
            (
                "required command present"
                if requires_command
                else "required command relkit_host.py retrospect is missing"
            ),
            requires_command,
        )
        prose_gate = bool(re.search(r"读.*RETROSPECT\.md.*(?:宣称|完成)", text))
        check(
            f"skill-mechanical-gate:{display_path}",
            display_path,
            "reading RETROSPECT.md alone is not treated as completion",
            (
                "RETROSPECT.md reading is incorrectly sufficient"
                if prose_gate
                else "completion remains tied to the mechanical command"
            ),
            not prose_gate,
        )

    encountered, digested, undigested = classify_ops_journal(root)
    return {
        "schema": "relkit.retrospect/2",
        "root": str(root),
        "groups": {
            "landed": [item for item in items if item["outcome"] == "landed"],
            "todo": [item for item in items if item["outcome"] == "todo"],
            "skipped": [item for item in items if item["outcome"] == "skipped"],
            "encountered": encountered,
            "digested": digested,
            "undigested": undigested,
        },
    }


def classify_ops_journal(
    root: Path,
) -> tuple[list[dict[str, str]], list[dict[str, str]], list[dict[str, str]]]:
    seen: list[str] = []
    for row in load_ops_journal(root):
        code = str(row.get("code") or "").strip()
        if not code or code == "unclassified" or code in seen:
            continue
        seen.append(code)
    encountered: list[dict[str, str]] = []
    digested: list[dict[str, str]] = []
    undigested: list[dict[str, str]] = []
    journal = ".relkit/cache/ops-journal.jsonl"
    for code in seen:
        item = _retrospect_item(
            "landed" if code in DIGESTED_ISSUE_CODES else "todo",
            f"ops-journal:{code}",
            journal,
            "issue class already digested into host.py or skill",
            code,
        )
        encountered.append(item)
        if code in DIGESTED_ISSUE_CODES:
            digested.append(item)
        else:
            undigested.append(item)
    return encountered, digested, undigested


def _retrospect_line(item: dict[str, str]) -> str:
    return (
        f"- [{item['check']}] 文件: {item['path']} | "
        f"期望: {item['expected']} | 现状: {item['actual']}"
    )


def retrospect_failures(root: Path) -> list[str]:
    """Compatibility view of actionable todo lines."""
    return [_retrospect_line(item) for item in retrospect_report(root)["groups"]["todo"]]


def print_retrospect_report(report: dict[str, Any]) -> None:
    labels = (
        ("landed", "已落地"),
        ("todo", "待办"),
        ("skipped", "跳过"),
        ("encountered", "本次遇到"),
        ("digested", "已修进脚本或 skill"),
        ("undigested", "未消化"),
    )
    for key, label in labels:
        print(label)
        items = report["groups"][key]
        if items:
            for item in items:
                print(_retrospect_line(item))
        else:
            print("- 无")


def cmd_retrospect(root: Path, as_json: bool = False) -> int:
    report = retrospect_report(root)
    if as_json:
        print(dump_json(report), end="")
    else:
        print_retrospect_report(report)
    if report["groups"]["todo"] or report["groups"]["undigested"]:
        return 1
    state = load_state(root)
    set_step(
        state,
        "ops.retrospect",
        "verified",
        "relkit_host.py retrospect",
        "mechanical ops checks passed",
    )
    save_state(root, state)
    if not as_json:
        print("结果: 通过；ops.retrospect=verified")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="relkit_host.py",
        description="Product-repo relkit ops. Does not install systemd units.",
        add_help=True,
    )
    parser.add_argument("--project-root", help="product repository root")
    sub = parser.add_subparsers(dest="cmd")

    onboard = sub.add_parser("onboard", help="decision wizard; does not mutate remotes")
    onboard_sub = onboard.add_subparsers(dest="onboard_cmd", required=True)
    start = onboard_sub.add_parser("start")
    start_mode = start.add_mutually_exclusive_group()
    start_mode.add_argument("--interactive", action="store_true")
    start_mode.add_argument("--non-interactive", action="store_true")
    resume = onboard_sub.add_parser("resume")
    resume_mode = resume.add_mutually_exclusive_group()
    resume_mode.add_argument("--interactive", action="store_true")
    resume_mode.add_argument("--non-interactive", action="store_true")
    explain = onboard_sub.add_parser("explain")
    explain.add_argument("step")
    setter = onboard_sub.add_parser("set")
    setter.add_argument("step")
    setter.add_argument("value", nargs="?")
    setter.add_argument("--share-with")
    setter.add_argument("--note", help="short product fact only; not an architecture essay")
    reset = onboard_sub.add_parser("reset", help="delete product .relkit/; does not write docs")
    reset.add_argument("--yes", action="store_true")
    onboard_sub.add_parser("inspect", help="dump env facts; error findings block product.id")
    questions = onboard_sub.add_parser(
        "questions", help="emit one batch of independent human decisions"
    )
    questions.add_argument("--intent", required=True, choices=ONBOARD_INTENTS)
    questions.add_argument("--json", action="store_true")
    apply_answers = onboard_sub.add_parser(
        "apply", help="validate and atomically record one answer batch"
    )
    apply_answers.add_argument("--answers", required=True)

    sub.add_parser("install", help="install lock-pinned artifacts via relkit_consume.py")
    retrospect = sub.add_parser(
        "retrospect", help="run the final non-interactive ops consistency gate"
    )
    retrospect.add_argument("--json", action="store_true")
    upgrade = sub.add_parser("upgrade", help="rewrite lock to a GitHub release and install")
    upgrade.add_argument("release")

    fake = sub.add_parser("fake", help="verify staged release wiring without publishing")
    fake_sub = fake.add_subparsers(dest="fake_cmd", required=True)
    fake_verify = fake_sub.add_parser("verify")
    fake_verify.add_argument("--version")

    release = sub.add_parser("release", help="publish if onboard is verified and there is no drift")
    release.add_argument("--execute", action="store_true")

    serve = sub.add_parser("serve")
    serve_sub = serve.add_subparsers(dest="serve_cmd", required=True)
    serve_sub.add_parser("list")
    serve_restart = serve_sub.add_parser("restart")
    serve_restart.add_argument("--host")
    serve_restart.add_argument("--execute", action="store_true")
    serve_restart.add_argument("--restart", action="store_true")
    serve_add = serve_sub.add_parser("add")
    serve_add.add_argument("--product")
    serve_add.add_argument("--share-with")
    serve_add.add_argument("--host")
    serve_add.add_argument("--config-dir")
    serve_add.add_argument("--execute", action="store_true")
    serve_add.add_argument("--restart", action="store_true")
    serve_rot = serve_sub.add_parser("rotate")
    serve_rot.add_argument("--product")
    serve_rot.add_argument("--host")
    serve_rot.add_argument("--config-dir")
    serve_rot.add_argument("--execute", action="store_true")
    serve_rot.add_argument("--restart", action="store_true")
    serve_rm = serve_sub.add_parser("remove")
    serve_rm.add_argument("--product")
    serve_rm.add_argument("--host")
    serve_rm.add_argument("--config-dir")
    serve_rm.add_argument("--execute", action="store_true")
    serve_rm.add_argument("--restart", action="store_true")

    agent = sub.add_parser("agent")
    agent_sub = agent.add_subparsers(dest="agent_cmd", required=True)
    agent_sub.add_parser("list")
    agent_restart = agent_sub.add_parser("restart")
    agent_restart.add_argument("--product")
    agent_restart.add_argument("--host")
    agent_restart.add_argument("--root-path")
    agent_restart.add_argument("--execute", action="store_true")
    agent_restart.add_argument("--restart", action="store_true")
    agent_add = agent_sub.add_parser("add")
    agent_add.add_argument("--product")
    agent_add.add_argument("--host")
    agent_add.add_argument("--config")
    agent_add.add_argument("--root-path")
    agent_add.add_argument("--share-with")
    agent_add.add_argument("--execute", action="store_true")
    agent_add.add_argument("--restart", action="store_true")
    agent_prov = agent_sub.add_parser("provision")
    agent_prov.add_argument("--product")
    agent_prov.add_argument("--host")
    agent_prov.add_argument("--config")
    agent_prov.add_argument("--root-path")
    agent_prov.add_argument("--execute", action="store_true")
    agent_prov.add_argument("--restart", action="store_true")
    agent_rm = agent_sub.add_parser("remove")
    agent_rm.add_argument("--product")
    agent_rm.add_argument("--host")
    agent_rm.add_argument("--config")
    agent_rm.add_argument("--execute", action="store_true")

    keys = sub.add_parser("keys")
    keys_sub = keys.add_subparsers(dest="keys_cmd", required=True)
    keys_gen = keys_sub.add_parser("gen")
    keys_gen.add_argument("--product")
    keys_gen.add_argument("--key-id", default="k1")
    keys_gen.add_argument("--out", default=".relkit-keys")
    keys_gen.add_argument("--bin")
    keys_gen.add_argument("--execute", action="store_true")

    status = sub.add_parser("status")
    status.add_argument("--json", action="store_true")
    verify = sub.add_parser("verify")
    verify.add_argument("--all", action="store_true")
    verify.add_argument("--json", action="store_true")
    return parser


def dispatch(root: Path, args: argparse.Namespace) -> int:
    if not args.cmd:
        print(routing_help())
        return 0
    if args.cmd == "onboard":
        interactive = bool(
            getattr(args, "interactive", False)
            or (
                sys.stdin.isatty()
                and not getattr(args, "non_interactive", False)
            )
        )
        if args.onboard_cmd == "start":
            return cmd_onboard_start(root, interactive=interactive)
        if args.onboard_cmd == "resume":
            return cmd_onboard_resume(root, interactive=interactive)
        if args.onboard_cmd == "explain":
            return cmd_onboard_explain(args.step)
        if args.onboard_cmd == "reset":
            return cmd_onboard_reset(root, yes=bool(getattr(args, "yes", False)))
        if args.onboard_cmd == "inspect":
            return cmd_onboard_inspect(root)
        if args.onboard_cmd == "questions":
            return cmd_onboard_questions(root, args.intent, args.json)
        if args.onboard_cmd == "apply":
            return cmd_onboard_apply(root, Path(args.answers))
        return cmd_onboard_set(
            root,
            args.step,
            args.value,
            args.share_with,
            getattr(args, "note", None),
        )
    if args.cmd == "install":
        return cmd_install(root, [])
    if args.cmd == "retrospect":
        return cmd_retrospect(root, args.json)
    if args.cmd == "upgrade":
        return cmd_upgrade(root, args.release)
    if args.cmd == "fake":
        return cmd_fake_verify(root, args.version)
    if args.cmd == "release":
        return cmd_release(root, args)
    if args.cmd == "serve":
        if args.serve_cmd == "list":
            return cmd_serve_list(root)
        if args.serve_cmd == "restart":
            return cmd_serve_restart(root, args)
        if args.serve_cmd == "add":
            return cmd_serve_add(root, args)
        if args.serve_cmd == "rotate":
            return cmd_serve_rotate(root, args)
        return cmd_serve_remove(root, args)
    if args.cmd == "agent":
        if args.agent_cmd == "list":
            return cmd_agent_list(root)
        if args.agent_cmd == "restart":
            return cmd_agent_restart(root, args)
        if args.agent_cmd == "add":
            return cmd_agent_add(root, args)
        if args.agent_cmd == "provision":
            return cmd_agent_provision(root, args)
        return cmd_agent_remove(root, args)
    if args.cmd == "keys":
        return cmd_keys_gen(root, args)
    if args.cmd == "status":
        return cmd_status(root, args.json)
    if args.cmd == "verify":
        state = load_state(root)
        report = reconcile(root, state, write=True)
        if args.json:
            print(dump_json(report))
        else:
            cmd_status(root, False)
        if report.get("drift") or report.get("unconfirmed"):
            return 1
        return 0
    raise Fail(f"unknown command {args.cmd}")


def main(argv: Optional[Sequence[str]] = None) -> int:
    force_utf8_stdio()
    raw = list(argv) if argv is not None else sys.argv[1:]
    root = host_root()
    try:
        parser = build_parser()
        args = parser.parse_args(raw)
        root = (
            Path(args.project_root).resolve()
            if getattr(args, "project_root", None)
            else host_root()
        )
        return dispatch(root, args)
    except Fail as error:
        print(f"ERROR: {error}", file=sys.stderr)
        try:
            append_ops_journal(root, raw, error)
        except OSError:
            pass
        return 1
    except Exception as error:
        print(f"ERROR: relkit_host failed: {error}", file=sys.stderr)
        print(traceback.format_exc(), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
