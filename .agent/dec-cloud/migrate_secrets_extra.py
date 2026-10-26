#!/usr/bin/env python3
"""补齐 migrate_secrets.py 未覆盖的两份非 .env 密钥来源。

migrate_secrets.py 只处理 KEY=VALUE 形式的 .env。旧系统还有两处：

  vsx-publish/release.config.json   JSON（带尾逗号），三个发布令牌
  cnb/.gcm/cnb_agent.yaml           YAML，git 凭据

这两处的值都可能与 .env 里的同名变量不同，所以必须单独读、单独写，
不能按变量名去重。

用法：
    python3 migrate_secrets_extra.py --dry-run
    python3 migrate_secrets_extra.py

密钥值不打印、不落盘、不进日志。
"""
import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request

HOME = os.path.expanduser("~")
OLD_SECRETS = os.path.join(HOME, ".dec", "secrets")
LOOPBACK = "http://127.0.0.1:8787/api/call"


def call(method, payload):
    body = json.dumps({"method": method, "payload": payload}).encode()
    req = urllib.request.Request(
        LOOPBACK, data=body, headers={"Content-Type": "application/json"}
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        return {"error": e.read().decode()[:300]}


def load_json_lenient(path):
    """读带尾逗号的 JSON。

    旧文件是手写的，最后一个成员后留了逗号。不引入第三方解析器，
    先去掉 ,} 和 ,] 再解析。
    """
    with open(path, "r", encoding="utf-8") as f:
        raw = f.read()
    cleaned = re.sub(r",(\s*[}\]])", r"\1", raw)
    return json.loads(cleaned)


def read_yaml_fields(path):
    """从 cnb_agent.yaml 里取扁平的 key: value，够用即可。

    只为读一个小文件，不引入 PyYAML：这里只有单层标量与注释。
    """
    out = {}
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            s = line.strip()
            if not s or s.startswith("#") or ":" not in s:
                continue
            k, v = s.split(":", 1)
            v = v.strip()
            if len(v) >= 2 and v[0] == v[-1] and v[0] in "\"'":
                v = v[1:-1]
            out[k.strip()] = v
    return out


# 迁移清单：收集函数返回 [(新密钥名, 值, 来源说明)]
def collect():
    out = []

    rc_path = os.path.join(OLD_SECRETS, "vsx-publish/release.config.json")
    if os.path.exists(rc_path):
        rc = load_json_lenient(rc_path)
        notes_src = "旧系统 ~/.dec/secrets/vsx-publish/release.config.json"
        for key, new_name, label in [
            ("rider", "vsx-rider-token", "JetBrains Rider 发布令牌"),
            ("vscode", "vsx-vscode-pat", "VS Code 市场发布令牌（release.config.json 版本）"),
            ("openvsx", "vsx-openvsx-pat", "Open VSX 发布令牌"),
        ]:
            node = rc.get(key)
            if not isinstance(node, dict):
                print(f"  跳过（{'rider' if key else key} 段缺失）: {key}", file=sys.stderr)
                continue
            val = node.get("token") or node.get("pat") or ""
            if not val:
                print(f"  跳过（令牌为空）: {key}", file=sys.stderr)
                continue
            out.append((new_name, val, f"{label}。来源：{notes_src}"))
    else:
        print(f"  跳过（文件不存在）: {rc_path}", file=sys.stderr)

    yaml_path = os.path.join(OLD_SECRETS, "cnb/.gcm/cnb_agent.yaml")
    if os.path.exists(yaml_path):
        f = read_yaml_fields(yaml_path)
        pw = f.get("password", "")
        if pw:
            out.append((
                "cnb-git-password", pw,
                "CNB 仓库 HTTPS 访问密码（git 凭据）。"
                "来源：旧系统 ~/.dec/secrets/cnb/.gcm/cnb_agent.yaml",
            ))
        else:
            print("  跳过（password 字段为空）: cnb_agent.yaml", file=sys.stderr)
    else:
        print(f"  跳过（文件不存在）: {yaml_path}", file=sys.stderr)

    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    items = collect()
    print(f"计划迁移 {len(items)} 条密钥")
    for name, val, _ in items:
        print(f"  {name:<22} {len(val):>3} 字符")

    if args.dry_run:
        return 0

    print()
    failed = 0
    for name, val, note in items:
        r = call("put_secret", {"name": name, "value": val, "notes": note})
        if r.get("error"):
            print(f"  写入 {name} 失败: {r['error']}")
            failed += 1
        else:
            print(f"  写入 {name} 成功")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
