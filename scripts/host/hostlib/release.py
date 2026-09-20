"""Implementation cluster: release."""

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
from .gates import GATES, run_gates, has_webview
from . import runtime as _runtime


def _impl_import_consume() -> Any:
    scripts = host_scripts_dir()
    if str(scripts) not in sys.path:
        sys.path.insert(0, str(scripts))
    import relkit_consume

    return relkit_consume

def _impl_consume_components(root: Path) -> list[str]:
    stack = detect_stack(root)
    languages = set(stack["languages"])
    trees = [
        row.name for row in registry_components("product-tree")
        if row.updater_process in languages
        or (row.webview_projection and has_webview(root))
    ]
    return [*dict.fromkeys(trees), "cli", "updater"]

def _impl_require_host_scripts_match_lock(root: Path) -> dict[str, Any]:
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

def _impl_lock_artifact_names(root: Path) -> set[str]:
    """Artifact keys the lock pins, or an empty set when the lock is unusable."""
    path = root / "scripts" / "relkit.lock.json"
    if not path.is_file():
        return set()
    try:
        artifacts = load_json(path).get("artifacts")
    except (OSError, json.JSONDecodeError, AttributeError):
        return set()
    return set(artifacts) if isinstance(artifacts, dict) else set()

def _impl_cmd_install(root: Path, consume_argv: Sequence[str]) -> int:
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

def _impl_cmd_sidecar_universal(root: Path, out: Path) -> int:
    """Fuse the lock's darwin updater attachments into one universal sidecar.

    macOS products ship a universal main binary, so the sidecar beside it must
    be universal too. ``install`` can only ever place the packaging machine's
    own architecture at ``tools/bin/relkit-updater``, and every target installs
    under that same name, so the two-architecture shape needs its own command
    instead of a product reaching into the pinned consumer.
    """
    if sys.platform != "darwin":
        raise Fail(
            "sidecar universal needs macOS lipo; run it on the macOS packaging job",
            code="sidecar-universal-not-darwin",
        )
    consume = import_consume()
    lock = consume.load_lock(root / "scripts" / "relkit.lock.json")
    with tempfile.TemporaryDirectory(prefix="relkit-sidecar-universal-") as raw:
        staged: list[Path] = []
        for target in ("darwin-amd64", "darwin-arm64"):
            try:
                spec = consume.artifact_spec(lock, "updater", target)
            except (RuntimeError, KeyError, ValueError) as error:
                raise Fail(
                    f"lock does not pin the updater for {target}: {error}",
                    code="sidecar-universal-missing-attachment",
                ) from error
            artifact = consume.download_artifact(root, f"updater-{target}", spec)
            copy = Path(raw) / target
            shutil.copyfile(artifact, copy)
            copy.chmod(copy.stat().st_mode | 0o755)
            staged.append(copy)
        out.parent.mkdir(parents=True, exist_ok=True)
        archs = _lipo_fuse(staged, out)
    print(f"relkit: universal updater sidecar {out} ({' '.join(archs)})")
    return 0

def _impl__lipo_fuse(inputs: Sequence[Path], out: Path) -> list[str]:
    """Run lipo and report the architectures the result actually carries."""
    try:
        subprocess.run(
            ["lipo", "-create", *(str(item) for item in inputs), "-output", str(out)],
            check=True,
        )
        out.chmod(out.stat().st_mode | 0o755)
        archs = subprocess.run(
            ["lipo", "-archs", str(out)],
            check=True,
            capture_output=True,
            text=True,
        ).stdout.split()
    except (OSError, subprocess.CalledProcessError) as error:
        raise Fail(
            f"lipo failed to fuse the updater sidecar: {error}",
            code="sidecar-universal-lipo-failed",
        ) from error
    missing = [item for item in ("x86_64", "arm64") if item not in archs]
    if missing:
        raise Fail(
            f"universal sidecar is missing {' '.join(missing)}: {out}",
            code="sidecar-universal-lipo-failed",
        )
    return archs

def _impl_int_window(raw: Any, fallback: int) -> dict[str, int]:
    try:
        minimum = int((raw or {}).get("min") or fallback)
        maximum = int((raw or {}).get("max") or minimum)
    except (AttributeError, TypeError, ValueError):
        return {"min": fallback, "max": fallback}
    return {"min": minimum, "max": maximum}

def _impl_build_release_lock(
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
    lock's shape is never a precondition. The updater IPC window comes from
    the release manifest (`minUpdaterIpc` / `maxUpdaterIpc`); older releases
    that omit those fields fall back to UPDATER_IPC_FALLBACK.
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
        "updaterIpc": int_window(
            {
                "min": manifest.get("minUpdaterIpc"),
                "max": manifest.get("maxUpdaterIpc"),
            },
            UPDATER_IPC_FALLBACK,
        ),
        "artifacts": artifacts,
    }

def _impl_cmd_upgrade(root: Path, release: str) -> int:
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

def _impl_http_get(url: str) -> str:
    request = Request(url, headers={"User-Agent": "relkit-host"})
    try:
        with urlopen(request, timeout=30) as response:
            return response.read().decode("utf-8")
    except (HTTPError, URLError, TimeoutError, OSError) as error:
        raise Fail(f"GET {url} failed: {error}") from error

def _impl_parse_sha256sums(text: str) -> dict[str, str]:
    mapping: dict[str, str] = {}
    for line in text.splitlines():
        parts = line.split()
        if len(parts) >= 2:
            mapping[parts[-1].lstrip("*")] = parts[0].lower()
    return mapping

def _impl_release_manifest(base: str) -> dict[str, Any]:
    """Read immutable build metadata from the release CDN, never the REST API."""
    raw = http_get(f"{base}/manifest.json")
    try:
        manifest = json.loads(raw)
    except json.JSONDecodeError as error:
        raise Fail(f"{base}/manifest.json is not JSON: {error}") from error
    if not isinstance(manifest, dict):
        raise Fail(f"{base}/manifest.json is not a JSON object")
    return manifest

def _impl_release_commit(manifest: dict[str, Any], base: str) -> str:
    commit = str(manifest.get("commit") or "").strip().lower()
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise Fail(
            f"{base}/manifest.json must pin a 40-char commit; refusing to keep the previous lock commit"
        )
    return commit

def _impl_release_commit_from_manifest(base: str) -> str:
    """Compatibility helper used by callers that only need the commit."""
    return release_commit(release_manifest(base), base)

def _impl_rewrite_lock_artifacts(artifacts: dict[str, Any], base: str, sums: dict[str, str]) -> None:
    for row in registry_components("product-tree"):
        if row.archive in sums:
            artifacts[row.name] = {
                "url": f"{base}/{row.archive}",
                "sha256": sums[row.archive],
            }
    for row in registry_components("product-binary"):
        bucket = artifacts.setdefault(row.name, {})
        for target in TARGETS:
            filename = f"{row.binary_prefix}-{target}"
            if target.startswith("windows-"):
                filename += ".exe"
            if filename in sums:
                bucket[target] = {
                    "url": f"{base}/{filename}",
                    "sha256": sums[filename],
                }

def _impl_relkit_config(root: Path) -> dict[str, Any]:
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

def _impl_agent_base_url(root: Path) -> Optional[str]:
    url = str((relkit_config(root).get("agent") or {}).get("url") or "").strip()
    return url or None

def _impl_publish_protocol_window(root: Path) -> tuple[int, int]:
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

def _impl_sanitize_upload_token(raw: str) -> str:
    token = raw.replace("\ufeff", "").strip()
    if len(token) >= 2 and token[0] == token[-1] and token[0] in "'\"":
        token = token[1:-1].strip()
    return token.replace("\r", "").replace("\n", "").strip()

def _impl_upload_token(root: Path, note: Path) -> str:
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

def _impl_publish_via_agent(
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

def _impl_release_incomplete_steps(
    state: dict[str, Any], *, via_ci: bool = False, root: Optional[Path] = None
) -> list[str]:
    missing: list[str] = []
    irrelevant: set[str] = set()
    if root is not None and publish_topology(root).get("mode") == "direct":
        irrelevant.update(
            {
                "ssh.host",
                "ssh.config_dir",
                "token.isolation",
                "serve.register",
                "agent.register",
            }
        )
    for step in REQUIRED_FOR_RELEASE:
        if step in irrelevant:
            continue
        if step == "ops.retrospect" and via_ci:
            continue
        status = state["steps"][step]["status"]
        allowed = ("confirmed", "verified", "skipped") if step in DECISION_STEPS else ("verified", "skipped")
        if step == "pack.ci" and via_ci:
            allowed = ("confirmed", "verified", "skipped")
        if status not in allowed:
            missing.append(step)
    return missing

def _impl_cmd_release(root: Path, args: argparse.Namespace) -> int:
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
    missing = release_incomplete_steps(state, via_ci=via_ci, root=root)
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
                "agent publish is CI-only; relkit_host.py ci release must set "
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

def _impl_relkit_cli_version(raw: str) -> str:
    text = (raw or "").strip()
    if text[:1] in "vV" and len(text) > 1 and text[1].isdigit():
        return text[1:]
    return text

def _impl_project_version_for_relkit(root: Path) -> str:
    for name in ("VERSION.json", "version.json"):
        path = root / name
        if not path.is_file():
            continue
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError, UnicodeError):
            continue
        if isinstance(data, dict):
            raw = str(data.get("version") or "").strip()
            if raw:
                return relkit_cli_version(raw)
    return ""

def _impl_cmd_fake_verify(root: Path, version: Optional[str]) -> int:
    binary = relkit_bin(root)
    resolved = relkit_cli_version(version or "")
    if not resolved:
        resolved = project_version_for_relkit(root)
    if not resolved:
        current = run_relkit(root, binary, ["version", "get"])
        resolved = relkit_cli_version(
            (current.stdout or "").strip().splitlines()[-1]
            if (current.stdout or "").strip()
            else ""
        )
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

def _impl_serve_token_path(config_dir: str, rel: str) -> str:
    """Absolute path for a token file taken from init -list-products."""
    rel = (rel or "").strip()
    if not rel:
        raise Fail("token file path is empty")
    if rel.startswith("/"):
        return rel
    return str(Path(config_dir) / rel).replace("\\", "/")

def _impl_cmd_keys_gen(root: Path, args: argparse.Namespace) -> int:
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

def _impl_dummy_stage_zip(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("DUMMY.txt", "relkit fake.release dummy\n")


def _impl_stage_dummy_release(root: Path, version: str, binary: Path) -> None:
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
            "--install",
            str(windows),
            "os=windows,arch=x64",
            "--install",
            str(macos),
            "os=macos",
        ],
    )


def _impl_routing_help() -> str:
    return """relkit_host.py — product-repo ops gate. Prints routing only; confirms nothing.

  onboard start|resume [--interactive|--non-interactive]
  onboard inspect|questions|apply|explain|set|reset
  retrospect
  install
  upgrade vX.Y.Z
  fake verify [--version X.Y.Z+N]
  release [--execute]
  ci release --channel <dev|stable> [--execute]
  serve list|add|restart|rotate|remove
  agent list|add|provision|restart|remove
  keys gen
  status [--json]
  verify [--all]

CI must name a subcommand. Mutations need --execute. Restarts need --restart.

Empty-machine install / binary replace lives in the relkit repo:
  python scripts/deploy/relkit.py build|install|upgrade
"""

RELEASE_ARTIFACTS_SCHEMA = "relkit.release-artifacts/1"
DEFAULT_RELEASE_MANIFEST = "dist/release-artifacts.json"


def _impl_release_pack_config(root: Path) -> dict[str, str]:
    config = relkit_config(root)
    release = config.get("release")
    if not isinstance(release, dict):
        raise Fail(
            "relkit.json must declare release.packScript for ci release",
            code="release-pack-missing",
        )
    pack_script = str(release.get("packScript") or "").strip()
    if not pack_script:
        raise Fail(
            "relkit.json release.packScript must be a non-empty string",
            code="release-pack-missing",
        )
    manifest = str(release.get("manifest") or DEFAULT_RELEASE_MANIFEST).strip()
    if not manifest:
        raise Fail(
            "relkit.json release.manifest must be a non-empty path",
            code="release-manifest-missing",
        )
    return {"packScript": pack_script, "manifest": manifest}


def _impl_resolve_project_path(root: Path, relative: str, *, label: str) -> Path:
    raw = Path(relative)
    if raw.is_absolute():
        raise Fail(f"{label} must be relative to the project root: {relative}")
    resolved = (root / raw).resolve()
    try:
        resolved.relative_to(root.resolve())
    except ValueError as error:
        raise Fail(f"{label} escapes the project root: {relative}") from error
    return resolved


def _impl_pack_script_command(script: Path) -> list[str]:
    suffix = script.suffix.lower()
    if suffix in {".mjs", ".js", ".cjs"}:
        node = os.environ.get("RELKIT_NODE") or shutil.which("node") or "node"
        return [node, str(script)]
    if suffix == ".py":
        python = os.environ.get("RELKIT_PYTHON") or sys.executable
        return [python, str(script)]
    if suffix == ".ps1":
        powershell = shutil.which("powershell.exe") or shutil.which("powershell")
        if not powershell:
            raise Fail("powershell is required to run release.packScript")
        return [
            powershell,
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            str(script),
        ]
    if suffix in {".cmd", ".bat"}:
        return ["cmd.exe", "/c", str(script)]
    raise Fail(
        f"unsupported release.packScript type {suffix or '(none)'}; "
        "use .mjs/.js/.py/.ps1/.cmd"
    )


def _impl_run_release_pack_script(root: Path, script: Path) -> None:
    if not script.is_file():
        raise Fail(f"release.packScript is missing: {script.relative_to(root).as_posix()}")
    command = pack_script_command(script)
    print("> " + " ".join(command))
    result = subprocess.run(command, cwd=str(root), check=False)
    if result.returncode != 0:
        raise Fail(
            f"release.packScript failed (exit {result.returncode}): "
            f"{script.relative_to(root).as_posix()}"
        )


def _impl_normalize_selectors(raw: Any, *, label: str) -> str:
    if not isinstance(raw, str) or not raw.strip():
        raise Fail(f"{label}.selectors must be a non-empty string")
    return raw.strip()


def _impl_load_release_artifacts_manifest(
    root: Path, manifest_path: Path, *, expected_version: str
) -> dict[str, Any]:
    if not manifest_path.is_file():
        raise Fail(
            f"release manifest missing after packScript: "
            f"{manifest_path.relative_to(root).as_posix()}"
        )
    try:
        data = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise Fail(f"release manifest is not readable JSON: {error}") from error
    if not isinstance(data, dict):
        raise Fail("release manifest must be a JSON object")
    if data.get("schema") != RELEASE_ARTIFACTS_SCHEMA:
        raise Fail(
            f"release manifest schema must be {RELEASE_ARTIFACTS_SCHEMA!r}"
        )
    version = str(data.get("version") or "").strip()
    if version != expected_version:
        raise Fail(
            f"release manifest version {version!r} does not match "
            f"project version {expected_version!r}"
        )
    install = data.get("install")
    payload = data.get("payload")
    if not isinstance(install, dict) or not isinstance(payload, dict):
        raise Fail("release manifest requires install and payload objects")
    install_path = resolve_project_path(
        root, str(install.get("path") or "").strip(), label="install.path"
    )
    payload_path = resolve_project_path(
        root, str(payload.get("path") or "").strip(), label="payload.path"
    )
    if not install_path.is_file() or install_path.stat().st_size == 0:
        raise Fail(f"install artifact missing or empty: {install_path}")
    if not payload_path.is_dir():
        raise Fail(f"payload path must be a directory: {payload_path}")
    kind = str(install.get("kind") or "").strip()
    if kind != "installer":
        raise Fail('install.kind must be "installer" for the full-install track')
    install_selectors = normalize_selectors(install.get("selectors"), label="install")
    payload_selectors = normalize_selectors(payload.get("selectors"), label="payload")
    if install_selectors == payload_selectors:
        # Same selector group is required so one UpdateAvailable carries both tracks.
        pass
    archives_raw = data.get("archives") or []
    if archives_raw is None:
        archives_raw = []
    if not isinstance(archives_raw, list):
        raise Fail("release manifest archives must be a list")
    archives: list[dict[str, str]] = []
    for index, item in enumerate(archives_raw):
        if not isinstance(item, dict):
            raise Fail(f"archives[{index}] must be an object")
        path = resolve_project_path(
            root,
            str(item.get("path") or "").strip(),
            label=f"archives[{index}].path",
        )
        if not path.is_file() or path.stat().st_size == 0:
            raise Fail(f"archives[{index}] missing or empty: {path}")
        role = str(item.get("role") or "ci-only").strip() or "ci-only"
        if role != "ci-only":
            raise Fail(
                f"archives[{index}].role must be ci-only "
                "(archives are never staged)"
            )
        archives.append(
            {
                "path": path.relative_to(root).as_posix(),
                "absolute": str(path),
                "role": role,
            }
        )
    return {
        "schema": RELEASE_ARTIFACTS_SCHEMA,
        "version": version,
        "install": {
            "path": install_path.relative_to(root).as_posix(),
            "absolute": str(install_path),
            "kind": kind,
            "selectors": install_selectors,
        },
        "payload": {
            "path": payload_path.relative_to(root).as_posix(),
            "absolute": str(payload_path),
            "selectors": payload_selectors,
        },
        "archives": archives,
    }


def _impl_resolve_ci_channel(root: Path, explicit: str) -> str:
    config = relkit_config(root)
    channels = config.get("channels")
    allowed = (
        [str(item).strip() for item in channels if str(item).strip()]
        if isinstance(channels, list)
        else ["stable", "dev"]
    )
    channel = (explicit or os.environ.get("RUP_CHANNEL") or "").strip()
    if channel not in allowed:
        raise Fail(
            f"--channel must be one of {', '.join(allowed)}; got {channel or '(empty)'}"
        )
    tag = (
        os.environ.get("BK_CI_REPO_GIT_WEBHOOK_TAG_NAME")
        or os.environ.get("BK_CI_GIT_REPO_TAG_NAME")
        or ""
    ).strip()
    if tag and not tag.startswith(f"{channel}/"):
        raise Fail(
            f"trigger tag {tag} does not match channel {channel}; "
            f"expected {channel}/<VERSION.json>"
        )
    return channel


def _impl_cmd_ci_release(root: Path, args: argparse.Namespace) -> int:
    """Single CI entry: install → pack → stage → simulate → fake → publish."""
    channel = resolve_ci_channel(root, getattr(args, "channel", ""))
    execute = bool(getattr(args, "execute", False))
    if execute and os.environ.get("RELKIT_RELEASE_VIA_CI") != "1":
        raise Fail(
            "ci release --execute requires RELKIT_RELEASE_VIA_CI=1 "
            "(CI holds the agent token; local shells must not publish)"
        )
    if execute and not os.environ.get(TOKEN_ENV, "").strip():
        raise Fail(
            f"ci release --execute requires {TOKEN_ENV} "
            "(product agent Bearer injected by CI secrets)"
        )

    print(f"relkit ci release channel={channel} execute={execute}")
    cmd_install(root, [])
    binary = relkit_bin(root)
    version = project_version_for_relkit(root)
    if not version:
        raise Fail("VERSION.json / VERSION is required for ci release")

    pack = release_pack_config(root)
    script = resolve_project_path(root, pack["packScript"], label="release.packScript")
    manifest_path = resolve_project_path(
        root, pack["manifest"], label="release.manifest"
    )
    run_release_pack_script(root, script)
    artifacts = load_release_artifacts_manifest(
        root, manifest_path, expected_version=version
    )

    install = artifacts["install"]
    payload = artifacts["payload"]
    stage_argv = [
        "stage",
        version,
        "--channel",
        channel,
        "--install",
        install["absolute"],
        f"kind={install['kind']},{install['selectors']}",
        "--payload",
        payload["absolute"],
        payload["selectors"],
    ]
    print(
        "staging install="
        + install["path"]
        + " payload="
        + payload["path"]
        + " archives="
        + str(len(artifacts["archives"]))
        + " (ci-only, not staged)"
    )
    run_relkit(root, binary, stage_argv)
    run_relkit(root, binary, ["simulate", "--with-staged", version, "--from", "all"])
    cmd_fake_verify(root, version)

    if not execute:
        print(
            "ci release dry-run complete "
            "(install → pack → stage → simulate → fake); "
            "pass --execute with RELKIT_RELEASE_VIA_CI=1 to publish"
        )
        return 0

    # Existing release gate owns drift / incomplete / agent publish.
    publish_args = argparse.Namespace(execute=True)
    return cmd_release(root, publish_args)


_IMPLEMENTATIONS = {
    "import_consume": _impl_import_consume,
    "consume_components": _impl_consume_components,
    "require_host_scripts_match_lock": _impl_require_host_scripts_match_lock,
    "lock_artifact_names": _impl_lock_artifact_names,
    "cmd_install": _impl_cmd_install,
    "cmd_sidecar_universal": _impl_cmd_sidecar_universal,
    "_lipo_fuse": _impl__lipo_fuse,
    "int_window": _impl_int_window,
    "build_release_lock": _impl_build_release_lock,
    "cmd_upgrade": _impl_cmd_upgrade,
    "http_get": _impl_http_get,
    "parse_sha256sums": _impl_parse_sha256sums,
    "release_manifest": _impl_release_manifest,
    "release_commit": _impl_release_commit,
    "release_commit_from_manifest": _impl_release_commit_from_manifest,
    "rewrite_lock_artifacts": _impl_rewrite_lock_artifacts,
    "relkit_config": _impl_relkit_config,
    "agent_base_url": _impl_agent_base_url,
    "publish_protocol_window": _impl_publish_protocol_window,
    "sanitize_upload_token": _impl_sanitize_upload_token,
    "upload_token": _impl_upload_token,
    "publish_via_agent": _impl_publish_via_agent,
    "release_incomplete_steps": _impl_release_incomplete_steps,
    "cmd_release": _impl_cmd_release,
    "relkit_cli_version": _impl_relkit_cli_version,
    "project_version_for_relkit": _impl_project_version_for_relkit,
    "cmd_fake_verify": _impl_cmd_fake_verify,
    "serve_token_path": _impl_serve_token_path,
    "cmd_keys_gen": _impl_cmd_keys_gen,
    "dummy_stage_zip": _impl_dummy_stage_zip,
    "stage_dummy_release": _impl_stage_dummy_release,
    "routing_help": _impl_routing_help,
    "release_pack_config": _impl_release_pack_config,
    "resolve_project_path": _impl_resolve_project_path,
    "pack_script_command": _impl_pack_script_command,
    "run_release_pack_script": _impl_run_release_pack_script,
    "normalize_selectors": _impl_normalize_selectors,
    "load_release_artifacts_manifest": _impl_load_release_artifacts_manifest,
    "resolve_ci_channel": _impl_resolve_ci_channel,
    "cmd_ci_release": _impl_cmd_ci_release,
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
