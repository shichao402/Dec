# CursorToolset 分发资源目录

本目录包含随 CursorToolset 分发给**包项目**使用的资源文件。

> **注意**：项目开发文档请查看 [Documents/README.md](../Documents/README.md)。

## 目录结构

```
resources/
├── README.md                 # 本文件
├── public/                   # 公开资源（随二进制分发）
│   ├── package-dev-guide.md  # 包开发指南模板
│   ├── release-workflow-template.yml  # GitHub Actions 发布模板
│   └── examples/             # 配置示例
└── temp/                     # 临时文件（不纳入版本控制）
    └── .gitkeep
```

## 资源说明

### 公开资源 (`public/`)

通过 `cursortoolset init` 和 `cursortoolset sync` 命令分发给包项目的资源。

| 文件 | 说明 |
|------|------|
| `package-dev-guide.md` | 包开发完整指南 |
| `release-workflow-template.yml` | GitHub Actions 发布模板 |
| `examples/` | 配置示例（package.json 等） |

## 快速链接

- **我要开发一个包** → [public/package-dev-guide.md](public/package-dev-guide.md)
- **我要参与开发 CursorToolset** → [../Documents/README.md](../Documents/README.md)
