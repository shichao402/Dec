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
    "dec_vault": {
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

	if err := upsertJSONEntry(path, "cursor", "http://127.0.0.1:43679/mcp"); err != nil {
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
	if url := servers["dec_vault"].(map[string]any)["url"]; url != "http://127.0.0.1:43679/mcp" {
		t.Fatalf("dec 条目未更新: %v", servers["dec_vault"])
	}
	if got["otherSetting"] != true {
		t.Fatalf("顶层其它字段被改动: %v", got)
	}

	// 键顺序必须保持：alpha 在 dec 之前，dec 在 zeta 之前。
	text := string(data)
	if !(strings.Index(text, `"alpha"`) < strings.Index(text, `"dec_vault"`) &&
		strings.Index(text, `"dec_vault"`) < strings.Index(text, `"zeta"`)) {
		t.Fatalf("键顺序被打乱:\n%s", text)
	}
}

// TestDeleteJSONOnlyRemovesDec 验证移除只删 `dec`（需求 2.5）。
func TestDeleteJSONOnlyRemovesDec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	original := `{"mcpServers":{"alpha":{"url":"https://a"},"dec_vault":{"url":"http://127.0.0.1:43679/mcp"},"zeta":{"command":"x"}}}`
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
	if _, ok := got.MCPServers["dec_vault"]; ok {
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

	if err := upsertJSONEntry(path, "cursor", "http://127.0.0.1:43679/mcp"); err != nil {
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
	if got.MCPServers["dec_vault"].URL != "http://127.0.0.1:43679/mcp" {
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

[mcp_servers.dec_vault]
url = "http://127.0.0.1:47654/mcp"

[other]
key = "value"
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	if err := upsertTOMLEntry(path, "http://127.0.0.1:43679/mcp"); err != nil {
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
	if !strings.Contains(text, `url = "http://127.0.0.1:43679/mcp"`) {
		t.Fatalf("dec 表未更新:\n%s", text)
	}
	if strings.Contains(text, "47654") {
		t.Fatalf("旧地址还在:\n%s", text)
	}
	if !strings.Contains(text, "[other]") || !strings.Contains(text, `key = "value"`) {
		t.Fatalf("其它表被改动:\n%s", text)
	}
	// dec 表只能出现一次。
	if n := strings.Count(text, "[mcp_servers.dec_vault]"); n != 1 {
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

	if err := upsertTOMLEntry(path, "http://127.0.0.1:43679/mcp"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "model = \"gpt-5\"") {
		t.Fatalf("原有内容被丢掉:\n%s", text)
	}
	if !strings.Contains(text, "[mcp_servers.dec_vault]") {
		t.Fatalf("dec 表没有追加:\n%s", text)
	}
	if !strings.Contains(text, `url = "http://127.0.0.1:43679/mcp"`) {
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

[mcp_servers.dec_vault]
url = "http://127.0.0.1:43679/mcp"

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
	if strings.Contains(text, "[mcp_servers.dec_vault]") {
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

	results := syncHosts(DefaultHosts(home), "http://127.0.0.1:43679/mcp")
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
	syncHosts(hosts, "http://127.0.0.1:43679/mcp")
	syncHosts(hosts, "http://127.0.0.1:9001/mcp")

	data, err := os.ReadFile(filepath.Join(cursorDir, "mcp.json"))
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "43679") {
		t.Fatalf("旧端口没有被替换:\n%s", text)
	}
	if !strings.Contains(text, "9001") {
		t.Fatalf("新端口没有写入:\n%s", text)
	}
	if n := strings.Count(text, `"dec_vault"`); n != 1 {
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
		[]byte(`{"mcpServers":{"dec_vault":{"url":"http://127.0.0.1:43679/mcp"}}}`), 0o644); err != nil {
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
	// 得靠不同宿主，而这个测试只准备了三个宿主的目录，塞不下第四个状态。
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

	if got := byName["cursor"]; got.Status != HostWritten || !strings.Contains(got.Entry, "43679") {
		t.Fatalf("cursor 应为已写入且带出条目内容，实际 %v entry=%q reason=%q",
			got.Status, got.Entry, got.Reason)
	}
	if got := byName["claude"]; got.Status != HostSkipped || !strings.Contains(got.Reason, "没有 dec_vault 条目") {
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
	original := `{"mcpServers":{"alpha":{"url":"https://a"},"dec_vault":{"url":"http://127.0.0.1:43679/mcp"}}}`
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
	if _, ok := got.MCPServers["dec_vault"]; ok {
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
	entry := entryFor("claude", "http://127.0.0.1:43679/mcp")
	if entry["type"] != "http" {
		t.Fatalf("claude 条目应带 type=http，实际 %+v", entry)
	}
	// Cursor 多写不认识的字段会在它自己的 UI 里显示成未知配置。
	if _, ok := entryFor("cursor", "http://127.0.0.1:43679/mcp")["type"]; ok {
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

	if err := upsertJSONEntry(path, "cursor", "http://127.0.0.1:43679/mcp"); err == nil {
		t.Fatal("坏 JSON 应报错而不是覆盖")
	}
	data, _ := os.ReadFile(path)
	if string(data) != broken {
		t.Fatalf("文件被改动了:\n%s", data)
	}
}

// TestSyncHostsIncludesWith 验证启动登记覆盖 With（ADR 0054）。
//
// 以前 DefaultHosts 只有 claude/codex/cursor，代理换端口后 With 里的 dec
// 条目一直指向旧地址，With 拿到的工具清单停在旧会话。
func TestSyncHostsIncludesWith(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".bg-agent", "config-with-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	path := filepath.Join(dir, "mcp_config.json")
	original := `{"mcpServers":{"other":{"url":"https://o"},"dec_vault":{"disabled":true,"url":"http://127.0.0.1:8787/mcp","transportType":"streamable-http"}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	var with *HostResult
	results := syncHosts(DefaultHosts(home), "http://127.0.0.1:43679/mcp")
	for i := range results {
		if results[i].Name == "with" {
			with = &results[i]
		}
	}
	if with == nil || with.Status != HostWritten {
		t.Fatalf("with 应已写入，实际 %+v", with)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	var got struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("解析失败: %v\n%s", err, data)
	}
	dec := got.MCPServers["dec_vault"]
	if dec["url"] != "http://127.0.0.1:43679/mcp" || dec["transportType"] != "streamable-http" {
		t.Fatalf("with 的 dec 条目不对: %v", dec)
	}
	// 用户关掉的开关不能被代理悄悄打开。
	if dec["disabled"] != true {
		t.Fatalf("disabled 应保留为 true: %v", dec)
	}
	if got.MCPServers["other"]["url"] != "https://o" {
		t.Fatalf("其它条目被改动: %v", got.MCPServers)
	}
}

// TestUpsertJSONDoesNotAddDisabled 验证原条目没有 disabled 时不凭空加上。
func TestUpsertJSONDoesNotAddDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := upsertJSONEntry(path, "with", "http://127.0.0.1:43679/mcp"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "disabled") {
		t.Fatalf("不应写入 disabled:\n%s", data)
	}
}

// TestUpsertJSONMigratesLegacyName 验证旧名 dec 迁到 dec_vault（ADR 0055）。
//
// 只写新名不删旧名，宿主里会剩一条地址过期的 dec，AI 看到两个同类入口，
// 其中一个永远连不上。旧条目的 disabled 也要继承，改名不等于重新启用。
func TestUpsertJSONMigratesLegacyName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	original := `{"mcpServers":{"alpha":{"url":"https://a"},"dec":{"disabled":true,"url":"http://old/mcp"},"dec-woa":{"url":"https://w"}}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}
	if err := upsertJSONEntry(path, "with", "http://127.0.0.1:43679/mcp"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	data, _ := os.ReadFile(path)
	var got struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("解析失败: %v\n%s", err, data)
	}
	if _, ok := got.MCPServers["dec"]; ok {
		t.Fatalf("旧名 dec 应已移除:\n%s", data)
	}
	nv := got.MCPServers["dec_vault"]
	if nv["url"] != "http://127.0.0.1:43679/mcp" || nv["disabled"] != true {
		t.Fatalf("dec_vault 条目不对（应继承 disabled）: %v", nv)
	}
	// 云端下发的 dec- 托管条目与用户条目都不能被波及。
	if got.MCPServers["dec-woa"]["url"] != "https://w" || got.MCPServers["alpha"]["url"] != "https://a" {
		t.Fatalf("其它条目被改动:\n%s", data)
	}
}

// TestUpsertTOMLMigratesLegacyName 验证 Codex 的 [mcp_servers.dec] 被换成新表。
func TestUpsertTOMLMigratesLegacyName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "model = \"gpt-5\"\n\n[mcp_servers.dec]\nurl = \"http://old/mcp\"\n\n[other]\nkey = \"value\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}
	if err := upsertTOMLEntry(path, "http://127.0.0.1:43679/mcp"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Contains(text, "[mcp_servers.dec]") || strings.Contains(text, "http://old/mcp") {
		t.Fatalf("旧表应已移除:\n%s", text)
	}
	if strings.Count(text, "[mcp_servers.dec_vault]") != 1 || !strings.Contains(text, `url = "http://127.0.0.1:43679/mcp"`) {
		t.Fatalf("新表不对:\n%s", text)
	}
	if !strings.Contains(text, "[other]") || !strings.Contains(text, `model = "gpt-5"`) {
		t.Fatalf("其它内容被改动:\n%s", text)
	}
}

// TestDeleteRemovesLegacyName 验证卸载同时删新旧两个名字，不留连不上的条目。
func TestDeleteRemovesLegacyName(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(jsonPath, []byte(`{"mcpServers":{"dec":{"url":"http://old"},"mine":{"url":"https://m"}}}`), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}
	if err := deleteJSONEntry(jsonPath); err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	data, _ := os.ReadFile(jsonPath)
	if strings.Contains(string(data), `"dec"`) || !strings.Contains(string(data), `"mine"`) {
		t.Fatalf("JSON 移除结果不对:\n%s", data)
	}

	tomlPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(tomlPath, []byte("[mcp_servers.dec]\nurl = \"http://old\"\n\n[mcp_servers.dec_vault]\nurl = \"http://new\"\n\n[other]\nk = 1\n"), 0o644); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}
	if err := deleteTOMLEntry(tomlPath); err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	data, _ = os.ReadFile(tomlPath)
	if strings.Contains(string(data), "mcp_servers.dec") || !strings.Contains(string(data), "[other]") {
		t.Fatalf("TOML 移除结果不对:\n%s", data)
	}
}

// TestEntryNameOutsideManagedPrefix 守住命名约束：内置入口不能落进 dec- 托管前缀。
func TestEntryNameOutsideManagedPrefix(t *testing.T) {
	if strings.HasPrefix(mcpEntryName, entryPrefix) {
		t.Fatalf("内置入口名 %q 落进了托管前缀 %q，会被同名下发覆盖并计入托管哈希", mcpEntryName, entryPrefix)
	}
}
