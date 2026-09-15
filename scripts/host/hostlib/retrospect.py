"""Implementation cluster: retrospect."""

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


def _impl__retrospect_skill_paths(root: Path) -> list[Path]:
    candidates = (
        root / "DecAssets" / "skills" / "relkit-ops" / "SKILL.md",
        root / "skills" / "relkit-ops" / "SKILL.md",
        root / ".cursor" / "skills" / "relkit-ops" / "SKILL.md",
        root / ".cursor" / "skills" / "dec-relkit-ops" / "SKILL.md",
        root / ".codebuddy" / "skills" / "relkit-ops" / "SKILL.md",
    )
    return [path for path in candidates if path.is_file()]

def _impl__retrospect_item(
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

def _impl_retrospect_report(root: Path) -> dict[str, Any]:
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
    canonical_deploy = "python scripts/deploy/relkit.py build|install|upgrade"
    check(
        "canonical-deploy-route",
        host_path,
        f"routing_help contains {canonical_deploy!r}",
        "canonical deploy route present" if canonical_deploy in routes else "canonical deploy route missing",
        canonical_deploy in routes,
    )
    forbidden_routes = (
        "scripts/relkit_host.py",
        "scripts/deploy/relkit.py serve",
        "scripts/deploy/relkit.py agent",
    )
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

    evidence_gates_questions = callable(build_question_batch) and callable(decision_options)
    check(
        "decision-evidence-before-questions",
        host_path,
        "question batches use live topology/inventory and never invent share-with ids",
        (
            "questions are applicability-filtered and share-with ids come from remote products"
            if evidence_gates_questions
            else "questions can be emitted before live evidence or with placeholder product ids"
        ),
        evidence_gates_questions,
    )
    direct_skips_remote = callable(release_incomplete_steps)
    check(
        "direct-publish-applicability",
        host_path,
        "direct backend releases do not require serve/agent token steps",
        (
            "direct topology removes remote token steps from the release gate"
            if direct_skips_remote
            else "release still requires remote token steps for direct backends"
        ),
        direct_skips_remote,
    )

    upgrade_builds_lock = callable(cmd_upgrade) and callable(build_release_lock)
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

    # Matches sudo followed directly by a bare binary name. systemctl unit
    # names are safe because sudo is followed by systemctl there.
    sudo_bare_name = not (
        SERVE_BIN.startswith("/") and AGENT_BIN.startswith("/")
    )
    check(
        "sudo-absolute-remote-binaries",
        host_path,
        "remote sudo calls spell out /usr/local/bin, which sudoers secure_path excludes",
        (
            "sudo invokes relkit-serve/relkit-agent by bare name"
            if sudo_bare_name
            else f"remote binaries resolve to {SERVE_BIN} and {AGENT_BIN}"
        ),
        not sudo_bare_name,
    )

    detects_ignored_record = "onboarding-json-ignored" in DIGESTED_ISSUE_CODES
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
    forces_removal = callable(consume_module.remove_tree)
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

    sidecar_is_config_driven = callable(reconcile_sidecar_layout)
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
    check(
        "registered-product-gates",
        "scripts/host/hostlib/gates.py",
        "inspect/reconcile/retrospect gates are registered functions",
        f"registered={sorted(GATES)}",
        {"updater-sdk-contract", "webview-projection", "other-declarations",
         "updater-chokepoints", "legacy-in-process-updater"}.issubset(GATES),
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
                "DecAssets/skills/relkit-ops/SKILL.md",
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
        evidence_first = (
            "evidence.topology" in text
            and "evidence.remote" in text
            and "`blocked` 中的决策本轮禁止询问" in text
            and "operatorTokenPresent=true" in text
        )
        check(
            f"skill-evidence-first:{display_path}",
            display_path,
            "skill requires showing live evidence before dependent questions",
            (
                "topology, remote inventory, blocked decisions and operator/product distinction are required"
                if evidence_first
                else "skill still permits dependent questions before live evidence"
            ),
            evidence_first,
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

def _impl_classify_ops_journal(
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

def _impl__retrospect_line(item: dict[str, str]) -> str:
    return (
        f"- [{item['check']}] 文件: {item['path']} | "
        f"期望: {item['expected']} | 现状: {item['actual']}"
    )

def _impl_retrospect_failures(root: Path) -> list[str]:
    """Compatibility view of actionable todo lines."""
    return [_retrospect_line(item) for item in retrospect_report(root)["groups"]["todo"]]

def _impl_print_retrospect_report(report: dict[str, Any]) -> None:
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

def _impl_cmd_retrospect(root: Path, as_json: bool = False) -> int:
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

_IMPLEMENTATIONS = {
    "_retrospect_skill_paths": _impl__retrospect_skill_paths,
    "_retrospect_item": _impl__retrospect_item,
    "retrospect_report": _impl_retrospect_report,
    "classify_ops_journal": _impl_classify_ops_journal,
    "_retrospect_line": _impl__retrospect_line,
    "retrospect_failures": _impl_retrospect_failures,
    "print_retrospect_report": _impl_print_retrospect_report,
    "cmd_retrospect": _impl_cmd_retrospect,
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
