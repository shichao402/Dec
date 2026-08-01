#!/usr/bin/env python3
"""
Dec 标准测试入口。

设计目标：
1. 以 Python 作为跨平台核心入口
2. Shell / Batch 仅负责代理调用
3. 优先执行稳定、可重复的测试：
   - go test ./...
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


@dataclass
class StepResult:
    name: str
    returncode: int


class RunTestsError(RuntimeError):
    pass


def print_header(title: str) -> None:
    print("\n" + "=" * 42)
    print(title)
    print("=" * 42)


def print_step(title: str) -> None:
    print(f"\n>>> {title}")


def run_command(command: list[str], cwd: Path, env: dict[str, str]) -> None:
    process = subprocess.run(command, cwd=str(cwd), env=env)
    if process.returncode != 0:
        raise RunTestsError(
            f"命令执行失败: {' '.join(command)} (exit code: {process.returncode})"
        )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Dec 标准测试入口")
    parser.add_argument(
        "--skip-go-test",
        action="store_true",
        help="跳过 go test ./...",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    project_root = Path(__file__).resolve().parent.parent

    if not (project_root / "go.mod").exists():
        raise RunTestsError(f"未找到 go.mod，项目根目录无效: {project_root}")

    env = os.environ.copy()

    print_header("Dec 标准测试")
    print(f"项目目录: {project_root}")

    results: list[StepResult] = []

    if not args.skip_go_test:
        print_step("Go 测试")
        run_command(["go", "test", "./..."], project_root, env)
        results.append(StepResult("go test ./...", 0))

    print_header("测试结果统计")
    for result in results:
        print(f"通过: {result.name}")
    print("\n[OK] 所有测试通过！")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RunTestsError as exc:
        print(f"\n[FAIL] {exc}", file=sys.stderr)
        raise SystemExit(1)
