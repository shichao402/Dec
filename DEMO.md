# CursorToolset v1.1.0 功能演示

## 🎯 核心改进

本次更新将 CursorToolset 从一个简单的安装工具升级为**功能完整的包管理器**，向 pip/brew 的成熟度对齐。

---

## 🆕 新增命令一览

| 命令 | 功能 | 类比 |
|------|------|------|
| `search` | 搜索工具集 | `brew search`, `pip search` |
| `info` | 查看详细信息 | `brew info`, `pip show` |
| `uninstall` | 卸载单个工具集 | `brew uninstall`, `pip uninstall` |

---

## 📋 完整命令列表

```bash
cursortoolset
├── install [name]     # 安装工具集（支持 --version）
├── uninstall <name>   # 卸载工具集 (新)
├── list               # 列出所有工具集
├── search <keyword>   # 搜索工具集 (新)
├── info <name>        # 查看详细信息 (新)
├── clean              # 清理所有安装
└── update             # 更新管理器和工具集
```

---

## 🎬 实战演示

### 场景 1: 探索可用工具集

```bash
# 1. 列出所有工具集
$ cursortoolset list
📋 可用工具集 (1 个):

1. github-action-toolset (GitHub Action AI 工具集)
   描述: GitHub Actions 构建和调试的 AI 规则工具集
   仓库: https://github.com/shichao402/GithubActionAISelfBuilder.git
   状态: ⏳ 未安装

# 2. 搜索特定工具
$ cursortoolset search action
🔍 找到 1 个匹配 "action" 的工具集:

1. github-action-toolset (GitHub Action AI 工具集)
   描述: GitHub Actions 构建和调试的 AI 规则工具集
   匹配: 名称, 显示名称, 描述
   仓库: https://github.com/shichao402/GithubActionAISelfBuilder.git
   状态: ⏳ 未安装

# 3. 查看详细信息
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

---

### 场景 2: 安装工具集（基础）

```bash
# 安装工具集
$ cursortoolset install github-action-toolset

📦 开始安装工具集: GitHub Action AI 工具集
  📥 克隆工具集: https://github.com/shichao402/GithubActionAISelfBuilder.git
  ✅ 克隆成功
  📄 拷贝文件: best-practices.mdc -> .cursor/rules/github-actions/best-practices.mdc
  📄 拷贝文件: debugging.mdc -> .cursor/rules/github-actions/debugging.mdc
  📄 拷贝文件: github-actions.mdc -> .cursor/rules/github-actions/github-actions.mdc
✅ 工具集 GitHub Action AI 工具集 安装完成
```

---

### 场景 3: 版本管理

```bash
# 安装特定版本
$ cursortoolset install github-action-toolset --version v1.0.0

📦 开始安装工具集: GitHub Action AI 工具集
  📥 克隆工具集: https://github.com/...
  ✅ 克隆成功
  🔄 切换到版本 v1.0.0...
  ✅ 已切换到版本 v1.0.0
  📄 拷贝文件: ...
✅ 工具集 GitHub Action AI 工具集 安装完成

# 也可以使用提交哈希
$ cursortoolset install github-action-toolset --version abc123def
```

---

### 场景 4: 依赖自动安装

假设 `available-toolsets.json` 配置如下：

```json
[
  {
    "name": "advanced-ci",
    "displayName": "高级 CI 工具集",
    "githubUrl": "https://github.com/user/advanced-ci.git",
    "dependencies": ["github-action-toolset", "docker-toolset"]
  },
  {
    "name": "github-action-toolset",
    "displayName": "GitHub Actions 工具集",
    "githubUrl": "https://github.com/..."
  },
  {
    "name": "docker-toolset",
    "displayName": "Docker 工具集",
    "githubUrl": "https://github.com/..."
  }
]
```

安装时自动处理依赖：

```bash
$ cursortoolset install advanced-ci

📦 安装依赖...
  📦 安装依赖: GitHub Actions 工具集
📦 开始安装工具集: GitHub Actions 工具集
  ...
✅ 工具集 GitHub Actions 工具集 安装完成

  📦 安装依赖: Docker 工具集
📦 开始安装工具集: Docker 工具集
  ...
✅ 工具集 Docker 工具集 安装完成

📦 开始安装工具集: 高级 CI 工具集
  ...
✅ 工具集 高级 CI 工具集 安装完成
```

---

### 场景 5: SHA256 校验

配置 `available-toolsets.json`:

```json
{
  "name": "secure-toolset",
  "sha256": "a1b2c3d4e5f6789012345678901234567890123456789012345678901234567890"
}
```

安装时自动验证：

```bash
$ cursortoolset install secure-toolset

📦 开始安装工具集: 安全工具集
  📥 克隆工具集: https://github.com/...
  ✅ 克隆成功
  🔒 验证 SHA256 校验和...
  ✅ SHA256 校验通过
  ...
```

如果校验失败：

```bash
  🔒 验证 SHA256 校验和...
❌ 安装工具集 secure-toolset 失败: SHA256 校验失败
  期望: a1b2c3d4e5f6789012345678901234567890123456789012345678901234567890
  实际: e5f6789012345678901234567890123456789012345678901234567890123456
```

---

### 场景 6: 卸载工具集

```bash
# 交互式卸载
$ cursortoolset uninstall github-action-toolset

🗑️  准备卸载工具集: GitHub Action AI 工具集
   将删除:
   - 工具集源码: /Users/user/.cursor/toolsets/github-action-toolset
   - 安装的规则文件
   - 安装的脚本文件

⚠️  确认卸载？ [y/N]: y

🗑️  开始卸载工具集: GitHub Action AI 工具集
  🗑️  删除: .cursor/rules/github-actions
  🗑️  删除工具集源码: /Users/user/.cursor/toolsets/github-action-toolset
✅ 工具集 GitHub Action AI 工具集 卸载完成

# 强制卸载（无需确认）
$ cursortoolset uninstall github-action-toolset --force
```

---

### 场景 7: 查看已安装工具集详情

```bash
$ cursortoolset info github-action-toolset

📋 工具集信息
==================================================

名称: github-action-toolset
显示名称: GitHub Action AI 工具集
版本: 1.0.0
描述: GitHub Actions 构建和调试的 AI 规则工具集
仓库: https://github.com/shichao402/GithubActionAISelfBuilder.git

状态: ✅ 已安装
路径: /Users/user/.cursor/toolsets/github-action-toolset

作者: John Doe
许可证: MIT
关键词: github, actions, ci/cd

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
```

---

## 🔄 典型工作流程

### 开发者日常使用

```bash
# 第一次使用
cursortoolset search github          # 搜索需要的工具
cursortoolset info github-action-toolset  # 查看详情
cursortoolset install github-action-toolset  # 安装

# 日常维护
cursortoolset list                   # 查看已安装
cursortoolset update --toolsets      # 更新工具集

# 清理不需要的
cursortoolset uninstall old-toolset
```

### 团队协作

```bash
# 项目开发者
cursortoolset install                # 安装所有工具集
# 依赖会自动安装，无需手动干预

# 项目维护者
cursortoolset update                 # 保持工具集最新
```

---

## 📊 性能对比

### 旧版本 vs 新版本

| 操作 | 旧版本 | 新版本 |
|------|--------|--------|
| 查找工具集 | 只能看完整列表 | ✅ 可搜索过滤 |
| 了解工具集 | 只有简单描述 | ✅ 完整详情展示 |
| 安装依赖 | ❌ 手动安装 | ✅ 自动处理 |
| 版本控制 | ❌ 只能最新 | ✅ 指定任意版本 |
| 安全验证 | ❌ 无 | ✅ SHA256 校验 |
| 卸载工具 | ❌ 只能全部清理 | ✅ 精确卸载 |

---

## 🎯 关键改进点

### 1. **可发现性** 📍
- 搜索功能让用户快速找到需要的工具集
- 详细信息展示帮助用户做决策

### 2. **可靠性** 🔒
- SHA256 校验确保工具集完整性
- 版本管理避免意外更新

### 3. **易用性** 🎨
- 依赖自动安装减少手动操作
- 精确卸载提供更好的控制

### 4. **专业性** 🏆
- 向 brew/pip 对齐，降低学习成本
- 完整的包管理功能

---

## 💡 最佳实践

### 工具集维护者

1. **添加 SHA256**：确保用户安装的是可信版本
```json
{
  "sha256": "计算的校验和"
}
```

2. **声明依赖**：让用户体验更流畅
```json
{
  "dependencies": ["base-toolset"]
}
```

3. **使用语义化版本**：便于用户选择
```bash
git tag v1.0.0
git push --tags
```

### 工具集用户

1. **先搜索再安装**：避免安装不需要的工具
2. **查看详情再决策**：了解工具集功能和依赖
3. **锁定版本**：生产环境使用稳定版本

---

## 🚀 升级建议

从 v1.0.x 升级到 v1.1.0：

```bash
# 使用 update 命令
cursortoolset update --self

# 或重新下载安装脚本
curl -fsSL https://raw.githubusercontent.com/firoyang/CursorToolset/main/install.sh | bash
```

无需额外配置，完全向后兼容！

---

**版本**: v1.1.0  
**发布日期**: 2024-12-04  
**主要贡献**: 6 个新功能，向 pip/brew 包管理器对齐
