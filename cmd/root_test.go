package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionCommandRegistered(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"version"})
	if err != nil {
		t.Fatalf("查找 version 命令失败: %v", err)
	}
	if cmd == nil || cmd.Name() != "version" {
		t.Fatalf("期望找到 version 命令")
	}
}

func TestRunVersionPrintsCurrentVersion(t *testing.T) {
	oldVersion := appVersion
	oldBuildTime := appBuildTime
	defer func() {
		appVersion = oldVersion
		appBuildTime = oldBuildTime
		RootCmd.Version = getVersionString()
	}()

	SetVersion("v1.10.40", "2026-04-03_00:00:00")

	var buf bytes.Buffer
	versionCmd.SetOut(&buf)
	versionCmd.SetErr(&buf)

	if err := runVersion(versionCmd, nil); err != nil {
		t.Fatalf("runVersion 返回错误: %v", err)
	}

	if got := buf.String(); got != "v1.10.40\n" {
		t.Fatalf("runVersion 输出 = %q, 期望 %q", got, "v1.10.40\n")
	}
}

func TestGetVersionFallsBackToVersionFileWhenAppVersionIsDev(t *testing.T) {
	tempDir := t.TempDir()
	versionFile := filepath.Join(tempDir, "version.json")
	if err := os.WriteFile(versionFile, []byte("{\n  \"version\": \"v9.9.9\"\n}\n"), 0644); err != nil {
		t.Fatalf("写入 version.json 失败: %v", err)
	}

	oldVersion := appVersion
	oldBuildTime := appBuildTime
	defer func() {
		appVersion = oldVersion
		appBuildTime = oldBuildTime
		RootCmd.Version = getVersionString()
	}()

	appVersion = "dev"
	appBuildTime = "unknown"
	RootCmd.Version = getVersionString()
	chdirForTest(t, tempDir)

	if got := GetVersion(); got != "v9.9.9" {
		t.Fatalf("GetVersion() = %q, 期望 %q", got, "v9.9.9")
	}
}
