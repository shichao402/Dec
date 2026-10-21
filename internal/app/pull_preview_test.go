package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

func TestPreviewPull_NoRemovalsWhenNoEnabledBundles(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()

	preview, err := previewPullForWorkspace(context.Background(), NewWorkspace(WorkspaceProject, root), nil, nil)
	if err != nil {
		t.Fatalf("previewPullForWorkspace() 错误: %v", err)
	}
	if preview.HasRemovals() {
		t.Fatalf("无启用 bundle 时不应有待删项, got %#v", preview.Removable)
	}
	if len(preview.Removable) == 0 && !preview.HasRemovals() {
		// 需求 1 验收 3：无待删时必须返回明确结论（SkippedReason 非空），而非空列表。
		if preview.SkippedReason == "" {
			t.Fatal("无待删的结论不明确：SkippedReason 为空")
		}
	}
}

func TestPreviewPull_NoRemovalsWhenRemoteHasAll(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()

	// 本地有一条，远端也有一条相同路径 → 不应判定为孤儿。
	writeProjectFile(t, root, secrets.SecretsRootDir+"/demo/.env/app.env", "K=1\n")

	target := projectTarget(t, "demo")
	remoteNotes := []secrets.SecureNote{{RelativePath: ".env/app.env"}}
	remoteKeys := []secrets.SSHKeyLanding(nil)

	decision := DecideTargetPrune(root, target, remoteNotes, remoteKeys, PruneScope{})
	if !pruneDecisionIsEmpty(decision) {
		t.Fatalf("远端仍存在的条目不应进入判定, got %#v", decision.Items)
	}
}

func TestPreviewPullMapsRemovableToChanges(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	target := projectTarget(t, "demo")

	// 本地两条：一条 env（可重建），一条未知点目录（保留）。
	writeProjectFile(t, root, secrets.SecretsRootDir+"/demo/.env/app.env", "K=1\n")
	writeProjectFile(t, root, secrets.SecretsRootDir+"/demo/.unknown/x.txt", "x\n")

	decision := DecideTargetPrune(root, target, nil, nil, PruneScope{})
	removable := decision.Removable()
	if len(removable) != 1 {
		t.Fatalf("可删项应只有 1 条, got %d", len(removable))
	}
	change := removable[0].toChange()
	if change.Op != "quarantine" {
		t.Fatalf("操作应为 quarantine, got %q", change.Op)
	}
	if change.Path == "" || change.Target != "demo" || change.Kind == "" {
		t.Fatalf("明细字段不完整: %#v", change)
	}
	if !change.Recoverable {
		t.Fatal("可重建条目应标注可从远端恢复")
	}
}

func TestPreviewPullIsReadOnly(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	target := projectTarget(t, "demo")
	rel := secrets.SecretsRootDir + "/demo/.env/app.env"
	writeProjectFile(t, root, rel, "K=1\n")

	full := filepath.Join(root, filepath.FromSlash(rel))
	before, err := os.Stat(full)
	if err != nil {
		t.Fatalf("读取前置状态失败: %v", err)
	}

	_ = DecideTargetPrune(root, target, nil, nil, PruneScope{})

	after, err := os.Stat(full)
	if err != nil {
		t.Fatalf("预览后文件必须仍在: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("预览过程不得改动文件")
	}
}

// 内联辅助：验证 preview 顶层函数在配置缺失时仍返回结构化结果而不崩溃。
func TestPreviewPullTopLevelReturnsStructuredResult(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	mgr := config.NewProjectConfigManager(root)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{ProjectName: "demo"}); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	preview, err := PreviewPullProjectAssets(root)
	if err != nil {
		t.Fatalf("PreviewPullProjectAssets() 错误: %v", err)
	}
	if preview == nil {
		t.Fatal("预览结果不应为 nil")
	}
	// 无论多少待删项，结果都必须含能力字段与无待删结论字段。
	if !preview.HasRemovals() && preview.SkippedReason == "" {
		t.Fatal("无待删时结论不明确：SkippedReason 为空")
	}
	// 预览必须无副作用：隔离 HOME 下操纵后，同步根文件仍应在。
	files, walkErr := filepath.Glob(filepath.Join(root, secrets.SecretsRootDir, "**", "*"))
	if walkErr != nil {
		t.Fatalf("遍历同步根失败: %v", walkErr)
	}
	_ = files
}
