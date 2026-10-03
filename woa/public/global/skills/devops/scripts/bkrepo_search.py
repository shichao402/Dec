#!/usr/bin/env python3
"""
蓝盾制品库（bk-repo）制品搜索脚本
按项目、仓库、文件名、路径、元数据等条件搜索制品
"""

import argparse
import json
import sys
import urllib.request
import urllib.error
import urllib.parse

from auth import get_access_token


BKREPO_BASE_URL = "https://bkrepo.apigw.o.woa.com/prod"


def format_size(size_bytes: int) -> str:
    """格式化文件大小"""
    for unit in ["B", "KB", "MB", "GB"]:
        if size_bytes < 1024:
            return f"{size_bytes:.2f} {unit}"
        size_bytes /= 1024
    return f"{size_bytes:.2f} TB"


def search_artifacts(
    project_id: str,
    repo_name: str,
    name: str = None,
    path_prefix: str = None,
    metadata: list = None,
    page: int = 1,
    page_size: int = 20,
) -> dict:
    """搜索制品库中的制品"""
    url = f"{BKREPO_BASE_URL}/repository/api/node/search"

    # 构建查询规则
    rules = [
        {"field": "projectId", "value": project_id, "operation": "EQ"},
        {"field": "repoName", "value": repo_name, "operation": "EQ"},
        {"field": "folder", "value": False, "operation": "EQ"},
    ]

    if name:
        if "*" in name:
            rules.append({"field": "name", "value": name, "operation": "MATCH"})
        else:
            rules.append({"field": "name", "value": name, "operation": "EQ"})

    if path_prefix:
        rules.append({"field": "fullPath", "value": path_prefix, "operation": "PREFIX"})

    if metadata:
        for m in metadata:
            if "=" in m:
                key, value = m.split("=", 1)
                rules.append({"field": f"metadata.{key}", "value": value, "operation": "EQ"})

    body = {
        "select": ["projectId", "repoName", "fullPath", "name", "size", "sha256", "lastModifiedDate", "metadata"],
        "page": {"pageNumber": page, "pageSize": page_size},
        "sort": {"properties": ["lastModifiedDate"], "direction": "DESC"},
        "rule": {"relation": "AND", "rules": rules},
    }

    data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Bkapi-Authorization", f'{{"access_token":"{get_access_token()}"}}')

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

    if result.get("code") != 0:
        print(f"API 错误 [{result.get('code')}]: {result.get('message')}", file=sys.stderr)
        sys.exit(1)

    return result.get("data", {})


def main():
    parser = argparse.ArgumentParser(
        description="搜索蓝盾制品库（bk-repo）中的制品",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  # 按文件名搜索（支持通配符）
  python bkrepo_search.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --name "app-*.tgz"

  # 按路径前缀搜索
  python bkrepo_search.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --path-prefix /release/

  # 按元数据搜索
  python bkrepo_search.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --metadata version=1.0.0
        """,
    )
    parser.add_argument("--project-id", required=True, help="项目 ID")
    parser.add_argument("--repo-name", required=True, help="仓库名称")
    parser.add_argument("--name", help="文件名，支持通配符 *（如 *.tgz、app-*）")
    parser.add_argument("--path-prefix", help="路径前缀（如 /release/）")
    parser.add_argument("--metadata", action="append", help="元数据过滤，格式 key=value，可多次指定")
    parser.add_argument("--page", type=int, default=1, help="页码，从 1 开始（默认 1）")
    parser.add_argument("--page-size", type=int, default=20, help="每页条数（默认 20）")

    args = parser.parse_args()

    data = search_artifacts(
        project_id=args.project_id,
        repo_name=args.repo_name,
        name=args.name,
        path_prefix=args.path_prefix,
        metadata=args.metadata,
        page=args.page,
        page_size=args.page_size,
    )

    records = data.get("records", [])
    total = data.get("totalRecords", len(records))
    total_pages = data.get("totalPages", 1)
    page_num = data.get("pageNumber", args.page)
    page_size = data.get("pageSize", args.page_size)

    lines = []
    lines.append(f"共 {total} 个制品，第 {page_num}/{total_pages} 页（每页 {page_size} 条）\n")
    lines.append("| # | 文件名 | 路径 | 大小 | 修改时间 | SHA256 |")
    lines.append("|---|--------|------|------|----------|--------|")
    for i, r in enumerate(records, 1):
        name = r.get("name", "")
        full_path = r.get("fullPath", "")
        size_human = format_size(r.get("size", 0))
        modified = r.get("lastModifiedDate", "")[:10] if r.get("lastModifiedDate") else ""
        sha256 = (r.get("sha256") or "")[:8] + "..." if r.get("sha256") else ""
        lines.append(f"| {i} | {name} | {full_path} | {size_human} | {modified} | {sha256} |")

    if records:
        lines.append("")
        lines.append("**详细信息：**")
        for i, r in enumerate(records, 1):
            lines.append(f"\n**{i}. {r.get('name')}**")
            lines.append(f"- fullPath: `{r.get('fullPath')}`")
            lines.append(f"- projectId: `{r.get('projectId')}`")
            lines.append(f"- repoName: `{r.get('repoName')}`")
            lines.append(f"- size: {format_size(r.get('size', 0))} ({r.get('size', 0)} bytes)")
            lines.append(f"- sha256: `{r.get('sha256', '')}`")
            lines.append(f"- lastModifiedDate: {r.get('lastModifiedDate', '')}")
            metadata = r.get("metadata")
            if metadata:
                lines.append("- metadata:")
                if isinstance(metadata, dict):
                    for k, v in metadata.items():
                        lines.append(f"  - {k}: {v}")
                elif isinstance(metadata, list):
                    for m in metadata:
                        lines.append(f"  - {m.get('key', '')}: {m.get('value', '')}")

    print("\n".join(lines))


if __name__ == "__main__":
    main()
