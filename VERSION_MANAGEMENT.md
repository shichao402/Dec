# 版本号管理规范

## 📋 概述

CursorToolset 使用 **单一数据源（Single Source of Truth）** 原则管理版本号：

- **唯一来源**: `version.json` 文件（项目根目录）
- **所有版本号**: 都从 `version.json` 读取
- **禁止**: 在代码、配置文件或其他地方硬编码版本号

## 📁 version.json 文件格式

```json
{
  "version": "v1.0.0",
  "build_time": "",
  "commit": "",
  "branch": ""
}
```

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `version` | string | ✅ | 版本号，格式：`v1.0.1` |
| `build_time` | string | ❌ | 构建时间，CI/CD 自动填充 |
| `commit` | string | ❌ | Git 提交哈希，CI/CD 自动填充 |
| `branch` | string | ❌ | Git 分支名，CI/CD 自动填充 |

### 版本号格式

遵循语义化版本（Semantic Versioning）：

```
vMAJOR.MINOR.PATCH

例如:
- v1.0.0  - 初始版本
- v1.0.1  - 补丁版本（Bug 修复）
- v1.1.0  - 次版本（新功能）
- v2.0.0  - 主版本（重大变更）
```

## 🔄 版本号使用流程

### 1. 开发阶段

**手动更新 `version.json`**：

```bash
# 编辑 version.json
vim version.json

# 修改 version 字段
{
  "version": "v1.0.1",  # ← 更新这里
  "build_time": "",
  "commit": "",
  "branch": ""
}
```

**提交到 Git**：

```bash
git add version.json
git commit -m "chore: bump version to v1.0.1"
git push origin main
```

### 2. 构建阶段

**本地构建**（使用 Makefile）：

```bash
make build
# Makefile 自动从 version.json 读取版本号
```

**CI/CD 构建**（GitHub Actions）：

```yaml
# .github/workflows/build.yml
- name: 读取版本信息
  run: |
    VERSION=$(cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
    # CI/CD 会自动更新 build_time, commit, branch 字段
```

### 3. 运行时

**程序读取版本号**：

```go
// cmd/root.go
func GetVersion() string {
    // 优先使用编译时注入的版本（来自 version.json）
    if appVersion != "" {
        return appVersion
    }
    
    // 如果未注入，运行时从 version.json 读取
    ver, err := version.GetVersion(workDir)
    if err == nil {
        return ver
    }
    
    return "dev"
}
```

**查看版本**：

```bash
cursortoolset --version
# 输出: cursortoolset version v1.0.1 (built at 2024-12-04_12:00:00)
```

### 4. 更新检查

**自更新功能**：

```go
// cmd/update.go
currentVer, err := version.GetVersion(workDir)
// 从 version.json 读取当前版本
// 与 GitHub Release 的最新版本比较
```

## 🛠️ 版本号更新操作

### 更新版本号

#### 方式 1: 手动编辑（推荐）

```bash
# 1. 编辑 version.json
vim version.json

# 2. 修改 version 字段
{
  "version": "v1.0.2",  # 更新版本号
  ...
}

# 3. 提交
git add version.json
git commit -m "chore: bump version to v1.0.2"
```

#### 方式 2: 使用脚本（未来可添加）

```bash
# 可以创建一个脚本
./scripts/bump-version.sh v1.0.2
```

### 版本号递增规则

| 变更类型 | 版本递增 | 示例 |
|---------|---------|------|
| Bug 修复 | PATCH | v1.0.0 → v1.0.1 |
| 新功能（向后兼容） | MINOR | v1.0.1 → v1.1.0 |
| 重大变更（不兼容） | MAJOR | v1.1.0 → v2.0.0 |

## 📍 版本号读取位置

### 代码中读取

```go
import "github.com/firoyang/CursorToolset/pkg/version"

// 获取版本号
ver, err := version.GetVersion(workDir)

// 获取完整版本信息
info, err := version.LoadVersionInfo(workDir)
// info.Version, info.BuildTime, info.Commit, info.Branch
```

### Makefile 中读取

```makefile
VERSION=$(shell cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
```

### CI/CD 中读取

```yaml
# GitHub Actions
- name: 读取版本
  run: |
    VERSION=$(cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
```

### Shell 脚本中读取

```bash
VERSION=$(cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
```

## ✅ 验证版本号

### 检查 version.json 格式

```bash
# 使用 jq 验证（如果安装了）
cat version.json | jq .

# 或使用 Python
python3 -m json.tool version.json
```

### 检查版本号格式

```bash
# 验证版本号格式
VERSION=$(cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "❌ 版本号格式错误: $VERSION"
    exit 1
fi
```

### 测试版本号读取

```bash
# 构建并查看版本
make build
./cursortoolset --version
```

## 🚫 禁止的做法

### ❌ 不要在代码中硬编码版本号

```go
// ❌ 错误
const Version = "v1.0.1"

// ✅ 正确
version, _ := version.GetVersion(workDir)
```

### ❌ 不要从多个地方读取版本号

```yaml
# ❌ 错误：从分支名提取
VERSION=$(echo $BRANCH | sed 's/build-v//')

# ✅ 正确：从 version.json 读取
VERSION=$(cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
```

### ❌ 不要跳过 version.json

```bash
# ❌ 错误：直接使用 Git 标签
VERSION=$(git describe --tags)

# ✅ 正确：从 version.json 读取
VERSION=$(cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
```

## 🔍 版本号查找逻辑

程序会从当前目录向上查找 `version.json`：

```
当前目录
  ├─ version.json  ← 找到，使用这个
  └─ subdir/
      └─ 程序运行在这里

如果当前目录没有，向上查找：
父目录
  ├─ version.json  ← 找到，使用这个
  └─ 当前目录/
      └─ 程序运行在这里
```

最多向上查找 10 层，防止无限循环。

## 📊 CI/CD 自动填充

在 GitHub Actions 构建时，会自动更新 `version.json` 的以下字段：

```json
{
  "version": "v1.0.1",                    # ← 保持不变（手动设置）
  "build_time": "2024-12-04_12:00:00",   # ← CI/CD 自动填充
  "commit": "abc1234",                    # ← CI/CD 自动填充
  "branch": "build-v1.0.1"                # ← CI/CD 自动填充
}
```

这些字段会在构建时更新，但**不会提交回 Git**，只在构建产物中生效。

## 🎯 最佳实践

1. **单一数据源**
   - ✅ 所有版本号都从 `version.json` 读取
   - ✅ 不要在其他地方硬编码版本号

2. **版本号格式**
   - ✅ 使用语义化版本：`v1.0.1`
   - ✅ 遵循 MAJOR.MINOR.PATCH 规则

3. **更新时机**
   - ✅ 发布前更新版本号
   - ✅ 提交 `version.json` 到 Git
   - ✅ 在提交信息中说明版本变更

4. **验证**
   - ✅ 构建前验证 `version.json` 格式
   - ✅ 构建后验证版本号正确性
   - ✅ 发布前确认版本号已更新

## 📝 示例流程

### 发布 v1.0.1 版本

```bash
# 1. 更新 version.json
vim version.json
# 修改: "version": "v1.0.1"

# 2. 提交版本更新
git add version.json
git commit -m "chore: bump version to v1.0.1"

# 3. 创建 build 分支
git checkout -b build-v1.0.1
git push origin build-v1.0.1

# 4. Build Pipeline 自动触发
#    - 从 version.json 读取 v1.0.1
#    - 构建所有平台
#    - 注入版本号到二进制文件

# 5. 运行 Release Pipeline
#    - 从 build 分支的 version.json 读取版本号
#    - 创建 Release 分支
#    - 创建 GitHub Release
```

## 🔗 相关文档

- [版本控制说明](./VERSION_CONTROL.md) - 版本比较和更新机制
- [CI/CD 使用指南](./CI_CD_GUIDE.md) - 构建和发布流程
- [语义化版本规范](https://semver.org/lang/zh-CN/) - 版本号规范

---

**最后更新**: 2024-12-04  
**当前版本**: v1.0.0

