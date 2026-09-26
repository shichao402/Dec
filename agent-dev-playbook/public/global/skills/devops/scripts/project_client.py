#!/usr/bin/env python3
"""蓝盾项目查询与管理客户端。"""

from __future__ import annotations

import argparse
import json
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any, Dict, Optional

import yaml

from auth import get_access_token


BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects"
LIST_SUMMARY_FIELDS = (
    "projectId",
    "projectCode",
    "englishName",
    "projectName",
    "enabled",
    "offlined",
    "managePermission",
    "canView",
)


def print_data(data: Any) -> None:
    print(yaml.dump(data, allow_unicode=True, sort_keys=False))


def positive_int(value: str) -> int:
    number = int(value)
    if number < 1:
        raise argparse.ArgumentTypeError("必须是大于 0 的整数。")
    return number


def page_size(value: str) -> int:
    size = positive_int(value)
    if not 1 <= size <= 100:
        raise argparse.ArgumentTypeError("每页数量必须在 1 到 100 之间。")
    return size


def summarize_project_list(
    response: Dict[str, Any],
    page: int,
    size: int,
) -> Dict[str, Any]:
    """精简项目列表，并明确当前输出只代表一个分页响应。"""
    data = response.get("data")
    if not isinstance(data, list):
        return response

    projects = []
    for item in data:
        if not isinstance(item, dict):
            projects.append(item)
            continue
        projects.append(
            {
                field: item[field]
                for field in LIST_SUMMARY_FIELDS
                if field in item
            }
        )

    result = {
        key: response[key]
        for key in ("code", "message", "request_id", "result")
        if key in response
    }
    result["pagination"] = {
        "scope": "single-page",
        "page": page,
        "pageSize": size,
        "returnedCount": len(data),
        "nextPageRequired": len(data) >= size,
    }
    result["data"] = projects
    return result


def read_body(body: Optional[str], body_file: Optional[str]) -> Dict[str, Any]:
    """读取内联 JSON 或 JSON 文件，并确保顶层为对象。"""
    try:
        raw = Path(body_file).read_text(encoding="utf-8") if body_file else body
        data = json.loads(raw or "")
    except OSError as exc:
        raise SystemExit(f"读取请求体失败: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise SystemExit(f"请求体不是合法 JSON: {exc}") from exc
    if not isinstance(data, dict):
        raise SystemExit("请求体顶层必须是 JSON 对象。")
    return data


def make_request(
    url: str,
    access_token: str,
    method: str = "GET",
    data: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    headers = {
        "Accept": "application/json",
        "Content-Type": "application/json",
        "X-Bkapi-Authorization": json.dumps(
            {"access_token": access_token}, separators=(",", ":")
        ),
    }
    payload = json.dumps(data, ensure_ascii=False).encode("utf-8") if data is not None else None
    request = urllib.request.Request(
        url,
        data=payload,
        headers=headers,
        method=method,
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        print(f"HTTP Error {exc.code}: {detail}", file=sys.stderr)
        raise SystemExit(1) from exc
    except urllib.error.URLError as exc:
        print(f"URL Error: {exc.reason}", file=sys.stderr)
        raise SystemExit(1) from exc
    except json.JSONDecodeError as exc:
        print(f"JSON Decode Error: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc


def cmd_list(args: argparse.Namespace) -> None:
    token = get_access_token(args.access_token)
    query = {
        key: value
        for key, value in {
            "productIds": args.product_ids,
            "channelCodes": args.channel_codes,
            "sort": args.sort,
            "page": args.page,
            "pageSize": args.page_size,
        }.items()
        if value is not None
    }
    url = f"{BASE_URL}/project_list"
    if query:
        url = f"{url}?{urllib.parse.urlencode(query)}"
    response = make_request(url, token)
    if args.full_response:
        print_data(response)
    else:
        print_data(summarize_project_list(response, args.page, args.page_size))
    data = response.get("data")
    if isinstance(data, list) and len(data) >= args.page_size:
        print(
            f"提示：以上仅为第 {args.page} 页，不能据此宣称已返回全部项目；"
            f"请继续查询第 {args.page + 1} 页。",
            file=sys.stderr,
        )


def cmd_get(args: argparse.Namespace) -> None:
    token = get_access_token(args.access_token)
    project_id = urllib.parse.quote(args.project_id, safe="")
    print_data(make_request(f"{BASE_URL}/{project_id}", token))


def cmd_create(args: argparse.Namespace) -> None:
    token = get_access_token(args.access_token)
    body = read_body(args.body, args.body_file)
    print_data(make_request(f"{BASE_URL}/project_create", token, method="POST", data=body))


def cmd_edit(args: argparse.Namespace) -> None:
    token = get_access_token(args.access_token)
    project_id = urllib.parse.quote(args.project_id, safe="")
    body = read_body(args.body, args.body_file)
    print_data(make_request(f"{BASE_URL}/{project_id}", token, method="PUT", data=body))


def add_body_args(parser: argparse.ArgumentParser) -> None:
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--body", help="项目信息 JSON 字符串")
    group.add_argument("--body-file", help="项目信息 JSON 文件路径（推荐）")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="查询、创建和编辑蓝盾项目",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python project_client.py list
  python project_client.py get --project-id myproject
  python project_client.py create --body-file ./project-create.json
  python project_client.py edit --project-id myproject --body-file ./project-edit.json

创建和编辑属于写操作。AI 必须先展示完整请求体并获得用户明确确认。
        """,
    )
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--access-token", help="可选；本轮对话提供的访问令牌")
    subparsers = parser.add_subparsers(dest="command", required=True)

    list_parser = subparsers.add_parser("list", parents=[common], help="查询当前用户可见项目")
    list_parser.add_argument("--product-ids", help="运营产品 ID，多个值以逗号分隔")
    list_parser.add_argument("--channel-codes", help="渠道号，多个值以逗号分隔")
    list_parser.add_argument(
        "--sort",
        choices=("PROJECT_NAME", "ENGLISH_NAME"),
        help="排序字段",
    )
    list_parser.add_argument("--page", type=positive_int, default=1, help="页码，默认 1")
    list_parser.add_argument(
        "--page-size",
        type=page_size,
        default=20,
        help="每页数量，默认 20，最大 100",
    )
    list_parser.add_argument(
        "--full-response",
        action="store_true",
        help="输出当前页完整 ProjectVO；默认只输出常用字段",
    )
    list_parser.set_defaults(func=cmd_list)

    get_parser = subparsers.add_parser("get", parents=[common], help="获取项目详情")
    get_parser.add_argument("--project-id", required=True, help="项目英文名")
    get_parser.set_defaults(func=cmd_get)

    create_parser = subparsers.add_parser("create", parents=[common], help="创建项目")
    add_body_args(create_parser)
    create_parser.set_defaults(func=cmd_create)

    edit_parser = subparsers.add_parser("edit", parents=[common], help="编辑项目")
    edit_parser.add_argument("--project-id", required=True, help="项目英文名")
    add_body_args(edit_parser)
    edit_parser.set_defaults(func=cmd_edit)
    return parser


def main() -> None:
    args = build_parser().parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
