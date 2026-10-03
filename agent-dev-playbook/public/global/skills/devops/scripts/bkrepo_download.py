#!/usr/bin/env python3
"""
蓝盾制品库（bk-repo）文件下载脚本
从 Generic 仓库下载文件，支持断点续传
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
    """创建临时下载 token"""
    url = f"{BKREPO_BASE_URL}/generic/temporary/token/create"

    body = json.dumps({
        "projectId": project_id,
        "repoName": repo_name,
        "fullPathSet": [path],
        "type": "DOWNLOAD",
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


def download_file(project_id: str, repo_name: str, path: str, output: str) -> None:
    """通过临时 token 下载文件，支持断点续传"""
    # 创建临时下载 token
    print("正在创建临时下载 token...", file=sys.stderr)
    token = create_temporary_token(project_id, repo_name, path)
    print("临时 token 创建成功", file=sys.stderr)

    # 使用制品库自身域名拼接临时下载链接
    encoded_path = urllib.parse.quote(path, safe="/")
    url = f"{BKREPO_DOMAIN}/generic/temporary/download/{project_id}/{repo_name}{encoded_path}?token={token}"

    # 检查是否需要断点续传
    existing_size = 0
    if os.path.exists(output):
        existing_size = os.path.getsize(output)

    req = urllib.request.Request(url)

    if existing_size > 0:
        req.add_header("Range", f"bytes={existing_size}-")
        print(f"检测到已下载 {format_size(existing_size)}，从断点继续下载...", file=sys.stderr)

    try:
        resp = urllib.request.urlopen(req, timeout=60)
    except urllib.error.HTTPError as e:
        if e.code == 416:
            # Range Not Satisfiable，说明文件已完整下载
            print("文件已完整下载，无需重新下载", file=sys.stderr)
            return
        body_text = e.read().decode("utf-8", errors="replace")
        print(f"HTTP 错误 {e.code}: {body_text}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"请求失败: {e.reason}", file=sys.stderr)
        sys.exit(1)

    # 获取总大小
    content_length = resp.headers.get("Content-Length")
    total_size = int(content_length) + existing_size if content_length else None

    # 确保输出目录存在
    output_dir = os.path.dirname(output)
    if output_dir:
        os.makedirs(output_dir, exist_ok=True)

    # 写入模式：断点续传用追加，否则新建
    mode = "ab" if existing_size > 0 else "wb"
    downloaded = existing_size
    chunk_size = 8192

    try:
        with open(output, mode) as f:
            while True:
                chunk = resp.read(chunk_size)
                if not chunk:
                    break
                f.write(chunk)
                downloaded += len(chunk)
                if total_size:
                    progress = downloaded / total_size * 100
                    print(
                        f"\r下载进度: {format_size(downloaded)} / {format_size(total_size)} ({progress:.1f}%)",
                        end="",
                        file=sys.stderr,
                    )
        print("", file=sys.stderr)  # 换行
    except Exception as e:
        print(f"\n写入文件失败: {e}", file=sys.stderr)
        sys.exit(1)
    finally:
        resp.close()

    # 验证文件大小
    actual_size = os.path.getsize(output)
    if total_size and actual_size != total_size:
        print(
            f"警告: 文件大小不匹配，期望 {total_size} 字节，实际 {actual_size} 字节",
            file=sys.stderr,
        )

    print(f"下载完成: {output} ({format_size(actual_size)})", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(
        description="从蓝盾制品库（bk-repo）下载文件",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  # 下载到当前目录
  python bkrepo_download.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --path /release/app-1.0.0.tgz

  # 指定输出路径
  python bkrepo_download.py \\
    --project-id myproject \\
    --repo-name generic-local \\
    --path /release/app-1.0.0.tgz \\
    --output ./downloads/app.tgz
        """,
    )
    parser.add_argument("--project-id", required=True, help="项目 ID")
    parser.add_argument("--repo-name", required=True, help="仓库名称（如 generic-local）")
    parser.add_argument("--path", required=True, help="文件在仓库中的完整路径（如 /release/app-1.0.0.tgz）")
    parser.add_argument("--output", help="输出文件路径（默认为当前目录下的文件名）")

    args = parser.parse_args()

    # 确定输出路径
    output = args.output
    if not output:
        filename = os.path.basename(args.path)
        output = os.path.join(".", filename)

    # 下载文件
    download_file(args.project_id, args.repo_name, args.path, output)

    # 输出结果（YAML 格式）
    actual_size = os.path.getsize(output) if os.path.exists(output) else 0
    result = {
        "file": os.path.basename(args.path),
        "path": args.path,
        "size": actual_size,
        "output": output,
        "status": "completed",
    }
    for key, value in result.items():
        print(f"{key}: {value}")


if __name__ == "__main__":
    main()