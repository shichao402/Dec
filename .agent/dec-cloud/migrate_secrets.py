#!/usr/bin/env python3
"""迁移旧系统的密钥到新系统。

需求 17.4：Agent 直接读旧密钥文件，通过常规密钥写入链路提交，不设专用工具。
明文经过 Agent 上下文是一次性迁移，用户已接受。

用法：
    python3 migrate_secrets.py --dry-run    只列计划，不读值
    python3 migrate_secrets.py              实际写入

密钥值不打印、不落盘、不进日志。写入后服务端立即加密。
"""
import argparse
import json
import os
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


def parse_env(path):
    """解析 KEY=VALUE 形式的 .env，返回 {key: value}。

    值可能带引号，也可能含 =，所以只按第一个 = 切分。
    """
    out = {}
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            k, v = line.split("=", 1)
            k = k.strip()
            v = v.strip()
            if len(v) >= 2 and v[0] == v[-1] and v[0] in "\"'":
                v = v[1:-1]
            if k and v:
                out[k] = v
    return out


# 迁移清单：旧文件 -> [(新密钥名, 旧变量名)]
# 命名沿用旧变量名的小写下划线形式，便于对照；同名变量在多个文件里出现时只取一次。
PLAN = [
    ("cnb/.env/app.env", [("cnb-token", "CNB_TOKEN")]),
    ("github/.env/github.env", [("github-token", "GH_TOKEN")]),
    ("tencent-cloud/.env/tencent-cloud.env", [
        ("tencent-cloud-secret-id", "TencentAPI_SecretId"),
        ("tencent-cloud-secret-key", "TencentAPI_SecretKey"),
        ("edgeone-pages-api-token", "EDGEONE_PAGES_API_TOKEN"),
    ]),
    ("common-pub-res/.env/pub-res.env", [
        ("civitai-apikey", "civitai_apikey"),
        ("jetbrains-publishtoken", "JETBRAINS_PUBLISHTOKEN"),
    ]),
    ("common-pub-res/.env/azure.env", [
        ("vsce-pat", "VSCE_PAT"),
        ("jetbrains-publish-token", "JETBRAINS_PUBLISH_TOKEN"),
    ]),
]

NOTES = {
    "cnb-token": "CNB 平台令牌。来源：旧系统 ~/.dec/secrets/cnb/.env/app.env",
    "github-token": "GitHub 个人令牌。来源：旧系统 ~/.dec/secrets/github/.env/github.env",
    "tencent-cloud-secret-id": "腾讯云 API SecretId。来源：旧系统 tencent-cloud/.env",
    "tencent-cloud-secret-key": "腾讯云 API SecretKey。来源：旧系统 tencent-cloud/.env",
    "edgeone-pages-api-token": "EdgeOne Pages API 令牌。来源：旧系统 tencent-cloud/.env",
    "civitai-apikey": "Civitai API key。来源：旧系统 common-pub-res/.env/pub-res.env",
    "jetbrains-publishtoken": "JetBrains 发布令牌。来源：旧系统 common-pub-res/.env/pub-res.env",
    "vsce-pat": "VS Code 市场发布令牌。来源：旧系统 common-pub-res/.env/azure.env",
    "jetbrains-publish-token": "JetBrains 发布令牌（azure.env 版本）。来源：旧系统 common-pub-res/.env/azure.env",
}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    # 先收集，避免读到一半才发现文件缺失
    collected = []
    for rel, entries in PLAN:
        path = os.path.join(OLD_SECRETS, rel)
        if not os.path.exists(path):
            print(f"  跳过（文件不存在）: {rel}", file=sys.stderr)
            continue
        vals = parse_env(path)
        for new_name, old_key in entries:
            if old_key not in vals:
                print(f"  跳过（变量缺失）: {rel} 的 {old_key}", file=sys.stderr)
                continue
            collected.append((new_name, vals[old_key]))

    print(f"计划迁移 {len(collected)} 条密钥")
    for name, val in collected:
        # 只打印长度，不打印任何字符
        print(f"  {name:<28} {len(val):>3} 字符")

    if args.dry_run:
        return 0

    print()
    failed = 0
    for name, val in collected:
        r = call("put_secret", {
            "name": name,
            "value": val,
            "notes": NOTES.get(name, ""),
        })
        if r.get("error"):
            print(f"  写入 {name} 失败: {r['error']}")
            failed += 1
        else:
            print(f"  写入 {name} 成功")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
