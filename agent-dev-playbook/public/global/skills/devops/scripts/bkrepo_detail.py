#!/usr/bin/env python3
"""
蓝盾制品库（bk-repo）文件详情查看脚本
查看 Generic 仓库中指定文件的详细信息
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


def format_datetime(dt_str: str) -> str:
    """格式化日期时间字符串，截取到秒"""
    if not dt_str:
        return "-"
    # 格式如 2024-04-25T21:11:14.975，截取到秒
    if "T" in dt_str:
        date_part, time_part = dt_str.split("T", 1)
        time_part = time_part.split(".")[0]  # 去掉毫秒
        return f"{date_part} {time_part}"
    return dt_str


def get_node_detail(project_id: str, repo_name: str, path: str) -> dict:
    """获取文件节点详情"""
    encoded_path = urllib.parse.quote(path, safe="/")
    url = f"{BKREPO_BASE_URL}/repository/api/node/detail/{project_id}/{repo_name}{encoded_path}"

    req = urllib.request.Request(url, method="GET")
    req.add_header("X-Bkapi-Authorization", f'{{"access_token":"{get_access_token()}"}}')

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        body_text = e.read().decode("utf-8", errors="replace")
        if e.code == 404:
            print(f"文件不存在: {path}", file=sys.stderr)
        else:
            print(f"获取文件详情失败，HTTP 错误 {e.code}: {body_text}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"请求失败: {e.reason}", file=sys.stderr)
        sys.exit(1)

    if result.get("code") != 0:
        print(f"API 错误 [{result.get('code')}]: {result.get('message')}", file=sys.stderr)
        sys.exit(1)

    data = result.get("data")
    if not data:
        print(f"文件不存在: {path}", file=sys.stderr)
        sys.exit(1)

    return data


def print_detail(data: dict, output_format: str = "yaml") -> None:
    """输出文件详情"""
    if output_format == "json":
        print(json.dumps(data, indent=2, ensure_ascii=False))
        return

    # 默认 YAML 格式输出
    name = data.get("name", "-")
    full_path = data.get("fullPath", "-")
    project_id = data.get("projectId", "-")
    repo_name = data.get("repoName", "-")
    folder = data.get("folder", False)
    size = data.get("size", 0)
    sha256 = data.get("sha256", "-")
    md5 = data.get("md5", "-")
    created_by = data.get("createdBy", "-")
    created_date = format_datetime(data.get("createdDate", ""))
    last_modified_by = data.get("lastModifiedBy", "-")
    last_modified_date = format_datetime(data.get("lastModifiedDate", ""))
    last_access_date = format_datetime(data.get("lastAccessDate", ""))
    metadata = data.get("metadata", {})
    node_metadata = data.get("nodeMetadata", [])

    print(f"name: {name}")
    print(f"fullPath: {full_path}")
    print(f"projectId: {project_id}")
    print(f"repoName: {repo_name}")
    print(f"type: {'目录' if folder else '文件'}")
    print(f"size: {size} ({format_size(size)})")
    print(f"sha256: {sha256}")
    print(f"md5: {md5}")
    print(f"createdBy: {created_by}")
    print(f"createdDate: {created_date}")
    print(f"lastModifiedBy: {last_modified_by}")
    print(f"lastModifiedDate: {last_modified_date}")
    print(f"lastAccessDate: {last_access_date}")

    # 输出元数据
    if metadata:
        print("metadata:")
        for key, value in metadata.items():
            print(f"  {key}: {value}")
    else:
        print("metadata: {}")

    # 输出节点元数据
    if node_metadata:
        print("nodeMetadata:")
        for item in node_metadata:
            key = item.get("key", "")
            value = item.get("value", "")
            system = item.get("system", False)
            print(f"  - key: {key}")
            print(f"    value: {value}")
            print(f"    system: {system}")


def main():
    parser = argparse.ArgumentParser(
        description="查看蓝盾制品库（bk-repo）文件详情",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  # 查看文件详情
  python bkrepo_detail.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --path /release/app-1.0.0.tgz

  # 以 JSON 格式输出
  python bkrepo_detail.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --path /release/app-1.0.0.tgz \\
    --format json
        """,
    )
    parser.add_argument("--project-id", required=True, help="项目 ID")
    parser.add_argument("--repo-name", required=True, help="仓库名称（如 generic-local）")
    parser.add_argument("--path", required=True, help="文件在仓库中的完整路径（如 /release/app-1.0.0.tgz）")
    parser.add_argument(
        "--format",
        choices=["yaml", "json"],
        default="yaml",
        help="输出格式：yaml（默认）或 json",
    )

    args = parser.parse_args()

    # 确保路径以 / 开头
    target_path = args.path
    if not target_path.startswith("/"):
        target_path = "/" + target_path

    # 获取文件详情
    print(f"正在获取文件详情: {target_path}", file=sys.stderr)
    data = get_node_detail(args.project_id, args.repo_name, target_path)
    print("获取成功", file=sys.stderr)

    # 输出详情
    print_detail(data, args.format)


if __name__ == "__main__":
    main()
