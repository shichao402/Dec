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
var plaintextPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"私钥块", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"AWS 访问密钥", regexp.MustCompile(`\bAKID[A-Za-z0-9]{13,}\b`)},
	{"OpenAI/Anthropic 风格密钥", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
	{"GitHub 令牌", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`)},
	{"Slack 令牌", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"带密码的连接串", regexp.MustCompile(`(?i)\b[a-z]+://[^\s/:@]+:[^\s/@]{6,}@`)},
	{"内联私钥字段", regexp.MustCompile(`(?i)"?(private_key|api_secret|client_secret|password)"?\s*[:=]\s*"[^"]{8,}"`)},
}

// scanForPlaintextSecrets 扫描资产内容里的疑似明文密钥（需求 7.3）。
func scanForPlaintextSecrets(files []AssetFile) error {
	for _, f := range files {
		for _, p := range plaintextPatterns {
			if loc := p.re.FindIndex(f.Content); loc != nil {
				return fmt.Errorf("%w: %s 中疑似包含%s（位置 %d），请改为密钥引用",
					ErrPlaintextSecret, f.Path, p.name, loc[0])
			}
		}
	}
	return nil
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
