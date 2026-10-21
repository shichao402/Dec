package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/prune"
	"github.com/shichao402/Dec/internal/secrets"
)

// isolateHome 把 HOME 指向临时目录，避免测试读写真实的 ~/.dec 与 ~/.ssh。
// 测试结束后自动还原。
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	prev, had := os.LookupEnv("HOME")
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("设置 HOME 失败: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("HOME", prev)
			return
		}
		_ = os.Unsetenv("HOME")
	})
	return home
}

func writeProjectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

// projectTarget 为测试构造一个 project 平面的同步目标。
func projectTarget(t *testing.T, name string) secrets.SyncTarget {
	t.Helper()
	target, err := secrets.NewPSyncTarget(name, secrets.SyncPlaneProject)
	if err != nil {
		t.Fatalf("构造 SyncTarget 失败: %v", err)
	}
	return target
}

func findItem(items []PruneItem, relPath string) (PruneItem, bool) {
	for _, item := range items {
		if item.RelPath == relPath {
			return item, true
		}
	}
	return PruneItem{}, false
}

// 需求 2 守护用例：路径形态不属于产品同步的 note 类型（未知点目录）时，
// 无法确认是远端派生内容，必须保守保留，绝不剪枝。
func TestDecideNoteKeepsLocalOnlyContent(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	target := projectTarget(t, "demo")

	// 未知点目录（ParseTypePath 硬失败）：产品不认识，视为可能含手工内容，保留。
	writeProjectFile(t, root, secrets.SecretsRootDir+"/demo/.material/keys.pem", "PRIVATE\n")

	decision := DecideTargetPrune(root, target, nil, nil, PruneScope{})
	item, ok := findItem(decision.Items, ".material/keys.pem")
	if !ok {
		t.Fatalf("应判定该条目, got %#v", decision.Items)
	}
	if item.Class == PruneClassRebuildable {
		t.Fatalf("未知点类型的条目绝不可剪枝, got %q（%s）", item.Class, item.Detail)
	}
	if len(decision.Removable()) != 0 {
		t.Fatal("未知类型不应产生可剪枝项")
	}
}

// 派生类型（env 源 / GCM 托管）在远端确认后应可重建，从而可剪枝。
func TestDecideNoteClassifiesDerivedTypesAsRebuildable(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	target := projectTarget(t, "demo")

	writeProjectFile(t, root, secrets.SecretsRootDir+"/demo/.env/app.env", "TOKEN=1\n")
	writeProjectFile(t, root, secrets.SecretsRootDir+"/demo/.gcm/cnb.yaml", "host: git\nusername: u\npassword: p\npath: r\n")

	decision := DecideTargetPrune(root, target, nil, nil, PruneScope{})

	envItem, ok := findItem(decision.Items, ".env/app.env")
	if !ok {
		t.Fatalf("应判定 env 条目, got %#v", decision.Items)
	}
	if envItem.Class != PruneClassRebuildable {
		t.Fatalf("env 条目应可重建, got %q（%s）", envItem.Class, envItem.Detail)
	}
	if envItem.Reason != prune.ReasonRemoteGone {
		t.Fatalf("理由应为 remote_gone, got %q", envItem.Reason)
	}
	if !envItem.RemoteRecoverable {
		t.Fatal("env 条目应标注可从远端恢复")
	}

	gcmItem, ok := findItem(decision.Items, ".gcm/cnb.yaml")
	if !ok {
		t.Fatalf("应判定 gcm 条目, got %#v", decision.Items)
	}
	if gcmItem.Class != PruneClassRebuildable {
		t.Fatalf("handler 托管条目应可重建, got %q（%s）", gcmItem.Class, gcmItem.Detail)
	}
}

func TestDecideNoteKeepsRemotePresent(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	target := projectTarget(t, "demo")
	writeProjectFile(t, root, secrets.SecretsRootDir+"/demo/.env/app.env", "TOKEN=1\n")

	remote := []secrets.SecureNote{{RelativePath: ".env/app.env", Content: "TOKEN=1\n"}}
	decision := DecideTargetPrune(root, target, remote, nil, PruneScope{})
	if len(decision.Items) != 0 {
		t.Fatalf("远端仍存在的条目不应进入判定, got %#v", decision.Items)
	}
	if !pruneDecisionIsEmpty(decision) {
		t.Fatal("判定结果应为空")
	}
}

func TestDecideScopeFiltersByBundleAndPrefix(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	alpha := projectTarget(t, "alpha")
	beta := projectTarget(t, "beta")
	writeProjectFile(t, root, secrets.SecretsRootDir+"/alpha/.env/a.env", "A=1\n")
	writeProjectFile(t, root, secrets.SecretsRootDir+"/alpha/.env/b.env", "B=1\n")
	writeProjectFile(t, root, secrets.SecretsRootDir+"/beta/.env/c.env", "C=1\n")

	// bundle 过滤：只判定 alpha。
	byBundle := DecideTargetPrune(root, alpha, nil, nil, PruneScope{Bundle: "alpha"})
	if len(byBundle.Items) != 2 {
		t.Fatalf("alpha 应判定 2 条, got %d", len(byBundle.Items))
	}
	other := DecideTargetPrune(root, beta, nil, nil, PruneScope{Bundle: "alpha"})
	if len(other.Items) != 0 {
		t.Fatalf("beta 目标在 alpha 过滤下应为空, got %d", len(other.Items))
	}

	// 路径前缀过滤。
	byPrefix := DecideTargetPrune(root, alpha, nil, nil, PruneScope{PathPrefix: ".secrets/alpha/.env/a"})
	if len(byPrefix.Items) != 1 {
		t.Fatalf("前缀过滤应只剩 1 条, got %d", len(byPrefix.Items))
	}
	if byPrefix.Items[0].RelPath != ".env/a.env" {
		t.Fatalf("过滤结果不符, got %q", byPrefix.Items[0].RelPath)
	}
}

func TestDecideMissingBundleDirsRespectsRemoteConfirmation(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	workspace := NewWorkspace(WorkspaceProject, root)
	writeProjectFile(t, root, secrets.SecretsRootDir+"/gone/.env/a.env", "A=1\n")

	confirmed := decideMissingBundleDirs(workspace, "gone", true)
	if len(confirmed) != 1 {
		t.Fatalf("应识别出 1 个目录, got %d", len(confirmed))
	}
	if confirmed[0].Class != PruneClassRebuildable {
		t.Fatalf("远端已确认时目录应可剪枝, got %q", confirmed[0].Class)
	}
	if confirmed[0].Reason != prune.ReasonDerivedBundle {
		t.Fatalf("理由应为 derived_bundle, got %q", confirmed[0].Reason)
	}

	unconfirmed := decideMissingBundleDirs(workspace, "gone", false)
	if len(unconfirmed) != 1 {
		t.Fatalf("应识别出 1 个目录, got %d", len(unconfirmed))
	}
	if unconfirmed[0].Class != PruneClassUnknown {
		t.Fatalf("未确认远端时必须保留, got %q", unconfirmed[0].Class)
	}
}

func TestDecideMissingBundleDirsIgnoresAbsentDir(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	workspace := NewWorkspace(WorkspaceProject, root)
	if items := decideMissingBundleDirs(workspace, "nope", true); len(items) != 0 {
		t.Fatalf("目录不存在时不应产生判定项, got %#v", items)
	}
}

func TestSummarizePruneDecision(t *testing.T) {
	cases := []struct {
		name string
		d    PruneDecision
		want string
	}{
		{name: "空", d: PruneDecision{}, want: "无待剪枝条目"},
		{
			name: "只有可剪枝",
			d: PruneDecision{Items: []PruneItem{
				{Class: PruneClassRebuildable}, {Class: PruneClassRebuildable},
			}},
			want: "2 项可剪枝",
		},
		{
			name: "只有保留",
			d:    PruneDecision{Items: []PruneItem{{Class: PruneClassLocalOnly}}},
			want: "无待剪枝条目（保留 1 项）",
		},
		{
			name: "混合",
			d: PruneDecision{Items: []PruneItem{
				{Class: PruneClassRebuildable}, {Class: PruneClassLocalOnly}, {Class: PruneClassUnknown},
			}},
			want: "1 项可剪枝，保留 2 项",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summarizePruneDecision(tc.d); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestDecideIsReadOnly(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	target := projectTarget(t, "demo")
	rel := secrets.SecretsRootDir + "/demo/.env/app.env"
	writeProjectFile(t, root, rel, "TOKEN=1\n")

	full := filepath.Join(root, filepath.FromSlash(rel))
	before, err := os.Stat(full)
	if err != nil {
		t.Fatalf("读取前置状态失败: %v", err)
	}
	_ = DecideTargetPrune(root, target, nil, nil, PruneScope{})
	after, err := os.Stat(full)
	if err != nil {
		t.Fatalf("判定后文件必须仍在: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("判定过程不得改动文件")
	}
}

func TestIsDerivedFromRemote(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{".env/app.env", true},            // env 点类型目录
		{"pkg/.env/app.env", true},        // 首段非点目录 → 普通 note
		{"env/app.env", true},             // 首段非点目录 → 普通 note
		{".env/nested/app.env", true},     // .env 目录下任意文件属 env 派生
		{".gcm/cnb.yaml", true},           // handler 托管
		{"config/server.yaml", true},      // 普通 note 落盘
		{".env/README.md", true},          // .env 目录下文件属 env 派生
		{"app.env", true},                 // 首段非点目录 → 普通 note
		{"../.env/app.env", false},        // 越界
		{"", false},
	}
	for _, tc := range cases {
		if got := isDerivedFromRemote(tc.rel); got != tc.want {
			t.Fatalf("isDerivedFromRemote(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

func TestPrunePathHint(t *testing.T) {
	isolateHome(t)
	target := projectTarget(t, "demo")
	hint := prunePathHint(target, ".env/a.env")
	if !strings.HasPrefix(filepath.ToSlash(hint), secrets.SecretsRootDir+"/demo/") {
		t.Fatalf("提示路径应带同步根前缀, got %q", hint)
	}
}

// 需求 8.2：strict 模式下，默认保护的本地独有条目也按隔离处理（仍可恢复）。
func TestDecideTargetPruneStrictForcesLocalOnlyIntoQuarantine(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	target := projectTarget(t, "demo")

	// 未知点目录：默认应保留（来源不明）。
	rel := secrets.SecretsRootDir + "/demo/.material/keys.pem"
	writeProjectFile(t, root, rel, "PRIVATE\n")

	// 默认（宽松）：保留。
	loose := DecideTargetPrune(root, target, nil, nil, PruneScope{})
	if len(loose.Removable()) != 0 {
		t.Fatalf("默认宽松不应清除未知类型, got %#v", loose.Items)
	}

	// strict：移入隔离区，且标注审计理由。
	strict := DecideTargetPrune(root, target, nil, nil, PruneScope{Strict: true})
	removable := strict.Removable()
	if len(removable) != 1 {
		t.Fatalf("strict 应把未知类型纳入隔离, got %#v", strict.Items)
	}
	if eq := removable[0].Class; eq != PruneClassRebuildable {
		t.Fatalf("strict 隔离项应归为 rebuildable, got %q", eq)
	}
	if !strings.Contains(removable[0].Detail, "strict") {
		t.Fatalf("strict 项应带审计原因: %q", removable[0].Detail)
	}
}
