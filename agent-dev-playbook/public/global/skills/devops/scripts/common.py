#!/usr/bin/env python3
"""
蓝盾流水线诊断 - 公共模块
提供鉴权、API 请求、URL 解析等基础能力。
"""

import json
import re
import sys
import urllib.request
import urllib.error
from datetime import datetime
from urllib.parse import urlencode

from auth import get_access_token

BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user"

# 蓝盾流水线格式规范约定
# projectId: 项目英文名；pipelineId: p- 开头；buildId: b- 开头
RE_PROJECT_ID = re.compile(r"^[a-zA-Z][a-zA-Z0-9_-]*$")
RE_PIPELINE_ID = re.compile(r"^p-[a-zA-Z0-9_-]+$")
RE_BUILD_ID = re.compile(r"^b-[a-zA-Z0-9_-]+$")

# 语义化 exit code
EXIT_OK = 0
EXIT_AUTH = 1       # token 缺失
EXIT_NETWORK = 2   # 网络不可达
EXIT_API = 3       # API 返回业务错误
EXIT_NOT_FOUND = 4 # 资源不存在
EXIT_ARGS = 5      # 参数错误


def get_token(args_token=None):
    """Compatibility wrapper around the unified credential loader."""
    return get_access_token(args_token)


def api_request(url, token, timeout=30, fatal=True):
    """发起蓝盾 API 请求，返回解析后的 JSON。fatal=False 时出错返回 None 而非退出。"""
    auth_header = json.dumps({"access_token": token})
    req = urllib.request.Request(url, headers={
        "Content-Type": "application/json",
        "X-Bkapi-Authorization": auth_header
    })
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        print(json.dumps({
            "error": f"HTTP {e.code}",
            "detail": body[:500]
        }, ensure_ascii=False), file=sys.stderr)
        if not fatal:
            return None
        sys.exit(EXIT_API if e.code < 500 else EXIT_NETWORK)
    except urllib.error.URLError as e:
        print(json.dumps({
            "error": "网络不可达",
            "detail": str(e.reason)
        }, ensure_ascii=False), file=sys.stderr)
        if not fatal:
            return None
        sys.exit(EXIT_NETWORK)
    except json.JSONDecodeError as e:
        print(json.dumps({
            "error": "JSON 解析失败",
            "detail": str(e)
        }, ensure_ascii=False), file=sys.stderr)
        if not fatal:
            return None
        sys.exit(EXIT_API)


def _invalid_id_error(field, value, expected):
    print(json.dumps({
        "error": f"无效的 {field}",
        "value": value,
        "expected": expected,
    }, ensure_ascii=False), file=sys.stderr)
    sys.exit(EXIT_ARGS)


def validate_project_id(project):
    """校验 projectId（项目英文名）"""
    if not project or not RE_PROJECT_ID.match(project):
        _invalid_id_error(
            "projectId",
            project,
            "项目英文名，如 myproject、lct-fe-ai",
        )
    return project


def validate_pipeline_id(pipeline):
    """校验 pipelineId（必须以 p- 开头）"""
    if not pipeline or not RE_PIPELINE_ID.match(pipeline):
        _invalid_id_error(
            "pipelineId",
            pipeline,
            "流水线 ID，以 p- 开头，如 p-abc123",
        )
    return pipeline


def validate_build_id(build, required=True):
    """校验 buildId（必须以 b- 开头）"""
    if not build:
        if required:
            _invalid_id_error(
                "buildId",
                build,
                "构建 ID，以 b- 开头，如 b-xyz789",
            )
        return build
    if not RE_BUILD_ID.match(build):
        _invalid_id_error(
            "buildId",
            build,
            "构建 ID，以 b- 开头，如 b-xyz789",
        )
    return build


def parse_devops_url(url, require_build=False):
    """
    解析蓝盾流水线 URL，返回 (projectId, pipelineId, buildId)。
    支持格式：
      https://devops.woa.com/console/pipeline/{project}/{pipeline}/detail/{build}/executeDetail
      https://devops.woa.com/console/pipeline/{project}/{pipeline}
    也支持不含 buildId 的 URL（返回 buildId 为空字符串，除非 require_build=True）。
    """
    pattern_full = (
        r"devops\.woa\.com/console/pipeline/"
        r"([^/]+)/(p-[^/]+)/detail/(b-[^/]+)"
    )
    m = re.search(pattern_full, url)
    if m:
        project, pipeline, build = m.group(1), m.group(2), m.group(3)
        validate_project_id(project)
        validate_pipeline_id(pipeline)
        validate_build_id(build, required=True)
        return project, pipeline, build

    # URL 含 /detail/ 但未匹配到合法 buildId，避免误回落到无 build 模式
    if re.search(
        r"devops\.woa\.com/console/pipeline/[^/]+/p-[^/]+/detail/[^/]+",
        url,
    ):
        detail_match = re.search(
            r"devops\.woa\.com/console/pipeline/"
            r"([^/]+)/(p-[^/]+)/detail/([^/]+)",
            url,
        )
        raw_build = detail_match.group(3) if detail_match else ""
        _invalid_id_error(
            "buildId",
            raw_build,
            "构建 ID，以 b- 开头，如 b-xyz789",
        )

    pattern_no_build = r"devops\.woa\.com/console/pipeline/([^/]+)/(p-[^/]+)(?:/|$|\?)"
    m2 = re.search(pattern_no_build, url)
    if m2:
        project, pipeline = m2.group(1), m2.group(2)
        validate_project_id(project)
        validate_pipeline_id(pipeline)
        if require_build:
            _invalid_id_error(
                "buildId",
                "",
                "URL 中缺少 buildId，请使用 .../detail/{buildId}/... 路径",
            )
        return project, pipeline, ""

    print(json.dumps({
        "error": "无法解析 URL",
        "url": url,
        "expected": (
            "https://devops.woa.com/console/pipeline/{projectId}/"
            "{pipelineId}/detail/{buildId}/..."
        ),
        "idFormat": {
            "projectId": "项目英文名",
            "pipelineId": "以 p- 开头",
            "buildId": "以 b- 开头",
        },
    }, ensure_ascii=False), file=sys.stderr)
    sys.exit(EXIT_ARGS)


def resolve_project_pipeline(args):
    """
    从 args 中解析 (project, pipeline)，不要求 buildId。
    优先 --url，其次 --project + --pipeline。
    """
    if getattr(args, "url", None):
        project, pipeline, _ = parse_devops_url(args.url, require_build=False)
        return project, pipeline
    if getattr(args, "project", None) and getattr(args, "pipeline", None):
        return (
            validate_project_id(args.project),
            validate_pipeline_id(args.pipeline),
        )
    print(json.dumps({
        "error": "缺少参数",
        "help": "需要 --url 或 --project + --pipeline",
    }, ensure_ascii=False), file=sys.stderr)
    sys.exit(EXIT_ARGS)


def resolve_params(args):
    """
    从 args 中解析出 (project, pipeline, build)。
    优先使用 --url 自动解析，其次使用 --project/--pipeline/--build。
    """
    if getattr(args, "url", None):
        return parse_devops_url(args.url, require_build=True)
    if args.project and args.pipeline and args.build:
        return (
            validate_project_id(args.project),
            validate_pipeline_id(args.pipeline),
            validate_build_id(args.build, required=True),
        )
    print(json.dumps({
        "error": "缺少参数",
        "help": "需要 --url 或 --project + --pipeline + --build",
    }, ensure_ascii=False), file=sys.stderr)
    sys.exit(EXIT_ARGS)


def build_status_url(project, pipeline, build):
    """构建状态查询 URL（query 参数风格）"""
    qs = urlencode({"pipelineId": pipeline, "buildId": build})
    return f"{BASE_URL}/projects/{project}/build_status?{qs}"


def build_log_download_url(project, build, pipeline=None, tag=None, container_hash_id=None, job_id=None, execute_count=1):
    """
    构建日志下载 URL (v4_user_log_download)
    返回 application/octet-stream 格式的日志文本
    """
    params = {"buildId": build, "executeCount": execute_count}
    if pipeline:
        params["pipelineId"] = pipeline
    if tag:
        params["tag"] = tag
    if container_hash_id:
        params["containerHashId"] = container_hash_id
    if job_id:
        params["jobId"] = job_id
    return f"{BASE_URL}/projects/{project}/logs/download_logs?{urlencode(params)}"


def build_artifactory_log_url(project, pipeline, build, element_id, execute_count=1):
    """
    构建熔断归档日志 URL (v4_user_artifactory_log_download)
    返回 JSON 含下载链接 data.url
    """
    qs = urlencode({
        "pipelineId": pipeline,
        "buildId": build,
        "elementId": element_id,
        "executeCount": execute_count,
    })
    return f"{BASE_URL}/projects/{project}/artifactories/log?{qs}"


def download_log(url, token, timeout=60):
    """下载日志文件（octet-stream），返回文本内容"""
    auth_header = json.dumps({"access_token": token})
    req = urllib.request.Request(url, headers={
        "Accept": "application/octet-stream",
        "Content-Type": "application/json",
        "X-Bkapi-Authorization": auth_header
    })
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        print(json.dumps({
            "error": f"HTTP {e.code}",
            "detail": body[:500]
        }, ensure_ascii=False), file=sys.stderr)
        return None
    except urllib.error.URLError as e:
        print(json.dumps({
            "error": "网络不可达",
            "detail": str(e.reason)
        }, ensure_ascii=False), file=sys.stderr)
        return None


def build_history_url(project, pipeline, page=1, page_size=10, status=None):
    """构建历史查询 URL"""
    params = {
        "pipelineId": pipeline,
        "page": page,
        "pageSize": page_size,
    }
    if status:
        params["status"] = status
    return f"{BASE_URL}/projects/{project}/build_histories?{urlencode(params)}"


def calc_duration_ms(start_ms, end_ms):
    """计算起止毫秒时间戳之间的耗时；未结束或缺少时间则返回 None"""
    if not start_ms or not end_ms:
        return None
    return end_ms - start_ms


def format_duration_ms(start_ms, end_ms):
    """将起止毫秒时间戳格式化为可读耗时"""
    duration_ms = calc_duration_ms(start_ms, end_ms)
    if duration_ms is None:
        return "N/A"
    return _format_seconds(duration_ms / 1000)


def format_duration_total(total_time_ms):
    """将总耗时毫秒数格式化为可读耗时"""
    if not total_time_ms:
        return "N/A"
    return _format_seconds(total_time_ms / 1000)


def _format_seconds(s):
    if s < 60:
        return f"{s:.1f}s"
    elif s < 3600:
        return f"{s/60:.1f}min"
    else:
        return f"{s/3600:.1f}h"


def format_timestamp(ts_ms, fmt="%Y-%m-%d %H:%M:%S"):
    """将毫秒时间戳格式化为可读时间字符串"""
    if not ts_ms:
        return "N/A"
    return datetime.fromtimestamp(ts_ms / 1000).strftime(fmt)
