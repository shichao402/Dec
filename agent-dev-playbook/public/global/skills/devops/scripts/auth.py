#!/usr/bin/env python3
"""Unified credential loading for the devops skill."""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path
from typing import Any, Dict, Optional, Tuple


CONFIG_PATH = Path(__file__).resolve().parent.parent / "config.json"
TOKEN_ENV = "BK_CI_ACCESS_TOKEN"
USER_ID_ENV = "BK_CI_USER_ID"


def load_config() -> Dict[str, Any]:
    """Read the skill-local config without exposing credential values."""
    if not CONFIG_PATH.is_file():
        return {}
    try:
        data = json.loads(CONFIG_PATH.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"错误：无法读取 {CONFIG_PATH}: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc
    if not isinstance(data, dict):
        print(f"错误：{CONFIG_PATH} 顶层必须是 JSON 对象。", file=sys.stderr)
        raise SystemExit(1)
    return data


def save_config(updates: Dict[str, Any], remove: tuple[str, ...] = ()) -> None:
    """Merge credential updates into config.json and restrict its permissions."""
    data = load_config()
    data.update(updates)
    for key in remove:
        data.pop(key, None)
    CONFIG_PATH.write_text(
        json.dumps(data, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    try:
        CONFIG_PATH.chmod(0o600)
    except OSError:
        pass


def resolve_access_token(args_token: Optional[str] = None) -> Tuple[Optional[str], str]:
    """Return (token, source) using conversation, config, then environment."""
    if args_token:
        return args_token.strip(), "command-line"
    token = load_config().get("access_token")
    if isinstance(token, str) and token.strip():
        return token.strip(), "config.json"
    token = os.environ.get(TOKEN_ENV, "").strip()
    if token:
        return token, TOKEN_ENV
    return None, "missing"


def get_access_token(args_token: Optional[str] = None) -> str:
    """Return an access token or exit with the unified setup guidance."""
    token, _ = resolve_access_token(args_token)
    if token:
        return token
    print(
        "错误：未找到蓝盾访问令牌。\n"
        "请先直接访问 https://devops.woa.com/ms/auth/api/user/bkToken/get 获取 token。\n"
        "仅当该链接返回 401、无权限或提示未登录时，先访问 "
        "https://devops.woa.com/console/ 完成登录，再回到 token 链接重试。\n"
        "获取后选择一种方式：\n"
        "  1) 把 token 告诉 AI，由 AI 配置\n"
        f"  2) 自行在 {CONFIG_PATH} 中设置 access_token\n"
        "详情：references/auth-setup.md",
        file=sys.stderr,
    )
    raise SystemExit(1)


def resolve_user_id(args_user_id: Optional[str] = None) -> Tuple[Optional[str], str]:
    """Return (operator user ID, source) using conversation, config, then env."""
    if args_user_id:
        return args_user_id.strip(), "command-line"
    user_id = load_config().get("user_id")
    if isinstance(user_id, str) and user_id.strip():
        return user_id.strip(), "config.json"
    user_id = os.environ.get(USER_ID_ENV, "").strip()
    if user_id:
        return user_id, USER_ID_ENV
    return None, "missing"


def get_user_id(args_user_id: Optional[str] = None) -> str:
    """Return the permission-governance operator ID or exit."""
    user_id, _ = resolve_user_id(args_user_id)
    if user_id:
        return user_id
    print(
        "错误：权限治理需要操作用户 ID。请在对话中告诉 AI，"
        "或在 config.json 中设置 user_id。",
        file=sys.stderr,
    )
    raise SystemExit(1)
