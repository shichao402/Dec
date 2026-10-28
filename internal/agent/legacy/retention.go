package legacy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ArchiveInfo 是工具页上的一行归档（需求 6.2）。
type ArchiveInfo struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Items     int       `json:"items"`
	Entries   int       `json:"entries"`
	// Bytes 是未压缩字节数，CompressedSize 是 .tar.gz 实际占用。
	Bytes          int64 `json:"bytes"`
	CompressedSize int64 `json:"compressed_size"`
	// Problem 非空表示这份归档缺件或清单坏了，不能还原。
	Problem string `json:"problem,omitempty"`
}

var archiveIDRE = regexp.MustCompile(`^\d{8}T\d{6}Z(-\d+)?$`)

// validID 防止通过 id 走出归档目录。
func validID(id string) bool { return archiveIDRE.MatchString(id) }

func idTime(id string) (time.Time, bool) {
	if !validID(id) {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102T150405Z", id[:16])
	return t, err == nil
}

// ListArchives 只读 manifest 副本列出归档，新的在前；不解压归档本体。
func ListArchives(stateDir string) ([]ArchiveInfo, error) {
	dir := ArchiveDir(stateDir)
	names, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []ArchiveInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, d := range names {
		n := d.Name()
		for _, suf := range []string{".tar.gz", ".json"} {
			if id := strings.TrimSuffix(n, suf); id != n && validID(id) {
				ids[id] = true
			}
		}
	}
	out := make([]ArchiveInfo, 0, len(ids))
	for id := range ids {
		info := ArchiveInfo{ID: id}
		if t, ok := idTime(id); ok {
			info.CreatedAt, info.ExpiresAt = t, t.AddDate(0, 0, RetentionDays)
		}
		if st, err := os.Stat(filepath.Join(dir, id+".tar.gz")); err == nil {
			info.CompressedSize = st.Size()
		} else {
			info.Problem = "缺少归档本体"
		}
		m, err := LoadManifest(stateDir, id)
		switch {
		case err != nil && info.Problem == "":
			info.Problem = "归档清单缺失或损坏"
		case err == nil:
			info.CreatedAt, info.ExpiresAt = m.CreatedAt, m.ExpiresAt
			info.Items, info.Entries, info.Bytes = len(m.Items), len(m.Entries), m.Bytes
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// DeleteArchive 立即删除一份归档（需求 6.4）。确认由界面负责。
func DeleteArchive(stateDir, id string) error {
	if !validID(id) {
		return fmt.Errorf("归档 id 不合法：%q", id)
	}
	dir := ArchiveDir(stateDir)
	found := false
	for _, suf := range []string{".tar.gz", ".json"} {
		err := os.Remove(filepath.Join(dir, id+suf))
		switch {
		case err == nil:
			found = true
		case !errors.Is(err, os.ErrNotExist):
			return err
		}
	}
	if !found {
		return fmt.Errorf("归档 %s 不存在", id)
	}
	return nil
}

// partialGrace 之后仍在的 .partial 是写归档时进程被杀留下的半成品。
const partialGrace = 24 * time.Hour

// PurgeExpired 清除到期的归档与遗留半成品，返回被清除的归档 id（需求 6.1）。
//
// 到期日以清单为准；清单缺失或损坏时按 id 里的时间加保留期判断，
// 免得一份坏归档永远清不掉。id 也解析不出时间的文件不认识，不碰。
func PurgeExpired(stateDir string, now time.Time) ([]string, error) {
	list, err := ListArchives(stateDir)
	if err != nil {
		return nil, err
	}
	var purged []string
	var errs []error
	for _, a := range list {
		if a.ExpiresAt.IsZero() || now.Before(a.ExpiresAt) {
			continue
		}
		if err := DeleteArchive(stateDir, a.ID); err != nil {
			errs = append(errs, err)
			continue
		}
		purged = append(purged, a.ID)
	}
	dir := ArchiveDir(stateDir)
	names, _ := os.ReadDir(dir)
	for _, d := range names {
		if !strings.HasSuffix(d.Name(), partialSuffix) {
			continue
		}
		if info, err := d.Info(); err == nil && now.Sub(info.ModTime()) > partialGrace {
			if err := os.Remove(filepath.Join(dir, d.Name())); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return purged, errors.Join(errs...)
}

// RunRetention 启动时清除一次，之后每天一次，直到 ctx 结束（需求 6.1）。
func RunRetention(ctx context.Context, stateDir string, interval time.Duration, logf func(string, ...any)) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	run := func() {
		purged, err := PurgeExpired(stateDir, time.Now())
		for _, id := range purged {
			logf("旧版残留归档 %s 已过 %d 天保留期，已清除", id, RetentionDays)
		}
		if err != nil {
			logf("清除过期的旧版残留归档失败：%v", err)
		}
	}
	run()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
