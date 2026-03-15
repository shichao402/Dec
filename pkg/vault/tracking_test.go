package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrackPathsAndCheckChanges(t *testing.T) {
	projectRoot := t.TempDir()

	pathA := filepath.Join(".cursor", "skills", "dec-create-api-test", "SKILL.md")
	pathB := filepath.Join(".windsurf", "skills", "dec-create-api-test", "SKILL.md")

	absA := filepath.Join(projectRoot, pathA)
	absB := filepath.Join(projectRoot, pathB)

	if err := os.MkdirAll(filepath.Dir(absA), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(absB), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	content := []byte("version-1")
	if err := os.WriteFile(absA, content, 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	if err := os.WriteFile(absB, content, 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	hash, err := HashPath(absA)
	if err != nil {
		t.Fatalf("计算哈希失败: %v", err)
	}

	td := &TrackingData{}
	td.TrackPaths("create-api-test", "skill", []string{pathA, pathB}, hash)

	if len(td.Tracked) != 1 {
		t.Fatalf("追踪项数量错误: 期望 1, 得到 %d", len(td.Tracked))
	}
	if len(td.Tracked[0].LocalPaths) != 2 {
		t.Fatalf("本地路径数量错误: 期望 2, 得到 %d", len(td.Tracked[0].LocalPaths))
	}

	if err := os.WriteFile(absB, []byte("version-2"), 0644); err != nil {
		t.Fatalf("更新文件失败: %v", err)
	}

	changes := td.CheckChanges(projectRoot, nil)
	if len(changes) != 1 {
		t.Fatalf("变更数量错误: 期望 1, 得到 %d", len(changes))
	}
	if changes[0].Status != "modified" {
		t.Fatalf("变更状态错误: 期望 modified, 得到 %s", changes[0].Status)
	}
	if changes[0].Item.LocalPath != pathB {
		t.Fatalf("变更路径错误: 期望 %s, 得到 %s", pathB, changes[0].Item.LocalPath)
	}
}
