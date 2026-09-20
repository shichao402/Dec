"""Registered product-tree gates.

Gates inspect files only. They never run a product build or code generator.
"""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any, Callable

from .const import MIN_PYTHON
from .facets import BY_NAME, components

Gate = Callable[[Path, dict[str, Any], list[str]], None]
GATES: dict[str, Gate] = {}
EXCLUDED = {".git", ".relkit", "third_party", "node_modules", "gen", "target", "dist"}
TEXT_SUFFIXES = {
    ".rs", ".go", ".dart", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
    ".py", ".sh", ".ps1", ".json", ".toml", ".yaml", ".yml", ".proto",
}
EXECUTABLE_SUFFIXES = {
    ".rs", ".go", ".dart", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
    ".py", ".sh", ".ps1",
}
# Entry scripts pick the interpreter, so this scan has to reach the Windows
# batch proxies that the shared source scan deliberately ignores.
ENTRY_SUFFIXES = {".sh", ".bash", ".bat", ".cmd", ".ps1", ".py", ".yml", ".yaml"}
HOST_ENTRY_SIGNAL = re.compile(r"relkit_host\.py|relkit_consume\.py")
CONSUMER_IMPORT = re.compile(r"^[ \t]*(?:import|from)[ \t]+relkit_consume\b", re.M)
RETIRED_CONSUMER_PATH = re.compile(r"scripts[/\\]relkit_consume\.py")
INTERNAL_APPLY_CALL = re.compile(r"\bapply\s*\(", re.I)


def gate(name: str) -> Callable[[Gate], Gate]:
    def register(function: Gate) -> Gate:
        if name in GATES:
            raise ValueError(f"duplicate host gate {name}")
        GATES[name] = function
        return function
    return register


def run_gates(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    for function in GATES.values():
        function(root, state, drift)


def _lock(root: Path) -> dict[str, Any]:
    path = root / "scripts" / "relkit.lock.json"
    if not path.is_file():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8-sig"))
        return value if isinstance(value, dict) else {}
    except (OSError, ValueError):
        return {}


def _config(root: Path) -> dict[str, Any]:
    path = root / "relkit.json"
    if not path.is_file():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8-sig"))
        return value if isinstance(value, dict) else {}
    except (OSError, ValueError):
        return {}


def _source_files(root: Path) -> list[Path]:
    return [
        path for path in root.rglob("*")
        if path.is_file()
        and path.suffix.lower() in TEXT_SUFFIXES
        and not any(part in EXCLUDED for part in path.relative_to(root).parts)
        and not any(part.startswith(".") for part in path.relative_to(root).parts[:-1])
        and not path.relative_to(root).as_posix().startswith("scripts/host/")
    ]

@gate("product-site-policy")
def product_site_policy(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    cfg = _config(root)
    if not cfg:
        return
    site = cfg.get("site")
    if not isinstance(site, dict) or not str(site.get("title") or "").strip():
        drift.append("relkit.json site.title is required for the human release catalog")
        return
    if "makers" in site:
        drift.append("relkit.json site.makers is forbidden; configure it once in relkit-agent.json")


def _entry_files(root: Path) -> list[Path]:
    return [
        path for path in root.rglob("*")
        if path.is_file()
        and path.suffix.lower() in ENTRY_SUFFIXES
        and not any(part in EXCLUDED for part in path.relative_to(root).parts)
        and not any(part.startswith(".") for part in path.relative_to(root).parts[:-1])
        and not path.relative_to(root).as_posix().startswith("scripts/host/")
    ]


def has_webview(root: Path) -> bool:
    return any(
        path.name == "package.json"
        and not any(part in EXCLUDED or part.startswith(".") for part in path.relative_to(root).parts[:-1])
        for path in root.rglob("package.json")
    )


def _step_value(state: dict[str, Any], step: str) -> Any:
    raw = (state.get("steps") or {}).get(step) or {}
    return raw.get("value") if isinstance(raw, dict) else None


def _tree_is_installed(root: Path, lock: dict[str, Any], name: str) -> bool:
    row = BY_NAME[name]
    spec = (lock.get("artifacts") or {}).get(name)
    if not isinstance(spec, dict):
        return False
    destination = root / row.destination
    marker = destination / ".relkit-artifact.json"
    try:
        installed = json.loads(marker.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return False
    return (
        installed.get("sha256") == spec.get("sha256")
        and all((destination / relative).exists() for relative in row.required_paths)
    )


@gate("consumer-entry-only")
def consumer_entry_only(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    """The pinned consumer is release-internal, not a product import.

    relkit_consume.py ships inside scripts/host next to hostlib and moves with
    the release, so a product that imports it as a top-level module or spells
    the retired root path only finds out at build time, one upgrade later.
    """
    if not _step_value(state, "consume.lock"):
        return
    for path in _source_files(root):
        relative = path.relative_to(root).as_posix()
        text = path.read_text(encoding="utf-8", errors="ignore")
        if CONSUMER_IMPORT.search(text):
            drift.append(
                "relkit_consume is release-internal; go through relkit_host.py: "
                f"{relative}"
            )
        if RETIRED_CONSUMER_PATH.search(text):
            drift.append(
                "scripts/relkit_consume.py is retired; the consumer ships in "
                f"scripts/host: {relative}"
            )


@gate("host-python-floor")
def host_python_floor(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    """Entry scripts must pick an interpreter that can import hostlib.

    Accepting an older interpreter fails deep inside the hostlib import on
    whichever CI worker happens to have only that version installed, so the
    floor is checked where the interpreter is chosen.
    """
    if not _step_value(state, "consume.lock"):
        return
    minor = MIN_PYTHON[1]
    declared = re.compile(r"version_info\s*(?:>=|<)\s*\(\s*3\s*,\s*(\d+)\s*\)")
    interpreters = (
        re.compile(r"python-?3\.?(\d{1,2})\b", re.I),
        # Windows proxies select through the py launcher instead of a name.
        re.compile(r"\bpy\s+-3\.(\d{1,2})\b", re.I),
    )
    for path in _entry_files(root):
        text = path.read_text(encoding="utf-8", errors="ignore")
        if not HOST_ENTRY_SIGNAL.search(text):
            continue
        found = {int(item) for item in declared.findall(text)}
        for pattern in interpreters:
            found.update(int(item) for item in pattern.findall(text))
        stale = sorted(item for item in found if item < minor)
        if stale:
            drift.append(
                f"entry script accepts Python 3.{stale[0]} but relkit host "
                f"scripts need >= 3.{minor}: "
                f"{path.relative_to(root).as_posix()}"
            )


@gate("updater-sdk-contract")
def updater_sdk_contract(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    process = str(_step_value(state, "updater.process") or "")
    if not process or process == "other":
        return
    row = next(
        (item for item in components("product-tree") if item.updater_process == process),
        None,
    )
    if row is None:
        drift.append(f"updater.process={process} has no registry component")
        return
    lock = _lock(root)
    if not _tree_is_installed(root, lock, row.name):
        drift.append(f"updater.process={process} requires lock-pinned installed {row.name}")
        return
    source = "\n".join(path.read_text(encoding="utf-8", errors="ignore") for path in _source_files(root))
    if row.import_signals and not any(signal in source for signal in row.import_signals):
        drift.append(f"updater.process={process} source does not import installed {row.name}")


@gate("internal-update-two-track")
def internal_update_two_track(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    """A host that calls the updater apply facade needs a payload publish track.

    A full-install artifact may still be selected for old clients, but the sidecar
    deliberately refuses to apply it. Without this cross-check a product can pass
    pack.ci, download a valid archive, and only fail after the user clicks install.
    """
    process = str(_step_value(state, "updater.process") or "")
    if not process:
        return
    row = next(
        (item for item in components("product-tree") if item.updater_process == process),
        None,
    )
    if row is None:
        return
    source = "\n".join(
        path.read_text(encoding="utf-8", errors="ignore")
        for path in _source_files(root)
    )
    if not any(signal in source for signal in row.import_signals):
        return
    if not INTERNAL_APPLY_CALL.search(source):
        return

    ci_files = [
        path
        for pattern in (
            ".github/workflows/*.yml",
            ".github/workflows/*.yaml",
            "ci/*.yml",
            "ci/*.yaml",
            "scripts/*.js",
            "scripts/*.mjs",
            "scripts/*.py",
            "scripts/*.sh",
            "scripts/*.ps1",
            "scripts/*.cmd",
            "scripts/*.bat",
        )
        for path in root.glob(pattern)
        if path.is_file()
    ]
    ci_text = "\n".join(
        path.read_text(encoding="utf-8", errors="ignore")
        for path in ci_files
    )
    # Legacy products hand-stage both tracks; new products declare
    # release.packScript and call `ci release`, which always stages payload.
    config_path = root / "relkit.json"
    release_pack = False
    if config_path.is_file():
        try:
            config = json.loads(config_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            config = {}
        release = config.get("release") if isinstance(config, dict) else None
        release_pack = (
            isinstance(release, dict)
            and bool(str(release.get("packScript") or "").strip())
        )
    if "--payload" in ci_text:
        return
    if release_pack and "ci release" in ci_text:
        return
    drift.append(
        "internal updater apply requires a CI payload track paired with "
        "the full-install selectors (--payload is missing)"
    )


@gate("webview-projection")
def webview_projection(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    process = str(_step_value(state, "updater.process") or "")
    if not process or not has_webview(root):
        return
    row = next(item for item in components("product-tree") if item.webview_projection)
    lock = _lock(root)
    if not _tree_is_installed(root, lock, row.name):
        drift.append(f"WebView requires lock-pinned installed {row.name}")
        return
    source = "\n".join(path.read_text(encoding="utf-8", errors="ignore") for path in _source_files(root))
    if not any(signal in source for signal in row.import_signals):
        drift.append(f"WebView source does not import installed {row.name}")


@gate("other-declarations")
def other_declarations(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    if _step_value(state, "updater.process") != "other":
        return
    updater = _config(root).get("updater")
    if not isinstance(updater, dict):
        drift.append(
            "updater.process=other requires relkit.json updater.entry"
            " (and updater.projection for WebView)"
        )
        return
    for field in ("entry",):
        value = updater.get(field)
        if not isinstance(value, str) or not value.strip() or not (root / value).is_file():
            drift.append(f"updater.process=other requires existing updater.{field}")
    if has_webview(root):
        value = updater.get("projection")
        if not isinstance(value, str) or not value.strip() or not (root / value).exists():
            drift.append("WebView with updater.process=other requires existing updater.projection")


@gate("updater-chokepoints")
def updater_chokepoints(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    if not _step_value(state, "updater.process"):
        return
    config = _config(root)
    updater = config.get("updater") if isinstance(config.get("updater"), dict) else {}
    sidecar = config.get("sidecar") if isinstance(config.get("sidecar"), dict) else {}
    allowed_sidecar = {
        str(value).replace("\\", "/")
        for value in (updater.get("entry"), sidecar.get("packScript"))
        if isinstance(value, str) and value
    }
    allow_urls = updater.get("urlAllowlist") or []
    if not isinstance(allow_urls, list) or not all(isinstance(item, str) for item in allow_urls):
        drift.append("updater.urlAllowlist must be a list of paths")
        allow_urls = []
    allowed_urls: set[str] = set()
    for value in allow_urls:
        path = root / value
        if not path.exists():
            drift.append(f"updater.urlAllowlist path is missing: {value}")
        allowed_urls.add(value.replace("\\", "/"))

    shape = re.compile(
        r"\b(message|interface|struct|class)\s+(CheckResult|UpdateAvailable)\b"
        r"|json\s*[:=(]\s*[\"']releaseNotesMarkdown"
        r"|package\s+(?:relkit\.)?updater\.v1"
    )
    serde_default = re.compile(r"serde\s*\(\s*default\s*\)")
    updater_shape_signal = re.compile(
        r"\b(CheckResult|UpdateAvailable|releaseNotesMarkdown|updater\.v1)\b"
    )
    url = re.compile(r"https?://[^\s\"']+/(?:rup|update|artifact|directory|index)(?:/|\\b)", re.I)
    files = _source_files(root)
    import_signals = tuple(
        signal
        for row in components("product-tree")
        for signal in row.import_signals
        if signal != "relkit-updater"
    )
    imported_directories = {
        path.parent
        for path in files
        if any(signal in path.read_text(encoding="utf-8", errors="ignore") for signal in import_signals)
    }
    allowed_urls.add("relkit.json")
    for path in files:
        relative = path.relative_to(root).as_posix()
        text = path.read_text(encoding="utf-8", errors="ignore")
        if (
            path.suffix.lower() in EXECUTABLE_SUFFIXES
            and "relkit-updater" in text
            and relative not in allowed_sidecar
            and path.parent not in imported_directories
        ):
            drift.append(f"sidecar name appears outside declared entry/packScript: {relative}")
        if shape.search(text) or (
            serde_default.search(text) and updater_shape_signal.search(text)
        ):
            drift.append(f"handwritten updater shape/proto/default detected: {relative}")
        if relative not in allowed_urls and url.search(text):
            drift.append(f"updater endpoint/base URL is not allowlisted: {relative}")


def _engine_authoring(relative: str) -> bool:
    parts = Path(relative).parts
    return (
        parts[:1] == ("sdk",)
		or parts[:2] == ("internal", "updater")
		or parts[:2] == ("internal", "inprocess")
		or parts[:2] == ("internal", "ipc")
        or parts[:2] == ("cmd", "relkit-updater")
        or parts[:1] == ("conformance",)
    )


@gate("legacy-in-process-updater")
def legacy_in_process_updater(root: Path, state: dict[str, Any], drift: list[str]) -> None:
    if not _step_value(state, "updater.process"):
        return
    legacy = re.compile(r"\bRupUpdater\b|\bsdk\.Updater\b")
    for path in _source_files(root):
        relative = path.relative_to(root).as_posix()
        if _engine_authoring(relative):
            continue
        text = path.read_text(encoding="utf-8", errors="ignore")
        if legacy.search(text):
            drift.append(
                f"in-process updater API is not a host calling surface: {relative}"
            )
