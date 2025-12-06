package installer

import (
	"os"
	"testing"

	"github.com/firoyang/CursorToolset/pkg/types"
)

func TestNewInstaller(t *testing.T) {
	inst := NewInstaller()

	if inst == nil {
		t.Fatal("NewInstaller() returned nil")
	}

	if inst.downloader == nil {
		t.Error("Installer.downloader should not be nil")
	}

	if !inst.useCache {
		t.Error("Installer.useCache should be true by default")
	}
}

func TestInstaller_SetUseCache(t *testing.T) {
	inst := NewInstaller()

	inst.SetUseCache(false)
	if inst.useCache {
		t.Error("SetUseCache(false) should set useCache to false")
	}

	inst.SetUseCache(true)
	if !inst.useCache {
		t.Error("SetUseCache(true) should set useCache to true")
	}
}

func TestInstaller_IsInstalled(t *testing.T) {
	inst := NewInstaller()

	// 测试不存在的包
	if inst.IsInstalled("non-existent-package-12345") {
		t.Error("IsInstalled should return false for non-existent package")
	}
}

func TestInstaller_Uninstall_NotInstalled(t *testing.T) {
	inst := NewInstaller()

	// 卸载不存在的包应该不报错
	err := inst.Uninstall("non-existent-package-12345")
	if err != nil {
		t.Errorf("Uninstall non-existent package should not return error, got: %v", err)
	}
}

func TestInstaller_Install_InvalidManifest(t *testing.T) {
	inst := NewInstaller()

	// 测试无效的 manifest（没有 tarball）
	manifest := &types.Manifest{
		Name:    "test-package",
		Version: "1.0.0",
		Dist: types.Distribution{
			Tarball: "", // 空的 tarball URL
		},
	}

	err := inst.Install(manifest)
	if err == nil {
		t.Error("Install with empty tarball URL should return error")
	}
}

func TestInstaller_ClearCache(t *testing.T) {
	inst := NewInstaller()

	// 清理缓存不应该报错（即使缓存目录不存在）
	err := inst.ClearCache()
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("ClearCache should not return error, got: %v", err)
	}
}
