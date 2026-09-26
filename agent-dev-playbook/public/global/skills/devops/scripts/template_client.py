#!/usr/bin/env python3
"""
蓝盾流水线模板 Python 客户端

用于管理蓝盾流水线模板操作，包括：
- 查询模板列表 (list)
- 获取模板详情 (get)
- 删除模板 (delete)
- 删除模板版本 (delete-version)
- 从模板创建流水线 (create-pipeline)
- 批量实例化模板 (instantiate)
- 获取模板实例列表 (instances)
- 批量更新模板实例 (update-instances)
- 安装商店模板 (install)

使用方法:
    python template_client.py <command> [options]

身份验证由 scripts/auth.py 统一处理。
"""

import argparse
import json
import sys
from typing import Any, Dict, List, Optional
import urllib.request
import urllib.error
import urllib.parse
import yaml

from auth import get_access_token


def print_data(data: Any) -> None:
    print(yaml.dump(data, allow_unicode=True, sort_keys=False))


BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user"


def make_request(
    url: str,
    method: str = "GET",
    data: Any = None,
    access_token: Optional[str] = None,
) -> Dict[str, Any]:
    headers = {"Content-Type": "application/json"}

    if access_token:
        auth = json.dumps(
            {"access_token": access_token}, separators=(",", ":")
        )
        headers["X-Bkapi-Authorization"] = auth

    req_data = None
    if data is not None and method in ("POST", "PUT"):
        req_data = json.dumps(data).encode("utf-8")

    request = urllib.request.Request(
        url, data=req_data, headers=headers, method=method
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


def build_url(path: str, params: Dict[str, Any]) -> str:
    filtered = {
        k: v for k, v in params.items() if v is not None
    }
    qs = urllib.parse.urlencode(filtered)
    return f"{path}?{qs}" if qs else path


def cmd_list(args: argparse.Namespace) -> None:
    """获取模板列表"""
    token = get_access_token(args.access_token)
    params: Dict[str, Any] = {
        "page": args.page or 1,
        "pageSize": args.page_size or 20,
    }
    if args.template_type:
        params["templateType"] = args.template_type
    if args.store_flag is not None:
        params["storeFlag"] = str(args.store_flag).lower()
    if args.order_by:
        params["orderBy"] = args.order_by
    if args.sort:
        params["sort"] = args.sort

    url = build_url(
        f"{BASE_URL}/projects/{args.project_id}/templates",
        params,
    )
    result = make_request(url, access_token=token)

    if (
        result.get("status") == 0
        and "data" in result
        and "models" in result["data"]
    ):
        result["data"]["models"] = clean_template_data(
            result["data"]["models"]
        )

    print_data(result)


def clean_template_data(models: List[Dict]) -> List[Dict]:
    cleaned = []
    for m in models:
        cleaned.append({
            "templateId": m.get("templateId"),
            "name": m.get("name"),
            "templateType": m.get("templateType"),
            "templateTypeDesc": m.get("templateTypeDesc"),
            "creator": m.get("creator"),
            "updateTime": m.get("updateTime"),
            "version": m.get("version"),
            "versionName": m.get("versionName"),
            "storeFlag": m.get("storeFlag"),
            "hasPermission": m.get("hasPermission"),
            "hasInstance2Upgrade": m.get("hasInstance2Upgrade"),
            "canEdit": m.get("canEdit"),
            "canDelete": m.get("canDelete"),
        })
    return cleaned


def cmd_get(args: argparse.Namespace) -> None:
    """获取模板详情"""
    token = get_access_token(args.access_token)
    params: Dict[str, Any] = {"templateId": args.template_id}
    if args.version is not None:
        params["version"] = args.version

    url = build_url(
        f"{BASE_URL}/projects/{args.project_id}"
        f"/templates/template_detail",
        params,
    )
    result = make_request(url, access_token=token)
    print_data(result)


def cmd_delete(args: argparse.Namespace) -> None:
    """删除模板"""
    token = get_access_token(args.access_token)
    url = build_url(
        f"{BASE_URL}/projects/{args.project_id}/templates",
        {"templateId": args.template_id},
    )
    result = make_request(url, method="DELETE", access_token=token)
    print_data(result)


def cmd_delete_version(args: argparse.Namespace) -> None:
    """删除模板版本"""
    token = get_access_token(args.access_token)
    url = build_url(
        f"{BASE_URL}/projects/{args.project_id}"
        f"/templates/template_version",
        {
            "templateId": args.template_id,
            "version": args.version,
        },
    )
    result = make_request(url, method="DELETE", access_token=token)
    print_data(result)


def cmd_create_pipeline(args: argparse.Namespace) -> None:
    """通过模板创建流水线"""
    token = get_access_token(args.access_token)
    body: Dict[str, Any] = {
        "pipelineName": args.pipeline_name,
        "templateId": args.template_id,
        "templateVersion": args.template_version,
    }
    if args.instance_type:
        body["instanceType"] = args.instance_type
    if args.empty_template is not None:
        body["emptyTemplate"] = args.empty_template

    url = (
        f"{BASE_URL}/projects/{args.project_id}"
        f"/version/create_with_template"
    )
    result = make_request(
        url, method="POST", data=body, access_token=token
    )
    print_data(result)


def cmd_instantiate(args: argparse.Namespace) -> None:
    """批量实例化模板"""
    token = get_access_token(args.access_token)
    params: Dict[str, Any] = {
        "templateId": args.template_id,
        "version": args.version,
    }
    if args.use_template_settings is not None:
        params["useTemplateSettings"] = str(
            args.use_template_settings
        ).lower()

    try:
        instances = json.loads(args.instances)
    except json.JSONDecodeError as e:
        print(f"实例列表解析错误: {e}", file=sys.stderr)
        sys.exit(1)

    url = build_url(
        f"{BASE_URL}/projects/{args.project_id}"
        f"/templates/templateInstances",
        params,
    )
    result = make_request(
        url, method="POST", data=instances, access_token=token
    )
    print_data(result)


def cmd_instances(args: argparse.Namespace) -> None:
    """获取模板实例列表"""
    token = get_access_token(args.access_token)
    params: Dict[str, Any] = {
        "templateId": args.template_id,
    }
    if args.page is not None:
        params["page"] = args.page
    if args.page_size is not None:
        params["pageSize"] = args.page_size
    if args.search_key:
        params["searchKey"] = args.search_key
    if args.sort_type:
        params["sortType"] = args.sort_type
    if args.desc is not None:
        params["desc"] = str(args.desc).lower()

    url = build_url(
        f"{BASE_URL}/projects/{args.project_id}"
        f"/templates/templateInstances",
        params,
    )
    result = make_request(url, access_token=token)
    print_data(result)


def cmd_update_instances(args: argparse.Namespace) -> None:
    """批量更新模板实例"""
    token = get_access_token(args.access_token)
    params: Dict[str, Any] = {
        "templateId": args.template_id,
    }
    if args.use_template_settings is not None:
        params["useTemplateSettings"] = str(
            args.use_template_settings
        ).lower()

    if args.version_name:
        params["versionName"] = args.version_name
        path = (
            f"{BASE_URL}/projects/{args.project_id}"
            f"/templates/templateInstances/update"
        )
    else:
        params["version"] = args.version
        path = (
            f"{BASE_URL}/projects/{args.project_id}"
            f"/templates/templateInstances"
        )

    try:
        instances = json.loads(args.instances)
    except json.JSONDecodeError as e:
        print(f"实例列表解析错误: {e}", file=sys.stderr)
        sys.exit(1)

    url = build_url(path, params)
    result = make_request(
        url, method="PUT", data=instances, access_token=token
    )
    print_data(result)


def cmd_create_template(args: argparse.Namespace) -> None:
    """基于已有模板创建新模板"""
    token = get_access_token(args.access_token)
    src_params: Dict[str, Any] = {
        "templateId": args.source_template_id,
    }
    if args.source_version is not None:
        src_params["version"] = args.source_version

    src_url = build_url(
        f"{BASE_URL}/projects/{args.project_id}"
        f"/templates/template_detail",
        src_params,
    )
    src_result = make_request(src_url, access_token=token)

    if src_result.get("status") != 0 or "data" not in src_result:
        print("获取源模板失败", file=sys.stderr)
        print_data(src_result)
        sys.exit(1)

    model = src_result["data"]["template"]
    model["name"] = args.template_name
    if args.description is not None:
        model["desc"] = args.description

    create_url = (
        f"{BASE_URL}/projects/{args.project_id}/templates/"
    )
    result = make_request(
        create_url, method="POST", data=model, access_token=token
    )
    print_data(result)


def cmd_update_template(args: argparse.Namespace) -> None:
    """更新模板（获取当前模型，应用修改后提交）"""
    token = get_access_token(args.access_token)
    src_params: Dict[str, Any] = {
        "templateId": args.template_id,
    }

    src_url = build_url(
        f"{BASE_URL}/projects/{args.project_id}"
        f"/templates/template_detail",
        src_params,
    )
    src_result = make_request(src_url, access_token=token)

    if src_result.get("status") != 0 or "data" not in src_result:
        print("获取模板详情失败", file=sys.stderr)
        print_data(src_result)
        sys.exit(1)

    model = src_result["data"]["template"]

    if args.add_params:
        params_to_add = json.loads(args.add_params)
        trigger = model.get("stages", [{}])[0] \
            .get("containers", [{}])[0]
        existing = trigger.get("params", [])
        existing.extend(params_to_add)
        trigger["params"] = existing
        tc = model.get("triggerContainer")
        if tc:
            tc_params = tc.get("params", [])
            tc_params.extend(params_to_add)
            tc["params"] = tc_params

    if args.model_json:
        custom = json.loads(args.model_json)
        model.update(custom)

    update_url = build_url(
        f"{BASE_URL}/projects/{args.project_id}/templates/",
        {
            "templateId": args.template_id,
            "versionName": args.version_name,
        },
    )
    result = make_request(
        update_url, method="PUT", data=model, access_token=token
    )
    print_data(result)


def cmd_get_pipeline(args: argparse.Namespace) -> None:
    """获取流水线编排详情"""
    token = get_access_token(args.access_token)
    url = build_url(
        f"{BASE_URL}/projects/{args.project_id}"
        f"/pipelines/pipeline",
        {"pipelineId": args.pipeline_id},
    )
    result = make_request(url, access_token=token)

    if args.params_only and result.get("status") == 0:
        model = result.get("data", {})
        stages = model.get("stages", [])
        params = []
        if stages:
            containers = stages[0].get("containers", [])
            if containers:
                params = containers[0].get("params", [])
        cleaned = []
        for p in params:
            cleaned.append({
                "id": p.get("id"),
                "name": p.get("name"),
                "type": p.get("type"),
                "required": p.get("required"),
                "defaultValue": p.get("defaultValue"),
                "options": p.get("options"),
                "desc": p.get("desc"),
            })
        print_data({"status": 0, "params": cleaned})
    else:
        print_data(result)


def cmd_get_pipeline_startup_info(
    args: argparse.Namespace,
) -> None:
    """获取流水线手动启动参数"""
    token = get_access_token(args.access_token)
    url = build_url(
        f"{BASE_URL}/projects/{args.project_id}"
        f"/build_manual_startup_info",
        {"pipelineId": args.pipeline_id},
    )
    result = make_request(url, access_token=token)
    print_data(result)


def cmd_install(args: argparse.Namespace) -> None:
    """安装研发商店模板到项目"""
    token = get_access_token(args.access_token)

    body = {
        "templateCode": args.template_code,
        "projectCodeList": args.project_codes.split(","),
    }

    endpoint = "template_install_from_store"
    if args.return_id:
        endpoint = "template_install_from_store_new"

    url = f"{BASE_URL.replace('/v4/apigw-user', '')}/v4/apigw-user/market/{endpoint}"
    result = make_request(
        url, method="POST", data=body, access_token=token
    )
    print_data(result)


def main():
    parser = argparse.ArgumentParser(
        prog="template_client.py",
        description="蓝盾流水线模板 Python 客户端",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  # 获取模板列表
  python template_client.py list --project-id myproject

  # 获取模板详情
  python template_client.py get --project-id myproject --template-id xxx

  # 从模板创建流水线
  python template_client.py create-pipeline --project-id myproject \\
      --template-id xxx --template-version 1 --pipeline-name my-pipe

  # 批量实例化模板
  python template_client.py instantiate --project-id myproject \\
      --template-id xxx --version 1 \\
      --instances '[{"pipelineName":"pipe1"},{"pipelineName":"pipe2"}]'

  # 查看模板实例列表
  python template_client.py instances --project-id myproject --template-id xxx

  # 安装商店模板
  python template_client.py install --template-code xxx --project-codes proj1,proj2

鉴权:
  从 bkToken/get 获取 token 后，交给 AI 配置或自行填写 Skill config.json
        """,
    )

    parser.add_argument(
        "--access-token",
        help="可选；本轮对话提供的访问令牌",
    )

    sub = parser.add_subparsers(dest="command", required=True)

    # list
    p = sub.add_parser("list", help="获取模板列表")
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument("--page", type=int, help="页码（默认1）")
    p.add_argument(
        "--page-size", type=int, help="每页条数（默认20，最大100）"
    )
    p.add_argument(
        "--template-type",
        choices=["CUSTOMIZE", "CONSTRAINT", "PUBLIC"],
        help="模板类型",
    )
    p.add_argument(
        "--store-flag",
        type=lambda x: x.lower() == "true",
        help="是否已关联商店（true/false）",
    )
    p.add_argument(
        "--order-by",
        choices=["NAME", "CREATOR", "CREATE_TIME"],
        help="排序字段",
    )
    p.add_argument(
        "--sort", choices=["ASC", "DESC"], help="排序方式"
    )

    # get
    p = sub.add_parser("get", help="获取模板详情")
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--template-id", required=True, help="模板ID"
    )
    p.add_argument(
        "--version", type=int, help="模板版本（默认最新）"
    )

    # delete
    p = sub.add_parser("delete", help="删除模板")
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--template-id", required=True, help="模板ID"
    )

    # delete-version
    p = sub.add_parser(
        "delete-version", help="删除模板版本"
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--template-id", required=True, help="模板ID"
    )
    p.add_argument(
        "--version", type=int, required=True, help="版本号"
    )

    # create-pipeline
    p = sub.add_parser(
        "create-pipeline", help="通过模板创建流水线"
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--template-id", required=True, help="模板ID"
    )
    p.add_argument(
        "--template-version",
        type=int,
        required=True,
        help="模板版本号",
    )
    p.add_argument(
        "--pipeline-name",
        required=True,
        help="流水线名称",
    )
    p.add_argument(
        "--instance-type",
        choices=["FREEDOM", "CONSTRAINT"],
        help="实例模式（自由/约束）",
    )
    p.add_argument(
        "--empty-template",
        type=lambda x: x.lower() == "true",
        help="是否为空模板（true/false）",
    )

    # instantiate
    p = sub.add_parser(
        "instantiate", help="批量实例化模板"
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--template-id", required=True, help="模板ID"
    )
    p.add_argument(
        "--version",
        type=int,
        required=True,
        help="模板版本",
    )
    p.add_argument(
        "--instances",
        required=True,
        help='实例列表JSON，如：'
        '\'[{"pipelineName":"p1"}]\'',
    )
    p.add_argument(
        "--use-template-settings",
        type=lambda x: x.lower() == "true",
        help="是否应用模板设置（true/false）",
    )

    # instances
    p = sub.add_parser(
        "instances", help="获取模板实例列表"
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--template-id", required=True, help="模板ID"
    )
    p.add_argument("--page", type=int, help="页码")
    p.add_argument(
        "--page-size", type=int, help="每页条数（默认20，最大30）"
    )
    p.add_argument("--search-key", help="名字搜索关键字")
    p.add_argument(
        "--sort-type",
        choices=[
            "PIPELINE_NAME",
            "VERSION",
            "UPDATE_TIME",
            "STATUS",
        ],
        help="排序字段",
    )
    p.add_argument(
        "--desc",
        type=lambda x: x.lower() == "true",
        help="是否降序（true/false）",
    )

    # update-instances
    p = sub.add_parser(
        "update-instances", help="批量更新模板实例"
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--template-id", required=True, help="模板ID"
    )
    p.add_argument(
        "--version", type=int, help="目标版本号（与 --version-name 二选一）"
    )
    p.add_argument(
        "--version-name", help="目标版本名称（与 --version 二选一）"
    )
    p.add_argument(
        "--instances",
        required=True,
        help='实例列表JSON，如：'
        '\'[{"pipelineId":"p-xxx"}]\'',
    )
    p.add_argument(
        "--use-template-settings",
        type=lambda x: x.lower() == "true",
        help="是否应用模板设置（true/false）",
    )

    # create-template
    p = sub.add_parser(
        "create-template", help="基于已有模板创建新模板"
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--source-template-id",
        required=True,
        help="源模板ID",
    )
    p.add_argument(
        "--source-version",
        type=int,
        help="源模板版本号（默认最新）",
    )
    p.add_argument(
        "--template-name",
        required=True,
        help="新模板名称",
    )
    p.add_argument(
        "--description",
        help="新模板描述",
    )

    # get-pipeline
    p = sub.add_parser(
        "get-pipeline", help="获取流水线编排详情"
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--pipeline-id", required=True, help="流水线ID"
    )
    p.add_argument(
        "--params-only",
        action="store_true",
        help="仅输出参数列表",
    )

    # get-pipeline-startup-info
    p = sub.add_parser(
        "get-pipeline-startup-info",
        help="获取流水线手动启动参数",
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--pipeline-id", required=True, help="流水线ID"
    )

    # update-template
    p = sub.add_parser(
        "update-template", help="更新模板"
    )
    p.add_argument(
        "--project-id", required=True, help="项目英文名"
    )
    p.add_argument(
        "--template-id", required=True, help="模板ID"
    )
    p.add_argument(
        "--version-name",
        required=True,
        help="新版本名称",
    )
    p.add_argument(
        "--add-params",
        help="要添加的参数列表JSON",
    )
    p.add_argument(
        "--model-json",
        help="自定义模型覆盖JSON",
    )

    # install
    p = sub.add_parser(
        "install", help="安装研发商店模板到项目"
    )
    p.add_argument(
        "--template-code",
        required=True,
        help="商店模板代码",
    )
    p.add_argument(
        "--project-codes",
        required=True,
        help="目标项目列表（逗号分隔）",
    )
    p.add_argument(
        "--return-id",
        action="store_true",
        help="是否返回安装后的模板ID",
    )

    args = parser.parse_args()

    commands = {
        "list": cmd_list,
        "get": cmd_get,
        "delete": cmd_delete,
        "delete-version": cmd_delete_version,
        "create-pipeline": cmd_create_pipeline,
        "instantiate": cmd_instantiate,
        "instances": cmd_instances,
        "update-instances": cmd_update_instances,
        "create-template": cmd_create_template,
        "update-template": cmd_update_template,
        "get-pipeline": cmd_get_pipeline,
        "get-pipeline-startup-info": cmd_get_pipeline_startup_info,
        "install": cmd_install,
    }

    commands[args.command](args)


if __name__ == "__main__":
    main()
