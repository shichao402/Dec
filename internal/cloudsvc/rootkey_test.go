package cloudsvc

import (
	"path/filepath"
	"testing"
)

// 根密钥路径可配置（需求 16.5）。
//
// 需求要求根密钥不能落在云厂商磁盘快照或镜像备份覆盖的位置。
// 最省事的满足方式是：数据目录整体进快照，根密钥单独放在快照之外。
// 所以路径必须能从数据目录里拆出来，而且默认值要保持不变。

// TestRootKeyPathDefaultsIntoDataDir 验证默认仍落在数据目录下。
//
// 首次部署、本机调试、测试都依赖这个默认值；改动它会静默改变
// 现有部署里根密钥的位置，等于让服务找不到自己的密钥。
func TestRootKeyPathDefaultsIntoDataDir(t *testing.T) {
	dir := t.TempDir()
	h := newHarnessAt(t, dir, "1.0.0")

	want := filepath.Join(dir, "root.key")
	if got := h.srv.cfg.RootKeyPath; got != want {
		t.Fatalf("默认根密钥路径应为 %q，实际 %q", want, got)
	}
}

// TestRootKeyPathCanLeaveDataDir 验证根密钥可以放到数据目录之外。
//
// 部署脚本会整体快照数据目录，根密钥必须能独立出去，
// 否则快照里就带着一份密钥副本（需求 21.5 也禁止这件事）。
func TestRootKeyPathCanLeaveDataDir(t *testing.T) {
	dataDir := t.TempDir()
	// 用另一棵目录模拟“不参与快照的那个卷”。
	outside := filepath.Join(t.TempDir(), "secrets", "root.key")

	srv, err := New(Config{
		Domain:          "dec.test",
		Listen:          "127.0.0.1:0",
		DataDir:         dataDir,
		ServerVersion:   "1.0.0",
		RequiredVersion: "1.0.0",
		AcceptAny:       true,
		RootKeyPath:     outside,
	})
	if err != nil {
		t.Fatalf("构造服务端失败: %v", err)
	}
	t.Cleanup(srv.closeAll)

	if got := srv.cfg.RootKeyPath; got != outside {
		t.Fatalf("根密钥路径应为 %q，实际 %q", outside, got)
	}
	if got := srv.boot.RootKeyPath(); got != outside {
		t.Fatalf("Bootstrapper 应使用配置的路径 %q，实际 %q", outside, got)
	}
}

// TestRootKeyPathNotInDataDirSnapshot 说明数据目录里不该出现根密钥。
//
// 这条约束由部署脚本与这里的路径配置共同保证：
// 路径移出去之后，快照数据目录自然带不上它。
func TestRootKeyPathNotInDataDirSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "root.key")

	srv, err := New(Config{
		Domain:      "dec.test",
		Listen:      "127.0.0.1:0",
		DataDir:     dataDir,
		AcceptAny:   true,
		RootKeyPath: outside,
	})
	if err != nil {
		t.Fatalf("构造服务端失败: %v", err)
	}
	t.Cleanup(srv.closeAll)

	if fileExists(rootKeyPath(dataDir)) {
		t.Fatal("数据目录里不应出现根密钥文件")
	}
}
