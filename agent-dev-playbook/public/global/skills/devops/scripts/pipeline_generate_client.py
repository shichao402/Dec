#!/usr/bin/env python3
"""
蓝盾流水线编排生成 Python 客户端

用于生成并保存普通流水线编排，包括：
- 查询市场插件列表 (list-atoms)
- 获取插件官方 YAML 模板（首选，用于 yaml 生成）(atom-yaml)
- 获取插件详细信息 (atom-detail)
- 通过模板创建流水线 (create-pipeline)
- 保存流水线编排草稿 (save-draft)
- 发布流水线版本 (release-version)
- 获取流水线指定版本编排 (get-version)

使用方法:
    python pipeline_generate_client.py <command> [options]

身份验证由 scripts/auth.py 统一处理。
"""

import argparse
import json
import sys
from typing import Any, Dict, Optional
from urllib.parse import urlencode
import urllib.request
import urllib.error
import yaml

from auth import get_access_token


def print_data(data: Any) -> None:
    """以 YAML 打印数据"""
    print(yaml.dump(data, allow_unicode=True, sort_keys=False))


BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user"


def make_request(
    url: str,
    method: str = "GET",
    headers: Optional[Dict[str, str]] = None,
    data: Optional[Dict[str, Any]] = None,
    access_token: Optional[str] = None,
) -> Dict[str, Any]:
    req_headers = headers or {}
    req_headers["Content-Type"] = "application/json"

    if access_token:
        auth_header = json.dumps({"access_token": access_token}, separators=(",", ":"))
        req_headers["X-Bkapi-Authorization"] = auth_header

    req_data = None
    if data is not None and method in ("POST", "PUT"):
        req_data = json.dumps(data).encode("utf-8")

    request = urllib.request.Request(
        url, data=req_data, headers=req_headers, method=method,
    )

    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        error_body = e.read().decode("utf-8")
        print(f"HTTP Error {e.code}: {error_body}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"URL Error: {e.reason}", file=sys.stderr)
        sys.exit(1)
    except json.JSONDecodeError as e:
        print(f"JSON Decode Error: {e}", file=sys.stderr)
        sys.exit(1)


def make_request_no_exit(
    url: str,
    method: str = "GET",
    headers: Optional[Dict[str, str]] = None,
    data: Optional[Dict[str, Any]] = None,
    access_token: Optional[str] = None,
) -> Dict[str, Any]:
    """与 make_request 相同，但出错时不 exit 而是返回错误信息，用于草稿保存重试场景"""
    req_headers = headers or {}
    req_headers["Content-Type"] = "application/json"

    if access_token:
        auth_header = json.dumps({"access_token": access_token}, separators=(",", ":"))
        req_headers["X-Bkapi-Authorization"] = auth_header

    req_data = None
    if data is not None and method in ("POST", "PUT"):
        req_data = json.dumps(data).encode("utf-8")

    request = urllib.request.Request(
        url, data=req_data, headers=req_headers, method=method,
    )

    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        error_body = e.read().decode("utf-8")
        try:
            return json.loads(error_body)
        except json.JSONDecodeError:
            return {"status": e.code, "message": error_body, "_http_error": True}
    except urllib.error.URLError as e:
        return {"status": -1, "message": str(e.reason), "_url_error": True}
    except json.JSONDecodeError as e:
        return {"status": -1, "message": str(e), "_json_error": True}


def read_json_input(args, file_attr: str, inline_attr: str) -> Optional[Dict[str, Any]]:
    """从 --xxx-file 或 --xxx 读取 JSON 数据"""
    raw = None
    if hasattr(args, file_attr) and getattr(args, file_attr):
        try:
            with open(getattr(args, file_attr), "r", encoding="utf-8") as f:
                raw = f.read().strip()
        except (IOError, OSError) as e:
            print(f"读取文件失败: {e}", file=sys.stderr)
            sys.exit(1)
    elif hasattr(args, inline_attr) and getattr(args, inline_attr):
        raw = getattr(args, inline_attr)

    if raw:
        try:
            return json.loads(raw)
        except json.JSONDecodeError as e:
            print(f"JSON 解析错误: {e}", file=sys.stderr)
            sys.exit(1)
    return None


def read_text_input(args, file_attr: str, inline_attr: str) -> Optional[str]:
    """从 --xxx-file 或 --xxx 读取纯文本（用于 YAML 等不需要解析的内容）"""
    if hasattr(args, file_attr) and getattr(args, file_attr):
        try:
            with open(getattr(args, file_attr), "r", encoding="utf-8") as f:
                return f.read()
        except (IOError, OSError) as e:
            print(f"读取文件失败: {e}", file=sys.stderr)
            sys.exit(1)
    if hasattr(args, inline_attr) and getattr(args, inline_attr):
        return getattr(args, inline_attr)
    return None


def validate_yaml_syntax(yaml_text: str) -> Optional[str]:
    """本地预校验 YAML 语法，发现错误时返回错误描述，正常时返回 None。"""
    try:
        yaml.safe_load(yaml_text)
        return None
    except yaml.YAMLError as e:
        return str(e)


def lint_yaml_or_exit(yaml_text: str, skip: bool = False) -> None:
    """按官方 schema 预校验编排，拦下后端 2100130「yaml不合法」那一类错误。"""
    if skip:
        return
    try:
        from pipeline_yaml_lint import format_report, lint_yaml_text
    except ImportError:
        print("提示：未找到 pipeline_yaml_lint.py，跳过 schema 预校验。", file=sys.stderr)
        return

    issues, note = lint_yaml_text(yaml_text)
    errors = [i for i in issues if i.level == "error"]
    if errors or any(i.level == "warning" for i in issues):
        print(format_report(issues, note), file=sys.stderr)
    if errors:
        print(
            "\n🔴 已在本地拦截（exit 2），不要直接提交给后端。按上面的「修法」改 YAML 后重试；\n"
            "   纯机械错误可以先跑一遍自动修复（会重排格式、丢注释）：\n"
            "     python pipeline_yaml_lint.py --yaml-file <file> --fix --in-place\n"
            "   改两次仍不通过，就换等价的 modelAndSetting JSON 走 --draft-file 兜底。",
            file=sys.stderr,
        )
        sys.exit(2)


# ==================== list-atoms ====================

def cmd_list_atoms(args: argparse.Namespace) -> None:
    """获取市场插件列表"""
    access_token = get_access_token(args.access_token)

    url = f"{BASE_URL}/atoms/atom_list"
    params: Dict[str, Any] = {
        "projectCode": args.project_id,
        "serviceScope": "PIPELINE",
        "category": "TASK",
        "queryProjectAtomFlag": "true",
        "page": 1,
        "pageSize": 100,
    }

    if args.category:
        params["category"] = args.category
    if args.classify_id:
        params["classifyId"] = args.classify_id
    if args.keyword:
        params["keyword"] = args.keyword
    if args.os:
        params["os"] = args.os
    if args.job_type:
        params["jobType"] = args.job_type
    if args.service_scope:
        params["serviceScope"] = args.service_scope
    if args.page is not None:
        params["page"] = args.page
    if args.page_size is not None:
        params["pageSize"] = args.page_size
    if args.recommend_flag is not None:
        params["recommendFlag"] = str(args.recommend_flag).lower()
    if args.query_project_atom_flag is not None:
        params["queryProjectAtomFlag"] = str(args.query_project_atom_flag).lower()

    full_url = f"{url}?{urlencode(params)}" if params else url

    result = make_request(full_url, access_token=access_token)

    if result.get("status") == 0 and "data" in result and "records" in result["data"]:
        cleaned_records = []
        for record in result["data"]["records"]:
            cleaned = {
                "name": record.get("name"),
                "atomCode": record.get("atomCode"),
                "version": record.get("version"),
                "defaultVersion": record.get("defaultVersion"),
                "classType": record.get("classType"),
                "serviceScope": record.get("serviceScope"),
                "os": record.get("os"),
                "category": record.get("category"),
                "summary": record.get("summary"),
                "description": record.get("description"),
                "labelList": [
                    {"labelName": lbl.get("labelName"), "labelCode": lbl.get("labelCode")}
                    for lbl in (record.get("labelList") or [])
                ],
                "installed": record.get("installed"),
                "buildLessRunFlag": record.get("buildLessRunFlag"),
            }
            cleaned_records.append(cleaned)
        result["data"]["records"] = cleaned_records
        result["_cleaned"] = True

    print_data(result)


# ==================== atom-yaml ====================

def cmd_atom_yaml(args: argparse.Namespace) -> None:
    """
    获取插件官方 YAML 模板（YAML 编排首选数据来源）

    API: GET /atoms/atom_yml_v2_detail?atomCode={atomCode}&defaultShowFlag={true|false}

    返回插件官方维护的 yml 2.0 片段，可直接拷贝/粘贴到流水线 YAML 的 steps 中，
    省去人工拼 with 字段的猜测成本。
    """
    access_token = get_access_token(args.access_token)

    url = f"{BASE_URL}/atoms/atom_yml_v2_detail"
    params: Dict[str, Any] = {"atomCode": args.atom_code}
    if args.default_show is not None:
        params["defaultShowFlag"] = "true" if args.default_show else "false"

    full_url = f"{url}?{urlencode(params)}"

    result = make_request(full_url, access_token=access_token)

    yaml_text = result.get("data") if isinstance(result.get("data"), str) else None
    if result.get("status") == 0 and yaml_text is not None:
        if args.format == "raw":
            sys.stdout.write(yaml_text)
            if not yaml_text.endswith("\n"):
                sys.stdout.write("\n")
            return
        if args.format == "envelope":
            print_data(result)
            return
        sys.stdout.write(f"# atomCode:        {args.atom_code}\n")
        sys.stdout.write(f"# defaultShowFlag: {args.default_show}\n")
        sys.stdout.write("# source:          atom_yml_v2_detail\n")
        sys.stdout.write("# ----- 插件官方 YAML 模板（可直接拷贝到 steps 下使用） -----\n")
        sys.stdout.write(yaml_text)
        if not yaml_text.endswith("\n"):
            sys.stdout.write("\n")
        return

    print_data(result)


# ==================== atom-detail ====================

def cmd_atom_detail(args: argparse.Namespace) -> None:
    """根据插件代码获取插件详细信息"""
    access_token = get_access_token(args.access_token)

    version = args.version or "1.*"

    url = f"{BASE_URL}/atoms/atom_detail"
    params: Dict[str, Any] = {
        "atomCode": args.atom_code,
        "version": version,
    }
    if args.service_scope:
        params["serviceScope"] = args.service_scope

    full_url = f"{url}?{urlencode(params)}"

    result = make_request(full_url, access_token=access_token)

    if result.get("status") == 0 and "data" in result:
        data = result["data"]
        cleaned = {
            "name": data.get("name"),
            "atomCode": data.get("atomCode"),
            "version": data.get("version"),
            "classType": data.get("classType"),
            "summary": data.get("summary"),
            "description": data.get("description"),
            "serviceScope": data.get("serviceScope"),
            "os": data.get("os"),
            "jobType": data.get("jobType"),
            "jobTypeMap": data.get("jobTypeMap"),
            "category": data.get("category"),
            "props": data.get("props"),
            "versionList": data.get("versionList"),
            "serviceScopeDetails": data.get("serviceScopeDetails"),
        }
        result["data"] = cleaned
        result["_cleaned"] = True

    print_data(result)


# ==================== create-pipeline ====================

def cmd_create_pipeline(args: argparse.Namespace) -> None:
    """
    通过模板创建流水线（仅创建，不含编排内容）

    API: POST /projects/{projectId}/version/create_with_template

    返回 pipelineId 供后续 save-draft 使用。
    --template-id 和 --template-version 需从项目模板列表中获取。
    """
    access_token = get_access_token(args.access_token)

    url = f"{BASE_URL}/projects/{args.project_id}/version/create_with_template"

    body: Dict[str, Any] = {
        "pipelineName": args.pipeline_name,
        "templateId": args.template_id,
        "templateVersion": int(args.template_version),
        "instanceType": "FREEDOM",
        "emptyTemplate": True,
    }

    result = make_request(url, method="POST", data=body, access_token=access_token)
    print_data(result)


# ==================== save-draft ====================

def cmd_save_draft(args: argparse.Namespace) -> None:
    """
    保存流水线编排草稿

    API: POST /projects/{projectId}/version/save_draft

    保存策略：对话用 YAML 展示，保存以 JSON 兜底，再回拉后端 YAML
    1. YAML（首选）：通过 --yaml / --yaml-file 传入 YAML 编排
       - 自动设置 storageType=YAML
       - 必须配合 --pipeline-id（从 create-pipeline 返回获取）
       - 调用前会本地预校验 YAML 语法
    2. JSON modelAndSetting（稳定兜底）：通过 --draft / --draft-file 传入完整 JSON
       - storageType 默认为 MODEL
       - YAML 一次失败（语法错或 API 校验错）立即用本模式兜底，不要在 YAML 上反复纠错
       - 兜底成功后调用方应紧跟一步 get-version --format yaml 拿后端规范化 YAML 重新展示
    """
    access_token = get_access_token(args.access_token)

    yaml_text = read_text_input(args, "yaml_file", "yaml")
    draft_data = read_json_input(args, "draft_file", "draft")

    if not yaml_text and not draft_data:
        print(
            "错误：必须通过 --yaml/--yaml-file 或 --draft/--draft-file 至少提供一种保存数据。\n"
            "保存策略：对话用 YAML 展示 → 优先 --yaml-file → 一次失败立即用 --draft-file 兜底\n"
            "  → 兜底成功后用 `get-version --format yaml` 拉后端规范 YAML 重新展示。",
            file=sys.stderr,
        )
        sys.exit(1)

    if yaml_text:
        yaml_err = validate_yaml_syntax(yaml_text)
        if yaml_err:
            print(
                "YAML 语法错误（exit 2）：\n"
                f"  {yaml_err}\n\n"
                "🔴 建议：不要反复修 YAML 试探（缩进/引号/类型这类错很难一次修对，浪费时间）。\n"
                "   立即把同样的编排意图组成等价 modelAndSetting JSON，写到 ./draft.json 后调：\n"
                "     pipeline_generate_client.py save-draft --project-id ... --pipeline-id ... --draft-file ./draft.json\n"
                "   兜底保存成功后，再用 get-version --pipeline-id ... --version <new_version> --format yaml\n"
                "   拿后端规范化 YAML 在对话中重新展示。",
                file=sys.stderr,
            )
            sys.exit(2)
        lint_yaml_or_exit(yaml_text, skip=getattr(args, "skip_lint", False))

    if yaml_text and draft_data:
        body = dict(draft_data)
        body["storageType"] = "YAML"
        body["yaml"] = yaml_text
        if args.pipeline_id and not body.get("pipelineId"):
            body["pipelineId"] = args.pipeline_id
    elif yaml_text:
        if not args.pipeline_id:
            print(
                "错误：YAML 模式下必须通过 --pipeline-id 指定要保存的流水线 ID（来自 create-pipeline 返回值）。",
                file=sys.stderr,
            )
            sys.exit(1)
        body = {
            "pipelineId": args.pipeline_id,
            "storageType": "YAML",
            "yaml": yaml_text,
            "baseVersion": 0,
            "description": args.description or "",
            "modelAndSetting": {
                "model": {
                    "name": args.pipeline_name or "",
                    "desc": "",
                    "labels": [],
                    "stages": [],
                    "staticViews": [],
                },
                "setting": {
                    "projectId": args.project_id,
                    "pipelineId": args.pipeline_id,
                    "pipelineName": args.pipeline_name or "",
                },
            },
        }
    else:
        body = dict(draft_data)
        if not body.get("storageType"):
            body["storageType"] = "MODEL"
        if args.pipeline_id and not body.get("pipelineId"):
            body["pipelineId"] = args.pipeline_id

    url = f"{BASE_URL}/projects/{args.project_id}/version/save_draft"

    result = make_request_no_exit(url, method="POST", data=body, access_token=access_token)
    print_data(result)


# ==================== release-version ====================

def cmd_release_version(args: argparse.Namespace) -> None:
    """
    将草稿发布为正式版本

    API: POST /projects/{projectId}/version/release_version?pipelineId={}&version={}
    """
    access_token = get_access_token(args.access_token)

    url = (
        f"{BASE_URL}/projects/{args.project_id}/version/release_version"
        f"?{urlencode({'pipelineId': args.pipeline_id, 'version': args.version})}"
    )

    body = {
        "description": args.description or "",
        "enablePac": False,
        "yamlInfo": None,
    }

    result = make_request(url, method="POST", data=body, access_token=access_token)
    print_data(result)


# ==================== get-version ====================

def _summarize_model(model: Dict[str, Any]) -> Dict[str, Any]:
    """提取 modelAndSetting.model 的关键结构信息（stages -> jobs -> elements）"""
    summary_stages = []
    for stage in model.get("stages") or []:
        jobs = []
        for container in stage.get("containers") or []:
            elements = [
                {
                    "name": e.get("name"),
                    "atomCode": e.get("atomCode"),
                    "version": e.get("version"),
                    "id": e.get("id"),
                }
                for e in (container.get("elements") or [])
            ]
            jobs.append({
                "jobName": container.get("name"),
                "containerType": container.get("classType"),
                "elementCount": len(elements),
                "elements": elements,
            })
        summary_stages.append({
            "stageId": stage.get("id"),
            "stageName": stage.get("name"),
            "finally": stage.get("finally", False),
            "jobCount": len(jobs),
            "jobs": jobs,
        })
    return {
        "name": model.get("name"),
        "desc": model.get("desc"),
        "stageCount": len(summary_stages),
        "stages": summary_stages,
    }


def cmd_get_version(args: argparse.Namespace) -> None:
    """
    获取流水线指定版本的编排（YAML / modelAndSetting）

    API: GET /projects/{projectId}/version/get_version?pipelineId={}&version={}

    --format yaml    默认，直接输出 YAML 原文（yamlSupported=false 时自动降级为 summary）
    --format json    输出 _meta + modelAndSetting JSON
    --format summary 输出版本元信息 + stage/job/插件骨架摘要
    --format full    输出 API 原始 data 全部字段
    """
    access_token = get_access_token(args.access_token)

    url = (
        f"{BASE_URL}/projects/{args.project_id}/version/get_version"
        f"?{urlencode({'pipelineId': args.pipeline_id, 'version': args.version})}"
    )

    result = make_request(url, access_token=access_token)

    if result.get("status") != 0 or "data" not in result:
        print_data(result)
        return

    data = result["data"]
    fmt = getattr(args, "format", "yaml")

    update_time_ms = data.get("updateTime")
    update_time_str = None
    if isinstance(update_time_ms, (int, float)) and update_time_ms > 0:
        import datetime
        dt = datetime.datetime.fromtimestamp(
            update_time_ms / 1000,
            tz=datetime.timezone(datetime.timedelta(hours=8)),
        )
        update_time_str = dt.strftime("%Y-%m-%d %H:%M:%S")

    yaml_supported = data.get("yamlSupported", False)
    yaml_invalid_msg = data.get("yamlInvalidMsg") or ""
    yaml_preview = data.get("yamlPreview") or {}
    yaml_content = yaml_preview.get("yaml") or ""

    meta = {
        "projectId": args.project_id,
        "pipelineId": args.pipeline_id,
        "version": data.get("version"),
        "versionName": data.get("versionName"),
        "baseVersion": data.get("baseVersion"),
        "baseVersionName": data.get("baseVersionName"),
        "description": data.get("description"),
        "updater": data.get("updater"),
        "updateTime": update_time_str or update_time_ms,
        "canDebug": data.get("canDebug"),
        "yamlSupported": yaml_supported,
        "yamlInvalidMsg": yaml_invalid_msg if yaml_invalid_msg else None,
    }

    if fmt == "full":
        print_data({"_meta": meta, "data": data})
        return

    if fmt == "json":
        model_and_setting = data.get("modelAndSetting") or {}
        output = {"_meta": meta, "modelAndSetting": model_and_setting}
        print(json.dumps(output, ensure_ascii=False, indent=2))
        return

    if fmt == "summary":
        model = (data.get("modelAndSetting") or {}).get("model") or {}
        print_data({"_meta": meta, "summary": _summarize_model(model)})
        return

    # fmt == "yaml"（默认）
    if yaml_supported and yaml_content:
        version_num = data.get("version", "?")
        version_name = data.get("versionName", "")
        base_ver = data.get("baseVersion", "?")
        base_ver_name = data.get("baseVersionName", "")
        sys.stdout.write(f"# projectId:    {args.project_id}\n")
        sys.stdout.write(f"# pipelineId:   {args.pipeline_id}\n")
        sys.stdout.write(f"# version:      {version_num} ({version_name})\n")
        sys.stdout.write(f"# baseVersion:  {base_ver} ({base_ver_name})\n")
        sys.stdout.write(f"# updater:      {data.get('updater', '')}\n")
        sys.stdout.write(f"# updateTime:   {update_time_str or update_time_ms or ''}\n")
        sys.stdout.write(f"# description:  {data.get('description', '')}\n")
        sys.stdout.write("# ----- YAML 编排原文如下 -----\n")
        sys.stdout.write(yaml_content)
        if not yaml_content.endswith("\n"):
            sys.stdout.write("\n")
    else:
        # YAML 不可用，自动降级为 summary
        model = (data.get("modelAndSetting") or {}).get("model") or {}
        note = (
            "yamlSupported=false，已自动降级为 summary 视图。"
            if not yaml_supported
            else f"YAML 解析异常: {yaml_invalid_msg}，已自动降级为 summary 视图。"
        )
        print_data({"_meta": meta, "_note": note, "summary": _summarize_model(model)})


# ==================== main ====================

def main():
    parser = argparse.ArgumentParser(
        prog="pipeline_generate_client.py",
        description="蓝盾流水线编排生成 Python 客户端",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
完整流程:
  1. list-atoms    → 查询可用插件
  2. atom-yaml     → ⭐ 拿插件官方 YAML 模板（首选，可直接拷到 steps 下）
                  → atom-detail 仅在需要看字段含义/类型时补查
  3. create-pipeline → 通过模板创建流水线（获取 pipelineId）
  4. save-draft    → 保存完整编排草稿（YAML 失败立即用 --draft-file JSON 兜底）
  5. release-version → 发布正式版本
  6. get-version   → 查看指定版本编排（YAML/JSON/摘要）

鉴权:
  从 bkToken/get 获取 token 后，交给 AI 配置或自行填写 Skill config.json
        """,
    )

    common_parser = argparse.ArgumentParser(add_help=False)
    common_parser.add_argument(
        "--access-token",
        help="可选；本轮对话提供的访问令牌",
    )

    subparsers = parser.add_subparsers(dest="command", required=True)

    # list-atoms
    p = subparsers.add_parser("list-atoms", parents=[common_parser],
                              help="获取市场插件列表")
    p.add_argument("--project-id", required=True, help="项目编码（用于过滤项目可见插件）")
    p.add_argument("--category", choices=["TRIGGER", "TASK"])
    p.add_argument("--classify-id")
    p.add_argument("--keyword", help="搜索关键字")
    p.add_argument("--os", choices=["ALL", "WINDOWS", "LINUX", "MACOS"])
    p.add_argument("--job-type", choices=["AGENT", "AGENT_LESS"])
    p.add_argument("--service-scope", help="服务范围（PIPELINE/QUALITY 等，默认 PIPELINE）")
    p.add_argument("--page", type=int)
    p.add_argument("--page-size", type=int)
    p.add_argument("--recommend-flag", type=lambda x: x.lower() == "true")
    p.add_argument("--query-project-atom-flag", type=lambda x: x.lower() == "true")

    # atom-yaml
    p = subparsers.add_parser(
        "atom-yaml", parents=[common_parser],
        help="获取插件官方 YAML 模板（生成流水线 YAML 时的首选）",
        description=(
            "返回插件官方维护的 yml 2.0 片段，可直接拷到流水线 YAML 的 steps 下使用。"
            "对比 atom-detail：atom-yaml 给的是即用模板（uses+with 完整骨架），"
            "atom-detail 给的是字段级元信息（props.input 字段定义）。"
        ),
    )
    p.add_argument("--atom-code", required=True, help="插件 atomCode（如 run / linuxScript）")
    p.add_argument(
        "--default-show", type=lambda x: x.lower() == "true",
        help="是否展示系统自带的 yml 信息（true/false）",
    )
    p.add_argument(
        "--format", choices=["pretty", "raw", "envelope"], default="pretty",
        help=(
            "输出格式：pretty=元信息头+YAML（默认）；"
            "raw=只输出 YAML 原文（便于 > 重定向到 .yml）；"
            "envelope=输出原始 API JSON 包装（debug 用）"
        ),
    )

    # atom-detail
    p = subparsers.add_parser("atom-detail", parents=[common_parser],
                              help="获取插件字段级定义（atom-yaml 拿不到字段含义时的兜底）")
    p.add_argument("--atom-code", required=True, help="插件 atomCode（如 run / linuxScript）")
    p.add_argument("--version", default="1.*",
                   help="插件版本号，默认 1.*（取该主版本下最新；部分插件需用 2.*）")
    p.add_argument("--service-scope")

    # create-pipeline
    p = subparsers.add_parser("create-pipeline", parents=[common_parser],
                              help="通过模板创建流水线（仅创建骨架，不含编排内容）")
    p.add_argument("--project-id", required=True, help="项目编码")
    p.add_argument("--pipeline-name", required=True, help="流水线名称")
    p.add_argument("--template-id", required=True,
                   help="模板 ID（从项目模板列表获取）")
    p.add_argument("--template-version", required=True,
                   help="模板版本号（从项目模板列表获取）")

    # save-draft
    p = subparsers.add_parser("save-draft", parents=[common_parser],
                              help="保存流水线编排草稿（支持 YAML/JSON 双模式，优先 YAML）")
    p.add_argument("--project-id", required=True, help="项目编码")
    p.add_argument("--pipeline-id",
                   help="流水线 ID（YAML 模式必填，来自 create-pipeline 返回值）")
    p.add_argument("--pipeline-name",
                   help="流水线名称（YAML 模式可选，仅作 modelAndSetting 兜底字段）")
    p.add_argument("--description", default="", help="本次草稿的版本变更说明")
    p.add_argument("--yaml", help="YAML 编排字符串（推荐使用 --yaml-file）")
    p.add_argument("--yaml-file",
                   help="从文件读取 YAML 编排（推荐方式）")
    p.add_argument("--draft", help="草稿 JSON 字符串（YAML 失败时的稳定兜底）")
    p.add_argument("--draft-file",
                   help=(
                       "从文件读取 modelAndSetting JSON（YAML 失败时的稳定兜底；"
                       "兜底成功后必须紧跟 get-version --format yaml 拿后端规范 YAML 重新展示）"
                   ))
    p.add_argument("--skip-lint", action="store_true",
                   help="跳过 pipeline_yaml_lint schema 预校验（仅排障用）")

    # release-version
    p = subparsers.add_parser("release-version", parents=[common_parser],
                              help="将草稿发布为正式版本")
    p.add_argument("--project-id", required=True, help="项目编码")
    p.add_argument("--pipeline-id", required=True, help="流水线 ID（p-开头）")
    p.add_argument("--version", required=True, type=int, help="草稿版本号（save-draft 返回）")
    p.add_argument("--description", default="", help="版本描述")

    # get-version
    p = subparsers.add_parser("get-version", parents=[common_parser],
                              help="获取流水线指定版本的编排（YAML/JSON/摘要）")
    p.add_argument("--project-id", required=True, help="项目编码")
    p.add_argument("--pipeline-id", required=True, help="流水线 ID（p-开头）")
    p.add_argument("--version", required=True, type=int,
                   help="流水线编排版本号（来自 pipelines 列表的 version/pipelineVersion 字段）")
    p.add_argument(
        "--format", choices=["yaml", "json", "summary", "full"], default="yaml",
        help=(
            "输出格式：yaml=YAML 原文（默认）；json=modelAndSetting JSON；"
            "summary=骨架摘要；full=API 原始数据"
        ),
    )

    args = parser.parse_args()

    cmd_map = {
        "list-atoms": cmd_list_atoms,
        "atom-yaml": cmd_atom_yaml,
        "atom-detail": cmd_atom_detail,
        "create-pipeline": cmd_create_pipeline,
        "save-draft": cmd_save_draft,
        "release-version": cmd_release_version,
        "get-version": cmd_get_version,
    }
    cmd_map[args.command](args)


if __name__ == "__main__":
    main()
