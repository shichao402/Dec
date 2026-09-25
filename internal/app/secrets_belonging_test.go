package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

// TestMain 把 registry 快照源换成「不可达」：包内所有测试默认不打真实 registry，
// 归属判定走 ADR 0034 的容错分支（留空、不判孤儿）。需要快照的用例显式 stub。
func TestMain(m *testing.M) {
	orig := secretsBelongingSnapshotSource
	secretsBelongingSnapshotSource = func(context.Context, string) map[string]registry.ProjectSnapshot { return nil }
	code := m.Run()
	secretsBelongingSnapshotSource = orig
	os.Exit(code)
}

// stubBelongingSnapshots 注入一份 registry head 快照。
func stubBelongingSnapshots(t *testing.T, projects map[string]registry.ProjectSnapshot) {
	t.Helper()
	prev := secretsBelongingSnapshotSource
	secretsBelongingSnapshotSource = func(context.Context, string) map[string]registry.ProjectSnapshot {
		return projects
	}
	t.Cleanup(func() { secretsBelongingSnapshotSource = prev })
}

func TestSecretsAddressProject(t *testing.T) {
	cases := []struct {
		address string
		want    string
		ok      bool
	}{
		{"cnb/private/local", "cnb", true},
		{"woa/private/global", "woa", true},
		{"relkit", "relkit", true},
		{"bundle/vikunja", "", false},
		{"Dec", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := secretsAddressProject(tc.address)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("secretsAddressProject(%q) = (%q, %v), 期望 (%q, %v)", tc.address, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSecretsBelongingResolver_States(t *testing.T) {
	const kitRepo = "https://example.com/DecPersonalDevKit.git"
	stubBelongingSnapshots(t, map[string]registry.ProjectSnapshot{
		"cnb":           {OriginRepo: kitRepo, Assets: []registry.SnapshotAsset{{Name: "demo"}}},
		"tencent-cloud": {OriginRepo: kitRepo},
	})

	projectRoot := t.TempDir()
	// cache 回落：ghost 已安装但不在 registry head。
	ghostMeta := filepath.Join(projectRoot, ".dec", "cache", "ghost", registry.ProviderMetaFile)
	if err := os.MkdirAll(filepath.Dir(ghostMeta), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghostMeta, []byte("origin_repo: https://example.com/ghost.git\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &types.ProjectConfig{
		ProjectName: "shared",
		OriginRepo:  kitRepo,
		Requires:    types.RequiresSpec{"subscribed-orphan": types.RequiresLatest},
		Products:    map[string]types.ProductDecl{"woa": {}},
	}
	resolver := newSecretsBelongingResolver(context.Background(), NewWorkspace(WorkspaceProject, projectRoot), cfg)

	if info := resolver.resolve("cnb"); info.OriginRepo != kitRepo || info.IdentityOnly || info.Orphan {
		t.Fatalf("正常产品 = %#v", info)
	}
	if info := resolver.resolve("tencent-cloud"); !info.IdentityOnly || info.Orphan || info.OriginRepo != kitRepo {
		t.Fatalf("身份型产品 = %#v", info)
	}
	if info := resolver.resolve("woa"); info.Orphan || info.OriginRepo != kitRepo {
		t.Fatalf("作者声明的产品不是孤儿: %#v", info)
	}
	if info := resolver.resolve("ghost"); info.Orphan || info.OriginRepo != "https://example.com/ghost.git" {
		t.Fatalf("cache 回落应视为有主: %#v", info)
	}
	if info := resolver.resolve("subscribed-orphan"); !info.Orphan {
		t.Fatalf("registry 查无即行级孤儿: %#v", info)
	}
	if resolver.highConfidenceOrphan("subscribed-orphan") {
		t.Fatal("requires 消费的产品不是高置信删除候选")
	}
	if !resolver.highConfidenceOrphan("nobody") {
		t.Fatal("registry 查无且无人消费应判高置信孤儿")
	}
	if resolver.highConfidenceOrphan("cnb") || resolver.highConfidenceOrphan("woa") {
		t.Fatal("registry 有产品或作者声明产品都不是删除候选")
	}
}

// 不可达红线：连不上 registry 时绝不把产品误判成孤儿；本机 cache 回落仍可填 origin。
func TestSecretsBelongingResolver_UnreachableLeavesBlank(t *testing.T) {
	stubBelongingSnapshots(t, nil)
	projectRoot := t.TempDir()
	ghostMeta := filepath.Join(projectRoot, ".dec", "cache", "ghost", registry.ProviderMetaFile)
	if err := os.MkdirAll(filepath.Dir(ghostMeta), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghostMeta, []byte("origin_repo: https://example.com/ghost.git\n"), 0644); err != nil {
		t.Fatal(err)
	}
	resolver := newSecretsBelongingResolver(context.Background(), NewWorkspace(WorkspaceProject, projectRoot), nil)
	info := resolver.resolve("anything")
	if info.Orphan || info.IdentityOnly || info.OriginRepo != "" {
		t.Fatalf("不可达时归属字段应留空: %#v", info)
	}
	if cached := resolver.resolve("ghost"); cached.OriginRepo != "https://example.com/ghost.git" || cached.Orphan {
		t.Fatalf("cache 回落仍应填充 origin 且不判孤儿: %#v", cached)
	}
}

// 删除候选复用孤儿判定：registry 查无 + 无 requires 消费的 folder 标高置信，
// 身份型产品标「仅密钥」，正常产品带 origin_repo。
func TestListRemoteInventory_AnnotatesBelonging(t *testing.T) {
	const kitRepo = "https://example.com/DecPersonalDevKit.git"
	writeRemoteBrowseSecretsConfig(t, secrets.Config{})
	stubBelongingSnapshots(t, map[string]registry.ProjectSnapshot{
		"cnb":           {OriginRepo: kitRepo, Assets: []registry.SnapshotAsset{{Name: "demo"}}},
		"tencent-cloud": {OriginRepo: kitRepo},
	})
	origFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client {
		return &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
			"cnb/private/project":           {{RelativePath: ".env/cnb.env", Content: "T=1\n"}},
			"tencent-cloud/private/project": {{RelativePath: ".env/tc.env", Content: "T=1\n"}},
			"nobody/private/project":        {{RelativePath: ".env/nobody.env", Content: "T=1\n"}},
		}}
	}
	t.Cleanup(func() { secretsClientFactory = origFactory })

	candidates, err := ListRemoteInventory(context.Background(), NewWorkspace(WorkspaceProject, t.TempDir()), true, nil)
	if err != nil {
		t.Fatalf("ListRemoteInventory: %v", err)
	}
	var sawCnb, sawIdentity, sawOrphan bool
	for _, c := range candidates {
		if c.Kind != DeleteKindSecret || c.Partition != PartitionRemote {
			continue
		}
		switch c.SecretsBundle {
		case "cnb/private/local":
			sawCnb = true
			if c.OriginRepo != kitRepo || c.IdentityOnly || c.HighConfidenceOrphan {
				t.Fatalf("正常产品候选 = %#v", c)
			}
		case "tencent-cloud/private/local":
			sawIdentity = true
			if !c.IdentityOnly || c.HighConfidenceOrphan || c.OriginRepo != kitRepo {
				t.Fatalf("身份型候选 = %#v", c)
			}
		case "nobody/private/local":
			sawOrphan = true
			if !c.HighConfidenceOrphan {
				t.Fatalf("疑似孤儿候选 = %#v", c)
			}
			if !strings.Contains(c.Label, "疑似孤儿") {
				t.Fatalf("Label = %q，应含疑似孤儿标注", c.Label)
			}
		}
	}
	if !sawCnb || !sawIdentity || !sawOrphan {
		t.Fatalf("三类候选都要出现: cnb=%v identity=%v orphan=%v, candidates=%#v", sawCnb, sawIdentity, sawOrphan, candidates)
	}
}
