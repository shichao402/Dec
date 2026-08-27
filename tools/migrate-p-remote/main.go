// Command migrate-p-remote 一次性把 Git vault 与 Bitwarden 从 bundles/projects
// 改写成 P 四象限。不改本机落地；新版本启动会清理本地旧目录。
//
//	go run ./tools/migrate-p-remote           # 只读预览
//	go run ./tools/migrate-p-remote --apply   # 写入远端
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/shichao402/Dec/internal/app"
)

func main() {
	apply := flag.Bool("apply", false, "执行远端写入与旧节点删除")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	reporter := app.ReporterFunc(func(event app.OperationEvent) {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", event.Level, event.Message)
	})
	ws := app.NewWorkspace(app.WorkspaceUser, "")
	plan, err := app.PreviewPMigration(ctx, ws, reporter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "预览失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("legacy=%v P=%d git=%d bw=%d issues=%d fingerprint=%s\n",
		plan.LegacyDetected, len(plan.Manifests), len(plan.GitMoves), len(plan.BWMoves), len(plan.Issues), plan.Fingerprint)
	for _, issue := range plan.Issues {
		fmt.Printf("  [%s/%s] %s\n", issue.Severity, issue.Code, issue.Message)
	}
	if !*apply {
		return
	}
	if plan.LegacyDetected {
		if plan.HasBlockers() {
			fmt.Fprintln(os.Stderr, "存在阻断问题，拒绝执行")
			os.Exit(1)
		}
		journal, err := app.RunPMigration(ctx, ws, plan.Fingerprint, reporter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "执行失败: %v (phase=%v)\n", err, journalPhase(journal))
			os.Exit(1)
		}
		fmt.Printf("OK phase=%s\n", journal.Phase)
	}
	if err := app.SyncPManifestsFromBitwarden(ctx, reporter); err != nil {
		fmt.Fprintf(os.Stderr, "补齐 P 声明失败: %v\n", err)
		os.Exit(1)
	}
}

func journalPhase(journal *app.PMigrationJournal) app.PMigrationPhase {
	if journal == nil {
		return ""
	}
	return journal.Phase
}
