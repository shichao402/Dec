# CursorToolset 架构设计文档

## 概述

CursorToolset 是一个用于管理 Cursor AI 工具集的解决方案。

### 角色定义

- **M (Manager)**: CursorToolset 本身，管理工具集的工具
- **P (Parent Project)**: 父项目，使用工具集的目标项目
- **S (Sub-toolset)**: 子工具集，被管理和安装的具体工具集

## 当前存在的问题

### 1. 目录结构混乱

**当前设计**：
```
P/ (父项目)
├── toolsets/           # M 安装 S 到这里
│   └── github-action-toolset/
├── .cursor/
│   └── rules/          # S 拷贝规则文件到这里
│       └── github-actions/
└── scripts/
    └── toolsets/       # S 拷贝脚本到这里
```

**问题**：
- `toolsets/` 在项目根目录，与 `.cursor/` 分离
- 如果 M 本身也被安装到 P，会造成路径混乱
- Git 子模块在项目根目录可能污染 P 的仓库

### 2. M 的集成方式不清晰

**场景 1**：M 作为独立工具使用
```bash
# M 安装在系统路径或独立目录
/usr/local/bin/cursortoolset
# 或
~/tools/CursorToolset/cursortoolset
```

**场景 2**：M 被集成到 P 中
```
P/
└── .cursor/
    └── toolsets/
        └── CursorToolset/  # M 安装在这里
            ├── cursortoolset
            └── available-toolsets.json
```

### 3. Git 子模块的使用场景不合理

- 在 `.cursortoolsets/` 中使用 Git 子模块不太合适
- `.cursor/` 目录通常应该被 `.gitignore` 忽略
- 子模块应该只用于开发 M 本身，不应该用于安装 S

## 重新设计的架构

### 架构目标

1. **清晰的目录结构**：所有工具集相关内容都在 `.cursor/` 下
2. **灵活的部署方式**：M 可以作为独立工具或集成到 P
3. **简单的安装方式**：不依赖 Git 子模块
4. **版本管理**：通过 available-toolsets.json 中的版本号管理

### 推荐的目录结构

```
P/ (父项目 - 使用工具集的项目)
├── .cursor/
│   ├── toolsets/
│   │   ├── CursorToolset/           # M (可选，如果 P 集成了 M)
│   │   │   ├── cursortoolset        # M 的可执行文件
│   │   │   ├── available-toolsets.json
│   │   │   └── README.md
│   │   │
│   │   ├── github-action-toolset/   # S1
│   │   │   ├── toolset.json
│   │   │   ├── core/
│   │   │   │   ├── rules/
│   │   │   │   └── tools/
│   │   │   └── README.md
│   │   │
│   │   └── other-toolset/           # S2
│   │       └── ...
│   │
│   └── rules/                       # S 拷贝的规则文件
│       ├── github-actions/
│       │   ├── github-actions.mdc
│       │   ├── debugging.mdc
│       │   └── best-practices.mdc
│       └── other-rules/
│
├── scripts/                         # S 拷贝的脚本文件（可选）
│   └── toolsets/
│       └── github-actions/
│           └── gh-action-debug
│
├── .gitignore
└── ...其他项目文件
```

### 关键设计决策

#### 1. 默认安装路径

**修改前**：
- 默认：`./toolsets/`
- 可配置：`--toolsets-dir`

**修改后**：
- 默认：`./.cursortoolsets/`
- 可配置：`--toolsets-dir`

**理由**：
- 所有 Cursor 相关内容统一在 `.cursor/` 目录下
- 符合 Cursor AI 的使用习惯
- 易于管理和清理

#### 2. 不使用 Git 子模块

**原因**：
1. `.cursor/` 目录通常应该被忽略，不提交到仓库
2. 使用 Git 子模块会让目录结构复杂化
3. 子模块需要 P 是 Git 仓库，限制太多
4. 通过版本号管理更简单灵活

**替代方案**：
- 直接克隆/下载 S 到指定目录
- 通过 available-toolsets.json 中的版本号和 URL 管理
- 支持 Git URL、HTTP URL、本地路径等多种方式

#### 3. M 的两种使用方式

**方式 1：独立工具（推荐用于开发）**
```bash
# 全局安装
go install github.com/firoyang/CursorToolset@latest

# 或本地构建
cd ~/tools/CursorToolset
go build -o cursortoolset

# 使用
cd /path/to/P
cursortoolset install
```

**方式 2：集成到 P（推荐用于分发）**
```bash
# 将 M 复制到 P 的 .cursortoolsets/ 目录
P/
└── .cursor/
    └── toolsets/
        └── CursorToolset/
            ├── cursortoolset
            └── available-toolsets.json

# 使用（在 P 的根目录执行）
.cursortoolsets/CursorToolset/cursortoolset install
```

#### 4. available-toolsets.json 的位置

**查找顺序**：
1. 命令行参数指定的路径
2. 当前目录 `./available-toolsets.json`
3. `.cursortoolsets/CursorToolset/available-toolsets.json`
4. M 安装目录的 `available-toolsets.json`

**理由**：
- 灵活支持多种部署方式
- P 可以自定义自己的 available-toolsets.json
- 回退到 M 自带的配置

## 实现计划

### 阶段 1：修改默认安装路径

```go
// cmd/install.go
if installToolsetsDir == "" {
    // 默认安装到 .cursortoolsets/
    installToolsetsDir = filepath.Join(installWorkDir, ".cursor", "toolsets")
}
```

### 阶段 2：移除 Git 子模块依赖

```go
// pkg/installer/installer.go
// 将 installAsSubmodule 改为 cloneOrDownload
func (i *Installer) cloneOrDownload(toolsetInfo *types.ToolsetInfo, targetPath string) error {
    // 1. 支持 Git URL: 直接克隆
    // 2. 支持 HTTP URL: 下载并解压
    // 3. 支持本地路径: 复制
}
```

### 阶段 3：智能查找 available-toolsets.json

```go
// pkg/loader/loader.go
func GetToolsetsPath(workDir string) string {
    // 1. 当前目录
    // 2. .cursortoolsets/CursorToolset/
    // 3. M 的安装目录
}
```

### 阶段 4：更新文档

- 更新 README.md
- 更新 ARCHITECTURE.md（本文档）
- 添加部署指南
- 添加集成示例

## 使用场景示例

### 场景 1：开发者本地使用

```bash
# 1. 克隆并构建 M
git clone https://github.com/shichao402/CursorToolset.git
cd CursorToolset
go build -o cursortoolset

# 2. 进入项目 P
cd /path/to/my-project

# 3. 使用 M 安装工具集
/path/to/CursorToolset/cursortoolset install

# 结果：工具集安装到 .cursortoolsets/
```

### 场景 2：团队分发（M 集成到 P）

```bash
# 1. 在 P 中创建 .cursortoolsets/ 目录
mkdir -p .cursortoolsets/CursorToolset

# 2. 复制 M 的可执行文件和配置
cp /path/to/CursorToolset/cursortoolset .cursortoolsets/CursorToolset/
cp /path/to/CursorToolset/available-toolsets.json .cursortoolsets/CursorToolset/

# 3. 提交到仓库（可选）
git add .cursortoolsets/CursorToolset/
git commit -m "Add CursorToolset"

# 4. 团队成员克隆 P 后直接使用
git clone https://github.com/team/project.git
cd project
.cursortoolsets/CursorToolset/cursortoolset install
```

### 场景 3：自定义工具集列表

```bash
# P 可以自定义自己的 available-toolsets.json
cat > .cursor/available-toolsets.json << EOF
[
  {
    "name": "my-custom-toolset",
    "githubUrl": "https://github.com/myteam/custom-toolset.git",
    "description": "团队自定义工具集"
  }
]
EOF

# M 会优先使用这个配置
cursortoolset install
```

## 兼容性和迁移

### 迁移旧版本

如果已经使用旧版本（安装到 `./toolsets/`）：

```bash
# 1. 清理旧安装
cursortoolset clean --force

# 2. 重新安装到新位置
cursortoolset install
# 默认会安装到 .cursortoolsets/
```

### 向后兼容

- 仍然支持 `--toolsets-dir` 参数指定自定义目录
- 仍然支持从旧位置 `./toolsets/` 读取（如果存在）

## 总结

### 核心设计原则

1. **统一目录**：所有工具集相关内容在 `.cursor/` 下
2. **简化依赖**：不依赖 Git 子模块
3. **灵活部署**：支持独立工具和集成两种方式
4. **清晰结构**：M、P、S 的关系和职责明确

### 优势

- ✅ 目录结构清晰，易于理解
- ✅ 不污染项目根目录
- ✅ 易于集成到现有项目
- ✅ 支持团队协作和分发
- ✅ 版本管理简单灵活

### 下一步

1. 实现新的目录结构
2. 移除 Git 子模块依赖
3. 更新所有文档
4. 添加迁移指南
5. 更新测试

