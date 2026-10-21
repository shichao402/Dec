// Package agent 实现超薄代理：只做鉴权、安全通讯和往白名单落点写文件。
//
// 代理的能力是固定的（需求 2.1、2.2）：上报清单、往白名单落点写、删除云端指定
// 且位于白名单内的文件、上报结果和状态、返回落点哈希。没有任意命令执行，
// 没有任意路径读写，也不接受云端扩展白名单——新增落点只能发布新版本代理。
//
// 白名单只防云端缺陷误写，不是防御服务端失陷的安全边界（已定决策 10）。
package agent

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shichao402/Dec/internal/proto"
)

// 落点的类别目录（与云端 render/layout.go 的约定一一对应）。
const (
	DirSkills = "skills"
	DirRules  = "rules"
	DirEnv    = "env"
	FileMCP   = "mcp.json"
	// RootDec 是 Dec 自有的落点根，不属于任何 IDE。
	RootDec = "dec"
)

// hostDirs 描述一个宿主在用户级与项目级的目录布局。
//
// 这张表就是白名单的实体：不在表里的 Root 一律拒绝（需求 2.2、2.3）。
// 表里刻意写死而不用 IDE 探测结果，因为探测结果会被环境影响，
// 而白名单必须是确定、可审计的。
type hostDirs struct {
	// userDir 是用户级根目录，相对用户目录；为空表示不支持用户平面。
	userDir string
	// projectDir 是项目级根目录，相对项目根；为空表示不支持项目平面。
	projectDir string
	// userMCP 是用户级 MCP 配置文件，相对用户目录；为空则用 <userDir>/mcp.json。
	userMCP string
	// projectMCP 是项目级 MCP 配置文件，相对项目根；为空则用 <projectDir>/mcp.json。
	projectMCP string
	// jsonKey 表示该宿主的 MCP 配置是 JSON、支持按键合并。
	jsonKey bool
}

// hosts 是内置宿主表。
//
// Codex 的 MCP 配置是 TOML，协议层没有 TOML 段合并，所以 jsonKey 为 false，
// 它不会收到 mcp.json 类型的下发（云端 render 层的 jsonKeyHosts 与此一致）。
var hosts = map[string]hostDirs{
	"cursor":    {userDir: ".cursor", projectDir: ".cursor", jsonKey: true},
	"claude":    {userDir: ".claude", projectDir: ".claude", jsonKey: true},
	"codebuddy": {userDir: ".codebuddy", projectDir: ".codebuddy", userMCP: ".mcp.json", projectMCP: ".mcp.json", jsonKey: true},
	"codex":     {userDir: ".codex", projectDir: ".codex", projectMCP: ".codex/config.toml"},
	"with": {
		userDir:    filepath.Join(".bg-agent", "config-with-app"),
		projectDir: ".with",
		userMCP:    filepath.Join(".bg-agent", "config-with-app", "mcp_config.json"),
		projectMCP: ".with/mcp_config.json",
		jsonKey:    true,
	},
	// dec 是 Dec 自有的落点根，不跟随任何 IDE（需求 23.2 之外的补充）。
	RootDec: {userDir: ".dec", projectDir: ".dec"},
}

// KnownHost 报告宿主是否在白名单内。
func KnownHost(name string) bool {
	_, ok := hosts[name]
	return ok
}

// HostNames 返回白名单里的全部宿主名，供上报清单使用。
func HostNames() []string {
	out := make([]string, 0, len(hosts))
	for name := range hosts {
		out = append(out, name)
	}
	return out
}

// Project 是代理侧的项目登记投影。
type Project struct {
	ID   string
	Path string
}

// Resolver 把云端的逻辑落点展开成本机绝对路径。
//
// 展开规则全部来自内置表与本地项目登记，云端无法影响：
// 它能决定的只有 Root 名、Plan 和相对路径，而 Root 名必须在白名单里。
type Resolver struct {
	home     string
	projects map[string]string
}

// NewResolver 构造展开器。
func NewResolver(home string, projects []Project) *Resolver {
	m := make(map[string]string, len(projects))
	for _, p := range projects {
		m[p.ID] = p.Path
	}
	return &Resolver{home: home, projects: m}
}

// Resolve 把一条落点展开成本机绝对路径（需求 2.3）。
//
// 任何越界情况都返回错误，由调用方拒绝执行并上报，而不是“尽量写”。
func (r *Resolver) Resolve(t proto.Target) (string, error) {
	if t.Plan == proto.PlanUser && t.Root == "" && t.Path != "" && filepath.IsAbs(t.Path) {
		// 删除下发用绝对路径表达（云端在删除时只能引用它上次记下的绝对路径）。
		return r.checkAbsolute(t.Path)
	}
	dirs, ok := hosts[t.Root]
	if !ok {
		return "", fmt.Errorf("%w: 宿主 %q 不在白名单内", ErrNotAllowed, t.Root)
	}
	if t.Path == "" {
		return "", fmt.Errorf("%w: 落点路径为空", ErrNotAllowed)
	}

	var base string
	switch t.Plan {
	case proto.PlanUser:
		if dirs.userDir == "" {
			return "", fmt.Errorf("%w: 宿主 %q 不支持用户平面", ErrNotAllowed, t.Root)
		}
		base = filepath.Join(r.home, dirs.userDir)
	case proto.PlanProject:
		if dirs.projectDir == "" {
			return "", fmt.Errorf("%w: 宿主 %q 不支持项目平面", ErrNotAllowed, t.Root)
		}
		root, ok := r.projects[t.ProjectID]
		if !ok {
			return "", fmt.Errorf("%w: 项目 %q 未在本机登记", ErrNotAllowed, t.ProjectID)
		}
		if err := checkProjectRoot(r.home, root); err != nil {
			return "", err
		}
		if t.Path == FileMCP && dirs.projectMCP != "" {
			return filepath.Join(root, filepath.FromSlash(dirs.projectMCP)), nil
		}
		base = filepath.Join(root, dirs.projectDir)
	default:
		return "", fmt.Errorf("%w: 未知平面 %q", ErrNotAllowed, t.Plan)
	}

	if t.Path == FileMCP {
		if !dirs.jsonKey {
			return "", fmt.Errorf("%w: 宿主 %q 的 MCP 配置不支持按键合并", ErrNotAllowed, t.Root)
		}
		if t.Plan == proto.PlanUser && dirs.userMCP != "" {
			return filepath.Join(r.home, filepath.FromSlash(dirs.userMCP)), nil
		}
		return filepath.Join(base, FileMCP), nil
	}

	// 类别目录只允许这三个，未知类别一律拒绝：这样云端无法用 ".." 之外的
	// 新类别把文件写进宿主目录里别的子目录。
	top, rest, ok := splitCategory(t.Path)
	if !ok {
		return "", fmt.Errorf("%w: 落点 %q 的类别不受支持", ErrNotAllowed, t.Path)
	}
	dir := filepath.Join(base, top)
	if rest == "" {
		return "", fmt.Errorf("%w: 落点 %q 缺少文件名", ErrNotAllowed, t.Path)
	}
	full := filepath.Join(dir, filepath.FromSlash(rest))
	if err := ensureWithin(dir, full); err != nil {
		return "", err
	}
	return full, nil
}

// splitCategory 拆分 "类别/其余路径"。
func splitCategory(p string) (category, rest string, ok bool) {
	i := strings.IndexByte(p, '/')
	if i <= 0 || i == len(p)-1 {
		return "", "", false
	}
	category = p[:i]
	switch category {
	case DirSkills, DirRules, DirEnv:
		return category, p[i+1:], true
	}
	return "", "", false
}

// checkAbsolute 校验删除下发里的绝对路径仍是白名单落点。
//
// 删除时必须按绝对路径定位（代理不保存下发清单，需求 5.2），
// 但也正因为它来自云端，必须再验一次：路径要位于 home 或某个已登记项目之下，
// 且不能是 home、项目根这类“整目录”本身。
func (r *Resolver) checkAbsolute(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: 删除路径 %q 不是绝对路径", ErrNotAllowed, p)
	}
	clean := filepath.Clean(p)
	if clean == r.home || clean == string(filepath.Separator) {
		return "", fmt.Errorf("%w: 删除路径 %q 指向根目录", ErrNotAllowed, p)
	}
	for _, root := range append([]string{r.home}, r.projectsList()...) {
		if root == "" {
			continue
		}
		if isWithin(root, clean) && filepath.Clean(root) != clean {
			return clean, nil
		}
	}
	return "", fmt.Errorf("%w: 删除路径 %q 不在白名单落点内", ErrNotAllowed, p)
}

func (r *Resolver) projectsList() []string {
	out := make([]string, 0, len(r.projects))
	for _, p := range r.projects {
		out = append(out, p)
	}
	return out
}

// checkProjectRoot 校验项目根是用户目录之下的绝对路径（需求 23.4）。
func checkProjectRoot(home, root string) error {
	if root == "" {
		return fmt.Errorf("%w: 项目路径为空", ErrNotAllowed)
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("%w: 项目路径 %q 不是绝对路径", ErrNotAllowed, root)
	}
	if !isWithin(home, root) || filepath.Clean(home) == filepath.Clean(root) {
		return fmt.Errorf("%w: 项目路径 %q 必须位于用户目录之下", ErrNotAllowed, root)
	}
	return nil
}

// ensureWithin 确认 full 位于 dir 之内。
func ensureWithin(dir, full string) error {
	if !isWithin(dir, full) || filepath.Clean(dir) == filepath.Clean(full) {
		return fmt.Errorf("%w: 路径 %q 越出落点目录", ErrNotAllowed, full)
	}
	return nil
}

// isWithin 报告 child 是否位于 parent 之下（含相等）。
func isWithin(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// HomeDir 返回当前用户目录。
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("取用户目录失败: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("用户目录为空")
	}
	return home, nil
}

// OSArch 返回当前平台标识，用于上报清单与服务端选择二进制。
func OSArch() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}

// cleanRel 清理相对路径并拒绝上跳，供上报清单使用。
func cleanRel(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}
