# Dec 开发指南

本文档面向当前仓库的开发者，描述与**现有实现**一致的开发方式。

## 当前实现范围

当前 CLI 只实现了以下主线命令：

- `dec init`
- `dec sync`
- `dec vault init`
- `dec vault save`
- `dec vault find`
- `dec vault pull`
- `dec vault push`
- `dec vault list`
- `dec vault status`

以下内容不属于当前实现，不应再作为开发基线：

- 顶层 `dec list`
- `dec serve`
- `dec publish-notify`
- `technology.yaml` / `mcp.yaml` / `packs.yaml`
- 依赖 GitHub Actions 的自动发布流程

## 环境准备

### 必需工具

```bash
go version        # Go 1.21+
python3 --version # 用于运行 scripts/run-tests.sh
```

### 推荐工具

```bash
golangci-lint --version  # 代码检查
gh --version             # 仅在使用 dec vault init --create 时需要
```

## 项目结构

```text
Dec/
├── cmd/
│   ├── root.go
│   ├── init.go
│   ├── sync.go
│   └── vault.go
├── pkg/
│   ├── config/
│   ├── ide/
│   ├── paths/
│   ├── service/
│   ├── types/
│   ├── vault/
│   └── version/
├── config/
│   ├── system.json
│   └── registry.json
├── scripts/
│   ├── build.sh
│   ├── install.sh
│   ├── install.ps1
│   ├── install-dev.sh
│   ├── uninstall.sh
│   ├── uninstall.ps1
│   ├── release-yolo.sh
│   └── run-tests.sh
├── Documents/
├── Makefile
├── main.go
└── version.json
```

## 常用开发命令

```bash
make build          # 构建当前平台二进制到 dist/
make build-all      # 构建全部平台产物
make test           # 运行 Go 单元测试
make test-self-host # 运行自托管流程测试
make fmt            # 格式化 Go 代码
make lint           # 运行 golangci-lint
```

也可以直接使用源码运行：

```bash
go run . --help
go run . init
go run . sync
go run . vault list
```

## 隔离开发环境

为了避免污染本机环境，建议开发时显式设置 `DEC_HOME`：

```bash
export DEC_HOME="$(pwd)/.tmp/dec-home"
mkdir -p "$DEC_HOME"
```

这样：

- 全局配置会写入 `DEC_HOME/config.yaml`
- 本地 Vault 会落在 `DEC_HOME/vault/`
- 安装脚本和测试脚本也会使用同一套隔离目录

## 本地调试建议

### 调试 `init` / `sync`

```bash
export DEC_HOME="$(pwd)/.tmp/dec-home"
go run . init
go run . sync
```

### 调试 Vault 行为

推荐使用临时目录和本地 bare Git 仓库，参考 `scripts/self_host_test.py` 的做法。

### 调试安装脚本

```bash
DEC_HOME="$(pwd)/.tmp/install-home" ./scripts/install-dev.sh
```

## 修改时的同步要求

### 命令或帮助文案变更

如果 `cmd/` 中的子命令、参数或输出语义发生变化，至少同步更新：

- 根 `README.md`
- `Documents/design/architecture/ARCHITECTURE.md`
- `Documents/development/testing/TESTING.md`

### 配置格式变更

如果 `.dec/config/ides.yaml` 或 `.dec/config/vault.yaml` 的格式变化，必须同步更新：

- `README.md`
- `Documents/design/architecture/ARCHITECTURE.md`
- 相关测试用例

### 安装目录变更

如果安装目录或环境变量变化，必须同步更新：

- `scripts/install.sh`
- `scripts/install.ps1`
- `scripts/uninstall.sh`
- `scripts/uninstall.ps1`
- `Documents/development/deployment/BUILD.md`

## 版本信息

默认通过 `version.json` 记录版本号，并在构建时用 `ldflags` 注入：

```bash
go build -ldflags "-X main.Version=$(jq -r '.version' version.json) -X main.BuildTime=$(date -u '+%Y-%m-%d_%H:%M:%S')" -o dist/dec .
```

## 当前发布约定

仓库中的 GitHub Actions 已停用，发布流程请参考 `Documents/development/deployment/RELEASE.md` 中的**手动发布**说明。
