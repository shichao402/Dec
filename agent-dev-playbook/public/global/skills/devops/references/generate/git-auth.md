# Git 授权：让编排能拉到私有代码

**脚本**：`scripts/codelib_client.py` · **接口细节**：[codelib-api.md](./codelib-api.md)

---

## 什么时候必须走这个流程

只要编排要访问私有仓库（`git.woa.com` 等），就要先把授权配好，否则构建会**卡在 clone 而不是报错**——构建机弹出 Git Credential Manager 登录框，没人点，任务一直挂着。

典型信号：

- 用户要「同步/拉取某个仓库」「定时更新代码」
- 编排里出现 `checkout:` 或脚本里有 `git clone` / `git fetch`
- 构建长时间停在某个 step、日志最后一行是 git 相关
- 构建机上弹出 `Git Credential Manager` 窗口

---

## 🔴 第 0 步：先查这个仓库是不是已经托管过了

**用户一提到某个代码库，第一件事是查，不是配。** 个人项目里常年躺着几个已托管的仓库，重复关联既多余又会产生同名代码库。

```bash
python scripts/codelib_client.py resolve --project-id {projectId} --repo https://git.woa.com/{group}/{repo}.git
```

`--repo` 可以是完整地址，也可以是 `group/repo` 简写；协议（http/https/ssh）、`.git` 后缀、`git@host:` 写法、URL 里的登录名都会被抹平后比对。

| 结果 | 退出码 | Agent 该做什么 |
|------|--------|---------------|
| 已托管 | 0 | **直接拿返回的 `hashId` 和 URL 写进编排，跳过下面所有授权流程**，只需告诉用户「这个库已托管，直接用了」 |
| 未命中但有疑似候选 | 3 | 把候选列给用户确认是不是同一个库，别自作主张 |
| 完全没有 | 3 | 才进入下面的方案选择 |

`create` 也内置了同样的检查：仓库已托管时会跳过关联并回显已有记录，除非显式加 `--force`。所以即使漏了这一步也不会重复建库，但会白跑一趟对话。

---

## 必须让用户选，不要替用户决定

确认仓库确实没托管过，再让用户选方案，**默认推荐方案 A**。会话有 `ai-hub-ask-user-input` 时用 `single_select` 卡片（选项文案用下面 A/B 两句），不要在对话里列 A/B/C。无该 skill 才用下面这段话：

> 拉取 `{仓库地址}` 需要授权，两种方式：
>
> **A. 托管到蓝盾代码库服务（推荐）** —— 授权存在蓝盾侧，编排直接引用，构建机不留任何凭据，后续本项目流水线可复用同一份授权。
>
> **B. 在构建机上做一次 git 登录并持久化凭据** —— 立刻能用，但凭据会长期留在那台机器上，任何在该机器运行的任务都能用它访问你的代码，需要你确认风险。

用户没明确选之前不要开始配置。

---

## 方案 A：托管到蓝盾代码库服务（推荐）

两条路径，能不开网页就不开网页。

### A1. 凭据方式（会话内全程 API 闭环，首选）

适合：用户能提供工蜂 private token（或账号密码），希望一次配好、权限可控。

```bash
# 1. 看清该类型支持哪些授权方式
python scripts/codelib_client.py scm-config

# 2. 把 token 存进蓝盾凭据中心（不会明文进编排，日志自动脱敏）
python scripts/codelib_client.py credential-create \
  --project-id {projectId} \
  --credential-id my_git_token \
  --credential-name "工蜂只读 token" \
  --credential-type TOKEN_USERNAME_PASSWORD \
  --v1 {企业微信名} --v2 {密码} --v3 {private token}

# 3. 关联代码库
python scripts/codelib_client.py create \
  --project-id {projectId} \
  --url https://git.woa.com/{group}/{repo}.git \
  --alias-name {group}/{repo} \
  --user-name {企业微信名} \
  --auth-type HTTPS \
  --credential-id my_git_token
```

返回的 `hashId` 就是编排里 `props.repo-id`（`git-ref` 变量）要用的值。

工蜂 private token 在 `https://git.woa.com/profile/private_tokens` 创建，**建议只勾 read_repository 这类最小权限**。

### A2. OAuth 方式（需要在网页点一次）

适合：用户不想管理 token，或已经授权过。网关没有开放取授权链接的接口，OAuth 必须在网页完成。

```bash
# 先查是否已经授权过，授权过就直接跳到 create
python scripts/codelib_client.py is-oauth --scm-code CODE_GIT

# 未授权：打印/打开授权入口，请用户点一次
python scripts/codelib_client.py oauth-page --project-id {projectId} --open

# 用户完成后再确认一次，然后关联
python scripts/codelib_client.py is-oauth --scm-code CODE_GIT
python scripts/codelib_client.py create \
  --project-id {projectId} \
  --url https://git.woa.com/{group}/{repo}.git \
  --alias-name {group}/{repo} \
  --user-name {企业微信名} \
  --auth-type OAUTH
```

网页步骤：代码库页 →「关联代码库」→ 工蜂 GIT → 授权方式选 OAUTH →「去授权」跳转工蜂。

> ⚠️ 告知用户：**OAuth 授权长期有效**，直到在 `https://devops.woa.com/console/permission/auth/oauth` 手动撤销。

### 已托管后在编排里怎么用

```yaml
stages:
  - name: stage-sync
    jobs:
      job-sync:
        name: 同步代码
        steps:
          - checkout: https://git.woa.com/{group}/{repo}.git
            name: 拉取代码
            with:
              refName: master
```

需要让用户在启动时选分支时，用 `git-ref` 变量 + 上一步拿到的 `hashId`：

```yaml
variables:
  target-branch:
    value: master
    props:
      label: 目标分支
      type: git-ref
      repo-id: {上一步返回的 hashId}
```

`resolve` 已经把 `hashId` 给你了；想看项目下全部已托管代码库用 `codelib_client.py list --project-id {projectId}`。

---

## 方案 B：构建机本地 git 登录 + 持久化凭据

用户明确选 B 时才做，并且**先把风险说清楚**：

> 这台构建机会长期保存你的 git 凭据，在它上面运行的任何任务都能用这份凭据访问你有权限的代码库；凭据不会自动过期，需要你手动清理。相比之下方案 A 的授权可以随时在蓝盾侧撤销。

### 一次登录，后续不再弹窗

关键是配好 credential helper，让第一次输入的凭据落进系统凭据库，**在构建机上执行**（不是 Agent 本机）：

| 构建机 OS | 命令 | 凭据存放 |
|-----------|------|---------|
| Windows | `git config --global credential.helper manager` | Windows 凭据管理器（加密） |
| macOS | `git config --global credential.helper osxkeychain` | 钥匙串（加密） |
| Linux | `git config --global credential.helper libsecret` | Secret Service（加密） |
| Linux（无桌面环境兜底） | `git config --global credential.helper store` | **明文** `~/.git-credentials` |

配好后在构建机上手工跑一次 `git clone https://git.woa.com/{group}/{repo}.git`，输入用户名 + private token（**不要用登录密码**），之后同一台机器上的所有 git 操作都不再提示。

非交互写入（用于脚本化，避免弹窗）：

```bash
git config --global credential.helper store
printf 'protocol=https\nhost=git.woa.com\nusername=%s\npassword=%s\n' "{用户名}" "{private token}" | git credential approve
```

Windows PowerShell：

```powershell
git config --global credential.helper manager
"protocol=https`nhost=git.woa.com`nusername={用户名}`npassword={private token}`n" | git credential approve
```

### 更安全的变体：每次构建临时注入，不落盘

如果只是想让流水线跑起来，又不想把凭据留在机器上，用凭据中心 + 每次运行时拼 URL：

```yaml
- name: 同步代码
  run: |
    git clone "https://${{settings.my_git_token.username}}:${{settings.my_git_token.password}}@git.woa.com/{group}/{repo}.git"
```

凭据由蓝盾运行时注入、日志自动脱敏，构建结束不残留。**这是方案 B 里应该优先推荐的写法。**

### 清理方式

| 方式 | 清理 |
|------|------|
| Windows 凭据管理器 | 控制面板 → 凭据管理器 → Windows 凭据 → 删除 `git:https://git.woa.com` |
| macOS 钥匙串 | 钥匙串访问 → 搜 `git.woa.com` → 删除 |
| `store` 明文 | 删掉 `~/.git-credentials` 对应行 |
| 工蜂 token | `https://git.woa.com/profile/private_tokens` 撤销 |

---

## Agent 检查清单

- [ ] **用户一提到代码库，先 `resolve` 查是否已托管；命中就直接用 `hashId`，不要发起任何授权流程**
- [ ] 有疑似候选时先让用户确认，别猜
- [ ] 确实没托管，才**问用户选 A 还是 B**，默认推荐 A
- [ ] 选 A 前 `is-oauth` 看是否已授权，已授权可直接 `create --auth-type OAUTH`，省掉开网页
- [ ] 优先 A1（纯 API），只有用户要 OAuth 或拿不到 token 时才开网页
- [ ] 选 B 必须显式告知「凭据长期留在该机器 + 需手动清理」并等用户确认
- [ ] 选 B 优先用「凭据中心临时注入」写法，而不是往机器上写凭据
- [ ] token 一律进凭据中心，**绝不写进 YAML 明文**
- [ ] Git 凭据只用于 Git；不要把其它 SCM 的账号填进 Git 托管/credential helper
- [ ] 脚本禁止 `set -x` / `set -ex` 以及把 token 拼进 argv
- [ ] 配完后告诉用户在哪撤销（蓝盾 OAuth 管理页 / 凭据中心 / 工蜂 token 页）
