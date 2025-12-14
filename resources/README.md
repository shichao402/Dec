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

### 创建规则包

1. 创建 `package.json`：

```json
{
  "name": "my-pack",
  "version": "1.0.0",
  "type": "rule",
  "description": "我的规则包",
  "rules": ["rules/my-rules.mdc"],
  "repository": {
    "type": "git",
    "url": "https://github.com/user/my-pack"
  }
}
```

2. 创建规则文件 `rules/my-rules.mdc`

3. 链接到本地测试：`dec link`

4. 发布后通知注册表：`dec publish-notify`

## 快速链接

- **我要开发一个包** → 查看 [README.md](../README.md) 的"开发包"部分
- **我要参与开发 Dec** → [Documents/README.md](../Documents/README.md)
