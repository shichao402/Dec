# Dec 文档

## 文档说明

- 根目录的 `README.md` 是用户视角的主入口，覆盖项目定位、快速开始和命令参考。
- `Documents/` 用于补充设计、开发、测试、构建与发布细节。
- 当前仓库已经停用 GitHub Actions，本文档中的流程均以**本地命令**和**手动发布**为准。

## 文档索引

### 设计文档
- [架构设计](design/architecture/ARCHITECTURE.md)

### 开发文档
- [开发指南](development/setup/DEVELOPMENT.md)
- [测试文档](development/testing/TESTING.md)
- [构建安装指南](development/deployment/BUILD.md)
- [发布指南](development/deployment/RELEASE.md)
- [AI 社区协议](development/AI_COMMUNITY_PROTOCOL.md)

### 变更记录
- [CHANGELOG](changelog/CHANGELOG.md)

## 维护约定

- 修改命令、配置格式或脚本行为时，需同步更新根 `README.md` 和对应专题文档。
- 项目级配置以 `.dec/config/ides.yaml` 与 `.dec/config/vault.yaml` 为准。
- 如果文档与实现冲突，以 `cmd/` 与 `pkg/` 中当前代码为准，并尽快回写文档。
