package compat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairOnStartup_RemovesLegacyDecConfigDir(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, ".dec", "config")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"architecture.json", "project.json", "technology.yaml"} {
		if err := os.WriteFile(filepath.Join(legacyDir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 现行配置：同级文件，必须保留。
	configYAML := filepath.Join(root, ".dec", "config.yaml")
	if err := os.WriteFile(configYAML, []byte("version: v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	notes := RepairOnStartup(root)
	if len(notes) != 1 || !strings.Contains(notes[0], ".dec/config") {
		t.Fatalf("notes = %#v", notes)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy dir 应已删除, err=%v", err)
	}
	if _, err := os.Stat(configYAML); err != nil {
		t.Fatalf("config.yaml 应保留: %v", err)
	}
}

func TestRepairOnStartup_IdempotentWhenAbsent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".dec"), 0o755); err != nil {
		t.Fatal(err)
	}
	notes := RepairOnStartup(root)
	if len(notes) != 0 {
		t.Fatalf("无遗留文件时 notes 应为空, got %#v", notes)
	}
	notes = RepairOnStartup(root)
	if len(notes) != 0 {
		t.Fatalf("第二次仍应为空, got %#v", notes)
	}
}

func TestRepairOnStartup_EmptyRoot(t *testing.T) {
	if notes := RepairOnStartup(""); notes != nil {
		t.Fatalf("空 root 应返回 nil, got %#v", notes)
	}
	if notes := RepairOnStartup("   "); notes != nil {
		t.Fatalf("空白 root 应返回 nil, got %#v", notes)
	}
}

func TestRepairOnStartup_LeavesNonDirConfigAlone(t *testing.T) {
	root := t.TempDir()
	decDir := filepath.Join(root, ".dec")
	if err := os.MkdirAll(decDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 极端情况：名为 config 的普通文件（不是目录）。
	configPath := filepath.Join(decDir, "config")
	if err := os.WriteFile(configPath, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := RepairOnStartup(root)
	if len(notes) != 0 {
		t.Fatalf("非目录 config 不应被删, notes=%#v", notes)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("非目录 config 应保留: %v", err)
	}
}
