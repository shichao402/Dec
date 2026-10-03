#!/usr/bin/env python3
"""
蓝盾制品库（bk-repo）文件上传脚本
通过临时 token 上传文件到 Generic 仓库，支持大文件上传
"""

import argparse
import json
import os
import sys
import urllib.request
import urllib.error
import urllib.parse


BKREPO_BASE_URL = "https://bkrepo.apigw.o.woa.com/prod"
BKREPO_DOMAIN = "https://bkrepo.woa.com"


from auth import get_access_token


def format_size(size_bytes: int) -> str:
    """格式化文件大小"""
    for unit in ["B", "KB", "MB", "GB"]:
        if size_bytes < 1024:
            return f"{size_bytes:.2f} {unit}"
        size_bytes /= 1024
    return f"{size_bytes:.2f} TB"


def create_temporary_token(project_id: str, repo_name: str, path: str) -> str:
    """创建临时上传 token"""
    url = f"{BKREPO_BASE_URL}/generic/temporary/token/create"

    body = json.dumps({
        "projectId": project_id,
        "repoName": repo_name,
        "fullPathSet": [path],
        "type": "UPLOAD",
        "expireSeconds": 3600,
        "permits": 1,
    }).encode("utf-8")

    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("X-Bkapi-Authorization", f'{{"access_token":"{get_access_token()}"}}')
    req.add_header("Content-Type", "application/json")

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        body_text = e.read().decode("utf-8", errors="replace")
        print(f"创建临时 token 失败，HTTP 错误 {e.code}: {body_text}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"创建临时 token 请求失败: {e.reason}", file=sys.stderr)
        sys.exit(1)

    if result.get("code") != 0:
        print(f"创建临时 token API 错误 [{result.get('code')}]: {result.get('message')}", file=sys.stderr)
        sys.exit(1)

    data = result.get("data", [])
    if not data:
        print("创建临时 token 失败: 返回数据为空", file=sys.stderr)
        sys.exit(1)

    token = data[0].get("token")
    if not token:
        print("创建临时 token 失败: token 为空", file=sys.stderr)
        sys.exit(1)

    return token


def upload_file(project_id: str, repo_name: str, path: str, local_file: str, overwrite: bool = False) -> None:
    """通过临时 token 上传文件到制品库"""
    # 检查本地文件是否存在
    if not os.path.exists(local_file):
        print(f"错误: 本地文件不存在: {local_file}", file=sys.stderr)
        sys.exit(1)

    if not os.path.isfile(local_file):
        print(f"错误: 路径不是文件: {local_file}", file=sys.stderr)
        sys.exit(1)

    file_size = os.path.getsize(local_file)
    print(f"准备上传: {local_file} ({format_size(file_size)})", file=sys.stderr)

    # 创建临时上传 token
    print("正在创建临时上传 token...", file=sys.stderr)
    token = create_temporary_token(project_id, repo_name, path)
    print("临时 token 创建成功", file=sys.stderr)

    # 使用制品库自身域名拼接临时上传链接
    encoded_path = urllib.parse.quote(path, safe="/")
    url = f"{BKREPO_DOMAIN}/generic/temporary/upload/{project_id}/{repo_name}{encoded_path}?token={token}"

    # 读取文件内容
    print("正在上传文件...", file=sys.stderr)
    try:
        with open(local_file, "rb") as f:
            file_data = f.read()
    except Exception as e:
        print(f"读取文件失败: {e}", file=sys.stderr)
        sys.exit(1)

    req = urllib.request.Request(url, data=file_data, method="PUT")
    req.add_header("Content-Type", "application/octet-stream")
    req.add_header("Content-Length", str(file_size))

    # 设置覆盖标志
    if overwrite:
        req.add_header("X-BKREPO-OVERWRITE", "true")

    try:
        with urllib.request.urlopen(req, timeout=300) as resp:
            result_text = resp.read().decode("utf-8")
            try:
                result = json.loads(result_text)
            except json.JSONDecodeError:
                # 如果响应不是 JSON，视为成功
                result = {"code": 0}
    except urllib.error.HTTPError as e:
        body_text = e.read().decode("utf-8", errors="replace")
        print(f"上传失败，HTTP 错误 {e.code}: {body_text}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"上传请求失败: {e.reason}", file=sys.stderr)
        sys.exit(1)

    if result.get("code") not in (0, None):
        print(f"上传 API 错误 [{result.get('code')}]: {result.get('message')}", file=sys.stderr)
        sys.exit(1)

    print(f"上传完成: {local_file} -> {path} ({format_size(file_size)})", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(
        description="上传文件到蓝盾制品库（bk-repo）",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  # 上传文件到指定路径
  python bkrepo_upload.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --path /release/app-1.0.0.tgz \\
    --local-file ./dist/app-1.0.0.tgz

  # 上传并覆盖已有文件
  python bkrepo_upload.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --path /release/app-1.0.0.tgz \\
    --local-file ./dist/app-1.0.0.tgz \\
    --overwrite
        """,
    )
    parser.add_argument("--project-id", required=True, help="项目 ID")
    parser.add_argument("--repo-name", required=True, help="仓库名称（如 generic-local）")
    parser.add_argument("--path", required=True, help="文件在仓库中的目标路径（如 /release/app-1.0.0.tgz）")
    parser.add_argument("--local-file", required=True, help="要上传的本地文件路径")
    parser.add_argument("--overwrite", action="store_true", help="如果目标路径已存在文件，是否覆盖（默认不覆盖）")

    args = parser.parse_args()

    # 确保目标路径以 / 开头
    target_path = args.path
    if not target_path.startswith("/"):
        target_path = "/" + target_path

    # 上传文件
    upload_file(args.project_id, args.repo_name, target_path, args.local_file, args.overwrite)

    # 输出结果（YAML 格式）
    file_size = os.path.getsize(args.local_file) if os.path.exists(args.local_file) else 0
    result = {
        "file": os.path.basename(args.local_file),
        "localFile": args.local_file,
        "remotePath": target_path,
        "projectId": args.project_id,
        "repoName": args.repo_name,
        "size": file_size,
        "status": "completed",
    }
    for key, value in result.items():
        print(f"{key}: {value}")


if __name__ == "__main__":
    main()
