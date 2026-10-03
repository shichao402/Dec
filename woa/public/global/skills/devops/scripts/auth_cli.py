#!/usr/bin/env python3
"""Manage the unified devops skill's local credentials."""

from __future__ import annotations

import argparse
import getpass

from auth import (
    CONFIG_PATH,
    resolve_access_token,
    resolve_user_id,
    save_config,
)


def mask(value: str | None) -> str:
    return "已配置" if value else "未配置"


def cmd_status(_: argparse.Namespace) -> None:
    token, token_source = resolve_access_token()
    user_id, user_source = resolve_user_id()
    print(f"config: {CONFIG_PATH}")
    print(f"access_token: {mask(token)} ({token_source})")
    print(f"user_id: {mask(user_id)} ({user_source})")


def cmd_login(args: argparse.Namespace) -> None:
    token = args.access_token or getpass.getpass("蓝盾 access_token: ").strip()
    if not token:
        raise SystemExit("错误：access_token 不能为空。")
    updates = {"access_token": token}
    if args.user_id:
        updates["user_id"] = args.user_id.strip()
    save_config(updates)
    print(f"已保存到 {CONFIG_PATH}")


def cmd_logout(args: argparse.Namespace) -> None:
    keys = ("access_token", "user_id") if args.all else ("access_token",)
    save_config({}, remove=keys)
    print("已清除本地凭证。" if args.all else "已清除本地 access_token。")


def main() -> None:
    parser = argparse.ArgumentParser(description="管理统一蓝盾 Skill 的本地鉴权配置")
    subparsers = parser.add_subparsers(dest="command", required=True)

    status_parser = subparsers.add_parser("status", help="脱敏显示当前凭证来源")
    status_parser.set_defaults(func=cmd_status)

    login_parser = subparsers.add_parser("login", help="写入 devops/config.json")
    login_parser.add_argument(
        "--access-token",
        help="本轮对话中用户提供的 token；省略时安全交互输入",
    )
    login_parser.add_argument("--user-id", help="权限治理操作用户 ID")
    login_parser.set_defaults(func=cmd_login)

    logout_parser = subparsers.add_parser("logout", help="清除本地凭证")
    logout_parser.add_argument("--all", action="store_true", help="同时清除 user_id")
    logout_parser.set_defaults(func=cmd_logout)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
