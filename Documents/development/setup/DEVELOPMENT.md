# Dec 开发指南

本文档面向 Dec 项目的开发者。

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
Dec/
├── cmd/                    # 命令行命令
│   ├── root.go            # 根命令
│   ├── init.go            # init 命令
│   ├── sync.go            # sync 命令
│   ├── list.go            # list 命令
│   ├── update.go          # update 命令
│   ├── source.go          # source 命令
│   ├── use.go             # use 命令
│   ├── serve.go           # MCP Server
│   └── publish_notify.go  # publish-notify 命令
├── pkg/                    # 核心包
│   ├── config/            # 全局配置、项目配置、包获取
│   ├── packages/          # 包扫描、占位符解析
│   ├── service/           # 同步服务
│   ├── ide/               # IDE 抽象层
│   ├── paths/             # 路径管理
│   ├── types/             # 类型定义
│   └── version/           # 版本管理
├── dec-packages/          # 内置包（规则和 MCP）
│   ├── rules/
│   └── mcp/
├── config/
│   ├── registry.json      # 包注册表
│   └── system.json        # 系统配置模板
├── scripts/
│   ├── install.sh         # 正式安装脚本
│   ├── install-dev.sh     # 开发安装脚本
│   └── run-tests.sh       # 测试脚本
├── Documents/             # 项目文档
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
dec --version
dec list
dec sync  # 在项目目录中
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
return fmt.Errorf("未找到包: %s\n\n提示: 检查包名是否正确，或运行 'dec list' 查看可用包", name)
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

### 全局配置

```yaml
# ~/.dec/config.yaml
packages_source: "https://github.com/shichao402/MyDecPackage"
packages_version: "latest"
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

- [测试指南](../testing/TESTING.md)
- [构建安装指南](../deployment/BUILD.md)
- [发布指南](../deployment/RELEASE.md)
- [架构设计](../../design/architecture/ARCHITECTURE.md)
