# 新功能说明文档

## 🎉 新增功能概览

本次更新为 CursorToolset 添加了与 pip/brew 对齐的关键包管理功能，大幅提升了用户体验和工具集管理能力。

---

## 1️⃣ 卸载单个工具集 (`uninstall`)

### 功能说明
允许用户卸载指定的工具集，而不是像 `clean` 命令那样清理所有内容。

### 使用方法

```bash
# 卸载指定工具集（交互式确认）
cursortoolset uninstall github-action-toolset

# 强制卸载（跳过确认）
cursortoolset uninstall github-action-toolset --force
cursortoolset uninstall github-action-toolset -f
```

### 卸载内容
- ✅ 工具集源码目录 (`.cursortoolsets/<toolset-name>/`)
- ✅ 安装的规则文件 (`.cursor/rules/...`)
- ✅ 安装的脚本文件 (`scripts/toolsets/...`)

### 示例输出

```
🗑️  准备卸载工具集: GitHub Action AI 工具集
   将删除:
   - 工具集源码: /path/to/.cursortoolsets/github-action-toolset
   - 安装的规则文件
   - 安装的脚本文件

⚠️  确认卸载？ [y/N]: y

🗑️  开始卸载工具集: GitHub Action AI 工具集
  🗑️  删除: .cursor/rules/github-actions
  🗑️  删除工具集源码: /path/to/.cursortoolsets/github-action-toolset
✅ 工具集 GitHub Action AI 工具集 卸载完成
```

---

## 2️⃣ 搜索工具集 (`search`)

### 功能说明
根据关键词搜索工具集，支持在名称、描述、仓库地址中模糊匹配。

### 使用方法

```bash
# 搜索包含 "github" 的工具集
cursortoolset search github

# 搜索包含 "action" 的工具集
cursortoolset search action

# 搜索包含 "CI/CD" 的工具集
cursortoolset search ci
```

### 搜索范围
- ✅ 工具集名称 (name)
- ✅ 显示名称 (displayName)
- ✅ 描述 (description)
- ✅ 仓库地址 (githubUrl)

### 示例输出

```bash
$ cursortoolset search github

🔍 找到 1 个匹配 "github" 的工具集:

1. github-action-toolset (GitHub Action AI 工具集)
   描述: GitHub Actions 构建和调试的 AI 规则工具集，帮助 AI 助手更好地完成 CI/CD 任务
   匹配: 名称, 显示名称, 描述, 仓库地址
   仓库: https://github.com/shichao402/GithubActionAISelfBuilder.git
   状态: ⏳ 未安装
```

---

## 3️⃣ 查看工具集详细信息 (`info`)

### 功能说明
显示指定工具集的完整信息，包括版本、作者、许可证、安装目标、功能列表等。

### 使用方法

```bash
# 查看工具集详细信息
cursortoolset info github-action-toolset
```

### 显示内容
- ✅ 基本信息（名称、版本、描述、作者、许可证）
- ✅ 安装状态和路径
- ✅ 安装目标列表
- ✅ 功能特性列表
- ✅ 文档链接

### 示例输出

```bash
$ cursortoolset info github-action-toolset

📋 工具集信息
==================================================

名称: github-action-toolset
显示名称: GitHub Action AI 工具集
版本: 1.0.0
描述: GitHub Actions 构建和调试的 AI 规则工具集
仓库: https://github.com/shichao402/GithubActionAISelfBuilder.git

状态: ⏳ 未安装

💡 使用以下命令安装:
   cursortoolset install github-action-toolset
```

如果已安装，会显示更多详细信息：

```
状态: ✅ 已安装
路径: /path/to/.cursortoolsets/github-action-toolset

作者: John Doe
许可证: MIT
关键词: github, actions, ci/cd, automation

📦 安装目标:
  • .cursor/rules/github-actions/
    源路径: core/rules/
    文件: ['*.mdc']
    说明: GitHub Actions AI 规则文件

✨ 功能列表:
  • GitHub Actions 调试 [核心]
    提供 AI 辅助的 GitHub Actions 调试功能
  • 最佳实践建议
    根据项目分析给出 CI/CD 最佳实践建议

📚 文档:
  • README: https://github.com/.../README.md
  • Wiki: https://github.com/.../wiki
```

---

## 4️⃣ 版本管理 (`install --version`)

### 功能说明
支持安装指定版本的工具集，可以使用 Git 标签或提交哈希。

### 使用方法

```bash
# 安装指定版本（Git 标签）
cursortoolset install github-action-toolset --version v1.0.0
cursortoolset install github-action-toolset -v v1.0.0

# 安装指定提交
cursortoolset install github-action-toolset --version abc123def

# 安装最新版本（默认）
cursortoolset install github-action-toolset
```

### 工作原理
1. 克隆或更新工具集仓库
2. 执行 `git fetch --tags` 获取所有标签
3. 执行 `git checkout <version>` 切换到指定版本
4. 继续正常的安装流程

### 示例输出

```bash
$ cursortoolset install github-action-toolset --version v1.0.0

📦 开始安装工具集: GitHub Action AI 工具集
  📥 克隆工具集: https://github.com/...
  ✅ 克隆成功
  🔄 切换到版本 v1.0.0...
  ✅ 已切换到版本 v1.0.0
  📄 拷贝文件: best-practices.mdc -> .cursor/rules/github-actions/best-practices.mdc
  ...
✅ 工具集 GitHub Action AI 工具集 安装完成
```

---

## 5️⃣ SHA256 校验

### 功能说明
支持在 `available-toolsets.json` 中为工具集指定 SHA256 校验和，安装时自动验证。

### 配置方法

在 `available-toolsets.json` 中添加 `sha256` 字段：

```json
[
  {
    "name": "github-action-toolset",
    "displayName": "GitHub Action AI 工具集",
    "githubUrl": "https://github.com/shichao402/GithubActionAISelfBuilder.git",
    "description": "GitHub Actions 构建和调试的 AI 规则工具集",
    "version": "1.0.0",
    "sha256": "a1b2c3d4e5f6789012345678901234567890123456789012345678901234567890"
  }
]
```

### 验证过程
1. 克隆/更新工具集
2. 计算工具集目录的 SHA256（排除 `.git` 目录）
3. 与配置中的 `sha256` 比较
4. 不匹配则安装失败

### 示例输出

```bash
📦 开始安装工具集: GitHub Action AI 工具集
  📥 克隆工具集: https://github.com/...
  ✅ 克隆成功
  🔒 验证 SHA256 校验和...
  ✅ SHA256 校验通过
  ...
```

失败时：

```
  🔒 验证 SHA256 校验和...
❌ 安装工具集 github-action-toolset 失败: SHA256 校验失败
  期望: a1b2c3d4...
  实际: e5f6789...
```

---

## 6️⃣ 依赖管理

### 功能说明
支持声明工具集之间的依赖关系，安装时自动安装依赖。

### 配置方法

在 `available-toolsets.json` 中添加 `dependencies` 字段：

```json
[
  {
    "name": "advanced-toolset",
    "displayName": "高级工具集",
    "githubUrl": "https://github.com/user/advanced-toolset.git",
    "description": "需要基础工具集的高级功能",
    "version": "2.0.0",
    "dependencies": ["basic-toolset", "common-utils"]
  },
  {
    "name": "basic-toolset",
    "displayName": "基础工具集",
    "githubUrl": "https://github.com/user/basic-toolset.git",
    "version": "1.0.0"
  },
  {
    "name": "common-utils",
    "displayName": "通用工具",
    "githubUrl": "https://github.com/user/common-utils.git",
    "version": "1.5.0"
  }
]
```

### 安装行为
- 安装单个工具集时，会先检查并安装所有依赖
- 安装所有工具集时，自动解决依赖顺序，避免重复安装
- 如果依赖已安装，会跳过

### 示例输出

```bash
$ cursortoolset install advanced-toolset

📦 安装依赖...
  📦 安装依赖: 基础工具集
📦 开始安装工具集: 基础工具集
  ...
✅ 工具集 基础工具集 安装完成

  📦 安装依赖: 通用工具
📦 开始安装工具集: 通用工具
  ...
✅ 工具集 通用工具 安装完成

📦 开始安装工具集: 高级工具集
  ...
✅ 工具集 高级工具集 安装完成
```

---

## 📊 功能对比总结

| 功能 | 旧版本 | 新版本 |
|------|--------|--------|
| 卸载单个工具集 | ❌ 只能全部清理 | ✅ 支持 |
| 搜索工具集 | ❌ | ✅ 支持 |
| 查看详细信息 | ❌ | ✅ 支持 |
| 版本管理 | ❌ | ✅ 支持指定版本 |
| SHA256 校验 | ❌ | ✅ 支持 |
| 依赖管理 | ❌ | ✅ 自动安装依赖 |

---

## 🚀 使用建议

### 日常使用流程

```bash
# 1. 搜索需要的工具集
cursortoolset search github

# 2. 查看详细信息
cursortoolset info github-action-toolset

# 3. 安装工具集（会自动安装依赖）
cursortoolset install github-action-toolset

# 4. 如需特定版本
cursortoolset install github-action-toolset --version v1.0.0

# 5. 不需要时卸载
cursortoolset uninstall github-action-toolset
```

### 开发者配置

#### 添加 SHA256 校验
```bash
# 在工具集目录计算 SHA256（需要先克隆）
cd .cursortoolsets/github-action-toolset
find . -type f ! -path "./.git/*" -exec sha256sum {} \; | sort | sha256sum

# 将结果添加到 available-toolsets.json
```

#### 声明依赖关系
```json
{
  "name": "my-toolset",
  "dependencies": ["base-toolset", "common-utils"]
}
```

---

## 📝 注意事项

1. **版本管理**：版本必须是有效的 Git 标签或提交哈希
2. **SHA256 校验**：计算时会排除 `.git` 目录，包含所有其他文件
3. **依赖管理**：确保依赖的工具集在 `available-toolsets.json` 中存在
4. **卸载顺序**：卸载时不会自动卸载依赖（避免误删）

---

## 🔮 未来计划

- [ ] 循环依赖检测
- [ ] 依赖版本约束（如 `>=1.0.0, <2.0.0`）
- [ ] 锁文件支持（类似 `package-lock.json`）
- [ ] 远程仓库镜像配置
- [ ] 本地缓存机制

---

**版本**: v1.1.0  
**更新日期**: 2024-12-04
