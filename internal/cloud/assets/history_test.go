package assets

import (
	"context"
	"errors"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

func TestHistoryOnlyIncludesTouchingCommits(t *testing.T) {
	w, _ := newTestWriter(t)
	// 改动 bundle b1 一次。
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1",
		Files:  []AssetFile{{Path: "a.md", Content: []byte("a1")}},
		Manifest: []byte(`name: b1
items:
  - kind: rule
    path: a.md
    name: a
    planes: [user]
`),
		Author: author(),
	}); err != nil {
		t.Fatalf("写入 b1: %v", err)
	}
	// 改动另一个 bundle，不应出现在 b1 的历史里。
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b2",
		Files:  []AssetFile{{Path: "b.md", Content: []byte("b1")}},
		Manifest: []byte(`name: b2
items:
  - kind: rule
    path: b.md
    name: b
    planes: [user]
`),
		Author: author(),
	}); err != nil {
		t.Fatalf("写入 b2: %v", err)
	}
	// 再改一次 b1。
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1",
		Files:  []AssetFile{{Path: "a.md", Content: []byte("a2")}},
		Manifest: []byte(`name: b1
items:
  - kind: rule
    path: a.md
    name: a
    planes: [user]
`),
		Author: author(),
	}); err != nil {
		t.Fatalf("再次写入 b1: %v", err)
	}

	hist, err := w.History(context.Background(), HistoryAsset, "b1", 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("b1 应有 2 条历史，实际 %d: %+v", len(hist), hist)
	}
	if hist[0].Source != "mcp" {
		t.Fatalf("来源错误: %q", hist[0].Source)
	}
	if hist[0].CreatedAt == "" {
		t.Fatal("时间未填充")
	}
}

func TestHistoryLimit(t *testing.T) {
	w, _ := newTestWriter(t)
	for i := 0; i < 5; i++ {
		if _, err := w.WriteAsset(WriteAssetInput{
			Bundle:   "b1",
			Files:    []AssetFile{{Path: "a.md", Content: []byte{byte('0' + i)}}},
			Manifest: []byte("name: b1\nitems:\n  - kind: rule\n    path: a.md\n    name: a\n    planes: [user]\n"),
			Author:   author(),
		}); err != nil {
			t.Fatalf("写入: %v", err)
		}
	}
	hist, err := w.History(context.Background(), HistoryAsset, "b1", 2)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("上限应为 2，实际 %d", len(hist))
	}
}

func TestHistoryRejectsBadID(t *testing.T) {
	w, _ := newTestWriter(t)
	if _, err := w.History(context.Background(), HistoryAsset, "Bad Name", 0); err == nil {
		t.Fatal("非法 bundle 名应被拒绝")
	}
	if _, err := w.History(context.Background(), HistoryKind("nope"), "x", 0); err == nil {
		t.Fatal("非法类型应被拒绝")
	}
}

func TestRollbackAsset(t *testing.T) {
	w, _ := newTestWriter(t)
	manifestV1 := []byte(`name: b1
items:
  - kind: rule
    path: a.md
    name: a
    planes: [user]
`)
	v1, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1", Files: []AssetFile{{Path: "a.md", Content: []byte("v1")}},
		Manifest: manifestV1, Author: author(),
	})
	if err != nil {
		t.Fatalf("v1: %v", err)
	}

	v2, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1", Files: []AssetFile{{Path: "a.md", Content: []byte("v2")}},
		Manifest: manifestV1, Author: author(),
	})
	if err != nil {
		t.Fatalf("v2: %v", err)
	}
	if v2.Revision == v1.Revision {
		t.Fatal("两次写入应产生不同 revision")
	}

	out, err := w.RollbackAsset(RollbackInput{
		Kind: HistoryAsset, ID: "b1", Commit: v1.Revision, Author: author(),
	})
	if err != nil {
		t.Fatalf("RollbackAsset: %v", err)
	}
	if out.Revision == v2.Revision {
		t.Fatal("回滚应产生新 revision")
	}

	files, err := w.ReadBundleFiles("b1")
	if err != nil {
		t.Fatalf("ReadBundleFiles: %v", err)
	}
	for _, f := range files {
		if f.Path == "a.md" && string(f.Content) != "v1" {
			t.Fatalf("回滚后内容应为 v1，实际 %q", f.Content)
		}
	}

	// 回滚不改写历史：历史条数还在增加。
	hist, err := w.History(context.Background(), HistoryAsset, "b1", 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 3 {
		t.Fatalf("回滚应新增一条历史，实际 %d", len(hist))
	}
}

func TestRollbackAssetMissingCommit(t *testing.T) {
	w, _ := newTestWriter(t)
	_, err := w.RollbackAsset(RollbackInput{
		Kind: HistoryAsset, ID: "b1", Commit: "0000000000000000000000000000000000000000", Author: author(),
	})
	if !errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
}

func TestRollbackAssetDetectsConflict(t *testing.T) {
	w, _ := newTestWriter(t)
	// v1：b1 里条目名 clash。
	v1, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1",
		Files:  []AssetFile{{Path: "a.md", Content: []byte("v1")}},
		Manifest: []byte(`name: b1
items:
  - kind: rule
    path: a.md
    name: clash
    planes: [user]
`),
		Author: author(),
	})
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	// v2：b1 换成别的名字，把 clash 让出来。
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1",
		Files:  []AssetFile{{Path: "a.md", Content: []byte("v2")}},
		Manifest: []byte(`name: b1
items:
  - kind: rule
    path: a.md
    name: renamed
    planes: [user]
`),
		Author: author(),
	}); err != nil {
		t.Fatalf("v2: %v", err)
	}
	// 别的 bundle 占用了 clash。
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b2",
		Files:  []AssetFile{{Path: "b.md", Content: []byte("b")}},
		Manifest: []byte(`name: b2
items:
  - kind: rule
    path: b.md
    name: clash
    planes: [user]
`),
		Author: author(),
	}); err != nil {
		t.Fatalf("b2: %v", err)
	}
	// 回滚 b1 到 v1 会重新占用 clash，必须被拒绝。
	if _, err := w.RollbackAsset(RollbackInput{
		Kind: HistoryAsset, ID: "b1", Commit: v1.Revision, Author: author(),
	}); !errors.Is(err, ssot.ErrConflict) {
		t.Fatalf("期望 ErrConflict，实际 %v", err)
	}
}

func TestDiffAsset(t *testing.T) {
	w, _ := newTestWriter(t)
	manifest := []byte(`name: b1
items:
  - kind: rule
    path: a.md
    name: a
    planes: [user]
  - kind: rule
    path: b.md
    name: b
    planes: [user]
`)
	v1, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1",
		Files: []AssetFile{
			{Path: "a.md", Content: []byte("a1")},
			{Path: "b.md", Content: []byte("b1")},
		},
		Manifest: manifest, Author: author(),
	})
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "b1",
		Files: []AssetFile{
			{Path: "a.md", Content: []byte("a2")},
		},
		Manifest: []byte(`name: b1
items:
  - kind: rule
    path: a.md
    name: a
    planes: [user]
`),
		Author: author(),
	}); err != nil {
		t.Fatalf("v2: %v", err)
	}

	diff, err := w.DiffAsset("b1", v1.Revision)
	if err != nil {
		t.Fatalf("DiffAsset: %v", err)
	}
	if len(diff.Added) != 1 || diff.Added[0] != "b.md" {
		t.Fatalf("应重新新增 b.md: %+v", diff)
	}
	if len(diff.Changed) != 1 || diff.Changed[0] != "a.md" {
		t.Fatalf("a.md 应被标记为改动: %+v", diff)
	}
	if len(diff.Removed) != 0 {
		t.Fatalf("不应有删除: %+v", diff)
	}
}

func TestRollbackSecret(t *testing.T) {
	w, key := newTestWriter(t)
	v1, err := w.PutSecret(key, PutSecretInput{Name: "api", Value: "v1", Author: author()})
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	if _, err := w.PutSecret(key, PutSecretInput{Name: "api", Value: "v2", Author: author()}); err != nil {
		t.Fatalf("v2: %v", err)
	}

	if err := w.VerifySecretRollback(key, "api", v1); err != nil {
		t.Fatalf("VerifySecretRollback: %v", err)
	}

	rev, err := w.RollbackSecret(RollbackInput{Kind: HistorySecret, ID: "api", Commit: v1, Author: author()})
	if err != nil {
		t.Fatalf("RollbackSecret: %v", err)
	}
	if rev == "" {
		t.Fatal("revision 为空")
	}
	got, err := w.GetSecret(key, "api")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if string(got) != "v1" {
		t.Fatalf("回滚后应为 v1，实际 %q", got)
	}
}

func TestVerifySecretRollbackAfterRotation(t *testing.T) {
	w, oldKey := newTestWriter(t)
	rev, err := w.PutSecret(oldKey, PutSecretInput{Name: "api", Value: "v1", Author: author()})
	if err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	newKey, err := crypt.NewKey(nil)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	// 轮换后旧密文无法用新密钥解开，回滚到旧提交应被提前拒绝。
	if err := w.VerifySecretRollback(newKey, "api", rev); err == nil {
		t.Fatal("轮换后回滚到旧密文应被拒绝")
	}
}

func TestRollbackDispatch(t *testing.T) {
	w, _ := newTestWriter(t)
	if err := w.Rollback(RollbackInput{Kind: HistoryKind("nope"), ID: "x", Commit: "abc", Author: author()}); err == nil {
		t.Fatal("非法类型应报错")
	}
	if err := w.Rollback(RollbackInput{Kind: HistoryAsset, ID: "b1", Commit: "0000000000000000000000000000000000000000", Author: author()}); err == nil {
		t.Fatal("不存在的提交应报错")
	}
}

func TestSecretMetaRefsRoundTrip(t *testing.T) {
	w, key := newTestWriter(t)
	refs := []proto.SecretRef{{Bundle: "b1", Item: "a"}, {Bundle: "b1", Item: "a"}}
	if _, err := w.PutSecret(key, PutSecretInput{Name: "api", Value: "v", Refs: refs, Author: author()}); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	metas, err := w.ListSecrets()
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(metas[0].Refs) != 1 {
		t.Fatalf("refs 应去重: %+v", metas[0].Refs)
	}
}
