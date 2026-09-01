package cmd

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/config"
	"github.com/spf13/cobra"
)

// serviceSetupCmd 是 `dec __service-setup`，一个 hidden 子命令。
//
// 它由远端置备流程经 SSH 在**目标机本地**调用（ADR 0019 阶段 3）：
//
//	ssh <target> '~/.dec/bin/dec __service-setup'
//
// 只做一件事——把 management_listen 幂等写成置备约定的固定 loopback 端口。
// 之所以不在发起端用 Go 直接改写远端 YAML：那会绕过 config 包的 kind/version
// 合并逻辑（ADR 0017），远端 schema 升级后必然漂移。让远端的 dec 自己写，
// 合并规则就只有一份实现。
//
// 之所以是 hidden 内部命令而非 `dec service setup` 用户面命令族：
// .cursor/rules/tui-first.mdc 要求不新增独立 Cobra 子命令，用户面能力一律走 TUI。
// 这一步没有用户面语义，人不会手敲它，与 __freshness-check 同类。
var serviceSetupCmd = &cobra.Command{
	Use:           "__service-setup",
	Short:         "（内部）在本机幂等写入 dec-server 固定管理监听端口",
	Hidden:        true,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		addr, _ := cmd.Flags().GetString("listen")
		if strings.TrimSpace(addr) == "" {
			addr = app.RemoteProvisionListen
		}
		result, err := config.EnsureManagementListen(addr)
		if err != nil {
			return err
		}
		// 输出被发起端解析，格式必须稳定：一行一个 key=value。
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "listen=%s\n", result.Addr)
		fmt.Fprintf(out, "changed=%t\n", result.Changed)
		fmt.Fprintf(out, "config=%s\n", result.Path)
		if result.Previous != "" && result.Previous != result.Addr {
			fmt.Fprintf(out, "previous=%s\n", result.Previous)
		}
		fmt.Fprintln(out, "service-setup=ok")
		return nil
	},
}

func init() {
	serviceSetupCmd.Flags().String("listen", "", "管理监听地址，默认为置备约定端口")
	RootCmd.AddCommand(serviceSetupCmd)
}
