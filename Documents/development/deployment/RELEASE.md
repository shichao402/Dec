# Dec 发布指南

本文档描述 Dec 的版本发布流程。

## 发布流程概览

```
代码开发 → 提交 main → 运行测试 → 打 test tag → CI 测试构建 → 验证 → 打正式 tag → 正式发布
```

### 核心原则

1. **测试通过的产物直接发布** - 不重新构建
2. **代码先提交到 main** - tag 基于 main 分支创建
3. **必须通过测试** - 运行 `./scripts/run-tests.sh`

## 快速发布（推荐）

使用 `dec release` 命令自动化发布流程：

```bash
# 发布 patch 版本并等待 CI 完成
dec release --wait

# 发布 minor 版本并等待
dec release --minor --wait

# 发布 major 版本并等待
dec release --major --wait

# 预览发布流程（不执行）
dec release --dry-run
```

### release 命令说明

`dec release` 自动完成以下步骤：

1. 提升版本号（默认 patch）
2. 打包并计算 SHA256
3. 更新 package.json
4. 创建 Git commit 和 tag
5. 推送到远程仓库
6. （可选）等待 GitHub Actions 完成

**选项：**

| 选项 | 说明 |
|------|------|
| `--major` | 发布主版本 (x.0.0) |
| `--minor` | 发布次版本 (0.x.0) |
| `--patch` | 发布补丁版本 (0.0.x)，默认 |
| `--wait` | 等待 GitHub Actions 完成并确认 Release 创建 |
| `--dry-run` | 预览模式，不执行实际操作 |
| `--skip-tag` | 跳过 Git tag 和 push |

### --wait 功能

`--wait` 选项会在推送 tag 后自动轮询状态：

```bash
dec release --wait
```

输出示例：
```
🚀 发布 my-package
   版本: 1.0.0 -> 1.0.1

📝 Step 1: 更新版本号
   ✅ package.json 版本已更新为 1.0.1

📦 Step 2: 打包
   ...

✅ 发布完成！

⏳ 等待 GitHub Actions 完成...
   仓库: owner/repo
   标签: v1.0.1

   🔄 Workflow 排队中... (5s)
   🔄 Workflow 运行中... (15s)
   ✅ Workflow 完成！

⏳ 检查 Release 状态...
   ✅ Release 已创建: https://github.com/owner/repo/releases/tag/v1.0.1

🎉 发布完成！所有步骤已成功执行。
```

**技术说明：**
- 使用 `gh` CLI 查询 GitHub API（自动使用认证 token，避免 API 限流）
- 轮询间隔：10 秒
- 超时时间：30 分钟
- 需要先安装并认证 `gh` CLI：`gh auth login`

## 手动发布步骤

### 1. 完成开发并测试

```bash
# 确保代码检查通过
make lint

# 运行完整测试
./scripts/run-tests.sh
```

### 2. 更新版本号

编辑 `version.json`：

```json
{
  "version": "1.4.3"
}
```

### 3. 提交到 main

```bash
git add .
git commit -m "chore: release v1.4.3"
git push origin main
```

### 4. 发布测试版本

```bash
# 创建 test tag（触发 build.yml）
git tag test-v1.4.3
git push origin test-v1.4.3
```

**CI 自动执行**:
- 运行测试
- 构建多平台二进制
- 发布到 `ReleaseTest` 分支
- 创建 prerelease

### 5. 验证测试版本

**重要：必须验证测试版本后才能发布正式版！**

有两种验证方式：

#### 方式一：使用测试渠道安装（推荐）

通过环境变量使用测试渠道，安装到隔离目录，不影响生产环境：

```bash
# 下载安装脚本并使用测试渠道
curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseTest/scripts/install.sh -o /tmp/install-test.sh
DEC_BRANCH=ReleaseTest DEC_HOME=/tmp/test-install bash /tmp/install-test.sh

# 验证版本
/tmp/test-install/bin/dec --version

# 验证核心功能
/tmp/test-install/bin/dec list
/tmp/test-install/bin/dec registry update

# 清理测试环境
rm -rf /tmp/test-install /tmp/install-test.sh
```

**环境变量说明：**
- `DEC_BRANCH=ReleaseTest` - 使用测试分支（默认 ReleaseLatest）
- `DEC_HOME=/tmp/test-install` - 隔离安装目录（默认 ~/.decs）

#### 方式二：直接下载二进制验证

```bash
# 下载测试版本（注意：是直接二进制文件，不是压缩包）
curl -L -o /tmp/dec-test \
  "https://github.com/shichao402/Dec/releases/download/test-v1.4.3/dec-darwin-arm64"
chmod +x /tmp/dec-test

# 验证版本
/tmp/dec-test --version

# 清理
rm -f /tmp/dec-test
```

### 6. 正式发布

```bash
# 测试通过后，创建正式 tag（触发 release.yml）
git tag v1.4.3
git push origin v1.4.3
```

**CI 自动执行**:
- 从 test release 下载已测试的产物（不重新构建）
- 更新 `ReleaseLatest` 分支
- 创建正式 GitHub Release

## CI/CD 工作流

| 工作流 | 触发条件 | 动作 |
|--------|----------|------|
| `build.yml` | `test-v*` tag | 构建测试，发布到 ReleaseTest 分支 |
| `release.yml` | `v*` tag | 从 test 下载产物，发布到 ReleaseLatest 分支 |
| `release-registry.yml` | registry.json 变更 | 发布包索引 |
| `auto-register.yml` | `[auto-register]` issue | 验证并注册新包 |
| `sync-registry.yml` | 定时/`[sync]` issue/手动 | 同步包信息到注册表 |

**注意：** 推送到 `build` 分支不会触发任何构建！必须使用 tag 触发。

## 版本号规范

遵循语义化版本 (SemVer)：`MAJOR.MINOR.PATCH`

- **MAJOR**: 不兼容的 API 变更
- **MINOR**: 向后兼容的功能新增
- **PATCH**: 向后兼容的问题修复

示例：`1.0.0`, `1.2.3`, `2.0.0`

## 发布检查清单

- [ ] `make lint` 通过
- [ ] `./scripts/run-tests.sh` 通过
- [ ] `version.json` 已更新
- [ ] 代码已提交到 main
- [ ] test tag 已创建并推送
- [ ] 测试版本验证通过
- [ ] 正式 tag 已创建并推送
- [ ] GitHub Release 已创建

## 发布包索引

Registry 采用自动化管理机制，包开发者无需手动编辑配置文件。

### 新包注册（自动）

包开发者通过创建 Issue 自动注册新包：

**步骤：**

1. 确保你的包已发布 Release，且 Release 中包含：
   - `package.json`（必须包含 `name`、`version`、`dist.tarball`、`dist.sha256` 字段）
   - 对应的 tarball 文件

2. 在 [Dec 仓库](https://github.com/shichao402/Dec/issues/new) 创建 Issue：
   - **标题**：`[auto-register] 你的包名`
   - **内容**：
     ```
     repository: https://github.com/your-username/your-repo
     ```

3. CI 自动执行：
   - 验证仓库可访问性
   - 下载并验证 `package.json` 格式
   - 添加到注册表
   - 关闭 Issue 并回复结果

**package.json 必需字段示例：**

```json
{
  "name": "my-toolset",
  "version": "1.0.0",
  "description": "我的工具集",
  "dist": {
    "tarball": "my-toolset-1.0.0.tar.gz",
    "sha256": "abc123..."
  }
}
```

### 同步更新（完全自动化）

**重要：包发布后会自动同步，无需任何手动操作。**

使用 `dec init` 生成的 release workflow 会在发布成功后**自动创建 sync issue**，触发注册表同步。

#### 自动同步流程

1. **Release 创建成功后**，workflow 自动创建 sync issue：
   - 使用 `gh` CLI 创建 issue（带 `pack-sync` label）
   - Issue body 包含 `repository: https://github.com/owner/repo`
   - 自动检测已存在的 sync issue，避免重复创建

2. **同步操作执行**（在 Dec 仓库）：
   - `sync-registry.yml` workflow 检测到 `pack-sync` label 的 issue
   - 从 issue body 读取 `repository:` 字段
   - 下载最新的 `package.json`，更新 `config/registry.json`
   - 自动提交更改并更新 registry release

#### 补充同步机制

- **定时同步**：每小时自动同步所有包的最新信息（作为补充，确保不遗漏）

**开发者无需任何手动操作，发布后会自动同步到注册表。**

### 手动管理（仅维护者）

维护者可通过命令行工具管理：

```bash
# 添加包到 registry
dec registry add https://github.com/user/repo

# 移除包
dec registry remove package-name

# 导出 registry
dec registry export > registry.json
```

修改 `config/registry.json` 后提交，CI 会自动发布到 `registry` Release。

## 回滚

如果发布有问题：

```bash
# 删除错误的 tag
git tag -d v1.4.3
git push origin :refs/tags/v1.4.3

# 在 GitHub 上删除对应的 Release

# 修复问题后重新发布
```

## 常见问题

### Q: test tag 和正式 tag 有什么区别？

- `test-v*`: 触发构建和测试，产物发布到 prerelease 和 ReleaseTest 分支
- `v*`: 不重新构建，直接使用 test 版本的产物发布到 ReleaseLatest 分支

### Q: 为什么正式发布不重新构建？

确保发布的产物与测试的产物完全一致，避免"在我机器上能跑"的问题。

### Q: 如何发布紧急修复？

1. 在 main 上修复
2. 直接创建正式 tag（跳过 test）
3. 手动验证

### Q: 推送到 build 分支会触发构建吗？

**不会！** build 分支在当前 CI/CD 配置中没有触发作用。必须使用 tag：
- 测试构建：`git tag test-vX.X.X && git push origin test-vX.X.X`
- 正式发布：`git tag vX.X.X && git push origin vX.X.X`

### Q: 测试渠道和正式渠道的区别？

| 渠道 | 分支 | 用途 |
|------|------|------|
| 测试渠道 | ReleaseTest | 验证新版本，prerelease |
| 正式渠道 | ReleaseLatest | 生产环境，stable release |

用户可通过 `DEC_BRANCH` 环境变量切换渠道。

## 相关文档

- [开发指南](DEVELOPMENT.md)
- [测试指南](TESTING.md)
- [构建安装指南](BUILD.md)
