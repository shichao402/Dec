package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGlobalVarsView_NoFile(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	view, err := LoadGlobalVarsView()
	if err != nil {
		t.Fatalf("LoadGlobalVarsView() 失败: %v", err)
	}
	if view.VarsFileReady {
		t.Fatal("无 vars.yaml 时 VarsFileReady 应为 false")
	}
	if view.VarsPath == "" {
		t.Fatal("VarsPath 应返回默认路径，即使文件不存在")
	}
	if len(view.Vars) != 0 {
		t.Fatalf("Vars = %#v, 期望空", view.Vars)
	}
	if view.EditorCommand == "" {
		t.Fatal("EditorCommand 应有默认值")
	}
}

func TestLoadGlobalVarsView_WithFile(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)

	localDir := filepath.Join(decHome, "local")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatalf("MkdirAll(local) 失败: %v", err)
	}
	content := `vars:
  MACHINE_TOKEN: "mt"
`
	if err := os.WriteFile(filepath.Join(localDir, "vars.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("写入 vars.yaml 失败: %v", err)
	}

	view, err := LoadGlobalVarsView()
	if err != nil {
		t.Fatalf("LoadGlobalVarsView() 失败: %v", err)
	}
	if !view.VarsFileReady {
		t.Fatal("期望 VarsFileReady = true")
	}
	if view.Vars["MACHINE_TOKEN"] != "mt" {
		t.Fatalf("Vars[MACHINE_TOKEN] = %q, 期望 mt", view.Vars["MACHINE_TOKEN"])
	}
}

func TestEnsureGlobalVarsFile_CreateThenIdempotent(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())

	r1, err := EnsureGlobalVarsFile()
	if err != nil {
		t.Fatalf("第一次 EnsureGlobalVarsFile() 失败: %v", err)
	}
	if !r1.Created {
		t.Fatal("首次调用应创建模板文件")
	}
	if _, err := os.Stat(r1.Path); err != nil {
		t.Fatalf("stat %s 失败: %v", r1.Path, err)
	}

	custom := []byte("vars:\n  CUSTOM: \"1\"\n")
	if err := os.WriteFile(r1.Path, custom, 0644); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}

	r2, err := EnsureGlobalVarsFile()
	if err != nil {
		t.Fatalf("第二次 EnsureGlobalVarsFile() 失败: %v", err)
	}
	if r2.Created {
		t.Fatal("文件已存在时不应再次创建")
	}
	got, err := os.ReadFile(r2.Path)
	if err != nil {
		t.Fatalf("读取已有文件失败: %v", err)
	}
	if string(got) != string(custom) {
		t.Fatal("EnsureGlobalVarsFile 二次调用不应覆盖已有内容")
	}
}
