#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
蓝盾代码库 / 凭据客户端（普通流水线）

把工蜂等代码库托管到蓝盾代码库服务，供流水线编排直接引用，避免在构建机上做
交互式 git 登录。

命令：
    scm-config          查看各类代码库支持的授权方式（scmCode / credentialType）
    is-oauth            查询当前用户对某类代码库是否已完成 OAuth 授权
    resolve             ⭐ 按仓库地址/名字查是否已托管，命中就直接拿 hashId 用
    list                列出项目下已托管的代码库
    get                 按 hashId 或别名查代码库详情
    credential-list     列出项目凭据
    credential-create   创建凭据（保存工蜂 token / 账号密码）
    create              关联代码库（凭据方式可全程 API；OAUTH 需先在网页授权）
    oauth-page          打印（或打开）网页授权入口

使用方法：
    python codelib_client.py <command> [options]

认证：统一走 scripts/auth.py（config.json / --access-token / 环境变量）。
网关：与 pipeline_generate_client 一致，走 prod/v4 用户态。
"""

import argparse
import json
import sys
import urllib.error
import urllib.parse
import urllib.request
import webbrowser
from typing import Any, Dict, Optional

import yaml

from auth import get_access_token

BASE_URL = "https://devops.apigw.o.woa.com/prod/v4/apigw-user"
CONSOLE_URL = "https://devops.woa.com/console"

# GET /ms/repository/api/user/repositories/config/ 的静态快照，用于离线提示
SCM_CONFIG = {
    "CODE_GIT": {
        "name": "工蜂 GIT", "hosts": "git.woa.com", "repo_type": "codeGit",
        "credential_types": ["OAUTH", "USERNAME_PASSWORD", "TOKEN_USERNAME_PASSWORD", "TOKEN_SSH_PRIVATEKEY"],
        "pac": True,
    },
    "CODE_TGIT": {
        "name": "工蜂 TGIT", "hosts": "git.tencent.com,git.code.tencent.com", "repo_type": "codeTGit",
        "credential_types": ["TOKEN_USERNAME_PASSWORD", "TOKEN_SSH_PRIVATEKEY"], "pac": False,
    },
    "CODE_SVN": {
        "name": "工蜂 SVN", "hosts": "svn.woa.com", "repo_type": "codeSvn",
        "credential_types": ["TOKEN_USERNAME_PASSWORD", "TOKEN_SSH_PRIVATEKEY"], "pac": False,
    },
    "GITHUB": {
        "name": "GITHUB", "hosts": "github.com", "repo_type": "github",
        "credential_types": ["OAUTH"], "pac": False,
    },
    "CODE_GITLAB": {
        "name": "Gitlab", "hosts": "", "repo_type": "codeGitLab",
        "credential_types": ["ACCESSTOKEN", "TOKEN_SSH_PRIVATEKEY"], "pac": False,
    },
    "CODE_P4": {
        "name": "Perforce", "hosts": "", "repo_type": "codeP4",
        "credential_types": ["USERNAME_PASSWORD"], "pac": False,
    },
}

# credentialType → (authType, v1..v4 的含义)
CREDENTIAL_FIELDS = {
    "USERNAME_PASSWORD": ("HTTPS", ["用户名", "密码"]),
    "TOKEN_USERNAME_PASSWORD": ("HTTPS", ["用户名", "密码", "private token"]),
    "ACCESSTOKEN": ("HTTPS", ["access token"]),
    "SSH_PRIVATEKEY": ("SSH", ["SSH 私钥"]),
    "TOKEN_SSH_PRIVATEKEY": ("SSH", ["SSH 私钥", "private token"]),
    "PASSWORD": ("HTTPS", ["密码"]),
}


def print_data(data: Any) -> None:
    print(yaml.dump(data, allow_unicode=True, sort_keys=False))


def make_request(
    url: str, method: str = "GET", data: Optional[Dict[str, Any]] = None,
    access_token: Optional[str] = None,
) -> Dict[str, Any]:
    headers = {
        "Content-Type": "application/json",
    }
    if access_token:
        headers["X-Bkapi-Authorization"] = json.dumps(
            {"access_token": access_token}, separators=(",", ":")
        )

    body = json.dumps(data).encode("utf-8") if data is not None and method in ("POST", "PUT", "DELETE") else None
    request = urllib.request.Request(url, data=body, headers=headers, method=method)

    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8")
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return {"status": e.code, "message": raw}
    except urllib.error.URLError as e:
        return {"status": -1, "message": f"网络错误：{e.reason}（内网接口需先登录 iOA）"}


def _check(result: Dict[str, Any]) -> None:
    if result.get("status") not in (0, None):
        print_data(result)
        sys.exit(1)


# ==================== 命令 ====================

def cmd_scm_config(args: argparse.Namespace) -> None:
    rows = []
    for scm_code, cfg in SCM_CONFIG.items():
        rows.append({
            "scmCode": scm_code,
            "名称": cfg["name"],
            "域名": cfg["hosts"] or "-",
            "@type": cfg["repo_type"],
            "支持的授权方式": cfg["credential_types"],
            "支持PAC": cfg["pac"],
        })
    print_data(rows)
    print(
        "说明：OAUTH 需要在网页完成一次授权（codelib_client.py oauth-page）；\n"
        "其余凭据类型可以用 credential-create + create 全程 API 完成。",
        file=sys.stderr,
    )


def cmd_is_oauth(args: argparse.Namespace) -> None:
    access_token = get_access_token(args.access_token)
    url = f"{BASE_URL}/repositories/oauth/isOauth?scmCode={urllib.parse.quote(args.scm_code)}"
    result = make_request(url, access_token=access_token)
    _check(result)
    authorized = bool(result.get("data"))
    print_data({"scmCode": args.scm_code, "已授权": authorized})
    if not authorized:
        print(
            f"未授权。可用 `codelib_client.py oauth-page --project-id <projectId>` 打开授权页，\n"
            f"或改用凭据方式（credential-create + create --auth-type HTTPS）避免页面操作。",
            file=sys.stderr,
        )


def normalize_git_url(value: str) -> str:
    """把仓库地址归一到 host/group/repo，抹平协议、.git 后缀、端口与登录名差异。"""
    text = (value or "").strip().lower()
    for prefix in ("https://", "http://", "ssh://", "git://"):
        if text.startswith(prefix):
            text = text[len(prefix):]
            break
    if text.startswith("git@"):
        text = text[4:].replace(":", "/", 1)
    elif "@" in text.split("/", 1)[0]:
        text = text.split("@", 1)[1]
    text = text.split("/", 1)
    host = text[0].split(":")[0]
    path = (text[1] if len(text) > 1 else "").strip("/")
    if path.endswith(".git"):
        path = path[:-4]
    return f"{host}/{path}" if path else host


def repo_path(value: str) -> str:
    """只取 group/repo 部分，用于跨域名/跨协议比对。"""
    normalized = normalize_git_url(value)
    return normalized.split("/", 1)[1] if "/" in normalized else normalized


def fetch_repositories(
    project_id: str, access_token: str, repository_type: Optional[str] = None
) -> list:
    """拉全项目已托管代码库（自动翻页）。"""
    records: list = []
    page, page_size = 1, 100
    while True:
        params = {"page": page, "pageSize": page_size}
        if repository_type:
            params["repositoryType"] = repository_type
        url = (
            f"{BASE_URL}/repositories/projects/{project_id}/repository_info_list"
            f"?{urllib.parse.urlencode(params)}"
        )
        result = make_request(url, access_token=access_token)
        _check(result)

        data = result.get("data") or {}
        batch = data.get("records") or []
        records.extend(batch)
        total = data.get("count") or 0
        if len(batch) < page_size or len(records) >= total or page >= 20:
            break
        page += 1
    return records


def _target_parts(target: str) -> tuple:
    """区分「完整地址」和「group/repo 简写」，返回 (归一地址 或 None, group/repo)。"""
    text = (target or "").strip().lower().strip("/")
    first = text.split("/", 1)[0]
    looks_like_url = "://" in text or text.startswith("git@") or "." in first
    if looks_like_url:
        return normalize_git_url(text), repo_path(text)
    if text.endswith(".git"):
        text = text[:-4]
    return None, text


def match_repositories(records: list, target: str) -> tuple:
    """返回 (精确命中列表, 疑似候选列表)。"""
    wanted_url, wanted_path = _target_parts(target)
    keyword = (target or "").strip().lower()

    exact, candidates = [], []
    for record in records:
        url = record.get("url") or ""
        alias = (record.get("aliasName") or "").lower()
        hit_url = wanted_url is not None and normalize_git_url(url) == wanted_url
        hit_path = bool(wanted_path) and "/" in wanted_path and repo_path(url) == wanted_path
        if hit_url or hit_path:
            exact.append(record)
        elif keyword and (keyword in url.lower() or keyword in alias):
            candidates.append(record)
    return exact, candidates


def _brief(record: Dict[str, Any]) -> Dict[str, Any]:
    return {
        "别名": record.get("aliasName"),
        "地址": record.get("url"),
        "类型": record.get("type"),
        "hashId": record.get("repositoryHashId"),
        "创建人": record.get("createUser"),
    }


def cmd_resolve(args: argparse.Namespace) -> None:
    access_token = get_access_token(args.access_token)
    records = fetch_repositories(args.project_id, access_token, args.repository_type)
    exact, candidates = match_repositories(records, args.repo)

    if exact:
        best = exact[0]
        hash_id = best.get("repositoryHashId")
        print_data({
            "已托管": True,
            "匹配到": _brief(best),
            "直接用": {
                "checkout": best.get("url"),
                "git-ref 变量的 repo-id": hash_id,
            },
            "其它同名匹配": [_brief(r) for r in exact[1:]] or None,
        })
        print(
            "该仓库已经在蓝盾代码库服务里，直接在编排中引用即可，**不要再走关联流程**。",
            file=sys.stderr,
        )
        return

    payload: Dict[str, Any] = {"已托管": False, "查询目标": args.repo}
    if candidates:
        payload["疑似候选（需用户确认是不是同一个）"] = [_brief(r) for r in candidates]
    payload["下一步"] = [
        "先和用户确认候选是否就是他要的仓库（有候选时）",
        "确实没有 → 让用户在「托管到蓝盾代码库」和「构建机本地 git 登录」之间选",
        "选托管 → is-oauth 看是否已授权：已授权直接 create --auth-type OAUTH；"
        "未授权优先 credential-create + create --auth-type HTTPS（纯 API，无需开网页）",
    ]
    print_data(payload)
    sys.exit(3)


def cmd_list(args: argparse.Namespace) -> None:
    access_token = get_access_token(args.access_token)
    records = fetch_repositories(args.project_id, access_token, args.repository_type)
    if not records:
        print("该项目下还没有托管任何代码库。")
        return
    print_data([_brief(r) for r in records])


def cmd_get(args: argparse.Namespace) -> None:
    access_token = get_access_token(args.access_token)
    query = urllib.parse.urlencode({
        "repositoryId": args.repository_id,
        "repositoryType": args.repository_type,
    })
    url = f"{BASE_URL}/repositories/projects/{args.project_id}/repository?{query}"
    result = make_request(url, access_token=access_token)
    _check(result)
    print_data(result.get("data"))


def cmd_credential_list(args: argparse.Namespace) -> None:
    access_token = get_access_token(args.access_token)
    query = urllib.parse.urlencode({
        "credentialTypes": args.credential_types,
        "keyword": args.keyword or "",
        "page": args.page,
        "pageSize": args.page_size,
    })
    url = f"{BASE_URL}/projects/{args.project_id}/credentials/credential_list?{query}"
    result = make_request(url, access_token=access_token)
    _check(result)
    records = (result.get("data") or {}).get("records") or []
    print_data([{
        "credentialId": r.get("credentialId"),
        "名称": r.get("credentialName"),
        "类型": r.get("credentialType"),
        "可用": (r.get("permissions") or {}).get("use"),
    } for r in records])


def cmd_credential_create(args: argparse.Namespace) -> None:
    access_token = get_access_token(args.access_token)

    if args.credential_type not in CREDENTIAL_FIELDS:
        print(
            f"错误：不支持的 credentialType {args.credential_type!r}。可选："
            f"{', '.join(CREDENTIAL_FIELDS)}",
            file=sys.stderr,
        )
        sys.exit(1)

    values = [args.v1, args.v2, args.v3, args.v4]
    expected = CREDENTIAL_FIELDS[args.credential_type][1]
    for idx, meaning in enumerate(expected):
        if not values[idx]:
            print(
                f"错误：{args.credential_type} 需要 --v{idx + 1}（{meaning}）。",
                file=sys.stderr,
            )
            sys.exit(1)

    body = {
        "credentialId": args.credential_id,
        "credentialName": args.credential_name or args.credential_id,
        "credentialRemark": args.remark or "",
        "credentialType": args.credential_type,
        "v1": args.v1 or "",
        "v2": args.v2 or "",
        "v3": args.v3 or "",
        "v4": args.v4 or "",
    }
    url = f"{BASE_URL}/projects/{args.project_id}/credentials/credential"
    result = make_request(url, method="POST", data=body, access_token=access_token)
    _check(result)
    print_data({
        "已创建凭据": args.credential_id,
        "类型": args.credential_type,
        "引用方式": f"${{{{settings.{args.credential_id}.password}}}}",
        "凭据管理页": f"{CONSOLE_URL}/ticket/{args.project_id}/createCredential",
    })


def cmd_create(args: argparse.Namespace) -> None:
    access_token = get_access_token(args.access_token)

    scm = SCM_CONFIG.get(args.scm_code)
    if not scm:
        print(f"错误：未知 scmCode {args.scm_code!r}，可选：{', '.join(SCM_CONFIG)}", file=sys.stderr)
        sys.exit(1)

    if not args.force:
        exact, _ = match_repositories(
            fetch_repositories(args.project_id, access_token), args.url
        )
        if exact:
            print_data({"已托管，跳过关联": _brief(exact[0])})
            print(
                "该仓库此前已关联到本项目，直接用上面的 hashId 引用即可。\n"
                "确实要再建一条（比如换授权方式）请加 --force。",
                file=sys.stderr,
            )
            return

    if args.auth_type == "OAUTH":
        check = make_request(
            f"{BASE_URL}/repositories/oauth/isOauth?scmCode={urllib.parse.quote(args.scm_code)}",
            access_token=access_token,
        )
        if not check.get("data"):
            print(
                f"未完成 {scm['name']} 的 OAuth 授权，无法用 OAUTH 方式关联。\n"
                f"请先打开授权页：{CONSOLE_URL}/codelib/{args.project_id}/\n"
                f"或改用凭据方式：credential-create 后 create --auth-type HTTPS --credential-id <id>",
                file=sys.stderr,
            )
            sys.exit(2)
    elif not args.credential_id:
        print("错误：非 OAUTH 方式必须传 --credential-id（先用 credential-create 创建）。", file=sys.stderr)
        sys.exit(1)

    project_name = args.git_project_name or _guess_project_name(args.url)
    body = {
        "@type": scm["repo_type"],
        "aliasName": args.alias_name,
        "url": args.url,
        "userName": args.user_name,
        "projectName": project_name,
        "credentialId": args.credential_id or "",
        "authType": args.auth_type,
        "scmType": args.scm_code,
        "projectId": args.project_id,
    }
    url = f"{BASE_URL}/repositories/projects/{args.project_id}/repository"
    result = make_request(url, method="POST", data=body, access_token=access_token)
    _check(result)

    hash_id = (result.get("data") or {}).get("hashId")
    print_data({
        "已托管代码库": args.alias_name,
        "地址": args.url,
        "授权方式": args.auth_type,
        "hashId": hash_id,
        "在编排中引用": {
            "checkout": f"- checkout: {args.url}",
            "变量 git-ref 的 repo-id": hash_id,
        },
        "代码库页": f"{CONSOLE_URL}/codelib/{args.project_id}/",
    })


def _guess_project_name(url: str) -> str:
    """从 https://git.woa.com/group/repo.git 推出 group/repo。"""
    path = urllib.parse.urlparse(url).path.strip("/")
    return path[:-4] if path.endswith(".git") else path


def cmd_oauth_page(args: argparse.Namespace) -> None:
    codelib = f"{CONSOLE_URL}/codelib/{args.project_id}/"
    manage = f"{CONSOLE_URL}/permission/auth/oauth"
    print_data({
        "关联并授权": codelib,
        "查看/撤销已有授权": manage,
        "操作步骤": [
            "打开「关联并授权」页面",
            "点「关联代码库」→ 选工蜂 GIT",
            "授权方式选 OAUTH → 点「去授权」跳转工蜂完成授权",
            "回到会话，用 is-oauth 确认已授权",
        ],
        "风险提示": "OAuth 授权长期有效，直到在「查看/撤销已有授权」页面手动撤销。",
    })
    if args.open:
        webbrowser.open(codelib)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="蓝盾代码库 / 凭据客户端",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--access-token", help="访问令牌（默认取 BK_CI_ACCESS_TOKEN）")

    project = argparse.ArgumentParser(add_help=False)
    project.add_argument("--project-id", required=True, help="项目编码（个人项目形如 _{企业微信名}）")

    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("scm-config", parents=[common], help="查看各代码库支持的授权方式")

    p = sub.add_parser("is-oauth", parents=[common], help="查询 OAuth 授权状态")
    p.add_argument("--scm-code", default="CODE_GIT", help="代码库类型，默认 CODE_GIT（工蜂 GIT）")

    p = sub.add_parser("resolve", parents=[common, project],
                       help="⭐ 查仓库是否已托管（命中直接用，未命中 exit 3）")
    p.add_argument("--repo", required=True,
                   help="仓库地址或名字，如 https://git.woa.com/group/repo.git 或 group/repo")
    p.add_argument("--repository-type", help="按类型过滤，如 CODE_GIT")

    p = sub.add_parser("list", parents=[common, project], help="列出已托管代码库")
    p.add_argument("--repository-type", help="按类型过滤，如 CODE_GIT")

    p = sub.add_parser("get", parents=[common, project], help="查代码库详情")
    p.add_argument("--repository-id", required=True, help="代码库 hashId 或别名")
    p.add_argument("--repository-type", default="ID", choices=["ID", "NAME"], help="ID=哈希ID，NAME=别名")

    p = sub.add_parser("credential-list", parents=[common, project], help="列出项目凭据")
    p.add_argument("--credential-types", default="TOKEN_USERNAME_PASSWORD,USERNAME_PASSWORD,ACCESSTOKEN",
                   help="凭据类型，逗号分隔")
    p.add_argument("--keyword", help="关键字")
    p.add_argument("--page", type=int, default=1)
    p.add_argument("--page-size", type=int, default=20)

    p = sub.add_parser("credential-create", parents=[common, project], help="创建凭据（保存 token/账号密码）")
    p.add_argument("--credential-id", required=True, help="凭据 ID，编排中用 ${{settings.<id>.password}} 引用")
    p.add_argument("--credential-name", help="凭据名称，默认同 ID")
    p.add_argument("--remark", help="凭据描述")
    p.add_argument("--credential-type", default="TOKEN_USERNAME_PASSWORD",
                   choices=sorted(CREDENTIAL_FIELDS), help="凭据类型")
    p.add_argument("--v1", help="见 scm-config：用户名 / access token / SSH 私钥")
    p.add_argument("--v2", help="密码 / private token")
    p.add_argument("--v3", help="private token")
    p.add_argument("--v4", help="备用字段")

    p = sub.add_parser("create", parents=[common, project], help="关联代码库到蓝盾")
    p.add_argument("--url", required=True, help="仓库地址，如 https://git.woa.com/group/repo.git")
    p.add_argument("--alias-name", required=True, help="代码库别名")
    p.add_argument("--user-name", required=True, help="授权人英文名")
    p.add_argument("--scm-code", default="CODE_GIT", help="代码库类型，默认 CODE_GIT")
    p.add_argument("--auth-type", default="HTTPS", choices=["OAUTH", "HTTPS", "HTTP", "SSH"], help="授权方式")
    p.add_argument("--credential-id", help="凭据 ID（非 OAUTH 必填）")
    p.add_argument("--git-project-name", help="git 项目名（默认从 url 推导，如 group/repo）")
    p.add_argument("--force", action="store_true", help="仓库已托管时仍然再关联一条")

    p = sub.add_parser("oauth-page", parents=[project], help="打印/打开网页授权入口")
    p.add_argument("--open", action="store_true", help="直接用默认浏览器打开")

    args = parser.parse_args()
    {
        "scm-config": cmd_scm_config,
        "is-oauth": cmd_is_oauth,
        "resolve": cmd_resolve,
        "list": cmd_list,
        "get": cmd_get,
        "credential-list": cmd_credential_list,
        "credential-create": cmd_credential_create,
        "create": cmd_create,
        "oauth-page": cmd_oauth_page,
    }[args.command](args)


if __name__ == "__main__":
    main()
