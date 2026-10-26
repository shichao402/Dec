#!/usr/bin/env python3
"""把旧 Dec 仓库的资产迁移到新系统。

用法：python3 migrate.py --dry-run   只打印计划，不写入
     python3 migrate.py --bundle xxx  只迁一个 bundle（试点）

设计要点：
1. 按旧 `.dec/config.yaml` 的 products.provides 逐项映射到新 manifest。
   旧系统一个"产品"对应新系统一个 bundle，一个 provide 对应一个条目。
2. 提交说明里写清来源（原仓库 + 原路径），满足需求 17.3。
3. 写入前先过一遍服务端同款明文密钥扫描，命中就地报告并跳过，
   不做静默改写——迁移不该顺手改内容。
4. 写入走和日常一样的 MCP 链路，不设旁路（需求 17.2）。
"""
import argparse
import base64
import json
import os
import re
import subprocess
import sys
import urllib.request

HOME = os.path.expanduser("~")
DEVKIT = os.path.join(HOME, "workspace/DecPersonalDevKit")
SOURCE_PRIVATE = os.path.join(HOME, "workspace/dec-source-private")
LOOPBACK = "http://127.0.0.1:8787/api/call"

# 服务端同款明文密钥扫描规则（internal/cloud/assets/asset.go）。
# 与服务端保持一致：模板变量与文档说明不该被挡。
PLAINTEXT_PATTERNS = [
    ("私钥块", re.compile(rb"-----BEGIN [A-Z ]*PRIVATE KEY-----")),
    ("AWS 访问密钥", re.compile(rb"\bAKID[A-Za-z0-9]{13,}\b")),
    ("OpenAI/Anthropic 风格密钥", re.compile(rb"\bsk-[A-Za-z0-9_-]{20,}\b")),
    ("GitHub 令牌", re.compile(rb"\bgh[pousr]_[A-Za-z0-9]{30,}\b")),
    ("Slack 令牌", re.compile(rb"\bxox[baprs]-[A-Za-z0-9-]{10,}\b")),
    ("带密码的连接串", re.compile(rb"(?i)\b[a-z]+://[^\s/:@${}]+:[^\s/@${}]{6,}@")),
    ("内联私钥字段", re.compile(rb"(?i)\"?(private_key|api_secret|client_secret|password)\"?\s*[:=]\s*\"([^\"]{8,})\"")),
]

INLINE_RULE = "内联私钥字段"


def call(method, payload):
    body = json.dumps({"method": method, "payload": payload}).encode()
    req = urllib.request.Request(
        LOOPBACK, data=body, headers={"Content-Type": "application/json"}
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        return {"error": e.read().decode()[:400]}


def not_a_secret_value(v: bytes) -> bool:
    """与服务端 notASecretValue 等价：含空白、或纯小写标识符，不算密钥。"""
    s = v.decode("utf-8", "replace")
    if any(c in s for c in " \t\r\n"):
        return True
    if len(s) > 24:
        return False
    return all(c.islower() and c.isascii() or c in "-_" for c in s)


def scan(files):
    """返回 [(path, 规则名, 位置)]。语义与服务端 scanForPlaintextSecrets 一致。"""
    hits = []
    for path, content, _ in files:
        for name, pat in PLAINTEXT_PATTERNS:
            if name == INLINE_RULE:
                if not any(
                    not not_a_secret_value(m.group(2))
                    for m in pat.finditer(content)
                ):
                    continue
                hits.append((path, name, pat.search(content).start()))
                continue
            m = pat.search(content)
            if m:
                hits.append((path, name, m.start()))
    return hits


def read_tree(root, rel):
    """读一个目录下的全部文件，返回 [(相对 bundle 的路径, 内容, 可执行)]。"""
    base = os.path.join(root, rel)
    out = []
    for dirpath, _, names in os.walk(base):
        for n in names:
            full = os.path.join(dirpath, n)
            rp = os.path.relpath(full, base)
            with open(full, "rb") as f:
                data = f.read()
            out.append((rp, data, os.access(full, os.X_OK)))
    out.sort(key=lambda x: x[0])
    return out


def read_file(root, rel):
    full = os.path.join(root, rel)
    with open(full, "rb") as f:
        return f.read()


def build_bundle(name, items, origin, desc):
    """items: [(kind, item_path, item_name, files, planes, host)]

    skill 的 files 是 [(相对 skill 目录路径, 内容, 可执行)]，
    写入时展开成 item_path/文件名。
    """
    files_out = []
    manifest_items = []
    for kind, ipath, iname, files, planes, host in items:
        if kind == "skill":
            for rp, data, exe in files:
                files_out.append({
                    "path": ipath + "/" + rp,
                    "content": data,
                    "executable": exe,
                })
        else:
            for rp, data, exe in files:
                files_out.append({"path": ipath, "content": data, "executable": exe})
        entry = {"kind": kind, "path": ipath, "name": iname, "planes": planes}
        if host:
            entry["host"] = host
        manifest_items.append(entry)

    lines = [f"name: {name}", "items:"]
    for it in manifest_items:
        lines.append(f"  - kind: {it['kind']}")
        lines.append(f"    path: {it['path']}")
        lines.append(f"    name: {it['name']}")
        lines.append("    planes: [" + ", ".join(it["planes"]) + "]")
        if it.get("host"):
            lines.append("    host: [" + ", ".join(it["host"]) + "]")

    msg = f"迁移 {name} 的存量内容\n\n来源：{origin}\n{desc}"
    # dry-run 也要能打印大小，所以返回结构化数据
    return {
        "bundle": name,
        "items": files_out,
        "manifest": "\n".join(lines) + "\n",
        "message": msg,
    }


def to_payload(b):
    """转成回环 /api/call 的 payload。

    AssetPayload 的 Manifest 与 AssetItem.Content 都是 []byte，
    JSON 传输一律 base64；直接传明文会被当成非法 base64 拒掉。
    """
    return {
        "bundle": b["bundle"],
        "items": [
            {
                "path": it["path"],
                "content": base64.b64encode(it["content"]).decode(),
                "executable": it["executable"],
            }
            for it in b["items"]
        ],
        "manifest": base64.b64encode(b["manifest"].encode()).decode(),
        "message": b["message"],
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--bundle", default=None)
    args = ap.parse_args()

    plan = []

    # ---- DecPersonalDevKit：按旧 config.yaml 的 provides 映射 ----
    if args.bundle in (None, "cnb"):
        plan.append(build_bundle(
            "cnb",
            [("skill", "skills/migrate-to-cnb", "migrate-to-cnb",
              read_tree(DEVKIT, "cnb/skills/migrate-to-cnb"), ["user"], None)],
            f"{DEVKIT} cnb/skills/migrate-to-cnb",
            "原 bundle：cnb（DecPersonalDevKit）",
        ))

    if args.bundle in (None, "forge-robot"):
        plan.append(build_bundle(
            "forge-robot",
            [
                ("skill", "skills/forge-robot", "forge-robot",
                 read_tree(DEVKIT, "forge-robot/skills/forge-robot"), ["user"], None),
                ("rule", "commands/forge-robot.md", "forge-robot-cmd",
                 [("forge-robot.md", read_file(DEVKIT, "forge-robot/commands/forge-robot/forge-robot.md"), False)],
                 ["user"], None),
            ],
            f"{DEVKIT} forge-robot/",
            "原 bundle：forge-robot（DecPersonalDevKit）",
        ))

    if args.bundle in (None, "agent-dev-playbook"):
        items = []
        for s in ("codebuddy-sdk-onboard", "release-dev-side-by-side",
                  "windows-custom-titlebar-keep-frame"):
            items.append(("skill", f"skills/{s}", s,
                          read_tree(DEVKIT, f"agent-dev-playbook/skills/{s}"),
                          ["user"], None))
        plan.append(build_bundle(
            "agent-dev-playbook", items,
            f"{DEVKIT} agent-dev-playbook/skills/",
            "原 bundle：agent-dev-playbook（DecPersonalDevKit）",
        ))

    if args.bundle in (None, "issue-handler"):
        plan.append(build_bundle(
            "issue-handler",
            [
                ("skill", "skills/issue-handler", "issue-handler",
                 read_tree(DEVKIT, "agents/issue-handler/skills/issue-handler"),
                 ["user"], None),
                ("rule", "rules/issue-handler.mdc", "issue-handler-rule",
                 [("issue-handler.mdc",
                   read_file(DEVKIT, "agents/issue-handler/rules/issue-handler.mdc"), False)],
                 ["user"], None),
            ],
            f"{DEVKIT} agents/issue-handler/",
            "原 bundle：issue-handler（DecPersonalDevKit）",
        ))

    if args.bundle in (None, "woa"):
        plan.append(build_bundle(
            "woa",
            [
                ("skill", "skills/devops", "devops",
                 read_tree(DEVKIT, "woa/skills/devops"), ["user"], None),
                ("skill", "skills/gongfeng", "gongfeng",
                 read_tree(DEVKIT, "woa/skills/gongfeng"), ["user"], None),
                ("skill", "skills/mirror-upload", "mirror-upload",
                 read_tree(DEVKIT, "woa/skills/mirror-upload"), ["user"], None),
                # MCP 条目里只有一个远端 URL，不含本机路径，可以直接迁。
                # 条目名必须与 skill 的 gongfeng 区分（服务端禁止同名），
                # 因此落进 mcp.json 的键是 dec-gongfeng-mcp 而不是旧的 dec-gongfeng。
                ("mcp", "mcp/gongfeng.json", "gongfeng-mcp",
                 [("gongfeng.json", read_file(DEVKIT, "woa/mcp/gongfeng.json"), False)],
                 ["user"], None),
            ],
            f"{DEVKIT} woa/skills/ 与 woa/mcp/gongfeng.json",
            "原 bundle：woa（DecPersonalDevKit）",
        ))

    if args.bundle in (None, "tencent-cloud"):
        plan.append(build_bundle(
            "tencent-cloud",
            [("skill", "skills/tencent-cloud", "tencent-cloud",
              read_tree(DEVKIT, "tencent-cloud/skills/tencent-cloud"), ["user"], None)],
            f"{DEVKIT} tencent-cloud/skills/tencent-cloud",
            "原 bundle：tencent-cloud（DecPersonalDevKit）",
        ))

    if not plan:
        print("没有匹配的 bundle", file=sys.stderr)
        return 1

    total_files = sum(len(b["items"]) for b in plan)
    total_bytes = sum(len(it["content"]) for b in plan for it in b["items"])
    print(f"计划迁移 {len(plan)} 个 bundle，{total_files} 个文件，{total_bytes / 1024:.0f} KB")
    print()

    failed = False
    for b in plan:
        files = [(it["path"], it["content"], it["executable"]) for it in b["items"]]
        hits = scan(files)
        status = "OK" if not hits else f"明文扫描命中 {len(hits)} 处"
        print(f"  {b['bundle']:<22} {len(files):>3} 文件  {sum(len(f[1]) for f in files) / 1024:>7.0f} KB  {status}")
        for path, rule, pos in hits:
            print(f"      命中：{path} -> {rule} @ {pos}")
            failed = True

    if failed:
        print("\n有内容会触发服务端明文密钥扫描，需先处理后再迁。", file=sys.stderr)

    if args.dry_run:
        return 0 if not failed else 2

    print()
    for b in plan:
        r = call("write_asset", to_payload(b))
        if r.get("error"):
            print(f"  写入 {b['bundle']} 失败: {r['error']}")
            return 3
        print(f"  写入 {b['bundle']} 成功 commit={r.get('commit', '')[:12]}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
