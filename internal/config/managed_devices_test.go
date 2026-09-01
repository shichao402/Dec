package config

import (
	"testing"

	"github.com/shichao402/Dec/internal/types"
)

func TestManagedDevicesRegisterDeduplicateAndRemove(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	first, err := RegisterManagedDevice(types.ManagedDevice{
		Alias:              "Build Box",
		SSHTarget:          "builder",
		ManagementListen:   "127.0.0.1:47653",
		Tags:               []string{"linux", "CI", "linux"},
		ProvisionedVersion: "v1.2.3",
	})
	if err != nil {
		t.Fatalf("首次登记失败: %v", err)
	}
	if len(first.Tags) != 2 || first.Tags[0] != "CI" || first.Tags[1] != "linux" {
		t.Fatalf("标签未规范化: %#v", first.Tags)
	}

	updated, err := RegisterManagedDevice(types.ManagedDevice{
		Alias:              "build box",
		SSHTarget:          "ops@builder",
		ManagementListen:   "127.0.0.1:47653",
		ProvisionedVersion: "v1.2.4",
	})
	if err != nil {
		t.Fatalf("更新登记失败: %v", err)
	}
	if updated.SSHTarget != "ops@builder" {
		t.Fatalf("SSH 目标未更新: %q", updated.SSHTarget)
	}

	items, err := ListManagedDevices()
	if err != nil {
		t.Fatalf("列出设备失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("大小写不同的同别名应去重，实际 %d 条", len(items))
	}
	if items[0].ProvisionedVersion != "v1.2.4" {
		t.Fatalf("版本未更新: %q", items[0].ProvisionedVersion)
	}

	removed, err := RemoveManagedDevice("BUILD BOX")
	if err != nil || !removed {
		t.Fatalf("移除失败: removed=%v err=%v", removed, err)
	}
	items, err = ListManagedDevices()
	if err != nil || len(items) != 0 {
		t.Fatalf("移除后仍有登记: %#v err=%v", items, err)
	}
}

func TestManagedDevicePreservesOtherGlobalFields(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.RepoURL = "https://example.com/vault.git"
	cfg.ManagedProjects = []types.ManagedProject{{Root: t.TempDir(), Label: "repo"}}
	if err := SaveGlobalConfig(cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := RegisterManagedDevice(types.ManagedDevice{Alias: "box", SSHTarget: "box"}); err != nil {
		t.Fatal(err)
	}
	next, err := LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if next.RepoURL != cfg.RepoURL || len(next.ManagedProjects) != 1 {
		t.Fatalf("登记设备冲掉了其他全局字段: %#v", next)
	}
}

func TestNormalizeSSHTargetRejectsOptionsAndWhitespace(t *testing.T) {
	for _, raw := range []string{"", "-oProxyCommand=bad", "host other", "host\nother", ":36000"} {
		if _, err := NormalizeSSHTarget(raw); err == nil {
			t.Fatalf("%q 应被拒绝", raw)
		}
	}
	for _, raw := range []string{"builder", "ops@builder", "10.0.0.8", "update.devcloud.woa.com:36000", "root@update.devcloud.woa.com:36000"} {
		if _, err := NormalizeSSHTarget(raw); err != nil {
			t.Fatalf("%q 应被接受: %v", raw, err)
		}
	}
	got, err := NormalizeSSHTarget("update.devcloud.woa.com:36000")
	if err != nil || got != "update.devcloud.woa.com:36000" {
		t.Fatalf("应保留端口，实际 %q err=%v", got, err)
	}
}
