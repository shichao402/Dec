package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePSyncTargets_MachinePlane(t *testing.T) {
	targets, err := ResolvePSyncTargets(SyncPlaneMachine, []string{"cnb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %+v", targets)
	}
	tg := targets[0]
	if tg.Name != "cnb" || !IsMachinePlane(tg.Plane) {
		t.Fatalf("target = %+v", tg)
	}
	// 机器平面同步根相对 ~/.dec/secrets，只有 P 名一级。
	if tg.LocalRoot != "cnb" {
		t.Fatalf("LocalRoot = %q", tg.LocalRoot)
	}
	if tg.Address != "cnb/private/global" {
		t.Fatalf("Address = %q", tg.Address)
	}
}

func TestResolvePSyncTargets_UserPlaneAlias(t *testing.T) {
	targets, err := ResolvePSyncTargets(SyncPlaneUser, []string{"woa"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || !IsMachinePlane(targets[0].Plane) || targets[0].Name != "woa" {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestResolvePSyncTargets_ProjectPlane(t *testing.T) {
	targets, err := ResolvePSyncTargets(SyncPlaneProject, []string{"tencent-cloud"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %+v", targets)
	}
	tg := targets[0]
	if (tg.Plane != SyncPlaneProject && tg.Plane != SyncPlaneLocal) || tg.Name != "tencent-cloud" {
		t.Fatalf("target = %+v", tg)
	}
	if tg.LocalRoot != ".secrets/tencent-cloud" {
		t.Fatalf("LocalRoot = %q", tg.LocalRoot)
	}
	if tg.Address != "tencent-cloud/private/local" {
		t.Fatalf("Address = %q", tg.Address)
	}
}

// 同步根相对路径是唯一的本地/远端映射键；BW 条目名前缀由 secrets 内部加。
func TestNormalizeNoteRel_KeepsFullPath(t *testing.T) {
	for raw, want := range map[string]string{
		"config/private.yaml":     "config/private.yaml",
		"./.env/a.env":            ".env/a.env",
		"nested//dir/./file.yaml": "nested/dir/file.yaml",
	} {
		got, err := NormalizeNoteRel(raw)
		if err != nil {
			t.Fatalf("NormalizeNoteRel(%q) = %v", raw, err)
		}
		if got != want {
			t.Fatalf("NormalizeNoteRel(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestLoadEnvForP_MachinePlaneOnly(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	projectRoot := t.TempDir()
	machineRoot, err := MachineSecretsRoot()
	if err != nil {
		t.Fatal(err)
	}
	machineEnv := filepath.Join(machineRoot, "demo", ".env")
	if err := os.MkdirAll(machineEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machineEnv, "a.env"), []byte("A=machine\nB=machine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	projEnv := filepath.Join(projectRoot, ".secrets", "demo", ".env")
	if err := os.MkdirAll(projEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projEnv, "b.env"), []byte("B=project\nC=project\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vars, err := LoadEnvForP(projectRoot, "demo", SyncPlaneMachine)
	if err != nil {
		t.Fatal(err)
	}
	if vars["A"] != "machine" || vars["B"] != "machine" {
		t.Fatalf("machine plane vars = %#v", vars)
	}
	if _, ok := vars["C"]; ok {
		t.Fatalf("machine plane 不应读到 project 层: %#v", vars)
	}
}

func TestLoadEnvForP_ProjectPlaneOnly(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	projectRoot := t.TempDir()
	machineRoot, err := MachineSecretsRoot()
	if err != nil {
		t.Fatal(err)
	}
	machineEnv := filepath.Join(machineRoot, "demo", ".env")
	if err := os.MkdirAll(machineEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machineEnv, "a.env"), []byte("A=machine\nB=machine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	projEnv := filepath.Join(projectRoot, ".secrets", "demo", ".env")
	if err := os.MkdirAll(projEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projEnv, "b.env"), []byte("B=project\nC=project\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vars, err := LoadEnvForP(projectRoot, "demo", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	if vars["B"] != "project" || vars["C"] != "project" {
		t.Fatalf("project plane vars = %#v", vars)
	}
	if _, ok := vars["A"]; ok {
		t.Fatalf("project plane 不应读到 machine 层: %#v", vars)
	}
}
