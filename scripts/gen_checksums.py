#!/usr/bin/env python3
"""为 version.json 写入各产物的 sha256。

发布流水线在 build 之后、发布之前调用：

    python3 scripts/gen_checksums.py dist version.json

只写入 dist/ 下形如 dec*-<os>-<arch>[.exe] 的产物，key 为产物文件名，
与 install.sh 拼出的 ${binary}-${platform} 一致，便于脚本按名查表。

install.sh 在摘要缺失时降级为显式警告而非静默通过（ADR 0019），
因此本脚本失败不应静默忽略——发布流水线需让它中止。
"""

import hashlib
import json
import os
import re
import sys

ARTIFACT_RE = re.compile(r"^dec[a-z-]*-(linux|darwin|windows)-(amd64|arm64)(\.exe)?$")


def sha256_of(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def main():
    if len(sys.argv) != 3:
        print("用法: gen_checksums.py <dist 目录> <version.json 路径>", file=sys.stderr)
        return 2

    dist_dir, version_path = sys.argv[1], sys.argv[2]
    if not os.path.isdir(dist_dir):
        print("产物目录不存在: %s" % dist_dir, file=sys.stderr)
        return 1

    checksums = {}
    for name in sorted(os.listdir(dist_dir)):
        path = os.path.join(dist_dir, name)
        if not os.path.isfile(path) or not ARTIFACT_RE.match(name):
            continue
        checksums[name] = sha256_of(path)

    if not checksums:
        print("未在 %s 找到可校验产物" % dist_dir, file=sys.stderr)
        return 1

    with open(version_path, "r", encoding="utf-8") as handle:
        data = json.load(handle)
    data["checksums"] = checksums

    with open(version_path, "w", encoding="utf-8", newline="\n") as handle:
        json.dump(data, handle, indent=2, ensure_ascii=False)
        handle.write("\n")

    print("已写入 %d 个产物摘要到 %s" % (len(checksums), version_path))
    for name in checksums:
        print("  %s  %s" % (checksums[name], name))
    return 0


if __name__ == "__main__":
    sys.exit(main())
