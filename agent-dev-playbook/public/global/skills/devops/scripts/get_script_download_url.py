#!/usr/bin/env python3
"""
蓝盾制品脚本端下载链接获取脚本
使用 v2 接口，access_token 通过 query 参数传递
"""

from __future__ import annotations

import argparse
import json
import sys
import urllib.request
import urllib.error
import urllib.parse

import yaml

from auth import get_access_token


BASE_URL = "https://devops.apigw.o.woa.com/prod/v2/apigw-user"


def get_script_download_url(
    project_id: str,
    artifactory_type: str,
    path: str,
    access_token: str | None = None,
) -> list:
    params = urllib.parse.urlencode({
        "access_token": get_access_token(access_token),
        "artifactoryType": artifactory_type,
        "path": path,
    })
    url = f"{BASE_URL}/artifactories/projects/{project_id}/thirdPartyDownloadUrl?{params}"

    req = urllib.request.Request(url)
    req.add_header("accept", "application/json")
    req.add_header("Content-Type", "application/json")

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

    return result.get("data", [])


def main():
    parser = argparse.ArgumentParser(
        description="获取蓝盾制品的脚本端下载链接（供 wget/curl 等使用）",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
artifactoryType 取值:
  PIPELINE    流水线仓库
  CUSTOM_DIR  自定义仓库

示例:
  python get_script_download_url.py \\
    --project-id myproject \\
    --artifactory-type PIPELINE \\
    --path /app-1.0.0.tgz
        """,
    )
    parser.add_argument("--project-id", required=True, help="项目 ID")
    parser.add_argument(
        "--artifactory-type",
        required=True,
        choices=["PIPELINE", "CUSTOM_DIR"],
        help="制品仓库类型：PIPELINE 或 CUSTOM_DIR",
    )
    parser.add_argument("--path", required=True, help="文件完整路径（来自搜索结果的 fullPath）")
    parser.add_argument("--access-token", help="可选；本轮对话提供的访问令牌")

    args = parser.parse_args()

    urls = get_script_download_url(
        project_id=args.project_id,
        artifactory_type=args.artifactory_type,
        path=args.path,
        access_token=args.access_token,
    )

    print(yaml.dump({"urls": urls}, allow_unicode=True, sort_keys=False, default_flow_style=False))


if __name__ == "__main__":
    main()
