# CursorToolset 包开发指南

本指南帮助你开发符合 CursorToolset 规范的工具集包。

---

## 目录结构

```
my-toolset/
├── package.json          # 包配置文件（必需）
├── .cursortoolset/       # AI 规则目录
│   └── rules/            # 规则文件
├── .github/
│   └── workflows/
│       └── release.yml   # 自动发布工作流（可选）
├── .gitignore
└── README.md
```

---

## 注册表与发布规范

### 注册表格式

包在 CursorToolset 注册表中只需提供仓库地址：

```json
{
  "repository": "https://github.com/USERNAME/my-toolset"
}
```

管理工具会自动组装 URL 获取包信息：
- 最新版本：`https://github.com/{repo}/releases/latest/download/package.json`
- 特定版本：`https://github.com/{repo}/releases/download/v1.0.0/package.json`

### 发布产物结构

每次 Release 需要上传两个文件：

```
Release v1.0.0/
├── package.json                  # 包配置
├── my-toolset-1.0.0.tar.gz       # 打包产物
```

**重要**：`package.json` 中的 `dist.tarball` 使用**相对路径**：

```json
{
  "dist": {
    "tarball": "my-toolset-1.0.0.tar.gz",
    "sha256": "..."
  }
}
```

管理工具会根据 `package.json` 的 URL 自动解析 tarball 的完整下载地址。

---

## package.json 规范

这是发布时上传的包配置文件，定义包的元数据和下载信息。

```json
{
  "name": "my-toolset",
  "displayName": "My Toolset",
  "version": "1.0.0",
  "description": "包的简短描述",
  "author": "你的名字",
  "license": "MIT",
  "keywords": ["keyword1", "keyword2"],
  "repository": {
    "type": "git",
    "url": "https://github.com/USERNAME/my-toolset.git"
  },
  "dist": {
    "tarball": "my-toolset-1.0.0.tar.gz",
    "sha256": "SHA256校验和",
    "size": 12345
  },
  "cursortoolset": {
    "minVersion": "1.0.0"
  }
}
```

### 字段说明

| 字段 | 必需 | 说明 |
|------|------|------|
| name | ✅ | 包名，小写字母、数字、连字符 |
| version | ✅ | 语义化版本号 (MAJOR.MINOR.PATCH) |
| displayName | ❌ | 显示名称 |
| description | ❌ | 包描述 |
| dist.tarball | ✅ | 下载文件名（相对路径） |
| dist.sha256 | ✅ | SHA256 校验和 |
| dist.size | ❌ | 文件大小（字节） |
| bin | ❌ | 可执行文件配置（见下文） |

---

## 可执行文件 (bin) 配置

如果你的包包含可执行文件（如 Go/Rust 编译的二进制），需要配置 `bin` 字段。

### 支持的平台标识

| 平台 | 标识 |
|------|------|
| macOS Intel | `darwin-amd64` |
| macOS Apple Silicon | `darwin-arm64` |
| Linux x64 | `linux-amd64` |
| Linux ARM64 | `linux-arm64` |
| Windows x64 | `windows-amd64` |
| Windows ARM64 | `windows-arm64` |

### 配置格式

```json
{
  "bin": {
    "命令名": {
      "平台标识": "tarball内的相对路径"
    }
  }
}
```

### 完整示例

```json
{
  "name": "github-action-toolset",
  "version": "1.0.5",
  "bin": {
    "gh-action-debug": {
      "darwin-amd64": "core/tools/go/dist/gh-action-debug-darwin-amd64",
      "darwin-arm64": "core/tools/go/dist/gh-action-debug-darwin-arm64",
      "linux-amd64": "core/tools/go/dist/gh-action-debug-linux-amd64",
      "linux-arm64": "core/tools/go/dist/gh-action-debug-linux-arm64",
      "windows-amd64": "core/tools/go/dist/gh-action-debug-windows-amd64.exe",
      "windows-arm64": "core/tools/go/dist/gh-action-debug-windows-arm64.exe"
    }
  },
  "dist": {
    "tarball": "github-action-toolset-1.0.5.tar.gz",
    "sha256": "8f107e5d303e9e8645c6714d73483b4d87317c10e6747523dbf02b8d035d103b",
    "size": 30461952
  }
}
```

### 目录结构建议

```
my-toolset/
├── core/
│   └── tools/
│       └── go/
│           ├── cmd/
│           │   └── my-command/
│           │       └── main.go
│           └── dist/                    # 构建产物目录
│               ├── my-command-darwin-amd64
│               ├── my-command-darwin-arm64
│               ├── my-command-linux-amd64
│               ├── my-command-linux-arm64
│               ├── my-command-windows-amd64.exe
│               └── my-command-windows-arm64.exe
├── package.json
└── .github/
    └── workflows/
        └── release.yml
```

### 构建配置（可选）

在 `package.json` 中添加 `build` 字段，供工具自动构建：

```json
{
  "build": {
    "type": "go",
    "entry": "core/tools/go/cmd/my-command",
    "output": "core/tools/go/dist",
    "platforms": [
      "darwin-amd64",
      "darwin-arm64", 
      "linux-amd64",
      "linux-arm64",
      "windows-amd64",
      "windows-arm64"
    ]
  }
}
```

---

## 版本号规范

遵循语义化版本 (SemVer)：

- **MAJOR**: 不兼容的 API 变更
- **MINOR**: 向后兼容的功能新增
- **PATCH**: 向后兼容的问题修复

示例：`1.0.0`, `1.2.3`, `2.0.0`

---

## 发布流程

### 使用 cursortoolset release 命令

**这是唯一推荐的发布方式**，自动保证版本一致性，避免常见错误。

```bash
# 发布 patch 版本 (0.0.x) - 默认
cursortoolset release

# 发布 minor 版本 (0.x.0)
cursortoolset release --minor

# 发布 major 版本 (x.0.0)
cursortoolset release --major

# 预览发布流程（不执行）
cursortoolset release --dry-run
```

**命令自动完成：**
1. ✅ 提升 `package.json` 中的版本号
2. ✅ 打包 tarball（包含正确版本的 package.json）
3. ✅ 计算 SHA256 并更新 `package.json`
4. ✅ Git commit
5. ✅ 创建 Git tag
6. ✅ 推送到远程仓库

**发布后，在 GitHub 创建 Release：**
1. 在 GitHub 仓库页面点击 "Releases" → "Draft a new release"
2. 选择刚才推送的 tag（如 `v1.0.0`）
3. 上传生成的 `<package-name>-<version>.tar.gz` 文件
4. 上传更新后的 `package.json` 文件
5. 发布

> **提示**：使用 `--dry-run` 先预览，确认无误后再正式发布。

### 配置 GitHub Actions 自动创建 Release（可选）

如果希望推送 tag 后自动创建 GitHub Release，可以使用 `cursortoolset init` 生成的 workflow：

```bash
# 初始化时会自动创建 .github/workflows/release.yml
cursortoolset init my-toolset
```

配置好后，`cursortoolset release` 推送 tag 后，GitHub Actions 会自动创建 Release 并上传文件。

---

## 本地验证（dry-run）

发布前可以本地验证打包内容：

```bash
# 预览发布内容（不实际发布）
cursortoolset release --dry-run

# 输出示例：
# ✓ 版本: 1.0.0
# ✓ 将包含的文件:
#   - package.json
#   - rules/
#   - core/tools/go/dist/
# ✗ 将排除的文件:
#   - .git/
#   - *.go
#   - go.mod
# ✓ bin 文件检查:
#   - gh-action-debug-darwin-arm64 ✓
#   - gh-action-debug-linux-amd64 ✓
```

---

## AI 规则编写

在 `.cursortoolset/rules/` 目录下创建 `.md` 文件作为 AI 规则。

### 规则文件示例

```markdown
# 项目开发规范

## 代码风格
- 使用 4 空格缩进
- 函数命名使用驼峰式

## 提交规范
- feat: 新功能
- fix: 修复
- docs: 文档
```

### 最佳实践

1. **清晰明确** - 规则应该具体、可执行
2. **分类组织** - 按主题拆分多个规则文件
3. **保持更新** - 随项目演进更新规则

---

## 常用命令

```bash
# 初始化新包
cursortoolset init my-toolset

# 打包（唯一标准方式）
cursortoolset pack

# 发布预览
cursortoolset release --dry-run

# 发布新版本（唯一标准方式）
cursortoolset release

# 本地安装测试
cursortoolset install ./my-toolset
```

> **重要**：始终使用 `cursortoolset pack` 和 `cursortoolset release` 命令，不要手动执行 tar 打包或版本管理，以确保版本一致性。

---

## 常见问题

### Q: 每次 `cursortoolset update` 都重复安装同一个包

**原因**：tarball 内的 `package.json` 版本号与 release 版本不一致。

**解决**：使用 `cursortoolset release` 重新发布，它会自动保证版本一致性。

### Q: tarball 打包时出现 "file changed as we read it"

**原因**：tar 输出在当前目录，打包时包含了自己。

**解决**：输出到 `/tmp/release/` 或其他目录。

### Q: 排除规则把构建产物也排除了

**原因**：使用了 `--exclude='core/tools'` 这样的目录排除。

**解决**：精确排除源码文件：
```bash
--exclude='*.go' \
--exclude='go.mod' \
--exclude='go.sum'
```

### Q: 安装后 bin 命令找不到

**检查**：
1. `bin` 配置的路径是否正确
2. 平台标识是否匹配当前系统
3. 文件是否有执行权限

---

## 参考资源

- [CursorToolset 仓库](https://github.com/shichao402/CursorToolset)
- [语义化版本规范](https://semver.org/lang/zh-CN/)
- [GitHub Releases 文档](https://docs.github.com/en/repositories/releasing-projects-on-github)
