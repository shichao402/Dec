#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Install immutable relkit release artifacts into a host repository.

Copy the whole ``scripts/host/`` tree byte-for-byte into the product repo.
``relkit_host.py install`` calls this file. All variable input is pinned by
``scripts/relkit.lock.json``. This consumer never clones relkit, installs Go,
or builds source code.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import ssl
import stat
import subprocess
import sys
import tempfile
import time
import traceback
import zipfile
from pathlib import Path
from typing import Any, Optional, Sequence
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import Request, urlopen

# Must refuse before the hostlib import, which itself needs 3.9. Keep in sync
# with hostlib.const.MIN_PYTHON; retrospect checks both literals agree.
if sys.version_info < (3, 9):
    raise SystemExit(
        "relkit host scripts need Python >= 3.9, but this interpreter is "
        f"{sys.version_info.major}.{sys.version_info.minor}; "
        "point the entry script at a newer interpreter"
    )

from hostlib.facets import (
    BY_NAME,
    TARGETS,
    default_components,
    portable_components,
    product_components,
)
from hostlib.digest import tree_sha256

LOCK_SCHEMA = "relkit.consume/2"
COMPONENTS = tuple(row.name for row in product_components())
DEFAULT_COMPONENTS = tuple(row.name for row in default_components())
PORTABLE_COMPONENTS = tuple(row.name for row in portable_components())
DOWNLOAD_ATTEMPTS = 3
ALLOW_INSECURE_ENV = "RELKIT_CONSUME_ALLOW_INSECURE"
CURL_OVERRIDE_ENV = "RELKIT_CURL"
_CA_ENV_KEYS = (
    "RELKIT_CA_BUNDLE",
    "SSL_CERT_FILE",
    "CURL_CA_BUNDLE",
    "REQUESTS_CA_BUNDLE",
)
_ERROR_SUMMARY_LIMIT = 240
_windows_ca_bundle: Optional[Path] = None
# Path -> backend name. A single global would poison later picks after PATH
# found a Cygwin OpenSSL curl on the first call.
_curl_backends: dict[str, str] = {}


class CertificateVerificationError(RuntimeError):
    """A strict HTTPS transport failed specifically on certificate validation."""


def force_utf8_stdio() -> None:
    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is not None:
            try:
                reconfigure(encoding="utf-8", errors="replace")
            except (ValueError, OSError):
                pass


def host_root(script_file: Optional[Path] = None) -> Path:
    return (script_file or Path(__file__)).resolve().parent.parent


def load_lock(path: Path) -> dict[str, Any]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise RuntimeError(f"{path} is not a JSON object")
    if data.get("schema") != LOCK_SCHEMA:
        raise RuntimeError(
            f"{path} must use schema {LOCK_SCHEMA!r}; source-build locks are unsupported"
        )
    release = str(data.get("release") or "").strip()
    commit = str(data.get("commit") or "").strip()
    if not release or not commit:
        raise RuntimeError(f"{path} must pin non-empty release and commit")
    artifacts = data.get("artifacts")
    if not isinstance(artifacts, dict):
        raise RuntimeError(f"{path} must contain an artifacts object")
    return data


def host_target() -> str:
    system = platform.system().lower()
    machine = platform.machine().lower()
    os_name = {"windows": "windows", "linux": "linux", "darwin": "darwin"}.get(
        system
    )
    if os_name is None:
        raise RuntimeError(f"unsupported host OS: {system}")
    arch = "arm64" if machine in ("arm64", "aarch64") else "amd64"
    target = f"{os_name}-{arch}"
    if target not in TARGETS:
        raise RuntimeError(f"unsupported host target: {target}")
    return target


def artifact_spec(
    lock: dict[str, Any], component: str, target: str
) -> dict[str, str]:
    artifacts = lock["artifacts"]
    raw = artifacts.get(component)
    if component not in PORTABLE_COMPONENTS:
        raw = raw.get(target) if isinstance(raw, dict) else None
    if not isinstance(raw, dict):
        suffix = "" if component in PORTABLE_COMPONENTS else f" for {target}"
        raise RuntimeError(f"lock has no {component} artifact{suffix}")
    url = str(raw.get("url") or "").strip()
    digest = str(raw.get("sha256") or "").strip().lower()
    if not url.startswith(("https://", "http://")):
        raise RuntimeError(f"{component} artifact URL must be absolute HTTP(S)")
    if len(digest) != 64 or any(ch not in "0123456789abcdef" for ch in digest):
        raise RuntimeError(f"{component} artifact has invalid sha256")
    return {"url": url, "sha256": digest}


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def summarize_error(error: BaseException | str) -> str:
    text = str(error).strip() or type(error).__name__
    # curl --help / usage dumps are useless noise on CI logs.
    lines = [
        line.strip()
        for line in text.replace("\r\n", "\n").split("\n")
        if line.strip()
        and not line.strip().lower().startswith("usage:")
        and "curl --help" not in line.lower()
    ]
    compact = " | ".join(lines) if lines else text
    if len(compact) > _ERROR_SUMMARY_LIMIT:
        return compact[: _ERROR_SUMMARY_LIMIT - 3] + "..."
    return compact


def env_allows_insecure(environ: Optional[dict[str, str]] = None) -> bool:
    env = environ if environ is not None else os.environ
    return env.get(ALLOW_INSECURE_ENV, "").strip().lower() in {
        "1",
        "true",
        "yes",
        "on",
    }


def resolve_ca_bundle(environ: Optional[dict[str, str]] = None) -> Optional[Path]:
    env = environ if environ is not None else os.environ
    for key in _CA_ENV_KEYS:
        raw = env.get(key, "").strip()
        if not raw:
            continue
        path = Path(raw)
        if path.is_file():
            return path
    if os.name == "nt":
        return ensure_windows_ca_bundle()
    return None


def ensure_windows_ca_bundle() -> Optional[Path]:
    """Export Windows Root/CA stores to a PEM file for OpenSSL-backed transports.

    Python's ssl and OpenSSL curl do not read the Windows certificate store.
    Enterprise intermediates live there; PowerShell/.NET and Schannel curl
    already succeed. Export once per process and share across transports.
    """
    global _windows_ca_bundle
    if _windows_ca_bundle is not None and _windows_ca_bundle.is_file():
        return _windows_ca_bundle

    cache_dir = Path(tempfile.gettempdir()) / "relkit-consume-certs"
    try:
        cache_dir.mkdir(parents=True, exist_ok=True)
    except OSError:
        return None
    destination = cache_dir / f"windows-ca-{os.getpid()}.pem"
    if destination.is_file() and destination.stat().st_size > 0:
        _windows_ca_bundle = destination
        return destination

    script = (
        "$ErrorActionPreference = 'Stop'\n"
        "$out = $env:RELKIT_CA_OUT\n"
        "if (-not $out) { throw 'RELKIT_CA_OUT missing' }\n"
        "$stores = @(\n"
        "  'Cert:\\LocalMachine\\Root',\n"
        "  'Cert:\\LocalMachine\\CA',\n"
        "  'Cert:\\CurrentUser\\Root',\n"
        "  'Cert:\\CurrentUser\\CA'\n"
        ")\n"
        "$sb = New-Object System.Text.StringBuilder\n"
        "foreach ($path in $stores) {\n"
        "  if (-not (Test-Path $path)) { continue }\n"
        "  Get-ChildItem $path -ErrorAction SilentlyContinue | ForEach-Object {\n"
        "    if ($null -eq $_.RawData -or $_.RawData.Length -eq 0) { return }\n"
        "    $b64 = [Convert]::ToBase64String($_.RawData, 'InsertLineBreaks')\n"
        "    [void]$sb.AppendLine('-----BEGIN CERTIFICATE-----')\n"
        "    [void]$sb.AppendLine($b64)\n"
        "    [void]$sb.AppendLine('-----END CERTIFICATE-----')\n"
        "  }\n"
        "}\n"
        "if ($sb.Length -eq 0) { throw 'no certificates exported' }\n"
        "[IO.File]::WriteAllText($out, $sb.ToString(), "
        "[Text.UTF8Encoding]::new($false))\n"
    )
    powershell = shutil.which("powershell.exe") or shutil.which("powershell")
    if not powershell:
        return None
    env = os.environ.copy()
    env["RELKIT_CA_OUT"] = str(destination)
    try:
        result = subprocess.run(
            [
                powershell,
                "-NoProfile",
                "-NonInteractive",
                "-ExecutionPolicy",
                "Bypass",
                "-Command",
                script,
            ],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=60,
            check=False,
            env=env,
        )
    except (OSError, subprocess.TimeoutExpired):
        destination.unlink(missing_ok=True)
        return None
    if result.returncode != 0 or not destination.is_file() or destination.stat().st_size == 0:
        destination.unlink(missing_ok=True)
        return None
    _windows_ca_bundle = destination
    return destination


def ssl_context(*, verify: bool, ca_bundle: Optional[Path] = None) -> ssl.SSLContext:
    if not verify:
        return ssl._create_unverified_context()
    context = ssl.create_default_context()
    if ca_bundle is not None:
        context.load_verify_locations(cafile=str(ca_bundle))
    return context


def detect_curl_backend(curl: str) -> str:
    cached = _curl_backends.get(curl)
    if cached is not None:
        return cached
    try:
        result = subprocess.run(
            [curl, "--version"],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=15,
            check=False,
        )
        text = (result.stdout or "") + "\n" + (result.stderr or "")
    except (OSError, subprocess.TimeoutExpired):
        text = ""
    lowered = text.lower()
    if "schannel" in lowered:
        backend = "schannel"
    elif "openssl" in lowered:
        backend = "openssl"
    else:
        backend = "unknown"
    _curl_backends[curl] = backend
    return backend


def system32_curl() -> Optional[Path]:
    """Windows Schannel curl shipped with the OS; never PATH-ordered."""
    if os.name != "nt":
        return None
    root = Path(os.environ.get("SystemRoot") or r"C:\Windows")
    candidate = root / "System32" / "curl.exe"
    return candidate if candidate.is_file() else None


def resolve_curl(*, environ: Optional[dict[str, str]] = None) -> str:
    """Pick a deterministic curl for HTTPS downloads.

    On Windows the PATH often puts Cygwin/Git OpenSSL curl ahead of the OS
    Schannel curl. That backend does not share the machine Root store and is
    frequently decade-old. Prefer System32 Schannel; allow RELKIT_CURL to
    override for tests and break-glass.
    """
    env = environ if environ is not None else os.environ
    override = env.get(CURL_OVERRIDE_ENV, "").strip()
    if override:
        path = Path(override)
        if not path.is_file():
            raise RuntimeError(f"{CURL_OVERRIDE_ENV}={override} is not a file")
        return str(path)

    if os.name == "nt":
        system = system32_curl()
        if system is not None:
            backend = detect_curl_backend(str(system))
            if backend != "schannel":
                raise RuntimeError(
                    f"Windows System32 curl is not Schannel ({backend}): {system}"
                )
            return str(system)
        raise RuntimeError(
            "Windows System32 curl.exe is missing; refuse PATH curl "
            "(Cygwin/Git OpenSSL curl is not a trusted TLS transport)"
        )

    curl = shutil.which("curl")
    if not curl:
        raise RuntimeError("system curl is unavailable")
    return curl


def ca_source_label(ca_bundle: Optional[Path]) -> str:
    if ca_bundle is None:
        return "none"
    if _windows_ca_bundle is not None and ca_bundle == _windows_ca_bundle:
        return "windows-export"
    for key in _CA_ENV_KEYS:
        raw = os.environ.get(key, "").strip()
        if raw and Path(raw) == ca_bundle:
            return key
    return "explicit"


def log_transport(event: str, **fields: Any) -> None:
    parts = [f"{key}={fields[key]}" for key in sorted(fields) if fields[key] is not None]
    suffix = (" " + " ".join(parts)) if parts else ""
    print(f"relkit consume: transport {event}{suffix}", file=sys.stderr)


def download_with_python(
    url: str,
    destination: Path,
    *,
    verify: bool,
    ca_bundle: Optional[Path] = None,
) -> None:
    request = Request(
        url,
        headers={"User-Agent": "relkit-consume/2"},
        method="GET",
    )
    context = ssl_context(verify=verify, ca_bundle=ca_bundle if verify else None)
    try:
        with urlopen(request, timeout=120, context=context) as response, destination.open(
            "wb"
        ) as out:
            shutil.copyfileobj(response, out)
    except URLError as error:
        if verify and isinstance(error.reason, ssl.SSLCertVerificationError):
            raise CertificateVerificationError(summarize_error(error.reason)) from error
        raise
    except ssl.SSLCertVerificationError as error:
        if verify:
            raise CertificateVerificationError(summarize_error(error)) from error
        raise


def download_with_curl(
    url: str,
    destination: Path,
    *,
    extra_args: Sequence[str] = (),
    ca_bundle: Optional[Path] = None,
    curl: Optional[str] = None,
) -> None:
    curl_path = curl or resolve_curl()
    scheme = "=https" if url.startswith("https://") else "=http"
    args = list(extra_args)
    insecure = "--insecure" in args
    backend = detect_curl_backend(curl_path)
    # Schannel already trusts the Windows store; OpenSSL curl needs an explicit
    # PEM. Prefer --cacert over env mutation so the choice is visible in argv.
    if not insecure and ca_bundle is not None and backend != "schannel":
        args = ["--cacert", str(ca_bundle), *args]
    command = [
        curl_path,
        "--fail",
        "--location",
        "--silent",
        "--show-error",
        "--proto",
        scheme,
        "--tlsv1.2",
        *args,
        "--output",
        str(destination),
        url,
    ]
    try:
        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=180,
            check=False,
        )
    except subprocess.TimeoutExpired as error:
        raise RuntimeError("system curl timed out after 180s") from error
    if result.returncode != 0:
        detail = summarize_error(result.stderr or result.stdout or "no stderr")
        if not insecure and result.returncode == 60:
            raise CertificateVerificationError(detail)
        raise RuntimeError(f"system curl failed ({result.returncode}): {detail}")


def fetch_url(
    url: str,
    destination: Path,
    *,
    allow_insecure: Optional[bool] = None,
) -> None:
    # Strict TLS first. Windows OpenSSL transports get the system Root/CA PEM.
    # Insecure fallback is fail-closed unless explicitly opted in; lock SHA-256
    # remains a second integrity layer, never a standing TLS substitute.
    if allow_insecure is None:
        allow_insecure = env_allows_insecure()
    ca_bundle = resolve_ca_bundle()
    curl_path: Optional[str] = None
    curl_backend = "unavailable"
    try:
        curl_path = resolve_curl()
        curl_backend = detect_curl_backend(curl_path)
    except RuntimeError as error:
        curl_backend = f"unavailable:{summarize_error(error)}"
    host = ""
    try:
        host = urlparse(url).hostname or ""
    except Exception:
        host = ""
    log_transport(
        "attempt",
        host=host or "-",
        ca=ca_source_label(ca_bundle),
        curl=curl_path or "-",
        backend=curl_backend,
    )

    def curl_download(*, extra_args: Sequence[str] = ()) -> None:
        if curl_path is None:
            raise RuntimeError(f"system curl is unavailable ({curl_backend})")
        download_with_curl(
            url,
            destination,
            extra_args=extra_args,
            ca_bundle=ca_bundle,
            curl=curl_path,
        )

    strict_methods: list[tuple[str, Any]] = [
        (
            "Python HTTPS",
            lambda: download_with_python(
                url, destination, verify=True, ca_bundle=ca_bundle
            ),
        ),
        ("system curl", curl_download),
    ]
    strict_errors: list[str] = []
    certificate_failed = False
    for label, download in strict_methods:
        destination.unlink(missing_ok=True)
        try:
            download()
            log_transport(
                "success",
                method=label,
                host=host or "-",
                misses=len(strict_errors),
                ca=ca_source_label(ca_bundle),
                backend=curl_backend if label == "system curl" else "python-ssl",
            )
            if strict_errors:
                print(
                    f"relkit consume: download ok via {label} "
                    f"after {len(strict_errors)} strict miss(es)",
                    file=sys.stderr,
                )
            return
        except CertificateVerificationError as error:
            certificate_failed = True
            strict_errors.append(
                f"{label}: certificate verification failed: {summarize_error(error)}"
            )
        except (HTTPError, URLError, OSError, TimeoutError, RuntimeError) as error:
            strict_errors.append(f"{label}: {summarize_error(error)}")

    summary = "; ".join(strict_errors) if strict_errors else "no transports tried"
    if not certificate_failed:
        log_transport("failure", host=host or "-", summary=summary)
        raise RuntimeError(
            "strict transports failed without a certificate verification error; "
            "refusing insecure fallback: "
            + summary
        )
    if not allow_insecure:
        hint = (
            f"set {ALLOW_INSECURE_ENV}=1 to opt into a checksum-guarded "
            "insecure download (lock sha256 still required)"
        )
        if ca_bundle is None and os.name == "nt":
            hint += (
                "; or export a PEM via SSL_CERT_FILE / CURL_CA_BUNDLE "
                "(Windows Root+CA) before consume"
            )
        log_transport("failure", host=host or "-", summary=summary)
        raise RuntimeError(
            "certificate verification failed on all strict transports; "
            f"{hint}: {summary}"
        )

    fallback_methods: list[tuple[str, Any]] = [
        (
            "Python HTTPS without TLS verify",
            lambda: download_with_python(url, destination, verify=False),
        ),
        (
            "system curl --insecure",
            lambda: curl_download(extra_args=("--insecure",)),
        ),
    ]
    fallback_errors: list[str] = []
    for label, download in fallback_methods:
        destination.unlink(missing_ok=True)
        try:
            download()
            log_transport(
                "insecure-success",
                method=label,
                host=host or "-",
                ca=ca_source_label(ca_bundle),
            )
            print(
                "relkit consume: opted-in insecure fallback via "
                f"{label}; downloaded bytes must still match lock sha256",
                file=sys.stderr,
            )
            return
        except (HTTPError, URLError, OSError, TimeoutError, RuntimeError) as error:
            fallback_errors.append(f"{label}: {summarize_error(error)}")
    log_transport(
        "failure",
        host=host or "-",
        summary="; ".join([*strict_errors, *fallback_errors]),
    )
    raise RuntimeError("; ".join([*strict_errors, *fallback_errors]))


def download_artifact(
    root: Path,
    component: str,
    spec: dict[str, str],
    *,
    allow_insecure: Optional[bool] = None,
) -> Path:
    cache = root / ".relkit" / "cache" / "artifacts"
    cache.mkdir(parents=True, exist_ok=True)
    destination = cache / spec["sha256"]
    if destination.is_file() and file_sha256(destination) == spec["sha256"]:
        print(f"relkit consume: cache hit {component} {spec['sha256'][:12]}")
        return destination
    destination.unlink(missing_ok=True)
    errors: list[str] = []
    for attempt in range(1, DOWNLOAD_ATTEMPTS + 1):
        temporary = destination.with_suffix(f".tmp-{os.getpid()}")
        temporary.unlink(missing_ok=True)
        try:
            print(f"relkit consume: download {component} ({attempt}/{DOWNLOAD_ATTEMPTS})")
            fetch_url(spec["url"], temporary, allow_insecure=allow_insecure)
            actual = file_sha256(temporary)
            if actual != spec["sha256"]:
                raise RuntimeError(
                    f"sha256 mismatch: expected {spec['sha256']}, got {actual}"
                )
            os.replace(temporary, destination)
            return destination
        except (HTTPError, URLError, OSError, TimeoutError, RuntimeError) as error:
            temporary.unlink(missing_ok=True)
            errors.append(summarize_error(error))
            if attempt < DOWNLOAD_ATTEMPTS:
                time.sleep(2**attempt)
    raise RuntimeError(
        f"cannot download {component} from {spec['url']}:\n  - "
        + "\n  - ".join(errors)
    )


def binary_destination(root: Path, component: str, target: str) -> Path:
    row = BY_NAME.get(component)
    if row is None or row.role != "product-binary":
        raise RuntimeError(f"{component} is not a binary component")
    return root / row.destination / row.install_name(target)


def install_binary(
    root: Path, component: str, target: str, artifact: Path, digest: str
) -> Path:
    destination = binary_destination(root, component, target)
    if destination.is_file() and file_sha256(destination) == digest:
        print(f"relkit consume: verified {destination.relative_to(root)}")
        return destination
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_suffix(destination.suffix + f".tmp-{os.getpid()}")
    shutil.copyfile(artifact, temporary)
    if not target.startswith("windows-"):
        temporary.chmod(temporary.stat().st_mode | 0o755)
    os.replace(temporary, destination)
    if file_sha256(destination) != digest:
        raise RuntimeError(f"installed binary hash drifted: {destination}")
    print(f"relkit consume: installed {destination.relative_to(root)}")
    return destination


def remove_tree(path: Path, *, ignore_errors: bool = False) -> None:
    """Delete a tree that may contain read-only files.

    Windows refuses os.unlink on read-only entries, and a tree replaced in
    place can legitimately hold them: a sparse checkout leaves .git/objects/pack
    read-only. Clearing the bit on failure keeps install idempotent instead of
    stranding a half-removed backup beside the real destination.
    """

    def clear_readonly(func: Any, target: Any, _exc: Any) -> None:
        try:
            os.chmod(target, stat.S_IWRITE)
            func(target)
        except OSError:
            if not ignore_errors:
                raise

    if sys.version_info >= (3, 12):
        shutil.rmtree(path, onexc=clear_readonly)
    else:  # pragma: no cover - exercised on hosts older than 3.12
        shutil.rmtree(path, onerror=clear_readonly)


def sdk_destination(root: Path, component: str) -> Path:
    """Where a product-tree artifact lands."""
    row = BY_NAME.get(component)
    if row is None or row.role != "product-tree" or component == "host-scripts":
        raise RuntimeError(f"{component} is not an SDK/bindings tree component")
    return root / row.destination


def clear_orphan_relkit_proto(root: Path) -> None:
    """Remove leftover third_party/relkit/proto that shadows the packaged Rust IDL.

    Older consume layouts left a sibling proto tree. Rust build.rs prefers
    manifest_dir/../../proto when present, so a stale product-tree path wins
    over sdk/rust/proto even after sdk-rust is current. That path is not a
    registry destination, so deleting it is safe.
    """
    orphan = root / "third_party" / "relkit" / "proto"
    if not orphan.exists():
        return
    destinations = {
        row.destination.rstrip("/")
        for row in product_components()
        if row.role == "product-tree" and row.name != "host-scripts"
    }
    relative = "third_party/relkit/proto"
    if relative in destinations:
        return
    remove_tree(orphan)
    print(f"relkit consume: removed orphan {relative} (shadows packaged sdk-rust IDL)")


def preserved_sdk_subtrees(component: str) -> tuple[str, ...]:
    """Other registry trees nested below the tree being replaced."""
    owner = BY_NAME[component].destination.rstrip("/")
    prefix = owner + "/"
    return tuple(
        row.destination[len(prefix):]
        for row in product_components()
        if row.role == "product-tree"
        and row.name not in (component, "host-scripts")
        and row.destination.startswith(prefix)
    )


def sdk_complete(destination: Path, component: str) -> bool:
    row = BY_NAME.get(component)
    if row is None or row.role != "product-tree":
        return False
    return all((destination / relative).exists() for relative in row.required_paths)


def host_scripts_tree_sha256(directory: Path) -> str:
    try:
        return tree_sha256(directory)
    except ValueError as error:
        raise RuntimeError("host scripts artifact is empty") from error


def safe_extract_host_scripts(
    artifact: Path, destination: Path, expected_tree_hash: str
) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(
        tempfile.mkdtemp(prefix=".host-scripts-", dir=str(destination.parent))
    )
    try:
        with zipfile.ZipFile(artifact) as archive:
            names = sorted(
                item.filename for item in archive.infolist() if not item.is_dir()
            )
            expected = set(names)
            required = set(BY_NAME["host-scripts"].required_paths)
            if not required.issubset(expected) or any(
                name.startswith("/") or ".." in Path(name).parts or "__pycache__" in Path(name).parts
                for name in names
            ):
                raise RuntimeError("host scripts artifact has unsafe or incomplete tree")
            root = temporary.resolve()
            for member in archive.infolist():
                target = (temporary / member.filename).resolve()
                if target != root and root not in target.parents:
                    raise RuntimeError(f"unsafe host scripts archive path: {member.filename}")
            archive.extractall(temporary)
        actual = host_scripts_tree_sha256(temporary)
        if actual != expected_tree_hash:
            raise RuntimeError(
                "host scripts tree hash mismatch: "
                f"expected {expected_tree_hash}, got {actual}"
            )
        destination.mkdir(parents=True, exist_ok=True)
        backup = destination.with_name(destination.name + ".relkit-old")
        try:
            if backup.exists():
                remove_tree(backup)
            os.replace(destination, backup)
            os.replace(temporary, destination)
            if host_scripts_tree_sha256(destination) != expected_tree_hash:
                raise RuntimeError("installed host scripts hash drifted")
        except Exception as install_error:
            if destination.exists():
                remove_tree(destination, ignore_errors=True)
            if backup.exists():
                os.replace(backup, destination)
            raise
        if backup.exists():
            remove_tree(backup, ignore_errors=True)
        print("relkit consume: installed scripts/host")
    finally:
        shutil.rmtree(temporary, ignore_errors=True)


def sdk_display(destination: Path) -> str:
    parts = destination.parts
    if "third_party" in parts:
        return "/".join(parts[parts.index("third_party") :])
    return destination.name


def carry_preserved_subtrees(
    destination: Path, staging: Path, component: str
) -> None:
    """Copy sibling SDK trees into the staging tree before it replaces the old one."""
    for relative in preserved_sdk_subtrees(component):
        existing = destination / relative
        if not existing.is_dir():
            continue
        carried = staging / relative
        if carried.exists():
            remove_tree(carried)
        carried.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(existing, carried)


def replace_watched_tree_on_windows(staging: Path, destination: Path, backup: Path) -> None:
    """Replace a tree whose directory handle is held by an IDE watcher.

    Windows cannot rename a watched directory even when every file is closed.
    Keep file replacement atomic and retain a full rollback copy instead.
    """
    shutil.copytree(destination, backup)
    desired_files = {
        path.relative_to(staging)
        for path in staging.rglob("*")
        if path.is_file()
    }
    try:
        for source in sorted(staging.rglob("*")):
            relative = source.relative_to(staging)
            target = destination / relative
            if source.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            pending = target.with_name(target.name + ".relkit-new")
            shutil.copy2(source, pending)
            os.replace(pending, target)
        for existing in sorted(destination.rglob("*"), reverse=True):
            relative = existing.relative_to(destination)
            if existing.is_file() and relative not in desired_files:
                existing.unlink()
            elif existing.is_dir():
                try:
                    existing.rmdir()
                except OSError:
                    pass
    except Exception:
        # Restore the previous tree file-by-file; the watched directory itself
        # remains stable throughout both install and rollback.
        for existing in sorted(destination.rglob("*"), reverse=True):
            if existing.is_file():
                existing.unlink()
        for source in sorted(backup.rglob("*")):
            relative = source.relative_to(backup)
            target = destination / relative
            if source.is_dir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(source, target)
        raise


def safe_extract_sdk(
    artifact: Path, destination: Path, digest: str, component: str = "sdk-dart"
) -> None:
    label = sdk_display(destination)
    marker = destination / ".relkit-artifact.json"
    if marker.is_file():
        try:
            state = json.loads(marker.read_text(encoding="utf-8"))
            if (
                state.get("sha256") == digest
                and sdk_complete(destination, component)
            ):
                print(f"relkit consume: verified {label}")
                return
        except (OSError, ValueError):
            pass

    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(
        tempfile.mkdtemp(prefix=f".{component[4:]}-sdk-", dir=str(destination.parent))
    )
    backup = destination.with_name(destination.name + ".relkit-old")
    try:
        with zipfile.ZipFile(artifact) as archive:
            root = temporary.resolve()
            for member in archive.infolist():
                target = (temporary / member.filename).resolve()
                if target != root and root not in target.parents:
                    raise RuntimeError(f"unsafe SDK archive path: {member.filename}")
            archive.extractall(temporary)
        if not sdk_complete(temporary, component):
            raise RuntimeError(f"{component} artifact is incomplete")
        carry_preserved_subtrees(destination, temporary, component)
        (temporary / ".relkit-artifact.json").write_text(
            json.dumps({"schema": LOCK_SCHEMA, "sha256": digest}, indent=2) + "\n",
            encoding="utf-8",
        )
        if backup.exists():
            remove_tree(backup)
        if destination.exists():
            try:
                os.replace(destination, backup)
            except PermissionError:
                if os.name != "nt":
                    raise
                replace_watched_tree_on_windows(temporary, destination, backup)
                remove_tree(temporary, ignore_errors=True)
            else:
                os.replace(temporary, destination)
        else:
            os.replace(temporary, destination)
        if backup.exists():
            # The new tree is already in place; a stubborn backup is litter,
            # not a failed install.
            try:
                remove_tree(backup)
            except OSError as error:
                print(
                    f"relkit consume: installed {label}, but could not remove "
                    f"{backup.name} ({error}); remove it manually",
                    file=sys.stderr,
                )
        print(f"relkit consume: installed {label}")
    except Exception:
        if not destination.exists() and backup.exists():
            os.replace(backup, destination)
        raise
    finally:
        if temporary.exists():
            remove_tree(temporary, ignore_errors=True)


def verify_binary(path: Path, component: str) -> None:
    result = subprocess.run(
        [str(path), "--version"],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=30,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(
            f"{component} smoke test failed ({result.returncode}): "
            f"{(result.stderr or result.stdout).strip()}"
        )


def check_installed(
    root: Path, lock: dict[str, Any], component: str, target: str
) -> None:
    spec = artifact_spec(lock, component, target)
    if component == "host-scripts":
        expected = str(lock.get("hostScriptsSha256") or "").lower()
        if not re.fullmatch(r"[0-9a-f]{64}", expected):
            raise RuntimeError("lock has no valid hostScriptsSha256")
        actual = host_scripts_tree_sha256(root / "scripts" / "host")
        if actual != expected:
            raise RuntimeError("installed scripts/host does not match lock")
        return
    if BY_NAME[component].role == "product-tree":
        destination = sdk_destination(root, component)
        marker = destination / ".relkit-artifact.json"
        state = json.loads(marker.read_text(encoding="utf-8"))
        if state.get("sha256") != spec["sha256"]:
            raise RuntimeError(f"installed {component} does not match lock")
        if not sdk_complete(destination, component):
            raise RuntimeError(f"installed {component} is incomplete")
        return
    destination = binary_destination(root, component, target)
    if not destination.is_file() or file_sha256(destination) != spec["sha256"]:
        raise RuntimeError(f"installed {component} does not match lock: {destination}")
    verify_binary(destination, component)


def create_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Install checksum-pinned relkit release artifacts"
    )
    parser.add_argument("command", choices=("install", "check"))
    parser.add_argument("--lock", help="relkit.consume/2 lock JSON")
    parser.add_argument("--project-root", help="host project root")
    parser.add_argument("--target", default="host", choices=("host", *TARGETS))
    parser.add_argument(
        "--component",
        action="append",
        choices=COMPONENTS,
        help="component to install/check; repeatable (default: all)",
    )
    parser.add_argument("--resolved-out", help="write resolved installation JSON")
    parser.add_argument(
        "--allow-insecure",
        action="store_true",
        help=(
            "opt into checksum-guarded insecure TLS fallback after every strict "
            f"transport fails certificate checks (same as {ALLOW_INSECURE_ENV}=1)"
        ),
    )
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    force_utf8_stdio()
    try:
        args = create_parser().parse_args(argv)
        root = (
            Path(args.project_root).resolve()
            if args.project_root
            else host_root()
        )
        lock_path = (
            Path(args.lock).resolve()
            if args.lock
            else root / "scripts" / "relkit.lock.json"
        )
        lock = load_lock(lock_path)
        target = host_target() if args.target == "host" else args.target
        components = args.component or list(DEFAULT_COMPONENTS)
        if "host-scripts" in lock["artifacts"] and "host-scripts" not in components:
            components.insert(0, "host-scripts")
        allow_insecure = True if args.allow_insecure else None
        resolved_artifacts: dict[str, str] = {}
        for component in components:
            spec = artifact_spec(lock, component, target)
            resolved_artifacts[component] = spec["sha256"]
            if args.command == "install":
                artifact = download_artifact(
                    root, component, spec, allow_insecure=allow_insecure
                )
                if component == "host-scripts":
                    expected = str(lock.get("hostScriptsSha256") or "").lower()
                    if not re.fullmatch(r"[0-9a-f]{64}", expected):
                        raise RuntimeError("lock has no valid hostScriptsSha256")
                    safe_extract_host_scripts(
                        artifact,
                        root / "scripts" / "host",
                        expected,
                    )
                elif BY_NAME[component].role == "product-tree":
                    safe_extract_sdk(
                        artifact,
                        sdk_destination(root, component),
                        spec["sha256"],
                        component,
                    )
                else:
                    destination = install_binary(
                        root, component, target, artifact, spec["sha256"]
                    )
                    verify_binary(destination, component)
            check_installed(root, lock, component, target)
            if component == "sdk-rust":
                clear_orphan_relkit_proto(root)
        resolved = {
            "schema": LOCK_SCHEMA,
            "release": lock["release"],
            "commit": lock["commit"],
            "target": target,
            "artifacts": resolved_artifacts,
        }
        if args.resolved_out:
            output = Path(args.resolved_out)
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(json.dumps(resolved, indent=2) + "\n", encoding="utf-8")
        print(
            f"relkit consume: {args.command} complete "
            f"release={lock['release']} target={target}"
        )
        return 0
    except Exception as error:
        print(f"ERROR: relkit consume failed: {error}", file=sys.stderr)
        print(traceback.format_exc(), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
