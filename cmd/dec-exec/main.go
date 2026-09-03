package main

import (
	"fmt"
	"os"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(Version)
		return
	}
	var projectRoot, pName, legacyBundle, plane string
	root := &cobra.Command{
		Use:          "dec-exec --p NAME --plane project|user -- <command> [args...]",
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
			if pName == "" {
				pName = legacyBundle
			}
			code, err := app.RunExecWithSecrets(app.ExecWithSecretsInput{
				ProjectRoot: projectRoot,
				Bundle:      pName,
				Plane:       parsePlane(plane),
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
	root.Flags().StringVar(&pName, "p", "", "只注入该项目在指定 plane 的 env")
	root.Flags().StringVar(&legacyBundle, "bundle", "", "兼容别名：等同 --p")
	root.Flags().StringVar(&plane, "plane", "project", "secrets 平面：project 或 user")
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parsePlane(value string) secrets.SyncPlane {
	if value == "user" || value == "machine" {
		return secrets.SyncPlaneMachine
	}
	return secrets.SyncPlaneProject
}
