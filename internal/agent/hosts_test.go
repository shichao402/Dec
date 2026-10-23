package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpsertJSONOnlyTouchesDec 验证写入只改 `dec` 一条（需求 2.2）。
//
// 这条是硬约束：配置文件由用户与其它工具共用，把别人的条目改坏
// 比配置没写上更糟。所以除了内容，键顺序也要原样保留。
func TestUpsertJSONOnlyTouchesDec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	original := `{
  "mcpServers": {
    "alpha": {
      "url": "https://alpha.example.com/mcp"
    },
    "dec": {
      "url": "http://127.0.0.1:47654/mcp"
    },
    "zeta": {
      "command": "node",
      "args": ["server.mjs"]
    }
  },
  "otherSetting": true
}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	if err := upsertJSONEntry(path, "cursor", "http://127.0.0.1:8787/mcp"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("写出来的不是合法 JSON: %v\n%s", err, data)
	}

	servers := got["mcpServers"].(map[string]any)
	if len(servers) != 3 {
		t.Fatalf("条目数应为 3，实际 %d: %v", len(servers), servers)
	}
	if url := servers["alpha"].(map[string]any)["url"]; url != "https://alpha.example.com/mcp" {
		t.Fatalf("alpha 条目被改动: %v", servers["alpha"])
	}
	if cmd := servers["zeta"].(map[string]any)["command"]; cmd != "node" {
		t.Fatalf("zeta 条目被改动: %v", servers["zeta"])
	}
	if url := servers["dec"].(map[string]any)["url"]; url != "http://127.0.0.1:8787/mcp" {
		t.Fatalf("dec 条目未更新: %v", servers["dec"])
	}
	if got["otherSetting"] != true {
		t.Fatalf("顶层其它字段被改动: %v", got)
	}

	// 键顺序必须保持：alpha 在 dec 之前，dec 在 zeta 之前。
	text := string(data)
	if !(strings.Index(text, `"alpha"`) < strings.Index(text, `"dec"`) &&
		strings.Index(text, `"dec"`) < strings.Index(text, `"zeta"`)) {
		t.Fatalf("键顺序被打乱:\n%s", text)
	}
}

// TestDeleteJSONOnlyRemovesDec 验证移除只删 `dec`（需求 2.5）。
func TestDeleteJSONOnlyRemovesDec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	original := `{"mcpServers":{"alpha":{"url":"https://a"},"dec":{"url":"http://127.0.0.1:8787/mcp"},"zeta":{"command":"x"}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	if err := deleteJSONEntry(path); err != nil {
		t.Fatalf("移除失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	var got struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("写出来的不是合法 JSON: %v", err)
	}
	if _, ok := got.MCPServers["dec"]; ok {
		t.Fatal("dec 条目应已移除")
	}
	if len(got.MCPServers) != 2 {
		t.Fatalf("应只剩 2 条，实际 %d: %v", len(got.MCPServers), got.MCPServers)
	}
}

// TestUpsertJSONCreatesFile 验证配置文件不存在时能创建。
func TestUpsertJSONCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	if err := upsertJSONEntry(path, "cursor", "http://127.0.0.1:8787/mcp"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	var got struct {
		MCPServers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("写出来的不是合法 JSON: %v\n%s", err, data)
	}
	if got.MCPServers["dec"].URL != "http://127.0.0.1:8787/mcp" {
		t.Fatalf("dec 条目内容不对: %+v", got.MCPServers)
	}
}

// TestUpsertTOMLOnlyTouchesDec 验证 TOML 写入只改 dec 表（需求 2.2）。
func TestUpsertTOMLOnlyTouchesDec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := `# 我的 Codex 配置
model = "gpt-5"

[mcp_servers.alpha]
url = "https://alpha.example.com/mcp"

[mcp_servers.dec]
url = "http://127.0.0.1:47654/mcp"

[other]
key = "value"
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	if err := upsertTOMLEntry(path, "http://127.0.0.1:8787/mcp"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "# 我的 Codex 配置") {
		t.Fatalf("注释被丢掉:\n%s", text)
	}
	if !strings.Contains(text, `model = "gpt-5"`) {
		t.Fatalf("顶层键被改动:\n%s", text)
	}
	if !strings.Contains(text, `[mcp_servers.alpha]`) || !strings.Contains(text, `url = "https://alpha.example.com/mcp"`) {
		t.Fatalf("alpha 表被改动:\n%s", text)
	}
	if !strings.Contains(text, `url = "http://127.0.0.1:8787/mcp"`) {
		t.Fatalf("dec 表未更新:\n%s", text)
	}
	if strings.Contains(text, "47654") {
		t.Fatalf("旧地址还在:\n%s", text)
	}
	if !strings.Contains(text, "[other]") || !strings.Contains(text, `key = "value"`) {
		t.Fatalf("其它表被改动:\n%s", text)
	}
	// dec 表只能出现一次。
	if n := strings.Count(text, "[mcp_servers.dec]"); n != 1 {
		t.Fatalf("dec 表出现 %d 次:\n%s", n, text)
	}
}

// TestUpsertTOMLAppendsWhenMissing 验证没有 dec 表时追加到末尾。
func TestUpsertTOMLAppendsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "model = \"gpt-5\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	if err := upsertTOMLEntry(path, "http://127.0.0.1:8787/mcp"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "model = \"gpt-5\"") {
		t.Fatalf("原有内容被丢掉:\n%s", text)
	}
	if !strings.Contains(text, "[mcp_servers.dec]") {
		t.Fatalf("dec 表没有追加:\n%s", text)
	}
	if !strings.Contains(text, `url = "http://127.0.0.1:8787/mcp"`) {
		t.Fatalf("地址不对:\n%s", text)
	}
}

// TestDeleteTOMLRemovesOnlyDec 验证 TOML 删除只移除 dec 表。
func TestDeleteTOMLRemovesOnlyDec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := `model = "gpt-5"

[mcp_servers.alpha]
url = "https://alpha.example.com/mcp"

[mcp_servers.dec]
url = "http://127.0.0.1:8787/mcp"

[other]
key = "value"
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	if err := deleteTOMLEntry(path); err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Contains(text, "[mcp_servers.dec]") {
		t.Fatalf("dec 表应已移除:\n%s", text)
	}
	if !strings.Contains(text, "[mcp_servers.alpha]") {
		t.Fatalf("alpha 表被误删:\n%s", text)
	}
	if !strings.Contains(text, "[other]") {
		t.Fatalf("other 表被误删:\n%s", text)
	}
}

// TestSyncSkipsMissingHosts 验证没装的宿主不会创建目录（需求 2.3）。
func TestSyncSkipsMissingHosts(t *testing.T) {
	home := t.TempDir()
	// 只创建 cursor 的目录，模拟只装了 Cursor 的机器。
	cursorDir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}

	results := syncHosts(DefaultHosts(home), "http://127.0.0.1:8787/mcp")
	byName := map[string]HostResult{}
	for _, r := range results {
		byName[r.Name] = r
	}

	if byName["cursor"].Status != HostWritten {
		t.Fatalf("cursor 应已写入，实际 %v（%s）", byName["cursor"].Status, byName["cursor"].Reason)
	}
	for _, name := range []string{"claude", "codex"} {
		if byName[name].Status != HostSkipped {
			t.Fatalf("%s 应被跳过，实际 %v", name, byName[name].Status)
		}
	}

	// 关键：跳过不能等于「顺手建了个目录」。
	for _, name := range []string{".claude", ".codex"} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			t.Fatalf("不应为未安装的宿主创建目录 %s", name)
		}
	}
}

// TestSyncUpdatesPort 验证端口变化后条目被更新（需求 2.4）。
func TestSyncUpdatesPort(t *testing.T) {
	home := t.TempDir()
	cursorDir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}

	hosts := DefaultHosts(home)
	syncHosts(hosts, "http://127.0.0.1:8787/mcp")
	syncHosts(hosts, "http://127.0.0.1:9001/mcp")

	data, err := os.ReadFile(filepath.Join(cursorDir, "mcp.json"))
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "8787") {
		t.Fatalf("旧端口没有被替换:\n%s", text)
	}
	if !strings.Contains(text, "9001") {
		t.Fatalf("新端口没有写入:\n%s", text)
	}
	if n := strings.Count(text, `"dec"`); n != 1 {
		t.Fatalf("dec 条目出现 %d 次:\n%s", n, text)
	}
}

// TestRemoveHostsRemovesOnlyDec 验证移除只删 dec（需求 2.5）。
func TestRemoveHostsRemovesOnlyDec(t *testing.T) {
	home := t.TempDir()
	cursorDir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	path := filepath.Join(cursorDir, "mcp.json")
	original := `{"mcpServers":{"alpha":{"url":"https://a"},"dec":{"url":"http://127.0.0.1:8787/mcp"}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	results := removeHosts(DefaultHosts(home))
	byName := map[string]HostResult{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if byName["cursor"].Status != HostRemoved {
		t.Fatalf("cursor 应已移除，实际 %v", byName["cursor"].Status)
	}

	data, _ := os.ReadFile(path)
	var got struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("写出来的不是合法 JSON: %v", err)
	}
	if _, ok := got.MCPServers["dec"]; ok {
		t.Fatal("dec 条目应已移除")
	}
	if _, ok := got.MCPServers["alpha"]; !ok {
		t.Fatal("alpha 条目被误删")
	}
}

// TestClaudeEntryHasType 验证 Claude 条目带 type=http。
//
// Claude Code 的 HTTP 传输必须显式声明 type，只给 url 会被当作未知类型。
func TestClaudeEntryHasType(t *testing.T) {
	entry := entryFor("claude", "http://127.0.0.1:8787/mcp")
	if entry["type"] != "http" {
		t.Fatalf("claude 条目应带 type=http，实际 %+v", entry)
	}
	// Cursor 多写不认识的字段会在它自己的 UI 里显示成未知配置。
	if _, ok := entryFor("cursor", "http://127.0.0.1:8787/mcp")["type"]; ok {
		t.Fatal("cursor 条目不应带 type")
	}
}

// TestUpsertJSONRejectsBrokenFile 验证坏 JSON 不被覆盖。
//
// 用户手工编辑时漏个逗号是常事。此时如果直接写入，会把他整个配置换成
// 只含 dec 一条的文件——所有其它 MCP 服务器一起消失。
func TestUpsertJSONRejectsBrokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	broken := `{"mcpServers":{"alpha":{"url":"https://a"},}}`
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	if err := upsertJSONEntry(path, "cursor", "http://127.0.0.1:8787/mcp"); err == nil {
		t.Fatal("坏 JSON 应报错而不是覆盖")
	}
	data, _ := os.ReadFile(path)
	if string(data) != broken {
		t.Fatalf("文件被改动了:\n%s", data)
	}
}
