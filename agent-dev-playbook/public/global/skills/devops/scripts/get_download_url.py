#!/usr/bin/env python3
"""
蓝盾制品浏览器端下载链接获取脚本
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.request
import urllib.error
import urllib.parse

import yaml

from auth import get_access_token


BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user"


def get_auth_header(access_token: str | None = None) -> str:
    token = get_access_token(access_token)
    auth = {"access_token": token}
    username = os.environ.get("BK_CI_USERNAME")
    if username:
        auth["bk_username"] = username

    return json.dumps(auth, ensure_ascii=False)


def get_download_url(
    project_id: str,
    artifactory_type: str,
    path: str,
    access_token: str | None = None,
) -> dict:
    params = urllib.parse.urlencode({
        "artifactoryType": artifactory_type,
        "path": path,
    })
    url = f"{BASE_URL}/projects/{project_id}/artifactories/user_download_url?{params}"

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
        description="获取蓝盾制品的浏览器端下载链接",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
artifactoryType 取值:
  PIPELINE    流水线仓库
  CUSTOM_DIR  自定义仓库

示例:
  python get_download_url.py \\
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

    data = get_download_url(
        project_id=args.project_id,
        artifactory_type=args.artifactory_type,
        path=args.path,
        access_token=args.access_token,
    )

    output = {"url": data.get("url", "")}
    if data.get("url2"):
        output["url2"] = data["url2"]

    print(yaml.dump(output, allow_unicode=True, sort_keys=False, default_flow_style=False))


if __name__ == "__main__":
    main()
