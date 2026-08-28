// Command migrate-bw-flat 一次性把 Bitwarden 从「folder 名含整串 <p>/private/<plane>」
// 改写成「folder 只有 P 名，平面进条目名」。搬完删除旧 folder。
//
//	go run ./tools/migrate-bw-flat                      # 只读预览
//	go run ./tools/migrate-bw-flat --apply              # 执行搬移
//	go run ./tools/migrate-bw-flat --apply --expect <fp> # 仅当远端仍是已复核的那份计划时执行
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/secrets"
)

func main() {
	apply := flag.Bool("apply", false, "执行搬移与旧 folder 删除")
	expect := flag.String("expect", "", "只在远端仍与该指纹一致时执行；填上一轮 dry-run 打印的 fingerprint")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	if err := secrets.EnsureSession(ctx, &secrets.EnsureSessionOpts{
		RequestSource: "tools.migrate-bw-flat",
		Facade:        "tools",
		Operation:     "bw.flatten",
		OnStatus:      func(msg string) { fmt.Fprintln(os.Stderr, "[auth] "+msg) },
	}); err != nil {
		fmt.Fprintf(os.Stderr, "建立 Bitwarden session 失败: %v\n", err)
		os.Exit(1)
	}

	client, ok := secrets.DefaultClient().(*secrets.APIClient)
	if !ok {
		fmt.Fprintln(os.Stderr, "Bitwarden 客户端不可用（session 或配置缺失）")
		os.Exit(1)
	}

	plan, err := client.PlanFlatMigration(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "预览失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("moves=%d legacy_folders=%d blockers=%d untouched=%d fingerprint=%s\n",
		len(plan.Moves), len(plan.LegacyFolders), len(plan.Blockers), len(plan.Untouched), plan.Fingerprint())
	for _, move := range plan.Moves {
		fmt.Printf("  %s\n", move)
	}
	for _, folder := range plan.LegacyFolders {
		fmt.Printf("  删除旧 folder: %s\n", folder)
	}
	for _, name := range plan.Untouched {
		fmt.Printf("  不动: %s\n", name)
	}
	for _, blocker := range plan.Blockers {
		fmt.Printf("  [阻断] %s\n", blocker)
	}

	if !*apply {
		return
	}
	if len(plan.Blockers) > 0 {
		fmt.Fprintln(os.Stderr, "存在阻断冲突，拒绝执行")
		os.Exit(1)
	}
	if want := strings.TrimSpace(*expect); want != "" && want != plan.Fingerprint() {
		fmt.Fprintf(os.Stderr, "远端与已复核的计划不一致：期望 %s，当前 %s；请重新 dry-run 复核\n",
			want, plan.Fingerprint())
		os.Exit(1)
	}
	if err := client.ApplyFlatMigration(ctx, plan.Fingerprint(), func(msg string) {
		fmt.Fprintln(os.Stderr, "[apply] "+msg)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "执行失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}
