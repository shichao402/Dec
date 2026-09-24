# Dec

Dec 是一个个人 AI 知识仓库工具。

它把你在 Cursor、CodeBuddy 等 IDE 中积累的 Skills、Rules、MCP 配置统一保存到一个个人仓库（Git 仓库）里。然后你可以在不同项目中快速获取这些资产，实现跨项目、跨机器复用自己的 AI 资产，而不是在每个仓库里重复维护一份。

## 这是什么问题的解法

很多团队和个人都会遇到这些问题：

- 常用 Skill 只能留在某一个项目里，难以跨项目复用
- Rule 散落在不同仓库中，风格难以统一
- MCP 配置复制粘贴多次，容易漂移
- 项目里既想复用资产，又不想直接提交 IDE 生成副本

Dec 的解决方案：

- 个人维度：在 Console **设置** 页连接你的资产仓库
- 项目维度：Console **引导 / 项目** 初始化 project；资产页调整 bundle；**更新** 页安装订阅
- IDE 维度：Dec 自动将资产部署到配置的 IDE 目录
- 私密维度：Bitwarden folder ↔ 项目 **`.secrets/`** 同步根（project / bundle 同构）；env 经独立 `dec-exec` 注入；SSH Key 落地机器级 `~/.ssh/`，均不进 `.dec/`

> **交互入口**：打开 Dec Console（`client/`）。根 `dec` CLI 已移除。

## 核心概念

### 1. 三套存储

| 存储 | 写谁 | 装什么 |
|------|------|--------|
| **注册表** | 提供方 CI → Dec 仓 orphan 分支 `registry`，tag `registry/<项目>/<v>` | relkit 等已发布的项目 |
| **个人私仓** | 人 `git push` / Console 对私仓提交 | 你自己要留的 Git 资产 |
| **Bitwarden** | Console 认证后由 `dec-server` | 密钥，不进任何 Git |

消费仓用一张 `requires` 表声明订阅。值是订阅版本：`latest` 或某个 `v*`，都从注册表安装。

### 2. 消费配置 `requires`

写在消费仓 `.dec/config.yaml`（提交进该仓）。本机 global 平面写在 `~/.dec/config.yaml`。
这是唯一的消费声明，见 [ADR 0029](Documents/decisions/0029-single-consumer-requires.md)。

```yaml
requires:
  relkit: v0.3.20       # 订阅版本：固定在这一版
  tencent-cloud: latest # 订阅版本：跟随注册表最新 tag
```

订阅版本只接受 `latest` 或 `v*`。指向不存在或已被 purge 的版本会报错，不回落。`latest` 跳过已 yank 的 tag。
在 Console 项目页 / Global 资产页的「订阅」面板勾选保存即写这张表。

`install` 按 `requires` 从 registry 重画 IDE 目录。`.dec/cache` 只是只读下载缓存。

### 3. 资产部署

Console **更新** 页只做远端到本地：跨工作区多选订阅，预览后安装。个人 Git 与密钥在项目 / Global 资产页分别写回。注册表里的项目禁止 `dec_push`；临时修改已安装内容时，从具体项目页进入下级页「**本地覆写**」创建覆写并关联源仓 PR/Issue。

Dec 部署出来的资产会以 `dec-` 前缀命名，例如：

- `.cursor/skills/dec-create-api-test/`
- `.cursor/rules/dec-my-rule.mdc`
- `.cursor/mcp.json` 中的 `dec-postgres-tool`

### 4. 支持的 IDE

| IDE | Skills 路径 | Rules 路径 | MCP 配置 |
|-----|-----------|----------|---------|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Claude | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Codex | `.codex/skills/` | `.codex/rules/` | `.codex/config.toml` |

更详细的使用语义见 `internal/assets/dec/SKILL.md`，实现与存储结构见 [Documents/ARCHITECTURE.md](Documents/ARCHITECTURE.md)。

Codex MCP 写入 `.codex/config.toml` 的 `[mcp_servers.<name>]` 段。

## 快速开始

### 1. 安装 Console

从 [Dec 发布页](https://update.firoyang.com/dec.html) 下载当前平台的 **Dec Console** 并打开。
用户只安装面板；首次连接本机或 SSH 设备时，Console 会按自身版本检查并初始化目标端运行时，
无需另外下载二进制，也不用跑 `install.sh` / `install.ps1`。

开发者若要从源码跑面板或把当前源码装到本机开发用 Console，见下方「从源码跑 Console」。

### 2. 打开 Console

连接本机或远端 `dec-server`；目标端未安装时由连接流程检查并初始化。

Console 主要页面：

| 页面 | 用途 |
|------|------|
| 连接 | 本机 / SSH 远端、探测与置备 |
| 认证 | 按需完成 Bitwarden Authenticate |
| 概览 / 引导 | 项目概览、建议下一步、project 初始化 |
| 项目 / 资产 | 浏览资产、选择 bundle、保存 enabled |
| 更新 | 多选订阅，预览并安装到 Global / 项目 |
| 删除 | 列远端与本机库存、勾选删除；密钥进 Bitwarden 回收站可恢复 |
| 设置 | Console 更新；仓库、Bitwarden、全局 IDE、服务与清理 |

Console Authenticate 是 Bitwarden **唯一人工认证入口**。本机交互 MCP 缺 session 时会
自动拉起或聚焦 Console 并等待；管理远端设备时也在当前 Console 输入，远端主机无需桌面。
CI、测试和其他非交互环境不会自动弹 Console，而是收到结构化错误。`DEC_BW_PASSWORD`
仍可用于受控的程序化认证。session、vault key 与 2FA 中间态只在内存、不落盘。

### 3. 首次使用流程

1. **设置** → 连接个人 Git 仓库 URL
2. **设置** → 配置本机 IDE（安装 Dec 内置 Skills）
3. **引导 / 项目** → 初始化 project（**自动匹配** vault 中同名 `projects/<目录名>.yaml`，或选择/新建）
4. **更新** → 选择订阅，预览并安装

### 4. 变量与占位符

拉取时若资产模板包含 `{{VAR_NAME}}` 占位符，Dec 按以下优先级替换：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `.dec/vars.d/*.yaml` 中的 `vars`（按文件名字典序合并，主文件覆盖同名键）
4. `~/.dec/local/vars.yaml` 中的机器级变量

私密 env 从 `.secrets/**/.env/*.env` 读取，经独立 `dec-exec` 注入子进程（MCP 安装时自动包装），不通过模板占位符注入。未定义的公开占位符会保留原样，并在拉取时提示。可在 Console 项目设置里编辑 `.dec/vars.yaml`。

### 5. 发布与新增资产

提供方项目可在 `.dec/config.yaml` 的 `provides` 中登记作者目录
`skills/`、`commands/`、`rules/`、`mcp/` 中的官方资产，由提供方 CI 发布到 Dec registry。
这四个目录在新项目中默认位于 `DecAssets/`，项目页可用 `provides_root` 整组挪走，
避免与业务目录撞名；基准点只影响本地落点，远端布局不变。已有 `provides` 但没有
该字段的旧项目继续按仓库根解释。
来源、类型、名称和目标路径均由项目页的资产选择派生，不要求手写路径。
`provides` 只登记 Git 资产：secrets 由 `.secrets/<项目>` 的 SyncTarget 规则同步，
不在这里声明。

新增资产流程：

1. 在产品仓中创建并提交作者文件
2. 在项目页“我提供的资产”里勾选扫描到的候选
3. 修改产品版本并打 `v*` tag，由 CI 执行 `publish-provides`

提供方 CI 推荐直接使用 Dec 的复合 Action：

```yaml
- uses: shichao402/Dec/.github/actions/publish-provides@main
  with:
    project-root: ${{ github.workspace }}
    ref: ${{ github.ref_name }}
    registry-ssh-key: ${{ secrets.DEC_REGISTRY_SSH_KEY }}
```

Action 优先下载 stable GitHub Release 中 `dist/ci/` 构建出的 `dec-registry-<os>-<arch>`；
尚无该产物时从同一 Action ref 的源码构建。CI 工具不进入 RUP、Console resources 或 `~/.dec/bin`。

`.dec/` 与 `.cursor/`、
`.codex/` 等 IDE 目录都是状态或渲染结果，校验会拒绝把它们声明为 source，
`provides_root` 同样不能指向这类点目录。本机不再把官方 provides 推入任何仓库；
个人 Git 资产仍从 cache 推回设置中的私仓。

## 推荐工作流

### 工作流 A：第一次设置

1. 打开 Dec Console
2. **设置** → 连接仓库、配置 IDE
3. 资产页 → 选择 bundle / 资产并保存
4. **更新** → 预览并安装

### 工作流 B：在新项目中复用

1. 打开 Console 并连接到该项目所在设备
2. **引导 / 项目** → 自动匹配或选择 vault 中同名 project
3. **更新** → 预览并安装

### 工作流 C：更新已有资产

1. 修改 `.dec/config.yaml` 的 `provides[].source` 指向的作者文件
2. 在提供方源仓提交、评审并修改产品版本
3. 打产品 `v*`，由 CI 发布不可变的 `registry/<项目>/<版本>`

### 工作流 D：新增资产

1. 在产品仓选择作者路径并创建文件
2. 项目页登记到“我提供的资产”
3. 随产品版本由 CI 发布

## 程序边界

Dec 没有用户面 CLI。人通过 Console 操作；`dec-server`、`dec-exec` 和单用途
`dec-host-setup` 只作为 Console 管理的运行时组件存在。Agent 使用 `dec-server` 上的 Streamable HTTP。每个组件都支持 `--version`。

## 资产格式要求

资产模板支持 `{{VAR_NAME}}` 占位符，变量名必须以大写字母开头，只能包含大写字母、数字和下划线。

### Skill

Skill 必须是目录，包含 `SKILL.md`。

### Rule

Rule 必须是单个 `.mdc` 文件。

### MCP

MCP 必须是单个 server 片段 JSON，`command` 必填：

```json
{
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres"],
  "env": {
    "DATABASE_URL": "${DATABASE_URL}"
  }
}
```

## 项目目录结构

```
.dec/
├── config.yaml      # requires map（唯一消费声明）+ project_name + provides（提供方）
├── cache/           # 只读下载缓存（按来源/项目/tag）
├── overrides/       # 消费方官方资产本地覆写 + 上游票据元数据
├── vars.yaml        # 项目变量定义
└── vars.d/          # 可选：拆分的变量片段
```

官方快照在 Dec 仓 `registry` 分支。个人 Git 在设置里的 `repo_url`。密钥仍在 Bitwarden。

全局配置位于 `~/.dec/config.yaml`，例如：

```yaml
repo_url: https://github.com/<user>/<your-repo>
registry_url: https://github.com/shichao402/Dec.git

requires:
  relkit: latest

ides:
  - cursor
```

## 故障排查

### 仓库未连接

在 Console **设置** 页连接仓库 URL。

### 订阅被拒

保存订阅时会校验：项目必须已经发布到注册表。
被拒条目连同理由显示在「订阅」面板，检查拼写或先让提供方 CI 发布一版。

### 推送/拉取失败

若出现远端冲突，在 **Run** 页重试即可。Dec 使用临时 worktree，不会留下中间状态。

### secrets bundle 路径冲突

`.dec/` 树与敏感落地路径禁止相交。若 pull 报错，检查 Bitwarden Note 名是否误落在 `.dec/` 下，或 Dec cache 是否占用了敏感目标路径。

## 安装、构建与测试

### 从源码跑 Console

```bash
git clone https://github.com/shichao402/Dec.git
cd Dec
python scripts/build-console.py --prepare-runtime-only
cd client
npm install
npm run tauri dev
```

第一条命令只编译并保留当前平台的 Tauri runtime resources，不打完整安装包；源码 debug 需要先执行一次。要求 Node.js、Go、Rust stable；Windows 需要 WebView2。细节见 [client/README.md](client/README.md)。

### 开发期本机安装

日常改代码要在这台开发机上点开看，用本地 NSIS/DMG 覆盖安装，**不要**当成发版，也**不要**为此打 tag：

```bash
cd Dec
python scripts/build-console.py --deploy
```

`--deploy` 会停掉正在跑的 Console 再静默装上。只出包、自己点安装时用 `--skip-deps`，产物是 `dist/dec-console-<os>-<arch>.<ext>`。增量构建约 3 分钟。安装包内置同版本运行时套件，Console 启动后会释放到 `~/.dec/bin`，因此**不需要**单独部署运行时——两者出自同一次构建，版本天然相等。

内置 `dec` MCP 是 URL `http://127.0.0.1:47654/mcp`，没有单独的 stdio 进程，也不带请求头。本机连接由服务直接放行。若 pull / 更新 / 删除让其它托管 MCP 条目在 `mcp.json` 里发生增删改，Console 会关 → 杀匹配进程 → 再开，并在结果区列出名字；IDE 若仍显示旧工具，再在 MCP 面板手动 Reload。

给别人用的安装包只走 GitHub Actions / RUP（`dev/v*`、`stable/v*`）。只改 Go、还要从源码跑 Console UI 时，用上一节的 `--prepare-runtime-only` + `npm run tauri dev`，不必再打安装包。

### 运行测试

```bash
go test ./...
```

## 平台支持

人面 Console 只发两套：

- Windows `amd64`（x86_64）
- macOS `amd64`（Intel；Apple Silicon 通过 Rosetta 2 运行）

不再发布 Linux Console，也不再发布原生 Apple Silicon（`darwin-arm64`）安装包。SSH 置备仍可从 RUP 拉取对应目标的运行时套件。

## 项目文档

- [Documents/ARCHITECTURE.md](Documents/ARCHITECTURE.md) — 架构设计、vault 结构与模块说明
- [Documents/BUNDLE-SECRETS-MODEL.md](Documents/BUNDLE-SECRETS-MODEL.md) — Dec bundle 与 Bitwarden secrets bundle 同构模型
- [Documents/TUI_ARCHITECTURE.md](Documents/TUI_ARCHITECTURE.md) — TUI 已卸下；人机入口见 Console
- [client/README.md](client/README.md) — 桌面管理客户端
- [schema/dec/v1/README.md](schema/dec/v1/README.md) — Dec 配置 Protobuf schema
- [schema/secrets/v1/README.md](schema/secrets/v1/README.md) — Secrets bundle Protobuf schema
- [Documents/decisions/0022-console-bitwarden-unlock.md](Documents/decisions/0022-console-bitwarden-unlock.md) — Console Bitwarden 人工认证决策
- `internal/assets/dec/SKILL.md` — Dec Skill 的完整使用说明
- `internal/assets/dec-extract-asset/SKILL.md` — 把当前项目能力沉淀为 Dec 资产的内置 Skill

## 许可证

MIT
