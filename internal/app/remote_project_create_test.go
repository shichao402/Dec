package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
)

func setupCreateRemoteProjectRepo(t *testing.T, files map[string]string) {
	t.Helper()
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, files)
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRemoteProjectPushesManifest(t *testing.T) {
	setupCreateRemoteProjectRepo(t, declaredDemoP())

	result, err := CreateRemoteProject(CreateRemoteProjectInput{
		Name:        "agentshelpme",
		Description: "工作区仓库",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed || result.AlreadyExists {
		t.Fatalf("result = %#v", result)
	}

	// 新项目必须能被 bind / pull 的同一套扫描识别，否则绑定仍会报「仓库中不存在」。
	var names []string
	if err := withLocalReadRepoDir(func(repoDir string) error {
		projects, scanErr := pmodel.Scan(repoDir)
		if scanErr != nil {
			return scanErr
		}
		for name := range projects {
			names = append(names, name)
		}
		manifest, readErr := os.ReadFile(filepath.Join(repoDir, result.ManifestPath))
		if readErr != nil {
			return readErr
		}
		if !strings.Contains(string(manifest), "name: agentshelpme") {
			t.Fatalf("manifest = %s", manifest)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !contains(names, "agentshelpme") || !contains(names, "demo") {
		t.Fatalf("scanned projects = %v", names)
	}
}

func TestCreateRemoteProjectIsIdempotent(t *testing.T) {
	setupCreateRemoteProjectRepo(t, declaredDemoP())

	result, err := CreateRemoteProject(CreateRemoteProjectInput{Name: "demo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.AlreadyExists || result.Committed {
		t.Fatalf("result = %#v", result)
	}
}

func TestCreateRemoteProjectRejectsInvalidName(t *testing.T) {
	setupCreateRemoteProjectRepo(t, declaredDemoP())

	for _, name := range []string{"", "AgentsHelpMe", "agents_help_me", "-agents"} {
		if _, err := CreateRemoteProject(CreateRemoteProjectInput{Name: name}, nil); err == nil {
			t.Fatalf("项目名 %q 应被拒绝", name)
		}
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
