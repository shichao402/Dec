package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout 捕获 fn 执行期间写入 os.Stdout 的内容。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 stdout pipe 失败: %v", err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	os.Stdout = oldStdout
	<-done
	_ = r.Close()
	return buf.String()
}

// ========================================
// pushBundles
// ========================================

// TestPushBundles_CopiesValidYAML 验证：合法 bundle 文件会被校验通过并复制到 repo 正确位置。
func TestPushBundles_CopiesValidYAML(t *testing.T) {
	projectRoot := t.TempDir()
	repoDir := t.TempDir()

	bundlesDir := filepath.Join(projectRoot, ".dec", "cache", "cli", "bundles")
	if err := os.MkdirAll(bundlesDir, 0755); err != nil {
		t.Fatalf("创建 bundles 目录失败: %v", err)
	}
	cachePath := filepath.Join(bundlesDir, "vikunja.yaml")
	content := `name: vikunja
description: Vikunja 工作流
members:
  - mcp/vikunja-mcp
  - rule/vikunja-integration
`
	if err := os.WriteFile(cachePath, []byte(content), 0644); err != nil {
		t.Fatalf("写入 bundle 失败: %v", err)
	}

	pushed, err := pushBundles(projectRoot, repoDir)
	if err != nil {
		t.Fatalf("pushBundles 出错: %v", err)
	}
	if pushed != 1 {
		t.Fatalf("期望推送 1 个 bundle, 得到 %d", pushed)
	}

	want := filepath.Join(repoDir, "cli", "bundles", "vikunja.yaml")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("repo 中未找到 bundle: %v", err)
	}
	if string(data) != content {
		t.Fatalf("内容不一致\n期望: %s\n实际: %s", content, string(data))
	}
}

// TestPushBundles_SkipsInvalidYAML 验证：格式错误的 bundle 会被跳过并产生警告，
// 不会中断其它合法 bundle 的推送。
func TestPushBundles_SkipsInvalidYAML(t *testing.T) {
	projectRoot := t.TempDir()
	repoDir := t.TempDir()

	bundlesDir := filepath.Join(projectRoot, ".dec", "cache", "cli", "bundles")
	if err := os.MkdirAll(bundlesDir, 0755); err != nil {
		t.Fatalf("mkdir 失败: %v", err)
	}
	// 非法：缺少 name
	bad := filepath.Join(bundlesDir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("members:\n  - skill/foo\n"), 0644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	// 合法
	good := filepath.Join(bundlesDir, "good.yaml")
	if err := os.WriteFile(good, []byte("name: good\nmembers:\n  - skill/foo\n"), 0644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	var pushed int
	out := captureStdout(t, func() {
		var perr error
		pushed, perr = pushBundles(projectRoot, repoDir)
		if perr != nil {
			t.Fatalf("pushBundles 出错: %v", perr)
		}
	})

	if pushed != 1 {
		t.Fatalf("期望推送 1 个合法 bundle, 得到 %d", pushed)
	}
	if !strings.Contains(out, "校验 bundle") {
		t.Fatalf("非法 bundle 应产生警告, 输出:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "cli", "bundles", "good.yaml")); err != nil {
		t.Fatalf("合法 bundle 应被推送: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "cli", "bundles", "bad.yaml")); !os.IsNotExist(err) {
		t.Fatal("非法 bundle 不应被推送到 repo")
	}
}

// TestPushBundles_MultipleVaults 验证：多个 vault 下的 bundle 均可被扫描并推送。
func TestPushBundles_MultipleVaults(t *testing.T) {
	projectRoot := t.TempDir()
	repoDir := t.TempDir()

	for _, vault := range []string{"cli", "vikunja"} {
		dir := filepath.Join(projectRoot, ".dec", "cache", vault, "bundles")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir 失败: %v", err)
		}
		path := filepath.Join(dir, "pkg.yaml")
		body := "name: pkg\nmembers:\n  - skill/a\n"
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	pushed, err := pushBundles(projectRoot, repoDir)
	if err != nil {
		t.Fatalf("pushBundles 出错: %v", err)
	}
	if pushed != 2 {
		t.Fatalf("期望推送 2 个 bundle, 得到 %d", pushed)
	}
	for _, vault := range []string{"cli", "vikunja"} {
		if _, err := os.Stat(filepath.Join(repoDir, vault, "bundles", "pkg.yaml")); err != nil {
			t.Fatalf("vault %s 的 bundle 未推送: %v", vault, err)
		}
	}
}

// TestPushBundles_NoCacheDir 验证：.dec/cache 不存在时返回 (0, nil)。
func TestPushBundles_NoCacheDir(t *testing.T) {
	projectRoot := t.TempDir()
	repoDir := t.TempDir()

	pushed, err := pushBundles(projectRoot, repoDir)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pushed != 0 {
		t.Fatalf("期望 0, 得到 %d", pushed)
	}
}

// TestPushBundles_NoBundlesDir 验证：vault 存在但无 bundles/ 子目录时忽略。
func TestPushBundles_NoBundlesDir(t *testing.T) {
	projectRoot := t.TempDir()
	repoDir := t.TempDir()

	// 只有 skills 目录
	if err := os.MkdirAll(filepath.Join(projectRoot, ".dec", "cache", "cli", "skills"), 0755); err != nil {
		t.Fatalf("mkdir 失败: %v", err)
	}

	pushed, err := pushBundles(projectRoot, repoDir)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pushed != 0 {
		t.Fatalf("期望 0, 得到 %d", pushed)
	}
}

// TestPushBundles_IgnoresNonYAML 验证：非 yaml/yml 扩展名的文件会被静默忽略。
func TestPushBundles_IgnoresNonYAML(t *testing.T) {
	projectRoot := t.TempDir()
	repoDir := t.TempDir()

	dir := filepath.Join(projectRoot, ".dec", "cache", "cli", "bundles")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# readme"), 0644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	pushed, err := pushBundles(projectRoot, repoDir)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pushed != 0 {
		t.Fatalf("期望 0, 得到 %d", pushed)
	}
}
