# CursorToolset 构建安装指南

本文档描述如何构建和安装 CursorToolset。

## 从源码构建

### 前置条件

- Go 1.21+
- Git

### 构建步骤

```bash
# 克隆仓库
git clone https://github.com/shichao402/CursorToolset.git
cd CursorToolset

# 构建
go build -o cursortoolset .

# 验证
./cursortoolset --version
```

### 使用 Makefile

```bash
make build          # 构建
make clean          # 清理
make install-dev    # 安装到系统
```

## 安装方式

### 方式一：一键安装（推荐）

#### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseLatest/scripts/install.sh | bash
```

#### Windows (PowerShell)

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseLatest/scripts/install.ps1 | iex
```

### 方式二：手动安装

1. 从 [Releases](https://github.com/shichao402/CursorToolset/releases) 下载对应平台的二进制
2. 解压到 `~/.cursortoolsets/bin/`
3. 添加到 PATH

```bash
# Linux/macOS
mkdir -p ~/.cursortoolsets/bin
tar -xzf cursortoolset-*.tar.gz -C ~/.cursortoolsets/bin/
export PATH="$HOME/.cursortoolsets/bin:$PATH"

# 永久生效
echo 'export PATH="$HOME/.cursortoolsets/bin:$PATH"' >> ~/.bashrc
```

### 方式三：开发安装

从源码安装到系统（覆盖现有安装）：

```bash
cd /path/to/CursorToolset
make install-dev
```

## 目录结构

安装后的目录结构：

```
~/.cursortoolsets/
├── bin/
│   └── cursortoolset       # 可执行文件
├── repos/                   # 已安装的包
│   └── github-action-toolset/
├── cache/
│   ├── packages/           # 下载缓存
│   └── manifests/          # manifest 缓存
└── config/
    ├── registry.json       # 本地包索引
    └── system.json         # 系统配置
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `CURSOR_TOOLSET_HOME` | 安装根目录 | `~/.cursortoolsets` |

## 交叉编译

构建其他平台的二进制：

```bash
# macOS Intel
GOOS=darwin GOARCH=amd64 go build -o cursortoolset-darwin-amd64 .

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -o cursortoolset-darwin-arm64 .

# Linux x64
GOOS=linux GOARCH=amd64 go build -o cursortoolset-linux-amd64 .

# Windows x64
GOOS=windows GOARCH=amd64 go build -o cursortoolset-windows-amd64.exe .
```

## 版本注入

构建时注入版本信息：

```bash
VERSION=$(cat version.json | jq -r '.version')
BUILD_TIME=$(date '+%Y-%m-%d_%H:%M:%S')

go build -ldflags "-X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o cursortoolset .
```

## 验证安装

```bash
# 检查版本
cursortoolset --version

# 更新包索引
cursortoolset registry update

# 列出可用包
cursortoolset list
```

## 卸载

```bash
# 删除安装目录
rm -rf ~/.cursortoolsets

# 从 PATH 中移除（编辑 ~/.bashrc 或 ~/.zshrc）
```

## 常见问题

### Q: 命令找不到？

确保 `~/.cursortoolsets/bin` 在 PATH 中：

```bash
export PATH="$HOME/.cursortoolsets/bin:$PATH"
```

### Q: 权限被拒绝？

```bash
chmod +x ~/.cursortoolsets/bin/cursortoolset
```

### Q: 构建失败？

检查 Go 版本：

```bash
go version  # 需要 1.21+
```

## 相关文档

- [开发指南](DEVELOPMENT.md)
- [测试指南](TESTING.md)
