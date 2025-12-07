# Changelog

所有重要的变更都会记录在这个文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Added
- 待添加的新功能

### Changed
- 待变更的内容

### Fixed
- 待修复的问题

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
