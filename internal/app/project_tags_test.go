package app

import (
	"testing"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

func TestSaveProjectTagsWritesAndPushes(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"tencent-cloud/dec.yaml": "name: tencent-cloud\ndescription: 腾讯云\n",
		"relkit/dec.yaml":        "name: relkit\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}

	result, err := DefaultPWriter().SaveProjectTags(SaveProjectTagsInput{
		Name: "tencent-cloud",
		Tags: []string{types.ProjectTagGlobal},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed || len(result.Tags) != 1 || result.Tags[0] != types.ProjectTagGlobal {
		t.Fatalf("result = %#v", result)
	}

	if err := withAppReadRepo(func(tx *repo.Transaction) error {
		loaded, err := pmodel.Load(tx.WorkDir(), "tencent-cloud")
		if err != nil {
			return err
		}
		if !pmodel.HasTag(loaded.Manifest.Tags, types.ProjectTagGlobal) {
			t.Fatalf("tags = %#v", loaded.Manifest.Tags)
		}
		relkit, err := pmodel.Load(tx.WorkDir(), "relkit")
		if err != nil {
			return err
		}
		if len(relkit.Manifest.Tags) != 0 {
			t.Fatalf("未改动的项目不应被写标签: %#v", relkit.Manifest.Tags)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	again, err := DefaultPWriter().SaveProjectTags(SaveProjectTagsInput{
		Name: "tencent-cloud",
		Tags: []string{types.ProjectTagGlobal},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Committed {
		t.Fatal("相同标签不应再次提交")
	}
}

func TestSaveProjectTagsRejectsUnknownShape(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	remote := setupRemoteBareRepoProjectTest(t, map[string]string{
		"tools/dec.yaml": "name: tools\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultPWriter().SaveProjectTags(SaveProjectTagsInput{
		Name: "tools",
		Tags: []string{"Not_Valid"},
	}, nil); err == nil {
		t.Fatal("非法标签应失败")
	}
}

func TestResolvePAssetsSurfacesTags(t *testing.T) {
	repoDir := setupRepoWithVault(t, map[string]string{
		"tencent-cloud/dec.yaml":                  "name: tencent-cloud\ntags: [global]\n",
		"tencent-cloud/public/global/rules/x.mdc": "x",
		"relkit/dec.yaml":                         "name: relkit\n",
		"relkit/public/global/rules/y.mdc":        "y",
	})
	got, err := resolveDesiredAssetsForPlane(&types.ProjectConfig{}, repoDir, WorkspaceUser, nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]BundleOverview{}
	for _, item := range got.Bundles {
		byName[item.Name] = item
	}
	if !pmodel.HasTag(byName["tencent-cloud"].Tags, types.ProjectTagGlobal) {
		t.Fatalf("tencent-cloud tags = %#v", byName["tencent-cloud"].Tags)
	}
	if len(byName["relkit"].Tags) != 0 {
		t.Fatalf("relkit tags = %#v", byName["relkit"].Tags)
	}
}
