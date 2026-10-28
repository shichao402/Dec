package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrStale 表示页面确认的扫描结果与执行时重新扫描的结果不一致。
//
// 执行接口不接受页面传来的路径：页面只回传它展示过的那份结果的指纹，
// 代理自己重扫一次，指纹一致才执行。这样「用户看到的」就是「会被删的」，
// 页面也没有机会让代理删一个扫描之外的路径。
var ErrStale = errors.New("残留与上次扫描时不一致，请重新扫描后再确认")

// View 是工具页拿到的扫描结果。
type View struct {
	*Report
	// Fingerprint 是本次结果的指纹，执行时回传（需求 3.2）。
	Fingerprint string `json:"fingerprint"`
	// Empty 为真时页面显示「未发现旧版残留」（需求 3.3）。
	Empty bool `json:"empty"`
	// Rules 是命中规则的说明，按编号索引。
	Rules map[string]Rule `json:"rules"`
}

// ArchiveView 是一份归档的清单，用于按单项还原。
type ArchiveView struct {
	Info  ArchiveInfo `json:"info"`
	Items []Item      `json:"items"`
}

// Service 是工具页背后的操作入口：扫描、执行、列出、还原、删除归档。
//
// 会动文件的操作互斥执行：同时点两次执行，第二次重扫时会看到第一次的结果，
// 而不是两个执行者同时去删同一批文件。
type Service struct {
	StateDir string
	// ScanFn 做一次只读扫描，由调用方装配（家目录、项目、云端期望条目）。
	ScanFn func() (*Report, error)
	Logf   func(string, ...any)
	Git    GitOptions
	Procs  Processes
	Now    func() time.Time

	mu sync.Mutex
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Fingerprint 只覆盖会决定删什么的字段，按路径排序后求 sha256。
func Fingerprint(r *Report) string {
	type key struct {
		Rule    string   `json:"r"`
		Action  Action   `json:"a"`
		Path    string   `json:"p"`
		Entries []string `json:"e,omitempty"`
		Size    int64    `json:"s"`
		IsDir   bool     `json:"d,omitempty"`
	}
	ks := make([]key, 0, len(r.Items))
	for _, it := range r.Items {
		if it.Action == ActionList {
			continue // 只列出的凭据不会被动，不影响执行内容
		}
		es := append([]string(nil), it.Entries...)
		sort.Strings(es)
		ks = append(ks, key{it.Rule, it.Action, it.Path, es, it.Size, it.IsDir})
	}
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].Path != ks[j].Path {
			return ks[i].Path < ks[j].Path
		}
		return ks[i].Rule < ks[j].Rule
	})
	b, _ := json.Marshal(ks)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Service) scan() (*View, error) {
	if s.ScanFn == nil {
		return nil, errors.New("扫描未装配")
	}
	r, err := s.ScanFn()
	if err != nil {
		return nil, err
	}
	v := &View{Report: r, Fingerprint: Fingerprint(r), Empty: r.Empty(), Rules: map[string]Rule{}}
	for _, it := range r.Items {
		if rule, ok := RuleByID(it.Rule); ok {
			v.Rules[it.Rule] = rule
		}
	}
	for _, sk := range r.Skipped {
		if rule, ok := RuleByID(sk.Rule); ok {
			v.Rules[sk.Rule] = rule
		}
	}
	return v, nil
}

// Scan 只读扫描（需求 3.1、7.1）。
func (s *Service) Scan() (*View, error) {
	v, err := s.scan()
	if err != nil {
		s.logf("旧版残留扫描失败：%v", err)
		return nil, err
	}
	n := 0
	for _, it := range v.Items {
		if it.Action != ActionList {
			n++
		}
	}
	s.logf("旧版残留扫描：待清理 %d 项，仅列出 %d 项，预计归档 %d 字节", n, len(v.Items)-n, v.ArchiveBytes)
	return v, nil
}

// Execute 在指纹与重扫结果一致时执行清理（需求 3.2、7.1）。
func (s *Service) Execute(fingerprint string) (*Result, error) {
	if fingerprint == "" {
		return nil, errors.New("缺少扫描指纹，请先扫描")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.scan()
	if err != nil {
		s.logf("旧版残留清理未执行：重扫失败：%v", err)
		return nil, err
	}
	if v.Fingerprint != fingerprint {
		s.logf("旧版残留清理未执行：残留在确认后发生了变化")
		return nil, ErrStale
	}
	res, err := Execute(s.StateDir, v.Report, ExecOptions{Now: s.now, Procs: s.Procs, Git: s.Git})
	if err != nil {
		s.logf("旧版残留清理中止：%v", err)
		return nil, err
	}
	if res.Empty {
		s.logf("旧版残留清理：未发现旧版残留，未产生归档")
		return res, nil
	}
	counts := map[Status]int{}
	for _, it := range res.Items {
		counts[it.Status]++
		if it.Status == StatusFailed || it.Status == StatusSkipped {
			s.logf("旧版残留清理 %s：%s %s：%s", res.Archive.ID, it.Status, it.Item.Path, it.Reason)
		}
	}
	s.logf("旧版残留清理 %s：%d 项，删除 %d，改写 %d，跳过 %d，失败 %d",
		res.Archive.ID, len(res.Items), counts[StatusDeleted], counts[StatusStripped], counts[StatusSkipped], counts[StatusFailed])
	for _, rr := range res.Repos {
		msg := fmt.Sprintf("旧版残留清理 %s：仓库 %s %s", res.Archive.ID, rr.Repo, rr.Status)
		if rr.Commit != "" {
			msg += " " + rr.Commit[:min(12, len(rr.Commit))]
		}
		if rr.Reason != "" {
			msg += "：" + rr.Reason
		}
		s.logf("%s", msg)
	}
	return res, nil
}

// Archives 列出归档（需求 6.2）。
func (s *Service) Archives() ([]ArchiveInfo, error) { return ListArchives(s.StateDir) }

// Archive 读一份归档的清单，供按单项还原。
func (s *Service) Archive(id string) (*ArchiveView, error) {
	if !validID(id) {
		return nil, fmt.Errorf("归档 id 不合法：%q", id)
	}
	list, err := ListArchives(s.StateDir)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.ID != id {
			continue
		}
		m, err := LoadManifest(s.StateDir, id)
		if err != nil {
			return nil, err
		}
		return &ArchiveView{Info: a, Items: m.Items}, nil
	}
	return nil, fmt.Errorf("归档 %s 不存在", id)
}

// Restore 还原整份或部分条目（需求 6.3、7.1）。
func (s *Service) Restore(id string, indexes []int) (*RestoreResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := Restore(s.StateDir, id, indexes, s.now())
	if err != nil {
		s.logf("还原归档 %s 失败：%v", id, err)
		return nil, err
	}
	ok, renamed := 0, 0
	for _, it := range res.Items {
		if it.OK {
			ok++
			if it.Renamed {
				renamed++
			}
		} else {
			s.logf("还原归档 %s：%s 失败：%s", id, it.Item.Path, it.Reason)
		}
	}
	s.logf("还原归档 %s：%d 项，成功 %d（其中写到旁边 %d），失败 %d", id, len(res.Items), ok, renamed, len(res.Items)-ok)
	return res, nil
}

// DeleteArchive 立即删除一份归档（需求 6.4、7.1）。
func (s *Service) DeleteArchive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := DeleteArchive(s.StateDir, id); err != nil {
		s.logf("删除归档 %s 失败：%v", id, err)
		return err
	}
	s.logf("已手动删除归档 %s", id)
	return nil
}
