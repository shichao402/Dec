# Dec 测试指南

本文档描述 Dec 的测试流程和规范。

## 测试原则

> **重要**: 命令执行成功 ≠ 测试通过

每个测试必须有明确的验证条件：
1. 命令必须成功执行（exit code = 0）
2. 输出必须符合预期
3. **关键文件必须存在且内容正确**

## 测试类型

| 类型 | 命令/触发 | 说明 |
|------|-----------|------|
| 单元测试 | `make test` | Go 单元测试 |
| 功能测试 | `./scripts/run-tests.sh` | 完整功能验证 |
| 定时测试 | GitHub Actions (每天 8:00 北京时间) | 自动化回归测试 |
| 手动测试 | 见下文 | 按步骤手动验证 |

## CI 定时测试

### 触发条件

- **定时触发**: 北京时间每天早上 8:00 (UTC 0:00)
- **变更检查**: 仅当 main 分支在过去 24 小时有新提交时运行
- **手动触发**: 支持在 GitHub Actions 页面手动运行

### 测试内容

| 测试项 | 说明 |
|--------|------|
| 单元测试 | `go test ./... -v -cover` |
| 代码检查 | golangci-lint |
| 功能测试 | `./scripts/run-tests.sh` |
| 跨平台构建 | linux/darwin/windows × amd64/arm64 |

### 手动触发

1. 访问 [GitHub Actions](https://github.com/shichao402/Dec/actions/workflows/scheduled-test.yml)
2. 点击 "Run workflow"
3. 可选择 "跳过变更检查" 强制运行

## 快速测试

### 运行自动化测试

```bash
cd /path/to/Dec
./scripts/run-tests.sh
```

**预期输出**:
```
==========================================
Dec 完整功能测试
==========================================
...
测试结果统计
==========================================
通过: X
失败: 0

🎉 所有测试通过！
```

## 测试覆盖的功能

| # | 功能 | 命令 | 验证点 |
|---|------|------|--------|
| 1 | 初始化项目 | `init` | `.dec/config/` 目录和配置文件存在 |
| 2 | 列出包 | `list` | 输出包含可用包信息 |
| 3 | 同步规则 | `sync` | 规则文件生成到 `.cursor/rules/` |
| 4 | MCP 配置 | `sync` | `.cursor/mcp.json` 正确生成 |
| 5 | 版本显示 | `--version` | 显示当前版本 |

## 手动测试步骤

### 1. 构建管理器

```bash
cd /path/to/Dec
go build -o dec .
./dec --version
```

### 2. 初始化项目

```bash
cd /tmp
mkdir test-project && cd test-project
/path/to/dec init
ls -la .dec/config/
```

**预期文件结构**:
```
.dec/config/
├── ides.yaml
├── mcp.yaml
└── technology.yaml
```

### 3. 同步规则

```bash
/path/to/dec sync
ls -la .cursor/rules/
cat .cursor/mcp.json
```

**预期**:
- `.cursor/rules/` 目录包含规则文件
- `.cursor/mcp.json` 包含 MCP 配置

### 4. 测试包查询

```bash
/path/to/dec list
```

## 测试检查清单

每次修改后，确保以下测试通过：

- [ ] `make lint` 无错误
- [ ] `make test` 单元测试通过
- [ ] `./scripts/run-tests.sh` 功能测试通过
- [ ] `dec sync` 规则生成正确
- [ ] `dec sync` MCP 配置正确

## 常见问题

### Q: 规则文件没有生成？

检查 `.dec/config/packs.yaml` 中是否启用了对应的包。

### Q: MCP 配置为空？

确保 `dec` 包在 `packs.yaml` 中启用。

### Q: 测试脚本报错？

1. 检查 Go 版本（需要 1.21+）
2. 检查网络连接
3. 查看具体错误信息

### Q: 测试通过但功能有问题？

检查验证函数是否足够严格。例如：
- `sync` 后不仅要检查目录存在，还要检查规则文件内容
- 检查 MCP 配置格式是否正确

## 维护测试脚本

### 添加新验证

当发现测试遗漏时，在 `scripts/run-tests.sh` 中添加验证函数：

```bash
# 验证函数模板
verify_xxx() {
    # 1. 检查文件/目录存在
    if [ ! -f "$expected_file" ]; then
        echo "  ⚠️  文件不存在: $expected_file"
        return 1
    fi
    
    # 2. 检查内容正确
    if ! grep -q "expected_content" "$file"; then
        echo "  ⚠️  内容不符合预期"
        return 1
    fi
    
    echo "  ✓ 验证通过"
    return 0
}
```

## 相关文档

- [开发指南](../setup/DEVELOPMENT.md)
- [构建安装指南](../deployment/BUILD.md)
