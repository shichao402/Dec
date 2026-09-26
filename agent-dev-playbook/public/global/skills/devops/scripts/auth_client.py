#!/usr/bin/env python3
"""
蓝盾权限成员治理 Python 客户端。

用于管理员侧权限治理，包括：
- 查询用户组、成员、资源和管理员
- 做权限分析、对比、诊断和健康检查
- 推荐用户组、添加成员、续期、移除、交接、移出项目
- 校验成员身份和资源动作权限
- 查询与重置流水线代持人

使用方法:
    python auth_client.py <command> [options]

身份验证由 scripts/auth.py 统一处理。
"""

import argparse
import json
import sys
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Dict, List, Optional

import yaml

from auth import get_access_token, get_user_id


BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user"
DEFAULT_TIMEOUT = 30


def print_data(data: Any) -> None:
    print(yaml.dump(data, allow_unicode=True, sort_keys=False))


def error_exit(message: str) -> None:
    print(message, file=sys.stderr)
    sys.exit(1)


def parse_bool(value: str) -> bool:
    lowered = value.lower()
    if lowered in ("true", "1", "yes", "y"):
        return True
    if lowered in ("false", "0", "no", "n"):
        return False
    raise argparse.ArgumentTypeError(f"无效布尔值: {value}")


def parse_csv(value: Optional[str]) -> List[str]:
    if not value:
        return []
    return [item.strip() for item in value.split(",") if item.strip()]


def parse_int_csv(value: Optional[str]) -> List[int]:
    if not value:
        return []
    try:
        return [int(item.strip()) for item in value.split(",") if item.strip()]
    except ValueError as exc:
        error_exit(f"整数列表解析失败: {exc}")
        raise exc


def sanitize_params(params: Dict[str, Any]) -> Dict[str, Any]:
    sanitized: Dict[str, Any] = {}
    for key, value in params.items():
        if value is None:
            continue
        if isinstance(value, bool):
            sanitized[key] = str(value).lower()
        else:
            sanitized[key] = value
    return sanitized


def build_url(path: str, params: Optional[Dict[str, Any]] = None) -> str:
    if not params:
        return path
    qs = urllib.parse.urlencode(sanitize_params(params), doseq=True)
    return f"{path}?{qs}" if qs else path


def make_request(
    url: str,
    method: str = "GET",
    data: Any = None,
    access_token: Optional[str] = None,
    user_id: Optional[str] = None,
) -> Dict[str, Any]:
    headers = {
        "Content-Type": "application/json",
        "Accept": "application/json",
    }

    if access_token:
        auth = json.dumps({"access_token": access_token}, separators=(",", ":"))
        headers["X-Bkapi-Authorization"] = auth
    if user_id:
        headers["X-DEVOPS-UID"] = user_id

    req_data = None
    if data is not None and method in ("POST", "PUT", "DELETE"):
        req_data = json.dumps(data).encode("utf-8")

    request = urllib.request.Request(
        url,
        data=req_data,
        headers=headers,
        method=method,
    )

    try:
        with urllib.request.urlopen(request, timeout=DEFAULT_TIMEOUT) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        error_body = exc.read().decode("utf-8")
        error_exit(f"HTTP Error {exc.code}: {error_body}")
    except urllib.error.URLError as exc:
        error_exit(f"URL Error: {exc.reason}")
    except json.JSONDecodeError as exc:
        error_exit(f"JSON Decode Error: {exc}")

    return {}


def request(
    args: argparse.Namespace,
    path: str,
    method: str = "GET",
    params: Optional[Dict[str, Any]] = None,
    data: Any = None,
    user_id: Optional[str] = None,
) -> Dict[str, Any]:
    token = get_access_token(args.access_token)
    actual_user_id = user_id or get_user_id(args.user_id)
    url = build_url(f"{BASE_URL}{path}", params)
    return make_request(
        url,
        method=method,
        data=data,
        access_token=token,
        user_id=actual_user_id,
    )


def cmd_list_groups(args: argparse.Namespace) -> None:
    condition = {
        "projectCode": args.project_id,
        "groupName": args.group_name,
        "iamGroupIds": parse_int_csv(args.iam_group_ids) or None,
        "relatedResourceType": args.resource_type,
        "relatedResourceCode": args.resource_code,
        "action": args.action,
        "uniqueManagerGroupsQueryFlag": args.unique_manager_groups,
        "page": args.page,
        "pageSize": args.page_size,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/list_groups",
        method="POST",
        data=condition,
    )
    print_data(result)


def cmd_get_group_permissions(args: argparse.Namespace) -> None:
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/groups/{args.group_id}/permission_detail",
    )
    print_data(result)


def cmd_list_group_members(args: argparse.Namespace) -> None:
    params = {
        "resourceType": args.resource_type,
        "resourceCode": args.resource_code,
        "iamGroupId": args.iam_group_id,
        "groupCode": args.group_code,
        "memberId": args.member_id,
        "memberType": args.member_type,
        "minExpiredAt": args.min_expired_at,
        "maxExpiredAt": args.max_expired_at,
        "page": args.page,
        "pageSize": args.page_size,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/list_group_members",
        params=params,
    )
    print_data(result)


def cmd_list_project_members(args: argparse.Namespace) -> None:
    params = {
        "memberType": args.member_type,
        "userName": args.user_name,
        "departed": args.departed,
        "page": args.page,
        "pageSize": args.page_size,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/list_project_members",
        params=params,
    )
    print_data(result)


def cmd_list_member_groups(args: argparse.Namespace) -> None:
    params = {
        "memberId": args.member_id,
        "resourceType": args.resource_type,
        "iamGroupIds": args.iam_group_ids,
        "groupName": args.group_name,
        "minExpiredAt": args.min_expired_at,
        "maxExpiredAt": args.max_expired_at,
        "relatedResourceType": args.related_resource_type,
        "relatedResourceCode": args.related_resource_code,
        "action": args.action,
        "page": args.page,
        "pageSize": args.page_size,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/members/all_groups",
        params=params,
    )
    print_data(result)


def cmd_search_users(args: argparse.Namespace) -> None:
    params = {
        "keyword": args.keyword,
        "projectId": args.project_id,
        "limit": args.limit,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/users/search",
        params=params,
    )
    print_data(result)

def cmd_list_resource_types(args: argparse.Namespace) -> None:
    result = request(args, path="/auth/metadata/list_resource_types")
    print_data(result)


def cmd_list_actions(args: argparse.Namespace) -> None:
    result = request(
        args,
        path="/auth/metadata/list_actions",
        params={"resourceType": args.resource_type},
    )
    print_data(result)


def cmd_search_resource(args: argparse.Namespace) -> None:
    params = {
        "resourceType": args.resource_type,
        "keyword": args.keyword,
    }
    result = request(
        args,
        path=f"/auth/metadata/projects/{args.project_id}/search_resource",
        params=params,
    )
    print_data(result)


def cmd_get_resource_by_name(args: argparse.Namespace) -> None:
    params = {
        "resourceType": args.resource_type,
        "resourceName": args.resource_name,
    }
    result = request(
        args,
        path=f"/auth/metadata/projects/{args.project_id}/get_resource_by_name",
        params=params,
    )
    print_data(result)


def cmd_get_resource_by_code(args: argparse.Namespace) -> None:
    params = {
        "resourceType": args.resource_type,
        "resourceCode": args.resource_code,
    }
    result = request(
        args,
        path=f"/auth/metadata/projects/{args.project_id}/get_resource_by_code",
        params=params,
    )
    print_data(result)


def cmd_analyze_member(args: argparse.Namespace) -> None:
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_insight/members/{args.member_id}/analysis",
    )
    print_data(result)


def cmd_permissions_matrix(args: argparse.Namespace) -> None:
    result = request(
        args,
        path=(
            f"/auth/project/{args.project_id}/member_insight/resources/"
            f"{args.resource_type}/{args.resource_code}/permissions_matrix"
        ),
    )
    print_data(result)


def cmd_diagnose(args: argparse.Namespace) -> None:
    params = {
        "memberId": args.member_id,
        "resourceType": args.resource_type,
        "resourceCode": args.resource_code,
        "action": args.action,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_insight/diagnose",
        params=params,
    )
    print_data(result)


def cmd_compare(args: argparse.Namespace) -> None:
    params = {
        "userIdA": args.user_id_a,
        "userIdB": args.user_id_b,
        "resourceType": args.resource_type,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_insight/compare",
        params=params,
    )
    print_data(result)


def cmd_health_check(args: argparse.Namespace) -> None:
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_insight/authorization/health_check",
    )
    print_data(result)


def cmd_recommend_groups(args: argparse.Namespace) -> None:
    body = {
        "resourceType": args.resource_type,
        "resourceCode": args.resource_code,
        "action": args.action,
        "targetUserId": args.target_user_id,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/recommend_groups",
        method="POST",
        data=body,
    )
    print_data(result)


def cmd_add_members(args: argparse.Namespace) -> None:
    user_ids = parse_csv(args.target_user_ids)
    dept_ids = parse_csv(args.dept_ids)
    if not user_ids and not dept_ids:
        error_exit("错误：至少需要提供 --target-user-ids 或 --dept-ids")

    body = {
        "createUserId": get_user_id(args.user_id),
        "roleName": None,
        "roleId": None,
        "groupId": args.group_id,
        "userIds": user_ids,
        "deptIds": dept_ids,
        "resourceType": None,
        "resourceCode": None,
        "expiredTime": args.expired_days,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/members/add",
        method="POST",
        data=body,
    )
    print_data(result)


def cmd_renewal(args: argparse.Namespace) -> None:
    body = {
        "groupIds": parse_int_csv(args.group_ids),
        "targetMemberId": args.target_member_id,
        "renewalDuration": args.renewal_days,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/members/batch_renewal",
        method="PUT",
        data=body,
    )
    print_data(result)


def cmd_operate_check(args: argparse.Namespace) -> None:
    body = {
        "groupIds": parse_int_csv(args.group_ids),
        "targetMemberId": args.target_member_id,
    }
    result = request(
        args,
        path=(
            f"/auth/project/{args.project_id}/member_manage/members/"
            f"{args.operate_type}/check"
        ),
        method="POST",
        data=body,
    )
    print_data(result)


def cmd_exit_check(args: argparse.Namespace) -> None:
    params = {
        "handoverTo": args.handover_to,
        "groupIds": args.group_ids,
        "recommendLimit": args.recommend_limit,
    }
    result = request(
        args,
        path=(
            f"/auth/project/{args.project_id}/member_manage/members/"
            f"{args.target_member_id}/exit_check"
        ),
        params=params,
    )
    print_data(result)


def cmd_remove(args: argparse.Namespace) -> None:
    body = {
        "groupIds": parse_int_csv(args.group_ids),
        "targetMemberId": args.target_member_id,
        "handoverToMemberId": None,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/members/batch_remove",
        method="DELETE",
        data=body,
    )
    print_data(result)


def cmd_handover(args: argparse.Namespace) -> None:
    body = {
        "groupIds": parse_int_csv(args.group_ids),
        "targetMemberId": args.target_member_id,
        "handoverToMemberId": args.handover_to,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/members/batch_handover",
        method="PUT",
        data=body,
    )
    print_data(result)


def cmd_remove_from_project_check(args: argparse.Namespace) -> None:
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/members/batch_remove_from_project_check",
        method="POST",
        data=parse_csv(args.target_member_ids),
    )
    print_data(result)


def cmd_remove_from_project(args: argparse.Namespace) -> None:
    body = {
        "targetMemberIds": parse_csv(args.target_member_ids),
        "handoverToMemberId": args.handover_to,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/members/batch_remove_from_project",
        method="PUT",
        data=body,
    )
    print_data(result)


def cmd_clone_permissions(args: argparse.Namespace) -> None:
    params = {
        "sourceUserId": args.source_user_id,
        "targetUserId": args.target_user_id,
        "resourceTypes": args.resource_types,
        "dryRun": args.dry_run,
    }
    result = request(
        args,
        path=f"/auth/project/{args.project_id}/member_manage/permissions/clone",
        method="POST",
        params=params,
    )
    print_data(result)


def cmd_check_project_user(args: argparse.Namespace) -> None:
    params = {"group": args.group}
    result = request(
        args,
        path=f"/auth/validate/projects/{args.project_id}/check_project_users",
        params=params,
        user_id=args.target_user_id,
    )
    print_data(result)

def cmd_get_resource_authorization(args: argparse.Namespace) -> None:
    result = request(
        args,
        path=(
            f"/auth/authorization/{args.project_id}/{args.resource_type}/"
            f"{args.resource_code}/get_resource_authorization"
        ),
    )
    print_data(result)


def cmd_list_pipeline_authorization(args: argparse.Namespace) -> None:
    params = {
        "pipelineName": args.pipeline_name,
        "handoverFrom": args.handover_from,
        "page": args.page,
        "pageSize": args.page_size,
    }
    result = request(
        args,
        path=f"/auth/authorization/{args.project_id}/pipelines/list_authorization",
        params=params,
    )
    print_data(result)


def cmd_reset_pipeline_authorization(args: argparse.Namespace) -> None:
    body = {"handoverTo": args.handover_to}
    result = request(
        args,
        path=(
            f"/auth/authorization/{args.project_id}/pipelines/"
            f"{args.pipeline_id}/reset_authorization"
        ),
        method="PUT",
        data=body,
    )
    print_data(result)


def add_common_args(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "--access-token",
        help="可选；本轮对话提供的访问令牌",
    )
    parser.add_argument(
        "--user-id",
        help="可选；本轮对话提供的操作用户 ID",
    )


def add_project_arg(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--project-id", required=True, help="项目英文名")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="auth_client.py",
        description="蓝盾权限成员治理 Python 客户端",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  # 查询成员全部用户组
  python auth_client.py list-member-groups --project-id demo --member-id alice

  # 推荐用户组
  python auth_client.py recommend-groups --project-id demo \\
      --target-user-id alice --resource-type pipeline \\
      --resource-code p-123 --action pipeline_execute

  # 预演权限克隆
  python auth_client.py clone-permissions --project-id demo \\
      --source-user-id alice --target-user-id bob --dry-run true

鉴权:
  从 bkToken/get 获取 token 后，交给 AI 配置或自行填写 Skill config.json
        """,
    )
    common = argparse.ArgumentParser(add_help=False)
    add_common_args(common)
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("list-groups", parents=[common], help="查询项目用户组")
    add_project_arg(p)
    p.add_argument("--iam-group-ids", help="IAM 用户组 ID 列表，逗号分隔")
    p.add_argument("--resource-type", help="关联资源类型")
    p.add_argument("--resource-code", help="关联资源 Code")
    p.add_argument("--group-name", help="用户组名称模糊匹配")
    p.add_argument("--action", help="按动作过滤")
    p.add_argument("--unique-manager-groups", type=parse_bool, help="是否只查唯一管理员组")
    p.add_argument("--page", type=int, help="页码")
    p.add_argument("--page-size", type=int, help="每页条数")
    p.set_defaults(func=cmd_list_groups)

    p = sub.add_parser("get-group-permissions", parents=[common], help="查询用户组权限详情")
    add_project_arg(p)
    p.add_argument("--group-id", type=int, required=True, help="用户组 ID / relationId")
    p.set_defaults(func=cmd_get_group_permissions)

    p = sub.add_parser("list-group-members", parents=[common], help="查询用户组成员详情")
    add_project_arg(p)
    p.add_argument("--resource-type", help="资源类型")
    p.add_argument("--resource-code", help="资源 Code")
    p.add_argument("--iam-group-id", type=int, help="IAM 用户组 ID")
    p.add_argument("--group-code", help="用户组 code")
    p.add_argument("--member-id", help="成员 ID")
    p.add_argument("--member-type", help="成员类型")
    p.add_argument("--min-expired-at", type=int, help="过期时间下限，毫秒时间戳")
    p.add_argument("--max-expired-at", type=int, help="过期时间上限，毫秒时间戳")
    p.add_argument("--page", type=int, default=1, help="页码")
    p.add_argument("--page-size", type=int, default=20, help="每页条数")
    p.set_defaults(func=cmd_list_group_members)

    p = sub.add_parser("list-project-members", parents=[common], help="查询项目成员")
    add_project_arg(p)
    p.add_argument("--member-type", help="成员类型")
    p.add_argument("--user-name", help="用户名模糊匹配")
    p.add_argument("--departed", type=parse_bool, help="是否只查离职成员")
    p.add_argument("--page", type=int, default=1, help="页码")
    p.add_argument("--page-size", type=int, default=20, help="每页条数")
    p.set_defaults(func=cmd_list_project_members)

    p = sub.add_parser("list-member-groups", parents=[common], help="查询成员全部用户组")
    add_project_arg(p)
    p.add_argument("--member-id", required=True, help="目标成员 ID")
    p.add_argument("--resource-type", help="资源类型")
    p.add_argument("--iam-group-ids", help="IAM 用户组 ID 列表，逗号分隔")
    p.add_argument("--group-name", help="用户组名称")
    p.add_argument("--min-expired-at", type=int, help="过期时间下限，毫秒时间戳")
    p.add_argument("--max-expired-at", type=int, help="过期时间上限，毫秒时间戳")
    p.add_argument("--related-resource-type", help="关联资源类型")
    p.add_argument("--related-resource-code", help="关联资源 Code")
    p.add_argument("--action", help="动作过滤")
    p.add_argument("--page", type=int, default=1, help="页码")
    p.add_argument("--page-size", type=int, default=500, help="每页条数")
    p.set_defaults(func=cmd_list_member_groups)

    p = sub.add_parser("search-users", parents=[common], help="搜索用户")
    add_project_arg(p)
    p.add_argument("--keyword", required=True, help="搜索关键词")
    p.add_argument("--limit", type=int, default=10, help="返回上限")
    p.set_defaults(func=cmd_search_users)

    p = sub.add_parser("list-resource-types", parents=[common], help="获取资源类型列表")
    p.set_defaults(func=cmd_list_resource_types)

    p = sub.add_parser("list-actions", parents=[common], help="获取资源动作列表")
    p.add_argument("--resource-type", required=True, help="资源类型")
    p.set_defaults(func=cmd_list_actions)

    p = sub.add_parser("search-resource", parents=[common], help="搜索资源")
    add_project_arg(p)
    p.add_argument("--resource-type", required=True, help="资源类型")
    p.add_argument("--keyword", required=True, help="搜索关键词")
    p.set_defaults(func=cmd_search_resource)

    p = sub.add_parser("get-resource-by-name", parents=[common], help="按名称查询资源")
    add_project_arg(p)
    p.add_argument("--resource-type", required=True, help="资源类型")
    p.add_argument("--resource-name", required=True, help="资源名称")
    p.set_defaults(func=cmd_get_resource_by_name)

    p = sub.add_parser("get-resource-by-code", parents=[common], help="按 Code 查询资源")
    add_project_arg(p)
    p.add_argument("--resource-type", required=True, help="资源类型")
    p.add_argument("--resource-code", required=True, help="资源 Code")
    p.set_defaults(func=cmd_get_resource_by_code)

    p = sub.add_parser("analyze-member", parents=[common], help="成员权限分析报告")
    add_project_arg(p)
    p.add_argument("--member-id", required=True, help="目标成员 ID")
    p.set_defaults(func=cmd_analyze_member)

    p = sub.add_parser("permissions-matrix", parents=[common], help="资源权限矩阵")
    add_project_arg(p)
    p.add_argument("--resource-type", required=True, help="资源类型")
    p.add_argument("--resource-code", required=True, help="资源 Code")
    p.set_defaults(func=cmd_permissions_matrix)

    p = sub.add_parser("diagnose", parents=[common], help="权限诊断")
    add_project_arg(p)
    p.add_argument("--member-id", required=True, help="目标成员 ID")
    p.add_argument("--resource-type", required=True, help="资源类型")
    p.add_argument("--resource-code", required=True, help="资源 Code")
    p.add_argument("--action", required=True, help="动作名")
    p.set_defaults(func=cmd_diagnose)

    p = sub.add_parser("compare", parents=[common], help="权限对比")
    add_project_arg(p)
    p.add_argument("--user-id-a", required=True, help="用户 A")
    p.add_argument("--user-id-b", required=True, help="用户 B")
    p.add_argument("--resource-type", help="限定资源类型")
    p.set_defaults(func=cmd_compare)

    p = sub.add_parser("health-check", parents=[common], help="项目授权健康检查")
    add_project_arg(p)
    p.set_defaults(func=cmd_health_check)

    p = sub.add_parser("recommend-groups", parents=[common], help="智能推荐用户组")
    add_project_arg(p)
    p.add_argument("--target-user-id", required=True, help="目标成员 ID")
    p.add_argument("--resource-type", required=True, help="资源类型")
    p.add_argument("--resource-code", required=True, help="资源 Code")
    p.add_argument("--action", required=True, help="动作名")
    p.set_defaults(func=cmd_recommend_groups)

    p = sub.add_parser("add-members", parents=[common], help="批量添加用户组成员")
    add_project_arg(p)
    p.add_argument("--group-id", type=int, required=True, help="用户组 ID / relationId")
    p.add_argument("--target-user-ids", help="目标用户 ID 列表，逗号分隔")
    p.add_argument("--dept-ids", help="目标部门 ID 列表，逗号分隔")
    p.add_argument("--expired-days", type=int, default=365, help="有效期天数")
    p.set_defaults(func=cmd_add_members)

    p = sub.add_parser("renewal", parents=[common], help="批量续期权限")
    add_project_arg(p)
    p.add_argument("--group-ids", required=True, help="用户组 ID 列表，逗号分隔")
    p.add_argument("--target-member-id", required=True, help="目标成员 ID")
    p.add_argument("--renewal-days", type=int, required=True, help="续期天数")
    p.set_defaults(func=cmd_renewal)

    p = sub.add_parser("operate-check", parents=[common], help="批量操作预检查")
    add_project_arg(p)
    p.add_argument(
        "--operate-type",
        required=True,
        choices=["RENEWAL", "REMOVE", "HANDOVER"],
        help="批量操作类型",
    )
    p.add_argument("--group-ids", required=True, help="用户组 ID 列表，逗号分隔")
    p.add_argument("--target-member-id", required=True, help="目标成员 ID")
    p.set_defaults(func=cmd_operate_check)

    p = sub.add_parser("exit-check", parents=[common], help="退出/交接综合预检查")
    add_project_arg(p)
    p.add_argument("--target-member-id", required=True, help="目标成员 ID")
    p.add_argument("--handover-to", help="指定接收人 ID")
    p.add_argument("--group-ids", help="用户组 ID 列表，逗号分隔；不传表示整个项目")
    p.add_argument("--recommend-limit", type=int, default=5, help="推荐交接人数上限")
    p.set_defaults(func=cmd_exit_check)

    p = sub.add_parser("remove", parents=[common], help="批量移除成员权限")
    add_project_arg(p)
    p.add_argument("--group-ids", required=True, help="用户组 ID 列表，逗号分隔")
    p.add_argument("--target-member-id", required=True, help="目标成员 ID")
    p.set_defaults(func=cmd_remove)

    p = sub.add_parser("handover", parents=[common], help="批量交接成员权限")
    add_project_arg(p)
    p.add_argument("--group-ids", required=True, help="用户组 ID 列表，逗号分隔")
    p.add_argument("--target-member-id", required=True, help="目标成员 ID")
    p.add_argument("--handover-to", required=True, help="接收人 ID")
    p.set_defaults(func=cmd_handover)

    p = sub.add_parser(
        "remove-from-project-check",
        parents=[common],
        help="批量移出项目检查",
    )
    add_project_arg(p)
    p.add_argument("--target-member-ids", required=True, help="成员 ID 列表，逗号分隔")
    p.set_defaults(func=cmd_remove_from_project_check)

    p = sub.add_parser("remove-from-project", parents=[common], help="批量移出项目")
    add_project_arg(p)
    p.add_argument("--target-member-ids", required=True, help="成员 ID 列表，逗号分隔")
    p.add_argument("--handover-to", help="接收人 ID")
    p.set_defaults(func=cmd_remove_from_project)

    p = sub.add_parser("clone-permissions", parents=[common], help="权限克隆")
    add_project_arg(p)
    p.add_argument("--source-user-id", required=True, help="来源用户 ID")
    p.add_argument("--target-user-id", required=True, help="目标用户 ID")
    p.add_argument("--resource-types", help="限定资源类型列表，逗号分隔")
    p.add_argument("--dry-run", type=parse_bool, default=True, help="是否仅预演")
    p.set_defaults(func=cmd_clone_permissions)

    p = sub.add_parser("check-project-user", parents=[common], help="校验是否为项目成员")
    add_project_arg(p)
    p.add_argument("--target-user-id", required=True, help="目标用户 ID")
    p.add_argument("--group", help="限定项目组角色")
    p.set_defaults(func=cmd_check_project_user)

    p = sub.add_parser("get-resource-authorization", parents=[common], help="查询资源授权记录")
    add_project_arg(p)
    p.add_argument("--resource-type", required=True, help="资源类型")
    p.add_argument("--resource-code", required=True, help="资源 Code")
    p.set_defaults(func=cmd_get_resource_authorization)

    p = sub.add_parser("list-pipeline-authorization", parents=[common], help="查询流水线代持人列表")
    add_project_arg(p)
    p.add_argument("--pipeline-name", help="流水线名称模糊匹配")
    p.add_argument("--handover-from", help="代持人")
    p.add_argument("--page", type=int, default=1, help="页码")
    p.add_argument("--page-size", type=int, default=20, help="每页条数")
    p.set_defaults(func=cmd_list_pipeline_authorization)

    p = sub.add_parser("reset-pipeline-authorization", parents=[common], help="重置流水线授权人")
    add_project_arg(p)
    p.add_argument("--pipeline-id", required=True, help="流水线 ID")
    p.add_argument("--handover-to", required=True, help="新的授权人")
    p.set_defaults(func=cmd_reset_pipeline_authorization)

    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
