// Command migrate-cnb-gcm 把 host=cnb.cool 的非托管 GCM Note 迁到 bundle/cnb 并启用。
//
// 用法（会打开浏览器做 Bitwarden web unlock）：
//
//	go run ./tools/migrate-cnb-gcm
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/shichao402/Dec/internal/app"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	reporter := app.ReporterFunc(func(event app.OperationEvent) {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", event.Level, event.Message)
	})

	result, err := app.MigrateCNBGCMToUserBundle(ctx, reporter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "迁移失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK: %s/%s → %s/%s (enabled=%v created_vault=%v)\n",
		result.SourceFolder, result.NotePath, result.DestFolder, result.NotePath,
		result.Enabled, result.CreatedVault)
}
