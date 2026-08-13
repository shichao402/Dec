package main

import (
	"fmt"
	"os"

	"github.com/shichao402/Dec/internal/app"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	var projectRoot, bundle string
	root := &cobra.Command{
		Use:          "dec-exec --bundle NAME -- <command> [args...]",
		Short:        "注入已落地的 Dec secrets 环境变量后执行命令",
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("缺少要执行的命令")
			}
			if projectRoot == "" {
				var err error
				projectRoot, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			code, err := app.RunExecWithSecrets(app.ExecWithSecretsInput{
				ProjectRoot: projectRoot,
				Bundle:      bundle,
				Command:     args,
			})
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	root.Flags().StringVar(&projectRoot, "project-root", "", "项目根目录（默认当前目录）")
	root.Flags().StringVar(&bundle, "bundle", "", "只注入该 bundle + project 的 env")
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
