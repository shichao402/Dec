package cloudsvc

import (
	"os"
	"path/filepath"
	"testing"
)

// 要求版本由发布清单驱动（需求 19.1、19.2、21.2）。
//
// 这一条看起来只是“读一个文件”，实际决定三件事：
//
//   - 发布即生效：部署脚本把新值落盘，服务启动就读到，不需要改 systemd；
//   - 回滚一致：要求版本跟着快照一起回退，不会出现“回滚了服务端却还要求新代理”；
//   - 只修服务端时能沿用旧要求版本，避免全设备白下载一趟。

// TestRequiredVersionFromFile 验证文件优先于命令行参数。
//
// 部署脚本只能写文件（服务当时是停的），因此文件必须是权威来源。
func TestRequiredVersionFromFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(requiredVersionFile(dir), []byte("v1.14.0\n"), 0o644); err != nil {
		t.Fatalf("写要求版本文件失败: %v", err)
	}

	srv, err := New(Config{
		Domain:    "dec.test",
		Listen:    "127.0.0.1:0",
		DataDir:   dir,
		AcceptAny: true,
		// 参数值故意给一个不同的版本：它应该被文件覆盖。
		RequiredVersion: "v0.0.1",
	})
	if err != nil {
		t.Fatalf("构造服务端失败: %v", err)
	}
	t.Cleanup(srv.closeAll)

	if got, want := srv.cfg.RequiredVersion, "v1.14.0"; got != want {
		t.Fatalf("要求版本应为文件里的 %q，实际 %q", want, got)
	}
}

// TestRequiredVersionFallsBackToServerVersion 验证没有文件时与服务端版本对齐。
//
// 首次部署还没有发布清单，此时“要求版本 = 服务端版本”是最自然的默认。
func TestRequiredVersionFallsBackToServerVersion(t *testing.T) {
	dir := t.TempDir()
	srv, err := New(Config{
		Domain:        "dec.test",
		Listen:        "127.0.0.1:0",
		DataDir:       dir,
		ServerVersion: "v2.0.0",
		AcceptAny:     true,
	})
	if err != nil {
		t.Fatalf("构造服务端失败: %v", err)
	}
	t.Cleanup(srv.closeAll)

	if got, want := srv.cfg.RequiredVersion, "v2.0.0"; got != want {
		t.Fatalf("要求版本应回退到服务端版本 %q，实际 %q", want, got)
	}
}

// TestRequiredVersionEmptyFileFallsThrough 验证空文件不会把版本清成空串。
//
// 空的要求版本会让 aligned() 对所有代理放行，等于关掉版本管控。
func TestRequiredVersionEmptyFileFallsThrough(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(requiredVersionFile(dir), []byte("  \n"), 0o644); err != nil {
		t.Fatalf("写空文件失败: %v", err)
	}

	srv, err := New(Config{
		Domain:        "dec.test",
		Listen:        "127.0.0.1:0",
		DataDir:       dir,
		ServerVersion: "v3.0.0",
		AcceptAny:     true,
	})
	if err != nil {
		t.Fatalf("构造服务端失败: %v", err)
	}
	t.Cleanup(srv.closeAll)

	if got := srv.cfg.RequiredVersion; got != "v3.0.0" {
		t.Fatalf("空文件的处理应是回退，实际要求版本 %q", got)
	}
	if !srv.aligned("v3.0.0") {
		t.Fatal("与服务端版本一致时应判定对齐")
	}
	if srv.aligned("v2.9.0") {
		t.Fatal("不一致的版本应判定不对齐")
	}
}

// TestRequiredVersionFilePath 固定文件位置，避免部署脚本与服务端各写各的。
func TestRequiredVersionFileLocation(t *testing.T) {
	dir := t.TempDir()
	if got, want := requiredVersionFile(dir), filepath.Join(dir, "required-version"); got != want {
		t.Fatalf("要求版本文件应在 %q，实际 %q", want, got)
	}
}
