package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSyncTargets_MachinePlane(t *testing.T) {
	cfg := &Config{}
	targets, err := cfg.ResolveSyncTargets(SyncPlaneMachine, []string{"cnb"}, "Dec")
	if err != nil {
		t.Fatal(err)
	}
	var machine, projectBundle, projectSecrets int
	for _, tg := range targets {
		switch {
		case tg.Name == "cnb" && tg.Plane == SyncPlaneMachine:
			machine++
			if tg.LocalRoot != "bundles/cnb" {
				t.Fatalf("LocalRoot = %q", tg.LocalRoot)
			}
			if tg.Folder != "bundle/cnb" {
				t.Fatalf("Folder = %q", tg.Folder)
			}
		case tg.Name == "cnb" && tg.Plane == SyncPlaneProject:
			projectBundle++
		case tg.Kind == SyncKindProject:
			projectSecrets++
		}
	}
	if machine != 1 || projectBundle != 0 || projectSecrets != 0 {
		t.Fatalf("machine=%d projectBundle=%d projectSecrets=%d targets=%+v", machine, projectBundle, projectSecrets, targets)
	}
}

func TestResolveSyncTargets_UserPlaneAlias(t *testing.T) {
	cfg := &Config{}
	targets, err := cfg.ResolveSyncTargets(SyncPlaneUser, []string{"woa"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Plane != SyncPlaneMachine || targets[0].Name != "woa" {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestResolveSyncTargets_ProjectPlane(t *testing.T) {
	cfg := &Config{}
	targets, err := cfg.ResolveSyncTargets(SyncPlaneProject, []string{"tencent-cloud"}, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	var projectBundle, projectSecrets, machine int
	for _, tg := range targets {
		switch {
		case tg.Plane == SyncPlaneMachine:
			machine++
		case tg.Kind == SyncKindBundle && tg.Plane == SyncPlaneProject && tg.Name == "tencent-cloud":
			projectBundle++
			if tg.LocalRoot != ".secrets/bundles/tencent-cloud" {
				t.Fatalf("LocalRoot = %q", tg.LocalRoot)
			}
			if tg.Folder != "bundle/tencent-cloud" {
				t.Fatalf("Folder = %q", tg.Folder)
			}
		case tg.Kind == SyncKindProject:
			projectSecrets++
		}
	}
	if machine != 0 || projectBundle != 1 || projectSecrets != 0 {
		t.Fatalf("machine=%d projectBundle=%d projectSecrets=%d (ADR 0014: no project target)", machine, projectBundle, projectSecrets)
	}
}

func TestLocalNoteRelFromRemote_PlainPath(t *testing.T) {
	proj, err := NewProjectSyncTarget("Dec", "Dec")
	if err != nil {
		t.Fatal(err)
	}
	rel, ok, err := LocalNoteRelFromRemote(proj, "config/private.yaml")
	if err != nil || !ok || rel != "config/private.yaml" {
		t.Fatalf("got %q ok=%v err=%v", rel, ok, err)
	}
	rel, ok, err = LocalNoteRelFromRemote(proj, "bundles/cnb/cnb_gitgcm.yaml")
	if err != nil || !ok || rel != "bundles/cnb/cnb_gitgcm.yaml" {
		t.Fatalf("plain mapping should keep full path, got %q ok=%v err=%v", rel, ok, err)
	}
	remote, err := RemoteNoteName(proj, "env/a.env")
	if err != nil || remote != "env/a.env" {
		t.Fatalf("RemoteNoteName = %q err=%v", remote, err)
	}
}

func TestLoadEnvForBundle_MachineOnly(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	projectRoot := t.TempDir()
	machineRoot, err := MachineSecretsRoot()
	if err != nil {
		t.Fatal(err)
	}
	machineEnv := filepath.Join(machineRoot, "bundles", "demo", ".env")
	if err := os.MkdirAll(machineEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machineEnv, "a.env"), []byte("A=machine\nB=machine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	projEnv := filepath.Join(projectRoot, ".secrets", "bundles", "demo", ".env")
	if err := os.MkdirAll(projEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projEnv, "b.env"), []byte("B=project\nC=project\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vars, err := LoadEnvForBundle(projectRoot, "demo", SyncPlaneMachine)
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

func TestLoadEnvForBundle_ProjectOnly(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	projectRoot := t.TempDir()
	machineRoot, err := MachineSecretsRoot()
	if err != nil {
		t.Fatal(err)
	}
	machineEnv := filepath.Join(machineRoot, "bundles", "demo", ".env")
	if err := os.MkdirAll(machineEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machineEnv, "a.env"), []byte("A=machine\nB=machine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	projEnv := filepath.Join(projectRoot, ".secrets", "bundles", "demo", ".env")
	if err := os.MkdirAll(projEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projEnv, "b.env"), []byte("B=project\nC=project\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// 第三层不应再参与合并
	projectLayer := filepath.Join(projectRoot, ".secrets", "project", ".env")
	if err := os.MkdirAll(projectLayer, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectLayer, "p.env"), []byte("C=from-project-secrets\nD=only\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vars, err := LoadEnvForBundle(projectRoot, "demo", SyncPlaneProject)
	if err != nil {
		t.Fatal(err)
	}
	if vars["B"] != "project" || vars["C"] != "project" {
		t.Fatalf("project plane vars = %#v", vars)
	}
	if _, ok := vars["A"]; ok {
		t.Fatalf("project plane 不应读到 machine 层: %#v", vars)
	}
	if _, ok := vars["D"]; ok {
		t.Fatalf("不应再合并 .secrets/project/.env: %#v", vars)
	}
}
