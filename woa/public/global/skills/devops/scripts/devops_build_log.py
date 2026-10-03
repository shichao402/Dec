#!/usr/bin/env python3
"""
蓝盾流水线构建日志查询与诊断脚本

支持两种日志获取方式：
  1. download_logs: 直接下载 task 级别日志（octet-stream）
  2. artifactories/log: 获取熔断归档日志的下载 URL

用法：
  # 自动定位失败 task 并下载日志
  python3 devops_build_log.py --url <蓝盾URL>

  # 指定 element 下载日志
  python3 devops_build_log.py --url <蓝盾URL> --tag <elementId>

  # 获取熔断归档日志 URL
  python3 devops_build_log.py --url <蓝盾URL> --tag <elementId> --archive

  # 限制日志行数（默认 500）
  python3 devops_build_log.py --url <蓝盾URL> --lines 200
"""

import argparse
import json
import re
import sys

from common import (
    get_token, api_request, resolve_params, download_log,
    build_status_url, build_log_download_url, build_artifactory_log_url,
    EXIT_API
)

ANSI_ESCAPE_RE = re.compile(r"\x1b\[[0-9;]*m")


def get_failed_elements(token, project, pipeline, build):
    """从 build_status 获取失败 task 的 elementId"""
    url = build_status_url(project, pipeline, build)
    result = api_request(url, token)
    if result.get("status") != 0:
        return []

    data = result.get("data", {})
    error_info_list = data.get("errorInfoList", [])

    elements = []
    for err in error_info_list:
        elements.append({
            "stage": err.get("stageId", ""),
            "task": err.get("taskName", ""),
            "taskId": err.get("taskId", ""),
            "containerId": err.get("containerId", ""),
            "atomCode": err.get("atomCode", ""),
            "errorMsg": err.get("errorMsg", ""),
        })
    return elements


def strip_ansi(text):
    """移除 ANSI escape codes"""
    return ANSI_ESCAPE_RE.sub("", text)


def extract_key_lines(log_text, max_lines=500, search_pattern=None):
    """从日志中提取关键行（错误、警告和上下文）。支持 search_pattern 自定义搜索。"""
    log_text = strip_ansi(log_text)
    lines = log_text.split("\n")
    total = len(lines)

    if total <= max_lines and not search_pattern:
        return lines, total

    # 自定义搜索模式或内置错误关键词
    if search_pattern:
        try:
            pattern_re = re.compile(search_pattern, re.IGNORECASE)
        except re.error:
            pattern_re = re.compile(re.escape(search_pattern), re.IGNORECASE)

        def match_fn(line):
            return pattern_re.search(line)
    else:
        error_keywords_lower = [
            "error", "failed", "fatal", "exception", "traceback",
            "npm err", "cannot find", "module not found", "permission denied",
            "exit code", "exited with", "timed out", "timeout",
            "oomkilled", "killed", "core dumped", "out of memory",
            "outofmemoryerror", "segmentation fault", "build failure",
            "no space left", "connection refused",
        ]

        def match_fn(line):
            lower = line.lower()
            return any(kw in lower for kw in error_keywords_lower)

    important_indices = set()
    for i, line in enumerate(lines):
        if match_fn(line):
            for j in range(max(0, i - 3), min(total, i + 3)):
                important_indices.add(j)

    # 如果匹配到的重要行太多，截取最后 max_lines 行
    if len(important_indices) > max_lines:
        important_indices = set(sorted(important_indices)[-max_lines:])

    # 如果没有匹配到任何错误关键词，返回最后 max_lines 行
    if not important_indices:
        return lines[-max_lines:], total

    selected = sorted(important_indices)
    result_lines = []
    prev_idx = -2
    for idx in selected:
        if idx > prev_idx + 1:
            result_lines.append(f"... (跳过 {idx - prev_idx - 1} 行) ...")
        result_lines.append(lines[idx])
        prev_idx = idx

    return result_lines[:max_lines], total


def main():
    parser = argparse.ArgumentParser(description="蓝盾流水线构建日志查询与诊断")
    parser.add_argument("--url", help="蓝盾流水线 URL（自动解析 project/pipeline/build）")
    parser.add_argument("--project", help="项目 ID")
    parser.add_argument("--pipeline", help="流水线 ID")
    parser.add_argument("--build", help="构建 ID")
    parser.add_argument("--access-token", help="可选；本轮对话提供的访问令牌")
    parser.add_argument("--tag", help="Element ID（e-开头），不指定则自动定位失败 task")
    parser.add_argument("--container", help="Container Hash ID（c-开头）")
    parser.add_argument("--job", help="Job ID")
    parser.add_argument("--archive", action="store_true", help="获取熔断归档日志 URL（而非直接下载）")
    parser.add_argument("--lines", type=int, default=500, help="最大输出行数（默认 500）")
    parser.add_argument("--all-failed", action="store_true", help="下载所有失败 task 的日志")
    parser.add_argument("--search", help="自定义搜索 regex（替代内置错误关键词）")
    args = parser.parse_args()

    project, pipeline, build = resolve_params(args)
    token = get_token(getattr(args, "access_token", None))

    # 如果指定了 --archive 模式
    if args.archive and args.tag:
        print(f"🔍 获取归档日志 URL: element={args.tag}", file=sys.stderr)
        url = build_artifactory_log_url(project, pipeline, build, args.tag)
        result = api_request(url, token, fatal=False)
        if result is None:
            print(json.dumps({"error": "归档日志不存在（仅日志被熔断截断时可用）"}))
        else:
            print(json.dumps(result, indent=2, ensure_ascii=False))
        return

    # 如果指定了 tag，直接下载该 element 的日志
    if args.tag:
        print(f"🔍 下载日志: tag={args.tag}", file=sys.stderr)
        url = build_log_download_url(
            project, build, pipeline=pipeline, tag=args.tag,
            container_hash_id=args.container, job_id=args.job
        )
        log_text = download_log(url, token)
        if log_text is None:
            # 降级：尝试归档日志
            print("⚠️ 直接下载失败，尝试获取归档日志 URL...", file=sys.stderr)
            archive_url = build_artifactory_log_url(project, pipeline, build, args.tag)
            result = api_request(archive_url, token, fatal=False)
            if result:
                print(json.dumps(result, indent=2, ensure_ascii=False))
            else:
                print(json.dumps({"error": "日志下载失败，归档日志也不可用"}))
            return

        key_lines, total_lines = extract_key_lines(log_text, args.lines, search_pattern=args.search)
        print(json.dumps({
            "elementId": args.tag,
            "totalLines": total_lines,
            "shownLines": len(key_lines),
            "searchPattern": args.search,
            "logs": key_lines
        }, indent=2, ensure_ascii=False))
        return

    # 自动定位失败 task
    print("🔍 定位失败 task...", file=sys.stderr)
    failed_elements = get_failed_elements(token, project, pipeline, build)

    if not failed_elements:
        print("✅ 未找到失败的 task", file=sys.stderr)
        print(json.dumps({"failures": [], "message": "No failed tasks found"}))
        return

    targets = failed_elements if args.all_failed else failed_elements[:1]
    results = []

    for elem in targets:
        tag = elem["taskId"]
        task_name = elem["task"]

        if not tag:
            results.append({
                "task": task_name,
                "error": "taskId 为空，无法拉取日志",
                "errorMsg": elem["errorMsg"],
            })
            continue

        print(f"📋 下载日志: {task_name} (tag={tag})", file=sys.stderr)
        url = build_log_download_url(project, build, pipeline=pipeline, tag=tag)
        log_text = download_log(url, token)

        if log_text is None:
            # 降级：尝试归档日志
            print(f"  ⚠️ 直接下载失败，尝试归档日志...", file=sys.stderr)
            archive_url = build_artifactory_log_url(project, pipeline, build, tag)
            archive_result = api_request(archive_url, token, fatal=False)
            if archive_result:
                archive_data = archive_result.get("data", {})
                results.append({
                    "task": task_name,
                    "elementId": tag,
                    "errorMsg": elem["errorMsg"],
                    "logSource": "archive",
                    "archiveUrl": archive_data.get("url", ""),
                    "archiveUrl2": archive_data.get("url2", ""),
                })
            else:
                results.append({
                    "task": task_name,
                    "elementId": tag,
                    "errorMsg": elem["errorMsg"],
                    "logSource": "unavailable",
                    "note": "日志下载失败且归档不可用",
                })
            continue

        key_lines, total_lines = extract_key_lines(log_text, args.lines, search_pattern=args.search)
        results.append({
            "task": task_name,
            "elementId": tag,
            "atomCode": elem.get("atomCode", ""),
            "errorMsg": elem["errorMsg"],
            "logSource": "download",
            "totalLines": total_lines,
            "shownLines": len(key_lines),
            "logs": key_lines,
        })

    output = {
        "totalFailures": len(failed_elements),
        "queriedTasks": len(results),
        "results": results,
    }
    print(json.dumps(output, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
