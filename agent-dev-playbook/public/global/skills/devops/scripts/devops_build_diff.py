#!/usr/bin/env python3
"""
蓝盾流水线构建对比脚本

对比两次构建的参数、耗时、错误差异，用于定位"为什么这次失败/变慢了"。

用法：
  # 对比同一流水线的两次构建
  python3 devops_build_diff.py --url <蓝盾URL> --good <buildId> --bad <buildId>

  # 用最近一次成功的作为 baseline
  python3 devops_build_diff.py --url <蓝盾URL> --auto-baseline
"""

import argparse
import json
import sys

from common import (
    get_token, api_request, build_status_url, build_history_url,
    parse_devops_url, validate_build_id, resolve_project_pipeline,
    calc_duration_ms, format_duration_total, EXIT_ARGS,
)


def get_build_summary(token, project, pipeline, build):
    """获取构建摘要信息"""
    url = build_status_url(project, pipeline, build)
    result = api_request(url, token)
    if result.get("status") != 0:
        return None

    data = result.get("data", {})
    variables = data.get("variables", {})

    params = {}
    for p in data.get("buildParameters", []):
        key = p.get("key", "")
        if not key.startswith("BK_CI_"):
            params[key] = p.get("value", "")

    stages = []
    for s in data.get("stageStatus", []):
        stages.append({
            "name": s.get("name", s.get("stageId", "")),
            "status": s.get("status", ""),
            "elapsed_ms": s.get("elapsed", 0),
        })

    errors = []
    for err in data.get("errorInfoList", []):
        errors.append({
            "task": err.get("taskName", ""),
            "code": err.get("errorCode", ""),
            "msg": err.get("errorMsg", ""),
        })

    return {
        "buildNum": data.get("buildNum"),
        "status": data.get("status"),
        "pipelineName": variables.get("BK_CI_PIPELINE_NAME", ""),
        "startTime": data.get("startTime"),
        "endTime": data.get("endTime"),
        "totalDuration_ms": calc_duration_ms(data.get("startTime"), data.get("endTime")),
        "trigger": data.get("trigger", ""),
        "params": params,
        "stages": stages,
        "errors": errors,
    }


def find_last_success(token, project, pipeline):
    """找到最近一次成功构建的 buildId"""
    url = build_history_url(project, pipeline, page=1, page_size=5, status="SUCCEED")
    result = api_request(url, token, fatal=False)
    if not result or result.get("status") != 0:
        return None
    records = result.get("data", {}).get("records", [])
    if records:
        return records[0].get("id")
    return None


def diff_params(good_params, bad_params):
    """对比构建参数差异"""
    all_keys = set(list(good_params.keys()) + list(bad_params.keys()))
    diffs = []
    for key in sorted(all_keys):
        good_val = good_params.get(key)
        bad_val = bad_params.get(key)
        if good_val != bad_val:
            diffs.append({
                "param": key,
                "good": good_val,
                "bad": bad_val,
            })
    return diffs


def diff_stages(good_stages, bad_stages):
    """对比各 stage 耗时差异"""
    good_map = {s["name"]: s for s in good_stages}
    bad_map = {s["name"]: s for s in bad_stages}
    all_names = list(dict.fromkeys([s["name"] for s in good_stages] + [s["name"] for s in bad_stages]))

    diffs = []
    for name in all_names:
        g = good_map.get(name)
        b = bad_map.get(name)
        entry = {"stage": name}
        if g and b:
            entry["good_ms"] = g["elapsed_ms"]
            entry["bad_ms"] = b["elapsed_ms"]
            entry["good_status"] = g["status"]
            entry["bad_status"] = b["status"]
            delta = b["elapsed_ms"] - g["elapsed_ms"]
            entry["delta_ms"] = delta
            if g["elapsed_ms"] > 0:
                entry["ratio"] = round(b["elapsed_ms"] / g["elapsed_ms"], 2)
        elif b:
            entry["bad_ms"] = b["elapsed_ms"]
            entry["bad_status"] = b["status"]
            entry["note"] = "仅 bad 构建存在此 stage"
        elif g:
            entry["good_ms"] = g["elapsed_ms"]
            entry["good_status"] = g["status"]
            entry["note"] = "仅 good 构建存在此 stage"
        diffs.append(entry)
    return diffs


def main():
    parser = argparse.ArgumentParser(description="蓝盾流水线构建对比")
    parser.add_argument("--url", help="蓝盾流水线 URL（用于解析 project/pipeline）")
    parser.add_argument("--project", help="项目 ID")
    parser.add_argument("--pipeline", help="流水线 ID")
    parser.add_argument("--good", help="基准构建 buildId（成功的）")
    parser.add_argument("--bad", help="问题构建 buildId（失败/慢的）")
    parser.add_argument("--auto-baseline", action="store_true",
                        help="自动使用最近一次成功构建作为 good baseline")
    parser.add_argument("--access-token", help="访问令牌")
    parser.add_argument("--json", action="store_true", help="输出原始对比 JSON")
    args = parser.parse_args()

    # 解析 project/pipeline
    if args.url:
        project, pipeline, url_build = parse_devops_url(args.url, require_build=False)
        if not args.bad:
            args.bad = url_build
    else:
        project, pipeline = resolve_project_pipeline(args)

    token = get_token(getattr(args, "access_token", None))

    # 确定 bad build
    if not args.bad:
        print(json.dumps({"error": "需要 --bad <buildId> 或通过 --url 指定"}), file=sys.stderr)
        sys.exit(EXIT_ARGS)
    validate_build_id(args.bad, required=True)

    # 确定 good build
    if args.auto_baseline or not args.good:
        print("🔍 查找最近成功构建作为 baseline...", file=sys.stderr)
        good_build = find_last_success(token, project, pipeline)
        if not good_build:
            print(json.dumps({"error": "未找到成功的历史构建，无法自动对比"}))
            sys.exit(EXIT_ARGS)
    else:
        good_build = validate_build_id(args.good, required=True)

    print(f"📊 对比: good={good_build} vs bad={args.bad}", file=sys.stderr)

    if args.json:
        good_url = build_status_url(project, pipeline, good_build)
        bad_url = build_status_url(project, pipeline, args.bad)
        good_raw = api_request(good_url, token)
        bad_raw = api_request(bad_url, token)
        print(json.dumps({"good": good_raw, "bad": bad_raw}, indent=2, ensure_ascii=False))
        return

    good_summary = get_build_summary(token, project, pipeline, good_build)
    bad_summary = get_build_summary(token, project, pipeline, args.bad)

    if not good_summary:
        print(json.dumps({"error": f"无法获取 good 构建信息: {good_build}"}))
        sys.exit(EXIT_ARGS)
    if not bad_summary:
        print(json.dumps({"error": f"无法获取 bad 构建信息: {args.bad}"}))
        sys.exit(EXIT_ARGS)

    # 对比
    param_diffs = diff_params(good_summary["params"], bad_summary["params"])
    stage_diffs = diff_stages(good_summary["stages"], bad_summary["stages"])

    output = {
        "good": {
            "buildNum": good_summary["buildNum"],
            "buildId": good_build,
            "status": good_summary["status"],
            "duration": format_duration_total(good_summary["totalDuration_ms"]),
            "trigger": good_summary["trigger"],
        },
        "bad": {
            "buildNum": bad_summary["buildNum"],
            "buildId": args.bad,
            "status": bad_summary["status"],
            "duration": format_duration_total(bad_summary["totalDuration_ms"]),
            "trigger": bad_summary["trigger"],
        },
        "durationDiff": (
            format_duration_total(bad_summary["totalDuration_ms"] - good_summary["totalDuration_ms"])
            if good_summary["totalDuration_ms"] is not None
            and bad_summary["totalDuration_ms"] is not None
            else "N/A"
        ),
        "paramDiffs": param_diffs,
        "stageDiffs": [s for s in stage_diffs if s.get("delta_ms", 0) != 0 or s.get("note") or s.get("bad_status") != s.get("good_status")],
        "newErrors": bad_summary["errors"],
    }

    if not param_diffs:
        output["paramDiffSummary"] = "构建参数完全一致"
    else:
        output["paramDiffSummary"] = f"{len(param_diffs)} 个参数不同"

    print(json.dumps(output, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
