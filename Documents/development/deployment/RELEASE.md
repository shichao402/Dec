# Dec 发布指南

本文档描述当前仓库的发布约定。

## 当前状态

- 仓库中的 GitHub Actions 工作流已经停用。
- 推送 tag **不会**自动触发构建、测试或发布。
- 当前发布方式以**本地构建 + 手动创建 Release**为准。

## 推荐发布流程

### 1. 更新版本与文档

- 修改 `version.json`
- 更新 `Documents/changelog/CHANGELOG.md`
- 确认根 `README.md` 和安装脚本中的使用方式仍然成立

### 2. 本地测试

```bash
make test
./scripts/run-tests.sh
```

### 3. 构建发布产物

```bash
./scripts/build.sh --all
```

构建完成后检查：

- `dist/` 下各平台二进制是否齐全
- `dist/BUILD_INFO*.txt` 是否已生成
- 二进制文件大小与版本号是否合理

### 4. 本地冒烟验证

至少验证一项：

```bash
./dist/dec-darwin-arm64 --version
./dist/dec-darwin-arm64 --help
```

如需更完整验证，建议使用隔离目录测试安装脚本：

```bash
DEC_HOME=/tmp/dec-release-check DEC_BRANCH=ReleaseLatest bash ./scripts/install.sh
```

### 5. 创建 Git tag

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

### 6. 手动创建 GitHub Release

可以使用 GitHub Web UI，或使用 `gh` CLI：

```bash
gh release create vX.Y.Z dist/* --title "vX.Y.Z" --notes-file Documents/changelog/CHANGELOG.md
```

### 7. 维护安装渠道

当前安装脚本默认从 `ReleaseLatest` 分支读取：

- `version.json`
- `scripts/install.sh`
- `scripts/install.ps1`
- `scripts/uninstall.sh`
- `scripts/uninstall.ps1`
- `config/system.json`

如果你的发布策略仍然依赖在线安装脚本，请确保这些文件在 `ReleaseLatest` 分支中同步更新。

## 发布辅助脚本

`scripts/release-yolo.sh` 现用于**本地发布前检查与构建准备**，不再依赖 GitHub Actions，也不会自动等待 CI。

## 发布检查清单

- [ ] `version.json` 已更新
- [ ] `CHANGELOG` 已更新
- [ ] `make test` 通过
- [ ] `./scripts/run-tests.sh` 通过
- [ ] `./scripts/build.sh --all` 完成
- [ ] 已完成至少一次本地冒烟验证
- [ ] Git tag 已推送
- [ ] GitHub Release 已手动创建
- [ ] 如依赖在线安装，`ReleaseLatest` 分支已同步维护

## 不再适用的旧流程

以下流程已经不再是当前仓库的发布基线：

- `test-v*` / `v*` tag 自动触发 CI
- `build.yml` / `release.yml` 自动构建与正式发布
- `release-registry.yml` / `auto-register.yml` / `sync-registry.yml` 自动维护 registry
- `dec publish-notify`
