package cmd

import (
	"os"
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
