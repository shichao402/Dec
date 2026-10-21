package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 本机项目登记（需求 2.1 的第二个原语）。
//
// 项目登记回答的是“这台机器上有哪些项目根，云端可以往哪个项目级落点写”。
// 它必须由用户在本机登记，不能由云端下发：云端的任何一条指令都不该
// 扩大“能写到哪里”的范围，否则白名单就形同虚设。
//
// 登记保存的是绝对路径，且只在代理本机存在。上报给云端的只有 ID 与路径，
// 用途是让云端把项目级落点渲染成这台机器上的真实路径。

// projectsFile 返回项目登记文件路径。
func projectsFile(stateDir string) string { return filepath.Join(stateDir, "projects.json") }

// LoadProjects 读取项目登记。文件不存在时返回空列表。
func LoadProjects(stateDir string) ([]Project, error) {
	data, err := readFileOrEmpty(projectsFile(stateDir))
	if err != nil {
		return nil, fmt.Errorf("读取项目登记失败: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var raw []Project
	if err := jsonUnmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析项目登记失败: %w", err)
	}
	out := make([]Project, 0, len(raw))
	for _, p := range raw {
		p.ID = strings.TrimSpace(p.ID)
		p.Path = strings.TrimSpace(p.Path)
		if p.ID == "" || p.Path == "" {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SaveProjects 写入项目登记。
func SaveProjects(stateDir string, projects []Project) error {
	clean, err := normalizeProjects(projects)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}
	data, err := jsonMarshalIndent(clean)
	if err != nil {
		return fmt.Errorf("序列化项目登记失败: %w", err)
	}
	return writeFileAtomic(projectsFile(stateDir), data, 0o600)
}

// normalizeProjects 校验并规范化项目登记。
//
// 两个约束：ID 唯一（云端按 ID 引用项目），路径必须是绝对路径（项目级落点
// 全部由它拼出来，相对路径会拼出意料之外的位置）。
func normalizeProjects(projects []Project) ([]Project, error) {
	seen := make(map[string]bool, len(projects))
	out := make([]Project, 0, len(projects))
	for _, p := range projects {
		id := strings.TrimSpace(p.ID)
		raw := strings.TrimSpace(p.Path)
		if id == "" {
			return nil, errors.New("项目 ID 不能为空")
		}
		if raw == "" {
			return nil, fmt.Errorf("项目 %q 的路径不能为空", id)
		}
		if !filepath.IsAbs(raw) {
			return nil, fmt.Errorf("项目 %q 的路径必须是绝对路径: %s", id, raw)
		}
		if seen[id] {
			return nil, fmt.Errorf("项目 ID 重复: %s", id)
		}
		seen[id] = true
		out = append(out, Project{ID: id, Path: filepath.Clean(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
