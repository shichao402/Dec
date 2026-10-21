package assets

import (
	"errors"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

func newTestWriter(t *testing.T) (*Writer, *crypt.Key) {
	t.Helper()
	store, err := ssot.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	key, err := crypt.NewKey(nil)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return NewWriter(store), key
}

func author() ssot.Author { return ssot.Author{Name: "dev-1", Source: "mcp"} }

func skillFiles() []AssetFile {
	return []AssetFile{
		{Path: "demo/SKILL.md", Content: []byte("# Demo skill\n")},
		{Path: "demo/run.sh", Content: []byte("echo hi\n"), Executable: true},
	}
}

func manifestFor(name string, planes ...ssot.Plane) []byte {
	if len(planes) == 0 {
		planes = []ssot.Plane{ssot.PlaneUser}
	}
	var b strings.Builder
	b.WriteString("name: " + name + "\nitems:\n")
	b.WriteString("  - kind: skill\n")
	b.WriteString("    path: demo\n")
	b.WriteString("    name: demo-skill\n")
	b.WriteString("    planes: [")
	for i, p := range planes {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(string(p))
	}
	b.WriteString("]\n")
	return []byte(b.String())
}

func TestWriteAssetCreatesManifestAndFiles(t *testing.T) {
	w, _ := newTestWriter(t)
	out, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  skillFiles(),
		Author: author(),
	})
	if err != nil {
		t.Fatalf("WriteAsset: %v", err)
	}
	if out.Revision == "" {
		t.Fatal("revision 为空")
	}
	if out.Manifest.Name != "woa" || len(out.Manifest.Items) != 1 {
		t.Fatalf("manifest 推断错误: %+v", out.Manifest)
	}
	if out.Manifest.Items[0].Kind != ssot.KindSkill {
		t.Fatalf("应识别为 skill，实际 %s", out.Manifest.Items[0].Kind)
	}
	if out.Manifest.Items[0].Name != "demo" {
		t.Fatalf("条目名错误: %s", out.Manifest.Items[0].Name)
	}

	files, err := w.ReadBundleFiles("woa")
	if err != nil {
		t.Fatalf("ReadBundleFiles: %v", err)
	}
	// manifest + 2 个资产文件
	if len(files) != 3 {
		t.Fatalf("文件数错误: %d", len(files))
	}

	bundles, err := w.ListBundles()
	if err != nil {
		t.Fatalf("ListBundles: %v", err)
	}
	if len(bundles) != 1 || bundles[0] != "woa" {
		t.Fatalf("bundle 列表错误: %v", bundles)
	}
}

// TestInferManifestAlwaysValid 锁定推断规则的自洽性。
//
// 推断出来的 manifest 必须能通过自己的校验：带 SKILL.md 的一级目录是 skill，
// 其余文件各自是 rule。曾经的规则把“没有 SKILL.md 的目录”也当成一条 rule，
// 于是 rules/a.md 这种自然写法会推断出一条指向目录的 rule，报“内容文件缺失”。
func TestInferManifestAlwaysValid(t *testing.T) {
	w, _ := newTestWriter(t)
	out, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files: []AssetFile{
			{Path: "rules/style.md", Content: []byte("style")},
			{Path: "demo/SKILL.md", Content: []byte("# demo")},
			{Path: "demo/run.sh", Content: []byte("echo hi\n"), Executable: true},
			{Path: "notes.md", Content: []byte("note")},
		},
		Author: author(),
	})
	if err != nil {
		t.Fatalf("推断出的 manifest 应能通过校验: %v", err)
	}
	got := map[string]ssot.AssetKind{}
	for _, item := range out.Manifest.Items {
		got[item.Path] = item.Kind
	}
	want := map[string]ssot.AssetKind{
		"rules/style.md": ssot.KindRule,
		"demo":           ssot.KindSkill,
		"notes.md":       ssot.KindRule,
	}
	if len(got) != len(want) {
		t.Fatalf("条目数 %d，期望 %d：%+v", len(got), len(want), got)
	}
	for p, kind := range want {
		if got[p] != kind {
			t.Fatalf("条目 %q 类型为 %q，期望 %q", p, got[p], kind)
		}
	}
}

func TestWriteAssetReplacesWholeBundle(t *testing.T) {
	w, _ := newTestWriter(t)
	if _, err := w.WriteAsset(WriteAssetInput{Bundle: "woa", Files: skillFiles(), Author: author()}); err != nil {
		t.Fatalf("首次写入: %v", err)
	}
	// 第二次只写一个文件，旧的 run.sh 必须消失。
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  []AssetFile{{Path: "demo/SKILL.md", Content: []byte("# v2\n")}},
		Author: author(),
	}); err != nil {
		t.Fatalf("二次写入: %v", err)
	}
	files, err := w.ReadBundleFiles("woa")
	if err != nil {
		t.Fatalf("ReadBundleFiles: %v", err)
	}
	for _, f := range files {
		if f.Path == "demo/run.sh" {
			t.Fatal("整体替换语义下旧文件应被删除")
		}
	}
}

func TestWriteAssetRejectsBadBundleName(t *testing.T) {
	w, _ := newTestWriter(t)
	for _, name := range []string{"", "_cloud", "Bad Name", "../x"} {
		if _, err := w.WriteAsset(WriteAssetInput{Bundle: name, Files: skillFiles(), Author: author()}); err == nil {
			t.Fatalf("bundle 名 %q 应被拒绝", name)
		}
	}
}

func TestWriteAssetRejectsPathTraversal(t *testing.T) {
	w, _ := newTestWriter(t)
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  []AssetFile{{Path: "../../etc/passwd", Content: []byte("x")}},
		Author: author(),
	})
	if err == nil {
		t.Fatal("越界路径应被拒绝")
	}
}

func TestWriteAssetRejectsMissingSecret(t *testing.T) {
	w, _ := newTestWriter(t)
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files: []AssetFile{{
			Path:    "demo/SKILL.md",
			Content: []byte("token={{secret:api-token}}\n"),
		}},
		Author: author(),
	})
	if !errors.Is(err, ErrMissingSecret) {
		t.Fatalf("期望 ErrMissingSecret，实际 %v", err)
	}
}

func TestWriteAssetAcceptsExistingSecret(t *testing.T) {
	w, key := newTestWriter(t)
	if _, err := w.PutSecret(key, PutSecretInput{Name: "api-token", Value: "v", Author: author()}); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files: []AssetFile{{
			Path:    "demo/SKILL.md",
			Content: []byte("token={{secret:api-token}}\n"),
		}},
		Author: author(),
	}); err != nil {
		t.Fatalf("引用了存在的密钥应通过: %v", err)
	}
}

func TestWriteAssetRejectsPlaintextSecrets(t *testing.T) {
	w, _ := newTestWriter(t)
	cases := map[string]string{
		"私钥块":       "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n",
		"GitHub 令牌": "token=ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"连接串":       "url=redis://user:supersecret@host:6379",
		"内联字段":      `api_secret: "abcdefgh12345678"`,
	}
	for name, content := range cases {
		_, err := w.WriteAsset(WriteAssetInput{
			Bundle: "woa",
			Files:  []AssetFile{{Path: "demo/SKILL.md", Content: []byte(content)}},
			Author: author(),
		})
		if !errors.Is(err, ErrPlaintextSecret) {
			t.Fatalf("%s: 期望 ErrPlaintextSecret，实际 %v", name, err)
		}
	}
}

func TestWriteAssetRejectsDuplicateItemNames(t *testing.T) {
	w, _ := newTestWriter(t)
	raw := []byte(`name: woa
items:
  - kind: rule
    path: a.md
    name: same
    planes: [user]
  - kind: rule
    path: b.md
    name: same
    planes: [user]
`)
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle:   "woa",
		Files:    []AssetFile{{Path: "a.md", Content: []byte("a")}, {Path: "b.md", Content: []byte("b")}},
		Manifest: raw,
		Author:   author(),
	})
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("期望 ErrSchema，实际 %v", err)
	}
}

func TestWriteAssetRejectsCrossBundleNameConflict(t *testing.T) {
	w, _ := newTestWriter(t)
	// 两个 bundle 用同一个条目名，第二个必须被拒绝（需求 23.5）。
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1",
		Files:  []AssetFile{{Path: "a.md", Content: []byte("a")}},
		Manifest: []byte(`name: b1
items:
  - kind: rule
    path: a.md
    name: clash
    planes: [user]
`),
		Author: author(),
	}); err != nil {
		t.Fatalf("首次写入: %v", err)
	}
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b2",
		Files:  []AssetFile{{Path: "a.md", Content: []byte("a")}},
		Manifest: []byte(`name: b2
items:
  - kind: rule
    path: a.md
    name: clash
    planes: [user]
`),
		Author: author(),
	})
	if !errors.Is(err, ssot.ErrConflict) {
		t.Fatalf("期望 ssot.ErrConflict，实际 %v", err)
	}
}

func TestWriteAssetManifestMissingFile(t *testing.T) {
	w, _ := newTestWriter(t)
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  []AssetFile{{Path: "other.md", Content: []byte("x")}},
		Manifest: []byte(`name: woa
items:
  - kind: rule
    path: declared.md
    name: declared
    planes: [user]
`),
		Author: author(),
	})
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("期望 ErrSchema，实际 %v", err)
	}
}

func TestWriteAssetRejectsUnknownFields(t *testing.T) {
	w, _ := newTestWriter(t)
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  []AssetFile{{Path: "a.md", Content: []byte("a")}},
		Manifest: []byte(`name: woa
bogus_field: 1
items: []
`),
		Author: author(),
	})
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("未知字段应被拒绝，实际 %v", err)
	}
}

func TestWriteAssetBaseRevision(t *testing.T) {
	w, _ := newTestWriter(t)
	out, err := w.WriteAsset(WriteAssetInput{Bundle: "woa", Files: skillFiles(), Author: author()})
	if err != nil {
		t.Fatalf("首次写入: %v", err)
	}
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa", Files: skillFiles(), BaseRevision: "deadbeef", Author: author(),
	}); !errors.Is(err, ssot.ErrRevisionMismatch) {
		t.Fatalf("过期 revision 应被拒绝，实际 %v", err)
	}
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa", Files: skillFiles(), BaseRevision: out.Revision, Author: author(),
	}); err != nil {
		t.Fatalf("基于最新 revision 应通过: %v", err)
	}
}

func TestPutGetSecret(t *testing.T) {
	w, key := newTestWriter(t)
	rev, err := w.PutSecret(key, PutSecretInput{
		Name:   "api-token",
		Value:  "super-secret-value",
		Notes:  "测试用",
		Author: author(),
	})
	if err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	if rev == "" {
		t.Fatal("revision 为空")
	}

	// 磁盘上不得出现明文。
	raw, err := w.store.ReadFile(ssot.SecretFile("api-token"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "super-secret-value") {
		t.Fatal("密钥文件里出现了明文")
	}

	got, err := w.GetSecret(key, "api-token")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if string(got) != "super-secret-value" {
		t.Fatalf("明文不一致: %q", got)
	}

	metas, err := w.ListSecrets()
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(metas) != 1 || metas[0].Name != "api-token" || metas[0].Notes != "测试用" {
		t.Fatalf("元数据错误: %+v", metas)
	}
}

func TestPutSecretUpdateKeepsRefsAndDevices(t *testing.T) {
	w, key := newTestWriter(t)
	if _, err := w.PutSecret(key, PutSecretInput{
		Name: "k", Value: "v1", Refs: []proto.SecretRef{{Bundle: "woa", Item: "demo"}}, Author: author(),
	}); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	if err := w.RecordDownload(key, "k", "dev-1"); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	// 只改值，不传 refs。
	if _, err := w.PutSecret(key, PutSecretInput{Name: "k", Value: "v2", Author: author()}); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	metas, err := w.ListSecrets()
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(metas[0].Refs) != 1 {
		t.Fatalf("refs 应被保留: %+v", metas[0])
	}
	if len(metas[0].Devices) != 1 || metas[0].Devices[0] != "dev-1" {
		t.Fatalf("下发记录应被保留: %+v", metas[0].Devices)
	}
	got, err := w.GetSecret(key, "k")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if string(got) != "v2" {
		t.Fatalf("值未更新: %q", got)
	}
}

func TestDeleteSecretInUse(t *testing.T) {
	w, key := newTestWriter(t)
	if _, err := w.PutSecret(key, PutSecretInput{Name: "api-token", Value: "v", Author: author()}); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  []AssetFile{{Path: "demo/SKILL.md", Content: []byte("t={{secret:api-token}}\n")}},
		Author: author(),
	}); err != nil {
		t.Fatalf("WriteAsset: %v", err)
	}

	_, err := w.DeleteSecret(PutSecretInput{Name: "api-token", Author: author()}, false)
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("被引用的密钥删除应被拒绝，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "woa/") {
		t.Fatalf("错误信息应列出引用方: %v", err)
	}

	if _, err := w.DeleteSecret(PutSecretInput{Name: "api-token", Author: author()}, true); err != nil {
		t.Fatalf("force 删除应成功: %v", err)
	}
	if _, err := w.GetSecret(key, "api-token"); !errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("删除后应读不到，实际 %v", err)
	}
}

func TestDeleteSecretMissing(t *testing.T) {
	w, _ := newTestWriter(t)
	if _, err := w.DeleteSecret(PutSecretInput{Name: "absent", Author: author()}, false); !errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
}

func TestRekeyRotatesAllSecrets(t *testing.T) {
	w, oldKey := newTestWriter(t)
	for _, n := range []string{"a", "b", "c"} {
		if _, err := w.PutSecret(oldKey, PutSecretInput{Name: n, Value: "v-" + n, Author: author()}); err != nil {
			t.Fatalf("PutSecret: %v", err)
		}
	}
	newKey, err := crypt.NewKey(nil)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	count, err := w.Rekey(oldKey, newKey, ssot.Author{Name: "admin", Source: "admin"})
	if err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if count != 3 {
		t.Fatalf("应重新加密 3 条，实际 %d", count)
	}
	for _, n := range []string{"a", "b", "c"} {
		got, err := w.GetSecret(newKey, n)
		if err != nil {
			t.Fatalf("新密钥解密 %s 失败: %v", n, err)
		}
		if string(got) != "v-"+n {
			t.Fatalf("明文错误: %q", got)
		}
		if _, err := w.GetSecret(oldKey, n); err == nil {
			t.Fatalf("旧密钥不应再能解密 %s", n)
		}
	}
}

func TestExtractSecretRefs(t *testing.T) {
	content := []byte("a={{secret:one}} b={{ secret:two }} c={{secret:one}} d={{notsecret:x}}")
	got := ExtractSecretRefs(content)
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("引用解析错误: %v", got)
	}
	if refs := ExtractSecretRefs([]byte("no refs here")); refs != nil {
		t.Fatalf("无引用应返回 nil: %v", refs)
	}
}

func TestSecretsDeliveredTo(t *testing.T) {
	w, key := newTestWriter(t)
	for _, n := range []string{"x", "y"} {
		if _, err := w.PutSecret(key, PutSecretInput{Name: n, Value: "v", Author: author()}); err != nil {
			t.Fatalf("PutSecret: %v", err)
		}
	}
	if err := w.RecordDownload(key, "x", "dev-1"); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	if err := w.RecordDownload(key, "y", "dev-2"); err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
	got, err := w.SecretsDeliveredTo("dev-1")
	if err != nil {
		t.Fatalf("SecretsDeliveredTo: %v", err)
	}
	if len(got) != 1 || got[0] != "x" {
		t.Fatalf("结果错误: %v", got)
	}
}

func TestDeleteAsset(t *testing.T) {
	w, _ := newTestWriter(t)
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files: []AssetFile{
			{Path: "a.md", Content: []byte("a")},
			{Path: "b.md", Content: []byte("b")},
		},
		Manifest: []byte(`name: woa
items:
  - kind: rule
    path: a.md
    name: a
    planes: [user]
  - kind: rule
    path: b.md
    name: b
    planes: [user]
`),
		Author: author(),
	}); err != nil {
		t.Fatalf("WriteAsset: %v", err)
	}

	out, err := w.DeleteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  []AssetFile{{Path: "b.md", Content: []byte("b")}},
		Manifest: []byte(`name: woa
items:
  - kind: rule
    path: b.md
    name: b
    planes: [user]
`),
		Author: author(),
	}, "a.md")
	if err != nil {
		t.Fatalf("DeleteAsset: %v", err)
	}
	if len(out.Manifest.Items) != 1 || out.Manifest.Items[0].Name != "b" {
		t.Fatalf("删除后 manifest 错误: %+v", out.Manifest)
	}
	files, err := w.ReadBundleFiles("woa")
	if err != nil {
		t.Fatalf("ReadBundleFiles: %v", err)
	}
	for _, f := range files {
		if f.Path == "a.md" {
			t.Fatal("a.md 应已删除")
		}
	}
}
