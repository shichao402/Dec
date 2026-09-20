"""Constants and shared error type for host orchestration."""
from __future__ import annotations
import os
from pathlib import Path
from .facets import updater_process_values

SCHEMA = "relkit.onboarding/1"

LOCAL_SCHEMA = "relkit.onboarding.local/1"

LOCK_SCHEMA = "relkit.consume/2"

# hostlib evaluates PEP 585 builtin generics while importing (gates.Gate), so an
# older interpreter dies inside the import instead of at the entry guard. The
# entry scripts repeat this floor as a literal because they must refuse before
# importing hostlib at all; retrospect checks the two agree.
MIN_PYTHON = (3, 9)

DEFAULT_SERVE_DIR = "/etc/relkit-serve"

DEFAULT_AGENT_CONFIG = "/etc/relkit-agent/relkit-agent.json"

SERVE_BIN = "/usr/local/bin/relkit-serve"

AGENT_BIN = "/usr/local/bin/relkit-agent"

AGENT_ORIGIN_HOST = "update.devcloud.woa.com"

AGENT_ORIGIN_NETLOC = "update.devcloud.woa.com:8080"

# The agent writes to its backends with the operator credential systemd puts in
# its own environment. Which variable holds it is a fact about the box, not
# about any product repository: CI's RELKIT_UPLOAD_TOKEN only reaches the
# agent's HTTP API and is never in the agent process environment. Used to seed
# a brand new profile; an existing profile on the box wins over it.
AGENT_BACKEND_TOKEN_ENV = "RELKIT_SERVE_TOKEN"

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

UPDATER_PROCESS_VALUES = updater_process_values()

# Writable publish destinations. Read-only mirrors are not a backend type.
BACKEND_KIND_VALUES = (
    "intranet-relkit-compatible",
    "s3-compatible",
)

UPDATER_PROCESS_EXPLAIN = (
    "谁调用 Updater.open。只记封闭词，不要另写决策备忘。"
    " rust/node/dart/go：该语言 facade 直连 sidecar。"
    " 给 WebView 只用对应 SDK 的 JSON 投影（Rust：check_result_to_json）。"
    " 禁止再开工一份 CheckResult / UpdateAvailable 手写 DTO。"
    " 缺 releaseNotesMarkdown 这类键是投影器或手写层 bug，不是旧 updater；"
    " 禁止 serde(default) / Option 吞掉。"
    " other：relkit.json 必须声明 updater.entry；存在 WebView 时还必须声明"
    " updater.projection。声明路径每次 reconcile 都接受机械检查。"
)

STATUSES = (
    "unanswered",
    "confirmed",
    "applied",
    "verified",
    "skipped",
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

STALE_BACKEND_TYPES = frozenset({"http-put", "local", "static-http"})

INSPECT_SCHEMA = "relkit.inspect/1"

OPS_JOURNAL_SCHEMA = "relkit.ops-journal/1"

QUESTION_BATCH_SCHEMA = "relkit.onboarding-questions/1"

ANSWER_BATCH_SCHEMA = "relkit.onboarding-answers/1"

ONBOARD_INTENTS = ("fresh", "reconfigure", "upgrade")

# A product repo that silently stays on an old lock looks healthy: every local
# hash agrees with itself. The newest published tag is the only outside
# reference, so inspect resolves it once an hour and caches the answer.
UPSTREAM_LATEST_TTL = 3600

# retrospect can only read files and command failures, so a finding that only a
# human or agent observed needs its own intake. The class decides where the fix
# belongs, mirroring the skill's conversation-retrospect split.
RETROSPECT_NOTE_CLASSES = ("generic", "product", "agent")

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
        "upgrade-legacy-inventory-unreadable",
        "decision-before-live-inventory",
        "sidecar-universal-not-darwin",
        "sidecar-universal-missing-attachment",
        "sidecar-universal-lipo-failed",
        "retrospect-note-code-invalid",
        "retrospect-note-class-invalid",
        "retrospect-note-text-missing",
        "consume-stale-proto-shadow",
        "ci-zip-as-full-install",
        "host-ignore-full-install-disposition",
        "pack-ci-missing-payload-track",
        "full-install-disposition-unhandled",
        "windows-install-root-unwritable",
        "lock-latest-cache-stale",
        "dec-publish-not-in-ci",
        "dec-skill-header-push-route",
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
    "backend.kind": (
        "Where bits live for publish. Intranet products share a serve host "
        "with a new product id."
    ),
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
