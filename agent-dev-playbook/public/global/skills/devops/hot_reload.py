#!/usr/bin/env python3
"""从 BK-Repo 热更新 devops skill。
执行 TTL 检查、远端版本探测、ZIP 下载与校验、逐文件覆盖及版本记录。
BK-Repo 的 X-Checksum-Sha256 直接作为版本号，并用于下载后二次校验。
"""
from __future__ import annotations

import argparse
import contextlib
import hashlib
import io
import json
import os
import shutil
import sys
import tempfile
import time
import urllib.request
import zipfile
from pathlib import Path


SKILL_KEY = "devops"
DEFAULT_ARTIFACT_URL = (
    "https://bkrepo.woa.com/generic/bkdevops/static/skill/devops.zip"
)
DEFAULT_STATE_HOME = "~/.devops-skill-manager"
TTL_SECONDS = 300
HTTP_TIMEOUT = 10
LOCK_STALE_SECONDS = 600
BACKUP_KEEP = 5

SKILL_DIR = Path(__file__).resolve().parent


class LockBusy(RuntimeError):
    """同一安装目录已有进程在更新。"""

# 用户配置和 Knot 安装元数据永远不参与覆盖、删除和发布打包。
PRESERVE_FILES = {"config.json"}
PRESERVE_DIRS = {".knot"}


def _log(message: str) -> None:
    print(f"[devops-skill] {message}", file=sys.stderr)


def _emit(payload: dict, report: bool) -> None:
    message = str(payload.get("message") or "").strip()
    if message:
        _log(message)
    if report:
        print(json.dumps(payload, ensure_ascii=False, indent=2))


def _artifact_url() -> str:
    url = (
        os.environ.get("DEVOPS_SKILL_ARTIFACT_URL", "").strip()
        or DEFAULT_ARTIFACT_URL
    )
    # ZIP 会直接覆盖可执行的 Skill 脚本，SHA256 又来自同一个响应，
    # 所以传输层必须是 https，不能退化成 http:// 或 file://。
    if not url.lower().startswith("https://"):
        raise ValueError(f"更新源必须是 https:// 地址：{url}")
    return url


def _state_dir() -> Path:
    home = os.environ.get("DEVOPS_SKILL_MANAGER_HOME") or DEFAULT_STATE_HOME
    base = Path(os.path.expandvars(os.path.expanduser(str(home))))
    key = hashlib.sha1(str(SKILL_DIR).encode("utf-8")).hexdigest()[:16]
    return base / "hot_reload" / key


def _cache_file() -> Path:
    return _state_dir() / ".update_cache"


def _version_file() -> Path:
    return _state_dir() / ".skill_version"


def _manifest_file() -> Path:
    return _state_dir() / ".skill_files"


def _lock_file() -> Path:
    return _state_dir() / ".install_lock"


def _backup_dir() -> Path:
    return _state_dir() / "backups"


def _read_text(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8").strip()
    except Exception:
        return ""


def _local_version() -> str:
    return _read_text(_version_file())


def _ttl_valid() -> bool:
    try:
        last = float(_read_text(_cache_file()))
    except (TypeError, ValueError):
        return False
    return (time.time() - last) < TTL_SECONDS


def _write_state(path: Path, value: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(value, encoding="utf-8")


def _touch_ttl() -> None:
    _write_state(_cache_file(), str(time.time()))


def _write_version(version: str) -> None:
    _write_state(_version_file(), version)


def _read_manifest() -> set[str] | None:
    """返回上一次装进 SKILL_DIR 的文件清单；没有记录时返回 None。"""
    try:
        data = json.loads(_manifest_file().read_text(encoding="utf-8"))
    except Exception:
        return None
    return set(data) if isinstance(data, list) else None


def _write_manifest(expected: dict[str, str]) -> None:
    _write_state(
        _manifest_file(),
        json.dumps(sorted(expected), ensure_ascii=False),
    )


@contextlib.contextmanager
def _install_lock():
    """同一安装目录同时只允许一个进程覆盖文件。"""
    path = _lock_file()
    path.parent.mkdir(parents=True, exist_ok=True)
    try:
        age = time.time() - path.stat().st_mtime
    except OSError:
        age = None
    if age is not None and age > LOCK_STALE_SECONDS:
        _remove_file(path)
    try:
        handle = os.open(str(path), os.O_CREAT | os.O_EXCL | os.O_WRONLY)
    except FileExistsError:
        raise LockBusy(f"另一个进程正在更新同一个 Skill 目录：{path}") from None
    try:
        os.write(handle, str(os.getpid()).encode("utf-8"))
    finally:
        os.close(handle)
    try:
        yield
    finally:
        _remove_file(path)


def _in_source_directory() -> bool:
    """识别本仓库的源码 checkout。

    只看 `CONTRIBUTING_GUIDE.md` 会误判任何恰好带同名文件的安装父目录，
    再要求一个 `.git` 才算源码树。
    """
    try:
        parent = SKILL_DIR.parent
        return (
            (SKILL_DIR / "SKILL.md").is_file()
            and (parent / "CONTRIBUTING_GUIDE.md").is_file()
            and (parent / ".git").exists()
        )
    except Exception:
        return False


def _allow_source_overwrite() -> bool:
    return os.environ.get("DEVOPS_HOT_RELOAD_IN_SOURCE", "").strip().lower() in {
        "1",
        "true",
        "yes",
    }


def _as_bool(value: object, default: bool = True) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return bool(value)
    text = str(value).strip().lower()
    if text in {"1", "true", "yes", "on"}:
        return True
    if text in {"0", "false", "no", "off"}:
        return False
    return default


def _load_local_config() -> dict:
    path = SKILL_DIR / "config.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return {}
    return data if isinstance(data, dict) else {}


def _config_hot_reload_enabled() -> bool:
    return _as_bool(_load_local_config().get("hot_reload"), default=True)


def _disabled_reason(*, force: bool = False) -> str:
    if os.environ.get("DEVOPS_NO_HOT_RELOAD"):
        return "DEVOPS_NO_HOT_RELOAD"
    if force:
        return ""
    if not _config_hot_reload_enabled():
        return "config.json"
    return ""


def _preserved(rel: Path) -> bool:
    return rel.name in PRESERVE_FILES or any(part in PRESERVE_DIRS for part in rel.parts)


def _ignored(rel: Path) -> bool:
    return (
        _preserved(rel)
        or rel.name == ".DS_Store"
        or rel.suffix in {".pyc", ".pyo"}
        or "__pycache__" in rel.parts
    )


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(64 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _file_hashes(root: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    if not root.is_dir():
        return result
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        rel = path.relative_to(root)
        if not _ignored(rel):
            result[rel.as_posix()] = _sha256_file(path)
    return result


def _valid_sha256(value: str) -> str:
    digest = value.strip().strip('"').lower()
    if len(digest) != 64 or any(ch not in "0123456789abcdef" for ch in digest):
        return ""
    return digest


def _remote_info() -> dict:
    url = _artifact_url()
    request = urllib.request.Request(
        url,
        method="HEAD",
        headers={"User-Agent": f"devops-hot-reload/{SKILL_KEY}"},
    )
    with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT) as response:
        checksum = _valid_sha256(response.headers.get("X-Checksum-Sha256", ""))
        if not checksum:
            checksum = _valid_sha256(response.headers.get("ETag", ""))
        if not checksum:
            raise ValueError("BK-Repo HEAD 响应缺少有效的 X-Checksum-Sha256")
        return {
            "version": checksum,
            "last_modified": response.headers.get("Last-Modified", ""),
            "content_length": response.headers.get("Content-Length", ""),
            "artifact_url": url,
        }


def _download_zip(info: dict) -> bytes:
    request = urllib.request.Request(
        str(info["artifact_url"]),
        headers={"User-Agent": f"devops-hot-reload/{SKILL_KEY}"},
    )
    with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT * 3) as response:
        data = response.read()
    actual = hashlib.sha256(data).hexdigest()
    expected = str(info["version"])
    if actual != expected:
        raise RuntimeError(f"ZIP SHA256 校验失败：期望 {expected}，实际 {actual}")
    return data


def _safe_extract(zip_bytes: bytes, destination: Path) -> Path:
    destination = destination.resolve()
    with zipfile.ZipFile(io.BytesIO(zip_bytes)) as archive:
        for member in archive.infolist():
            name = member.filename.replace("\\", "/")
            path = Path(name)
            if name.startswith("/") or path.is_absolute() or ".." in path.parts:
                raise ValueError(f"ZIP 含非法路径：{member.filename}")
            target = (destination / path).resolve()
            try:
                target.relative_to(destination)
            except ValueError as exc:
                raise ValueError(f"ZIP 含非法路径：{member.filename}") from exc
        archive.extractall(destination)

    direct = destination / SKILL_KEY
    if (direct / "SKILL.md").is_file():
        return direct
    if (destination / "SKILL.md").is_file():
        return destination
    candidates = [path.parent for path in destination.rglob("SKILL.md")]
    if len(candidates) != 1:
        raise ValueError("更新包中找不到唯一的 SKILL.md")
    return candidates[0]


def _replace_file(source: Path, target: Path) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    temporary = target.with_name(target.name + ".tmp")
    shutil.copy2(source, temporary)
    try:
        temporary.replace(target)
    except OSError:
        shutil.copy2(source, target)
        try:
            temporary.unlink()
        except Exception:
            pass


def _stale_files(current: dict[str, str], expected: dict[str, str]) -> set[str]:
    """安装目录里也会有用户自己的产物（下载的制品、日志、二维码），不能一概删掉。

    有上一次的安装清单时只删清单内的文件；没有清单时退化为「只删 ZIP 同样提供的
    子目录里的文件」，顶层陌生文件一律保留。
    """
    stale = set(current) - set(expected)
    installed = _read_manifest()
    if installed is not None:
        return stale & installed
    owned = {rel.split("/", 1)[0] for rel in expected if "/" in rel}
    return {rel for rel in stale if "/" in rel and rel.split("/", 1)[0] in owned}


def _snapshot(root: Path, backup: Path) -> None:
    backup.mkdir(parents=True, exist_ok=True)
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        rel = path.relative_to(root)
        if _ignored(rel):
            continue
        target = backup / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)


def _restore(backup: Path, root: Path) -> None:
    saved = _file_hashes(backup)
    for rel in saved:
        _replace_file(backup / rel, root / rel)
    for rel in set(_file_hashes(root)) - set(saved):
        _remove_file(root / rel)
    _prune_empty_dirs(root)


def _remove_file(path: Path) -> None:
    try:
        path.unlink()
    except FileNotFoundError:
        pass
    except OSError:
        # 只读文件或权限问题：放弃删除，留着比中断整次安装安全。
        pass


def _prune_empty_dirs(root: Path) -> None:
    for directory in sorted(
        (path for path in root.rglob("*") if path.is_dir()),
        key=lambda path: len(path.parts),
        reverse=True,
    ):
        if any(part in PRESERVE_DIRS for part in directory.relative_to(root).parts):
            continue
        try:
            directory.rmdir()
        except OSError:
            pass


def _apply_payload(payload: Path, expected: dict[str, str]) -> None:
    for rel, digest in expected.items():
        target = SKILL_DIR / rel
        if target.is_file() and _sha256_file(target) == digest:
            continue
        _replace_file(payload / rel, target)

    for rel in sorted(_stale_files(_file_hashes(SKILL_DIR), expected)):
        _remove_file(SKILL_DIR / rel)

    _prune_empty_dirs(SKILL_DIR)

    actual = _file_hashes(SKILL_DIR)
    missing = {rel for rel, digest in expected.items() if actual.get(rel) != digest}
    if missing:
        raise RuntimeError(f"逐文件覆盖后的内容与 ZIP 不一致：{sorted(missing)[:5]}")


def _files_to_backup(current: dict[str, str], expected: dict[str, str]) -> list[str]:
    """即将被覆盖或删除的 Skill 自身文件。

    安装目录里的制品、日志和二维码这次更新不会动，也不打进备份包。
    """
    installed = _read_manifest() or set()
    stale = _stale_files(current, expected)
    selected = (set(current) & (installed | set(expected))) | stale
    return sorted(selected)


def _backup_archives(directory: Path) -> list[Path]:
    archives = [path for path in directory.glob("*.zip") if path.is_file()]
    return sorted(archives, key=lambda path: (path.stat().st_mtime_ns, path.name))


def _prune_backups(directory: Path) -> None:
    archives = _backup_archives(directory)
    for old in archives[:-BACKUP_KEEP]:
        _remove_file(old)


def _backup_stamp() -> str:
    nanos = time.time_ns()
    seconds, fraction = divmod(nanos, 1_000_000_000)
    return time.strftime("%Y%m%dT%H%M%S", time.gmtime(seconds)) + f"{fraction:09d}Z"


def _save_local_backup(files: list[str]) -> Path | None:
    """把覆盖前的本地 Skill 版本打成 ZIP，留在状态目录。"""
    existing = [rel for rel in files if (SKILL_DIR / rel).is_file()]
    if not existing:
        return None
    directory = _backup_dir()
    directory.mkdir(parents=True, exist_ok=True)
    version = _local_version()[:12] or "unversioned"
    output = directory / f"{_backup_stamp()}-{version}.zip"
    while output.exists():
        output = directory / f"{_backup_stamp()}-{version}.zip"
    temporary = output.with_name(output.name + ".tmp")
    written = 0
    try:
        with zipfile.ZipFile(temporary, "w", compression=zipfile.ZIP_DEFLATED) as archive:
            for rel in existing:
                source = SKILL_DIR / rel
                if not source.is_file():
                    continue
                archive.write(source, f"{SKILL_KEY}/{rel}")
                written += 1
        if written == 0:
            return None
        temporary.replace(output)
    finally:
        if temporary.exists():
            _remove_file(temporary)
    _prune_backups(directory)
    return output


def _latest_backup() -> str:
    directory = _backup_dir()
    if not directory.is_dir():
        return ""
    archives = _backup_archives(directory)
    return str(archives[-1]) if archives else ""


def _install_zip(zip_bytes: bytes) -> Path | None:
    with tempfile.TemporaryDirectory(prefix="devops_skill_") as staging, tempfile.TemporaryDirectory(
        prefix="devops_skill_backup_"
    ) as rollback:
        payload = _safe_extract(zip_bytes, Path(staging))
        expected = _file_hashes(payload)
        local_backup = _save_local_backup(_files_to_backup(_file_hashes(SKILL_DIR), expected))
        backup = Path(rollback)
        _snapshot(SKILL_DIR, backup)
        try:
            _apply_payload(payload, expected)
        except Exception:
            # 半新半旧的安装目录是不可用的，失败必须退回原状再上报。
            _restore(backup, SKILL_DIR)
            raise
        _write_manifest(expected)
        return local_backup


def _skill_dir_writable() -> bool:
    try:
        probe = tempfile.NamedTemporaryFile(dir=str(SKILL_DIR), prefix=".devops_probe_")
        probe.close()
        return True
    except Exception:
        return False


def _failure_exit_code(force: bool) -> int:
    """Fail open for session startup, but keep explicit updates strict."""
    if not force:
        try:
            _touch_ttl()
        except Exception:
            pass
        return 0
    return 1


def status_payload() -> dict:
    disabled_by = _disabled_reason()
    try:
        artifact_url = _artifact_url()
    except ValueError as exc:
        artifact_url = f"<invalid: {exc}>"
    return {
        "skill_dir": str(SKILL_DIR),
        "artifact_url": artifact_url,
        "local_version": _local_version(),
        "ttl_valid": _ttl_valid(),
        "in_source_directory": _in_source_directory(),
        "writable": _skill_dir_writable(),
        "hot_reload_enabled": not disabled_by,
        "hot_reload_disabled_by": disabled_by,
        "local_backup": _latest_backup(),
    }


def run(force: bool = False, report: bool = False) -> int:
    disabled_by = _disabled_reason(force=force)
    if disabled_by:
        messages = {
            "DEVOPS_NO_HOT_RELOAD": "已禁用热更新，本次未检查远端。",
            "config.json": "已在 config.json 关闭热更新（hot_reload: false），本次未检查远端。",
        }
        _emit(
            {
                "ok": True,
                "updated": False,
                "action": "skipped",
                "reason": disabled_by,
                "message": messages.get(disabled_by, "已禁用热更新，本次未检查远端。"),
            },
            report,
        )
        return 0

    if _in_source_directory() and not _allow_source_overwrite():
        _emit(
            {
                "ok": True,
                "updated": False,
                "action": "skipped",
                "reason": "source_directory",
                "message": "当前是 Skill 源码目录，已跳过热更新。",
            },
            report,
        )
        return 0

    if not force and _ttl_valid():
        _emit(
            {
                "ok": True,
                "updated": False,
                "action": "skipped",
                "reason": "ttl",
                "local_version": _local_version(),
                "message": f"距离上次检查不足 {TTL_SECONDS} 秒，本次未联网。",
            },
            report,
        )
        return 0

    try:
        info = _remote_info()
    except Exception as exc:
        _emit(
            {
                "ok": False,
                "updated": False,
                "action": "error",
                "reason": "remote_probe_failed",
                "continue_with_local": True,
                "retry_recommended": False,
                "error": str(exc),
                "message": f"热更新检查失败，已停止自动重试并继续使用本地 Skill：{exc}",
            },
            report,
        )
        return _failure_exit_code(force)

    remote_version = str(info["version"])
    if remote_version == _local_version():
        try:
            _touch_ttl()
        except Exception:
            pass
        _emit(
            {
                "ok": True,
                "updated": False,
                "action": "up_to_date",
                "local_version": remote_version,
                "remote_version": remote_version,
                "message": f"当前已是最新版本：{info['last_modified'] or remote_version}。",
            },
            report,
        )
        return 0

    if not _skill_dir_writable():
        _emit(
            {
                "ok": False,
                "updated": False,
                "action": "error",
                "reason": "skill_dir_not_writable",
                "continue_with_local": True,
                "retry_recommended": False,
                "message": f"Skill 目录不可写，已停止自动重试并继续使用本地 Skill：{SKILL_DIR}",
            },
            report,
        )
        return _failure_exit_code(force)

    previous = _local_version()
    local_backup: Path | None = None
    try:
        with _install_lock():
            local_backup = _install_zip(_download_zip(info))
            _write_version(remote_version)
            _touch_ttl()
    except LockBusy as exc:
        _emit(
            {
                "ok": True,
                "updated": False,
                "action": "skipped",
                "reason": "locked",
                "local_version": previous,
                "remote_version": remote_version,
                "message": f"已有其他进程在更新，本次沿用本地 Skill：{exc}",
            },
            report,
        )
        return 0
    except Exception as exc:
        _emit(
            {
                "ok": False,
                "updated": False,
                "action": "error",
                "reason": "update_failed",
                "local_version": previous,
                "remote_version": remote_version,
                "continue_with_local": True,
                "retry_recommended": False,
                "error": str(exc),
                "message": f"热更新失败，已停止自动重试并继续使用本地 Skill：{exc}",
            },
            report,
        )
        return _failure_exit_code(force)

    message = f"热更新成功：{previous or '未记录'} → {info['last_modified'] or remote_version}。"
    if local_backup is not None:
        message += f" 更新前的本地版本已保存：{local_backup}"
    _emit(
        {
            "ok": True,
            "updated": True,
            "action": "updated",
            "previous_version": previous,
            "local_version": remote_version,
            "remote_version": remote_version,
            "local_backup": str(local_backup or ""),
            "message": message,
        },
        report,
    )
    return 0


def pack_zip(output: Path) -> Path:
    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for source in SKILL_DIR.rglob("*"):
            if not source.is_file():
                continue
            # 输出路径常常就落在 SKILL_DIR 里，不排除会把半成品 ZIP 打进自己。
            if source.resolve() == output:
                continue
            rel = source.relative_to(SKILL_DIR)
            if _ignored(rel):
                continue
            archive.write(source, f"{SKILL_KEY}/{rel.as_posix()}")
    return output


def _self_test() -> int:
    failures: list[str] = []

    def check(condition: bool, message: str) -> None:
        if not condition:
            failures.append(message)

    with tempfile.TemporaryDirectory(prefix="devops_hr_test_") as temporary:
        root = Path(temporary)
        global SKILL_DIR
        original_dir = SKILL_DIR
        original_home = os.environ.get("DEVOPS_SKILL_MANAGER_HOME")
        os.environ["DEVOPS_SKILL_MANAGER_HOME"] = str(root / "state")
        try:
            target = root / "installed" / SKILL_KEY
            target.mkdir(parents=True)
            (target / "SKILL.md").write_text("# old\n", encoding="utf-8")
            (target / "scripts").mkdir()
            (target / "scripts" / "stale.py").write_text("old\n", encoding="utf-8")
            (target / "build.log").write_text("user log\n", encoding="utf-8")
            (target / "downloads").mkdir()
            (target / "downloads" / "app.apk").write_text("binary\n", encoding="utf-8")
            (target / "config.json").write_text(
                '{"access_token":"secret"}\n', encoding="utf-8"
            )
            knot = target / ".knot"
            knot.mkdir()
            (knot / "metadata.json").write_text("{}\n", encoding="utf-8")
            SKILL_DIR = target

            package = io.BytesIO()
            with zipfile.ZipFile(package, "w") as archive:
                archive.writestr(f"{SKILL_KEY}/SKILL.md", "# new\n")
                archive.writestr(f"{SKILL_KEY}/scripts/ok.py", "print(1)\n")
                archive.writestr(
                    f"{SKILL_KEY}/config.json", '{"access_token":"remote"}\n'
                )
                archive.writestr(f"{SKILL_KEY}/__pycache__/ignored.pyc", b"cache")
            saved = _install_zip(package.getvalue())

            check(saved is not None and saved.is_file(), "覆盖前应留下本地版本压缩包")
            with zipfile.ZipFile(saved) as archive:
                backed_up = set(archive.namelist())
                check(
                    archive.read(f"{SKILL_KEY}/SKILL.md") == b"# old\n",
                    "备份应是覆盖前的 SKILL.md",
                )
                check(
                    f"{SKILL_KEY}/scripts/stale.py" in backed_up,
                    "备份应包含即将删除的旧文件",
                )
                check(f"{SKILL_KEY}/config.json" not in backed_up, "备份不应包含 config.json")
                check(f"{SKILL_KEY}/build.log" not in backed_up, "备份不应包含用户产物")
                check(
                    f"{SKILL_KEY}/downloads/app.apk" not in backed_up,
                    "备份不应包含用户下载的制品",
                )
            check(_read_text(target / "SKILL.md") == "# new", "应更新 SKILL.md")
            check((target / "scripts" / "ok.py").is_file(), "应新增文件")
            check(
                not (target / "scripts" / "stale.py").exists(),
                "应删除 skill 自有目录下的旧文件",
            )
            check(
                (target / "build.log").is_file()
                and (target / "downloads" / "app.apk").is_file(),
                "必须保留用户在安装目录里的产物",
            )
            check(
                _read_text(target / "config.json") == '{"access_token":"secret"}',
                "必须保留本地 config.json",
            )
            check((knot / "metadata.json").is_file(), "必须保留 .knot")
            check(not (target / "__pycache__").exists(), "不应安装 pyc")
            check(
                _read_manifest() == {"SKILL.md", "scripts/ok.py"},
                "应记录本次安装的文件清单",
            )

            shrunk = io.BytesIO()
            with zipfile.ZipFile(shrunk, "w") as archive:
                archive.writestr(f"{SKILL_KEY}/SKILL.md", "# newer\n")
            _install_zip(shrunk.getvalue())
            with zipfile.ZipFile(_latest_backup()) as archive:
                check(
                    archive.read(f"{SKILL_KEY}/SKILL.md") == b"# new\n",
                    "最近一份备份应是第二次覆盖前的版本",
                )
            check(
                not (target / "scripts" / "ok.py").exists(),
                "有清单后应删除上次装进去、这次不再提供的文件",
            )
            check((target / "build.log").is_file(), "第二次安装仍须保留用户产物")

            broken = io.BytesIO()
            with zipfile.ZipFile(broken, "w") as archive:
                archive.writestr(f"{SKILL_KEY}/SKILL.md", "# broken\n")
                archive.writestr(f"{SKILL_KEY}/scripts/new.py", "print(2)\n")
            original_apply = globals()["_apply_payload"]

            def _explode(payload: Path, expected: dict) -> None:
                original_apply(payload, expected)
                raise RuntimeError("boom")

            globals()["_apply_payload"] = _explode
            try:
                _install_zip(broken.getvalue())
                check(False, "安装失败应向上抛出")
            except RuntimeError:
                pass
            finally:
                globals()["_apply_payload"] = original_apply
            check(_read_text(target / "SKILL.md") == "# newer", "安装失败应回滚已覆盖的文件")
            check(
                not (target / "scripts" / "new.py").exists(),
                "安装失败应回滚新增的文件",
            )
            check((target / "build.log").is_file(), "回滚不应误删用户产物")

            with _install_lock():
                try:
                    with _install_lock():
                        check(False, "同一目录不应允许并发安装")
                except LockBusy:
                    pass
            check(not _lock_file().exists(), "退出后应释放锁")

            digest = hashlib.sha256(b"artifact").hexdigest()
            check(_valid_sha256(digest.upper()) == digest, "应规范化 SHA256")

            original_url = os.environ.get("DEVOPS_SKILL_ARTIFACT_URL")
            os.environ["DEVOPS_SKILL_ARTIFACT_URL"] = "http://example.com/devops.zip"
            try:
                _artifact_url()
                check(False, "应拒绝非 https 更新源")
            except ValueError:
                pass
            finally:
                if original_url is None:
                    os.environ.pop("DEVOPS_SKILL_ARTIFACT_URL", None)
                else:
                    os.environ["DEVOPS_SKILL_ARTIFACT_URL"] = original_url

            packed = pack_zip(target / "devops.zip")
            with zipfile.ZipFile(packed) as archive:
                check(
                    f"{SKILL_KEY}/devops.zip" not in archive.namelist(),
                    "打包不应包含输出 ZIP 自身",
                )
            packed.unlink()

            (target / "config.json").write_text(
                '{"access_token":"secret","hot_reload":false}\n',
                encoding="utf-8",
            )
            check(_disabled_reason() == "config.json", "config.json 应能关闭自动热更新")
            check(_disabled_reason(force=True) == "", "--force 应绕过 config.json")
            check(run(force=False, report=False) == 0, "关闭热更新时 run 应跳过")
            (target / "config.json").write_text(
                '{"access_token":"secret"}\n',
                encoding="utf-8",
            )
            check(_disabled_reason() == "", "未配置 hot_reload 时应默认开启")
            check(
                _failure_exit_code(force=False) == 0,
                "会话初始化更新失败时应 fail-open",
            )
            check(_ttl_valid(), "会话初始化更新失败后应进入短暂冷却")
            check(
                _failure_exit_code(force=True) == 1,
                "用户显式更新失败时应返回非零",
            )

            backup_dir = _backup_dir()
            for index in range(BACKUP_KEEP + 2):
                (backup_dir / f"2099010{index}T000000Z-abc.zip").write_bytes(b"")
            _prune_backups(backup_dir)
            check(
                len(list(backup_dir.glob("*.zip"))) == BACKUP_KEEP,
                "只保留最近几次本地版本备份",
            )

            original_flag = os.environ.get("DEVOPS_NO_HOT_RELOAD")
            os.environ["DEVOPS_NO_HOT_RELOAD"] = "1"
            try:
                check(
                    _disabled_reason(force=True) == "DEVOPS_NO_HOT_RELOAD",
                    "环境变量应优先于 --force",
                )
            finally:
                if original_flag is None:
                    os.environ.pop("DEVOPS_NO_HOT_RELOAD", None)
                else:
                    os.environ["DEVOPS_NO_HOT_RELOAD"] = original_flag

            bad = io.BytesIO()
            with zipfile.ZipFile(bad, "w") as archive:
                archive.writestr("../escape.txt", "no")
            try:
                _safe_extract(bad.getvalue(), root / "bad")
                check(False, "应拒绝路径穿越 ZIP")
            except ValueError:
                pass
        finally:
            SKILL_DIR = original_dir
            if original_home is None:
                os.environ.pop("DEVOPS_SKILL_MANAGER_HOME", None)
            else:
                os.environ["DEVOPS_SKILL_MANAGER_HOME"] = original_home

    if failures:
        for failure in failures:
            _log(f"self-test fail: {failure}")
        return 1
    print(json.dumps({"ok": True, "self_test": "passed"}, ensure_ascii=False))
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="hot_reload.py",
        description="从 BK-Repo 热更新 devops skill。",
    )
    parser.add_argument("--force", action="store_true", help="忽略 TTL 与 config.json 开关，立即检查并更新")
    parser.add_argument("--status", action="store_true", help="输出当前热更新状态，不联网")
    parser.add_argument("--self-test", action="store_true", help="运行内置自检")
    parser.add_argument("--pack", metavar="ZIP路径", help="把当前目录打成发布用 ZIP")
    # 拼错的参数必须报错，否则 --forse 会被当成一次普通检查并回报“已跳过”。
    args = parser.parse_args(sys.argv[1:] if argv is None else argv)

    if args.self_test:
        return _self_test()
    if args.status:
        print(json.dumps(status_payload(), ensure_ascii=False, indent=2))
        return 0
    if args.pack:
        packed = pack_zip(Path(args.pack))
        print(json.dumps({"ok": True, "zip": str(packed)}, ensure_ascii=False))
        return 0
    return run(force=args.force, report=True)


if __name__ == "__main__":
    raise SystemExit(main())
