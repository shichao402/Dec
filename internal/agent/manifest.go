package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/proto"
)

// 本机清单（需求 2.1 的第一个原语）。
//
// 上报的是“代理管了什么、现在长什么样”，不是“云端应该下发什么”。
// 代理不保存下发清单（需求 5.2），所以清单只能从文件系统现场算出来。

// 探测结果。

// HostState 是一个宿主的落点情况。
type HostState struct {
	// Root 是宿主名（cursor、claude...）。
	Root string
	// Installed 表示本机看起来装了这个宿主（目录存在）。
	Installed bool
	// UserRoot 是用户级根目录的绝对路径。
	UserRoot string
}

// Detect 探测本机已安装的宿主。
//
// 只按目录存在性判断：读注册表、跑命令都会引入平台差异，
// 而代理只需要一个“大概装了哪些”的参考。真正决定落点的是云端下发的 Root。
func Detect(home string) []HostState {
	names := HostNames()
	sort.Strings(names)
	out := make([]HostState, 0, len(names))
	for _, name := range names {
		dirs := hosts[name]
		if dirs.userDir == "" {
			continue
		}
		dir := filepath.Join(home, dirs.userDir)
		_, err := os.Stat(dir)
		out = append(out, HostState{
			Root:      name,
			Installed: err == nil,
			UserRoot:  dir,
		})
	}
	return out
}

// BuildManifest 按云端给出的条目形状，计算本机落点的当前哈希（需求 5.3）。
//
// entries 来自云端状态：它知道每个条目应该落在哪些绝对路径上。
// 代理只算哈希，不做任何“猜路径”的动作。
func BuildManifest(entries []proto.ManifestEntry) []proto.ManifestEntry {
	out := make([]proto.ManifestEntry, 0, len(entries))
	for _, e := range entries {
		cur := proto.ManifestEntry{
			ID:     e.ID,
			Format: e.Format,
			Key:    e.Key,
			Paths:  e.Paths,
		}
		// 多落点条目：所有落点内容一致，取第一个能读到的哈希即可。
		// 有任一落点缺失都上报空哈希，让云端补发，而不是假装完整。
		var hash string
		missing := false
		for _, p := range e.Paths {
			h, err := HashFile(p, e.Format, e.Key)
			if err != nil {
				missing = true
				break
			}
			if h == "" {
				missing = true
				break
			}
			hash = h
		}
		if missing {
			cur.Hash = ""
		} else {
			cur.Hash = hash
		}
		out = append(out, cur)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// LocalProjects 把项目登记投影成协议结构。
func LocalProjects(projects []Project) []proto.LocalProject {
	out := make([]proto.LocalProject, 0, len(projects))
	for _, p := range projects {
		out = append(out, proto.LocalProject{ID: p.ID, Path: p.Path})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ScanManaged 扫出一个落点目录下由 Dec 管理的文件。
//
// 用于“云端不知道某个条目的路径”时的兜底：比如项目被移出登记后，
// 残留文件需要能被找出来删掉。只扫类别目录，不扫宿主根。
func ScanManaged(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描落点 %q 失败: %w", dir, err)
	}
	sort.Strings(out)
	return out, nil
}

// SkillDirName 从落点相对路径里取出 skill 目录名，供上报清单展示。
func SkillDirName(rel string) (string, bool) {
	if !strings.HasPrefix(rel, DirSkills+"/") {
		return "", false
	}
	rest := strings.TrimPrefix(rel, DirSkills+"/")
	name, _, _ := strings.Cut(rest, "/")
	if name == "" {
		return "", false
	}
	return name, true
}
