#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
蓝盾流水线日志下载脚本

Usage:
    python download_log.py --project-id <project_id> --pipeline-id <pipeline_id> --build-id <build_id> [options]

身份验证由 scripts/auth.py 统一处理。
"""

import argparse
import json
import os
import sys
import zipfile
from urllib.parse import urlencode

import requests

from auth import get_access_token


def get_headers(access_token: str) -> dict:
    """获取请求头"""
    return {
        "Content-Type": "application/json",
        "X-Bkapi-Authorization": f'{{"access_token":"{access_token}"}}',
    }


def download_log_from_stream(
    url: str,
    headers: dict,
    output_path: str,
    timeout: int = 300,
) -> str:
    """
    从流式响应下载日志

    Args:
        url: 下载 URL
        headers: 请求头
        output_path: 保存路径
        timeout: 超时时间（秒）

    Returns:
        保存的文件路径
    """
    response = requests.get(url, headers=headers, stream=True, timeout=timeout)
    response.raise_for_status()

    with open(output_path, "wb") as f:
        for chunk in response.iter_content(chunk_size=8192):
            if chunk:
                f.write(chunk)

    return output_path


def is_log_truncated(file_path: str) -> bool:
    """
    检查日志是否因熔断而截断

    Args:
        file_path: 日志文件路径

    Returns:
        如果日志包含熔断标记返回 True
    """
    # 读取文件末尾 1KB 内容进行检查
    try:
        with open(file_path, "rb") as f:
            f.seek(0, 2)  # 移动到文件末尾
            file_size = f.tell()
            check_size = min(10240, file_size)
            f.seek(-check_size, 2)  # 从末尾向前移动 check_size 字节
            tail_content = f.read().decode("utf-8", errors="ignore")
            return "Please download logs to view." in tail_content
    except Exception:
        return False


def get_full_log_download_url(
    project_id: str,
    pipeline_id: str,
    build_id: str,
    element_id: str,
    execute_count: int,
    access_token: str,
) -> str:
    """
    获取完整日志下载 URL

    Args:
        project_id: 项目 ID
        pipeline_id: 流水线 ID
        build_id: 构建 ID
        element_id: 步骤/插件 ID (tag)
        execute_count: 执行次数
        access_token: 访问令牌

    Returns:
        完整日志的下载 URL
    """
    url = f"https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{project_id}/artifactories/log"

    params = {
        "pipelineId": pipeline_id,
        "buildId": build_id,
        "elementId": element_id,
    }
    if execute_count is not None:
        params["executeCount"] = execute_count

    full_url = f"{url}?{urlencode(params)}"
    headers = get_headers(access_token)

    print(f"检测到日志熔断，正在获取完整日志下载链接...")
    print(f"URL: {full_url}")

    response = requests.get(full_url, headers=headers, timeout=60)
    response.raise_for_status()

    data = response.json()
    if data.get("status") != 0:
        raise RuntimeError(f"获取完整日志 URL 失败: {data}")

    download_url = data.get("data", {}).get("url")
    if not download_url:
        raise RuntimeError("未获取到完整日志下载 URL")

    print(f"获取到完整日志下载链接")
    return download_url


def download_and_extract_zip(
    url: str,
    output_dir: str,
    headers: dict = None,
    timeout: int = 300,
) -> str:
    """
    下载并解压 zip 文件

    Args:
        url: zip 文件下载 URL
        output_dir: 解压目标目录
        headers: 请求头（可选）
        timeout: 超时时间（秒）

    Returns:
        解压后的目录路径
    """
    # 下载 zip 文件
    zip_path = os.path.join(output_dir, "full_log.zip")

    print(f"正在下载完整日志 zip 包...")
    response = requests.get(url, headers=headers, stream=True, timeout=timeout)
    response.raise_for_status()

    with open(zip_path, "wb") as f:
        for chunk in response.iter_content(chunk_size=8192):
            if chunk:
                f.write(chunk)

    print(f"zip 包已下载: {zip_path}")

    # 解压 zip 文件
    print(f"正在解压日志文件...")
    extract_dir = os.path.join(output_dir, "full_log")
    os.makedirs(extract_dir, exist_ok=True)

    with zipfile.ZipFile(zip_path, "r") as zip_ref:
        zip_ref.extractall(extract_dir)

    # 删除 zip 文件
    os.remove(zip_path)

    print(f"完整日志已解压到: {extract_dir}")
    return extract_dir


def download_log(
    project_id: str,
    pipeline_id: str,
    build_id: str,
    output_path: str,
    archive_flag: bool = False,
    container_hash_id: str = None,
    execute_count: int = None,
    job_id: str = None,
    step_id: str = None,
    tag: str = None,
    access_token: str = None,
    auto_download_full: bool = True,
) -> str:
    """
    下载蓝盾流水线日志

    如果检测到日志因熔断而截断，会自动获取并下载完整日志。

    Args:
        project_id: 项目 ID
        pipeline_id: 流水线 ID (p-开头)
        build_id: 构建 ID (b-开头)
        output_path: 日志保存路径
        archive_flag: 是否查询归档数据
        container_hash_id: 对应 containerHashId (c-开头)
        execute_count: 执行次数
        job_id: 对应 jobId
        step_id: 对应 stepId
        tag: 对应 element ID (e-开头)
        access_token: 访问令牌，由统一鉴权层提供
        auto_download_full: 检测到熔断时是否自动下载完整日志

    Returns:
        保存的日志文件或目录路径
    """
    access_token = get_access_token(access_token)

    # 构建 URL
    base_url = f"https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{project_id}/logs/download_logs"

    params = {
        "pipelineId": pipeline_id,
        "buildId": build_id,
    }

    if archive_flag:
        params["archiveFlag"] = "true"
    if container_hash_id:
        params["containerHashId"] = container_hash_id
    if execute_count is not None:
        params["executeCount"] = execute_count
    if job_id:
        params["jobId"] = job_id
    if step_id:
        params["stepId"] = step_id
    if tag:
        params["tag"] = tag

    url = f"{base_url}?{urlencode(params)}"
    headers = get_headers(access_token)

    # 发送请求
    print(f"正在下载日志...")
    print(f"URL: {url}")

    download_log_from_stream(url, headers, output_path)
    print(f"日志已保存到: {output_path}")

    # 检查日志是否熔断
    if auto_download_full and tag and is_log_truncated(output_path):
        print("\n⚠️  检测到日志因熔断而截断")

        if not tag:
            print("⚠️  未提供 tag/elementId，无法获取完整日志")
            print("    如需获取完整日志，请使用 --tag 参数指定步骤 ID")
            return output_path

        # 获取完整日志下载 URL
        full_log_url = get_full_log_download_url(
            project_id=project_id,
            pipeline_id=pipeline_id,
            build_id=build_id,
            element_id=tag,
            execute_count=execute_count,
            access_token=access_token,
        )

        # 下载并解压完整日志
        output_dir = os.path.dirname(os.path.abspath(output_path))
        full_log_dir = download_and_extract_zip(full_log_url, output_dir)

        # 在原始日志文件旁创建标记文件
        marker_file = output_path + ".truncated"
        with open(marker_file, "w", encoding="utf-8") as f:
            f.write(f"日志因熔断而截断\n")
            f.write(f"完整日志已下载到: {full_log_dir}\n")

        print(f"\n✅ 处理完成:")
        print(f"   原始日志: {output_path} (熔断截断)")
        print(f"   完整日志: {full_log_dir}")

        return full_log_dir

    return output_path


def main():
    parser = argparse.ArgumentParser(
        description="蓝盾流水线日志下载工具",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  # 下载完整构建日志
  python download_log.py -p your_project -pipeline-id p-xxx -b b-xxx -o ./build.log

  # 下载特定步骤日志（自动处理熔断）
  python download_log.py -p your_project -pipeline-id p-xxx -b b-xxx -t e-xxx -o ./step.log

  # 不自动下载完整日志
  python download_log.py -p your_project -pipeline-id p-xxx -b b-xxx -o ./build.log --no-auto-full
        """,
    )

    # 必填参数
    parser.add_argument(
        "--project-id", "-p", required=True, help="项目 ID"
    )
    parser.add_argument(
        "--pipeline-id", required=True, help="流水线 ID (p-开头)"
    )
    parser.add_argument(
        "--build-id", "-b", required=True, help="构建 ID (b-开头)"
    )
    parser.add_argument(
        "--output", "-o", required=True, help="日志保存路径"
    )

    # 可选参数
    parser.add_argument(
        "--archive-flag", action="store_true", help="是否查询归档数据"
    )
    parser.add_argument(
        "--container-hash-id", help="对应 containerHashId (c-开头)"
    )
    parser.add_argument(
        "--execute-count", type=int, help="执行次数", default=1
    )
    parser.add_argument(
        "--job-id", help="对应 jobId"
    )
    parser.add_argument(
        "--step-id", help="对应 stepId"
    )
    parser.add_argument(
        "--tag", "-t", help="对应 element ID (e-开头)，用于获取熔断后的完整日志"
    )
    parser.add_argument(
        "--access-token", help="可选；本轮对话提供的访问令牌"
    )
    parser.add_argument(
        "--no-auto-full", action="store_true",
        help="检测到日志熔断时不自动下载完整日志"
    )

    args = parser.parse_args()

    try:
        result = download_log(
            project_id=args.project_id,
            pipeline_id=args.pipeline_id,
            build_id=args.build_id,
            output_path=args.output,
            archive_flag=args.archive_flag,
            container_hash_id=args.container_hash_id,
            execute_count=args.execute_count,
            job_id=args.job_id,
            step_id=args.step_id,
            tag=args.tag,
            access_token=args.access_token,
            auto_download_full=not args.no_auto_full,
        )
        print(f"\n最终输出: {result}")
    except requests.exceptions.HTTPError as e:
        print(f"HTTP 请求失败: {e}", file=sys.stderr)
        if e.response is not None:
            try:
                error_data = e.response.json()
                print(f"响应内容: {json.dumps(error_data, indent=2, ensure_ascii=False)}", file=sys.stderr)
            except Exception:
                print(f"响应内容: {e.response.text}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"错误: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
