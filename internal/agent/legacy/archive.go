package legacy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 归档布局（需求 4.1）：
//
//	<StateDir>/legacy-archive/<id>.tar.gz   归档本体
//	<StateDir>/legacy-archive/<id>.json     manifest 副本，供工具页列表，免得逐个解压一年的归档
//
// 归档内：files/<原绝对路径> 按原路径镜像存放，最后一个成员是 manifest.json。
// manifest 写在最后，因为 sha256 要在内容写完后才知道。
const (
	ArchiveDirName = "legacy-archive"
	// RetentionDays 是归档保留期（需求 6.1）。
	RetentionDays = 365

	manifestVersion = 1
	manifestMember  = "manifest.json"
	filesPrefix     = "files/"
	partialSuffix   = ".partial"
)

// EntryType 是归档条目的类型。
type EntryType string

const (
	EntryFile    EntryType = "file"
	EntryDir     EntryType = "dir"
	EntrySymlink EntryType = "symlink"
	// EntryFragment 是共享配置文件：整份原文归档，执行时只删其中的旧条目（需求 5.1、5.2）。
	// 原路径是软链接时（例如 ~/.zshrc 指向 dotfiles 仓库）归档的是链接指向的内容，
	// 执行时也是改链接指向的文件，LinkTarget 记录链接本身便于核对。
	EntryFragment EntryType = "fragment"
	// EntryOther 是无法归档内容的特殊文件（socket、命名管道、设备）。
	// 只记录元数据；它们没有可还原的内容，删掉也不会丢数据。
	EntryOther EntryType = "other"
)

// ArchiveEntry 是归档中的一项（需求 4.1）。
type ArchiveEntry struct {
	Path       string    `json:"path"`
	Member     string    `json:"member,omitempty"`
	Type       EntryType `json:"type"`
	Mode       uint32    `json:"mode"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256,omitempty"`
	LinkTarget string    `json:"link_target,omitempty"`
	// ModTime 供删除前核对：归档后被改过的文件不应按归档内容删掉。
	ModTime time.Time `json:"mod_time"`
	Rule    string    `json:"rule"`
	// Item 是所属命中在 Manifest.Items 中的下标。
	Item int `json:"item"`
}

// Manifest 是一次归档的完整记录。
type Manifest struct {
	Version   int            `json:"version"`
	ID        string         `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt time.Time      `json:"expires_at"`
	Items     []Item         `json:"items"`
	Entries   []ArchiveEntry `json:"entries"`
	// Bytes 是归档内容的未压缩字节数。
	Bytes int64 `json:"bytes"`
}

// Archive 是写好并校验过的归档。
type Archive struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Manifest *Manifest `json:"manifest"`
	// CompressedSize 是 .tar.gz 的体积。
	CompressedSize int64 `json:"compressed_size"`
}

// ArchiveDir 返回归档目录。
func ArchiveDir(stateDir string) string { return filepath.Join(stateDir, ArchiveDirName) }

// WriteArchive 把待删内容写入归档并回读校验（需求 4.1–4.4）。
//
// 返回成功即表示归档已落盘并通过校验，可以开始删除；任何失败都不留下半成品，
// 也不碰原文件。只列出的命中不进入归档。
func WriteArchive(stateDir string, items []Item, now time.Time) (*Archive, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("状态目录为空")
	}
	stateDir = filepath.Clean(stateDir)
	kept, err := normalizeItems(stateDir, items)
	if err != nil {
		return nil, err
	}
	if len(kept) == 0 {
		return nil, errors.New("没有需要归档的条目")
	}

	dir := ArchiveDir(stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建归档目录：%w", err)
	}
	// 目录可能早已存在且权限更宽，这里收紧（需求 4.3）。
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("收紧归档目录权限：%w", err)
	}

	created := now.UTC().Truncate(time.Second)
	id, err := newArchiveID(dir, created)
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		Version:   manifestVersion,
		ID:        id,
		CreatedAt: created,
		ExpiresAt: created.AddDate(0, 0, RetentionDays),
		Items:     kept,
	}

	final := filepath.Join(dir, id+".tar.gz")
	sidecar := filepath.Join(dir, id+".json")
	partial := final + partialSuffix
	sidecarPartial := sidecar + partialSuffix
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(partial)
			_ = os.Remove(sidecarPartial)
			_ = os.Remove(final)
			_ = os.Remove(sidecar)
		}
	}()

	manifestJSON, err := writeTar(partial, m)
	if err != nil {
		return nil, err
	}
	if err := verifyFile(partial, m, manifestJSON); err != nil {
		return nil, fmt.Errorf("归档校验失败：%w", err)
	}
	if err := writePrivate(sidecarPartial, manifestJSON); err != nil {
		return nil, fmt.Errorf("写入归档清单：%w", err)
	}
	if err := os.Rename(partial, final); err != nil {
		return nil, fmt.Errorf("落定归档：%w", err)
	}
	if err := os.Rename(sidecarPartial, sidecar); err != nil {
		return nil, fmt.Errorf("落定归档清单：%w", err)
	}
	info, err := os.Stat(final)
	if err != nil {
		return nil, err
	}
	ok = true
	return &Archive{ID: id, Path: final, Manifest: m, CompressedSize: info.Size()}, nil
}

// VerifyArchive 重新读取一份已落定的归档并按 manifest 校验，供还原前使用。
func VerifyArchive(path string) (*Manifest, error) {
	sidecar := strings.TrimSuffix(path, ".tar.gz") + ".json"
	data, err := os.ReadFile(sidecar)
	if err != nil {
		return nil, fmt.Errorf("读取归档清单：%w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("解析归档清单：%w", err)
	}
	if err := verifyFile(path, &m, data); err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadManifest 只读 manifest 副本，不解压归档；用于列表。
func LoadManifest(stateDir, id string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(ArchiveDir(stateDir), id+".json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// normalizeItems 去掉只列出的命中、重复与被包含的命中，并拒绝归档自身所在目录。
//
// 同一路径可能被两条规则命中（例如用户目录本身被登记成项目，H08 与 P03 重叠）；
// 被删目录里的共享文件也不必再单独处理。
func normalizeItems(stateDir string, items []Item) ([]Item, error) {
	var cand []Item
	for _, it := range items {
		if it.Action == ActionList {
			continue
		}
		if it.Action != ActionDelete && it.Action != ActionStrip {
			return nil, fmt.Errorf("未知动作 %q：%s", it.Action, it.Path)
		}
		if !filepath.IsAbs(it.Path) {
			return nil, fmt.Errorf("不是绝对路径：%s", it.Path)
		}
		it.Path = filepath.Clean(it.Path)
		if overlaps(it.Path, stateDir) {
			return nil, fmt.Errorf("拒绝归档代理自身的状态目录：%s", it.Path)
		}
		cand = append(cand, it)
	}
	// 外层在前；同路径时整删优先于只删条目。
	sort.SliceStable(cand, func(i, j int) bool {
		if cand[i].Path != cand[j].Path {
			return cand[i].Path < cand[j].Path
		}
		return cand[i].Action == ActionDelete && cand[j].Action != ActionDelete
	})
	var kept []Item
	for _, it := range cand {
		covered := false
		for _, k := range kept {
			if k.Action == ActionDelete && within(it.Path, k.Path) {
				covered = true
				break
			}
			if k.Path == it.Path {
				covered = true
				break
			}
		}
		if !covered {
			kept = append(kept, it)
		}
	}
	return kept, nil
}

func newArchiveID(dir string, t time.Time) (string, error) {
	base := t.Format("20060102T150405Z")
	for n := 1; n < 1000; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		taken := false
		for _, suffix := range []string{".tar.gz", ".json", ".tar.gz" + partialSuffix, ".json" + partialSuffix} {
			if _, err := os.Lstat(filepath.Join(dir, id+suffix)); err == nil {
				taken = true
				break
			}
		}
		if !taken {
			return id, nil
		}
	}
	return "", errors.New("同一秒内的归档过多")
}

// writeTar 写出归档并 fsync，返回写入的 manifest.json 原文。
func writeTar(path string, m *Manifest) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建归档：%w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	for i, it := range m.Items {
		var err error
		if it.Action == ActionStrip {
			err = addFragment(tw, m, i, it)
		} else {
			err = addTree(tw, m, i, it)
		}
		if err != nil {
			return nil, err
		}
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: manifestMember, Mode: 0o600, Size: int64(len(data)),
		ModTime: m.CreatedAt, Typeflag: tar.TypeReg, Format: tar.FormatPAX,
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(data); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	return data, f.Close()
}

// addTree 归档一个整删对象。不跟随软链接（需求 4.4）：WalkDir 对根和子项都用 Lstat。
func addTree(tw *tar.Writer, m *Manifest, idx int, it Item) error {
	return filepath.WalkDir(it.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("读取 %s：%w", p, err)
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("读取 %s：%w", p, err)
		}
		e := ArchiveEntry{
			Path: p, Mode: uint32(info.Mode().Perm()), ModTime: info.ModTime().UTC(),
			Rule: it.Rule, Item: idx,
		}
		switch {
		case info.Mode().IsDir():
			e.Type = EntryDir
			e.Member = memberName(p)
			if err := tw.WriteHeader(&tar.Header{
				Name: e.Member + "/", Mode: int64(e.Mode), ModTime: e.ModTime,
				Typeflag: tar.TypeDir, Format: tar.FormatPAX,
			}); err != nil {
				return err
			}
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("读取链接 %s：%w", p, err)
			}
			e.Type, e.LinkTarget, e.Member = EntrySymlink, target, memberName(p)
			if err := tw.WriteHeader(&tar.Header{
				Name: e.Member, Linkname: target, Mode: int64(e.Mode), ModTime: e.ModTime,
				Typeflag: tar.TypeSymlink, Format: tar.FormatPAX,
			}); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			e.Type = EntryFile
			if err := addFile(tw, &e, p, info); err != nil {
				return err
			}
			m.Bytes += e.Size
		default:
			e.Type = EntryOther
		}
		m.Entries = append(m.Entries, e)
		return nil
	})
}

// addFragment 归档一份共享配置文件的全文（需求 5.1、5.2）。
func addFragment(tw *tar.Writer, m *Manifest, idx int, it Item) error {
	linfo, err := os.Lstat(it.Path)
	if err != nil {
		return fmt.Errorf("读取 %s：%w", it.Path, err)
	}
	e := ArchiveEntry{Path: it.Path, Type: EntryFragment, Rule: it.Rule, Item: idx}
	if linfo.Mode()&fs.ModeSymlink != 0 {
		if e.LinkTarget, err = os.Readlink(it.Path); err != nil {
			return fmt.Errorf("读取链接 %s：%w", it.Path, err)
		}
	}
	info, err := os.Stat(it.Path)
	if err != nil {
		return fmt.Errorf("读取 %s：%w", it.Path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s 不是普通文件，不能只删其中的条目", it.Path)
	}
	e.Mode, e.ModTime = uint32(info.Mode().Perm()), info.ModTime().UTC()
	if err := addFile(tw, &e, it.Path, info); err != nil {
		return err
	}
	m.Bytes += e.Size
	m.Entries = append(m.Entries, e)
	return nil
}

// addFile 写入一个普通文件并计算 sha256。
//
// tar 头里的大小取自写入前的 stat；读到的字节数不符说明文件正在被改，整次中止。
func addFile(tw *tar.Writer, e *ArchiveEntry, p string, info fs.FileInfo) error {
	f, err := os.Open(p)
	if err != nil {
		return fmt.Errorf("打开 %s：%w", p, err)
	}
	defer f.Close()
	e.Member, e.Size = memberName(p), info.Size()
	if err := tw.WriteHeader(&tar.Header{
		Name: e.Member, Mode: int64(e.Mode), Size: e.Size, ModTime: e.ModTime,
		Typeflag: tar.TypeReg, Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.CopyN(tw, io.TeeReader(f, h), e.Size)
	if err != nil || n != e.Size {
		return fmt.Errorf("%s 在归档过程中发生变化（预期 %d 字节，读到 %d）：%v", p, e.Size, n, err)
	}
	if extra, _ := f.Read(make([]byte, 1)); extra > 0 {
		return fmt.Errorf("%s 在归档过程中变长", p)
	}
	e.SHA256 = hex.EncodeToString(h.Sum(nil))
	return nil
}

// verifyFile 重新读取归档，逐项核对类型、大小、sha256 与链接目标，条目数必须一致（需求 4.2）。
func verifyFile(path string, m *Manifest, manifestJSON []byte) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("不是合法的 gzip：%w", err)
	}
	defer gz.Close()

	want := map[string]ArchiveEntry{}
	for _, e := range m.Entries {
		if e.Member == "" {
			continue
		}
		if _, dup := want[e.Member]; dup {
			return fmt.Errorf("清单中成员重复：%s", e.Member)
		}
		want[e.Member] = e
	}
	seen := map[string]bool{}
	sawManifest := false
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取归档：%w", err)
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		if name == manifestMember {
			got, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, manifestJSON) {
				return errors.New("归档内的 manifest 与预期不一致")
			}
			sawManifest = true
			continue
		}
		e, ok := want[name]
		if !ok {
			return fmt.Errorf("归档中有清单外的成员：%s", name)
		}
		if seen[name] {
			return fmt.Errorf("归档中成员重复：%s", name)
		}
		seen[name] = true
		switch e.Type {
		case EntryDir:
			if hdr.Typeflag != tar.TypeDir {
				return fmt.Errorf("%s 应为目录", name)
			}
		case EntrySymlink:
			if hdr.Typeflag != tar.TypeSymlink || hdr.Linkname != e.LinkTarget {
				return fmt.Errorf("%s 的链接目标不符", name)
			}
		case EntryFile, EntryFragment:
			if hdr.Typeflag != tar.TypeReg || hdr.Size != e.Size {
				return fmt.Errorf("%s 的大小不符", name)
			}
			h := sha256.New()
			if _, err := io.Copy(h, tr); err != nil {
				return fmt.Errorf("读取 %s：%w", name, err)
			}
			if hex.EncodeToString(h.Sum(nil)) != e.SHA256 {
				return fmt.Errorf("%s 的 sha256 不符", name)
			}
		default:
			return fmt.Errorf("%s 的类型 %s 不应有归档内容", name, e.Type)
		}
	}
	// 读到 gzip 流末尾才会校验 CRC。
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return fmt.Errorf("gzip 校验：%w", err)
	}
	if !sawManifest {
		return errors.New("归档中缺少 manifest.json")
	}
	if len(seen) != len(want) {
		return fmt.Errorf("条目数不符：清单 %d，归档 %d", len(want), len(seen))
	}
	return nil
}

// memberName 把绝对路径映射成归档成员名，保留原路径结构。
// Windows 的盘符 C: 写成 C，便于解压后肉眼查找。
func memberName(abs string) string {
	vol := filepath.VolumeName(abs)
	rest := filepath.ToSlash(strings.TrimPrefix(abs, vol))
	if vol != "" {
		rest = strings.TrimSuffix(filepath.ToSlash(vol), ":") + rest
	}
	return filesPrefix + strings.TrimPrefix(rest, "/")
}

func writePrivate(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}
