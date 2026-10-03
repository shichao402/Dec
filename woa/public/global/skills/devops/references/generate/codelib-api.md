# 蓝盾代码库 / 凭据 / Git 授权 API（普通流水线 · prod）

来源：蓝盾 APIGW `prod` 用户态与代码库前端 `https://devops.woa.com/console/codelib/{projectId}/` 的实际请求。

网关地址：`https://devops.apigw.o.woa.com/prod/v4/apigw-user`  
认证：请求头 `X-Bkapi-Authorization: {"access_token":"<token>"}`，与 `pipeline_generate_client` 一致。令牌由 `scripts/auth.py` 读取。

---

## 1. 代码库类型配置（scmCode 从哪来）

前端接口（浏览器 Cookie 认证，网关未发布同名资源）：

```
GET https://devops.woa.com/ms/repository/api/user/repositories/config/
```

返回每种代码库支持的授权方式，是选型的依据：

| scmCode | 名称 | hosts | 支持的凭据类型（authType） | PAC |
|---------|------|-------|--------------------------|-----|
| `CODE_GIT` | 工蜂 GIT | git.woa.com | `OAUTH`(OAUTH)、`USERNAME_PASSWORD`(HTTPS)、`TOKEN_USERNAME_PASSWORD`(HTTPS)、`TOKEN_SSH_PRIVATEKEY`(SSH) | ✅ |
| `CODE_TGIT` | 工蜂 TGIT | git.tencent.com, git.code.tencent.com | `TOKEN_USERNAME_PASSWORD`(HTTPS)、`TOKEN_SSH_PRIVATEKEY`(SSH) | ❌ |
| `CODE_SVN` | 工蜂 SVN | svn.woa.com | `TOKEN_USERNAME_PASSWORD`(http)、`TOKEN_SSH_PRIVATEKEY`(ssh) | ❌ |
| `GITHUB` | GitHub | github.com | `OAUTH` | ❌ |
| `CODE_GITLAB` | GitLab | — | `ACCESSTOKEN`(HTTPS)、`TOKEN_SSH_PRIVATEKEY`(SSH) | ❌ |
| `CODE_P4` | Perforce | — | `USERNAME_PASSWORD`(HTTPS) | ❌ |
| `codeGitee` | 社区版码云 | gitee.com | `OAUTH`、`TOKEN_USERNAME_PASSWORD`、`TOKEN_SSH_PRIVATEKEY`、`ACCESSTOKEN` | ❌ |
| `bkCode` / `BKCode_WOA` | 离岸云研发 / 蓝鲸内部代码库 | bkcode.net / bkcode.woa.com | `ACCESSTOKEN`、`TOKEN_USERNAME_PASSWORD`、`TOKEN_SSH_PRIVATEKEY` | ❌ |

**关键结论：工蜂 GIT（git.woa.com）既支持 OAUTH，也支持凭据（token/账号密码）。凭据方式可以全程走 API；OAUTH 必须在网页上完成一次跳转授权。**

---

## 2. 校验 OAuth 授权状态

```
GET /v4/apigw-user/repositories/oauth/isOauth?scmCode={scmCode}
```

| 参数 | 位置 | 必填 | 说明 |
|------|------|------|------|
| scmCode | query | √ | 代码库类型，如 `CODE_GIT` |

返回 `ResultBoolean`：`{"data": true, "message": "", "status": 0}`，`data=true` 表示当前用户已对该类型完成 OAuth 授权。

网关资源名：`v4_user_oauth_isOauth`（应用态 `v4_app_oauth_isOauth`）。

---

## 3. 凭据管理

### 新增凭据

```
POST /v4/apigw-user/projects/{projectId}/credentials/credential
```

Body（`CredentialCreate`）：

```json
{
  "credentialId": "my_git_token",
  "credentialName": "工蜂只读 token",
  "credentialRemark": "由 AI 助手创建，用于代码库拉取",
  "credentialType": "TOKEN_USERNAME_PASSWORD",
  "v1": "royalhuang",
  "v2": "<personal access token>",
  "v3": "",
  "v4": ""
}
```

`credentialType` 枚举：`PASSWORD`、`ACCESSTOKEN`、`OAUTHTOKEN`、`USERNAME_PASSWORD`、`SECRETKEY`、`APPID_SECRETKEY`、`SSH_PRIVATEKEY`、`TOKEN_SSH_PRIVATEKEY`、`TOKEN_USERNAME_PASSWORD`、`COS_APPID_SECRETID_SECRETKEY_REGION`、`MULTI_LINE_PASSWORD`。

`v1`~`v4` 的含义随类型变化，工蜂常用：

| credentialType | v1 | v2 | v3 | 适用 |
|----------------|----|----|----|------|
| `USERNAME_PASSWORD` | 用户名 | 密码 | — | 工蜂 HTTPS 账号密码 |
| `TOKEN_USERNAME_PASSWORD` | 用户名 | 密码 | private token | 工蜂 HTTPS（推荐，token 权限可控） |
| `ACCESSTOKEN` | access token | — | — | GitLab / bkCode |
| `TOKEN_SSH_PRIVATEKEY` | SSH 私钥 | private token | — | SSH 方式 |

返回 `ResultBoolean`。网关资源名：`v4_user_credential_create`。

### 凭据列表

```
GET /v4/apigw-user/projects/{projectId}/credentials/credential_list?credentialTypes=TOKEN_USERNAME_PASSWORD,USERNAME_PASSWORD&keyword=&page=1&pageSize=20
```

`credentialTypes` 必填，逗号分隔。返回分页的 `credentialId` / `credentialName` / `credentialType` / `permissions`。

其它：`v4_user_credential_get`（GET）、`v4_user_credential_edit`（PUT）、`v4_user_credential_delete`（DELETE），路径均为 `/v4/apigw-user/projects/{projectId}/credentials/credential`。

---

## 4. 代码库托管

### 关联代码库

```
POST /v4/apigw-user/repositories/projects/{projectId}/repository
```

Body 是多态模型 `Repository`，`@type` 决定实现类：

| @type | 对应代码库 |
|-------|-----------|
| `codeGit` | 工蜂 GIT（CodeGitRepository） |
| `codeTGit` | 工蜂 TGIT |
| `codeSvn` | 工蜂 SVN |
| `codeGitLab` | GitLab |
| `github` | GitHub |
| `codeP4` | Perforce |

`codeGit` 字段：

| 字段 | 必填 | 说明 |
|------|------|------|
| `@type` | √ | 固定 `codeGit` |
| `aliasName` | √ | 代码库别名，编排里引用的名字 |
| `url` | √ | 仓库地址，如 `https://git.woa.com/bkdevops/bk-certs.git` |
| `userName` | √ | 授权人英文名 |
| `projectName` | √ | git 项目名，如 `bkdevops/bk-certs` |
| `credentialId` | √（OAUTH 时传空串） | 凭据 ID |
| `authType` | | `SSH` / `HTTP` / `HTTPS` / `OAUTH` |
| `scmType` | | `CODE_GIT` |
| `enablePac` | | 是否开启 PAC |
| `gitProjectId` | | 工蜂项目数字 ID |

返回 `{"data": {"hashId": "..."}, "status": 0}`，`hashId` 即编排里 `repo-id` / `repositoryHashId` 要用的值。

### 其它代码库接口

| 资源 | 方法 | 路径 |
|------|------|------|
| 列表 | GET | `/v4/apigw-user/repositories/projects/{projectId}/repository_info_list?repositoryType=CODE_GIT&page=1&pageSize=100` |
| 详情 | GET | `/v4/apigw-user/repositories/projects/{projectId}/repository?repositoryId={hashId或别名}&repositoryType=ID\|NAME` |
| 编辑 | PUT | `/v4/apigw-user/repositories/projects/{projectId}/repository` |
| 删除 | DELETE | `/v4/apigw-user/repositories/projects/{projectId}/repository` |
| 开启 PAC | PUT | `/v4/apigw-user/repositories/projects/{projectId}/{repositoryHashId}/pac/enable` |
| 关闭 PAC | PUT | `/v4/apigw-user/repositories/projects/{projectId}/{repositoryHashId}/pac/disable` |

`repository_info_list` 的 `repositoryType` 枚举：`CODE_SVN`、`CODE_GIT`、`CODE_GITLAB`、`GITHUB`、`CODE_TGIT`、`CODE_P4`。

列表返回分页结构 `{count, page, pageSize, totalPages, records}`，每条 `RepositoryInfo` 只有这些字段：

| 字段 | 说明 |
|------|------|
| `aliasName` | 仓库别名 |
| `url` | 仓库地址 |
| `type` | 代码库类型枚举 |
| `repositoryHashId` | **编排里 `repo-id` 要用的哈希 ID** |
| `repositoryId` / `remoteRepoId` | 数字 ID / 远程仓库 ID |
| `createUser` / `createdTime` / `updatedTime` | 创建与更新信息 |

注意列表里**没有 `authType` / `userName`**，要看授权方式得用详情接口（`ResultRepository`）。

### 判断仓库是否已托管

列表里的 `url` 可能是 `https://`、`http://` 或 `git@host:group/repo.git` 各种写法，直接字符串比对会漏。归一到 `host/group/repo`（去协议、去 `.git`、去 URL 内登录名、`git@host:` 转 `host/`）后再比，或退化成只比 `group/repo`。`codelib_client.py resolve` 就是这么做的。

---

## 5. OAuth 授权：网关没有取授权链接的接口

网关 creative 环境下与 oauth 相关的资源只有：

- `v4_user_oauth_isOauth` — 只能查状态
- `v4_stream_user_ci_reset_oauth` — Stream（工蜂 CI）场景刷新项目启动人，不是代码库托管授权
- `v4_app_oauth2_access_token` — 应用态 oauth2 换 token

取「去授权」跳转链接的能力只在前端接口上（`refreshGitOauth({type:'git', redirectUrl, refreshToken:true})` → 返回 `{url}` → 浏览器跳工蜂授权页），网关未发布。

**因此 OAuth 授权必须走网页**，两个入口：

- 关联代码库时授权：`https://devops.woa.com/console/codelib/{projectId}/` → 「关联代码库」→ 工蜂 GIT → 授权方式选 OAUTH → 去授权
- 查看 / 撤销已有授权：`https://devops.woa.com/console/permission/auth/oauth`

而**凭据方式（token / 账号密码）可以全程用 API 完成**：创建凭据 → 关联代码库（`authType: HTTPS` + `credentialId`），无需任何页面操作。

---

## 6. 两条落地路径对照

| | A. 凭据方式（纯 API） | B. OAUTH（需页面一次） |
|---|---|---|
| 步骤 | `credential-create` → `codelib-create` | 开网页授权 → `codelib-create`（`authType=OAUTH`） |
| 会话内能否闭环 | ✅ 可以 | ❌ 需用户在浏览器点一次 |
| 凭据存放 | 蓝盾凭据中心（加密，日志脱敏） | 蓝盾侧持有 OAuth token |
| 有效期 | 取决于 token 有效期，可随时删凭据 | **长期有效**，需到 `/console/permission/auth/oauth` 手动撤销 |
| 适合 | 会话中一次性配好、权限最小化 | 用户已授权过、或不想管理 token |
