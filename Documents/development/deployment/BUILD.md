# Dec 构建安装指南

本文档描述当前代码库的构建、安装与卸载方式。

## 从源码构建

### 前置条件

- Go 1.21+
- Git
- Python 3（仅在运行 `./scripts/run-tests.sh` 时需要）

### 直接构建

```bash
git clone https://github.com/shichao402/Dec.git
cd Dec
go build -o dist/dec .
./dist/dec --version
```

### 使用 Makefile

```bash
make build          # 构建当前平台版本
make build-all      # 构建全部平台版本
make test           # 运行 Go 单元测试
make test-self-host # 运行自托管流程测试
```

### 使用构建脚本

```bash
./scripts/build.sh
./scripts/build.sh --all
```

`scripts/build.sh` 会：

- 读取 `version.json`
- 注入构建时间与版本号
- 输出构建日志到 `logs/`
- 生成 `dist/BUILD_INFO*.txt`

## 安装方式

### 方式一：在线安装（推荐）

#### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.sh | bash
```

#### Windows PowerShell

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.ps1 | iex
```

### 方式二：开发安装

```bash
./scripts/install-dev.sh
```

如需安装前先跑单元测试：

```bash
./scripts/install-dev.sh --test
```

## 安装目录

默认安装到 `~/.dec`，也可通过 `DEC_HOME` 覆盖：

```bash
export DEC_HOME=/custom/path
```

当前目录布局如下：

```text
~/.dec/
├── bin/
│   └── dec
├── config/
│   └── system.json
├── config.yaml      # 首次执行 vault 相关命令后可能生成
└── vault/           # 执行 dec vault init 后生成
```

## 相关环境变量

| 变量 | 说明 | 默认值 |
|---|---|---|
| `DEC_HOME` | Dec 根目录 | `~/.dec` |
| `DEC_BRANCH` | 安装脚本读取的发布分支 | `ReleaseLatest` |

## 验证安装

```bash
dec --version
dec --help
dec init
```

如果你只想确认 Vault 子命令可用，也可以执行：

```bash
dec vault list
```

## 卸载

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/uninstall.sh | bash
```

### Windows PowerShell

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/uninstall.ps1 | iex
```

**注意**：卸载会删除整个 `DEC_HOME` 目录，因此也会删除其中的本地 Vault 与全局配置。

## 常见问题

### 找不到 `dec`

确保 `~/.dec/bin` 已加入 PATH，或重新打开终端。

### 想用隔离目录安装测试版本

```bash
DEC_HOME=/tmp/dec-test DEC_BRANCH=ReleaseTest bash ./scripts/install.sh
```

### 安装成功但 `vault` 还不存在

这是正常现象。只有在执行 `dec vault init` 后，本地 `vault/` 目录才会创建。
