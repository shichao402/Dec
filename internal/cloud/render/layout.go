// Package render 实现云端渲染与单向下发引擎（需求 4、5、6、23）。
//
// 服务端在这里做三件事：
//
//  1. 按设备的用户平面订阅与项目订阅，把 bundle 里的资产渲染成最终字节内容，
//     包括密钥解密内联与设备变量替换。
//  2. 把渲染结果表达成“逻辑落点”（宿主名 + 相对路径），真实绝对路径由代理展开，
//     因为同一台机器上不同 IDE 的目录布局不同，项目根目录也只有代理知道。
//  3. 与代理上报的哈希对账，只下发内容有变化的文件，保证幂等。
//
// 明文密钥只在这次渲染的内存里存在：不写进仓库，不写进任何缓存。
package render

import (
	"fmt"
	"sort"

	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// 落点相对路径约定。
//
// Target.Path 的第一段是“类别目录”，代理据此选择展开基准：
//
//	skills/...  → IDE 的 skills 目录
//	rules/...   → IDE 的 rules 目录
//	mcp.json    → IDE 的 MCP 配置文件（只改 dec- 前缀的键）
//	env/...     → Dec 自己的 env 目录
//
// 这样同一份 Target 结构既能表达 IDE 资产，也能表达 Dec 自有文件，
// 不需要在协议里再加一个“资产类型”字段。
const (
	// RootDec 是 Dec 自己拥有的落点根，用于不归属任何 IDE 的文件（需求 23.2 之外的补充）。
	RootDec = "dec"
	// DirSkills 是 skill 条目的落点目录。
	DirSkills = "skills"
	// DirRules 是 rule 条目的落点目录。
	DirRules = "rules"
	// DirEnv 是 env 条目的落点目录。
	DirEnv = "env"
	// FileMCP 是 MCP 配置文件名。
	FileMCP = "mcp.json"
)

// allHosts 是服务端认识的宿主。名字必须与代理侧 internal/agent 的宿主表一致，
// 否则代理无法把宿主名展开成目录。
var allHosts = []string{"cursor", "claude", "codebuddy", "codex", "with"}

// jsonKeyHosts 是 MCP 配置为 JSON、可以用 json_key 合并格式下发的宿主。
//
// Codex 的 MCP 配置是 TOML（.codex/config.toml），协议层目前只有 whole 和 json_key
// 两种落盘方式，没有 TOML 段合并，所以这里不参与 MCP 下发。
// 需要覆盖 Codex 时再给协议加一个 toml_section 格式，不要在渲染层假装它是 JSON。
var jsonKeyHosts = []string{"cursor", "claude", "codebuddy", "with"}

func knownHost(name string) bool {
	for _, h := range allHosts {
		if h == name {
			return true
		}
	}
	return false
}

func supportsJSONKey(name string) bool {
	for _, h := range jsonKeyHosts {
		if h == name {
			return true
		}
	}
	return false
}

// hostsFor 返回一条条目应该渲染到哪些宿主。
//
// 空 Host 表示全部已启用的宿主。服务端不知道各设备装了哪些 IDE，
// 所以把“全部已知宿主”当作已启用；写多了不会出错，写少了才是问题。
func hostsFor(item ssot.ManifestItem) ([]string, error) {
	switch item.Kind {
	case ssot.KindEnv:
		// env 是 Dec 自有的文件，不跟随任何 IDE。
		return []string{RootDec}, nil
	}

	base := allHosts
	if item.Kind == ssot.KindMCP {
		base = jsonKeyHosts
	}
	if len(item.Host) == 0 {
		out := make([]string, len(base))
		copy(out, base)
		sort.Strings(out)
		return out, nil
	}

	seen := map[string]bool{}
	var out []string
	for _, h := range item.Host {
		if !knownHost(h) {
			return nil, fmt.Errorf("%w: 条目 %q 声明了未知宿主 %q", assets.ErrSchema, item.Path, h)
		}
		if item.Kind == ssot.KindMCP && !supportsJSONKey(h) {
			// 明确点名了不支持合并格式的宿主，属于配置错误，而不是静默跳过。
			return nil, fmt.Errorf("%w: 条目 %q 不能渲染到宿主 %q，该宿主的 MCP 配置不是 JSON",
				assets.ErrSchema, item.Path, h)
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	sort.Strings(out)
	return out, nil
}

// piece 是一个待下发的文件单元。
type piece struct {
	// id 是稳定标识，同时用于回执比对和状态记录。
	id string
	// rel 是相对宿主平面根的路径，见上面的类别目录约定。
	rel string
	// raw 是原始字节，密钥与变量还没替换。
	raw []byte
	// src 是这份内容在 bundle 里的源文件路径（相对 bundle 根），
	// 写进说明行，告诉读到副本的人去哪里改（ADR 0053）。
	src string
	// forceSecret 表示与内容无关、天然含密钥的落点（env）。
	forceSecret bool
}

// piecesOf 把一条 manifest 条目展开成若干文件单元。
//
// skill 是目录型资产，一个条目对应多个文件；其余类型一个条目对应一个文件。
func piecesOf(bundle string, item ssot.ManifestItem, files map[string][]byte) ([]piece, error) {
	id := "asset:" + bundle + ":" + item.Path
	switch item.Kind {
	case ssot.KindSkill:
		prefix := item.Path + "/"
		var names []string
		for p := range files {
			if len(p) > len(prefix) && p[:len(prefix)] == prefix {
				names = append(names, p)
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("%w: skill 条目 %q 目录下没有文件", assets.ErrSchema, item.Path)
		}
		sort.Strings(names)
		out := make([]piece, 0, len(names))
		for _, name := range names {
			out = append(out, piece{
				id:  id + "/" + name[len(prefix):],
				rel: DirSkills + "/" + item.Name + "/" + name[len(prefix):],
				raw: files[name],
				src: name,
			})
		}
		return out, nil

	case ssot.KindRule:
		raw, ok := files[item.Path]
		if !ok {
			return nil, fmt.Errorf("%w: rule 条目 %q 的内容文件缺失", assets.ErrSchema, item.Path)
		}
		return []piece{{
			id:  id,
			rel: DirRules + "/" + baseName(item.Path),
			raw: raw,
			src: item.Path,
		}}, nil

	case ssot.KindMCP:
		raw, ok := files[item.Path]
		if !ok {
			return nil, fmt.Errorf("%w: mcp 条目 %q 的内容文件缺失", assets.ErrSchema, item.Path)
		}
		// MCP 条目写进共享配置文件里的单个键，路径固定是配置文件本身。
		return []piece{{
			id:  id,
			rel: FileMCP,
			raw: raw,
			src: item.Path,
		}}, nil

	case ssot.KindEnv:
		raw, ok := files[item.Path]
		if !ok {
			return nil, fmt.Errorf("%w: env 条目 %q 的内容文件缺失", assets.ErrSchema, item.Path)
		}
		return []piece{{
			id:          id,
			rel:         DirEnv + "/" + item.Name,
			raw:         raw,
			src:         item.Path,
			forceSecret: true,
		}}, nil
	}
	return nil, fmt.Errorf("%w: 条目 %q 的类型 %q 不受支持", assets.ErrSchema, item.Path, item.Kind)
}

// baseName 返回路径的最后一段，用于 rule 的落点文件名。
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// hasPlane 报告条目是否属于指定平面。
func hasPlane(item ssot.ManifestItem, plane ssot.Plane) bool {
	for _, p := range item.Planes {
		if p == plane {
			return true
		}
	}
	return false
}
