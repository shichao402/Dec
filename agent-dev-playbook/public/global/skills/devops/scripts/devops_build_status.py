#!/usr/bin/env python3
"""
蓝盾流水线构建状态查询与诊断脚本
用法：
  python3 devops_build_status.py --url <蓝盾URL>
  python3 devops_build_status.py --url <蓝盾URL> --recursive --depth 3
  python3 devops_build_status.py --project <projectId> --pipeline <pipelineId> --build <buildId>
"""

import argparse
import json
import re
import sys
import time

from common import (
    get_token, api_request, resolve_params, build_status_url, build_history_url,
    parse_devops_url, format_duration_ms, format_timestamp, EXIT_API,
)


def parse_stage_status(stage_status_list):
    stages = []
    for stage in stage_status_list:
        stages.append({
            "name": stage.get("name", stage.get("stageId", "Unknown")),
            "stageId": stage.get("stageId", ""),
            "status": stage.get("status", "UNKNOWN"),
            "elapsed": stage.get("elapsed", 0),
        })
    return stages


def parse_error_info_list(error_info_list, stages):
    """从 errorInfoList 解析失败信息"""
    failures = []
    stage_name_map = {s["stageId"]: s["name"] for s in stages}
    for err in error_info_list:
        failures.append({
            "stage": stage_name_map.get(err.get("stageId"), err.get("stageId", "")),
            "stageId": err.get("stageId", ""),
            "containerId": err.get("containerId", ""),
            "task": err.get("taskName", ""),
            "taskId": err.get("taskId", ""),
            "atomCode": err.get("atomCode", ""),
            "errorType": err.get("errorType", ""),
            "errorCode": err.get("errorCode", ""),
            "errorMsg": err.get("errorMsg", ""),
        })
    return failures


def extract_sub_pipelines_from_variables(variables):
    """从构建变量中提取所有子流水线信息"""
    url_pattern = re.compile(r"^(.+_sub_pipeline)_url$")
    prefixes = set()
    for key in variables:
        if key.startswith("jobs."):
            continue
        m = url_pattern.match(key)
        if m:
            prefixes.add(m.group(1))

    sub_pipelines = []
    for prefix in sorted(prefixes):
        info = {
            "name": prefix,
            "pipelineId": variables.get(f"{prefix}_id", ""),
            "buildId": variables.get(f"{prefix}_buildId", ""),
            "buildNum": variables.get(f"{prefix}_build_num", ""),
            "url": variables.get(f"{prefix}_url", ""),
        }
        for key, value in variables.items():
            if key.startswith("jobs."):
                continue
            if key.startswith(prefix + "_") and key not in (
                f"{prefix}_id", f"{prefix}_buildId",
                f"{prefix}_build_num", f"{prefix}_url",
            ):
                short_key = key[len(prefix) + 1:]
                info.setdefault("outputs", {})[short_key] = value
        sub_pipelines.append(info)

    return sub_pipelines


def extract_trigger_chain(data):
    """从 webhookInfo 和 variables 提取触发链"""
    chain = []
    webhook_info = data.get("webhookInfo")
    if webhook_info and webhook_info.get("webhookEventType") == "PARENT_PIPELINE":
        chain.append({
            "pipelineName": webhook_info.get("parentPipelineName", ""),
            "pipelineId": webhook_info.get("parentPipelineId", ""),
            "buildId": webhook_info.get("parentBuildId", ""),
            "buildNum": webhook_info.get("parentBuildNum", ""),
            "url": webhook_info.get("linkUrl", ""),
        })

    variables = data.get("variables", {})
    grandparent_id = variables.get("BK_CI_PARENT_PIPELINE_ID", "")
    if grandparent_id and (not chain or grandparent_id != chain[0].get("pipelineId")):
        chain.append({
            "pipelineName": variables.get("BK_CI_PARENT_PIPELINE_NAME", ""),
            "pipelineId": grandparent_id,
            "buildId": variables.get("BK_CI_PARENT_BUILD_ID", ""),
            "buildNum": variables.get("BK_CI_PARENT_BUILD_NUM", ""),
        })

    return chain


def extract_build_params(data):
    """从 buildParameters 提取关键构建参数（过滤系统内置参数）"""
    params = data.get("buildParameters", [])
    result = {}
    for p in params:
        key = p.get("key", "")
        if key.startswith("BK_CI_"):
            continue
        value = p.get("value", "")
        if value:
            result[key] = value
    return result


def diagnose_build(token, project, pipeline, build, depth=0, max_depth=0, visited=None):
    """
    诊断单次构建，返回结构化结果。
    当 max_depth > 0 且构建含失败子流水线时，自动递归分析。
    """
    if visited is None:
        visited = set()

    build_key = f"{project}/{pipeline}/{build}"
    if build_key in visited:
        return {"error": "循环引用", "key": build_key}
    visited.add(build_key)

    url = build_status_url(project, pipeline, build)
    indent = "  " * depth
    print(f"{indent}🔍 [{depth}] 查询: {project}/{pipeline}/{build}", file=sys.stderr)
    result = api_request(url, token)

    if result.get("status") != 0:
        return {"error": result.get("message", "Unknown error")}

    data = result.get("data", {})
    variables = data.get("variables", {})

    stage_status = data.get("stageStatus", [])
    stages = parse_stage_status(stage_status)
    error_info_list = data.get("errorInfoList", [])

    failures = parse_error_info_list(error_info_list, stages)
    all_sub_pipelines = extract_sub_pipelines_from_variables(variables)
    trigger_chain = extract_trigger_chain(data)
    build_params = extract_build_params(data)

    stage_timing = sorted(
        [{"name": s["name"], "status": s["status"], "elapsed_ms": s["elapsed"]} for s in stages],
        key=lambda x: x["elapsed_ms"],
        reverse=True
    )

    output = {
        "buildNum": data.get("buildNum"),
        "pipelineName": variables.get("BK_CI_PIPELINE_NAME", ""),
        "status": data.get("status"),
        "trigger": data.get("trigger"),
        "startTime": format_timestamp(data.get("startTime")),
        "endTime": format_timestamp(data.get("endTime")),
        "duration": format_duration_ms(data.get("startTime"), data.get("endTime")),
        "stages": stage_timing,
        "failures": failures,
        "subPipelines": all_sub_pipelines,
        "totalStages": len(stages),
        "failedStages": sum(1 for s in stages if s["status"] == "FAILED"),
    }

    if trigger_chain:
        output["triggerChain"] = trigger_chain
    if build_params:
        output["buildParams"] = build_params

    # 自动递归分析失败的子流水线
    if max_depth > 0 and depth < max_depth and data.get("status") == "FAILED":
        failed_sub_details = []
        # 从 errorInfoList 找到失败的 SubPipeline task
        failed_task_names = set()
        for err in error_info_list:
            atom_code = err.get("atomCode", "")
            if "SubPipeline" in atom_code or "sub_pipeline" in atom_code:
                failed_task_names.add(err.get("taskName", ""))

        # 在 sub_pipelines 变量中找到对应的 URL 并递归
        for sp in all_sub_pipelines:
            sp_url = sp.get("url", "")
            if not sp_url:
                continue
            # 匹配失败的子流水线：名字包含关键词或有 MR_SUCCESS=F 标记
            is_failed = False
            outputs = sp.get("outputs", {})
            for key, val in outputs.items():
                if "SUCCESS" in key and val == "F":
                    is_failed = True
                    break
            if not is_failed:
                # 尝试通过名称匹配
                sp_name = sp.get("name", "")
                for task_name in failed_task_names:
                    norm_task = task_name.lower().replace("主干", "").replace("mr", "")
                    norm_sp = sp_name.lower().replace("_sub_pipeline", "").replace("_", "")
                    if norm_task in norm_sp or norm_sp in norm_task:
                        is_failed = True
                        break

            if is_failed:
                try:
                    sp_project, sp_pipeline, sp_build = parse_devops_url(sp_url)
                    sub_result = diagnose_build(
                        token, sp_project, sp_pipeline, sp_build,
                        depth=depth + 1, max_depth=max_depth, visited=visited
                    )
                    failed_sub_details.append({
                        "name": sp.get("name", ""),
                        "url": sp_url,
                        "diagnosis": sub_result,
                    })
                except SystemExit:
                    failed_sub_details.append({
                        "name": sp.get("name", ""),
                        "url": sp_url,
                        "error": "递归分析失败",
                    })

        if failed_sub_details:
            output["failedSubPipelineDiagnosis"] = failed_sub_details

    return output


def check_stuck(token, project, pipeline, build):
    """检测构建是否卡住：对比历史平均耗时判断是否异常"""
    url = build_status_url(project, pipeline, build)
    result = api_request(url, token)
    if result.get("status") != 0:
        return {"error": result.get("message", "Unknown error")}

    data = result.get("data", {})
    status = data.get("status", "")
    variables = data.get("variables", {})

    if status not in ("RUNNING", "QUEUE"):
        return {
            "stuck": False,
            "status": status,
            "message": f"构建不在运行中（当前状态: {status}），无需检测卡住"
        }

    start_time = data.get("startTime")
    if not start_time:
        return {"stuck": False, "message": "构建尚未开始"}

    now_ms = int(time.time() * 1000)
    elapsed_ms = now_ms - start_time
    elapsed_min = elapsed_ms / 60000

    # 获取历史构建耗时作为基准
    hist_url = build_history_url(project, pipeline, page=1, page_size=10, status="SUCCEED")
    hist_result = api_request(hist_url, token, fatal=False)

    avg_duration_min = None
    if hist_result and hist_result.get("status") == 0:
        records = hist_result.get("data", {}).get("records", [])
        durations = [r.get("totalTime", 0) for r in records if r.get("totalTime")]
        if durations:
            avg_duration_min = (sum(durations) / len(durations)) / 60000

    # 当前卡在哪个 stage
    stage_status = data.get("stageStatus", [])
    current_stage = None
    for stage in stage_status:
        if stage.get("status") == "RUNNING":
            current_stage = stage.get("name", stage.get("stageId", ""))
            break

    output = {
        "stuck": False,
        "status": status,
        "elapsedMinutes": round(elapsed_min, 1),
        "currentStage": current_stage,
        "pipelineName": variables.get("BK_CI_PIPELINE_NAME", ""),
        "buildNum": data.get("buildNum"),
    }

    if avg_duration_min:
        output["historyAvgMinutes"] = round(avg_duration_min, 1)
        ratio = elapsed_min / avg_duration_min
        output["ratio"] = round(ratio, 1)
        if ratio >= 2.0:
            output["stuck"] = True
            output["verdict"] = f"疑似卡住：已运行 {elapsed_min:.0f}min，历史平均 {avg_duration_min:.0f}min（{ratio:.1f}x）"
        elif ratio >= 1.5:
            output["verdict"] = f"偏慢：已运行 {elapsed_min:.0f}min，历史平均 {avg_duration_min:.0f}min（{ratio:.1f}x）"
        else:
            output["verdict"] = f"正常范围：已运行 {elapsed_min:.0f}min，历史平均 {avg_duration_min:.0f}min"
    else:
        output["verdict"] = f"无历史成功记录可对比，当前已运行 {elapsed_min:.0f}min"

    return output


def main():
    parser = argparse.ArgumentParser(description="蓝盾流水线构建状态查询与诊断")
    parser.add_argument("--url", help="蓝盾流水线 URL（自动解析 project/pipeline/build）")
    parser.add_argument("--project", help="项目 ID")
    parser.add_argument("--pipeline", help="流水线 ID")
    parser.add_argument("--build", help="构建 ID")
    parser.add_argument("--access-token", help="可选；本轮对话提供的访问令牌")
    parser.add_argument("--recursive", action="store_true", help="自动递归分析失败的子流水线")
    parser.add_argument("--depth", type=int, default=3, help="递归深度（默认 3）")
    parser.add_argument("--check-stuck", action="store_true", help="检测构建是否卡住（对比历史耗时）")
    parser.add_argument("--json", action="store_true", help="输出原始 API JSON")
    args = parser.parse_args()

    project, pipeline, build = resolve_params(args)
    token = get_token(getattr(args, "access_token", None))

    if args.json:
        url = build_status_url(project, pipeline, build)
        result = api_request(url, token)
        print(json.dumps(result, indent=2, ensure_ascii=False))
        return

    if args.check_stuck:
        output = check_stuck(token, project, pipeline, build)
        print(json.dumps(output, indent=2, ensure_ascii=False))
        return

    max_depth = args.depth if args.recursive else 0
    output = diagnose_build(token, project, pipeline, build, max_depth=max_depth)
    print(json.dumps(output, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
