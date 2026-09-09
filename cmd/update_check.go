package cmd

import (
	"fmt"

	"github.com/shichao402/Dec/internal/update"
	"github.com/spf13/cobra"
)

// updateCheckCmd wires Check/DoUpdate to the CLI so the updater entry exists
// before the relkit-updater sidecar can be spawned (ADR 0010).
var updateCheckCmd = &cobra.Command{
	Use:           "__update-check",
	Short:         "（内部）检查并可选安装 Dec runtime 更新",
	Hidden:        true,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		do, _ := cmd.Flags().GetBool("apply")
		current := GetVersion()
		result, err := update.Check(current)
		if err != nil {
			return err
		}
		if result == nil {
			return fmt.Errorf("empty check result")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "current=%s latest=%s need=%v\n",
			result.CurrentVersion, result.LatestVersion, result.NeedUpdate)
		if !do || !result.NeedUpdate {
			return nil
		}
		return update.DoUpdate(current, result.LatestVersion)
	},
}

func init() {
	updateCheckCmd.Flags().Bool("apply", false, "download and replace after a successful check")
	RootCmd.AddCommand(updateCheckCmd)
}
