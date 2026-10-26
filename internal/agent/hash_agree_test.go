package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/agent"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 这个文件盯的是一个完全没有外部症状的缺陷。
//
// MCP 条目走 json_key 格式下发：服务端把条目在源文件里的原始字节算成哈希，
// 代理把落盘后容器里的字节算成哈希。源文件是缩进两行的
//
//	{
//	  "url": "https://..."
//	}
//
// 落盘后是紧凑的 {"url":"https://..."}。两个哈希永不可能相等，
// 于是每一轮对账都判定为漂移：设备列表长期显示 drift=1、同一条 MCP
// 被反复重发，而文件内容其实一直是对的，人眼完全看不出问题。
//
// 所以这里的断言只有一条：服务端算出什么，代理就必须算出什么。
// 不去断言具体的哈希值——那会把测试绑死在某个序列化细节上，
// 而真正的契约是「两侧一致」。

// serverContent 是与真实源文件同形状的 MCP 条目：带缩进与换行。
const serverContent = "{\n  \"url\": \"https://mcpgw.example.com/x\"\n}\n"

// applyServerContent 用真实的 Writer 把服务端下发的字节落盘到 .cursor/mcp.json。
//
// 走 Writer 而不是直接写文件：落盘过程本身（合并容器、缩进输出）也会影响
// 最终字节，跳过它就等于跳过了半个被测对象。
func applyServerContent(t *testing.T, home, key, content string) string {
	t.Helper()
	w := agent.NewWriter(agent.NewResolver(home, nil), home)
	res, err := w.Apply(proto.File{
		ID:      "asset:woa:mcp",
		Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "mcp.json"}},
		Content: content,
		Format:  proto.FormatJSONKey,
		Key:     key,
	})
	if err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	if len(res.Paths) != 1 {
		t.Fatalf("应有一个落点，实际 %d", len(res.Paths))
	}
	return res.Paths[0]
}

// TestServerAndAgentHashAgreeOnJSONEntry 是最核心的一致性断言。
//
// 服务端：proto.NormalizeJSONEntry(下发内容) → ssot.SumHex
// 代理：落盘后 agent.HashFile 读回
// 两者必须逐字节相等，否则就是 drift 的根因。
func TestServerAndAgentHashAgreeOnJSONEntry(t *testing.T) {
	home := t.TempDir()
	path := applyServerContent(t, home, "dec-x", serverContent)

	agentHash, err := agent.HashFile(path, proto.FormatJSONKey, "dec-x")
	if err != nil {
		t.Fatalf("代理算哈希失败: %v", err)
	}

	norm, err := proto.NormalizeJSONEntry([]byte(serverContent))
	if err != nil {
		t.Fatalf("服务端规范化失败: %v", err)
	}
	serverHash := ssot.SumHex(norm)

	if agentHash != serverHash {
		t.Fatalf("两端哈希必须相等，否则每轮对账都误判漂移\n代理: %s\n服务端: %s",
			agentHash, serverHash)
	}
	if agentHash == "" {
		t.Fatal("哈希不该为空")
	}
}

// TestAgentHashIgnoresWhitespaceInSource 覆盖「源文件改写排版但语义未变」。
//
// 云端把同一个条目重新缩进、调换键序后再下发，代理算出的哈希必须不变，
// 否则一次纯格式调整会触发全设备重写。
func TestAgentHashIgnoresWhitespaceInSource(t *testing.T) {
	home := t.TempDir()
	path := applyServerContent(t, home, "dec-x", serverContent)
	before, err := agent.HashFile(path, proto.FormatJSONKey, "dec-x")
	if err != nil {
		t.Fatalf("算哈希失败: %v", err)
	}

	// 同一语义、不同排版。
	reformatted := "{\"url\":\"https://mcpgw.example.com/x\"}"
	applyServerContent(t, home, "dec-x", reformatted)
	after, err := agent.HashFile(path, proto.FormatJSONKey, "dec-x")
	if err != nil {
		t.Fatalf("算哈希失败: %v", err)
	}
	if before != after {
		t.Fatalf("语义相同不应改变哈希：%q -> %q", before, after)
	}
}

// TestAgentHashDetectsEntryChange 覆盖条目真的被改时必须被发现。
//
// 上一条测试证明哈希忽略排版，这条证明它不是「任何变化都忽略」：
// url 变了，哈希必须变，否则本地改动永远不会被纠正。
func TestAgentHashDetectsEntryChange(t *testing.T) {
	home := t.TempDir()
	path := applyServerContent(t, home, "dec-x", serverContent)
	before, err := agent.HashFile(path, proto.FormatJSONKey, "dec-x")
	if err != nil {
		t.Fatalf("算哈希失败: %v", err)
	}

	local := "{\"url\":\"https://evil.example.com/x\"}"
	applyServerContent(t, home, "dec-x", local)
	after, err := agent.HashFile(path, proto.FormatJSONKey, "dec-x")
	if err != nil {
		t.Fatalf("算哈希失败: %v", err)
	}
	if before == after {
		t.Fatalf("条目被改必须改变哈希，实际仍为 %q", before)
	}
}

// TestAgentHashIgnoresOtherServers 覆盖用户在同一个文件里加自己的 MCP。
//
// 用户的键不属于 Dec，加多少都不该被算成 Dec 落点被改。
func TestAgentHashIgnoresOtherServers(t *testing.T) {
	home := t.TempDir()
	path := applyServerContent(t, home, "dec-x", serverContent)
	before, err := agent.HashFile(path, proto.FormatJSONKey, "dec-x")
	if err != nil {
		t.Fatalf("算哈希失败: %v", err)
	}

	// 用户自己加一条，并改顶层字段。
	withUser := `{"theme":"dark","mcpServers":{"dec-x":{"url":"https://mcpgw.example.com/x"},"mine":{"command":"npx"}}}`
	if err := os.WriteFile(path, []byte(withUser), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	after, err := agent.HashFile(path, proto.FormatJSONKey, "dec-x")
	if err != nil {
		t.Fatalf("算哈希失败: %v", err)
	}
	if before != after {
		t.Fatalf("用户自己的条目不应影响托管哈希：%q -> %q", before, after)
	}
}

// TestAgentHashMissingKeyIsEmpty 覆盖文件还在但托管条目被删掉。
//
// 空哈希是代理表达「我保证不了这条的状态」（ADR 0047），服务端据此补发。
// 如果这里返回一个有效哈希，条目被删就再也不会被补回来。
func TestAgentHashMissingKeyIsEmpty(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"mine":{"command":"x"}}}`), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	got, err := agent.HashFile(path, proto.FormatJSONKey, "dec-x")
	if err != nil {
		t.Fatalf("算哈希失败: %v", err)
	}
	if got != "" {
		t.Fatalf("条目不存在时应返回空哈希，实际 %q", got)
	}
}
