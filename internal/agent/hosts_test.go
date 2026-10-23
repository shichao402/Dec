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

// TestInspectHostsReportsThreeStates 验证 mcp-config 读到的状态三种语义（需求 2.6）。
//
// `dec-agent mcp-config` 是排障入口，它给出的结论只有三种：
// 已写入（配置里有 dec）、已跳过（目录/文件不存在或没有条目）、失败（配置坏了）。
// 这三种必须能区分开，否则用户看到「没写」时不知道该装 IDE 还是修 JSON/TOML。
func TestInspectHostsReportsThreeStates(t *testing.T) {
	home := t.TempDir()

	// cursor：目录与文件都在，且已经写入 dec 条目 → 已写入
	cursorDir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatalf("准备 cursor 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "mcp.json"),
		[]byte(`{"mcpServers":{"dec":{"url":"http://127.0.0.1:8787/mcp"}}}`), 0o644); err != nil {
		t.Fatalf("准备 cursor 配置失败: %v", err)
	}

	// claude：目录在、配置也在，但没有 mcpServers → 已跳过
	// 注意 Claude 的用户级 MCP 配置在 ~/.claude.json，~/.claude 只是数据目录。
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("准备 claude 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatalf("准备 claude 配置失败: %v", err)
	}

	// codex：目录不存在 → 已跳过，且原因要指到目录
	// 这是「IDE 没装」的正常情况，必须与「配置坏了」区分开。

	// 失败状态另起一个家目录：同一个家目录里既要「目录不存在」又要「解析失败」
	// 得靠不同宿主，而 DefaultHosts 只有三个宿主，塞不下第四个状态。
	badHome := t.TempDir()
	badDir := filepath.Join(badHome, ".cursor")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("准备坏配置目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "mcp.json"), []byte(`{"mcpServers":`), 0o644); err != nil {
		t.Fatalf("准备坏配置失败: %v", err)
	}

	byName := map[string]HostResult{}
	for _, res := range inspectHosts(DefaultHosts(home)) {
		byName[res.Name] = res
	}

	if got := byName["cursor"]; got.Status != HostWritten || !strings.Contains(got.Entry, "8787") {
		t.Fatalf("cursor 应为已写入且带出条目内容，实际 %v entry=%q reason=%q",
			got.Status, got.Entry, got.Reason)
	}
	if got := byName["claude"]; got.Status != HostSkipped || !strings.Contains(got.Reason, "没有 dec 条目") {
		t.Fatalf("claude 应为已跳过并说明没有条目，实际 %v reason=%q", got.Status, got.Reason)
	}
	if got := byName["codex"]; got.Status != HostSkipped || !strings.Contains(got.Reason, "配置目录不存在") {
		t.Fatalf("codex 应为已跳过并说明目录不存在，实际 %v reason=%q", got.Status, got.Reason)
	}

	var bad *HostResult
	badResults := inspectHosts(DefaultHosts(badHome))
	for i := range badResults {
		if badResults[i].Name == "cursor" {
			bad = &badResults[i]
		}
	}
	if bad == nil || bad.Status != HostFailed {
		t.Fatalf("坏 JSON 应报告失败，实际 %+v", badResults)
	}

	// 读操作不能顺手改文件：坏 JSON 必须原样留在那里。
	data, err := os.ReadFile(filepath.Join(badDir, "mcp.json"))
	if err != nil || string(data) != `{"mcpServers":` {
		t.Fatalf("InspectHosts 不应修改配置文件，实际 %q err=%v", data, err)
	}
}

// TestRemoveHostsRemovesOnlyDec 验证移除只删 `dec`（需求 2.5）。
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
