# CursorToolset 开发指南

本文档面向 CursorToolset 项目的开发者。

## 环境准备

### 必需工具

```bash
# Go 1.21+
go version

# golangci-lint（代码检查）
brew install golangci-lint
# 或: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# 验证
golangci-lint --version
```

### 可选工具

```bash
# jq（JSON 处理）
brew install jq

# gh（GitHub CLI，用于发布）
brew install gh
```

## 项目结构

```
CursorToolset/
├── cmd/                    # 命令行命令
│   ├── root.go            # 根命令
│   ├── install.go         # install 命令
│   ├── init.go            # init 命令
│   └── ...
├── pkg/                    # 核心包
│   ├── config/            # 配置管理
│   ├── installer/         # 安装器
│   ├── registry/          # 包索引
│   ├── paths/             # 路径管理
│   ├── types/             # 类型定义
│   └── version/           # 版本管理
├── config/
│   ├── registry.json      # 包注册表
│   └── system.json        # 系统配置模板
├── scripts/
│   ├── install.sh         # 正式安装脚本
│   ├── install-dev.sh     # 开发安装脚本
│   └── run-tests.sh       # 测试脚本
├── docs/
│   ├── public/            # 公开文档
│   ├── internal/          # 内部文档
│   └── temp/              # 临时文档
├── .github/workflows/     # CI/CD
├── version.json           # 版本信息
├── Makefile
└── main.go
```

## 开发流程

```
本地开发 → 构建测试 → 运行测试脚本 → 提交 main → 打 test tag → CI 构建 → 测试 → 打正式 tag → 发布
```

### 核心原则

1. **测试通过的产物直接发布** - 不重新构建
2. **配置文件驱动** - 不硬编码
3. **代码先提交到 main** - tag 基于 main 分支创建
4. **每次修改必须测试** - 运行 `scripts/run-tests.sh`

## 日常开发

### 构建与测试

```bash
# 构建
make build

# 代码检查（提交前必须运行）
make lint

# 运行单元测试
make test

# 运行完整功能测试
./scripts/run-tests.sh

# 源码安装到本地
make install-dev
```

### 验证安装

```bash
cursortoolset --version
cursortoolset list
```

## 代码规范

### 命名规范

- 文件名：小写下划线 `install_test.go`
- 包名：小写无下划线 `installer`
- 函数/方法：驼峰式 `InstallPackage`
- 常量：大写下划线 `DEFAULT_TIMEOUT`

### 错误处理

```go
// 使用 fmt.Errorf 包装错误
if err != nil {
    return fmt.Errorf("安装包失败: %w", err)
}

// 用户友好的错误信息
return fmt.Errorf("未找到包: %s\n\n提示: 运行 'cursortoolset registry update' 更新包索引", name)
```

### 输出规范

```go
// 使用 emoji 增强可读性
fmt.Println("📦 安装包...")
fmt.Println("✅ 安装完成")
fmt.Println("❌ 安装失败")
fmt.Println("⚠️  警告信息")
fmt.Println("ℹ️  提示信息")
```

### 交互式操作规范

**重要：所有需要用户确认的操作必须提供 `--yes` 或 `--force` 选项跳过确认。**

这是为了支持：
- AI 辅助开发场景（AI 无法处理交互式输入）
- 自动化脚本
- CI/CD 流程

```go
// Go 命令示例
var forceFlag bool

func init() {
    cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "跳过确认提示")
    // 或
    cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "跳过确认提示")
}

func runCommand() error {
    if !forceFlag {
        fmt.Print("确认操作？[y/N]: ")
        var response string
        fmt.Scanln(&response)
        if response != "y" && response != "Y" {
            return fmt.Errorf("用户取消")
        }
    }
    // 执行操作...
}
```

```bash
# Shell 脚本示例
SKIP_CONFIRM=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --yes|-y) SKIP_CONFIRM=true; shift ;;
        *) shift ;;
    esac
done

if [[ "${SKIP_CONFIRM}" != "true" ]]; then
    read -p "确认？(y/N): " -n 1 -r
    [[ ! $REPLY =~ ^[Yy]$ ]] && exit 0
fi
```

**现有命令的跳过确认选项：**

| 命令 | 选项 | 说明 |
|------|------|------|
| `clean` | `--force` / `-f` | 跳过清理确认 |
| `uninstall` | `--force` / `-f` | 跳过卸载确认 |
| `update --self` | `--yes` / `-y` | 跳过更新确认 |
| `scripts/uninstall.sh` | `--yes` / `-y` | 跳过卸载确认 |

## 添加新命令

1. 在 `cmd/` 下创建新文件
2. 定义 cobra.Command
3. 在 `init()` 中注册到 RootCmd
4. 更新测试脚本

```go
// cmd/mycommand.go
package cmd

import "github.com/spf13/cobra"

var myCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "命令简述",
    RunE: func(cmd *cobra.Command, args []string) error {
        // 实现
        return nil
    },
}

func init() {
    RootCmd.AddCommand(myCmd)
}
```

## 配置管理

### 配置优先级

1. 环境变量
2. 用户配置 (`settings.json`)
3. 系统配置 (`system.json`)
4. 内置默认值

### system.json

```json
{
  "repo_owner": "shichao402",
  "repo_name": "CursorToolset",
  "registry_url": "https://github.com/.../registry.json",
  "update_branch": "ReleaseLatest"
}
```

## 常用命令速查

```bash
# 开发
make build              # 构建
make lint               # 代码检查
make test               # 单元测试
./scripts/run-tests.sh  # 完整功能测试
make install-dev        # 源码安装

# 清理
make clean              # 清理构建产物
```

## 相关文档

- [测试指南](TESTING.md)
- [构建安装指南](BUILD.md)
- [发布指南](RELEASE.md)
- [架构设计](ARCHITECTURE.md)
