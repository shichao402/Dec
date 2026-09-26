#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
蓝盾流水线插件编排查询脚本

Usage:
    python get_pipeline_model.py --project-id <project_id> --pipeline-id <pipeline_id> --build-id <build_id> [options]

身份验证由 scripts/auth.py 统一处理。
"""

import argparse
import json
import sys
from urllib.parse import urlencode

import requests

from auth import get_access_token


def get_headers(access_token: str) -> dict:
    """获取请求头"""
    return {
        "Content-Type": "application/json",
        "X-Bkapi-Authorization": f'{{"access_token":"{access_token}"}}',
    }


def fetch_build_detail(
    project_id: str,
    pipeline_id: str,
    build_id: str,
    archive_flag: bool = False,
    execute_count: int = None,
    access_token: str = None,
) -> dict:
    """
    获取构建详情（包含插件编排信息）

    Args:
        project_id: 项目 ID
        pipeline_id: 流水线 ID
        build_id: 构建 ID
        archive_flag: 是否查询归档数据
        execute_count: 执行次数
        access_token: 访问令牌

    Returns:
        构建详情数据
    """
    access_token = get_access_token(access_token)

    base_url = f"https://devops.apigw.o.woa.com/prod/v4/apigw-user/projects/{project_id}/build_detail"

    params = {
        "pipelineId": pipeline_id,
        "buildId": build_id,
    }

    if archive_flag:
        params["archiveFlag"] = "true"
    if execute_count is not None:
        params["executeCount"] = execute_count

    url = f"{base_url}?{urlencode(params)}"
    headers = get_headers(access_token)

    print(f"正在获取构建详情...")
    print(f"URL: {url}")

    response = requests.get(url, headers=headers, timeout=60)
    response.raise_for_status()

    data = response.json()
    if data.get("status") != 0:
        raise RuntimeError(f"获取构建详情失败: {data}")

    return data.get("data", {})


def parse_pipeline_model(model: dict) -> dict:
    """
    解析流水线模型，提取 Job 和插件信息

    Args:
        model: 流水线模型数据

    Returns:
        解析后的流水线结构信息
    """
    result = {
        "pipelineName": model.get("name", "Unknown"),
        "pipelineDesc": model.get("desc", ""),
        "stages": [],
    }

    stages = model.get("stages", [])

    for stage_idx, stage in enumerate(stages, 1):
        stage_info = {
            "stageIndex": stage_idx,
            "stageId": stage.get("id", ""),
            "stageName": stage.get("name", f"Stage {stage_idx}"),
            "status": stage.get("status", ""),
            "jobs": [],
        }

        containers = stage.get("containers", [])
        for container in containers:
            job_info = parse_job(container)
            if job_info:
                stage_info["jobs"].append(job_info)

        result["stages"].append(stage_info)

    return result


def parse_job(container: dict) -> dict:
    """
    解析单个 Job (container) 信息

    Args:
        container: container 数据

    Returns:
        Job 信息
    """
    container_type = container.get("@type", "")

    job_info = {
        "jobId": container.get("jobId", ""),
        "containerId": container.get("containerId", ""),
        "containerHashId": container.get("containerHashId", ""),
        "name": container.get("name", ""),
        "type": container_type,
        "status": container.get("status", ""),
        "baseOS": container.get("baseOS", ""),
        "elements": [],
    }

    # 解析 Job 控制选项
    job_control = container.get("jobControlOption", {})
    if job_control:
        job_info["controlOptions"] = {
            "enabled": job_control.get("enable", True),
            "timeout": job_control.get("timeout", 900),
            "runCondition": job_control.get("runCondition", ""),
            "continueWhenFailed": job_control.get("continueWhenFailed", False),
        }

    # 解析执行环境信息
    dispatch_type = container.get("dispatchType", {})
    if dispatch_type:
        job_info["buildEnv"] = {
            "buildType": dispatch_type.get("buildType", ""),
            "imageName": dispatch_type.get("imageName", ""),
            "imageVersion": dispatch_type.get("imageVersion", ""),
            "performanceUid": dispatch_type.get("performanceUid", ""),
        }

    # 解析插件 (elements)
    elements = container.get("elements", [])
    for element in elements:
        element_info = parse_element(element)
        if element_info:
            job_info["elements"].append(element_info)

    return job_info


def parse_element(element: dict) -> dict:
    """
    解析单个插件 (element) 信息

    Args:
        element: element 数据

    Returns:
        插件信息
    """
    element_type = element.get("@type", "")

    element_info = {
        "elementId": element.get("id", ""),
        "name": element.get("name", ""),
        "type": element_type,
        "atomCode": element.get("atomCode", ""),
        "classType": element.get("classType", ""),
        "status": element.get("status", ""),
        "stepId": element.get("stepId", ""),
        "version": element.get("version", ""),
        "executeCount": element.get("executeCount", 1),
    }

    # 解析不同类型的插件特有信息
    if element_type == "linuxScript":
        element_info["scriptType"] = element.get("scriptType", "")
        element_info["script"] = element.get("script", "")
    elif element_type == "manualTrigger":
        element_info["canElementSkip"] = element.get("canElementSkip", False)
    elif element_type == "timerTrigger":
        element_info["isAutoSubmit"] = element.get("isAutoSubmit", False)

    # 解析插件执行配置
    additional_options = element.get("additionalOptions", {})
    if additional_options:
        element_info["options"] = {
            "enabled": additional_options.get("enable", True),
            "timeout": additional_options.get("timeout", 900),
            "runCondition": additional_options.get("runCondition", ""),
            "continueWhenFailed": additional_options.get("continueWhenFailed", False),
            "retryWhenFailed": additional_options.get("retryWhenFailed", False),
            "retryCount": additional_options.get("retryCount", 1),
        }

    # 解析执行时间
    time_cost = element.get("timeCost", {})
    if time_cost:
        element_info["timeCost"] = {
            "totalCost": time_cost.get("totalCost", 0),
            "executeCost": time_cost.get("executeCost", 0),
            "systemCost": time_cost.get("systemCost", 0),
        }

    return element_info


def print_pipeline_model(model: dict, verbose: bool = False):
    """
    打印流水线模型信息

    Args:
        model: 解析后的流水线模型
        verbose: 是否显示详细信息
    """
    print("\n" + "=" * 80)
    print(f"流水线名称: {model['pipelineName']}")
    if model['pipelineDesc']:
        print(f"流水线描述: {model['pipelineDesc']}")
    print("=" * 80)

    for stage in model["stages"]:
        print_stage(stage, verbose)


def print_stage(stage: dict, verbose: bool = False):
    """
    打印 Stage 信息

    Args:
        stage: Stage 信息
        verbose: 是否显示详细信息
    """
    print(f"\n📦 Stage {stage['stageIndex']}: {stage['stageName']}")
    print(f"   ID: {stage['stageId']}, 状态: {stage['status']}")
    print(f"   {'─' * 70}")

    if not stage["jobs"]:
        print("   (无 Job)")
        return

    for job_idx, job in enumerate(stage["jobs"], 1):
        print_job(job_idx, job, verbose)


def print_job(job_idx: int, job: dict, verbose: bool = False):
    """
    打印 Job 信息

    Args:
        job_idx: Job 序号
        job: Job 信息
        verbose: 是否显示详细信息
    """
    print(f"\n   🔧 Job {job_idx}: {job['name']}")
    print(f"      类型: {job['type']}, 状态: {job['status']}")

    if verbose:
        print(f"      Job ID: {job['jobId']}")
        print(f"      Container ID: {job['containerId']}")
        print(f"      Container Hash ID: {job['containerHashId']}")

    if job.get("baseOS"):
        print(f"      操作系统: {job['baseOS']}")

    # 打印执行环境信息
    build_env = job.get("buildEnv", {})
    if build_env:
        print(f"      构建环境: {build_env.get('imageName', 'N/A')} ({build_env.get('imageVersion', 'N/A')})")
        if verbose and build_env.get('performanceUid'):
            print(f"      规格: {build_env['performanceUid']}")

    # 打印控制选项
    control = job.get("controlOptions", {})
    if control:
        print(f"      超时时间: {control.get('timeout', 'N/A')} 秒")
        if verbose:
            print(f"      运行条件: {control.get('runCondition', 'N/A')}")
            print(f"      失败继续: {control.get('continueWhenFailed', False)}")

    # 打印插件信息
    if job["elements"]:
        print(f"\n      插件列表:")
        for elem_idx, element in enumerate(job["elements"], 1):
            print_element(elem_idx, element, verbose)
    else:
        print(f"\n      (无插件)")


def print_element(elem_idx: int, element: dict, verbose: bool = False):
    """
    打印插件信息

    Args:
        elem_idx: 插件序号
        element: 插件信息
        verbose: 是否显示详细信息
    """
    print(f"         {elem_idx}. {element['name']} [{element['atomCode'] or element['type']}]")
    print(f"            ID: {element['elementId']}")
    print(f"            Step ID: {element.get('stepId', 'N/A')}")

    if verbose:
        print(f"            类型: {element['type']}")
        print(f"            版本: {element['version']}")

    print(f"            状态: {element['status']}, 执行次数: {element['executeCount']}")

    # 打印脚本内容（如果是脚本类型）
    if element.get("script") and verbose:
        script = element["script"]
        script_preview = script[:200] + "..." if len(script) > 200 else script
        print(f"            脚本内容:")
        for line in script_preview.split("\n"):
            print(f"               {line}")

    # 打印执行时间
    time_cost = element.get("timeCost", {})
    if time_cost and time_cost.get("totalCost", 0) > 0:
        print(f"            执行耗时: {time_cost['totalCost']} ms")

    # 打印选项
    options = element.get("options", {})
    if options and verbose:
        print(f"            配置选项:")
        print(f"               启用: {options.get('enabled', True)}")
        print(f"               超时: {options.get('timeout', 900)} 秒")
        print(f"               运行条件: {options.get('runCondition', 'N/A')}")
        if options.get('retryWhenFailed'):
            print(f"               失败重试: 是 (重试 {options.get('retryCount', 1)} 次)")


def export_to_json(model: dict, output_path: str):
    """
    将模型导出为 JSON 文件

    Args:
        model: 解析后的流水线模型
        output_path: 输出文件路径
    """
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(model, f, ensure_ascii=False, indent=2)
    print(f"\n✅ 流水线模型已导出到: {output_path}")


def get_pipeline_model(
    project_id: str,
    pipeline_id: str,
    build_id: str,
    archive_flag: bool = False,
    execute_count: int = None,
    access_token: str = None,
    output_json: str = None,
    verbose: bool = False,
) -> dict:
    """
    获取并解析流水线插件编排信息

    Args:
        project_id: 项目 ID
        pipeline_id: 流水线 ID
        build_id: 构建 ID
        archive_flag: 是否查询归档数据
        execute_count: 执行次数
        access_token: 访问令牌
        output_json: 导出 JSON 文件路径（可选）
        verbose: 是否显示详细信息

    Returns:
        解析后的流水线模型
    """
    # 获取构建详情
    build_detail = fetch_build_detail(
        project_id=project_id,
        pipeline_id=pipeline_id,
        build_id=build_id,
        archive_flag=archive_flag,
        execute_count=execute_count,
        access_token=access_token,
    )

    # 解析流水线模型
    model = build_detail.get("model", {})
    if not model:
        raise RuntimeError("构建详情中未找到流水线模型数据")

    parsed_model = parse_pipeline_model(model)

    # 打印信息
    print_pipeline_model(parsed_model, verbose)

    # 导出 JSON
    if output_json:
        export_to_json(parsed_model, output_json)

    return parsed_model


def main():
    parser = argparse.ArgumentParser(
        description="蓝盾流水线插件编排查询工具",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  # 查询构建的插件编排信息
  python get_pipeline_model.py -p your_project -pipeline-id p-xxx -b b-xxx

  # 显示详细信息
  python get_pipeline_model.py -p your_project -pipeline-id p-xxx -b b-xxx -v

  # 导出为 JSON 文件
  python get_pipeline_model.py -p your_project -pipeline-id p-xxx -b b-xxx -o model.json

  # 查询归档数据
  python get_pipeline_model.py -p your_project -pipeline-id p-xxx -b b-xxx --archive-flag
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

    # 可选参数
    parser.add_argument(
        "--archive-flag", action="store_true", help="是否查询归档数据"
    )
    parser.add_argument(
        "--execute-count", type=int, help="执行次数"
    )
    parser.add_argument(
        "--access-token", help="可选；本轮对话提供的访问令牌"
    )
    parser.add_argument(
        "--verbose", "-v", action="store_true", help="显示详细信息"
    )

    args = parser.parse_args()

    try:
        get_pipeline_model(
            project_id=args.project_id,
            pipeline_id=args.pipeline_id,
            build_id=args.build_id,
            archive_flag=args.archive_flag,
            execute_count=args.execute_count,
            access_token=args.access_token,
            verbose=args.verbose,
        )
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
