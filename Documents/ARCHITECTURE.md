# Dec 架构设计

本文档描述 Dec 的实现结构与运行机制。

用户侧说明以以下文档为准：

- [README.md](../README.md)：项目概览与快速开始
- [BUNDLE-SECRETS-MODEL.md](./BUNDLE-SECRETS-MODEL.md)：项目四象限与 Bitwarden private secrets
- [ROADMAP.md](./ROADMAP.md)：尚未排期的意向（含本机 Login with Device）
- [research/secrets-ci-centralization.md](./research/secrets-ci-centralization.md)：密钥源唯一与 CI 下放调研（2026-08）
- [TUI_ARCHITECTURE.md](./TUI_ARCHITECTURE.md)：TUI 已卸下（ADR 0020）
- [client/README.md](../client/README.md)：桌面 Console（人机入口）
- [schema/dec/v1/README.md](../schema/dec/v1/README.md)：Dec 配置 Protobuf schema
- [schema/secrets/v1/README.md](../schema/secrets/v1/README.md)：Secrets bundle Protobuf schema
- `internal/assets/dec/SKILL.md`：Dec Skill 的完整使用说明

## 概览

Dec 是一个以 **Console** 为第一人机入口、以 **MCP** 为 Agent 入口的个人 AI 资产管理工具，用于把 Skills、Rules、MCP 配置保存在个人 Vault 中，并在不同项目、不同 IDE 间复用。

运行时采用「一机一服务、多门面」：

| 程序 | 职责 |
|------|------|
| `dec-server` | 本机单例；启动即锁定，Authenticate 成功后持有 BW session 与 1h 控制权 |
| `dec` | 最小 CLI（`--version`、内部 hidden 命令）；无参提示改用 Console |
| `dec-mcp` | Agent stdio MCP 门面；无服务时自动拉起 `dec-server` |
| `dec-exec` | 独立 env 注入程序；只读已落地 `.secrets/**/.env/*.env`，不经过服务、不碰 session |
| Dec Console | 独立 Tauri 客户端（`client/`）；本机/远程连接、解锁与日常管理 |

门面与服务默认绑定 `127.0.0.1` 的 gRPC；可用 `management_listen` + TLS 做远程直连。端点与本机随机 token 写在 `~/.dec/run/server.json`。进程启动后锁定，见 [0018](decisions/0018-instance-lock-and-console.md)。同一 project 的 pull/push 等写操作互斥；未发起操作的门面可旁观该 project 当前操作的实时进度。详见 [0008](decisions/0008-service-facade-split.md)。

资产按 [ADR 0016](decisions/0016-p-four-quadrant-model.md) 的顶层 **项目** 组织：

| 层级 | 存储位置 | 职责 |
|------|----------|------|
| **项目声明** | Git Vault `<p>/dec.yaml` | 展示信息、IDE 默认值、direct `requires` |
| **Git 四象限** | `<p>/{public,private}/{user,project}/` | 全部为非敏感资产；private 仅表示不可被其它项目引用 |
| **BW private** | `<p>/private/{user,project}` folder | 敏感正文；与 Git 同 项目/plane/相对路径 零冲突 |

用户平面显式启用项目；项目工作区绑定家项目。家项目的 project 两象限可见，且只展开其
direct requires 的 `public/project`，不递归、不引入 user/private。Git 资产落到
`.dec/cache/<p>/<visibility>/<plane>/` 后渲染 IDE；BW 内容独立落到
`.secrets/<p>/` 或 `~/.dec/secrets/<p>/`。项目级 SSH/GCM 定向到家工作区，
机器级 SSH/GCM 保持用户范围。

用户操作通过 Console（`client/`）完成，业务逻辑在 `internal/app/`（仅 `dec-server` 内执行）：

- 仓库连接 / 本机 vars / 服务版本与重启：Console **设置**
- 项目初始化 / project 选择：Console **引导 / 项目**
- 项目启用、家项目 requires 与四象限浏览：Console **项目 / 资产**
- pull / push / remove（含成功对照后的孤儿 reconcile）：Console **同步**
- 远端设备探测与置备：Console **连接**（ADR 0019）
- 版本信息：`dec --version`

资产目录类型（skill / command / rule / mcp）以 `internal/bundle.VaultAssetKinds`
为共用真相源。`PWriter` 是新写路径唯一门面；`BundleWriter`、`enabled_bundles`
等名字只为源码/wire 兼容，不能据此回退到旧存储模型。

## 项目解析与写边界

- `internal/pmodel` 严格加载 `<p>/dec.yaml` 及四象限；项目名、manifest 名称一致性、
  自引用、未声明顶层目录和资产符号链接均硬失败。
- `internal/app/bundle_resolver.go` 在项目仓库中走项目 resolver：用户平面取已启用项目
  的 user 两象限；项目平面取家项目的 project 两象限和 direct requires 的
  `public/project`。
- 项目 push 只允许家项目；requires 副本不进入实际 push，也不进入 push 预览。
- `internal/app/bundle_writer.go` 的 `PWriter` 统一承接项目选择、push、Remote
  private 写入、delete/remove；服务端 dispatch 注入 writer，Console/MCP 不内嵌 app。
- 普通 push 发现非空 `projects/` 或 `bundles/` 会拒绝，提示远端尚未完成一次性项目迁移。
- 旧 Git/BW 结构由一次性远端迁移改写；新版本启动只清理本机旧 cache / `.secrets` 遗留并清空启用列表，由用户重新选择后 Pull。

## 文档边界

- `README.md` 负责概览、安装、快速上手
- `internal/assets/dec/SKILL.md` 负责完整的用户操作语义
- 本文档保留架构、目录结构、模块边界和关键运行机制

## 旧 Project + Bundle 目录（仅迁移输入）

本节至“关键运行机制”前记录一次性迁移所识别的旧布局，不代表迁移后运行时仍会双读。
新仓库不得创建 `projects/`、`bundles/` 或 `bundle/<name>`。

### 三层目录总览

```text
Git Vault（Dec 仓库）          Bitwarden                    本地工作区
─────────────────────          ─────────                    ──────────
projects/my-app.yaml           folder: vikunja（默认同名）     my-app/
  bundles: [vikunja]             Secure Note → .secrets/       ├── .dec/
bundles/vikunja/                 SSH Key → ~/.ssh/            │   ├── config.yaml  ← project_name
  skills/...                                                  │   └── cache/vikunja/
bundles/default/                                              ├── .secrets/bundles/vikunja/
                                                              │   └── .env/vikunja.env
                                                              └── .cursor/...
                                                              └── .secrets/...
```

## Project 层

**Project** 是 Dec 的配置入口：用户只需在 project 里声明需要哪些 bundle，不必在每个工作区手工维护完整 bundle 列表。

### Vault 中的 Project 声明

```text
<repo>/
├── projects/
│   ├── my-app.yaml          # project 声明
│   └── dec-cli.yaml
└── bundles/
    ├── vikunja/
    │   ├── bundle.yaml      # bundle 成员声明（可选）
    │   ├── skills/
    │   ├── rules/
    │   ├── mcp/
    │   └── commands/
    └── default/
        ├── bundle.yaml
        └── skills/helloworld/...
```

`projects/<name>.yaml` 示例：

```yaml
name: my-app
description: 我的应用项目

bundles:
  - vikunja
  - helloworld

ides:
  - cursor

editor: code --wait
```

- `bundles`：启用的 Dec bundle 短名（对应 `bundles/<name>/`）
- `ides` / `editor`：该项目默认值；本地 `.dec/config.yaml` 可覆盖

### 本地 Project 引用

工作区 `.dec/config.yaml` 以 **`project_name`** 引用 vault 中的 project；bundle 列表从 vault project 同步到本地 `enabled_bundles`：

```yaml
version: v2

project_name: my-app

ides:               # 可选：机器级 IDE 覆盖
  - cursor

enabled_bundles:    # 从 projects/my-app.yaml 同步；Bundles 页可调整
  - vikunja
  - helloworld
```

`enabled_bundles` 是唯一的资产启用入口：成员资产随 bundle 一并解析下发，不能单独启用或排除。
保存时按平面校验 vault 声明：本平面看不见的名字（仓库里已删除、或 `scope: user`）不写入 `enabled_bundles`，被拒条目连同理由回传给 Console；仓库未连接时放行以免离线存不了。项目平面只校验、不创建占位也不改写 scope（见 [0013](decisions/0013-secrets-belong-to-declared-target.md) §7a）。
早期版本的 `available` / `enabled` 字段已移除，`LoadProjectConfig` 读到旧配置时会把 `enabled` 涉及的 vault 折叠成 bundle 引用并立即回写，`available` 作为扫描缓存直接丢弃。

**职责划分**：

| 字段 | 存储 | 说明 |
|------|------|------|
| `project_name` | 本地 + vault 文件名 | 关联 `projects/<name>.yaml` |
| `bundles` | vault project | bundle 列表真相源 |
| `enabled_bundles` | 本地 | 从 vault 同步或 Bundles 页保存；pull 解析用 |
| `ides` / `editor` | vault project + 本地覆盖 | 本地优先 |

### Project 初始化（Console-first）

首次进入工作区或引导 **初始化 project** 时，Console 引导完成以下之一：

```mermaid
flowchart TD
  A[打开 Console / 连接设备] --> B{本地 .dec/config.yaml 存在?}
  B -->|否| C[推断 project 名]
  C --> D{vault 存在 projects/同名.yaml?}
  D -->|是| E[自动匹配并应用]
  D -->|否| F[Console：选择已有 project 或新建]
  B -->|是| G[加载 project_name]
  E --> H[写入 .dec/config.yaml]
  F -->|选择已有| I[从 vault 拉 project → 同步 enabled_bundles]
  F -->|新建| J[填名 + 选 bundles → push projects/名.yaml]
  I --> H
  J --> H
  G --> K[正常使用资产 / 同步]
  H --> K
```

1. **推断 project 名**：工作区目录 basename（如 `my-app`），或用户在 Console 中指定
2. **自动匹配（新机器）**：vault 存在 `projects/<basename>.yaml` → 自动应用，写入本地 `project_name` 与 `enabled_bundles`
3. **选择已有 project**：从 vault 列出 `projects/*.yaml`，用户选择 → 同步 bundle 列表到本地
4. **新建 project**：填 project 名、勾选 bundles → **push 到 vault** `projects/<name>.yaml` → 写入本地引用

不在 Console 外暴露 `dec init` 等 CLI 子命令；与 [Console 优先](../.cursor/rules/console-first.mdc) 一致。

## 目录结构

### Dec 根目录

```text
~/.dec/
├── config.yaml              # 全局配置（repo_url、默认 IDE、默认 editor）
├── local/
│   └── vars.yaml            # 本机级变量定义
├── repo.git/                # 本地 bare repo 缓存
└── secrets/
    ├── config.yaml          # Bitwarden 连接与 bundle ↔ folder 绑定
    └── device.json          # deviceIdentifier + 2FA remember 令牌（无密码、无 session）
```

secrets 没有本地同步状态文件：push 时**递归扫描** `.secrets/` 同步根；远端 note 列表用于 `MissingLocal` 报告。

若设置了 `DEC_HOME`，上述目录位于 `DEC_HOME` 下。

### 项目目录

```text
<workspace>/
├── .dec/
│   ├── config.yaml          # project_name + 本地 override + enabled_bundles
│   ├── cache/               # 资产缓存（pull 写入，push 读取）
│   ├── .version             # 最近一次 pull 的 commit 记录
│   ├── vars.yaml            # 项目变量定义（主文件，覆盖 vars.d/）
│   └── vars.d/              # 可选：拆分的变量片段 *.yaml / *.yml
├── .cursor/                 # IDE 渲染产物
└── .secrets/                # Bitwarden secrets 落地（须 .gitignore；不进 .dec/）
    ├── project/             # project 级 SyncTarget
    └── bundles/
        └── <name>/          # bundle 级 SyncTarget（如 .env/*.env）
```

`.dec/` 适合纳入版本控制：

- `config.yaml`：`project_name`、机器级 IDE / editor 覆盖、`enabled_bundles`
- `cache/`：pull 下来的 **公开** 资产缓存，也是 push 的读取源（私密文件不进 cache）
- `.version`：当前项目最近一次 pull 对应的远端 commit
- `vars.yaml`：项目级变量与资产级变量覆盖

### 仓库中的 Vault 结构

远端仓库顶层仅含 **projects/** 与 **bundles/**：

```text
<repo>/
├── projects/
│   └── <project-name>.yaml   # Project 声明（bundles、ides、描述）
└── bundles/
    └── <bundle-name>/        # bundle 目录，名与 project.bundles 引用一致
        ├── bundle.yaml       # bundle 成员声明（可选；缺省时合成隐式 bundle）
        ├── skills/
        │   └── <name>/
        │       └── SKILL.md
        ├── rules/
        │   └── <name>.mdc
        ├── mcp/
        │   └── <name>.json
        └── commands/
            └── <name>/
                └── ...
```

Dec 通过扫描 `projects/` 与 `bundles/` 发现 project 与资产，不依赖额外索引文件。

### Bitwarden secrets bundle 结构

与 Dec bundle **同构绑定**；project 启用的每个 bundle 在 pull 时成对拉取 Dec + secrets。Secure Note **名称** = 相对 **SyncTarget.LocalRoot** 的路径：

```text
Bitwarden folder: vikunja（默认同 Dec bundle 名）

Secure Note: .env/vikunja.env
  VIKUNJA_URL=https://vikunja.example.com
  VIKUNJA_API_TOKEN=...

Pull 后落地:
  <workspace>/.secrets/bundles/vikunja/.env/vikunja.env   # 不进 .dec/
```

环境变量由独立 `dec-exec --bundle vikunja` 读取 `.env/*.env` 注入子进程。

### SSH Key（机器级落地）

OpenSSH / Git 默认只认 **机器级** `~/.ssh/`，SSH 私钥 **不进项目根**（无 `keys/`、无项目级 `.ssh/config`）。

```text
Bitwarden folder: vikunja
  [SSH Key] .sshkey/deploy
    Name: .sshkey/deploy         # 规范名；落地时剥掉 .sshkey/ 前缀
    Notes: vikunja.example.com   # 可选；有内容时一行一个 host 或 host:port

Pull 后落地:
  ~/.ssh/dec_vikunja_deploy
  ~/.ssh/dec_vikunja_deploy.pub
  ~/.ssh/config                  # 有 hosts 时写入 Dec 管理区块（Host + IdentityFile）
```

- Notes 为空：只落私钥/公钥，不写 Host 配置、不报错。
- Notes 可为 `host` 或 `host:port`（如 `21.214.34.79:36000`）；有端口时写入 `Port`。
- Host 配置写入 `~/.ssh/config.d/dec.conf`，并在 `~/.ssh/config` **最顶部** `Include config.d/dec.conf`（OpenSSH 先匹配先生效，保证 Dec 注入优先）。
- `authorized_keys`：Dec 不管理（服务器侧自行维护）。
- `known_hosts`：Dec 不主动写入；首次连接由 OpenSSH 提示或由用户维护。
- 落地路径由 Bitwarden Item Name 推导（`.sshkey/<实例>` → `~/.ssh/dec_<bundle>_<实例>`），不记本地索引。
- 新建：Remote 页 `n` / `N` 选 `.sshkey` Processor，可本机生成或导入已有私钥（ADR 0004）。

**`.dec/` 树** 与 **`.secrets/` 树** **不得相交**；冲突时 pull 报错。SSH 落在 `~/.ssh/`，不参与项目内零重叠校验。详见 [BUNDLE-SECRETS-MODEL.md](./BUNDLE-SECRETS-MODEL.md) 与 [0002](decisions/0002-secrets-synctarget-root.md)。

### 端到端示例：vikunja project

以下展示 vault 声明、Dec bundle 与 pull 后本地三处目录的关系。

**Vault — project 声明**（`projects/my-app.yaml`）：

```yaml
name: my-app
description: 使用 Vikunja 集成的应用
bundles:
  - vikunja
ides:
  - cursor
```

**Vault — Dec bundle**（`bundles/vikunja/`）：

```text
bundles/vikunja/
├── bundle.yaml
├── skills/vikunja-workflow/SKILL.md
├── rules/vikunja-integration.mdc
└── mcp/vikunja-mcp.json          # command: dec-exec，无 token 占位
```

**Bitwarden — secrets bundle**（folder `vikunja`，默认同 Dec bundle 名）：

```text
Secure Note: .env/vikunja.env
  VIKUNJA_URL=https://vikunja.example.com
  VIKUNJA_API_TOKEN=...

[SSH Key] .sshkey/deploy  Notes: vikunja.example.com
```

**Pull 后本地目录**（存储根分离，零路径重叠）：

```text
my-app/
├── .dec/
│   ├── config.yaml               # project_name: my-app
│   └── cache/vikunja/            # Dec 公开资产（push 读取源）
│       ├── skills/...
│       ├── rules/...
│       └── mcp/vikunja-mcp.json
├── .cursor/                      # IDE 渲染产物（dec-* 前缀）
│   ├── skills/dec-vikunja-workflow/
│   ├── rules/dec-vikunja-integration.mdc
│   └── mcp.json                  # dec-vikunja-mcp 条目
└── .secrets/
    └── bundles/vikunja/
        └── .env/vikunja.env      # Bitwarden Secure Note 落地

机器级（不进项目根）：
  ~/.ssh/dec_vikunja_deploy
  ~/.ssh/dec_vikunja_deploy.pub
  ~/.ssh/config                   # Dec 管理区块
```

### IDE 托管输出

| IDE | Skills | Rules | MCP |
|---|---|---|---|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Claude | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Claude Internal | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Codex | `.codex/skills/` | `.codex/rules/` | `.codex/config.toml` |
| Codex Internal | `.codex/skills/` | `.codex/rules/` | `.codex/config.toml` |

Dec 托管产物统一使用 `dec-` 前缀。`claude-internal` / `codex-internal` 在项目级复用 `.claude/` / `.codex/`；用户级目录分别为 `~/.claude-internal/` 与 `~/.codex-internal/`。

## 关键运行机制

### 1. 仓库连接与事务

Console **设置** 页连接远端仓库到本地 `repo.git` bare repo 缓存。

- 读操作基于 bare repo 的最新远端引用
- 写操作通过短生命周期临时 worktree 完成，结束后自动清理
- Dec 依赖系统 `git`，日常认证由用户 Git 环境负责
- 首次连接 HTTPS 私仓若明确认证失败，Settings 可在用户确认后走
  [0011 Repo GCM Bootstrap](decisions/0011-private-repo-gcm-bootstrap.md)：`dec-server`
  直接按 repo host 从 Bitwarden 现有 `.gcm/*` Note 查找候选，复用 GCM Processor
  Apply 并验证后重试；token 不经 RPC、不另行持久化

### 2. 有效 IDE 与编辑器解析

资产部署目标优先级：

1. 项目级 `.dec/config.yaml`（机器级 override）
2. vault `projects/<project_name>.yaml` 的 `ides` / `editor`
3. 全局 `~/.dec/config.yaml`
4. 默认值 `cursor`

交互式编辑器优先级相同。Console **设置** 页安装 Dec 内置资产并写入全局 IDE 列表。

### 3. 内置资产与 Vault 资产的边界

三套内容平面：

- **Dec 产品源码**：`internal/`、`cmd/`、`Documents/` 等，通过构建进入二进制
- **项目级落地产物**：`.dec/`、`.cursor/`、`.claude/` 等，由 Console pull 写入
- **Vault 资产源**：远端 `projects/`、`projects/<name>.yaml` 与各 `bundles/<name>/` 目录与项目 `.dec/cache/`

修改 `internal/assets/` 走源码 commit + release；`.dec/cache/` 变更走 Console **同步** 页 push；project 声明变更走 vault `projects/` push。

### 4. 资产生命周期

#### project init（Home / 首次进入）

- 推断或让用户指定 project 名
- 自动匹配 vault `projects/<name>.yaml`，或列出已有 project 供选择，或新建并 push
- 写入本地 `.dec/config.yaml`（`project_name`、`enabled_bundles`）
- 生成 `.dec/vars.yaml` 模板

#### bundle 选择（Bundles 页）

- 扫描远端 vault 与 bundle，列出全部 bundle 及成员
- 勾选保存后只写 `.dec/config.yaml` 的 `enabled_bundles`
- 保留已有 `project_name` / `ides` / `editor`

#### pull（Run 页）

1. 解析 `project_name` → vault `projects/<name>.yaml`（或使用本地 `enabled_bundles`）
2. 对每个 enabled bundle：拉 Dec Git bundle → `.dec/cache/<bundle>/`
3. 自动拉 Bitwarden secrets（各 SyncTarget）→ Secure Note **`.secrets/` 同步根**；SSH Key Item → **`~/.ssh/`** + Dec 管理 config 区块
4. 零重叠校验（`.dec/` vs `.secrets/`）
5. 从 cache 渲染安装到 IDE 目录 + 非敏感 vars 占位符替换
6. 记录 commit 到 `.dec/.version`

#### push（Run 页）

- 从 `.dec/cache/` 读取已启用资产，写回 Git Vault
- project 声明变更：更新 vault `projects/<name>.yaml`
- secrets bundle 走 Bitwarden API，不进 Git

#### remove（Run 页）

- 删除远端匹配资产，同步清理 `.dec/config.yaml` 与 `.dec/cache/`

#### 自更新（Run 页 `u`）

- 唯一用户面入口：Console **同步** 页（检查 → 确认 → 下载替换）
- 无 `dec update` CLI
- 实现见 `internal/update/` 与 [UPDATE_ARCHITECTURE.md](./UPDATE_ARCHITECTURE.md)

#### 远端设备置备与按需拉起（Console / MCP）

置备是**服务端能力**：由发起端 `dec-server` 作为 SSH 客户端执行，Console 与 `dec-mcp`
共用同一条路径，客户端不内嵌 `internal/app`。实现见 `internal/app/remote_provision*.go`
与 `internal/app/remote_service.go`，决策见 [0019](decisions/0019-remote-provisioning.md)。

置备四段（`provision_remote_host`，走 `RunOperation` 进度流）：

1. **探测**（`probe_remote_host`，只读）：SSH 连通性、os/arch、四件套与版本、
   `git`/`bash`/`curl`/`ssh-keygen`、`~/.dec` 可写、能否拉起脱离会话的后台进程
2. **注入安装**：`go:embed` 的 `install.sh` 经 stdin 喂给远端 `bash -s`，脚本不落远端磁盘；
   下载产物按 `version.json` 的 `checksums` 校验 sha256
3. **配置**：远端执行自己的 `dec __service-setup`，用 config 包幂等写入
   `management_listen: 127.0.0.1:47653`。固定端口是隧道能自动找到远端的前提
4. **复探验证**：确认四件套齐全且监听地址已生效

远端**不安装常驻服务**，与本机同一套生命周期：空闲即退出。连接前经
`ensure_remote_service` 按需拉起，与本机门面的 `startServerProcess` 同构——
先读远端 `run/server.json`，不在运行则 `setsid nohup dec-server` 拉起，
再轮询至就绪。会话期间由 Console 的 `KeepAlive` 长连经 presence 压住空闲定时器。
**远端进程不在运行是正常状态，不是故障。**

安全边界：首次置备要求 typed confirm（真实键入目标主机名）；SSH 凭据只存引用，
复用 `~/.ssh/config` / ssh-agent / known_hosts；置备完成后远端服务仍是锁定态，
需 `Authenticate` 解锁，置备不携带主密码。设备级操作以合成键 `device:<alias>`
参与 broker 互斥，该键不是路径、不落盘、不参与项目解析。

受管设备登记保存在发起端 `GlobalConfig.managed_devices`，记录别名、SSH 目标引用、
固定监听地址、标签与最近置备版本，是 Console 与 MCP 共享的设备 SSOT。Tauri 的
`connections.json` 只保存 UI 偏好与系统凭据库引用：加载连接页时两者合并，MCP
置备出的设备因而会自动出现；删除设备只移除本机登记与连接元数据，不 SSH 到远端
卸载或删除 `~/.dec`。

Console 的 SSH 添加表单只要求一个目标（ssh_config Host、主机名或 `user@host`）：
先通过本机 `dec-server` 探测；未安装时要求用户真实键入目标进行首次置备确认；成功后
保存并自动连接。连接时先调用本机 `ensure_remote_service`，就绪后再建立到固定端口
`47653` 的隧道。连接前尚未进行 Bitwarden 解锁，因此 servicehost 对持有效本机
transport token 的请求设有精确 pre-auth 白名单，只放行设备探测、置备、拉起与清单；
资产、secrets 和仓库配置仍要求 `InstanceUnlocked`。

MCP 对外提供 `dec_provision_remote`，参数以 `ssh_target` 为核心并要求
`confirmed=true`；其内部仍走同一个 `provision_remote_host` operation，而非复制安装逻辑。

### 5. MCP 合并策略

MCP 采用非覆盖式合并：

- Vault 条目以 `dec-{name}` 写入 IDE MCP 配置
- 用户非 `dec-*` 条目保持不变
- 不再托管的 `dec-*` 条目会被清理

### 6. freshness 被动检查

`internal/freshness/` 在后台检查远端 Vault 是否有新提交。实现位于 `internal/freshness/` 与 hidden 子命令 `__freshness-check`：

- 分离子进程执行 fetch，不阻塞 Console 主流程
- cache：`~/.dec/local/freshness-result.<sha1>.json`，24h TTL
- lock：`~/.dec/local/freshness.lock`，busy 时静默 skip
- pull 成功后清 cache，避免误报

### 7. 变量替换

pull 后、从 cache 安装到 IDE 目录之后执行，仅作用于 **非敏感** 模板。优先级（由高到低）：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `.dec/vars.d/*.yaml`（按文件名字典序合并；主文件覆盖同名键）
4. `~/.dec/local/vars.yaml` 中的 `vars`

私密 env（如 `VIKUNJA_API_TOKEN`）由独立 `dec-exec` 从 `.secrets/bundles/<name>/.env/*.env` 注入子进程，**不通过**占位符替换注入。未定义占位符保留原样并通过 Reporter 提示。

## 模块划分

### `cmd/`

命令行入口层：

- `root.go`：根命令、最小 CLI、`dec --version`
- `freshness_check.go`：hidden 子命令，供 freshness 后台 worker 使用
- `output.go`：输出辅助

### `internal/app/`

用例层，编排 repo/config/ide 为可复用操作：

- `project.go`：project init、项目配置写入
- `overview.go`：概览数据（供 Console）
- `assets.go`：Bundles 页资产选择与持久化
- `operations.go`：pull/push/remove 编排
- `settings.go`：Settings 页仓库连接与全局配置
- `vault_bundle.go`：bundle 解析与合成
- `events.go`：`Reporter` / `OperationEvent` 事件模型

### `internal/config/`

- `global.go`：全局配置、有效 IDE / editor 解析
- `project.go`：项目级 `.dec/config.yaml` 与 `.dec/vars.yaml`

`ProjectConfigManager` 的读写入口一律要求**真实的项目根**：空 `projectRoot`（用户平面）返回 `ErrProjectRootRequired`，`.dec/` 解析后等于 Dec 根目录时报错。两者都会让项目配置写到全局配置那个文件上，见 [0015](decisions/0015-project-config-boundary.md)。

### `internal/repo/`

Git 仓库连接、bare repo 管理、事务 worktree。

### `internal/ide/`

IDE 抽象层，区分项目级输出目录与用户级内置资产安装目录。

### `internal/assets/`

内置 skill 内容（`dec`、`dec-extract-asset`），通过 embed 打包进二进制。

### `internal/types/`

全局配置、Project、ProjectConfig、资产列表、Bundle、MCP 配置等结构体。

### `internal/vars/`

变量文件加载、占位符提取与替换。

### `internal/version/`、`internal/update/`、`internal/freshness/`

版本信息、自更新、远端新鲜度检查。

## 关键设计点

### Console 驱动

日常交互通过 Console 完成；Agent / CI 经 `dec-mcp` 或服务 API 调用 `internal/app/`。

### 旧 Project > Bundle（仅迁移背景）

- **Project**（vault `projects/`）：声明启用哪些 bundle，跨机器共享
- **Bundle**（`bundles/<name>/` + Bitwarden secrets bundle）：公开与私密资产的组织单位
- 本地 `.dec/config.yaml` 引用 project，同步 `enabled_bundles` 供 pull 解析

### 多项目支持

一个仓库可含多个顶层项目；项目绑定一个家项目，家项目的 `requires` 只直接引用其它项目的
`public/project`。

### 托管范围有限

Dec 只管理 `dec-*` 产物，不修改用户手工维护的非托管内容。

### 基于文件系统的真实状态

Vault project 与 bundle 以目录和 YAML 文件直接组织，代码扫描真实目录发现状态。

### 项目 private secrets 同构

- 存储根分离：Git 项目 → `.dec/cache/<p>/`；Secure Note → `.secrets/<p>/`；SSH Key
  按 user/project 副作用域分别落地
- SyncTarget：Bitwarden `<p>/private/<plane>` ↔ 对应项目本地 secrets 根；Note 名 =
  相对同步根路径
- Pull：按家项目 / direct requires 或用户启用项目 → Git 四象限 → Bitwarden private →
  Git/BW 同路径与落地边界校验 → 独立落地 + IDE 渲染
- MCP 经独立 `dec-exec` 注入 `.env/*.env`；不再依赖 mise 落地路径
- Schema：`Project`（`schema/dec/v1/projects.proto`）、`BundleBinding`、`SecretsConfig`（`schema/secrets/v1/`）
- Console：同步页一次 pull；资产页选 bundle；设置页配置 Bitwarden

## 已知限制

### CodeBuddy MCP 路径

CodeBuddy MCP 位于项目根 `.mcp.json`。Codex 位于 `.codex/config.toml`。

### 文件权限

复制时使用固定权限位，不保留源文件权限。

### remove 按名称查找

多个 Vault 下存在同名同类型资产时，按仓库扫描顺序命中第一个。

### 测试覆盖

repo/ide 抽象与变量处理已有测试；Console 布局由 `client/tests` 守住。
