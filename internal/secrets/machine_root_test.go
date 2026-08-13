package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSyncTargets_UserOnlyMachine(t *testing.T) {
	cfg := &Config{UserEnabledBundles: []string{"cnb"}}
	targets, err := cfg.ResolveSyncTargets(nil, cfg.UserEnabledBundleNames(), "Dec")
	if err != nil {
		t.Fatal(err)
	}
	var machine, projectBundle int
	for _, tg := range targets {
		if tg.Name == "cnb" && tg.Plane == SyncPlaneMachine {
			machine++
			if tg.LocalRoot != "bundles/cnb" {
				t.Fatalf("LocalRoot = %q", tg.LocalRoot)
			}
			if tg.Folder != "bundle/cnb" {
				t.Fatalf("Folder = %q", tg.Folder)
			}
		}
		if tg.Name == "cnb" && tg.Plane == SyncPlaneProject && tg.NoteNamePrefix == "" {
			projectBundle++
		}
	}
	if machine != 1 || projectBundle != 0 {
		t.Fatalf("machine=%d projectBundle=%d targets=%+v", machine, projectBundle, targets)
	}
}

func TestResolveSyncTargets_BothAddsOverlay(t *testing.T) {
	cfg := &Config{}
	targets, err := cfg.ResolveSyncTargets([]string{"tencent-cloud"}, []string{"tencent-cloud"}, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	var machine, overlay, project int
	for _, tg := range targets {
		switch {
		case tg.Plane == SyncPlaneMachine && tg.Name == "tencent-cloud":
			machine++
		case tg.NoteNamePrefix == "bundles/tencent-cloud/" && tg.Folder == "my-app":
			overlay++
			if tg.LocalRoot != ".secrets/bundles/tencent-cloud" {
				t.Fatalf("overlay LocalRoot = %q", tg.LocalRoot)
			}
		case tg.Kind == SyncKindProject:
			project++
			if len(tg.NoteNameExcludePrefixes) != 1 || tg.NoteNameExcludePrefixes[0] != "bundles/tencent-cloud/" {
				t.Fatalf("exclude = %#v", tg.NoteNameExcludePrefixes)
			}
		}
	}
	if machine != 1 || overlay != 1 || project != 1 {
		t.Fatalf("machine=%d overlay=%d project=%d", machine, overlay, project)
	}
}

func TestLocalNoteRelFromRemote_PrefixAndExclude(t *testing.T) {
	overlay, err := NewProjectBundleOverlayTarget("cnb", "Dec")
	if err != nil {
		t.Fatal(err)
	}
	rel, ok, err := LocalNoteRelFromRemote(overlay, "bundles/cnb/cnb_gitgcm.yaml")
	if err != nil || !ok || rel != "cnb_gitgcm.yaml" {
		t.Fatalf("got %q ok=%v err=%v", rel, ok, err)
	}
	_, ok, err = LocalNoteRelFromRemote(overlay, "config/private.yaml")
	if err != nil || ok {
		t.Fatalf("should skip non-prefix, ok=%v err=%v", ok, err)
	}

	proj, err := NewProjectSyncTarget("Dec", "Dec")
	if err != nil {
		t.Fatal(err)
	}
	proj.NoteNameExcludePrefixes = []string{"bundles/cnb/"}
	_, ok, err = LocalNoteRelFromRemote(proj, "bundles/cnb/cnb_gitgcm.yaml")
	if err != nil || ok {
		t.Fatalf("excluded should skip, ok=%v err=%v", ok, err)
	}
	rel, ok, err = LocalNoteRelFromRemote(proj, "config/private.yaml")
	if err != nil || !ok || rel != "config/private.yaml" {
		t.Fatalf("got %q ok=%v err=%v", rel, ok, err)
	}
}

func TestLoadEnvForBundle_MergeProjectOverMachine(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	projectRoot := t.TempDir()
	machineRoot, err := MachineSecretsRoot()
	if err != nil {
		t.Fatal(err)
	}
	machineEnv := filepath.Join(machineRoot, "bundles", "demo", "env")
	if err := os.MkdirAll(machineEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machineEnv, "a.env"), []byte("A=machine\nB=machine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	projEnv := filepath.Join(projectRoot, ".secrets", "bundles", "demo", "env")
	if err := os.MkdirAll(projEnv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projEnv, "b.env"), []byte("B=project\nC=project\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vars, err := LoadEnvForBundle(projectRoot, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if vars["A"] != "machine" || vars["B"] != "project" || vars["C"] != "project" {
		t.Fatalf("vars = %#v", vars)
	}
}
