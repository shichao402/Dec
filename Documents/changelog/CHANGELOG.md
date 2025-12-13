# Changelog

所有重要的变更都会记录在这个文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

## [1.7.3] - 2025-12-13

### Fixed
- 修复发布后未自动创建 sync issue 的问题（Issue #8）
- 修复 sync issue 解析包含版本号时的问题
- 改进 sync issue 识别方式：使用 `pack-sync` label 替代 title 解析

### Improved
- 简化 release workflow：移除所有可选机制，使用唯一、标准化的流程
- 支持从 Gist payload 读取同步信息（如果使用 `github-issue` 工具创建）
- 改进 repository 解析逻辑：优先从 payload 读取，回退到 body 解析，最后回退到 title 解析
- 确保包发布后自动同步到注册表，无需手动操作

### Changed
- Release workflow 模板：自动创建 sync issue（带 `pack-sync` label）
- Sync workflow：使用 label 识别 sync issue，更可靠
- 文档更新：强调完全自动化，移除手动创建说明

## [1.7.2] - 2024-12-13

### Added
- 新增 `release --wait` 选项：发布后自动等待 GitHub Actions 完成并确认 Release 创建
  - 使用 `gh` CLI 查询状态，避免 API 限流
  - 实时输出 workflow 状态（排队中/运行中/完成）
  - 支持超时控制（30分钟）
- 新增自动注册机制：包开发者发布时自动注册到 Dec
- 新增 `auto-register.yml` workflow 处理 `[auto-register]` issue
- 新增 `sync-registry.yml` workflow 支持定时同步和 `[sync]` issue 触发
- Registry 新格式：包含完整包元信息，用户更新只需 1 次请求

### Changed
- Registry 格式升级到 v4，包含 name/version/description/dist 等完整信息
- `registry.go` 简化：从 registry 直接构建 manifest 缓存，无需逐个下载
- **包开发文档迁移到 CursorColdStart**：通过 `dec` pack 提供，不再随管理器分发
- `sync` 命令简化：移除文档同步功能，保留配置迁移能力
- `init` 命令简化：不再复制包开发指南文档

### Improved
- 用户 `update` 性能大幅提升：从 N 次请求减少到 1 次

## [1.5.9] - 2024-12-07

### Added
- 新增 `sync` 命令用于同步已有包项目的开发文件
- 新增 `pkg/state` 包管理版本状态
- 新增 `pkg/setup` 包处理文档自动同步

### Changed
- 重构安装/更新架构，采用单一职责 + 事件驱动模式
- `install.sh` 只写入 `.state/version`，文档同步由 setup 包处理
- `update --self` 只写入 `.state/version`

### Fixed
- 修复 `update --self` 后文档不更新的问题

## [1.5.8] - 2024-12-07

### Added
- 新增 `sync` 命令

## [1.5.7] - 2024-12-06

### Changed
- 更新包开发指南
