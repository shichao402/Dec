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
- 项目维度：Console **引导 / 项目** 初始化 project；资产页调整 bundle；**同步** 页拉取到项目
- IDE 维度：Dec 自动将资产部署到配置的 IDE 目录
- 私密维度：Bitwarden folder ↔ 项目 **`.secrets/`** 同步根（project / bundle 同构）；env 经独立 `dec-exec` 注入；SSH Key 落地机器级 `~/.ssh/`，均不进 `.dec/`

> **交互入口**：打开 Dec Console（`client/`）。`dec` CLI 仅保留 `--version` 与内部 hidden 命令。

## 核心概念

### 1. Project、Bundle 与资产仓库

在 Console **设置** 页连接你的资产仓库，底层是一个 Git 仓库。

配置按 **Project > Bundle** 两层组织：

| 层级 | 位置 | 说明 |
|------|------|------|
| **Project** | vault `projects/<name>.yaml` | 声明启用哪些 bundle；跨机器共享 |
| **Bundle** | Git Vault `bundles/<name>/` + Bitwarden secrets bundle | Skills、Rules、MCP 及对应私密文件 |

```text
<repo>/
├── projects/my-app.yaml    # bundles: [vikunja, helloworld]
└── bundles/
    ├── vikunja/
    │   ├── bundle.yaml
    │   ├── skills/
    │   ├── rules/
    │   ├── mcp/
    │   └── commands/
    └── default/
        └── skills/helloworld/...
```

同名 Bitwarden folder 与 `.secrets` 同步根镜像；Secure Note 名 = 相对同步根路径（如 `.env/vikunja.env` → `.secrets/bundles/vikunja/.env/vikunja.env`），SSH Key Item 名 `.sshkey/<实例>` pull 到 `~/.ssh/dec_<bundle>_<实例>`；公开资产仍在 `.dec/cache/`。MCP 安装时包一层 `dec-exec --bundle …`。详见 [Documents/BUNDLE-SECRETS-MODEL.md](Documents/BUNDLE-SECRETS-MODEL.md) 与 [ADR 0002](Documents/decisions/0002-secrets-synctarget-root.md)。

### 2. 项目配置

**Vault project**（真相源）示例：

```yaml
name: my-app
description: 我的应用项目
bundles:
  - vikunja
  - helloworld
ides:
  - cursor
```

**本地** `.dec/config.yaml` 引用 vault project：

```yaml
version: v2

project_name: my-app       # 引用 projects/my-app.yaml

ides:                      # 可选：机器级 IDE 覆盖
  - cursor

enabled_bundles:           # 唯一的资产启用入口；从 vault project 同步，Bundles 页可调整
  - vikunja
  - helloworld
```

资产只能按 bundle 启用，成员随 bundle 一并下发；早期的单资产字段（`available` / `enabled`）已移除，加载旧配置时会折叠成 `enabled_bundles` 并回写。

- Console **引导 / 项目** 初始化 project（自动匹配、选择或新建）
- Console 资产页扫描仓库、勾选 bundle
- **同步** 页按 project 的 bundle 列表拉取 Dec + secrets bundle

### 3. 资产部署

Console **同步** 页将资产部署到当前项目的配置 IDE。

Dec 部署出来的资产会以 `dec-` 前缀命名，例如：

- `.cursor/skills/dec-create-api-test/`
- `.cursor/rules/dec-my-rule.mdc`
- `.cursor/mcp.json` 中的 `dec-postgres-tool`

一次 **pull bundle** 会先拉 Dec Git bundle 到 `.dec/cache/`，再自动拉 Bitwarden secrets bundle（Secure Note → 项目根，SSH Key → `~/.ssh/`）；项目根零路径重叠校验后渲染 IDE。

### 4. 支持的 IDE

| IDE | Skills 路径 | Rules 路径 | MCP 配置 |
|-----|-----------|----------|---------|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Claude | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Claude Internal | `.claude/skills/` | `.claude/rules/` | `.claude/mcp.json` |
| Codex | `.codex/skills/` | `.codex/rules/` | `.codex/config.toml` |
| Codex Internal | `.codex/skills/` | `.codex/rules/` | `.codex/config.toml` |

更详细的使用语义见 `internal/assets/dec/SKILL.md`，实现与存储结构见 [Documents/ARCHITECTURE.md](Documents/ARCHITECTURE.md)。

说明：`claude-internal` 的项目级部署复用 `.claude/`，用户级目录为 `~/.claude-internal/`。`codex-internal` 的项目级部署复用 `.codex/`，用户级目录为 `~/.codex-internal/`。Codex MCP 写入 `.codex/config.toml` 的 `[mcp_servers.<name>]` 段。

## 快速开始

### 1. 安装 Console

从 [Dec 发布页](https://update.firoyang.com/dec.html) 下载当前平台的 **Dec Console** 并打开。
用户只安装面板；首次连接本机或 SSH 设备时，Console 会按自身版本检查并初始化目标端运行时，
无需另外下载二进制，也不用跑 `install.sh` / `install.ps1`。

开发者若要从源码跑面板，见下方「从源码跑 Console」。

### 2. 打开 Console

连接本机或远端 `dec-server`；目标端未安装时由连接流程检查并初始化。

Console 主要页面：

| 页面 | 用途 |
|------|------|
| 连接 | 本机 / SSH 远端、探测与置备 |
| 认证 | 按需完成 Bitwarden Authenticate |
| 概览 / 引导 | 项目概览、建议下一步、project 初始化 |
| 项目 / 资产 | 浏览资产、选择 bundle、保存 enabled |
| 同步 | 拉取、推送、移除资产、自更新 |
| 设置 | 连接仓库、Bitwarden、全局 IDE 与 editor |

Console Authenticate 是 Bitwarden **唯一人工认证入口**。本机交互 MCP 缺 session 时会
自动拉起或聚焦 Console 并等待；管理远端设备时也在当前 Console 输入，远端主机无需桌面。
CI、测试和其他非交互环境不会自动弹 Console，而是收到结构化错误。`DEC_BW_PASSWORD`
仍可用于受控的程序化认证。session、vault key 与 2FA 中间态只在内存、不落盘。

### 3. 首次使用流程

1. **设置** → 连接个人 Git 仓库 URL
2. **设置** → 配置本机 IDE（安装 Dec 内置 Skills）
3. **引导 / 项目** → 初始化 project（**自动匹配** vault 中同名 `projects/<目录名>.yaml`，或选择/新建）
4. **同步** → 拉取 project 内 bundle 到当前项目 IDE 目录

### 4. 变量与占位符

拉取时若资产模板包含 `{{VAR_NAME}}` 占位符，Dec 按以下优先级替换：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `.dec/vars.d/*.yaml` 中的 `vars`（按文件名字典序合并，主文件覆盖同名键）
4. `~/.dec/local/vars.yaml` 中的机器级变量

私密 env 从 `.secrets/**/.env/*.env` 读取，经独立 `dec-exec` 注入子进程（MCP 安装时自动包装），不通过模板占位符注入。未定义的公开占位符会保留原样，并在拉取时提示。可在 Console 项目设置里编辑 `.dec/vars.yaml`。

### 5. 推送与新增资产

在 Console **同步** 页推送 `.dec/cache/` 中的修改到远程仓库。secrets bundle 走 Bitwarden API，不进 Git。

新增资产流程：

1. 在 Console 资产页或 `.dec/config.yaml` 中启用 bundle / 资产
2. 在 `.dec/cache/<bundle>/` 下创建对应文件（skills / rules / mcp）
3. 在 **同步** 页执行推送

推送读取源是 `.dec/cache/`，不是 `.cursor/`、`.codex/` 等 IDE 目录。

## 推荐工作流

### 工作流 A：第一次设置

1. 打开 Dec Console
2. **设置** → 连接仓库、配置 IDE
3. 资产页 → 选择 bundle / 资产并保存
4. **同步** → 拉取到项目

### 工作流 B：在新项目中复用

1. 打开 Console 并连接到该项目所在设备
2. **引导 / 项目** → 自动匹配或选择 vault 中同名 project
3. **同步** → 拉取

### 工作流 C：更新已有资产

1. 修改 `.dec/cache/` 中的缓存文件
2. **同步** 页推送
3. 在其他项目的 **同步** 页拉取最新版本

### 工作流 D：新增资产

1. 资产页刷新并启用新 bundle / 资产
2. 编辑 `.dec/cache/<bundle>/` 下文件
3. **同步** 页推送

## 命令参考

Dec 以 Console 为主入口。CLI 仅保留：

| 命令 | 说明 |
|------|------|
| `dec --version` | 显示版本号 |
| `dec`（无参） | 提示改用 Console，不启动 TUI |

内部 hidden 命令（`__freshness-check`、`__service-setup`）供置备与后台 worker 使用，不是用户面。

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
├── config.yaml      # project_name + enabled_bundles + available/enabled
├── cache/           # 资产缓存（pull 写入，push 读取）
├── .version         # 当前 pull 的版本记录
├── vars.yaml        # 项目变量定义
└── vars.d/          # 可选：拆分的变量片段
```

Vault project 声明位于 Git 仓库 `projects/<name>.yaml`。

机器级变量文件位于 `~/.dec/local/vars.yaml`。

全局配置位于 `~/.dec/config.yaml`，例如：

```yaml
repo_url: https://github.com/<user>/<your-repo>

ides:
  - cursor
  - codebuddy

editor: code --wait
```

## 故障排查

### 仓库未连接

在 Console **设置** 页连接仓库 URL。

### 配置校验警告

拉取前会校验 enabled 中的资产是否在 available 中存在。若看到警告，检查拼写或在 **Assets** 页重新扫描。

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

### 从源码构建运行时

```bash
cd Dec
go build -o dec .
```

### 运行测试

```bash
go test ./...
```

## 平台支持

- macOS `amd64` / `arm64`
- Linux `amd64` / `arm64`
- Windows `amd64`

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
