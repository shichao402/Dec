# CursorToolset 发布指南

本文档描述 CursorToolset 的版本发布流程。

## 发布流程概览

```
代码开发 → 提交 main → 运行测试 → 打 test tag → CI 测试构建 → 验证 → 打正式 tag → 正式发布
```

### 核心原则

1. **测试通过的产物直接发布** - 不重新构建
2. **代码先提交到 main** - tag 基于 main 分支创建
3. **必须通过测试** - 运行 `./scripts/run-tests.sh`

## 发布步骤

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
curl -fsSL https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseTest/scripts/install.sh -o /tmp/install-test.sh
CURSOR_TOOLSET_BRANCH=ReleaseTest CURSOR_TOOLSET_HOME=/tmp/test-install bash /tmp/install-test.sh

# 验证版本
/tmp/test-install/bin/cursortoolset --version

# 验证核心功能
/tmp/test-install/bin/cursortoolset list
/tmp/test-install/bin/cursortoolset registry update

# 清理测试环境
rm -rf /tmp/test-install /tmp/install-test.sh
```

**环境变量说明：**
- `CURSOR_TOOLSET_BRANCH=ReleaseTest` - 使用测试分支（默认 ReleaseLatest）
- `CURSOR_TOOLSET_HOME=/tmp/test-install` - 隔离安装目录（默认 ~/.cursortoolsets）

#### 方式二：直接下载二进制验证

```bash
# 下载测试版本（注意：是直接二进制文件，不是压缩包）
curl -L -o /tmp/cursortoolset-test \
  "https://github.com/shichao402/CursorToolset/releases/download/test-v1.4.3/cursortoolset-darwin-arm64"
chmod +x /tmp/cursortoolset-test

# 验证版本
/tmp/cursortoolset-test --version

# 清理
rm -f /tmp/cursortoolset-test
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

当 `config/registry.json` 变更时，需要发布新的包索引：

```bash
# 修改 registry.json
vim config/registry.json

# 提交
git add config/registry.json
git commit -m "registry: add new-package"
git push origin main
```

CI 会自动发布到 `registry` Release。

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

用户可通过 `CURSOR_TOOLSET_BRANCH` 环境变量切换渠道。

## 相关文档

- [开发指南](DEVELOPMENT.md)
- [测试指南](TESTING.md)
- [构建安装指南](BUILD.md)
