package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/prune"
	"github.com/shichao402/Dec/internal/secrets"
)

// 需求 3 集成：一条被移入隔离区的条目可恢复回原位；冲突时不覆盖。
func TestRestoreQuarantineIntegration(t *testing.T) {
	home := isolateHome(t)
	_ = home
	root := t.TempDir()
	store := prune.New(filepath.Join(root, "pruned"), prune.Options{Retention: prune.DefaultRetention})
	executor := NewPruneExecutor(store)

	abs := filepath.Join(root, secrets.SecretsRootDir, "demo", ".env", "app.env")
	writeFileT(t, abs, "TOKEN=1\n")
	item := PruneItem{
		Class:       PruneClassRebuildable,
		Kind:        "note",
		AbsPath:     abs,
		DisplayPath: ".secrets/demo/.env/app.env",
		RelPath:     ".env/app.env",
		Bundle:      "demo",
		Plane:       string(secrets.SyncPlaneProject),
		ProjectRoot: root,
		Reason:      prune.ReasonRemoteGone,
	}
	res := executor.Execute(context.Background(), []PruneItem{item}, nil)
	if len(res.RemovedSecretPaths) != 1 {
		t.Fatalf("应移入 1 条, got %#v", res)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("原文件应消失: %v", err)
	}

	// 恢复。
	restored, skipped, err := restoreQuarantineWith(store, nil)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if len(restored) != 1 || len(skipped) != 0 {
		t.Fatalf("应恢复 1 条、无冲突, got restored=%d skipped=%d", len(restored), len(skipped))
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("恢复后文件不可读: %v", err)
	}
	if string(data) != "TOKEN=1\n" {
		t.Fatalf("恢复内容不符: %q", string(data))
	}
}

// 需求 3.3 集成：目标已存在时不覆盖并报告冲突。
func TestRestoreQuarantineIntegrationConflict(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	store := prune.New(filepath.Join(root, "pruned"), prune.Options{})
	executor := NewPruneExecutor(store)

	abs := filepath.Join(root, "conflict.env")
	writeFileT(t, abs, "OLD=1\n")
	item := PruneItem{Class: PruneClassRebuildable, Kind: "note", AbsPath: abs, DisplayPath: "conflict.env"}
	_ = executor.Execute(context.Background(), []PruneItem{item}, nil)

	// 恢复路径被新内容占用。
	writeFileT(t, abs, "NEW=2\n")

	_, skipped, err := restoreQuarantineWith(store, nil)
	if err != nil {
		t.Fatalf("恢复不应整体失败: %v", err)
	}
	if len(skipped) != 1 {
		t.Fatalf("应有 1 条冲突, got %d", len(skipped))
	}
	data, err := os.ReadFile(abs)
	if err != nil || string(data) != "NEW=2\n" {
		t.Fatalf("冲突时不得覆盖现有文件: %v %q", err, string(data))
	}
}

// 需求 3/8 端到端：隔离区现状可查（数量/保留期/到期清单）且条目可列。
func TestSummarizeAndListQuarantine(t *testing.T) {
	isolateHome(t)
	qdir := filepath.Join(t.TempDir(), "pruned")
	t.Setenv("DEC_PRUNE_QUARANTINE_DIR", qdir)

	// 移入一条，确认可被统计。
	root := t.TempDir()
	store := prune.New(qdir, prune.Options{Retention: 30 * 24 * time.Hour})
	executor := NewPruneExecutor(store)
	abs := filepath.Join(root, secrets.SecretsRootDir, "demo", ".env", "app.env")
	writeFileT(t, abs, "TOKEN=1\n")
	item := PruneItem{
		Class:       PruneClassRebuildable,
		Kind:        "note",
		AbsPath:     abs,
		DisplayPath: ".secrets/demo/.env/app.env",
		RelPath:     ".env/app.env",
		Bundle:      "demo",
		Plane:       string(secrets.SyncPlaneProject),
		Reason:      prune.ReasonRemoteGone,
	}
	_ = executor.Execute(context.Background(), []PruneItem{item}, nil)

	summary, err := SummarizeQuarantine()
	if err != nil {
		t.Fatalf("SummarizeQuarantine() = %v", err)
	}
	if summary.Count != 1 {
		t.Fatalf("应有 1 条隔离项, got %d", summary.Count)
	}
	if summary.Retention == "" || summary.MaxBytes == 0 {
		t.Fatalf("应体现保留期与容量, got %#v", summary)
	}

	entries, err := ListQuarantine(false)
	if err != nil {
		t.Fatalf("ListQuarantine() = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("应列出 1 条, got %d", len(entries))
	}
	if entries[0].RelPath != ".env/app.env" || entries[0].DisplayPath == "" || entries[0].ExpiresAt == "" {
		t.Fatalf("条目字段不完整: %#v", entries[0])
	}
}

