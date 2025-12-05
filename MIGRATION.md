# 迁移指南

## 从旧版本迁移到新版本

### 主要变化

#### 1. 默认安装目录变更

**旧版本**：
- 默认安装到 `./toolsets/`
- 使用 Git 子模块方式

**新版本**：
- 默认安装到 `./.cursortoolsets/`
- 使用普通 Git 克隆方式
- 不再依赖 Git 子模块

#### 2. 不再需要 Git 仓库

**旧版本**：
- 必须在 Git 仓库中运行
- 会创建 `.gitmodules` 文件

**新版本**：
- 可以在任何目录运行
- 不再创建 `.gitmodules` 文件

#### 3. 更清晰的目录结构

**新版本目录结构**：
```
your-project/
├── .cursor/
│   ├── toolsets/              # 工具集源码
│   │   └── github-action-toolset/
│   └── rules/                 # 安装的规则文件
│       └── github-actions/
├── scripts/                   # 可选脚本
│   └── toolsets/
└── ...
```

所有 Cursor 相关内容统一在 `.cursor/` 目录下。

## 迁移步骤

### 步骤 1：清理旧版本

```bash
# 如果使用的是旧版本（安装到 ./toolsets/）
cd your-project

# 1. 清理旧的安装
cursortoolset clean --force

# 2. 删除 Git 子模块配置（如果存在）
rm -f .gitmodules
git rm --cached toolsets/github-action-toolset 2>/dev/null || true

# 3. 删除旧的 toolsets 目录
rm -rf toolsets/
```

### 步骤 2：更新到新版本

```bash
# 1. 更新 CursorToolset
cd /path/to/CursorToolset
git pull
go build -o cursortoolset

# 2. 或者重新克隆
cd ~/tools
git clone https://github.com/shichao402/CursorToolset.git
cd CursorToolset
go build -o cursortoolset
```

### 步骤 3：重新安装工具集

```bash
# 回到项目目录
cd your-project

# 使用新版本安装（会自动安装到 .cursortoolsets/）
cursortoolset install

# 验证安装
cursortoolset list
```

### 步骤 4：更新 .gitignore

```bash
# 在项目的 .gitignore 中添加
echo ".cursor/" >> .gitignore
```

## 常见问题

### Q1: 我的项目已经有 toolsets/ 目录，会冲突吗？

A: 不会冲突。新版本默认安装到 `.cursortoolsets/`，与旧的 `toolsets/` 目录完全独立。

### Q2: 我能继续使用旧的安装目录吗？

A: 可以。使用 `--toolsets-dir ./toolsets` 参数指定旧目录：
```bash
cursortoolset install --toolsets-dir ./toolsets
```

但我们强烈建议使用新的默认目录以获得更好的组织结构。

### Q3: 新版本还需要 Git 仓库吗？

A: 不需要。新版本使用普通 Git 克隆，不依赖 Git 子模块，可以在任何目录运行。

### Q4: 我的团队已经在使用旧版本，如何平滑迁移？

A: 可以分步迁移：

1. **阶段 1**：个人开发环境先迁移
```bash
# 每个开发者本地
cursortoolset clean --force
cursortoolset install
```

2. **阶段 2**：更新文档
- 更新团队 Wiki
- 更新 README
- 添加迁移说明

3. **阶段 3**：清理旧配置
```bash
# 提交清理
git rm .gitmodules
git commit -m "chore: migrate to new CursorToolset structure"
```

### Q5: .cursor/ 目录应该提交到 Git 吗？

A: **不应该**。建议在 `.gitignore` 中添加：
```gitignore
.cursor/
```

理由：
- `.cursor/` 是本地生成的内容
- 每个开发者可能使用不同的工具集配置
- 工具集源码可以随时重新安装

### Q6: 如果我想分发 CursorToolset 给团队怎么办？

A: 有两种方式：

**方式 1：全局安装（推荐）**
```bash
# 团队成员各自安装
go install github.com/firoyang/CursorToolset@latest
```

**方式 2：项目集成**
```bash
# 将 cursortoolset 放到项目中
your-project/
└── .cursor/
    └── toolsets/
        └── CursorToolset/
            ├── cursortoolset
            └── available-toolsets.json

# 可以提交这部分到 Git（修改 .gitignore）
!.cursortoolsets/CursorToolset/
```

## 验证迁移

迁移完成后，验证一切正常：

```bash
# 1. 检查安装状态
cursortoolset list

# 2. 检查目录结构
ls -la .cursor/
ls -la .cursortoolsets/
ls -la .cursor/rules/

# 3. 验证工具集功能
# 根据具体工具集进行功能测试
```

## 需要帮助？

如果迁移过程中遇到问题：

1. 查看 [ARCHITECTURE.md](./ARCHITECTURE.md) 了解新架构
2. 查看 [README.md](./README.md) 了解使用方法
3. 提交 Issue 到 GitHub 仓库

