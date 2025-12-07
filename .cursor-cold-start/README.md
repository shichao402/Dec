# CursorColdStart 配置目录

此目录由 CursorColdStart 工具管理。

## 目录结构

```
.cursor-cold-start/
├── config/
│   ├── project.json      # 项目基本信息
│   ├── technology.json   # 技术栈配置
│   └── packs.json        # 功能包配置
└── modules/              # 已注入的模块配置
```

## 使用方法

1. **填写配置文件** - 让 AI 帮助填写 config/ 下的配置文件
2. **再次运行初始化** - `coldstart init .` 生成定制规则

## 配置说明

### project.json
- name: 项目名称（必填）
- description: 项目描述
- version: 项目版本

### technology.json
- language: 编程语言（必填）- dart/typescript/python/kotlin/swift
- framework: 框架 - flutter/react/vue/django/fastapi/android/ios
- platforms: 目标平台 - android/ios/web/macos/windows/linux

### packs.json
功能包配置，每个功能包可以独立启用/禁用：
- logging: 日志系统
- version-management: 版本管理
- github-actions: GitHub Actions CI/CD
- documentation: 文档管理
- cursortoolset: CursorToolset 包管理
- update-module: 应用更新模块

注意：安全规范、调试规范、脚本规范已内置在核心规则中，无需单独配置。

运行 `coldstart list packs` 查看所有可用功能包。
