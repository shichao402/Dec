# CursorToolset 使用示例

## 📦 安装工具集

### 示例：安装 github-action-toolset

```bash
# 1. 安装包（下载到 ~/.cursortoolsets/repos/）
cursortoolset install github-action-toolset

# 2. 链接规则文件（手动）
mkdir -p .cursor/rules
ln -sf ~/.cursortoolsets/repos/github-action-toolset/rules .cursor/rules/github-actions

# 3. 验证安装
ls -la .cursor/rules/github-actions/
# 应该看到：
# best-practices.mdc
# debugging.mdc
# github-actions.mdc
```

## 🔍 设计理念

CursorToolset 采用**最小化**设计，类似 Git 的哲学：

```
包管理器职责：
✅ 下载包
✅ 验证完整性（SHA256）
✅ 解压到统一目录

用户职责：
✅ 决定如何使用（链接/复制）
✅ 管理项目配置
✅ 自定义工作流
```

### 为什么不自动复制？

1. **灵活性**：用户可以选择链接或复制
   - 链接：更新包时自动生效
   - 复制：固化版本，不受包更新影响

2. **清晰性**：`.cursor/rules/` 的内容由用户明确控制

3. **简单性**：包管理器只做一件事——管理包

## 📁 目录结构

```
~/.cursortoolsets/                    # 环境目录
└── repos/                            # 所有已安装的包
    └── github-action-toolset/        # 包源码
        ├── rules/                    # 规则文件
        │   ├── github-actions.mdc
        │   ├── best-practices.mdc
        │   └── debugging.mdc
        ├── docs/                     # 文档
        ├── toolset.json              # 包配置
        └── PACKAGE.md                # 包说明

your-project/                         # 项目目录
└── .cursor/
    └── rules/
        └── github-actions/           # 符号链接 → ~/.cursortoolsets/repos/github-action-toolset/rules
            ├── github-actions.mdc
            ├── best-practices.mdc
            └── debugging.mdc
```

## 🎯 常见场景

### 场景 1：使用最新版本（推荐）

```bash
# 使用符号链接
cursortoolset install github-action-toolset
ln -sf ~/.cursortoolsets/repos/github-action-toolset/rules .cursor/rules/github-actions

# 更新包时，规则文件自动更新
cursortoolset update github-action-toolset
```

### 场景 2：固化特定版本

```bash
# 使用复制
cursortoolset install github-action-toolset
mkdir -p .cursor/rules/github-actions
cp ~/.cursortoolsets/repos/github-action-toolset/rules/*.mdc .cursor/rules/github-actions/

# 更新包时，项目中的规则文件不变
cursortoolset update github-action-toolset
```

### 场景 3：多项目共享

```bash
# 所有项目链接到同一个包
cd project-a
ln -sf ~/.cursortoolsets/repos/github-action-toolset/rules .cursor/rules/github-actions

cd ../project-b
ln -sf ~/.cursortoolsets/repos/github-action-toolset/rules .cursor/rules/github-actions

# 一次更新，所有项目生效
cursortoolset update github-action-toolset
```

## 🛠️ 实用脚本

### 快速安装脚本

创建 `install-toolset.sh`：

```bash
#!/bin/bash
# 快速安装并链接工具集

TOOLSET_NAME="$1"
if [ -z "$TOOLSET_NAME" ]; then
    echo "用法: $0 <toolset-name>"
    exit 1
fi

# 安装包
cursortoolset install "$TOOLSET_NAME"

# 链接规则文件
RULES_SOURCE="$HOME/.cursortoolsets/repos/$TOOLSET_NAME/rules"
RULES_TARGET=".cursor/rules/$TOOLSET_NAME"

if [ -d "$RULES_SOURCE" ]; then
    mkdir -p .cursor/rules
    ln -sf "$RULES_SOURCE" "$RULES_TARGET"
    echo "✅ $TOOLSET_NAME 安装完成并已链接规则文件"
else
    echo "⚠️  $TOOLSET_NAME 没有规则文件"
fi
```

使用：
```bash
chmod +x install-toolset.sh
./install-toolset.sh github-action-toolset
```

## 📝 总结

| 操作 | 命令 | 说明 |
|------|------|------|
| 安装包 | `cursortoolset install <name>` | 下载到 `~/.cursortoolsets/repos/` |
| 链接规则 | `ln -sf ~/.cursortoolsets/repos/<name>/rules .cursor/rules/<name>` | 创建符号链接 |
| 更新包 | `cursortoolset update <name>` | 更新包（链接会自动更新） |
| 卸载包 | `cursortoolset uninstall <name>` | 删除包源码 |
| 移除链接 | `rm .cursor/rules/<name>` | 移除项目中的链接 |

**核心思想**：包管理器管理包，用户管理项目配置。简单、清晰、可控。
