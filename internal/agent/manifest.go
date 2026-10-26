package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/notice"
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
		// 多落点条目：所有落点内容必须一致，否则上报空哈希让云端补发。
		// 只取最后一份的值是不行的——它会把「第一份被改过、最后一份没动」
		// 判成一致，于是被改的那一份永远不会被纠正回来。一份资产同时落到
		// .cursor/.claude/.codex/.with 等五个目录时，这种漏检是常态而非巧合。
		var hash string
		inconsistent := false
		for i, p := range e.Paths {
			h, err := HashFile(p, e.Format, e.Key)
			if err != nil {
				inconsistent = true
				break
			}
			if h == "" {
				inconsistent = true
				break
			}
			if i == 0 {
				hash = h
				continue
			}
			if h != hash {
				inconsistent = true
				break
			}
		}
		if inconsistent {
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
//
// 必须按说明行判定“是不是 Dec 写的”，不能把目录下的文件一律当自己的。
// skills/ 与 rules/ 是用户和 Dec 共用的目录：用户放进去的 skill、自己写的
// 规则就躺在同一层，按目录扫等于把用户的文件一起删掉。卸载是不可逆操作，
// 这种误删没有任何补救余地。
//
// 没有说明行不等于一定不是 Dec 写的（.yaml/.env 与含密钥文件不加说明），
// 但那些条目云端记着绝对路径，走核对清单就能删干净，不依赖这里兜底。
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
		ours, err := managedByNotice(p)
		if err != nil || !ours {
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

// managedByNotice 报告文件是否带 Dec 的说明行。
//
// 只读文件开头一段：说明行一定在文件最前面（或 front matter 之后），
// 不必把整个文件读进内存。
func managedByNotice(p string) (bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return false, err
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, err := f.Read(head)
	if err != nil && err != io.EOF {
		return false, err
	}
	return bytes.Contains(head[:n], []byte(notice.Text)), nil
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
