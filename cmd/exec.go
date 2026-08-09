package cmd

import (
	"fmt"
	"os"

	"github.com/shichao402/Dec/pkg/app"
	"github.com/spf13/cobra"
)

// execCmd 是运行时注入 shim：读取 .secrets/**/env/*.env 后 exec 目标命令。
// Hidden：不属于用户面 TUI 流程，仅供 MCP / Agent / 脚本调用。
var execCmd = &cobra.Command{
	Use:    "exec",
	Short:  "注入项目 secrets 环境变量后执行命令（运行时 shim）",
	Hidden: true,
	Long: `从 <project>/.secrets/bundles/<bundle>/env/*.env 与 .secrets/project/env/*.env
加载环境变量，注入子进程后执行命令。不打印密钥值，不依赖 Bitwarden session。

示例:
  dec exec --project-root /path/to/proj --bundle vikunja -- npx -y @shichao402/vikunja-mcp`,
	DisableFlagParsing: false,
	Args:               cobra.ArbitraryArgs,
	RunE:               runExec,
}

var (
	execProjectRoot string
	execBundle      string
)

func runExec(cmd *cobra.Command, args []string) error {
	cmd.SetOut(os.Stderr)
	cmd.SetErr(os.Stderr)

	root := execProjectRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("获取当前目录失败: %w", err)
		}
	}

	// cobra 保留 `--` 之后的参数在 args；若用户写了 `--`，Args 就是命令。
	command := args
	if len(command) == 0 {
		return fmt.Errorf("用法: dec exec [--project-root DIR] [--bundle NAME] -- <command> [args...]")
	}

	code, err := app.RunExecWithSecrets(app.ExecWithSecretsInput{
		ProjectRoot: root,
		Bundle:      execBundle,
		Command:     command,
	})
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

func init() {
	execCmd.Flags().StringVar(&execProjectRoot, "project-root", "", "项目根目录（默认当前目录）")
	execCmd.Flags().StringVar(&execBundle, "bundle", "", "只注入该 bundle + project 的 env")
	RootCmd.AddCommand(execCmd)
}
