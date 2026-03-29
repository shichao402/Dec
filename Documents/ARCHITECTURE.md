# Dec 架构设计

本文档描述当前代码库的真实实现：**Dec 是一个个人 AI 资产 Vault 与多 IDE 同步工具**，而不是传统的规则包管理器。

## 概览

Dec 解决的是“AI 资产如何跨项目、跨设备复用”的问题。

它把 Skills、Rules、MCP 配置保存在个人 Vault（Git 仓库）中，再由项目里的声明式配置决定哪些资产要同步到哪些 IDE。

当前源码主线只有三类命令：

- `dec init`
- `dec sync`
- `dec vault ...`

## 目录结构

### 全局目录

```text
~/.dec/
├── config.yaml              # 全局配置（如 vault_source）
├── config/
│   └── system.json          # 安装脚本下载的系统配置
└── vault/                   # 个人 Vault（Git 仓库）
```

如果设置了 `DEC_HOME`，则以上目录会迁移到 `DEC_HOME` 下。

### 项目目录

```text
.dec/config/
├── ides.yaml                # 当前项目要同步到哪些 IDE
└── vault.yaml               # 当前项目声明依赖哪些 Vault 资产
```

### IDE 托管输出

| IDE | Skills | Rules | MCP |
|---|---|---|---|
| Cursor | `.cursor/skills/` | `.cursor/rules/` | `.cursor/mcp.json` |
| CodeBuddy | `.codebuddy/skills/` | `.codebuddy/rules/` | `.mcp.json` |
| Windsurf | `.windsurf/skills/` | `.windsurf/rules/` | `.windsurf/mcp.json` |
| Trae | `.trae/skills/` | `.trae/rules/` | `.trae/mcp.json` |

Dec 托管的产物统一使用 `dec-` 前缀，以便与用户手工维护的内容区分。

## 核心流程

### `dec init`

1. 创建 `.dec/config/ides.yaml`
2. 创建 `.dec/config/vault.yaml`
3. 如有需要，尝试安装 Dec Skill

### `dec vault init`

1. 连接已有 Vault，或创建新的 GitHub 私有仓库
2. 在本地 `DEC_HOME/vault` 建立 Git 工作目录
3. 把 Vault 来源写入全局 `config.yaml`

### `dec vault save`

1. 校验本地 Skill / Rule / MCP 资产格式
2. 保存到 Vault 工作目录
3. 提交到 Vault 本地 Git 仓库
4. 尝试更新当前项目的追踪信息

### `dec vault pull`

1. 从 Vault 解析指定资产
2. 同步到当前项目配置的全部 IDE
3. 自动把资产名称写入 `.dec/config/vault.yaml`
4. 更新 `.dec/tracking.json`

### `dec sync`

1. 读取 `.dec/config/ides.yaml`
2. 读取 `.dec/config/vault.yaml`
3. 打开本地 Vault，并尝试刷新远端状态
4. 将声明的 Skills / Rules / MCP 同步到目标 IDE
5. 更新 `.dec/tracking.json`

### `dec update`

1. 检查远程版本信息（GitHub Release）
2. 与当前版本号比较
3. 如果有新版本，下载对应平台的二进制
4. 备份现有二进制，并替换为新版本
5. 失败时恢复原始二进制
6. 更新检查状态文件（每天最多检查一次）

## 模块划分

### `cmd/`

命令行入口层，负责参数解析与用户输出。

- `root.go`：根命令与版本信息
- `init.go`：初始化项目配置
- `sync.go`：同步 Vault 资产到 IDE
- `update.go`：版本检查和更新
- `vault.go`：Vault 子命令集合

### `pkg/config/`

配置读写与渲染。

- `project_v2.go`：项目级 `ides.yaml` / `vault.yaml`
- `global.go`：全局 `config.yaml`
- `system.go`：安装脚本使用的系统配置

### `pkg/vault/`

Vault 核心实现。

负责：

- 初始化 / 打开 Vault（Git 仓库管理）
- 资产保存、列出、搜索、拉取、推送
- 索引管理（`vault.json`）
- 追踪数据维护（`.dec/tracking.json`）
- 文件 / 目录复制与哈希计算
- GitHub 仓库创建（集成 `gh` CLI）

关键包：
- `vault.go`：Vault 主体和资产操作
- `index.go`：VaultIndex 索引管理
- `tracking.go`：追踪数据和变更检测
- `git.go`：Git 操作封装

### `pkg/service/`

同步编排层。

`sync_v2.go` 负责：

- 解析项目声明
- 备份当前 IDE 中已有的 `dec-*` 托管内容
- 清理旧托管内容
- 写入新的 Skill / Rule / MCP
- 失败时回滚恢复
- 更新追踪状态

### `pkg/ide/`

IDE 抽象层，封装不同 IDE 的目录结构差异。

### `pkg/paths/`

统一管理 `DEC_HOME`、项目配置目录和 IDE 输出路径。

### `pkg/types/`

声明项目配置、MCP 配置等结构体。

### `pkg/version/`

版本号管理和比较。

负责：

- 从 `version.json` 加载版本信息
- Semantic versioning 版本号比较
- 检查是否需要更新
- 编译时版本注入
- Git 标签版本回退

### `pkg/update/`

版本检查和自更新功能。

负责：

- 检查 GitHub Release 上的最新版本
- 后台版本检查（24小时一次）
- 下载新版本二进制
- 执行自更新（备份、替换、恢复机制）
- 更新检查状态管理

## 关键设计点

### 托管范围有限

Dec 只会覆盖自己托管的 `dec-*` 内容，不会删除用户手工维护的非托管资产。

### MCP 合并保留用户配置

同步 MCP 时：

- 当前声明的 `dec-*` 条目会被更新
- 旧的、未声明的 `dec-*` 条目会被移除
- 用户自己维护的非 `dec-*` 条目会被保留

### 同步失败可恢复

`synchronize -> clean -> write` 过程中如果任一步失败，Dec 会尝试恢复目标 IDE 中原有的托管资产，避免同步把环境留在半完成状态。

## 已实现但文档未重点说明的命令

### `dec update`

检查并更新 Dec 到最新版本。

- 后台自动检查：每次非 update/version 命令执行时，会在后台检查新版本并输出提示
- 手动检查：`dec update` 主动检查并下载最新二进制
- 版本检查 URL：通过 `pkg/config` 中的 `GetVersionURL()` 获取，指向 GitHub Release
- 支持平台：Linux (amd64/arm64)、macOS (amd64/arm64)、Windows (amd64)

## 当前边界

以下能力**不在当前实现中**：

- 顶层 `dec list` （只有 `dec vault list`）
- `dec serve`
- `dec publish-notify`
- `technology.yaml` / `mcp.yaml` / `packs.yaml` 项目配置体系
- 依赖 GitHub Actions 的自动发布与自动注册流程

如果后续引入上述能力，应先更新源码，再同步更新本文档与根 `README.md`。

## 已知问题与限制

### CodeBuddy MCP 配置路径

CodeBuddy 的 MCP 配置文件位置为项目根目录的 `.mcp.json`，与其他 IDE 的 `.{ide}/mcp.json` 结构不同。这是 CodeBuddy 自身的特殊设计，已通过 IDE 抽象层正确处理（`pkg/paths/paths.go` 中的冗余路径函数已删除）。

### 文件权限保留

**当前限制**：`CopyDir()` 和 `copyFile()` 使用固定的权限位（0755/0644），不保留原始文件权限。对于需要特殊权限的脚本或配置文件可能有影响。

注：符号链接已支持处理（保留链接而非跟随目标）。

## 已知技术债务

### 已修复项（v1.x）

- ~~P0: IDE 路径死代码~~ → 已删除 `pkg/paths/paths.go` 中未使用的 IDE 路径函数
- ~~P0: Git Push 错误被忽略~~ → `InitCreate()` 现在返回 `[]string` 警告
- ~~P1: IDE 名称无验证~~ → `pkg/ide/registry.go` 新增 `IsValid()`，`project_v2.go` 初始化时校验
- ~~P1: MCP 合并边界条件~~ → `sync_v2_test.go` 补充 5 个边界测试用例
- ~~P1: 文件权限和符号链接~~ → `copyFile()` 使用显式 0644 权限，`CopyDir()` 支持符号链接
- ~~P2: 配置示例硬编码~~ → 提取为包级变量 `sampleSkills`/`sampleRules`/`sampleMCPs`
- ~~P2: Rule 描述解析反馈~~ → 调用处添加注释明确语义

### 仍存在的债务

**哈希计算中的错误处理**
- 位置：`pkg/vault/tracking.go`
- 问题：`filepath.Rel()` 的错误被忽略
- 建议修复：改为返回错误或添加防护检查

**文件权限不保留原始值**
- 位置：`pkg/vault/vault.go` copyFile
- 问题：固定使用 0644，不保留源文件权限
- 影响：复制可执行脚本时可能丢失执行权限
- 建议修复：读取源文件 `os.Stat()` 后使用相同权限位
