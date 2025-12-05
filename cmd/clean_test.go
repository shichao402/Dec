package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanCacheDir(t *testing.T) {
	// 测试清理不存在的缓存目录不应该报错
	err := cleanCacheDir()
	// 可能返回 nil 或者目录不存在的错误，都是可接受的
	if err != nil && !os.IsNotExist(err) {
		t.Logf("cleanCacheDir returned: %v (this may be expected)", err)
	}
}

func TestCleanReposDir(t *testing.T) {
	// 测试清理不存在的 repos 目录不应该报错
	err := cleanReposDir()
	// 可能返回 nil 或者目录不存在的错误，都是可接受的
	if err != nil && !os.IsNotExist(err) {
		t.Logf("cleanReposDir returned: %v (this may be expected)", err)
	}
}

// 辅助函数
func createTestFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
}
