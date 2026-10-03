#!/usr/bin/env python3
"""
蓝盾流水线构建历史查询脚本
用法：
  python3 devops_build_history.py --url <蓝盾URL>                    # 查最近10次构建
  python3 devops_build_history.py --url <蓝盾URL> --count 20         # 查最近20次
  python3 devops_build_history.py --url <蓝盾URL> --status FAILED    # 只看失败记录
  python3 devops_build_history.py --project <id> --pipeline <id>
"""

import argparse
import json
import sys

from common import (
    get_token, api_request, build_history_url, resolve_project_pipeline,
    format_duration_total, format_timestamp, EXIT_API, EXIT_ARGS,
)


STATUS_ICONS = {
    "SUCCEED": "✅",
    "FAILED": "❌",
    "CANCELED": "🚫",
    "RUNNING": "🔄",
    "QUEUE": "⏳",
    "STAGE_SUCCESS": "✅",
}


def main():
    parser = argparse.ArgumentParser(description="蓝盾流水线构建历史查询")
    parser.add_argument("--url", help="蓝盾流水线 URL（自动解析 project/pipeline）")
    parser.add_argument("--project", help="项目 ID")
    parser.add_argument("--pipeline", help="流水线 ID")
    parser.add_argument("--access-token", help="可选；本轮对话提供的访问令牌")
    parser.add_argument("--count", type=int, default=10, help="查询条数（默认 10）")
    parser.add_argument("--status", choices=["SUCCEED", "FAILED", "CANCELED", "RUNNING"],
                        help="按状态过滤")
    parser.add_argument("--json", action="store_true", help="输出原始 JSON")
    args = parser.parse_args()

    project, pipeline = resolve_project_pipeline(args)

    token = get_token(getattr(args, "access_token", None))
    url = build_history_url(project, pipeline, page=1, page_size=args.count, status=args.status)

    print(f"🔍 查询构建历史: {project}/{pipeline} (最近 {args.count} 条)", file=sys.stderr)
    result = api_request(url, token)

    if args.json:
        print(json.dumps(result, indent=2, ensure_ascii=False))
        return

    if result.get("status") != 0:
        print(json.dumps({"error": result.get("message", "Unknown error")}, ensure_ascii=False), file=sys.stderr)
        sys.exit(EXIT_API)

    data = result.get("data", {})
    records = data.get("records", [])

    if not records:
        print(json.dumps({"message": "无构建记录", "project": project, "pipeline": pipeline}))
        return

    # 统计摘要
    total = len(records)
    succeed_count = sum(1 for r in records if r.get("status") == "SUCCEED")
    failed_count = sum(1 for r in records if r.get("status") == "FAILED")
    canceled_count = sum(1 for r in records if r.get("status") == "CANCELED")

    # 连续失败计数
    consecutive_failures = 0
    for r in records:
        if r.get("status") == "FAILED":
            consecutive_failures += 1
        else:
            break

    # 构建记录列表
    builds = []
    for r in records:
        status = r.get("status", "UNKNOWN")
        build_info = {
            "buildNum": r.get("buildNum"),
            "status": status,
            "icon": STATUS_ICONS.get(status, "❓"),
            "startTime": format_timestamp(r.get("startTime"), fmt="%m-%d %H:%M"),
            "duration": format_duration_total(r.get("totalTime")),
            "trigger": r.get("trigger", ""),
            "buildId": r.get("id", ""),
        }
        # 如果有错误信息
        error_info_list = r.get("errorInfoList", [])
        if error_info_list:
            build_info["errors"] = [
                {"task": e.get("taskName", ""), "msg": e.get("errorMsg", "")[:100]}
                for e in error_info_list[:3]
            ]
        builds.append(build_info)

    output = {
        "pipeline": pipeline,
        "project": project,
        "summary": {
            "total": total,
            "succeed": succeed_count,
            "failed": failed_count,
            "canceled": canceled_count,
            "successRate": f"{succeed_count/total*100:.0f}%" if total else "N/A",
            "consecutiveFailures": consecutive_failures,
        },
        "builds": builds,
    }

    # 判断问题类型
    if consecutive_failures >= 3:
        output["summary"]["verdict"] = "持续失败（可能是系统性问题）"
    elif consecutive_failures >= 1 and succeed_count > 0:
        output["summary"]["verdict"] = f"偶发失败（最近连续失败 {consecutive_failures} 次，之前有成功）"
    elif consecutive_failures >= 1 and succeed_count == 0:
        output["summary"]["verdict"] = "全部失败（无成功记录）"
    elif failed_count == 0:
        output["summary"]["verdict"] = "全部成功"

    print(json.dumps(output, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
