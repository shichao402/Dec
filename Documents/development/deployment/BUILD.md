# Dec 构建安装指南

本文档描述如何构建和安装 Dec。

## 从源码构建

### 前置条件

- Go 1.21+
- Git

### 构建步骤

```bash
# 克隆仓库
git clone https://github.com/shichao402/Dec.git
cd Dec

# 构建
go build -o dec .

# 验证
./dec --version
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
curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.sh | bash
```

#### Windows (PowerShell)

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.ps1 | iex
```

### 方式二：手动安装

1. 从 [Releases](https://github.com/shichao402/Dec/releases) 下载对应平台的二进制
2. 解压到 `~/.dec/bin/`
3. 添加到 PATH

```bash
# Linux/macOS
mkdir -p ~/.dec/bin
mv dec-* ~/.dec/bin/dec
chmod +x ~/.dec/bin/dec
export PATH="$HOME/.dec/bin:$PATH"

# 永久生效
echo 'export PATH="$HOME/.dec/bin:$PATH"' >> ~/.bashrc
```

### 方式三：开发安装

从源码安装到系统（覆盖现有安装）：

```bash
cd /path/to/Dec
make install-dev
```

## 目录结构

安装后的目录结构：

```
~/.dec/
├── bin/
│   └── dec                   # 可执行文件
├── config.yaml               # 全局配置
└── cache/
    └── packages-v1.0.0/      # 包缓存
        ├── rules/
        └── mcp/
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DEC_HOME` | 安装根目录 | `~/.dec` |

## 交叉编译

构建其他平台的二进制：

```bash
# macOS Intel
GOOS=darwin GOARCH=amd64 go build -o dec-darwin-amd64 .

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -o dec-darwin-arm64 .

# Linux x64
GOOS=linux GOARCH=amd64 go build -o dec-linux-amd64 .

# Windows x64
GOOS=windows GOARCH=amd64 go build -o dec-windows-amd64.exe .
```

## 版本注入

构建时注入版本信息：

```bash
VERSION=$(cat version.json | jq -r '.version')
BUILD_TIME=$(date '+%Y-%m-%d_%H:%M:%S')

go build -ldflags "-X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o dec .
```

## 验证安装

```bash
# 检查版本
dec --version

# 列出可用包
dec list

# 同步规则（在项目目录中）
dec sync
```

## 卸载

```bash
# 删除安装目录
rm -rf ~/.dec

# 从 PATH 中移除（编辑 ~/.bashrc 或 ~/.zshrc）
```

## 常见问题

### Q: 命令找不到？

确保 `~/.dec/bin` 在 PATH 中：

```bash
export PATH="$HOME/.dec/bin:$PATH"
```

### Q: 权限被拒绝？

```bash
chmod +x ~/.dec/bin/dec
```

### Q: 构建失败？

检查 Go 版本：

```bash
go version  # 需要 1.21+
```

## 相关文档

- [开发指南](../setup/DEVELOPMENT.md)
- [测试指南](../testing/TESTING.md)
