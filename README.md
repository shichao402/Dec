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

> **交互入口**：打开 Dec Console（`client/`）。根 `dec` CLI 已移除。

## 核心概念

### 1. 三套存储

| 存储 | 写谁 | 装什么 |
|------|------|--------|
| **官方注册表** | 提供方 CI → Dec 仓 orphan 分支 `registry`，tag `registry/<项目>/<v>` | relkit 等官方发行物 |
| **个人私仓** | 人 `git push` / Console 对私仓提交 | 你自己要留的 Git 资产 |
| **Bitwarden** | Console 认证后由 `dec-server` | 密钥，不进任何 Git |

消费仓用 `requires` 声明官方依赖。个人资产由本机/私仓启用列表决定，不和官方 `requires` 混写。

### 2. 消费配置 `requires`

写在消费仓 `.dec/config.yaml`（提交进该仓）。本机 global 平面写在 `~/.dec/config.yaml`。

```yaml
requires:
  relkit: v0.3.20
  tencent-cloud: latest
```

只接受精确版本或 `latest`。指向不存在或已被 purge 的版本会报错，不回落。`latest` 跳过已 yank 的 tag。

`install` 按 `requires` 从 registry 重画 IDE 目录。`.dec/cache` 只是只读下载缓存。

### 3. 资产部署

Console **同步** 页：官方走 registry 安装；个人 Git 与密钥仍走私仓 / Bitwarden。官方禁止 `dec_push`；改官方安装物用草稿 + 源仓 PR/Issue。

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
| 同步 | 拉取、推送（Global 与项目）、密钥清单（只读元数据） |
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
4. **同步** → 拉取 project 内 bundle 到当前项目 IDE 目录

### 4. 变量与占位符

拉取时若资产模板包含 `{{VAR_NAME}}` 占位符，Dec 按以下优先级替换：

1. `.dec/vars.yaml` 中的 `assets.<type>.<name>.vars`
2. `.dec/vars.yaml` 中的 `vars`
3. `.dec/vars.d/*.yaml` 中的 `vars`（按文件名字典序合并，主文件覆盖同名键）
4. `~/.dec/local/vars.yaml` 中的机器级变量

私密 env 从 `.secrets/**/.env/*.env` 读取，经独立 `dec-exec` 注入子进程（MCP 安装时自动包装），不通过模板占位符注入。未定义的公开占位符会保留原样，并在拉取时提示。可在 Console 项目设置里编辑 `.dec/vars.yaml`。

### 5. 推送与新增资产

项目可在 `.dec/config.yaml` 的 `provides` 中登记作者目录
`skills/`、`commands/`、`rules/`、`mcp/` 中的资产，经 Git vault 分发。
这四个目录在新项目中默认位于 `DecAssets/`，项目页可用 `provides_root` 整组挪走，
避免与业务目录撞名；基准点只影响本地落点，远端布局不变。已有 `provides` 但没有
该字段的旧项目继续按仓库根解释。
来源、类型、名称和目标路径均由项目页的资产选择派生，不要求手写路径。
`provides` 只登记 Git 资产：secrets 由 `.secrets/<项目>` 的 SyncTarget 规则同步，
不在这里声明。

新增资产流程：

1. 在产品仓中创建并提交作者文件
2. 在项目页“我提供的资产”里勾选扫描到的候选
3. 在 **同步** 二级页预览本地与远端，再自动同步或单向 Pull / Push

配置了 `provides` 的项目只从作者目录推送；`.dec/` 与 `.cursor/`、
`.codex/` 等 IDE 目录都是状态或渲染结果，校验会拒绝把它们声明为 source，
`provides_root` 同样不能指向这类点目录。
未配置 provides 的旧项目暂时保留 cache push
兼容路径。

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

1. 修改 `.dec/config.yaml` 的 `provides[].source` 指向的作者文件
2. **同步** 页查看 side-by-side 预览并自动同步
3. 有 Git 冲突时在保留的工作副本中解决，再继续推送

### 工作流 D：新增资产

1. 在产品仓选择作者路径并创建文件
2. 项目页登记到“我提供的资产”
3. **同步** 页预览并推送

## 程序边界

Dec 没有用户面 CLI。人通过 Console 操作；`dec-server`、`dec-mcp`、`dec-exec` 和单用途
`dec-host-setup` 只作为 Console/Agent 管理的运行时组件存在。每个组件都支持 `--version`。

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
├── config.yaml      # requires map + provides（提供方）+ 个人启用
├── cache/           # 只读下载缓存（按来源/项目/tag）
├── drafts/          # 官方安装物的本地草稿
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

### 开发期本机安装

日常改代码要在这台开发机上点开看，用本地 NSIS/DMG 覆盖安装，**不要**当成发版，也**不要**为此打 tag：

```bash
cd Dec
python scripts/build-console.py --deploy
```

`--deploy` 会停掉正在跑的 Console 再静默装上。只出包、自己点安装时用 `--skip-deps`，产物是 `dist/dec-console-<os>-<arch>.<ext>`。增量构建约 3 分钟。安装包内置同版本运行时套件，Console 启动后会释放到 `~/.dec/bin`，因此**不需要**单独部署运行时——两者出自同一次构建，版本天然相等。Cursor 里的 `dec-mcp` 需重载 MCP 才用上新二进制。

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
