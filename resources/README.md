# Dec 资源目录

本目录包含 Dec 项目的资源文件。

> **注意**：项目开发文档请查看 [Documents/README.md](../Documents/README.md)。

## 目录结构

```
resources/
├── README.md                 # 本文件
└── temp/                     # 临时文件（不纳入版本控制）
    └── .gitkeep
```

## 包开发指南

包开发文档和规则现已通过 **CursorColdStart** 的 `dec` pack 提供。

**获取包开发指南：**

```bash
# 在包项目中启用 dec pack
coldstart enable dec
coldstart init .
```

这将生成 AI 规则文件，包含完整的包开发、打包、发布和注册流程指南。

## 快速链接

- **我要开发一个包** → 运行 `coldstart enable dec`
- **我要参与开发 Dec** → [../Documents/README.md](../Documents/README.md)
