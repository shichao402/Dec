package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/prune"
	"github.com/shichao402/Dec/internal/secrets"
)

func TestPruneExecutorMovesNoteToQuarantine(t *testing.T) {
	home := isolateHome(t)
	_ = home
	root := t.TempDir()
	store := prune.New(filepath.Join(root, "pruned"), prune.Options{})
	executor := NewPruneExecutor(store)

	abs := filepath.Join(root, ".secrets", "demo", ".env", "app.env")
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
	if len(res.Warnings) != 0 {
		t.Fatalf("应无告警, got %#v", res.Warnings)
	}
	if len(res.RemovedSecretPaths) != 1 {
		t.Fatalf("应报告移除 1 项, got %#v", res)
	}

	// 原文件必须消失。
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("原文件应被移走, statErr=%v", err)
	}
	// 隔离区必须有副本且可列出。
	entries, err := store.List(false)
	if err != nil {
		t.Fatalf("列举隔离区失败: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("隔离区应有 1 条记录, got %d", len(entries))
	}
	if entries[0].DisplayPath != item.DisplayPath {
		t.Fatalf("隔离记录展示路径不符: %q", entries[0].DisplayPath)
	}
	stored := filepath.Join(store.ItemDir(entries[0].ID), entries[0].StoredRel)
	data, err := os.ReadFile(stored)
	if err != nil {
		t.Fatalf("隔离副本不可读: %v", err)
	}
	if string(data) != "TOKEN=1\n" {
		t.Fatalf("隔离额定内容不符: %q", string(data))
	}
}

func TestPruneExecutorReportsErrorWhenQuarantineFails(t *testing.T) {
	root := t.TempDir()
	// 隔离根被普通文件占住，Move 会失败。
	blocker := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := prune.New(filepath.Join(blocker, "nested"), prune.Options{})
	executor := NewPruneExecutor(store)

	abs := filepath.Join(root, "a.env")
	if err := os.WriteFile(abs, []byte("K=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	item := PruneItem{Class: PruneClassRebuildable, Kind: "note", AbsPath: abs, DisplayPath: "a.env"}
	res := executor.Execute(context.Background(), []PruneItem{item}, nil)
	if len(res.RemovedSecretPaths) != 0 {
		t.Fatalf("隔离区失败时不得报告移除, got %#v", res)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("隔离区失败应产生告警")
	}
	// 原文件必须保持。
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("隔离区失败时原件必须保留: %v", err)
	}
}

func TestPruneExecutorSkipWhenAbsent(t *testing.T) {
	root := t.TempDir()
	store := prune.New(filepath.Join(root, "pruned"), prune.Options{})
	executor := NewPruneExecutor(store)

	item := PruneItem{Class: PruneClassRebuildable, Kind: "note", AbsPath: filepath.Join(root, "missing.env"), DisplayPath: "missing.env"}
	res := executor.Execute(context.Background(), []PruneItem{item}, nil)
	if len(res.RemovedSecretPaths) != 0 {
		t.Fatalf("不存在文件不得报告移除, got %#v", res)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("跳过不存在文件应产生告警")
	}
}

func TestPruneExecutorHonorsContextCancellation(t *testing.T) {
	root := t.TempDir()
	store := prune.New(filepath.Join(root, "pruned"), prune.Options{})
	executor := NewPruneExecutor(store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	abs := filepath.Join(root, ".env", "app.env")
	writeFileT(t, abs, "TOKEN=1\n")
	item := PruneItem{Class: PruneClassRebuildable, Kind: "note", AbsPath: abs, DisplayPath: "app.env"}
	res := executor.Execute(ctx, []PruneItem{item}, nil)
	if len(res.RemovedSecretPaths) != 0 {
		t.Fatalf("取消后不得移除, got %#v", res)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("取消后原件必须保留: %v", err)
	}
}

// 需求 5.4：单条剪枝失败不影响其余条目；已完成项保留，失败项有明确记录。
func TestPruneExecutorContinuesAfterSingleFailure(t *testing.T) {
	root := t.TempDir()
	store := prune.New(filepath.Join(root, "pruned"), prune.Options{})
	executor := NewPruneExecutor(store)

	// 前、后两条正常；中间一条源不存在 → 触发失败。
	firstOk := filepath.Join(root, ".env", "first.env")
	missingAbs := filepath.Join(root, ".env", "missing.env")
	lastOk := filepath.Join(root, ".env", "last.env")
	writeFileT(t, firstOk, "FIRST=1\n")
	writeFileT(t, lastOk, "LAST=1\n")

	items := []PruneItem{
		{Class: PruneClassRebuildable, Kind: "note", AbsPath: firstOk, DisplayPath: "first.env", Reason: prune.ReasonRemoteGone},
		{Class: PruneClassRebuildable, Kind: "note", AbsPath: missingAbs, DisplayPath: "missing.env", Reason: prune.ReasonRemoteGone},
		{Class: PruneClassRebuildable, Kind: "note", AbsPath: lastOk, DisplayPath: "last.env", Reason: prune.ReasonRemoteGone},
	}
	res := executor.Execute(context.Background(), items, nil)
	if len(res.RemovedSecretPaths) != 2 {
		t.Fatalf("应移除 2 条可用项, got %v", res.RemovedSecretPaths)
	}
	if len(res.Warnings) < 1 {
		t.Fatalf("缺席条目应产生告警, got %#v", res.Warnings)
	}
	// 失败项不能产生假恢复，且首末良好项都成功移走了源文件。
	if _, err := os.Stat(firstOk); !os.IsNotExist(err) {
		t.Fatalf("首条应成功移走: %v", err)
	}
	if _, err := os.Stat(lastOk); !os.IsNotExist(err) {
		t.Fatalf("末条应成功移走: %v", err)
	}
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}
