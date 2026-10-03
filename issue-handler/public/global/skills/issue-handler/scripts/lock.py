#!/usr/bin/env python3
"""issue-handler 工作空间防重锁。

粒度：工作空间级。锁目录 <root>/.locks/issue-handler/，
root 取 --root 参数，默认当前目录，环境变量
ISSUE_HANDLER_LOCK_DIR 可直接指定锁目录（优先级最高）。

用法：
    python3 scripts/lock.py acquire --owner <执行流名>
    python3 scripts/lock.py heartbeat
    python3 scripts/lock.py release
    python3 scripts/lock.py status

acquire：原子获取（mkdir）；锁被占退出码 1；持有者心跳超时
（默认 300s）视为陈旧，自动抢占。
heartbeat：续期当前锁的心跳时间戳。
release：释放当前锁。
status：打印锁状态，不改动。

锁目录是运行时产物，应进项目 .gitignore。
"""

import argparse
import json
import os
import shutil
import sys
import time

STALE_SECONDS = 300.0
LOCK_SUBDIR = os.path.join(".locks", "issue-handler")


def lock_dir(args) -> str:
    override = os.environ.get("ISSUE_HANDLER_LOCK_DIR")
    if override:
        return override
    return os.path.join(args.root, LOCK_SUBDIR)


def lock_file(path: str) -> str:
    return os.path.join(path, "lock.json")


def read_holder(path: str):
    f = lock_file(path)
    try:
        with open(f, encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return None


def is_stale(holder, now: float) -> bool:
    heartbeat = float(holder.get("heartbeat", 0))
    return now - heartbeat > STALE_SECONDS


def cmd_acquire(args) -> int:
    path = lock_dir(args)
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    now = time.time()
    for attempt in range(2):
        try:
            os.mkdir(path)
        except FileExistsError:
            holder = read_holder(path)
            if holder is None or is_stale(holder, now):
                # 无主或陈旧：接管（读不到 holder 视为损坏，同样可接管）
                shutil.rmtree(path, ignore_errors=True)
                continue
            print(
                f"locked by owner={holder.get('owner')} pid={holder.get('pid')} "
                f"heartbeat={int(now - float(holder.get('heartbeat', 0)))}s ago",
                file=sys.stderr,
            )
            return 1
        payload = {
            "owner": args.owner,
            "pid": os.getpid(),
            "heartbeat": now,
            "acquired_at": now,
        }
        with open(lock_file(path), "w", encoding="utf-8") as handle:
            json.dump(payload, handle)
        print(f"acquired owner={args.owner}")
        return 0
    print("acquire failed: takeover retry exhausted", file=sys.stderr)
    return 1


def cmd_heartbeat(args) -> int:
    path = lock_dir(args)
    holder = read_holder(path)
    if holder is None:
        print("no lock held", file=sys.stderr)
        return 1
    now = time.time()
    holder["heartbeat"] = now
    with open(lock_file(path), "w", encoding="utf-8") as handle:
        json.dump(holder, handle)
    print(f"heartbeat ok owner={holder.get('owner')}")
    return 0


def cmd_release(args) -> int:
    path = lock_dir(args)
    if not os.path.isdir(path):
        print("no lock held")
        return 0
    holder = read_holder(path)
    if holder is not None and args.owner and holder.get("owner") != args.owner:
        print(
            f"refuse release: held by owner={holder.get('owner')}",
            file=sys.stderr,
        )
        return 1
    shutil.rmtree(path)
    print("released")
    return 0


def cmd_status(args) -> int:
    path = lock_dir(args)
    if not os.path.isdir(path):
        print("unlocked")
        return 0
    holder = read_holder(path)
    if holder is None:
        print("locked (holder unreadable)")
        return 0
    now = time.time()
    age = int(now - float(holder.get("heartbeat", 0)))
    state = "stale" if is_stale(holder, now) else "held"
    print(
        f"{state} owner={holder.get('owner')} pid={holder.get('pid')} "
        f"heartbeat={age}s ago"
    )
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=os.getcwd(),
                        help="工作空间根目录（默认当前目录）")
    sub = parser.add_subparsers(dest="cmd", required=True)
    p_acquire = sub.add_parser("acquire")
    p_acquire.add_argument("--owner", required=True, help="执行流名")
    sub.add_parser("heartbeat")
    p_release = sub.add_parser("release")
    p_release.add_argument("--owner", default=None,
                           help="校验持有者，不匹配则拒绝释放")
    sub.add_parser("status")
    args = parser.parse_args()
    return {
        "acquire": cmd_acquire,
        "heartbeat": cmd_heartbeat,
        "release": cmd_release,
        "status": cmd_status,
    }[args.cmd](args)


if __name__ == "__main__":
    sys.exit(main())
