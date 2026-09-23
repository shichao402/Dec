#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Thin parser/dispatch facade for the relkit product-repository host."""
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

# Must refuse before the hostlib import, which itself needs 3.9. Keep in sync
# with hostlib.const.MIN_PYTHON; retrospect checks both literals agree.
if sys.version_info < (3, 9):
    raise SystemExit(
        "relkit host scripts need Python >= 3.9, but this interpreter is "
        f"{sys.version_info.major}.{sys.version_info.minor}; "
        "point the entry script at a newer interpreter"
    )

from hostlib.const import *
from hostlib.facets import (
    BY_NAME,
    TARGETS,
    components as registry_components,
    detected_components,
    updater_process_values,
)
from hostlib.digest import tree_sha256 as _tree_sha256
from hostlib.gates import GATES, run_gates
from hostlib import runtime as _runtime


from hostlib.state import (
    force_utf8_stdio,
    host_root,
    host_scripts_dir,
    tree_sha256,
    relkit_dir,
    state_path,
    cache_dir,
    projection_path,
    local_path,
    empty_steps,
    default_state,
    load_json,
    dump_json,
    load_state,
    render_onboarding_md,
    write_onboarding_md,
    save_state,
    load_local,
    save_local,
    ensure_gitignore,
    redact_text,
    extract_export,
    set_step,
)
from hostlib.ssh import (
    _expand_ssh_path,
    _ssh_include_paths,
    parse_ssh_config,
    parse_ssh_config_ports,
    list_ssh_hosts,
    ssh_host_port,
    ssh_target,
    ssh_host_allowed,
    ssh_host_recommend,
    default_ssh_config_path,
    parse_known_host_names,
    collect_json_hosts,
    ssh_inventory,
    print_ssh_inventory,
    ssh_argv,
    ssh_run,
    ssh_write,
)
from hostlib.inspect import (
    inspect_path,
    inventory_path,
    ops_journal_path,
    onboarding_ignored,
    upstream_latest_release,
    lock_currency,
    env_inspect_report,
    print_env_inspect,
    apply_env_inspect,
    cmd_onboard_inspect,
    append_ops_journal,
    load_ops_journal,
)
from hostlib.onboard import (
    step_value,
    product_id,
    detect_stack,
    recommendations,
    recommend,
    explain_step,
    choice_hint,
    recommended_value,
    decision_options,
    question_batch_revision,
    build_question_batch,
    next_unresolved_step,
    apply_topology_to_state,
    reconcile_local_signing,
    print_decision,
    normalize_interactive_value,
    run_interactive_wizard,
    cmd_onboard_start,
    cmd_onboard_resume,
    cmd_onboard_explain,
    cmd_onboard_reset,
    apply_decision_to_state,
    cmd_onboard_set,
    cmd_onboard_questions,
    _answer_value,
    cmd_onboard_apply,
)
from hostlib.reconcile import (
    claim_remote_registration,
    reconcile_signing_profile,
    updater_sidecar_name,
    reconcile_sidecar_layout,
    github_release_workflow,
    github_workflow_publishes_relkit,
    reconcile_pack_ci,
    reconcile_fake_stage,
    clear_stale_staged_trees,
    reconcile,
)
from hostlib.remote import (
    ssh_path_exists,
    agent_profile_path,
    agent_upload_url,
    to_agent_backend,
    read_agent_profile,
    extract_publish_profile,
    machine_publish_config,
    parse_list_products,
    parse_product_token_files,
    token_file_abs,
    listed_product_token_path,
    list_serve_products,
    list_agent_products,
    publish_topology,
    _version_tuple,
    remote_inventory,
    decision_evidence,
    parse_agent_products,
    require_execute,
    require_restart_flag,
    write_secret,
    archive_secret,
    relkit_bin,
    cmd_status,
    agent_token_path,
    chown_token_file,
    chown_serve_product_token,
    chown_agent_product_token,
    cmd_serve_list,
    cmd_serve_add,
    cmd_serve_restart,
    cmd_agent_restart,
    cmd_serve_rotate,
    cmd_serve_remove,
    cmd_agent_list,
    cmd_agent_add,
    cmd_agent_provision,
    cmd_agent_remove,
    run_relkit,
)
from hostlib.release import (
    import_consume,
    consume_components,
    require_host_scripts_match_lock,
    lock_artifact_names,
    cmd_install,
    cmd_sidecar_universal,
    _lipo_fuse,
    int_window,
    build_release_lock,
    cmd_upgrade,
    http_get,
    parse_sha256sums,
    release_manifest,
    release_commit,
    release_commit_from_manifest,
    rewrite_lock_artifacts,
    relkit_config,
    agent_base_url,
    publish_protocol_window,
    sanitize_upload_token,
    upload_token,
    publish_via_agent,
    release_incomplete_steps,
    cmd_release,
    relkit_cli_version,
    project_version_for_relkit,
    cmd_fake_verify,
    serve_token_path,
    cmd_keys_gen,
    dummy_stage_zip,
    stage_dummy_release,
    routing_help,
    release_pack_config,
    resolve_project_path,
    pack_script_command,
    run_release_pack_script,
    normalize_selectors,
    load_release_artifacts_manifest,
    resolve_ci_channel,
    cmd_ci_release,
)
from hostlib.retrospect import (
    _retrospect_skill_paths,
    _retrospect_item,
    retrospect_report,
    cmd_retrospect_note,
    classify_ops_journal,
    _retrospect_line,
    retrospect_failures,
    print_retrospect_report,
    cmd_retrospect,
)

_runtime.bind(sys.modules[__name__])

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
    sidecar = sub.add_parser(
        "sidecar", help="build lock-pinned sidecar shapes install cannot place"
    )
    sidecar_sub = sidecar.add_subparsers(dest="sidecar_cmd", required=True)
    sidecar_universal = sidecar_sub.add_parser(
        "universal", help="fuse the lock's darwin updater attachments with lipo"
    )
    sidecar_universal.add_argument("--out", required=True)
    retrospect = sub.add_parser(
        "retrospect", help="run the final non-interactive ops consistency gate"
    )
    retrospect.add_argument("--json", action="store_true")
    retrospect_sub = retrospect.add_subparsers(dest="retrospect_cmd")
    retrospect_note = retrospect_sub.add_parser(
        "note", help="record a conversation finding the mechanical gate cannot see"
    )
    retrospect_note.add_argument("--code", required=True)
    retrospect_note.add_argument(
        "--class", dest="note_class", required=True, choices=list(RETROSPECT_NOTE_CLASSES)
    )
    retrospect_note.add_argument("--text", required=True)
    upgrade = sub.add_parser("upgrade", help="rewrite lock to a GitHub release and install")
    upgrade.add_argument("release")
    upgrade.add_argument("--finalize", action="store_true", help=argparse.SUPPRESS)

    fake = sub.add_parser("fake", help="verify staged release wiring without publishing")
    fake_sub = fake.add_subparsers(dest="fake_cmd", required=True)
    fake_verify = fake_sub.add_parser("verify")
    fake_verify.add_argument("--version")

    release = sub.add_parser("release", help="publish if onboard is verified and there is no drift")
    release.add_argument("--execute", action="store_true")

    ci = sub.add_parser("ci", help="CI-facing product release entry")
    ci_sub = ci.add_subparsers(dest="ci_cmd", required=True)
    ci_release = ci_sub.add_parser(
        "release",
        help="install → packScript → stage → simulate → fake verify → publish",
    )
    ci_release.add_argument("--channel", required=True)
    ci_release.add_argument("--execute", action="store_true")

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
    if args.cmd == "sidecar":
        return cmd_sidecar_universal(root, Path(args.out))
    if args.cmd == "retrospect":
        if getattr(args, "retrospect_cmd", None) == "note":
            return cmd_retrospect_note(root, args.code, args.note_class, args.text)
        return cmd_retrospect(root, args.json)
    if args.cmd == "upgrade":
        return cmd_upgrade(root, args.release, args.finalize)
    if args.cmd == "fake":
        return cmd_fake_verify(root, args.version)
    if args.cmd == "release":
        return cmd_release(root, args)
    if args.cmd == "ci":
        if args.ci_cmd == "release":
            return cmd_ci_release(root, args)
        raise Fail(f"unknown ci command {args.ci_cmd}")
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
