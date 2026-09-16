"""Implementation cluster: onboard."""

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


def _impl_step_value(state: dict[str, Any], step_id: str) -> Any:
    return state["steps"][step_id].get("value")

def _impl_product_id(state: dict[str, Any]) -> str:
    value = state.get("product") or step_value(state, "product.id")
    if not value:
        raise Fail("product.id is unanswered; run onboard set product.id <id>")
    return str(value)

def _impl_detect_stack(root: Path) -> dict[str, Any]:
    skip = {"node_modules", "third_party", "target", "dist", ".git", ".relkit"}
    signals: list[str] = []
    seen: set[str] = set()
    for row in registry_components("product-tree"):
        for pattern in row.detect:
            for path in root.glob(pattern):
                if not path.is_file():
                    continue
                relative = path.relative_to(root)
                if any(part in skip for part in relative.parts):
                    continue
                posix = relative.as_posix()
                if posix in seen:
                    continue
                seen.add(posix)
                signals.append(posix)
    rows = [BY_NAME[name] for name in detected_components(signals)]
    kind = list(dict.fromkeys(row.updater_process for row in rows if row.updater_process))
    updater = None
    if len(kind) > 1:
        updater = "choose rust or node (which process calls Updater.open)"
    elif kind:
        updater = kind[0]
    return {"languages": kind, "updater": updater, "root": str(root)}

def _impl_recommendations(root: Path, state: dict[str, Any]) -> dict[str, str]:
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
        "backend.kind": "choose: intranet-relkit-compatible / s3-compatible",
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

def _impl_recommend(root: Path, state: dict[str, Any], step_id: str) -> str:
    return recommendations(root, state)[step_id]

def _impl_explain_step(step_id: str) -> str:
    if step_id not in EXPLAIN_TEXTS:
        raise Fail(f"unknown step {step_id}")
    return EXPLAIN_TEXTS[step_id]

def _impl_choice_hint(root: Path, step_id: str) -> str:
    hints = {
        "product.id": "稳定的小写 ID，例如 loom；发布后不要随意改",
        "updater.process": " / ".join(UPDATER_PROCESS_VALUES),
        "channel.ssot": "migrate:VERSION->VERSION.json / VERSION.json / custom",
        "backend.kind": " / ".join(BACKEND_KIND_VALUES),
        "env.inspect": "clean error findings then onboard inspect; no typed confirmation",
        "ssh.host": ssh_host_recommend(root=root),
        "ssh.config_dir": "运行中服务日志实际打印的配置目录",
        "token.isolation": "exclusive / share-with:<existing-product>",
    }
    return hints.get(step_id, "")

def _impl_recommended_value(root: Path, state: dict[str, Any], step_id: str) -> Optional[str]:
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

def _impl_decision_options(
    root: Path, step_id: str, evidence: Optional[dict[str, Any]] = None
) -> list[str]:
    if step_id == "updater.process":
        return list(UPDATER_PROCESS_VALUES)
    if step_id == "channel.ssot":
        options = ["VERSION.json", "custom"]
        if (root / "VERSION").is_file() and not (root / "VERSION.json").is_file():
            options.insert(0, "migrate:VERSION->VERSION.json")
        return options
    if step_id == "backend.kind":
        return list(BACKEND_KIND_VALUES)
    if step_id == "ssh.host":
        inventory = ssh_inventory(root)
        values = list(inventory.get("exact") or [])
        for item in inventory.get("matched") or []:
            host = str(item.get("host") or "")
            if host and host not in values:
                values.append(host)
        return values
    if step_id == "token.isolation":
        options = ["exclusive"]
        remote = (evidence or {}).get("remote") or {}
        if remote.get("available"):
            options.extend(
                f"share-with:{product}"
                for product in remote.get("products") or []
            )
        return options
    return []

def _impl_question_batch_revision(
    root: Path,
    state: dict[str, Any],
    intent: str,
    evidence: Optional[dict[str, Any]] = None,
) -> str:
    inspection = env_inspect_report(root)
    resolved_evidence = evidence or decision_evidence(root, state)
    material = {
        "intent": intent,
        "inspect": inspection,
        "evidence": resolved_evidence,
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

def _impl_build_question_batch(
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
    evidence = decision_evidence(root, state)
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
    applicable = evidence.get("applicable") or {}
    blocked = evidence.get("blockedDecisions") or {}
    selected = [
        step
        for step in selected
        if applicable.get(step, True) and step not in blocked
    ]
    questions: list[dict[str, Any]] = []
    for step in selected:
        questions.append(
            {
                "id": step,
                "prompt": explain_step(step),
                "hint": choice_hint(root, step),
                "options": decision_options(root, step, evidence),
                "recommended": recommended_value(root, state, step),
                "current": state["steps"][step]["value"],
            }
        )
    return {
        "schema": QUESTION_BATCH_SCHEMA,
        "intent": intent,
        "revision": question_batch_revision(root, state, intent, evidence),
        "inspection": inspection,
        "evidence": evidence,
        "blocked": [
            {"id": step, "reason": reason}
            for step, reason in blocked.items()
        ],
        "questions": questions,
        "answerSchema": ANSWER_BATCH_SCHEMA,
        "answerShape": {
            "schema": ANSWER_BATCH_SCHEMA,
            "intent": intent,
            "revision": "<copy revision>",
            "answers": {item["id"]: "<value>" for item in questions},
        },
    }

def _impl_next_unresolved_step(state: dict[str, Any]) -> Optional[str]:
    return next(
        (
            step
            for step in STEP_IDS
            if state["steps"][step]["status"]
            in ("unanswered", "stale", "blocked", "drift")
        ),
        None,
    )

def _impl_apply_topology_to_state(root: Path, state: dict[str, Any]) -> dict[str, Any]:
    """Mark steps the configured publish path does not use.

    Direct backends still leave historical ssh/token answers in the record;
    skipping them is what keeps status from asking for a host that is no
    longer on the release route.
    """
    evidence = decision_evidence(root, state)
    applicable = evidence.get("applicable") or {}
    for step, needed in applicable.items():
        if needed or step not in STEP_IDS:
            continue
        current = state["steps"][step]
        if current["status"] == "skipped":
            continue
        set_step(
            state,
            step,
            "skipped",
            current.get("value"),
            "not on the configured publish route",
        )
    return evidence

def _impl_reconcile_local_signing(root: Path, state: dict[str, Any]) -> None:
    """Direct publishers keep the key id in relkit.json, not an agent profile."""
    if state["steps"]["signing.keys"]["status"] in ("verified",):
        return
    path = root / "relkit.json"
    if not path.is_file():
        return
    try:
        signing = load_json(path).get("signing") or {}
    except (OSError, json.JSONDecodeError, TypeError):
        return
    key_id = str(signing.get("keyId") or "").strip()
    public_keys = signing.get("publicKeys")
    if not key_id or not isinstance(public_keys, list) or not public_keys:
        return
    private = str(signing.get("privateKeyPath") or "").strip()
    note = "relkit.json names the key id and public key"
    if private:
        note += f"; private key at {private}"
    set_step(state, "signing.keys", "verified", key_id, note)

def _impl_print_decision(root: Path, state: dict[str, Any], step_id: str) -> None:
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

def _impl_normalize_interactive_value(step_id: str, raw: str) -> tuple[str, Optional[str]]:
    value = raw.strip()
    if step_id == "token.isolation" and value.startswith("share-with:"):
        shared = value.split(":", 1)[1].strip()
        if not shared:
            raise Fail("share-with 后必须给已有 product id")
        return "share-with", shared
    return value, None

def _impl_run_interactive_wizard(
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

def _impl_cmd_onboard_start(root: Path, *, interactive: bool = False) -> int:
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

def _impl_cmd_onboard_resume(root: Path, *, interactive: bool = False) -> int:
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

def _impl_cmd_onboard_explain(step_id: str) -> int:
    print(explain_step(step_id))
    return 0

def _impl_cmd_onboard_reset(root: Path, *, yes: bool = False) -> int:
    path = relkit_dir(root)
    if not yes:
        raise Fail("onboard reset deletes .relkit/; pass --yes")
    if path.exists():
        shutil.rmtree(path)
    print(f"deleted {path.as_posix()}")
    print("product onboarding is unanswered; no extra markdown was kept")
    return 0

def _impl_apply_decision_to_state(
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
        if value not in BACKEND_KIND_VALUES:
            raise Fail("backend.kind must be " + " / ".join(BACKEND_KIND_VALUES))
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

def _impl_cmd_onboard_set(
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

def _impl_cmd_onboard_questions(root: Path, intent: str, as_json: bool = False) -> int:
    batch = build_question_batch(root, load_state(root), intent)
    inventory_path(root).parent.mkdir(parents=True, exist_ok=True)
    inventory_path(root).write_text(
        dump_json(batch["evidence"]), encoding="utf-8"
    )
    if as_json:
        print(dump_json(batch), end="")
        return 0
    print(f"批量决策 intent={intent} revision={batch['revision']}")
    print("现场判断:")
    for implication in batch["evidence"].get("implications") or []:
        print(f"   - {implication}")
    for blocked in batch.get("blocked") or []:
        print(f"   - 暂不询问 {blocked['id']}: {blocked['reason']}")
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

def _impl__answer_value(
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

def _impl_cmd_onboard_apply(root: Path, answers_path: Path) -> int:
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
    questions_by_id = {item["id"]: item for item in batch["questions"]}
    if not conflicts:
        for step in expected:
            try:
                value, share_with, note = _answer_value(step, answers[step])
                if step == "token.isolation":
                    canonical = (
                        f"share-with:{share_with}"
                        if value == "share-with" and share_with
                        else value
                    )
                    allowed = questions_by_id[step].get("options") or []
                    if canonical not in allowed:
                        raise Fail(
                            "token choice is not supported by the live inventory; "
                            "regenerate questions after refreshing the remote evidence",
                            code="token-inventory-conflict",
                        )
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

_IMPLEMENTATIONS = {
    "step_value": _impl_step_value,
    "product_id": _impl_product_id,
    "detect_stack": _impl_detect_stack,
    "recommendations": _impl_recommendations,
    "recommend": _impl_recommend,
    "explain_step": _impl_explain_step,
    "choice_hint": _impl_choice_hint,
    "recommended_value": _impl_recommended_value,
    "decision_options": _impl_decision_options,
    "question_batch_revision": _impl_question_batch_revision,
    "build_question_batch": _impl_build_question_batch,
    "next_unresolved_step": _impl_next_unresolved_step,
    "apply_topology_to_state": _impl_apply_topology_to_state,
    "reconcile_local_signing": _impl_reconcile_local_signing,
    "print_decision": _impl_print_decision,
    "normalize_interactive_value": _impl_normalize_interactive_value,
    "run_interactive_wizard": _impl_run_interactive_wizard,
    "cmd_onboard_start": _impl_cmd_onboard_start,
    "cmd_onboard_resume": _impl_cmd_onboard_resume,
    "cmd_onboard_explain": _impl_cmd_onboard_explain,
    "cmd_onboard_reset": _impl_cmd_onboard_reset,
    "apply_decision_to_state": _impl_apply_decision_to_state,
    "cmd_onboard_set": _impl_cmd_onboard_set,
    "cmd_onboard_questions": _impl_cmd_onboard_questions,
    "_answer_value": _impl__answer_value,
    "cmd_onboard_apply": _impl_cmd_onboard_apply,
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
