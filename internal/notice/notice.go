// Package notice 生成落点文件里的「由 Dec 管理」说明行（需求 6.4）。
//
// 说明行只在一处生成：服务端渲染。代理只负责把收到的字节原样写下。
//
// 为什么必须只有一处：对账的前提是两侧对同一串字节达成一致。代理自己在
// 落盘时再加一行，它按"自己写下的内容"算哈希，服务端按"未加说明的内容"
// 算哈希，两个值永远不等，于是每次对账都判定为差异并重发——2026-10 这次
// 故障就是这么来的：单台设备反复重写同一批文件，审计被刷到 8.4 万条。
//
// 把"哪些文件加、加什么形式"放在这个包里，而不是服务端与代理各写一份，
// 也是同一个道理：两份规则一旦漂移，就又是同一个 bug，只是更难发现。
package notice

import (
	"path"
	"strings"

	"github.com/shichao402/Dec/internal/proto"
)

// Text 是说明行的正文。不含任何敏感信息，可以出现在含密钥文件之外的一切落点。
//
// 它同时是「这个文件归 Dec 管」的识别标记：卸载扫描（agent.ScanManaged）与
// Apply 的幂等判断都按「是否包含 Text」来认。所以定位信息只能追加在 Text 之后，
// 不能改动 Text 本身，否则旧版本写下的落点会突然不被认领。
const Text = "由 Dec 管理，本地修改会被覆盖；要改请改云端。"

// Locator 指明一个落点在云端的源：哪个 bundle 下的哪个文件。
//
// 有了它，说明行就能直接写出修改指令（调哪个工具、填什么参数），
// 读到这份副本的 AI 不必自己推断源在哪里，也就不会去改本地副本或某个
// 看起来像源的旧目录（ADR 0053）。
type Locator struct {
	Bundle string
	// Path 是相对 bundle 根的文件路径，与 dec_edit_asset_file 的 path 参数一致。
	Path string
}

// EditTool 是说明行里指名的固化修改工具。
const EditTool = "dec_edit_asset_file"

// Line 返回说明行正文（不含注释符号）。定位信息不全时退回只有 Text 的形式。
func Line(loc Locator) string {
	if loc.Bundle == "" || loc.Path == "" || !safeInComment(loc.Bundle) || !safeInComment(loc.Path) {
		return Text
	}
	return Text + "修改方法：调用 MCP 工具 " + EditTool +
		"（bundle=" + loc.Bundle + "，path=" + loc.Path + "），或在 Dec 管理面的资产页编辑；不要改这份副本。"
}

// safeInComment 拒绝会提前结束注释或打断单行注释的字符。
// bundle 与路径在写入时已经校验过，这里是防御性的第二道。
func safeInComment(s string) bool {
	return !strings.Contains(s, "-->") && !strings.ContainsAny(s, "\r\n")
}

// syntax 返回某个落点适用的注释写法。
//
// 不能一律用 HTML 注释：YAML 里 <!-- --> 是非法语法，会让整份文件解析失败；
// .mdc 规则常以 front matter 开头，插在它前面同样会让规则失效（见 apply）。
//
// 没有扩展名的文件不写说明：无法判断它是脚本、配置还是纯文本，
// 任何注释都可能改变语义，而说明行本身是提示，不值得冒这个险。
func syntax(rel string) (prefix, suffix string, ok bool) {
	switch strings.ToLower(path.Ext(rel)) {
	case ".md", ".txt":
		return "<!-- ", " -->", true
	case ".mdc":
		// Cursor 规则，可能带 front matter，交给 apply 决定插入位置。
		return "<!-- ", " -->", true
	case ".yaml", ".yml":
		return "# ", "", true
	}
	return "", "", false
}

// Applies 报告某个落点是否需要说明行。
func Applies(rel string, secret bool, format proto.Format) bool {
	// json_key 只写一个键，文件不属于 Dec，往里加说明等于改别人的配置。
	// 含密钥的文件不加：说明行可能被 env 解析器当成变量赋值。
	if format != proto.FormatWhole || secret {
		return false
	}
	_, _, ok := syntax(rel)
	return ok
}

// Apply 在内容前面加上说明行，已经加过就原样返回（幂等）。
func Apply(rel, content string, secret bool, format proto.Format, loc Locator) string {
	if !Applies(rel, secret, format) {
		return content
	}
	if strings.Contains(content, Text) {
		// 内容里已经有说明：可能是用户自己抄的，也可能是升级前代理写下的。
		// 再加一次会变成两行，也就会让哈希变化一次。
		return content
	}
	prefix, suffix, _ := syntax(rel)
	line := prefix + Line(loc) + suffix + "\n"
	if pl := strings.ToLower(path.Ext(rel)); pl == ".mdc" || pl == ".md" {
		// front matter 必须留在文件第一行，否则规则的前置声明失效。
		if out, ok := insertAfterFrontMatter(content, line); ok {
			return out
		}
	}
	return line + content
}

// insertAfterFrontMatter 把 line 插到 YAML front matter 之后。
//
// front matter 是文件开头的 "---" 行到下一处 "---" 或 "..." 行之间的块，
// 常见于 Cursor 的 .mdc 规则（description / globs / alwaysApply）。
// 没有 front matter 时返回 ok=false，由调用方按行首插入处理。
func insertAfterFrontMatter(content, line string) (string, bool) {
	lines := strings.SplitAfter(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return content, false
	}
	for i := 1; i < len(lines); i++ {
		switch strings.TrimRight(lines[i], "\r\n") {
		case "---", "...":
			var b strings.Builder
			for _, l := range lines[:i+1] {
				b.WriteString(l)
			}
			if !strings.HasSuffix(b.String(), "\n") {
				// 结束标记正好在文件末尾，没有换行，补一个再写说明。
				b.WriteString("\n")
			}
			b.WriteString(line)
			for _, l := range lines[i+1:] {
				b.WriteString(l)
			}
			return b.String(), true
		}
	}
	return content, false
}
