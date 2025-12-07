# CursorToolset 架构设计

本文档描述 CursorToolset 的系统架构和核心设计。

## 概述

CursorToolset 是一个用于管理 Cursor AI 工具集的包管理器。

### 设计理念

- **简单** - 像 pip/brew 一样简单：下载、解压、完成
- **安全** - 不执行任何脚本，只做文件分发
- **透明** - 所有包信息公开可查，SHA256 校验

## 角色定义

| 角色 | 说明 |
|------|------|
| **M (Manager)** | CursorToolset 本身，管理工具集的工具 |
| **P (Parent Project)** | 父项目，使用工具集的目标项目 |
| **S (Sub-toolset)** | 子工具集，被管理和安装的具体工具集 |

## 目录结构

### 安装目录

```
~/.cursortoolsets/
├── bin/                     # 可执行文件
│   ├── cursortoolset       # 管理器
│   └── gh-action-debug     # 包暴露的命令（符号链接）
├── repos/                   # 已安装的包
│   ├── github-action-toolset/
│   │   ├── package.json
│   │   ├── rules/
│   │   └── core/
│   └── test-package/
├── cache/
│   ├── packages/           # 下载缓存
│   └── manifests/          # manifest 缓存
└── config/
    ├── registry.json       # 本地包索引
    └── system.json         # 系统配置
```

## 核心流程

### 包安装流程

```
1. 下载 registry.json（从 GitHub Release）
2. 获取包的 repository URL
3. 下载 package.json（从包的 Release）
4. 从 package.json.dist.tarball 下载包
5. 验证 SHA256
6. 解压到 ~/.cursortoolsets/repos/
7. 创建 bin 符号链接（如果有）
```

### Registry 机制

```json
{
  "version": "2",
  "packages": [
    {
      "name": "github-action-toolset",
      "repository": "https://github.com/user/repo"
    }
  ]
}
```

管理器根据 repository 自动组装 URL：
- 最新版本：`{repo}/releases/latest/download/package.json`
- 特定版本：`{repo}/releases/download/v1.0.0/package.json`

### package.json 规范

```json
{
  "name": "my-toolset",
  "version": "1.0.0",
  "dist": {
    "tarball": "my-toolset-1.0.0.tar.gz",
    "sha256": "abc123...",
    "size": 12345
  },
  "bin": {
    "mytool": "bin/mytool"
  }
}
```

**关键点**：`dist.tarball` 使用相对路径，管理器自动解析完整 URL。

## 模块设计

### cmd/ - 命令行

| 文件 | 命令 | 说明 |
|------|------|------|
| `install.go` | `install` | 安装包 |
| `uninstall.go` | `uninstall` | 卸载包 |
| `list.go` | `list` | 列出包 |
| `search.go` | `search` | 搜索包 |
| `info.go` | `info` | 查看详情 |
| `update.go` | `update` | 更新 |
| `clean.go` | `clean` | 清理 |
| `init.go` | `init` | 初始化包项目 |
| `version.go` | `version` | 版本管理 |
| `registry.go` | `registry` | 索引管理 |

### pkg/ - 核心包

| 包 | 说明 |
|---|------|
| `config` | 配置管理 |
| `installer` | 安装器，处理下载、校验、解压 |
| `registry` | 包索引管理 |
| `paths` | 路径管理 |
| `types` | 类型定义 |
| `version` | 版本管理 |

## 配置系统

### 配置优先级

1. 环境变量
2. 用户配置 (`settings.json`)
3. 系统配置 (`system.json`)
4. 内置默认值

### 关键配置

| 配置 | 说明 |
|------|------|
| `repo_owner` | GitHub 仓库 owner |
| `repo_name` | GitHub 仓库名 |
| `registry_url` | 包索引 URL |
| `update_branch` | 更新分支 |

## 安全设计

### SHA256 校验

每个包必须提供 SHA256 校验和，安装时验证：

```go
// 下载后验证
actualHash := sha256.Sum256(data)
if actualHash != expectedHash {
    return errors.New("SHA256 校验失败")
}
```

### 不执行脚本

- 不执行 `postinstall` 脚本
- 不执行任何用户代码
- 只做文件分发

## 扩展点

### 添加新的包源

修改 `pkg/registry/` 支持新的源类型。

### 添加新的命令

在 `cmd/` 下创建新文件，注册到 `RootCmd`。

### 自定义安装行为

修改 `pkg/installer/` 中的安装逻辑。

## 相关文档

- [开发指南](DEVELOPMENT.md)
- [测试指南](TESTING.md)
- [包开发指南](../public/package-dev-guide.md)
