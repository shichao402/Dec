package legacy

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RestoreItem 是一项还原的结果（需求 6.3）。
type RestoreItem struct {
	Index int    `json:"index"`
	Item  Item   `json:"item"`
	Dest  string `json:"dest,omitempty"`
	// Renamed 为真表示原路径已被占用，还原到了旁边的 .restored-<时间戳>。
	Renamed bool   `json:"renamed,omitempty"`
	OK      bool   `json:"ok"`
	Reason  string `json:"reason,omitempty"`
	// Notes 是需要用户知道的附加说明，例如需要自行提交、特殊文件没有内容可还原。
	Notes []string `json:"notes,omitempty"`
}

// RestoreResult 是一次还原的结果。
type RestoreResult struct {
	ID    string        `json:"id"`
	Items []RestoreItem `json:"items"`
}

// Failed 报告是否有失败项。
func (r *RestoreResult) Failed() bool {
	for _, it := range r.Items {
		if !it.OK {
			return true
		}
	}
	return false
}

// Restore 按归档还原；indexes 为空表示整份还原（需求 6.3、8.7）。
//
// 先完整校验归档，再逐项解到目标旁边的临时目录，成功后一次 rename 落位，
// 所以单项失败不会留下半份内容。原路径已存在时不覆盖，改写到 <原名>.restored-<时间戳>。
// 还原不做 git 提交：要不要把还原结果提交回去由用户决定。
func Restore(stateDir, id string, indexes []int, now time.Time) (*RestoreResult, error) {
	if !validID(id) {
		return nil, fmt.Errorf("归档 id 不合法：%q", id)
	}
	path := filepath.Join(ArchiveDir(stateDir), id+".tar.gz")
	m, err := VerifyArchive(path)
	if err != nil {
		return nil, fmt.Errorf("归档 %s 校验失败，拒绝还原：%w", id, err)
	}
	if err := checkManifestPaths(m); err != nil {
		return nil, fmt.Errorf("归档 %s 的清单不可信，拒绝还原：%w", id, err)
	}

	selected := map[int]bool{}
	if len(indexes) == 0 {
		for i := range m.Items {
			selected[i] = true
		}
	}
	for _, i := range indexes {
		if i < 0 || i >= len(m.Items) {
			return nil, fmt.Errorf("归档 %s 没有第 %d 项", id, i)
		}
		selected[i] = true
	}

	stamp := now.UTC().Format("20060102T150405Z")
	jobs := map[int]*restoreJob{}
	res := &RestoreResult{ID: id, Items: []RestoreItem{}}
	order := make([]int, 0, len(selected))
	for i := range selected {
		order = append(order, i)
	}
	sort.Ints(order)
	for _, i := range order {
		it := m.Items[i]
		r := RestoreItem{Index: i, Item: it}
		if it.Project != "" {
			r.Notes = append(r.Notes, "还原到项目里的文件不会自动提交，需要的话请自行提交")
		}
		job, err := newRestoreJob(it, stamp)
		if err != nil {
			r.Reason = err.Error()
		} else {
			jobs[i] = job
		}
		res.Items = append(res.Items, r)
	}
	defer func() {
		for _, j := range jobs {
			j.cleanup()
		}
	}()

	if err := extractInto(path, m, jobs); err != nil {
		return nil, err
	}

	for k := range res.Items {
		r := &res.Items[k]
		job := jobs[r.Index]
		if job == nil {
			continue
		}
		if job.err != nil {
			r.Reason = job.err.Error()
			continue
		}
		if job.others > 0 {
			r.Notes = append(r.Notes, fmt.Sprintf("%d 个特殊文件（socket 等）没有内容可还原，已跳过", job.others))
		}
		if it := r.Item; it.Action == ActionStrip {
			for _, e := range m.Entries {
				if e.Item == r.Index && e.LinkTarget != "" {
					r.Notes = append(r.Notes, "原路径是指向 "+e.LinkTarget+" 的软链接，还原出的是它当时的内容")
				}
			}
		}
		dest, renamed, err := job.commit(stamp)
		if err != nil {
			r.Reason = err.Error()
			continue
		}
		r.Dest, r.Renamed, r.OK = dest, renamed, true
	}
	return res, nil
}

// checkManifestPaths 确认每个条目都落在所属命中之内、成员名与原路径对应。
// 清单是我们自己写的，但它在磁盘上躺一年；被改过的清单不能把文件写到别处。
func checkManifestPaths(m *Manifest) error {
	for _, it := range m.Items {
		if !filepath.IsAbs(it.Path) || filepath.Clean(it.Path) != it.Path {
			return fmt.Errorf("命中路径不规范：%s", it.Path)
		}
	}
	for _, e := range m.Entries {
		if e.Item < 0 || e.Item >= len(m.Items) {
			return fmt.Errorf("条目 %s 的归属下标越界", e.Path)
		}
		root := m.Items[e.Item].Path
		if filepath.Clean(e.Path) != e.Path || !within(e.Path, root) {
			return fmt.Errorf("条目 %s 不在 %s 之内", e.Path, root)
		}
		if e.Type == EntryFragment && e.Path != root {
			return fmt.Errorf("共享文件条目 %s 与命中不一致", e.Path)
		}
		if e.Member != "" && e.Member != memberName(e.Path) {
			return fmt.Errorf("条目 %s 的成员名不符", e.Path)
		}
	}
	return nil
}

type restoreJob struct {
	item    Item
	staging string // 目标父目录下的临时目录
	root    string // staging 里对应命中根的位置
	dirs    []ArchiveEntry
	others  int
	err     error
}

func newRestoreJob(it Item, stamp string) (*restoreJob, error) {
	parent := filepath.Dir(it.Path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("创建上级目录：%w", err)
	}
	staging, err := os.MkdirTemp(parent, ".dec-restore-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录：%w", err)
	}
	return &restoreJob{item: it, staging: staging, root: filepath.Join(staging, "root")}, nil
}

func (j *restoreJob) cleanup() {
	if j.staging != "" {
		_ = os.RemoveAll(j.staging)
	}
}

func (j *restoreJob) stagedPath(p string) string {
	rel, _ := filepath.Rel(j.item.Path, p)
	return filepath.Join(j.root, rel)
}

// commit 把解好的内容落位；原路径已存在时换成 .restored-<时间戳>，仍被占用再加序号。
func (j *restoreJob) commit(stamp string) (string, bool, error) {
	// 目录权限放到最后设：先设成只读会挡住子项写入。深的先设。
	sort.Slice(j.dirs, func(a, b int) bool { return len(j.dirs[a].Path) > len(j.dirs[b].Path) })
	for _, d := range j.dirs {
		p := j.stagedPath(d.Path)
		if err := os.Chmod(p, os.FileMode(d.Mode)); err != nil {
			return "", false, err
		}
		_ = os.Chtimes(p, d.ModTime, d.ModTime)
	}
	if _, err := os.Lstat(j.root); err != nil {
		return "", false, errors.New("归档里没有这一项的内容")
	}
	dest := j.item.Path
	renamed := false
	for n := 1; ; n++ {
		if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		if n > 100 {
			return "", false, errors.New("还原目标的备用名都已被占用")
		}
		renamed = true
		dest = j.item.Path + ".restored-" + stamp
		if n > 1 {
			dest = fmt.Sprintf("%s-%d", dest, n)
		}
	}
	if err := os.Rename(j.root, dest); err != nil {
		return "", false, err
	}
	return dest, renamed, nil
}

// extractInto 读一遍归档，把各命中的成员写进对应的临时目录。单项出错只记在该项上。
func extractInto(path string, m *Manifest, jobs map[int]*restoreJob) error {
	byMember := map[string]ArchiveEntry{}
	for _, e := range m.Entries {
		if e.Member != "" {
			byMember[e.Member] = e
		} else if j := jobs[e.Item]; j != nil {
			j.others++
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取归档：%w", err)
		}
		e, ok := byMember[strings.TrimSuffix(hdr.Name, "/")]
		if !ok {
			continue
		}
		j := jobs[e.Item]
		if j == nil || j.err != nil {
			continue
		}
		j.err = j.write(e, tr)
	}
}

func (j *restoreJob) write(e ArchiveEntry, r io.Reader) error {
	p := j.stagedPath(e.Path)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	switch e.Type {
	case EntryDir:
		if err := os.MkdirAll(p, 0o700); err != nil {
			return err
		}
		j.dirs = append(j.dirs, e)
		return nil
	case EntrySymlink:
		return os.Symlink(e.LinkTarget, p)
	case EntryFile, EntryFragment:
		out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(io.MultiWriter(out, h), r)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("写入 %s：%w", e.Path, err)
		}
		if hex.EncodeToString(h.Sum(nil)) != e.SHA256 {
			return fmt.Errorf("%s 解出后 sha256 不符", e.Path)
		}
		if err := os.Chmod(p, os.FileMode(e.Mode)); err != nil {
			return err
		}
		return os.Chtimes(p, e.ModTime, e.ModTime)
	}
	return nil
}
