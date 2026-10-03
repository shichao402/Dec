#!/usr/bin/env python3
"""
蓝盾流水线制品查询脚本
按流水线构建 ID 查询制品列表
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.request
import urllib.error
import urllib.parse

from auth import get_access_token


BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user"


def get_auth_header(access_token: str | None = None) -> str:
    token = get_access_token(access_token)
    auth = {"access_token": token}
    username = os.environ.get("BK_CI_USERNAME")
    if username:
        auth["bk_username"] = username

    return json.dumps(auth, ensure_ascii=False)


def format_size(size_bytes: int) -> str:
    for unit in ["B", "KB", "MB", "GB"]:
        if size_bytes < 1024:
            return f"{size_bytes:.2f} {unit}"
        size_bytes /= 1024
    return f"{size_bytes:.2f} TB"


def search_artifacts(
    project_id: str,
    pipeline_id: str,
    build_id: str,
    page: int = 1,
    page_size: int = 20,
    access_token: str | None = None,
) -> dict:
    params = urllib.parse.urlencode({
        "buildId": build_id,
        "pipelineId": pipeline_id,
        "page": page,
        "pageSize": page_size,
    })
    url = f"{BASE_URL}/projects/{project_id}/artifactories/file_info?{params}"

    req = urllib.request.Request(url)
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Bkapi-Authorization", get_auth_header(access_token))

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        body_text = e.read().decode("utf-8", errors="replace")
        print(f"HTTP 错误 {e.code}: {body_text}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"请求失败: {e.reason}", file=sys.stderr)
        sys.exit(1)

    if result.get("status") != 0:
        print(f"API 错误 [{result.get('status')}]: {result.get('message')}", file=sys.stderr)
        sys.exit(1)

    return result["data"]


def main():
    parser = argparse.ArgumentParser(
        description="按流水线构建查询蓝盾制品列表",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python search_build_artifacts.py \\
    --project-id myproject \\
    --pipeline-id p-abc123 \\
    --build-id b-xyz789
        """,
    )
    parser.add_argument("--project-id", required=True, help="项目 ID")
    parser.add_argument("--pipeline-id", required=True, help="流水线 ID（p- 开头）")
    parser.add_argument("--build-id", required=True, help="构建 ID（b- 开头）")
    parser.add_argument("--page", type=int, default=1, help="页码，从 1 开始（默认 1）")
    parser.add_argument("--page-size", type=int, default=20, help="每页条数（默认 20）")
    parser.add_argument("--access-token", help="可选；本轮对话提供的访问令牌")

    args = parser.parse_args()

    data = search_artifacts(
        project_id=args.project_id,
        pipeline_id=args.pipeline_id,
        build_id=args.build_id,
        page=args.page,
        page_size=args.page_size,
        access_token=args.access_token,
    )

    print(data)

    records = []
    for r in data.get("records", []):
        record = {
            "name": r.get("name"),
            "fullPath": r.get("fullPath"),
            "artifactoryType": r.get("artifactoryType"),
            "size": r.get("size"),
            "sizeHuman": format_size(r.get("size", 0)),
            "md5": r.get("md5"),
            "modifiedTime": r.get("modifiedTime"),
            "folder": r.get("folder"),
        }
        if r.get("downloadUrl"):
            record["downloadUrl"] = r["downloadUrl"]
        if r.get("shortUrl"):
            record["shortUrl"] = r["shortUrl"]
        props = r.get("properties")
        if props:
            record["properties"] = props
        records.append(record)

    total_pages = data.get("totalPages", 0)
    page_num = data.get("page", args.page)
    page_size = data.get("pageSize", args.page_size)
    count = data.get("count", len(records))

    lines = []
    lines.append(f"共 {count} 个制品，第 {page_num}/{total_pages} 页（每页 {page_size} 条）\n")
    lines.append("| # | 文件名 | 大小 | 类型 | MD5 |")
    lines.append("|---|--------|------|------|-----|")
    for i, r in enumerate(records, 1):
        name = r.get("name", "")
        size_human = r.get("sizeHuman", "")
        atype = r.get("artifactoryType", "")
        md5 = r.get("md5", "")
        lines.append(f"| {i} | {name} | {size_human} | {atype} | {md5} |")

    if records:
        lines.append("")
        lines.append("**详细信息：**")
        for i, r in enumerate(records, 1):
            lines.append(f"\n**{i}. {r.get('name')}**")
            lines.append(f"- fullPath: `{r.get('fullPath')}`")
            lines.append(f"- artifactoryType: `{r.get('artifactoryType')}`")
            lines.append(f"- size: {r.get('sizeHuman')} ({r.get('size')} bytes)")
            lines.append(f"- md5: `{r.get('md5')}`")
            lines.append(f"- modifiedTime: {r.get('modifiedTime')}")
            if r.get("downloadUrl"):
                lines.append(f"- downloadUrl: {r['downloadUrl']}")
            if r.get("shortUrl"):
                lines.append(f"- shortUrl: {r['shortUrl']}")
            props = r.get("properties")
            if props:
                lines.append("- properties:")
                for p in props:
                    lines.append(f"  - {p.get('key')}: {p.get('value')}")

    print("\n".join(lines))


if __name__ == "__main__":
    main()
