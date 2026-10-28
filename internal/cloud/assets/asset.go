// Package assets 实现资产与密钥的同步写入链路。
//
// 写入是同步的（需求 7.1）：一次请求内完成校验、提交、触发下发。
// 没有队列、没有草稿、没有提交状态机，因为这里唯一的写入者就是用户自己，
// 而所有写入都要么完整生效，要么整体失败。
//
// 校验顺序固定：schema → 命名 → 路径 → 密钥引用完整性 → 疑似明文扫描。
// 任何一步失败都不提交，事务回滚。
package assets

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shichao402/Dec/internal/cloud/ssot"
)

var (
	// ErrSchema 表示 manifest 结构不合规。
	ErrSchema = errors.New("schema invalid")
	// ErrMissingSecret 表示资产引用了不存在的密钥。
	ErrMissingSecret = errors.New("missing secret")
	// ErrPlaintextSecret 表示资产内容里出现了疑似明文密钥。
	ErrPlaintextSecret = errors.New("suspected plaintext secret")
	// ErrInUse 表示密钥仍被引用，不能删除。
	ErrInUse = errors.New("secret in use")
)

// Writer 是资产与密钥写入的执行者。
type Writer struct {
	store *ssot.Store
}

// NewWriter 构造写入器。
func NewWriter(store *ssot.Store) *Writer { return &Writer{store: store} }

// WriteAssetInput 是一次资产写入请求。
type WriteAssetInput struct {
	Bundle string
	// Files 是这条 bundle 下的全部文件，路径相对 assets/<bundle>/。
	// 这是整体替换语义：未出现在 Files 里的旧文件会被删除。
	Files []AssetFile
	// Manifest 是调用方给出的 manifest。为空时由服务端按 Files 推断。
	Manifest []byte
	// BaseRevision 用于乐观并发控制。
	BaseRevision string
	Message      string
	Author       ssot.Author
}

// DeleteAssetInput 是一次资产条目删除请求。
//
// 与 WriteAssetInput 的区别是它不接收文件内容：删除只需要路径，
// 剩余内容由服务端从当前 SSOT 读出，调用方无法因为少传而误删别的条目。
type DeleteAssetInput struct {
	Bundle string
	// Path 是要删除的条目路径，必须与 manifest 里的 path 一致。
	Path string
	// BaseRevision 用于乐观并发控制。
	BaseRevision string
	Message      string
	Author       ssot.Author
}

// DeleteBundleInput 是删除整个 bundle 的输入。
type DeleteBundleInput struct {
	Bundle       string
	BaseRevision string
	Message      string
	Author       ssot.Author
}

// AssetFile 是一个待写入的资产文件。
type AssetFile struct {
	Path       string
	Content    []byte
	Executable bool
}

// WriteAssetOutput 是写入结果。
type WriteAssetOutput struct {
	Bundle   string
	Revision string
	Commit   string
	Manifest ssot.BundleManifest
}

// WriteAsset 同步写入一个 bundle。
//
// 语义是“整体替换”：写入后这一 bundle 的目录内容与输入完全一致，
// 不在输入里的旧文件会被删除。这样调用方不需要关心增量，也不会留下孤儿文件。
func (w *Writer) WriteAsset(in WriteAssetInput) (*WriteAssetOutput, error) {
	if err := ssot.ValidateBundle(in.Bundle); err != nil {
		return nil, err
	}
	files, err := normalizeFiles(in.Files)
	if err != nil {
		return nil, err
	}
	manifest, err := parseManifest(in.Bundle, in.Manifest, files)
	if err != nil {
		return nil, err
	}
	if err := validateManifest(manifest, files); err != nil {
		return nil, err
	}
	if err := scanForPlaintextSecrets(files); err != nil {
		return nil, err
	}

	// 引用完整性校验需要读到当前所有密钥名，因此在写锁外先取一份快照，
	// 然后在事务里再用同一份快照复核，避免与并发写密钥交错。
	secretNames, err := w.SecretNames()
	if err != nil {
		return nil, err
	}
	if err := checkSecretRefs(files, secretNames); err != nil {
		return nil, err
	}

	var commit string
	revision, err := w.store.Update(in.Author, func(tx *ssot.Tx) (string, error) {
		if err := tx.EnsureRevision(in.BaseRevision); err != nil {
			return "", err
		}
		// 事务内复核密钥引用，防止在快照与提交之间有人删了密钥。
		live, err := secretNamesTx(tx)
		if err != nil {
			return "", err
		}
		if err := checkSecretRefs(files, live); err != nil {
			return "", err
		}
		// 冲突校验：与其它 bundle 的同名托管条目。
		if err := checkEntryConflicts(tx, in.Bundle, manifest); err != nil {
			return "", err
		}
		if err := replaceBundle(tx, in.Bundle, files, manifest); err != nil {
			return "", err
		}
		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("写入资产 %s", in.Bundle)
		}
		commit = ""
		return msg, nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteAssetOutput{Bundle: in.Bundle, Revision: revision, Commit: commit, Manifest: manifest}, nil
}

// DeleteAsset 从 bundle 中移除一个条目。
//
// 与 WriteAsset 的整包替换不同，这里是精确删除：事务内读出当前 manifest，
// 摘掉 Path 匹配的那一条，删掉它名下的文件，其余条目与文件原样保留。
// 传递性上也不存在「少给了内容就误删」的可能，因为调用方本来就不传内容。
//
// 条目不存在时返回 ssot.ErrNotFound，而不是静默提交一个空改动：
// 静默成功会让「删错名字」看起来像「删成功了」。
func (w *Writer) DeleteAsset(in DeleteAssetInput) (*WriteAssetOutput, error) {
	if err := ssot.ValidateBundle(in.Bundle); err != nil {
		return nil, err
	}
	itemPath := strings.TrimSpace(in.Path)
	if itemPath == "" {
		return nil, fmt.Errorf("%w: 条目路径不能为空", ErrSchema)
	}
	if err := ssot.SafeRelPath(itemPath); err != nil {
		return nil, err
	}

	var (
		commit   string
		manifest ssot.BundleManifest
	)
	revision, err := w.store.Update(in.Author, func(tx *ssot.Tx) (string, error) {
		if err := tx.EnsureRevision(in.BaseRevision); err != nil {
			return "", err
		}
		dir := ssot.AssetDir(in.Bundle)
		current, err := ssot.ReadYAML[ssot.BundleManifest](tx.Store(), path.Join(dir, ssot.FileManifest))
		if err != nil {
			return "", err
		}

		// 找出目标条目；同时确认它确实存在于当前 manifest 里。
		var target *ssot.ManifestItem
		kept := make([]ssot.ManifestItem, 0, len(current.Items))
		for i := range current.Items {
			if current.Items[i].Path == itemPath {
				target = &current.Items[i]
				continue
			}
			kept = append(kept, current.Items[i])
		}
		if target == nil {
			return "", fmt.Errorf("%w: bundle %q 里没有条目 %q",
				ssot.ErrNotFound, in.Bundle, itemPath)
		}

		// 删文件：skill 条目删整个目录，其余删单文件。
		// 只删这个条目自己名下的路径，不碰其它条目。
		if target.Kind == ssot.KindSkill {
			if err := tx.DeleteDir(path.Join(dir, target.Path)); err != nil {
				return "", err
			}
		} else {
			if err := tx.DeleteFile(path.Join(dir, target.Path)); err != nil {
				return "", err
			}
		}

		// 写回摘掉该条目后的 manifest。
		manifest = current
		manifest.Items = kept
		manifestBytes, err := yaml.Marshal(manifest)
		if err != nil {
			return "", fmt.Errorf("序列化 manifest 失败: %w", err)
		}
		if err := tx.WriteFile(path.Join(dir, ssot.FileManifest), manifestBytes, 0o600); err != nil {
			return "", err
		}

		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("删除资产 %s/%s", in.Bundle, itemPath)
		}
		commit = ""
		return msg, nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteAssetOutput{Bundle: in.Bundle, Revision: revision, Commit: commit, Manifest: manifest}, nil
}

// ErrEditMismatch 表示 old_string 在文件里不是恰好出现一次。
//
// 单独成一个错误而不是混进 ErrSchema：调用方（通常是 AI）拿到它应该
// 重新读文件、给出更长更唯一的上下文，而不是去怀疑 manifest 写错了。
var ErrEditMismatch = errors.New("edit target mismatch")

// EditFileInput 是一次单文件编辑请求。
type EditFileInput struct {
	Bundle string
	// Path 是相对 bundle 根的文件路径，例如 skills/foo/SKILL.md。
	Path string
	// OldString 必须在文件里恰好出现一次。
	OldString string
	// NewString 是替换后的文本，可以为空（表示删掉这段）。
	NewString string
	// BaseRevision 用于乐观并发控制，空表示允许覆盖。
	BaseRevision string
	Message      string
	Author       ssot.Author
}

// EditFileOutput 是编辑结果。
type EditFileOutput struct {
	Bundle   string
	Path     string
	Revision string
}

// EditFile 修改 bundle 里一个已存在文件的一段文本（ADR 0053）。
//
// 它存在的理由是把「读全量 → 改一处 → 整包写回」这件容易出错的事固化在
// 服务端：调用方只说改哪个文件、哪段换成什么，不传其余文件，也不传
// manifest，所以不可能因为少传一个文件而删掉它，也不可能因为 manifest
// 回传错了而悄悄改掉下发范围。
//
// 刻意不支持的事：新增文件、改 manifest.yaml、按正则或多处替换。这些都是
// 结构性改动，仍然走 WriteAsset，由调用方显式给出全部内容。
func (w *Writer) EditFile(in EditFileInput) (*EditFileOutput, error) {
	if err := ssot.ValidateBundle(in.Bundle); err != nil {
		return nil, err
	}
	rel := strings.TrimSpace(in.Path)
	if rel == "" {
		return nil, fmt.Errorf("%w: path 不能为空", ErrSchema)
	}
	if err := ssot.SafeRelPath(rel); err != nil {
		return nil, err
	}
	if rel == ssot.FileManifest {
		return nil, fmt.Errorf("%w: manifest.yaml 是结构声明，不能用单文件编辑修改；请用 dec_write_asset 整体写入", ErrSchema)
	}
	if in.OldString == "" {
		return nil, fmt.Errorf("%w: old_string 不能为空；要整体替换文件请用 dec_write_asset", ErrEditMismatch)
	}
	if in.OldString == in.NewString {
		return nil, fmt.Errorf("%w: old_string 与 new_string 相同，没有任何改动", ErrEditMismatch)
	}

	revision, err := w.store.Update(in.Author, func(tx *ssot.Tx) (string, error) {
		if err := tx.EnsureRevision(in.BaseRevision); err != nil {
			return "", err
		}
		dir := ssot.AssetDir(in.Bundle)
		manifest, err := ssot.ReadYAML[ssot.BundleManifest](tx.Store(), path.Join(dir, ssot.FileManifest))
		if err != nil {
			if errors.Is(err, ssot.ErrNotFound) {
				return "", fmt.Errorf("%w: bundle %q 不存在", ssot.ErrNotFound, in.Bundle)
			}
			return "", err
		}
		if !ownedByItem(manifest, rel) {
			return "", fmt.Errorf("%w: bundle %q 里没有文件 %q（新增文件请用 dec_write_asset）",
				ssot.ErrNotFound, in.Bundle, rel)
		}
		full := path.Join(dir, rel)
		old, err := tx.ReadFile(full)
		if err != nil {
			if errors.Is(err, ssot.ErrNotFound) {
				return "", fmt.Errorf("%w: bundle %q 里没有文件 %q（新增文件请用 dec_write_asset）",
					ssot.ErrNotFound, in.Bundle, rel)
			}
			return "", err
		}
		switch n := bytes.Count(old, []byte(in.OldString)); n {
		case 1:
		case 0:
			return "", fmt.Errorf("%w: old_string 在 %s 里没有出现；请先用 dec_read_asset 读出当前内容，逐字复制要改的那段", ErrEditMismatch, rel)
		default:
			return "", fmt.Errorf("%w: old_string 在 %s 里出现了 %d 次；请带上更多上下文让它只匹配一处", ErrEditMismatch, rel, n)
		}
		updated := bytes.Replace(old, []byte(in.OldString), []byte(in.NewString), 1)

		// 与 WriteAsset 相同的内容校验：不允许写进明文密钥，引用的密钥必须存在。
		files := []AssetFile{{Path: rel, Content: updated}}
		if err := scanForPlaintextSecrets(files); err != nil {
			return "", err
		}
		live, err := secretNamesTx(tx)
		if err != nil {
			return "", err
		}
		if err := checkSecretRefs(files, live); err != nil {
			return "", err
		}

		// 保留原来的可执行位：编辑一个脚本不应该顺手把它降级成普通文件。
		mode := fs.FileMode(fs0600)
		if isExecutable(tx.Store().Dir(), full) {
			mode = 0o700
		}
		if err := tx.WriteFile(full, updated, mode); err != nil {
			return "", err
		}
		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("编辑资产 %s/%s", in.Bundle, rel)
		}
		return msg, nil
	})
	if err != nil {
		return nil, err
	}
	return &EditFileOutput{Bundle: in.Bundle, Path: rel, Revision: revision}, nil
}

// ownedByItem 报告文件是否属于 manifest 里的某个条目。
//
// 只允许编辑条目名下的文件：bundle 目录里不属于任何条目的文件不会被下发，
// 编辑它等于做了一次看不到效果的修改，与「改了没生效」是同一类坑。
func ownedByItem(m ssot.BundleManifest, rel string) bool {
	for _, item := range m.Items {
		if item.Kind == ssot.KindSkill {
			if strings.HasPrefix(rel, item.Path+"/") {
				return true
			}
			continue
		}
		if rel == item.Path {
			return true
		}
	}
	return false
}

// DeleteBundle 删除整个 bundle。
//
// 与 DeleteAsset 的差别是范围：DeleteAsset 摘掉一条条目，留下 manifest
// 与其它条目；这里把 bundle 目录连同 manifest 一起删掉，列表里随即消失。
//
// 目录本身是删除的最小单位，所以不需要先读 manifest：少读一次就少一次
// 与并发写入交错的机会。bundle 不存在时照常提交，不报错——「确保它不存在」
// 与「它刚刚被人删了」对调用方是同一个结果，重复清理应当是幂等的。
func (w *Writer) DeleteBundle(in DeleteBundleInput) (*WriteAssetOutput, error) {
	if err := ssot.ValidateBundle(in.Bundle); err != nil {
		return nil, err
	}
	revision, err := w.store.Update(in.Author, func(tx *ssot.Tx) (string, error) {
		if err := tx.EnsureRevision(in.BaseRevision); err != nil {
			return "", err
		}
		if err := tx.DeleteDir(ssot.AssetDir(in.Bundle)); err != nil {
			return "", err
		}
		msg := in.Message
		if msg == "" {
			msg = fmt.Sprintf("删除资产 %s", in.Bundle)
		}
		return msg, nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteAssetOutput{Bundle: in.Bundle, Revision: revision}, nil
}

// ReadManifest 读取某个 bundle 的 manifest。
func (w *Writer) ReadManifest(bundle string) (ssot.BundleManifest, error) {
	if err := ssot.ValidateBundle(bundle); err != nil {
		return ssot.BundleManifest{}, err
	}
	return ssot.ReadYAML[ssot.BundleManifest](w.store, path.Join(ssot.AssetDir(bundle), ssot.FileManifest))
}

// ListBundles 列出全部 bundle。
func (w *Writer) ListBundles() ([]string, error) {
	entries, err := w.store.ListDir(ssot.DirAssets)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := entries[:0]
	for _, name := range entries {
		if ssot.ValidateBundle(name) == nil {
			out = append(out, name)
		}
	}
	return out, nil
}

// ReadBundleFiles 读取一个 bundle 的全部文件内容。
func (w *Writer) ReadBundleFiles(bundle string) ([]AssetFile, error) {
	if err := ssot.ValidateBundle(bundle); err != nil {
		return nil, err
	}
	dir := ssot.AssetDir(bundle)
	files, err := w.store.WalkFiles(dir)
	if err != nil {
		return nil, err
	}
	var out []AssetFile
	for _, rel := range files {
		data, err := w.store.ReadFile(rel)
		if err != nil {
			return nil, err
		}
		out = append(out, AssetFile{
			Path:       strings.TrimPrefix(rel, dir+"/"),
			Content:    data,
			Executable: isExecutable(w.store.Dir(), rel),
		})
	}
	return out, nil
}

// isExecutable 判断仓库内的一个文件是否带可执行位。
//
// 读模式是为了让内容能原样写回：上传链路用 Executable 表达“这个脚本要能跑”，
// 如果读的时候丢掉这个信息，管理面保存一次就会把它降级成普通文件。
// 取不到信息时按不可执行处理——这只会让文件少一个执行位，不会造成越权。
func isExecutable(repoDir, rel string) bool {
	info, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	return info.Mode()&0o100 != 0
}

// SecretNames 返回当前全部密钥名，按字典序。
func (w *Writer) SecretNames() ([]string, error) {
	_, names, err := secretNamesFromStore(w.store)
	return names, err
}

// normalizeFiles 校验文件路径并排序，保证提交内容稳定。
func normalizeFiles(files []AssetFile) ([]AssetFile, error) {
	seen := map[string]bool{}
	out := make([]AssetFile, 0, len(files))
	for _, f := range files {
		if err := ssot.SafeRelPath(f.Path); err != nil {
			return nil, err
		}
		if seen[f.Path] {
			return nil, fmt.Errorf("%w: 重复的文件路径 %q", ErrSchema, f.Path)
		}
		seen[f.Path] = true
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// parseManifest 解析调用方提供的 manifest；为空时按文件推断。
//
// 这个函数只负责解析与补全，不参与“删什么”的决策：整体替换语义下，
// 删除已经由“文件列表里没有它”表达了。
func parseManifest(bundle string, raw []byte, files []AssetFile) (ssot.BundleManifest, error) {
	var m ssot.BundleManifest
	if len(raw) > 0 {
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&m); err != nil {
			return m, fmt.Errorf("%w: %v", ErrSchema, err)
		}
		if m.Name == "" {
			m.Name = bundle
		}
	} else {
		m = inferManifest(bundle, files)
	}
	return m, nil
}

// inferManifest 在调用方没给 manifest 时，按文件树推断条目。
//
// 约定：带 SKILL.md 的一级目录是一个 skill 条目；其余文件各自成为一条 rule，
// 名称取文件名主干。刻意不把“无 SKILL.md 的目录”当成一条 rule：
// rule 是单文件资产，那样推断出来的路径找不到对应文件，只会给出难懂的报错。
func inferManifest(bundle string, files []AssetFile) ssot.BundleManifest {
	m := ssot.BundleManifest{Name: bundle}
	seen := map[string]bool{}
	for _, f := range files {
		if f.Path == ssot.FileManifest {
			continue
		}
		top := strings.SplitN(f.Path, "/", 2)[0]
		if top != f.Path && hasSkillManifest(files, top) {
			if seen[top] {
				continue
			}
			seen[top] = true
			m.Items = append(m.Items, ssot.ManifestItem{
				Kind:   ssot.KindSkill,
				Path:   top,
				Name:   top,
				Planes: []ssot.Plane{ssot.PlaneUser},
			})
			continue
		}
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		m.Items = append(m.Items, ssot.ManifestItem{
			Kind:   ssot.KindRule,
			Path:   f.Path,
			Name:   strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path)),
			Planes: []ssot.Plane{ssot.PlaneUser},
		})
	}
	sort.Slice(m.Items, func(i, j int) bool { return m.Items[i].Path < m.Items[j].Path })
	return m
}

func hasSkillManifest(files []AssetFile, dir string) bool {
	prefix := dir + "/"
	for _, f := range files {
		if f.Path == prefix+"SKILL.md" || f.Path == prefix+"skill.md" {
			return true
		}
	}
	return false
}

// validateManifest 校验条目结构。
func validateManifest(m ssot.BundleManifest, files []AssetFile) error {
	if err := ssot.ValidateBundle(m.Name); err != nil {
		return err
	}
	paths := map[string]bool{}
	for _, f := range files {
		paths[f.Path] = true
	}
	seenPath := map[string]bool{}
	seenName := map[string]bool{}
	seenSlot := map[string]bool{}
	for _, item := range m.Items {
		if !item.Kind.Valid() {
			return fmt.Errorf("%w: 条目 %q 的类型 %q 不受支持", ErrSchema, item.Path, item.Kind)
		}
		if err := ssot.SafeRelPath(item.Path); err != nil {
			return err
		}
		if err := ssot.ValidateItemName(item.Name); err != nil {
			return err
		}
		if seenPath[item.Path] {
			return fmt.Errorf("%w: 条目路径 %q 重复", ErrSchema, item.Path)
		}
		if seenName[item.Name] {
			return fmt.Errorf("%w: 条目名 %q 重复", ErrSchema, item.Name)
		}
		seenPath[item.Path] = true
		seenName[item.Name] = true

		if len(item.Planes) == 0 {
			return fmt.Errorf("%w: 条目 %q 未声明平面", ErrSchema, item.Path)
		}
		for _, p := range item.Planes {
			if !p.Valid() {
				return fmt.Errorf("%w: 条目 %q 的平面 %q 不受支持", ErrSchema, item.Path, p)
			}
		}
		if item.Kind == ssot.KindFile {
			if err := validateFileItem(item); err != nil {
				return err
			}
			slot := item.Root + "|" + item.Target
			if seenSlot[slot] {
				return fmt.Errorf("%w: 条目 %q 与本 bundle 另一条目落到同一位置 %s/%s",
					ErrSchema, item.Path, item.Root, item.Target)
			}
			seenSlot[slot] = true
		} else if item.Root != "" || item.Target != "" || item.Format != "" || item.Mode != 0 {
			return fmt.Errorf("%w: 条目 %q 不是 file 类型，不能声明 root/target/format/mode", ErrSchema, item.Path)
		}
		// 条目在文件树里必须真的存在。
		if item.Kind == ssot.KindSkill {
			found := false
			for _, f := range files {
				if strings.HasPrefix(f.Path, item.Path+"/") {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%w: 条目 %q 声明为 skill，但目录下没有文件", ErrSchema, item.Path)
			}
			continue
		}
		if !paths[item.Path] {
			return fmt.Errorf("%w: 条目 %q 的内容文件缺失", ErrSchema, item.Path)
		}
	}
	return nil
}

// replaceBundle 用输入内容整体替换一个 bundle 的目录。
func replaceBundle(tx *ssot.Tx, bundle string, files []AssetFile, manifest ssot.BundleManifest) error {
	dir := ssot.AssetDir(bundle)
	if err := tx.DeleteDir(dir); err != nil {
		return err
	}
	manifestBytes, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("序列化 manifest 失败: %w", err)
	}
	if err := tx.WriteFile(path.Join(dir, ssot.FileManifest), manifestBytes, 0o600); err != nil {
		return err
	}
	for _, f := range files {
		if f.Path == ssot.FileManifest {
			continue
		}
		mode := fs.FileMode(fs0600)
		if f.Executable {
			mode = 0o700
		}
		if err := tx.WriteFile(path.Join(dir, f.Path), f.Content, mode); err != nil {
			return err
		}
	}
	return nil
}

const fs0600 = 0o600

// checkEntryConflicts 检测同名托管条目冲突（需求 23.5）。
//
// 冲突只可能发生在“渲染到同一个落点的同一个键”上，所以比较的是条目名，
// 而不是条目路径。发现冲突时拒绝写入，让用户改名，而不是让后写入的静默覆盖。
//
// 注意：必须在 replaceBundle 之前调用，而且要先读完其它 bundle 的 manifest，
// 因为 replaceBundle 会删除当前 bundle 的目录。
func checkEntryConflicts(tx *ssot.Tx, bundle string, manifest ssot.BundleManifest) error {
	bundles, err := tx.Store().ListDir(ssot.DirAssets)
	if err != nil && !errors.Is(err, ssot.ErrNotFound) {
		return err
	}
	own := map[string]bool{}
	for _, item := range manifest.Items {
		own[item.Name] = true
	}
	if len(own) == 0 {
		return nil
	}
	for _, other := range bundles {
		if other == bundle || ssot.ValidateBundle(other) != nil {
			continue
		}
		m, err := ssot.ReadYAML[ssot.BundleManifest](tx.Store(), path.Join(ssot.AssetDir(other), ssot.FileManifest))
		if err != nil {
			return fmt.Errorf("读取 bundle %q 的 manifest 失败: %w", other, err)
		}
		for _, item := range m.Items {
			if own[item.Name] {
				return fmt.Errorf("%w: 条目名 %q 已被 bundle %q 使用，请改名", ssot.ErrConflict, item.Name, other)
			}
		}
	}
	return nil
}

// 疑似明文密钥的扫描规则。
//
// 这一道检查的目的不是“证明没有密钥”，而是拦住最常见的误操作：
// 把 token、私钥、连接串直接写进资产。真正的安全边界仍是服务端本身。
//
// 规则必须容忍两类常见误报，否则正常的资产会被挡住，用户只能改写内容或
// 绕过检查——两种结果都比漏一个启发式判断更糟（迁移存量时就撞上了）：
//
//   - CI 的模板变量写法 `https://${{settings.token.user}}:${{settings.token.pass}}@host`，
//     密码段是变量名不是值。排除 $ { } 三个字符即可放行。
//   - 文档与代码里的字段说明，例如类型映射表 `"password": "vuex-input"`、
//     帮助文案 `cdb_account_password: "修改密码请用控制台"`。
//     真实密钥不含空格；纯小写标识符是字段名或类型名，不是密钥。
//     这两条在 matchesPlaintext 里用后置判断排除——Go 的 RE2 不支持负向前瞻。
var plaintextPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"私钥块", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"AWS 访问密钥", regexp.MustCompile(`\bAKID[A-Za-z0-9]{13,}\b`)},
	{"OpenAI/Anthropic 风格密钥", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
	{"GitHub 令牌", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`)},
	{"Slack 令牌", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"带密码的连接串", regexp.MustCompile(`(?i)\b[a-z]+://[^\s/:@${}]+:[^\s/@${}]{6,}@`)},
	{"内联私钥字段", regexp.MustCompile(`(?i)"?(private_key|api_secret|client_secret|password)"?\s*[:=]\s*"([^"]{8,})"`)},
}

// notASecretValue 判断一段值看起来不像密钥，用于压掉文档与代码里的误报。
//
//   - 含空白：`修改密码请用控制台或 CDB API（...）` 这类是说明文字。
//     配置文件里的真实口令不含空格，这是启发式判断，不是绝对规则。
//   - 短、全小写、只含字母与 - _：`vuex-input` 这类是字段名或类型名。
//     含数字就不算——`abcdefgh12345678` 这种看着就像口令。
//
// 真实口令几乎必然含大写、数字、符号中的至少一种，两种情况都不会命中。
func notASecretValue(v string) bool {
	if strings.ContainsAny(v, " \t\r\n") {
		return true
	}
	if len(v) > 24 {
		return false
	}
	for _, r := range v {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// scanForPlaintextSecrets 扫描资产内容里的疑似明文密钥（需求 7.3）。
func scanForPlaintextSecrets(files []AssetFile) error {
	for _, f := range files {
		for _, p := range plaintextPatterns {
			loc := p.re.FindSubmatchIndex(f.Content)
			if loc == nil {
				continue
			}
			// 内联字段的第二个捕获组是值本身：值不像密钥就跳过这一处，
			// 继续往后找该规则的下一次命中。
			if p.name == "内联私钥字段" && !findInlineSecret(f.Content, p.re) {
				continue
			}
			return fmt.Errorf("%w: %s 中疑似包含%s（位置 %d），请改为密钥引用",
				ErrPlaintextSecret, f.Path, p.name, loc[0])
		}
	}
	return nil
}

// findInlineSecret 在内容里找一处「值确实像密钥」的内联字段。
func findInlineSecret(content []byte, re *regexp.Regexp) bool {
	for _, loc := range re.FindAllSubmatchIndex(content, -1) {
		if len(loc) < 6 || loc[4] < 0 {
			return true
		}
		if !notASecretValue(string(content[loc[4]:loc[5]])) {
			return true
		}
	}
	return false
}

// checkSecretRefs 校验资产里引用的密钥都存在（需求 7.2）。
//
// 引用语法：{{secret:名称}}。渲染时才取值，写入时只校验存在性。
func checkSecretRefs(files []AssetFile, available []string) error {
	if len(files) == 0 {
		return nil
	}
	known := make(map[string]bool, len(available))
	for _, n := range available {
		known[n] = true
	}
	var missing []string
	for _, f := range files {
		for _, name := range ExtractSecretRefs(f.Content) {
			if !known[name] {
				missing = append(missing, fmt.Sprintf("%s 引用 %s", f.Path, name))
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: %s", ErrMissingSecret, strings.Join(missing, "; "))
	}
	return nil
}

var secretRefRe = regexp.MustCompile(`\{\{\s*secret:([a-z0-9][a-z0-9._-]*)\s*\}\}`)

// ExtractSecretRefs 返回内容里引用的密钥名，按出现顺序去重。
func ExtractSecretRefs(content []byte) []string {
	matches := secretRefRe.FindAllSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := string(m[1])
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
