package assets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 历史与回滚（需求 14.1、1.4）。
//
// 回滚不改写历史：它把目标 revision 的内容以一个新 commit 写回去。
// 因此任何一次误回滚都能再被回滚，历史永远是只追加的。

// HistoryKind 是可查询的历史对象类型。
type HistoryKind string

const (
	// HistoryAsset 按 bundle 查资产历史。
	HistoryAsset HistoryKind = "asset"
	// HistorySecret 按密钥名查密钥历史。
	HistorySecret HistoryKind = "secret"
)

// Valid 报告类型是否受支持。
func (k HistoryKind) Valid() bool {
	return k == HistoryAsset || k == HistorySecret
}

// History 返回某个对象的历史，只包含真正改动过它的提交。
//
// 默认上限 50 条，避免一次拉出整个仓库历史。
func (w *Writer) History(ctx context.Context, kind HistoryKind, id string, limit int) ([]proto.Revision, error) {
	switch kind {
	case HistoryAsset:
		if err := ssot.ValidateBundle(id); err != nil {
			return nil, err
		}
	case HistorySecret:
		if err := ssot.ValidateItemName(id); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: 历史类型 %q 不受支持", ErrSchema, kind)
	}
	if limit <= 0 {
		limit = 50
	}

	prefix := historyPrefix(kind, id)
	commits, err := w.store.History(ctx, 0)
	if err != nil {
		return nil, err
	}

	out := make([]proto.Revision, 0, limit)
	for _, c := range commits {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		touched, err := w.commitTouched(c.Hash, prefix)
		if err != nil {
			return nil, err
		}
		if !touched {
			continue
		}
		out = append(out, proto.Revision{
			Commit:    c.Hash,
			Message:   c.Message,
			Author:    c.Author,
			Source:    proto.Source(c.Source),
			CreatedAt: c.When.UTC().Format(time.RFC3339),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func historyPrefix(kind HistoryKind, id string) string {
	if kind == HistoryAsset {
		return ssot.AssetDir(id) + "/"
	}
	return ssot.SecretFile(id)
}

// commitTouched 报告某个提交是否改动过以 prefix 开头的路径。
func (w *Writer) commitTouched(commit, prefix string) (bool, error) {
	paths, err := w.store.CommitPaths(commit)
	if err != nil {
		return false, err
	}
	for _, p := range paths {
		if p == prefix || strings.HasPrefix(p, prefix) {
			return true, nil
		}
	}
	return false, nil
}

// RollbackAsset 把某个 bundle 回滚到历史 revision。
//
// 回滚后该 bundle 的内容与目标提交时完全一致，包括 manifest。
func (w *Writer) RollbackAsset(in RollbackInput) (*WriteAssetOutput, error) {
	if err := ssot.ValidateBundle(in.ID); err != nil {
		return nil, err
	}
	target, err := w.loadBundleAt(in.ID, in.Commit)
	if err != nil {
		return nil, err
	}

	revision, err := w.store.Update(in.Author, func(tx *ssot.Tx) (string, error) {
		if err := tx.EnsureRevision(in.BaseRevision); err != nil {
			return "", err
		}
		// 回滚同样要做命名冲突检测：目标历史的条目名可能已被别的 bundle 占用。
		if err := checkEntryConflicts(tx, in.ID, target.manifest); err != nil {
			return "", err
		}
		if err := replaceBundle(tx, in.ID, target.files, target.manifest); err != nil {
			return "", err
		}
		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("回滚资产 %s 到 %s", in.ID, shortHash(in.Commit))
		}
		return msg, nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteAssetOutput{Bundle: in.ID, Revision: revision, Manifest: target.manifest}, nil
}

// RollbackSecret 把某条密钥回滚到历史 revision。
//
// 回滚的是密文而不是明文，所以历史里用旧根密钥加密的密文在轮换后仍能解开，
// 前提是旧根密钥还在冷备里（需求 9.3）。
func (w *Writer) RollbackSecret(in RollbackInput) (string, error) {
	if err := ssot.ValidateItemName(in.ID); err != nil {
		return "", err
	}
	rel := ssot.SecretFile(in.ID)
	data, err := w.store.FileAt(in.Commit, rel)
	if err != nil {
		return "", err
	}
	var f secretFile
	if err := json.Unmarshal(data, &f); err != nil {
		return "", fmt.Errorf("%w: 目标提交里 %s 不是合法密钥文件", ssot.ErrNotFound, rel)
	}
	if f.Sealed == nil {
		return "", fmt.Errorf("%w: 目标提交里 %s 没有密文", ssot.ErrNotFound, rel)
	}
	// 历史提交里可能还没有这个文件，也可能名字对不上。
	if f.Name == "" {
		f.Name = in.ID
	}

	return w.store.Update(in.Author, func(tx *ssot.Tx) (string, error) {
		if err := tx.EnsureRevision(in.BaseRevision); err != nil {
			return "", err
		}
		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("回滚密钥 %s 到 %s", in.ID, shortHash(in.Commit))
		}
		return msg, tx.WriteJSON(rel, f)
	})
}

// RollbackInput 是一次回滚请求。
type RollbackInput struct {
	Kind         HistoryKind
	ID           string
	Commit       string
	BaseRevision string
	Message      string
	Author       ssot.Author
}

// Rollback 按类型分派。
func (w *Writer) Rollback(in RollbackInput) error {
	switch in.Kind {
	case HistoryAsset:
		_, err := w.RollbackAsset(in)
		return err
	case HistorySecret:
		_, err := w.RollbackSecret(in)
		return err
	}
	return fmt.Errorf("%w: 回滚类型 %q 不受支持", ErrSchema, in.Kind)
}

type bundleSnapshot struct {
	files    []AssetFile
	manifest ssot.BundleManifest
}

// loadBundleAt 读出某个提交时某个 bundle 的全部内容。
func (w *Writer) loadBundleAt(bundle, commit string) (*bundleSnapshot, error) {
	paths, err := w.store.CommitPaths(commit)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, fmt.Errorf("%w: 提交 %s", ssot.ErrNotFound, commit)
		}
		return nil, err
	}
	prefix := ssot.AssetDir(bundle) + "/"
	var files []AssetFile
	for _, p := range paths {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rel := strings.TrimPrefix(p, prefix)
		if rel == "" || strings.HasSuffix(rel, "/") {
			continue
		}
		data, err := w.store.FileAt(commit, p)
		if err != nil {
			return nil, err
		}
		files = append(files, AssetFile{Path: rel, Content: data})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: 提交 %s 中不存在 bundle %q", ssot.ErrNotFound, shortHash(commit), bundle)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	// manifest 必须能从目标提交里读出，否则回滚出来的内容不可渲染。
	var manifest ssot.BundleManifest
	var manifestData []byte
	for _, f := range files {
		if f.Path == ssot.FileManifest {
			manifestData = f.Content
			break
		}
	}
	if manifestData == nil {
		return nil, fmt.Errorf("%w: 提交 %s 中 bundle %q 缺少 manifest", ssot.ErrNotFound, shortHash(commit), bundle)
	}
	if err := unmarshalManifest(manifestData, &manifest); err != nil {
		return nil, err
	}
	if manifest.Name == "" {
		manifest.Name = bundle
	}
	// 校验目标快照自身是合法的，避免把一个坏快照写回当前版本。
	if err := validateManifest(manifest, files); err != nil {
		return nil, fmt.Errorf("目标快照不合法: %w", err)
	}
	return &bundleSnapshot{files: files, manifest: manifest}, nil
}

// CurrentRevision 返回当前 revision，供管理面做乐观并发的基准。
func (w *Writer) CurrentRevision() string { return w.store.Head() }

// AssetDiff 描述一个 bundle 的当前内容与目标提交的差异，回滚前给用户看。
type AssetDiff struct {
	Bundle  string
	Added   []string
	Removed []string
	Changed []string
}

// DiffAsset 比较当前内容与目标提交。
func (w *Writer) DiffAsset(bundle, commit string) (*AssetDiff, error) {
	if err := ssot.ValidateBundle(bundle); err != nil {
		return nil, err
	}
	target, err := w.loadBundleAt(bundle, commit)
	if err != nil {
		return nil, err
	}
	current, err := w.ReadBundleFiles(bundle)
	if err != nil && !errors.Is(err, ssot.ErrNotFound) {
		return nil, err
	}
	targetMap := map[string]string{}
	for _, f := range target.files {
		if f.Path == ssot.FileManifest {
			continue
		}
		targetMap[f.Path] = ssot.SumHex(f.Content)
	}
	currentMap := map[string]string{}
	for _, f := range current {
		if f.Path == ssot.FileManifest {
			continue
		}
		currentMap[f.Path] = ssot.SumHex(f.Content)
	}

	diff := &AssetDiff{Bundle: bundle}
	for p, h := range targetMap {
		cur, ok := currentMap[p]
		if !ok {
			diff.Added = append(diff.Added, p)
			continue
		}
		if cur != h {
			diff.Changed = append(diff.Changed, p)
		}
	}
	for p := range currentMap {
		if _, ok := targetMap[p]; !ok {
			diff.Removed = append(diff.Removed, p)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Strings(diff.Changed)
	return diff, nil
}

// VerifySecretRollback 在回滚前确认目标密文能被当前根密钥解开。
//
// 如果解不开（多半是轮换根密钥后回了更早的提交），回滚会产生一条谁都读不了的密钥，
// 所以提前拒绝，而不是等到渲染时才发现。
func (w *Writer) VerifySecretRollback(key *crypt.Key, name, commit string) error {
	data, err := w.store.FileAt(commit, ssot.SecretFile(name))
	if err != nil {
		return err
	}
	var f secretFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("%w: 目标提交里密钥文件非法", ssot.ErrNotFound)
	}
	if f.Sealed == nil {
		return fmt.Errorf("%w: 目标提交里没有密文", ssot.ErrNotFound)
	}
	pt, err := key.Open(f.Sealed)
	if err != nil {
		return fmt.Errorf("目标密文无法用当前根密钥解开（key id %s）：%w", f.Sealed.KeyID, err)
	}
	for i := range pt {
		pt[i] = 0
	}
	return nil
}

// shortHash 把提交哈希缩到 8 位，用于 commit message。
func shortHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return h[:8]
}

// unmarshalManifest 用与写入一致的规则解析 manifest。
func unmarshalManifest(data []byte, out *ssot.BundleManifest) error {
	if err := yamlUnmarshalStrict(data, out); err != nil {
		return fmt.Errorf("%w: manifest 解析失败: %v", ErrSchema, err)
	}
	return nil
}

// bundlePath 便于测试拼接。
func bundlePath(bundle string) string { return path.Join(ssot.DirAssets, bundle) }
