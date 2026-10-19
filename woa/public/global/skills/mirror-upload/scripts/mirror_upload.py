#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把本地文件上传到腾讯内网 generic mirror（制品预置 / 缺口补齐）。

设计对齐 ADR 0018 六修 + 腾讯源补充层：
  消费端拉取顺序 mirrors GET → 腾讯源 → 官方源 → 回填。
本脚本承担「回填 / 预置」这一头：开发者在能出公网的机器上下载官方制品，
再 PUT 进内网 mirrors，让后续 CI 构建直接命中内网，不再碰外网。

凭据只从 DEC 密钥仓库读取，绝不硬编码、不写日志、不进仓库：
  1. --env-file 显式指定的 env 文件（DEC 落地路径）
  2. 环境变量 MIRRORS_USERNAME / MIRRORS_PASSWORD
  3. 默认 DEC 密钥路径 ~/.dec/secrets/<product>/.env/mirror.env

镜像路径分区：mirror/<tool>/<version>/<filename>

用法:
  mirror_upload.py probe
  mirror_upload.py put <local_file> <tool> <version> <filename> [--no-verify]
  mirror_upload.py check <tool> <version> <filename>
  mirror_upload.py sha <local_file>
  mirror_upload.py remote-sha <url>
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

MIRRORS_BASE = "https://mirrors.tencent.com/repository/generic/relkit/mirror"
UA = "RelkitMirrorFetch/1.0"
CHUNK = 1024 * 1024
OK_CODES = (200, 201, 204, 409)


def log(message: str) -> None:
    sys.stdout.write("[mirror-upload] %s\n" % message)
    sys.stdout.flush()


def sha256_of(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(CHUNK), b""):
            digest.update(block)
    return digest.hexdigest().lower()


def parse_env_file(path: Path) -> dict:
    values = {}
    if not path.is_file():
        return values
    for raw in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        values[key.strip()] = value.strip().strip('"').strip("'")
    return values


def resolve_credentials(env_file: str | None) -> tuple[str, str]:
    """按 DEC 密钥仓库 → 环境变量优先级解析凭据。"""
    candidates = []
    if env_file:
        candidates.append(Path(env_file).expanduser())
    product = os.environ.get("DEC_PRODUCT", "woa")
    candidates.append(Path.home() / ".dec" / "secrets" / product / ".env" / "mirror.env")

    user = os.environ.get("MIRRORS_USERNAME", "").strip()
    token = os.environ.get("MIRRORS_PASSWORD", "").strip()
    if user and token:
        return user, token

    for candidate in candidates:
        values = parse_env_file(candidate)
        found_user = values.get("MIRRORS_USERNAME", "").strip()
        found_token = values.get("MIRRORS_PASSWORD", "").strip()
        if found_user and found_token:
            log("凭据来源: %s" % candidate)
            return found_user, found_token

    searched = ", ".join(str(c) for c in candidates)
    log("未找到凭据。已查找: %s" % searched)
    log("请确认 DEC 密钥仓库中存在 MIRRORS_USERNAME / MIRRORS_PASSWORD")
    sys.exit(2)


def auth_header(user: str, token: str) -> str:
    raw = ("%s:%s" % (user, token)).encode("utf-8")
    return "Basic " + base64.b64encode(raw).decode("ascii")


def mirror_url(tool: str, version: str, filename: str) -> str:
    return "%s/%s/%s/%s" % (MIRRORS_BASE, tool, version, filename)


def http_put(url: str, source: Path, user: str, token: str) -> int:
    """PUT 上传。Content-Type 必须 application/octet-stream。

    （Spring FormContentFilter 会把默认 form-urlencoded 的二进制 body
    当表单解析炸 500——ADR 0018 T8.1 实证根因。）
    """
    size = source.stat().st_size
    with source.open("rb") as handle:
        request = urllib.request.Request(
            url, data=handle, method="PUT",
            headers={
                "User-Agent": UA,
                "Authorization": auth_header(user, token),
                "Content-Type": "application/octet-stream",
                "Content-Length": str(size),
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=1800) as resp:
                log("PUT ok http=%s size=%d" % (resp.status, size))
                return resp.status
        except urllib.error.HTTPError as error:
            body = ""
            try:
                body = error.read().decode("utf-8", "replace")[:300]
            except Exception:
                pass
            log("PUT failed http=%s body=%s" % (error.code, body))
            return error.code
        except Exception as error:  # noqa: BLE001
            log("PUT failed: %s" % error)
            return -1


def http_head(url: str) -> int:
    request = urllib.request.Request(url, method="HEAD", headers={"User-Agent": UA})
    try:
        with urllib.request.urlopen(request, timeout=60) as resp:
            return resp.status
    except urllib.error.HTTPError as error:
        return error.code
    except Exception:  # noqa: BLE001
        return -1


def remote_sha(url: str) -> tuple[str, int]:
    """流式校验远端文件 sha256（不落盘）。"""
    digest = hashlib.sha256()
    total = 0
    request = urllib.request.Request(url, headers={"User-Agent": UA})
    try:
        with urllib.request.urlopen(request, timeout=1800) as resp:
            while True:
                block = resp.read(CHUNK)
                if not block:
                    break
                digest.update(block)
                total += len(block)
    except Exception as error:  # noqa: BLE001
        log("远端读取失败: %s" % error)
        return "", 0
    return digest.hexdigest().lower(), total


def cmd_probe(args: argparse.Namespace) -> int:
    user, token = resolve_credentials(args.env_file)
    probe = Path(os.environ.get("TEMP", ".")) / "mirror_write_probe.bin"
    probe.write_bytes(b"relkit mirror write probe")
    url = "%s/_probe/write-probe.txt" % MIRRORS_BASE
    code = http_put(url, probe, user, token)
    if code in OK_CODES:
        log("写权限验证通过（http=%s）" % code)
        return 0
    log("写权限验证失败（http=%s）" % code)
    return 1


def cmd_put(args: argparse.Namespace) -> int:
    user, token = resolve_credentials(args.env_file)
    local = Path(args.local_file).expanduser()
    if not local.is_file():
        log("本地文件不存在: %s" % local)
        return 2

    url = mirror_url(args.tool, args.version, args.filename)
    log("target=%s" % url)
    log("local sha256=%s size=%d" % (sha256_of(local), local.stat().st_size))

    code = http_put(url, local, user, token)
    if code not in OK_CODES:
        return 1

    if args.no_verify:
        return 0

    log("校验上传结果…")
    remote, size = remote_sha(url)
    if not remote:
        log("无法读回校验，请手动确认")
        return 0
    expected = sha256_of(local)
    if remote == expected:
        log("VERIFY MATCH sha256=%s size=%d" % (remote, size))
        return 0
    log("VERIFY MISMATCH local=%s remote=%s" % (expected, remote))
    return 1


def cmd_check(args: argparse.Namespace) -> int:
    url = mirror_url(args.tool, args.version, args.filename)
    code = http_head(url)
    if code == 200:
        log("EXISTS %s" % url)
    elif code == 404:
        log("MISSING %s" % url)
    else:
        log("UNKNOWN(http=%s) %s" % (code, url))
    return 0 if code == 200 else 1


def cmd_sha(args: argparse.Namespace) -> int:
    path = Path(args.local_file).expanduser()
    if not path.is_file():
        log("文件不存在: %s" % path)
        return 2
    print("%s  %d  %s" % (sha256_of(path), path.stat().st_size, path))
    return 0


def cmd_remote_sha(args: argparse.Namespace) -> int:
    digest, size = remote_sha(args.url)
    if digest:
        print("%s  %d" % (digest, size))
        return 0
    return 1


def main() -> int:
    parser = argparse.ArgumentParser(
        description="上传制品到腾讯内网 generic mirror（制品预置 / 缺口补齐）")
    parser.add_argument("--env-file", help="DEC 密钥 env 文件路径")
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("probe", help="用小文件验证写权限").set_defaults(func=cmd_probe)

    put = sub.add_parser("put", help="上传本地文件到 mirror")
    put.add_argument("local_file")
    put.add_argument("tool")
    put.add_argument("version")
    put.add_argument("filename")
    put.add_argument("--no-verify", action="store_true", help="跳上传后读回校验")
    put.set_defaults(func=cmd_put)

    check = sub.add_parser("check", help="检查 mirror 上文件是否存在")
    check.add_argument("tool")
    check.add_argument("version")
    check.add_argument("filename")
    check.set_defaults(func=cmd_check)

    sha = sub.add_parser("sha", help="计算本地文件 sha256")
    sha.add_argument("local_file")
    sha.set_defaults(func=cmd_sha)

    rsha = sub.add_parser("remote-sha", help="流式计算远端文件 sha256（不落盘）")
    rsha.add_argument("url")
    rsha.set_defaults(func=cmd_remote_sha)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
