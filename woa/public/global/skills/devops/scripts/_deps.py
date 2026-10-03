#!/usr/bin/env python3
"""Dependency bootstrap: auto-install PyYAML into the skill-private lib/ dir.

Design goals (per user requirements):
1. Zero manual steps - scripts run directly even when PyYAML is missing.
2. No global pollution - packages land in <skill>/lib/, never site-packages,
   never --user, no PATH changes, nothing written outside this skill dir.

Usage from sibling scripts:

    import _deps  # noqa: F401  (must precede "import yaml")
    import yaml
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

SKILL_DIR = Path(__file__).resolve().parent.parent
LIB_DIR = SKILL_DIR / "lib"

# Honour an externally provided PyYAML first (dev / CI machines).
if "PYAML_OVERRIDE_PATH" in os.environ:
    sys.path.insert(0, os.environ["PYAML_OVERRIDE_PATH"])


def _ensure_pyyaml() -> None:
    try:
        import yaml  # noqa: F401
        return
    except ImportError:
        pass

    # Not installed globally -> bootstrap into skill-private lib/.
    LIB_DIR.mkdir(parents=True, exist_ok=True)
    if str(LIB_DIR) not in sys.path:
        # Prepend so the private copy wins over any partial global install.
        sys.path.insert(0, str(LIB_DIR))

    try:
        import yaml  # noqa: F401  (retry after path injection)
        return
    except ImportError:
        pass

    print(
        "[deps] PyYAML 未安装，正在自动安装到 skill 私有目录（不影响全局环境）…",
        file=sys.stderr,
    )
    # --no-cache-dir avoids polluting the user-level pip cache wholesale;
    # --target puts files only into LIB_DIR. --upgrade keeps it fresh when
    # the private copy is corrupted.
    cmd = [
        sys.executable,
        "-m",
        "pip",
        "install",
        "--no-cache-dir",
        "--target",
        str(LIB_DIR),
        "PyYAML",
    ]
    result = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        timeout=300,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if result.returncode != 0:
        raise SystemExit(
            f"\n[deps] 自动安装 PyYAML 失败（exit {result.returncode}）。\n"
            f"pip 输出：\n{result.stdout[-2000:]}\n"
            "可手动执行："
            f"{sys.executable} -m pip install --target \"{LIB_DIR}\" PyYAML\n"
        )
    print(f"[deps] PyYAML 已安装到 {LIB_DIR}", file=sys.stderr)

    try:
        import yaml  # noqa: F401
    except ImportError as exc:
        raise SystemExit(
            f"\n[deps] 安装后仍无法导入 yaml，私有目录：{LIB_DIR}\n"
            f"原始错误：{exc}\n"
        ) from exc


_ensure_pyyaml()
