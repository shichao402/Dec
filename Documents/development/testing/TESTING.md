# Dec 测试指南

本文档描述当前仓库可执行的测试方式，以及哪些行为应被视为“测试通过”。

## 测试目标

当前测试应覆盖以下核心能力：

- `dec init` 正确生成项目配置
- `dec vault init` 正确连接或创建 Vault
- `dec vault pull` 能同步资产到全部目标 IDE，并回写 `vault.yaml`
- `dec sync` 能根据声明同步 Skill / Rule / MCP
- 同步失败时能恢复原有托管内容
- `dec vault save` 与 `dec vault status` 能追踪本地变更

## 自动化测试

### 1. Go 单元测试

```bash
go test ./... -v -cover
```

用于验证：

- 配置读写
- IDE 路径与 MCP 合并
- Vault 追踪逻辑
- 同步服务的关键分支

### 2. 自托管流程测试

```bash
./scripts/run-tests.sh
```

该入口会调用 `scripts/run-tests.py`，进一步执行 `scripts/self_host_test.py`，验证接近真实使用流程的场景，包括：

- 初始化项目
- 初始化本地 Vault remote
- `vault pull` 自动更新 `.dec/config/vault.yaml`
- `sync` 输出到 IDE 目录
- `vault save` 回写到 remote
- `vault status` 检测本地修改

## 推荐执行顺序

日常开发建议按以下顺序验证：

```bash
make test
./scripts/run-tests.sh
```

如果只改动了文档或纯脚本注释，可视情况跳过耗时较长的流程测试；如果改动涉及 `cmd/`、`pkg/service/`、`pkg/vault/`、`pkg/config/`，应运行完整测试。

## 手动冒烟测试

建议始终在隔离环境下进行：

```bash
export DEC_HOME="$(pwd)/.tmp/dec-home"
rm -rf "$DEC_HOME"
mkdir -p "$DEC_HOME"

go build -o dist/dec .
./dist/dec --help
```

### 1. 初始化项目

```bash
mkdir -p /tmp/dec-smoke && cd /tmp/dec-smoke
/path/to/dec init
```

预期生成：

```text
.dec/config/
├── ides.yaml
└── vault.yaml
```

### 2. 验证配置语义

- `ides.yaml` 中默认启用 `cursor`
- `vault.yaml` 中按 `vault_skills` / `vault_rules` / `vault_mcps` 分组
- 不再生成 `technology.yaml` 或 `mcp.yaml`

### 3. 验证同步行为

当 `vault.yaml` 中没有声明资产时：

```bash
/path/to/dec sync
```

预期：

- 命令成功结束
- 输出提示当前未声明资产
- 不应误删用户的非 `dec-*` 内容

### 4. 验证 Vault 流程

推荐参考 `scripts/self_host_test.py` 使用本地 bare Git 仓库做完整演练，而不是依赖线上仓库。

## 常见误区

### 命令成功执行 ≠ 行为正确

至少还要检查：

- 关键文件是否存在
- 文件内容是否符合当前配置格式
- 托管路径是否带 `dec-` 前缀
- MCP 是否保留了用户自定义的非托管条目

### 不要再用旧配置模型做测试基线

以下检查都已经过时：

- `.dec/config/technology.yaml`
- `.dec/config/mcp.yaml`
- `.dec/config/packs.yaml`
- 顶层 `dec list`
- GitHub Actions 定时回归任务

## 维护建议

当新增命令或更改同步逻辑时：

- 先补或修改 `go test` 用例
- 再同步更新 `scripts/self_host_test.py`
- 最后回写 `README.md` 与 `Documents/`
