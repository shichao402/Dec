package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadVersionInfo(t *testing.T) {
	// 创建临时目录和 version.json
	tempDir := t.TempDir()
	versionPath := filepath.Join(tempDir, "version.json")
	
	versionJSON := `{
  "version": "v1.0.0",
  "build_time": "2024-12-04_11:00:00",
  "commit": "abc123",
  "branch": "main"
}`
	
	if err := os.WriteFile(versionPath, []byte(versionJSON), 0644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	info, err := LoadVersionInfo(tempDir)
	if err != nil {
		t.Fatalf("加载版本信息失败: %v", err)
	}

	if info.Version != "v1.0.0" {
		t.Errorf("版本号错误: 期望 v1.0.0, 得到 %s", info.Version)
	}
	if info.BuildTime != "2024-12-04_11:00:00" {
		t.Errorf("构建时间错误: 期望 2024-12-04_11:00:00, 得到 %s", info.BuildTime)
	}
}

func TestFindVersionFile(t *testing.T) {
	// 创建临时目录结构
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "sub", "dir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	versionPath := filepath.Join(tempDir, "version.json")
	if err := os.WriteFile(versionPath, []byte(`{"version": "v1.0.0"}`), 0644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	// 从子目录查找
	found := findVersionFile(subDir)
	if found != versionPath {
		t.Errorf("查找 version.json 失败: 期望 %s, 得到 %s", versionPath, found)
	}
}

func TestGetVersion(t *testing.T) {
	tempDir := t.TempDir()
	versionPath := filepath.Join(tempDir, "version.json")
	
	versionJSON := `{"version": "v1.2.3"}`
	if err := os.WriteFile(versionPath, []byte(versionJSON), 0644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	version, err := GetVersion(tempDir)
	if err != nil {
		t.Fatalf("获取版本号失败: %v", err)
	}

	if version != "v1.2.3" {
		t.Errorf("版本号错误: 期望 v1.2.3, 得到 %s", version)
	}
}

func TestUpdateVersionInfo(t *testing.T) {
	tempDir := t.TempDir()
	versionPath := filepath.Join(tempDir, "version.json")
	
	versionJSON := `{
  "version": "v1.0.0",
  "build_time": "",
  "commit": "",
  "branch": ""
}`
	if err := os.WriteFile(versionPath, []byte(versionJSON), 0644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	updates := map[string]string{
		"build_time": "2024-12-04_12:00:00",
		"commit":     "def456",
		"branch":     "main",
	}

	if err := UpdateVersionInfo(tempDir, updates); err != nil {
		t.Fatalf("更新版本信息失败: %v", err)
	}

	info, err := LoadVersionInfo(tempDir)
	if err != nil {
		t.Fatalf("重新加载版本信息失败: %v", err)
	}

	if info.BuildTime != "2024-12-04_12:00:00" {
		t.Errorf("构建时间未更新: 期望 2024-12-04_12:00:00, 得到 %s", info.BuildTime)
	}
	if info.Commit != "def456" {
		t.Errorf("提交哈希未更新: 期望 def456, 得到 %s", info.Commit)
	}
}

