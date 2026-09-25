package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
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
		"woa":           {OriginRepo: kitRepo, SecretsPlane: "global"},
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
	if info := resolver.resolve("woa"); info.DeclaredPlane != "global" {
		t.Fatalf("registry 快照应携带声明平面: %#v", info)
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
	if err := os.WriteFile(ghostMeta, []byte("origin_repo: https://example.com/ghost.git\nsecrets_plane: local\n"), 0644); err != nil {
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
	if cached := resolver.resolve("ghost"); cached.DeclaredPlane != "local" {
		t.Fatalf("cache 回落应填充声明平面: %#v", cached)
	}
}

// ADR 0035：声明平面判读——声明与 workspace 平面的匹配，迁移期空声明恒通过。
func TestDeclaredPlaneMatchesWorkspace(t *testing.T) {
	project := NewWorkspace(WorkspaceProject, "/tmp/demo")
	global := NewWorkspace(WorkspaceUser, "")
	cases := []struct {
		declared string
		project  bool
		global   bool
	}{
		{"", true, true},
		{"global", false, true},
		{"local", true, false},
		{"weird", true, true},
	}
	for _, tc := range cases {
		if got := assetPlaneMatchesWorkspace(tc.declared, project); got != tc.project {
			t.Fatalf("assetPlaneMatchesWorkspace(%q, project) = %v", tc.declared, got)
		}
		if got := assetPlaneMatchesWorkspace(tc.declared, global); got != tc.global {
			t.Fatalf("assetPlaneMatchesWorkspace(%q, global) = %v", tc.declared, got)
		}
	}
}

// ADR 0035：plan 按声明平面过滤——声明 global 的产品不进项目平面 plan；
// 未声明产品两侧都保留（迁移期 fail-open）。
func TestPlanWorkspaceSecretsSync_FiltersByDeclaredPlane(t *testing.T) {
	setEnvForProjectTest(t, "DEC_HOME", t.TempDir())
	stubBelongingSnapshots(t, map[string]registry.ProjectSnapshot{
		"cnb":    {OriginRepo: "https://example.com/kit.git", SecretsPlane: "global"},
		"local-product": {OriginRepo: "https://example.com/kit.git", SecretsPlane: "local"},
	})

	projectRoot := t.TempDir()
	mgr := config.NewProjectConfigManager(projectRoot)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName: "demo",
		Requires:    types.RequiresSpec{"cnb": types.RequiresLatest, "local-product": types.RequiresLatest},
	}); err != nil {
		t.Fatal(err)
	}

	// 项目平面：本仓项目 demo（未声明）保留；声明的订阅产品不落项目平面 target
	// （ADR 0009 平面隔离本来就只认本仓项目名，这里主要验证用户平面方向）。
	projectPlan, err := planWorkspaceSecretsSync(
		context.Background(), NewWorkspace(WorkspaceProject, projectRoot),
		nil, &secrets.Config{},
	)
	if err != nil {
		t.Fatalf("planWorkspaceSecretsSync(project) = %v", err)
	}
	if len(projectPlan.Targets) != 1 || projectPlan.Targets[0].Name != "demo" {
		t.Fatalf("project plan = %#v", projectPlan.Targets)
	}

	// 用户平面：声明 global 的 cnb 保留；声明 local 的 local-product 被过滤；未声明的 shared 保留。
	if err := config.SaveGlobalConfig(&types.GlobalConfig{
		Requires: types.RequiresSpec{"cnb": types.RequiresLatest, "local-product": types.RequiresLatest, "shared": types.RequiresLatest},
	}); err != nil {
		t.Fatal(err)
	}
	userPlan, err := planWorkspaceSecretsSync(
		context.Background(), NewWorkspace(WorkspaceUser, ""),
		[]string{"cnb", "local-product", "shared"}, &secrets.Config{},
	)
	if err != nil {
		t.Fatalf("planWorkspaceSecretsSync(user) = %v", err)
	}
	got := make(map[string]bool, len(userPlan.Targets))
	for _, target := range userPlan.Targets {
		got[target.Name] = true
	}
	if !got["cnb"] || got["local-product"] || !got["shared"] {
		t.Fatalf("user plan = %#v：cnb/shared 应保留，local-product 应被过滤", userPlan.Targets)
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
