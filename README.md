# CursorToolset

Cursor 工具集管理器 - 一个简洁的包管理工具，用于管理和安装 Cursor AI 工具集。

## 设计理念

- **简单** - 像 pip/brew 一样简单：下载、解压、完成
- **安全** - 不执行任何脚本，只做文件分发
- **透明** - 所有包信息公开可查，SHA256 校验

## 快速开始

### 安装 CursorToolset

#### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/firoyang/CursorToolset/main/install.sh | bash
```

#### Windows (PowerShell)

```powershell
iwr -useb https://raw.githubusercontent.com/firoyang/CursorToolset/main/install.ps1 | iex
```

### 基本使用

```bash
# 更新包索引
cursortoolset registry update

# 列出可用包
cursortoolset list

# 安装包
cursortoolset install github-action-toolset

# 搜索包
cursortoolset search github

# 查看包详情
cursortoolset info github-action-toolset

# 卸载包
cursortoolset uninstall github-action-toolset
```

## 命令参考

### 包管理

| 命令 | 说明 |
|------|------|
| `install [name]` | 安装包（不指定则安装所有） |
| `uninstall <name>` | 卸载包 |
| `list [--installed]` | 列出可用/已安装的包 |
| `search <keyword>` | 搜索包 |
| `info <name>` | 查看包详情 |
| `update` | 更新管理器和包 |

### Registry 管理

| 命令 | 说明 |
|------|------|
| `registry update` | 更新本地包索引 |
| `registry list` | 列出 registry 中的包 |
| `registry add <name>` | 添加包（维护者） |
| `registry remove <name>` | 移除包（维护者） |
| `registry export` | 导出 registry |

### 包开发

| 命令 | 说明 |
|------|------|
| `init <name>` | 初始化新的包项目 |

### 其他

| 命令 | 说明 |
|------|------|
| `clean [--cache] [--all]` | 清理缓存或所有 |
| `update --self` | 更新管理器本身 |

## 目录结构

```
~/.cursortoolsets/
├── repos/                    # 已安装的包
│   └── github-action-toolset/
├── cache/
│   ├── packages/             # 下载缓存
│   └── manifests/            # manifest 缓存
├── config/
│   └── registry.json         # 本地 registry
└── bin/
    └── cursortoolset
```

## 开发包

### 初始化包项目

```bash
cursortoolset init my-toolset
cd my-toolset
```

生成的结构：

```
my-toolset/
├── toolset.json          # 包配置（必需）
├── .cursortoolset/       # 开发规则
├── README.md
└── .gitignore
```

### toolset.json 规范

```json
{
  "name": "my-toolset",
  "displayName": "My Toolset",
  "version": "1.0.0",
  "description": "包描述",
  "author": "作者",
  "license": "MIT",
  "keywords": ["keyword1", "keyword2"],
  
  "repository": {
    "type": "git",
    "url": "https://github.com/user/my-toolset.git"
  },
  
  "dist": {
    "tarball": "https://github.com/user/my-toolset/releases/download/v1.0.0/my-toolset-1.0.0.tar.gz",
    "sha256": "abc123..."
  },
  
  "cursortoolset": {
    "minVersion": "1.0.0"
  }
}
```

### 发布包

1. **打包**
   ```bash
   tar -czvf my-toolset-1.0.0.tar.gz *
   shasum -a 256 my-toolset-1.0.0.tar.gz
   ```

2. **更新 toolset.json**
   - 更新 `version`
   - 更新 `dist.tarball` URL
   - 更新 `dist.sha256`

3. **创建 GitHub Release**
   - 创建 tag: `git tag v1.0.0`
   - 上传 tarball 到 Release

4. **提交到 Registry**
   - Fork CursorToolset 仓库
   - 编辑 `registry.json` 添加你的包
   - 提交 PR

详细指南请查看 [PACKAGE_DEV.md](./PACKAGE_DEV.md)

## Registry

Registry 是包的索引文件，托管在 GitHub Release 中：

```json
{
  "version": "1",
  "packages": [
    {
      "name": "github-action-toolset",
      "manifestUrl": "https://raw.githubusercontent.com/.../toolset.json"
    }
  ]
}
```

管理器通过以下流程获取包：

```
1. 下载 registry.json（从 GitHub Release）
2. 获取包的 manifestUrl
3. 下载 toolset.json（manifest）
4. 从 manifest.dist.tarball 下载包
5. 验证 SHA256
6. 解压到本地
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `CURSOR_TOOLSET_HOME` | 安装根目录 | `~/.cursortoolsets` |

## 从源码构建

```bash
git clone https://github.com/firoyang/CursorToolset.git
cd CursorToolset
go build -o cursortoolset .
```

## 许可证

MIT
