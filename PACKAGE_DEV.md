# 包开发指南

本文档介绍如何开发和发布 CursorToolset 包。

## 概述

CursorToolset 包是一个包含 AI 规则、提示词模板等资源的压缩包。包管理器只负责下载和解压，**不执行任何脚本**。

## 快速开始

### 1. 初始化包项目

```bash
cursortoolset init my-awesome-toolset --author "Your Name"
cd my-awesome-toolset
```

这会生成以下结构：

```
my-awesome-toolset/
├── toolset.json              # 包配置文件（必需）
├── .cursortoolset/           # 开发规则目录
│   └── rules/
│       └── dev-guide.md
├── README.md                 # 包说明文档
└── .gitignore
```

### 2. 添加你的内容

在包目录中添加你的规则文件、提示词模板等：

```
my-awesome-toolset/
├── toolset.json
├── rules/                    # 你的规则文件
│   ├── coding-style.md
│   └── best-practices.md
├── prompts/                  # 提示词模板（可选）
│   └── code-review.md
└── README.md
```

### 3. 更新 toolset.json

编辑 `toolset.json`，填写完整信息：

```json
{
  "name": "my-awesome-toolset",
  "displayName": "My Awesome Toolset",
  "version": "1.0.0",
  "description": "一个很棒的 AI 工具集",
  "author": "Your Name",
  "license": "MIT",
  "keywords": ["coding", "ai", "rules"],
  
  "repository": {
    "type": "git",
    "url": "https://github.com/yourname/my-awesome-toolset.git"
  },
  
  "dist": {
    "tarball": "https://github.com/yourname/my-awesome-toolset/releases/download/v1.0.0/my-awesome-toolset-1.0.0.tar.gz",
    "sha256": ""
  },
  
  "cursortoolset": {
    "minVersion": "1.0.0"
  }
}
```

### 4. 打包发布

```bash
# 打包（排除不需要的文件）
tar -czvf my-awesome-toolset-1.0.0.tar.gz \
  --exclude='.git' \
  --exclude='.DS_Store' \
  --exclude='*.tar.gz' \
  *

# 计算 SHA256
shasum -a 256 my-awesome-toolset-1.0.0.tar.gz
# 输出: abc123def456...  my-awesome-toolset-1.0.0.tar.gz

# 更新 toolset.json 中的 sha256 字段
```

### 5. 创建 GitHub Release

1. 提交代码并推送
   ```bash
   git add .
   git commit -m "Release v1.0.0"
   git push origin main
   ```

2. 创建 Tag
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

3. 在 GitHub 创建 Release
   - 访问仓库的 Releases 页面
   - 点击 "Create a new release"
   - 选择 tag `v1.0.0`
   - 上传 `my-awesome-toolset-1.0.0.tar.gz`
   - 发布

### 6. 提交到 Registry

1. Fork [CursorToolset](https://github.com/shichao402/CursorToolset) 仓库

2. 编辑 `registry.json`，添加你的包：
   ```json
   {
     "version": "1",
     "packages": [
       {
         "name": "my-awesome-toolset",
         "manifestUrl": "https://raw.githubusercontent.com/yourname/my-awesome-toolset/main/toolset.json"
       }
     ]
   }
   ```

3. 提交 Pull Request

## toolset.json 规范

### 必需字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 包名，只能包含小写字母、数字、连字符 |
| `version` | string | 语义化版本号 (SemVer) |
| `dist.tarball` | string | 下载地址 |
| `dist.sha256` | string | SHA256 校验和 |

### 可选字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `displayName` | string | 显示名称 |
| `description` | string | 描述 |
| `author` | string | 作者 |
| `license` | string | 许可证 |
| `keywords` | string[] | 关键词（用于搜索） |
| `repository.url` | string | 仓库地址 |
| `dependencies` | string[] | 依赖的包名列表 |
| `cursortoolset.minVersion` | string | 最低管理器版本 |

### 完整示例

```json
{
  "name": "github-action-toolset",
  "displayName": "GitHub Action AI 工具集",
  "version": "1.0.0",
  "description": "帮助 AI 助手更好地完成 GitHub Actions CI/CD 任务",
  "author": "shichao402",
  "license": "MIT",
  "keywords": ["github", "actions", "ci", "cd", "workflow"],
  
  "repository": {
    "type": "git",
    "url": "https://github.com/shichao402/GithubActionAISelfBuilder.git"
  },
  
  "dist": {
    "tarball": "https://github.com/shichao402/GithubActionAISelfBuilder/releases/download/v1.0.0/github-action-toolset-1.0.0.tar.gz",
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "size": 102400
  },
  
  "cursortoolset": {
    "minVersion": "1.0.0"
  },
  
  "dependencies": []
}
```

## 版本号规范

使用 [语义化版本](https://semver.org/lang/zh-CN/) (SemVer)：

```
MAJOR.MINOR.PATCH

例如: 1.0.0, 1.2.3, 2.0.0-beta.1
```

- **MAJOR**: 不兼容的 API 变更
- **MINOR**: 向下兼容的功能新增
- **PATCH**: 向下兼容的问题修复

### 版本更新流程

1. 修改 `toolset.json` 中的 `version`
2. 更新 `dist.tarball` URL 中的版本号
3. 重新打包并计算 SHA256
4. 更新 `dist.sha256`
5. 创建新的 Git Tag 和 Release

## 包内容建议

### 推荐的目录结构

```
my-toolset/
├── toolset.json              # 包配置（必需）
├── README.md                 # 使用说明
├── rules/                    # AI 规则文件
│   ├── general.md           # 通用规则
│   └── specific.md          # 特定场景规则
├── prompts/                  # 提示词模板（可选）
│   └── templates.md
└── examples/                 # 示例（可选）
    └── demo.md
```

### 规则文件编写建议

1. **清晰的标题和描述**
   ```markdown
   # 代码审查规则
   
   本规则帮助 AI 进行代码审查...
   ```

2. **具体的指令**
   ```markdown
   ## 检查项
   
   1. 检查变量命名是否符合规范
   2. 检查是否有未处理的错误
   3. ...
   ```

3. **示例**
   ```markdown
   ## 示例
   
   好的代码:
   ```python
   def get_user(user_id: int) -> User:
       ...
   ```
   ```

## 测试包

在发布前，可以本地测试：

```bash
# 打包
tar -czvf test.tar.gz *

# 手动解压测试
mkdir -p /tmp/test-install
tar -xzvf test.tar.gz -C /tmp/test-install

# 检查内容
ls -la /tmp/test-install
```

## 常见问题

### Q: 包名有什么限制？

包名只能包含：
- 小写字母 (a-z)
- 数字 (0-9)
- 连字符 (-)

不能以连字符开头或结尾。

### Q: 如何更新已发布的包？

1. 修改代码
2. 更新版本号
3. 重新打包
4. 创建新的 Release
5. 更新 `toolset.json` 中的 `dist` 信息

### Q: 如何处理依赖？

在 `toolset.json` 中声明依赖：

```json
{
  "dependencies": ["base-toolset", "common-rules"]
}
```

管理器会在安装时自动安装依赖。

### Q: SHA256 怎么计算？

```bash
# macOS / Linux
shasum -a 256 my-package.tar.gz

# 或使用 openssl
openssl dgst -sha256 my-package.tar.gz
```

## 获取帮助

- 查看示例包: [github-action-toolset](https://github.com/shichao402/GithubActionAISelfBuilder)
- 提交 Issue: [CursorToolset Issues](https://github.com/shichao402/CursorToolset/issues)
