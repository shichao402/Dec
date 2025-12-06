# CursorToolset 开发指南

## 环境准备

开发前需安装以下工具：

```bash
# Go 1.21+
go version

# golangci-lint（代码检查，必须安装）
brew install golangci-lint
# 或: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# 验证安装
golangci-lint --version
```

---

## 开发流程

```
本地开发 → 构建测试 → 源码安装验证 → 提交 main → 打 test tag → CI 构建 → 测试 → 打正式 tag → 发布
```

### 核心原则

1. **测试通过的产物直接发布** - 不重新构建
2. **配置文件驱动** - 不硬编码，所有配置来自 `config/system.json`
3. **代码先提交到 main** - tag 基于 main 分支创建

---

## 日常开发

### 本地构建与测试

```bash
# 构建
make build

# 代码检查（lint）
make lint

# 运行测试
make test

# 源码安装到本地（覆盖现有安装）
make install-dev

# 带测试的安装
make install-dev-test
```

> **注意**: 提交前务必运行 `make lint`，CI 会检查 lint 错误。需要安装 [golangci-lint](https://golangci-lint.run/usage/install/)。

### 验证安装

```bash
cursortoolset --version
cursortoolset list
```

---

## 发布流程

### 1. 提交代码到 main

```bash
git add .
git commit -m "feat: 新功能描述"
git push origin main
```

### 2. 发布测试版本

```bash
# 创建 test tag（触发 build.yml）
git tag test-v1.4.3
git push origin test-v1.4.3
```

CI 会自动：
- 运行测试
- 构建多平台二进制
- 发布到 `ReleaseTest` 分支
- 创建 prerelease

### 3. 验证测试版本

从 GitHub Releases 下载测试版本进行验证。

### 4. 正式发布

```bash
# 测试通过后，创建正式 tag（触发 release.yml）
git tag v1.4.3
git push origin v1.4.3
```

CI 会自动：
- 从 test release 下载已测试的产物（不重新构建）
- 更新 `ReleaseLatest` 分支
- 创建正式 GitHub Release

---

## CI/CD 配置

| 工作流 | 触发条件 | 动作 |
|--------|----------|------|
| `build.yml` | `test-v*` tag / 手动 / PR | 构建测试，发布 test 渠道 |
| `release.yml` | `v*` tag | 从 test 下载产物，正式发布 |
| `release-registry.yml` | registry.json 变更 | 发布包索引 |

---

## 项目结构

```
CursorToolset/
├── cmd/                    # 命令行命令
├── pkg/                    # 核心包
│   ├── config/            # 配置管理
│   │   ├── config.go      # 用户配置
│   │   └── system.go      # 系统配置加载
│   ├── installer/         # 安装器
│   ├── registry/          # 包索引
│   └── ...
├── config/
│   └── system.json        # 系统配置模板
├── scripts/
│   ├── install.sh         # 正式安装脚本
│   └── install-dev.sh     # 开发安装脚本
├── .github/workflows/     # CI/CD
├── version.json           # 版本信息
└── Makefile
```

---

## 配置文件

### system.json（系统配置）

安装时写入 `~/.cursortoolsets/config/system.json`：

```json
{
  "repo_owner": "shichao402",
  "repo_name": "CursorToolset",
  "registry_url": "https://github.com/.../registry.json",
  "update_branch": "ReleaseLatest"
}
```

### 配置优先级

1. 环境变量
2. 用户配置 (`settings.json`)
3. 系统配置 (`system.json`)
4. 内置默认值

---

## 安装脚本

| 脚本 | 用途 | 配置来源 |
|------|------|----------|
| `scripts/install.sh` | 正式安装，从 GitHub Release 下载 | 网络下载 |
| `scripts/install-dev.sh` | 开发安装，本地源码构建 | 本地拷贝 |

---

## 版本管理

版本号遵循语义化版本：`vMAJOR.MINOR.PATCH`

- 修改 `version.json` 中的版本号
- Tag 名称与版本号一致

---

## 常用命令速查

```bash
# 开发
make build              # 构建
make lint               # 代码检查
make test               # 测试
make install-dev        # 源码安装

# 发布
git tag test-v1.x.x && git push origin test-v1.x.x  # 测试版
git tag v1.x.x && git push origin v1.x.x            # 正式版

# 清理
make clean              # 清理构建产物
```
