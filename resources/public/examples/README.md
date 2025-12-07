# CursorToolset 示例配置

本目录包含各种 `package.json` 配置示例，帮助包开发者快速上手。

## 文件列表

### toolset-with-bin.json

展示如何配置 `bin` 字段，暴露可执行程序。

**特性：**
- 多个可执行程序配置
- 命令别名（`example` 和 `ex`）
- 不同类型的脚本（init、build、test）

**适用场景：**
- CLI 工具
- 开发辅助脚本集
- 构建工具链

## 快速开始

### 1. 基础配置（最小化）

```json
{
  "name": "my-simple-toolset",
  "version": "1.0.0",
  "description": "一个简单的工具集",
  
  "dist": {
    "tarball": "https://github.com/user/my-simple-toolset/releases/download/v1.0.0/my-simple-toolset-1.0.0.tar.gz",
    "sha256": "abc123..."
  }
}
```

### 2. 带 bin 的配置

```json
{
  "name": "my-cli-toolset",
  "version": "1.0.0",
  "description": "一个 CLI 工具集",
  
  "bin": {
    "mycli": "bin/mycli"
  },
  
  "dist": {
    "tarball": "https://github.com/user/my-cli-toolset/releases/download/v1.0.0/my-cli-toolset-1.0.0.tar.gz",
    "sha256": "abc123..."
  }
}
```

### 3. 完整配置

```json
{
  "name": "awesome-toolset",
  "displayName": "Awesome Toolset",
  "version": "2.1.0",
  "description": "一套完整的开发工具",
  "author": "Your Name <your.email@example.com>",
  "license": "MIT",
  "keywords": ["dev", "tools", "cli", "automation"],
  
  "repository": {
    "type": "git",
    "url": "https://github.com/user/awesome-toolset.git"
  },
  
  "bin": {
    "awesome": "bin/awesome",
    "aws": "bin/awesome",
    "awesome-dev": "scripts/dev.sh",
    "awesome-prod": "scripts/prod.sh"
  },
  
  "dist": {
    "tarball": "https://github.com/user/awesome-toolset/releases/download/v2.1.0/awesome-toolset-2.1.0.tar.gz",
    "sha256": "a1b2c3d4e5f6...",
    "size": 1048576
  },
  
  "cursortoolset": {
    "minVersion": "1.2.0"
  },
  
  "dependencies": [
    "dependency-toolset-1",
    "dependency-toolset-2"
  ]
}
```

## 字段说明

### 必需字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 包名（唯一标识，小写字母+连字符） |
| `version` | string | 版本号（语义化版本，如 1.0.0） |
| `dist.tarball` | string | 下载地址（tar.gz 格式） |
| `dist.sha256` | string | SHA256 校验和 |

### 可选字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `displayName` | string | 显示名称 |
| `description` | string | 包描述 |
| `author` | string | 作者信息 |
| `license` | string | 许可证（如 MIT、Apache-2.0） |
| `keywords` | array | 关键词（用于搜索） |
| `repository` | object | Git 仓库信息 |
| `bin` | object | 可执行程序配置 |
| `cursortoolset.minVersion` | string | 最低管理器版本要求 |
| `dependencies` | array | 依赖的包名列表 |

### bin 字段详解

```json
{
  "bin": {
    "command-name": "path/to/executable"
  }
}
```

- **键**：命令名称（用户输入的命令）
- **值**：包内可执行文件的相对路径

**示例：**
```json
{
  "bin": {
    "mytool": "bin/mytool",              // 主命令
    "mt": "bin/mytool",                   // 别名
    "mytool-init": "scripts/init.sh",    // 子命令
    "mytool-config": "config/setup.py"   // 配置工具
  }
}
```

## 包目录结构示例

### 简单包（无 bin）

```
my-toolset/
├── package.json           # 包配置
├── .cursorrules           # Cursor 规则文件
└── README.md              # 文档
```

### CLI 工具包（带 bin）

```
my-cli-toolset/
├── package.json           # 包配置
├── bin/                   # 可执行文件
│   ├── mycli              # 主程序
│   └── helper             # 辅助工具
├── scripts/               # 脚本
│   ├── init.sh            # 初始化脚本
│   └── build.sh           # 构建脚本
├── .cursorrules           # Cursor 规则
└── README.md              # 文档
```

### 完整工具集

```
awesome-toolset/
├── package.json           # 包配置
├── bin/                   # 可执行文件
│   └── awesome
├── scripts/               # 脚本目录
│   ├── dev.sh
│   ├── prod.sh
│   └── utils/
├── rules/                 # Cursor 规则目录
│   ├── coding-style.md
│   └── best-practices.md
├── templates/             # 模板文件
├── docs/                  # 文档
└── README.md
```

## 开发流程

### 1. 创建包

```bash
# 使用 cursortoolset 初始化
cursortoolset init my-toolset

# 或手动创建目录
mkdir my-toolset
cd my-toolset
```

### 2. 编写 package.json

参考上面的示例配置。

### 3. 添加可执行程序（如需要）

```bash
mkdir -p bin scripts
echo '#!/usr/bin/env bash' > bin/mytool
echo 'echo "Hello from mytool"' >> bin/mytool
chmod +x bin/mytool
```

### 4. 打包

```bash
# 打包为 tar.gz
tar -czf my-toolset-1.0.0.tar.gz \
  package.json \
  bin/ \
  scripts/ \
  README.md

# 计算 SHA256
shasum -a 256 my-toolset-1.0.0.tar.gz
```

### 5. 发布

1. 在 GitHub 创建 Release
2. 上传 `my-toolset-1.0.0.tar.gz`
3. 更新 `package.json` 中的 `dist.tarball` 和 `dist.sha256`

### 6. 提交到 Registry

1. Fork [CursorToolset](https://github.com/shichao402/CursorToolset)
2. 编辑 `registry.json`：
   ```json
   {
     "version": "1",
     "packages": [
       {
         "name": "my-toolset",
         "repository": "https://github.com/user/my-toolset"
       }
     ]
   }
   ```
3. 提交 Pull Request

## 最佳实践

### ✅ 推荐做法

1. **使用语义化版本**
   - 格式：`major.minor.patch`
   - 示例：`1.0.0`、`2.1.3`

2. **提供详细的描述和关键词**
   ```json
   {
     "description": "一个用于 React 项目的开发工具集",
     "keywords": ["react", "dev", "tools", "cli"]
   }
   ```

3. **bin 命令使用包名前缀**
   ```json
   {
     "bin": {
       "mytool": "bin/main",
       "mytool-init": "scripts/init.sh",
       "mytool-config": "scripts/config.sh"
     }
   }
   ```

4. **使用 shebang 让脚本跨平台**
   ```bash
   #!/usr/bin/env bash
   #!/usr/bin/env python3
   #!/usr/bin/env node
   ```

### ⚠️ 注意事项

1. **包名冲突**
   - 检查 registry 中是否已存在同名包
   - 使用有意义的唯一名称

2. **命令名冲突**
   - 避免使用常见系统命令名（如 `ls`、`cp`）
   - 建议使用包名作为前缀

3. **tarball 大小**
   - 建议小于 10MB
   - 不要包含 `node_modules`、`.git` 等

4. **跨平台兼容**
   - 使用跨平台的脚本语言
   - 或提供多平台的二进制文件

## 常见问题

### Q: 如何更新包？

A: 修改 `version`、`dist.tarball`、`dist.sha256`，创建新的 Release。

### Q: bin 命令不生效？

A: 确保：
1. 文件路径正确
2. 文件有执行权限（`chmod +x`）
3. 用户已将 `~/.cursortoolsets/bin` 添加到 PATH

### Q: 如何测试包？

A: 本地测试：
```bash
# 1. 构建 tarball
tar -czf test-1.0.0.tar.gz *

# 2. 计算 SHA256
shasum -a 256 test-1.0.0.tar.gz

# 3. 更新 package.json 使用本地路径
# 4. 安装测试
cursortoolset install test --no-cache
```

## 相关文档

- [包开发指南](../PACKAGE_DEV.md)
- [Bin 功能文档](../BIN_FEATURE.md)
- [使用示例](../USAGE_EXAMPLE.md)
