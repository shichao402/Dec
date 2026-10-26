package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/notice"
	"github.com/shichao402/Dec/internal/proto"
)

// 测试夹具。

type fixture struct {
	store  *ssot.Store
	writer *assets.Writer
	key    *crypt.Key
	render *Renderer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	store, err := ssot.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	key, err := crypt.NewKey(nil)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	r, err := New(Options{
		Store:    store,
		StateDir: t.TempDir(),
		RootKey:  func() *crypt.Key { return key },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &fixture{store: store, writer: assets.NewWriter(store), key: key, render: r}
}

func (f *fixture) author() ssot.Author {
	return ssot.Author{Name: "dev-1", Source: "mcp"}
}

// writeBundle 写入一个 bundle。manifest 与文件由调用方给出。
func (f *fixture) writeBundle(t *testing.T, name string, files []assets.AssetFile, manifest string) {
	t.Helper()
	if _, err := f.writer.WriteAsset(assets.WriteAssetInput{
		Bundle:   name,
		Files:    files,
		Manifest: []byte(manifest),
		Author:   f.author(),
	}); err != nil {
		t.Fatalf("WriteAsset(%s): %v", name, err)
	}
}

func (f *fixture) putSecret(t *testing.T, name, value string) {
	t.Helper()
	if _, err := f.writer.PutSecret(f.key, assets.PutSecretInput{
		Name:   name,
		Value:  value,
		Author: f.author(),
	}); err != nil {
		t.Fatalf("PutSecret(%s): %v", name, err)
	}
}

// registerDevice 直接改 devices.json，避免依赖 identity 包。
func (f *fixture) registerDevice(t *testing.T, dev ssot.Device) {
	t.Helper()
	if _, err := f.store.Update(f.author(), func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.DeviceTable](tx.Store(), ssot.FileDevices)
		if err != nil {
			return "", err
		}
		table.Devices = append(table.Devices, dev)
		return "登记设备", tx.WriteJSON(ssot.FileDevices, table)
	}); err != nil {
		t.Fatalf("登记设备: %v", err)
	}
}

// registerProject 直接改 projects.json。
func (f *fixture) registerProject(t *testing.T, p ssot.Project) {
	t.Helper()
	if _, err := f.store.Update(f.author(), func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.ProjectTable](tx.Store(), ssot.FileProjects)
		if err != nil {
			return "", err
		}
		table.Projects = append(table.Projects, p)
		return "登记项目", tx.WriteJSON(ssot.FileProjects, table)
	}); err != nil {
		t.Fatalf("登记项目: %v", err)
	}
}

func dev(id string, bundles ...string) ssot.Device {
	return ssot.Device{
		ID:          id,
		Name:        "测试机 " + id,
		Kind:        ssot.DeviceMine,
		OS:          "darwin",
		Arch:        "arm64",
		UserBundles: bundles,
	}
}

// 用例。

// TestRenderSkillAndRule 覆盖 skill 目录展开、rule 单文件、宿主展开。
func TestRenderSkillAndRule(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "demo/SKILL.md", Content: []byte("# Demo\n")},
		{Path: "demo/run.sh", Content: []byte("echo hi\n")},
		{Path: "style.mdc", Content: []byte("always be terse\n")},
	}, `name: woa
items:
  - kind: skill
    path: demo
    name: demo-skill
    planes: [user, project]
  - kind: rule
    path: style.mdc
    name: style
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))
	f.registerProject(t, ssot.Project{ID: "p1", DeviceID: "dev-1", Name: "proj", Path: "/tmp/proj", Bundles: []string{"woa"}})

	out, err := f.render.RenderDevice(dev("dev-1", "woa"))
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	if len(out.Alerts) != 0 {
		t.Fatalf("不应有告警: %+v", out.Alerts)
	}
	byID := map[string]File{}
	for _, file := range out.Files {
		byID[file.File.ID] = file
	}

	// skill 的每个文件都是一条独立下发单元。
	skillMD, ok := byID["asset:woa:demo/SKILL.md"]
	if !ok {
		t.Fatalf("缺少 SKILL.md 的下发项，实际 ID：%v", keysOf(byID))
	}
	// 说明行由服务端渲染时写入（需求 6.4），代理不再补。
	if want := "<!-- " + notice.Text + " -->\n# Demo\n"; skillMD.File.Content != want {
		t.Fatalf("SKILL.md 内容错误: %q", skillMD.File.Content)
	}
	// 两个平面 × 五种宿主 = 10 个落点。
	if len(skillMD.File.Targets) != 10 {
		t.Fatalf("skill 落点数应为 10，实际 %d: %+v", len(skillMD.File.Targets), skillMD.File.Targets)
	}
	var projectTargets int
	for _, tg := range skillMD.File.Targets {
		if tg.Plan == proto.PlanProject {
			projectTargets++
			if tg.ProjectID != "p1" {
				t.Fatalf("项目平面落点缺少 ProjectID: %+v", tg)
			}
		}
		if !strings.HasPrefix(tg.Path, DirSkills+"/demo-skill/") {
			t.Fatalf("skill 落点路径应以 skills/demo-skill/ 开头: %q", tg.Path)
		}
	}
	if projectTargets != 5 {
		t.Fatalf("项目平面落点应为 5，实际 %d", projectTargets)
	}

	// rule 只声明了 user 平面，落点是 rules/style.mdc。
	rule, ok := byID["asset:woa:style.mdc"]
	if !ok {
		t.Fatalf("缺少 rule 下发项")
	}
	if len(rule.File.Targets) != 5 {
		t.Fatalf("rule 落点数应为 5，实际 %d", len(rule.File.Targets))
	}
	if rule.File.Targets[0].Path != DirRules+"/style.mdc" {
		t.Fatalf("rule 落点路径错误: %q", rule.File.Targets[0].Path)
	}
	if rule.File.Format != proto.FormatWhole {
		t.Fatalf("rule 应是 whole 格式: %q", rule.File.Format)
	}
}

func keysOf(m map[string]File) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestRenderInlinesSecret 覆盖密钥内联与 0600 标记。
func TestRenderInlinesSecret(t *testing.T) {
	f := newFixture(t)
	f.putSecret(t, "api-token", "s3cr3t-value")
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "env/dev", Content: []byte("TOKEN={{secret:api-token}}\n")},
	}, `name: woa
items:
  - kind: env
    path: env/dev
    name: dev-env
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))

	out, err := f.render.RenderDevice(dev("dev-1", "woa"))
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	if len(out.Files) != 1 {
		t.Fatalf("应只有一条下发，实际 %d", len(out.Files))
	}
	got := out.Files[0].File
	if got.Content != "TOKEN=s3cr3t-value\n" {
		t.Fatalf("密钥未内联: %q", got.Content)
	}
	if !got.Secret {
		t.Fatal("含密钥的文件必须标记 Secret")
	}
	if got.Mode != 0o600 {
		t.Fatalf("含密钥的文件权限应为 0600，实际 %#o", got.Mode)
	}
	// env 不跟随 IDE，只落在 dec 根。
	if len(got.Targets) != 1 || got.Targets[0].Root != RootDec {
		t.Fatalf("env 落点错误: %+v", got.Targets)
	}
}

// TestRenderSkipsOnMissingSecret 覆盖需求 4.5：解密失败时不渲染，而不是渲染成空。
func TestRenderSkipsOnMissingSecret(t *testing.T) {
	f := newFixture(t)
	f.putSecret(t, "api-token", "v1")
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "env/dev", Content: []byte("TOKEN={{secret:api-token}}\n")},
	}, `name: woa
items:
  - kind: env
    path: env/dev
    name: dev-env
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))

	// 首次渲染成功。
	first, err := f.render.RenderDevice(dev("dev-1", "woa"))
	if err != nil {
		t.Fatalf("首次渲染: %v", err)
	}
	if len(first.Files) != 1 {
		t.Fatalf("首次应有一条下发")
	}

	// 删掉密钥后，该条目必须整体跳过并告警。
	if _, err := f.writer.DeleteSecret(assets.PutSecretInput{Name: "api-token", Author: f.author()}, true); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	second, err := f.render.RenderDevice(dev("dev-1", "woa"))
	if err != nil {
		t.Fatalf("二次渲染: %v", err)
	}
	if len(second.Files) != 0 {
		t.Fatalf("密钥缺失时不应渲染任何内容，实际 %d 条", len(second.Files))
	}
	if len(second.Alerts) != 1 {
		t.Fatalf("应有 1 条告警，实际 %d: %+v", len(second.Alerts), second.Alerts)
	}
	if !strings.Contains(second.Alerts[0].Reason, "api-token") {
		t.Fatalf("告警应指明缺失的密钥: %q", second.Alerts[0].Reason)
	}
}

// TestRenderMCPAsJSONKey 覆盖 MCP 条目走 json_key 且键名带 dec- 前缀。
func TestRenderMCPAsJSONKey(t *testing.T) {
	f := newFixture(t)
	f.putSecret(t, "mcp-token", "tok-123")
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "tavily.json", Content: []byte(`{"mcpServers":{"tavily":{"command":"npx","env":{"TOKEN":"{{secret:mcp-token}}"}}}}`)},
	}, `name: woa
items:
  - kind: mcp
    path: tavily.json
    name: tavily
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))

	out, err := f.render.RenderDevice(dev("dev-1", "woa"))
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	if len(out.Files) != 1 {
		t.Fatalf("应只有一条下发，实际 %d（告警 %+v）", len(out.Files), out.Alerts)
	}
	got := out.Files[0].File
	if got.Format != proto.FormatJSONKey {
		t.Fatalf("MCP 应为 json_key 格式: %q", got.Format)
	}
	if got.Key != "dec-tavily" {
		t.Fatalf("MCP 键名应为 dec-tavily，实际 %q", got.Key)
	}
	if !got.Secret {
		t.Fatal("MCP 条目引用了密钥，必须标记 Secret")
	}
	if got.Targets[0].Path != FileMCP {
		t.Fatalf("MCP 落点应为 mcp.json: %q", got.Targets[0].Path)
	}
	// 内容应只是单个 server 定义，不含 mcpServers 外壳。
	var server map[string]any
	if err := json.Unmarshal([]byte(got.Content), &server); err != nil {
		t.Fatalf("内容不是合法 JSON: %v", err)
	}
	if _, ok := server["mcpServers"]; ok {
		t.Fatalf("内容不应包含 mcpServers 外壳: %s", got.Content)
	}
	if !strings.Contains(got.Content, "tok-123") {
		t.Fatalf("密钥未内联: %s", got.Content)
	}
}

// TestRenderMCPRejectsTOMLHost 覆盖显式点名不支持 JSON 合并的宿主时报错。
func TestRenderMCPRejectsTOMLHost(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "x.json", Content: []byte(`{"command":"npx"}`)},
	}, `name: woa
items:
  - kind: mcp
    path: x.json
    name: x
    planes: [user]
    host: [codex]
`)
	f.registerDevice(t, dev("dev-1", "woa"))

	out, err := f.render.RenderDevice(dev("dev-1", "woa"))
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	if len(out.Files) != 0 {
		t.Fatalf("不应渲染到 codex，实际 %d 条", len(out.Files))
	}
	if len(out.Alerts) == 0 {
		t.Fatal("应给出告警")
	}
}

// TestRenderRespectsSubscriptions 覆盖订阅过滤：没订阅就不下发。
func TestRenderRespectsSubscriptions(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("a\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	f.writeBundle(t, "other", []assets.AssetFile{
		{Path: "b.mdc", Content: []byte("b\n")},
	}, `name: other
items:
  - kind: rule
    path: b.mdc
    name: rule-b
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))

	out, err := f.render.RenderDevice(dev("dev-1", "woa"))
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	if len(out.Files) != 1 || out.Files[0].File.ID != "asset:woa:a.mdc" {
		t.Fatalf("只应下发已订阅的 bundle，实际 %+v", out.Files)
	}
}

// TestSyncIsIdempotent 覆盖需求 4.4：内容一致时不下发。
func TestSyncIsIdempotent(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("a\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))
	d := dev("dev-1", "woa")

	// 首次对账：本地什么都没有，应下发。
	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	if first.Batch == nil || len(first.Batch.Files) != 1 {
		t.Fatalf("首次对账应下发 1 条，实际 %+v", first.Batch)
	}
	sent := first.Batch.Files[0]
	hash := ssot.SumHex([]byte(sent.Content))

	// 回执落地。
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, []proto.AckResult{
		{ID: sent.ID, OK: true, Hash: hash, Paths: []string{"/home/u/.cursor/rules/a.mdc"}},
	}); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	// 第二次对账：本地哈希已一致，不应再下发。
	second, err := f.render.Sync(d, proto.SyncRequest{
		Revision: first.Batch.Revision,
		Manifest: []proto.ManifestEntry{{
			ID: sent.ID, Hash: hash, Paths: []string{"/home/u/.cursor/rules/a.mdc"},
			Format: sent.Format, Key: sent.Key,
		}},
	})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if second.Batch != nil {
		t.Fatalf("内容一致时不应下发，实际 %+v", second.Batch)
	}
	if len(second.Overrides) != 0 {
		t.Fatalf("内容一致时不应有覆盖记录: %+v", second.Overrides)
	}
}

// TestSyncDetectsLocalOverride 覆盖需求 6.2：本地被改，直接覆盖并记录事件。
func TestSyncDetectsLocalOverride(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("cloud\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))
	d := dev("dev-1", "woa")

	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	sent := first.Batch.Files[0]
	goodHash := ssot.SumHex([]byte(sent.Content))
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, []proto.AckResult{
		{ID: sent.ID, OK: true, Hash: goodHash, Paths: []string{"/p/a.mdc"}},
	}); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	// 本地内容被改成别的：上报的哈希不同。
	localHash := ssot.SumHex([]byte("local edit\n"))
	resp, err := f.render.Sync(d, proto.SyncRequest{
		Revision: first.Batch.Revision,
		Manifest: []proto.ManifestEntry{{
			ID: sent.ID, Hash: localHash, Paths: []string{"/p/a.mdc"},
			Format: sent.Format, Key: sent.Key,
		}},
	})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if resp.Batch == nil || len(resp.Batch.Files) != 1 {
		t.Fatalf("被本地改动时应重新下发，实际 %+v", resp.Batch)
	}
	if want := "<!-- " + notice.Text + " -->\ncloud\n"; resp.Batch.Files[0].Content != want {
		t.Fatalf("应下发云端内容，实际 %q", resp.Batch.Files[0].Content)
	}
	if len(resp.Overrides) != 1 {
		t.Fatalf("应记录一条覆盖事件，实际 %d", len(resp.Overrides))
	}
	if resp.Overrides[0].ID != sent.ID || resp.Overrides[0].Hash != localHash {
		t.Fatalf("覆盖事件内容错误: %+v", resp.Overrides[0])
	}
}

// TestSyncDeletesRemovedItem 覆盖条目被删除后的下发删除。
func TestSyncDeletesRemovedItem(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("a\n")},
		{Path: "b.mdc", Content: []byte("b\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
  - kind: rule
    path: b.mdc
    name: rule-b
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))
	d := dev("dev-1", "woa")

	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	if len(first.Batch.Files) != 2 {
		t.Fatalf("首次应下发 2 条，实际 %d", len(first.Batch.Files))
	}
	var results []proto.AckResult
	var manifest []proto.ManifestEntry
	for _, fl := range first.Batch.Files {
		h := ssot.SumHex([]byte(fl.Content))
		p := "/p/" + fl.ID
		results = append(results, proto.AckResult{ID: fl.ID, OK: true, Hash: h, Paths: []string{p}})
		manifest = append(manifest, proto.ManifestEntry{ID: fl.ID, Hash: h, Paths: []string{p}, Format: fl.Format, Key: fl.Key})
	}
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, results); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	// 删掉 b.mdc。
	if _, err := f.writer.WriteAsset(assets.WriteAssetInput{
		Bundle: "woa",
		Files: []assets.AssetFile{
			{Path: "a.mdc", Content: []byte("a\n")},
		},
		Manifest: []byte(`name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`),
		Author: f.author(),
	}); err != nil {
		t.Fatalf("重写 bundle: %v", err)
	}

	resp, err := f.render.Sync(d, proto.SyncRequest{Revision: first.Batch.Revision, Manifest: manifest})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if resp.Batch == nil {
		t.Fatal("应产生删除下发")
	}
	if len(resp.Batch.Files) != 0 {
		t.Fatalf("a.mdc 未变，不应重发，实际 %d", len(resp.Batch.Files))
	}
	if len(resp.Batch.Deletes) != 1 {
		t.Fatalf("应下发 1 条删除，实际 %d", len(resp.Batch.Deletes))
	}
	del := resp.Batch.Deletes[0]
	if del.ID != "asset:woa:b.mdc" {
		t.Fatalf("删除目标错误: %q", del.ID)
	}
	if len(del.Targets) != 1 || del.Targets[0].Path != "/p/asset:woa:b.mdc" {
		t.Fatalf("删除落点应带上上次的绝对路径: %+v", del.Targets)
	}
}

// TestAffectedDevices 覆盖写入后决定推给谁。
func TestAffectedDevices(t *testing.T) {
	f := newFixture(t)
	f.registerDevice(t, dev("dev-user", "woa"))
	f.registerDevice(t, dev("dev-proj"))
	f.registerDevice(t, dev("dev-none", "other"))
	f.registerProject(t, ssot.Project{ID: "p1", DeviceID: "dev-proj", Name: "p", Path: "/tmp/p", Bundles: []string{"woa"}})

	got, err := f.render.Affected("woa")
	if err != nil {
		t.Fatalf("Affected: %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, d := range got {
		ids = append(ids, d.ID)
	}
	if len(ids) != 2 {
		t.Fatalf("应命中 2 台设备，实际 %v", ids)
	}
	if ids[0] != "dev-proj" || ids[1] != "dev-user" {
		t.Fatalf("命中设备错误: %v", ids)
	}
}

// TestRenderDeviceRejectsRevoked 覆盖吊销设备不再下发。
func TestRenderDeviceRejectsRevoked(t *testing.T) {
	f := newFixture(t)
	d := dev("dev-1", "woa")
	d.Revoked = true
	f.registerDevice(t, d)
	if _, err := f.render.RenderDevice(d); err == nil {
		t.Fatal("已吊销设备不应渲染")
	}
}

// TestRenderWarnsMissingBundle 覆盖订阅了不存在的 bundle 时只告警不阻塞。
func TestRenderWarnsMissingBundle(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("a\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa", "ghost"))

	out, err := f.render.RenderDevice(dev("dev-1", "woa", "ghost"))
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	if len(out.Files) != 1 {
		t.Fatalf("正常 bundle 仍应下发，实际 %d", len(out.Files))
	}
	if len(out.Alerts) != 1 || !strings.Contains(out.Alerts[0].ID, "ghost") {
		t.Fatalf("应对不存在的 bundle 告警: %+v", out.Alerts)
	}
}

// TestRenderDeviceVariables 覆盖设备变量替换。
func TestRenderDeviceVariables(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("id={{device.id}} os={{device.os}} arch={{device.arch}} name={{device.name}}\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))

	out, err := f.render.RenderDevice(dev("dev-1", "woa"))
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	if len(out.Files) != 1 {
		t.Fatalf("应有一条下发")
	}
	want := "<!-- " + notice.Text + " -->\nid=dev-1 os=darwin arch=arm64 name=测试机 dev-1\n"
	if out.Files[0].File.Content != want {
		t.Fatalf("变量替换错误:\n got %q\nwant %q", out.Files[0].File.Content, want)
	}
	if out.Files[0].File.Secret {
		t.Fatal("不含密钥的文件不该标记 Secret")
	}
}

// TestDrift 覆盖管理面的漂移计数。
func TestDrift(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("a\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))
	d := dev("dev-1", "woa")

	want, err := f.render.RenderDevice(d)
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	// 状态为空：所有条目都算漂移。
	drift, err := f.render.States().Drift("dev-1", want)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if drift != 1 {
		t.Fatalf("初始漂移应为 1，实际 %d", drift)
	}

	// 落地后漂移为 0。
	var results []proto.AckResult
	for _, fl := range want.Files {
		results = append(results, proto.AckResult{ID: fl.File.ID, OK: true, Hash: fl.Hash})
	}
	if err := f.render.States().RecordAck("dev-1", want.Revision, results); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}
	drift, err = f.render.States().Drift("dev-1", want)
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if drift != 0 {
		t.Fatalf("落地后漂移应为 0，实际 %d", drift)
	}
}

// TestDeleteAckClearsState 覆盖删除回执后不再重复产生删除下发。
//
// 删除没有内容可哈希，回执用空 Hash 表达。如果状态里不清理，
// 每次对账都会重复下发同一条删除。
func TestDeleteAckClearsState(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("a\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	d := dev("dev-1", "woa")
	f.registerDevice(t, d)

	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	sent := first.Batch.Files[0]
	h := ssot.SumHex([]byte(sent.Content))
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, []proto.AckResult{
		{ID: sent.ID, OK: true, Hash: h, Paths: []string{"/p/a.mdc"}},
	}); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	// 订阅取消，条目不再期望。
	if _, err := f.store.Update(f.author(), func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.DeviceTable](tx.Store(), ssot.FileDevices)
		if err != nil {
			return "", err
		}
		table.Devices[0].UserBundles = nil
		return "取消订阅", tx.WriteJSON(ssot.FileDevices, table)
	}); err != nil {
		t.Fatalf("取消订阅: %v", err)
	}
	unsub := dev("dev-1")
	manifest := []proto.ManifestEntry{{ID: sent.ID, Hash: h, Paths: []string{"/p/a.mdc"}, Format: sent.Format, Key: sent.Key}}

	resp, err := f.render.Sync(unsub, proto.SyncRequest{Revision: first.Batch.Revision, Manifest: manifest})
	if err != nil {
		t.Fatalf("取消订阅后 Sync: %v", err)
	}
	if resp.Batch == nil || len(resp.Batch.Deletes) != 1 {
		t.Fatalf("应下发 1 条删除，实际 %+v", resp.Batch)
	}
	delID := resp.Batch.Deletes[0].ID

	// 代理确认删除：Hash 为空。
	if err := f.render.States().RecordAck("dev-1", resp.Batch.Revision, []proto.AckResult{
		{ID: delID, OK: true},
	}); err != nil {
		t.Fatalf("RecordAck(delete): %v", err)
	}

	// 再次对账不应再产生删除。
	again, err := f.render.Sync(unsub, proto.SyncRequest{Revision: resp.Batch.Revision, Manifest: nil})
	if err != nil {
		t.Fatalf("三次 Sync: %v", err)
	}
	if again.Batch != nil {
		t.Fatalf("删除已确认后不应重复下发，实际 %+v", again.Batch)
	}
}

// TestRecordPlanKeepsDeleteTarget 覆盖“代理未回执就断线”的场景：
// 下发计划必须记进状态，否则条目删除后会留下孤儿文件。
func TestRecordPlanKeepsDeleteTarget(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("a\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	d := dev("dev-1", "woa")
	f.registerDevice(t, d)

	// 首次对账产生下发，但代理没有回执（直接断线）。
	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	sent := first.Batch.Files[0]

	// 状态里应已有这条条目的形状记录，但没有哈希（未确认落地）。
	st, err := f.render.States().Load("dev-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rec, ok := st.Files[sent.ID]
	if !ok {
		t.Fatalf("下发计划应记进状态，实际 %+v", st.Files)
	}
	if rec.Hash != "" {
		t.Fatalf("未回执时不应有落地哈希，实际 %q", rec.Hash)
	}

	// 条目被删除：仍应能算出删除目标。
	if _, err := f.writer.WriteAsset(assets.WriteAssetInput{
		Bundle:   "woa",
		Files:    nil,
		Manifest: []byte("name: woa\nitems: []\n"),
		Author:   f.author(),
	}); err != nil {
		t.Fatalf("清空 bundle: %v", err)
	}
	resp, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if resp.Batch == nil || len(resp.Batch.Deletes) != 1 {
		t.Fatalf("应下发删除，实际 %+v", resp.Batch)
	}
}

// TestOverridesDeduplicated 覆盖重复检测同一次改动时不重复记录。
func TestOverridesDeduplicated(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("cloud\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	d := dev("dev-1", "woa")
	f.registerDevice(t, d)

	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	sent := first.Batch.Files[0]
	h := ssot.SumHex([]byte(sent.Content))
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, []proto.AckResult{
		{ID: sent.ID, OK: true, Hash: h, Paths: []string{"/p/a.mdc"}},
	}); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	local := ssot.SumHex([]byte("local\n"))
	req := proto.SyncRequest{
		Revision: first.Batch.Revision,
		Manifest: []proto.ManifestEntry{{
			ID: sent.ID, Hash: local, Paths: []string{"/p/a.mdc"}, Format: sent.Format, Key: sent.Key,
		}},
	}
	// 连续两次对账（代理都没成功回执），只应记录一条覆盖事件。
	for i := 0; i < 2; i++ {
		if _, err := f.render.Sync(d, req); err != nil {
			t.Fatalf("Sync #%d: %v", i, err)
		}
	}
	list, err := f.render.States().Overrides("dev-1")
	if err != nil {
		t.Fatalf("Overrides: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("同一次改动只应记录一次，实际 %d 条: %+v", len(list), list)
	}
}

// TestSyncKeepsLastGoodOnRenderFailure 覆盖需求 4.5 的关键语义：
// 密钥解密失败时，落点必须保留上一次成功的内容，绝不能被当作“不再期望”而删除。
func TestSyncKeepsLastGoodOnRenderFailure(t *testing.T) {
	f := newFixture(t)
	f.putSecret(t, "tok", "v1")
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "env/dev", Content: []byte("TOKEN={{secret:tok}}\n")},
	}, `name: woa
items:
  - kind: env
    path: env/dev
    name: dev-env
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))
	d := dev("dev-1", "woa")

	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	sent := first.Batch.Files[0]
	h := ssot.SumHex([]byte(sent.Content))
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, []proto.AckResult{
		{ID: sent.ID, OK: true, Hash: h, Paths: []string{"/p/env/dev"}},
	}); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	// 删掉密钥：渲染必然失败。
	if _, err := f.writer.DeleteSecret(assets.PutSecretInput{Name: "tok", Author: f.author()}, true); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}

	resp, err := f.render.Sync(d, proto.SyncRequest{
		Revision: first.Batch.Revision,
		Manifest: []proto.ManifestEntry{{
			ID: sent.ID, Hash: h, Paths: []string{"/p/env/dev"}, Format: sent.Format, Key: sent.Key,
		}},
	})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if resp.Batch != nil {
		t.Fatalf("渲染失败时不应下发，也不应删除，实际 %+v", resp.Batch)
	}
	// 状态里这条条目必须还在，否则下次会误判成孤儿。
	st, err := f.render.States().Load("dev-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := st.Files[sent.ID]; !ok {
		t.Fatalf("渲染失败的条目不应从状态里消失: %+v", st.Files)
	}
}

// TestSyncUsesReportedPathsForDelete 覆盖删除优先用代理上报的现场路径。
func TestSyncUsesReportedPathsForDelete(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("a\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	d := dev("dev-1", "woa")
	f.registerDevice(t, d)

	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	sent := first.Batch.Files[0]
	h := ssot.SumHex([]byte(sent.Content))
	// 回执记下的是旧路径。
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, []proto.AckResult{
		{ID: sent.ID, OK: true, Hash: h, Paths: []string{"/old/a.mdc"}},
	}); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	// 取消订阅，代理这次上报的是新路径（用户挪了 home）。
	if _, err := f.store.Update(f.author(), func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.DeviceTable](tx.Store(), ssot.FileDevices)
		if err != nil {
			return "", err
		}
		table.Devices[0].UserBundles = nil
		return "取消订阅", tx.WriteJSON(ssot.FileDevices, table)
	}); err != nil {
		t.Fatalf("取消订阅: %v", err)
	}
	resp, err := f.render.Sync(dev("dev-1"), proto.SyncRequest{
		Revision: first.Batch.Revision,
		Manifest: []proto.ManifestEntry{{
			ID: sent.ID, Hash: h, Paths: []string{"/new/a.mdc"}, Format: sent.Format, Key: sent.Key,
		}},
	})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if resp.Batch == nil || len(resp.Batch.Deletes) != 1 {
		t.Fatalf("应下发删除，实际 %+v", resp.Batch)
	}
	got := resp.Batch.Deletes[0].Targets
	if len(got) != 1 || got[0].Path != "/new/a.mdc" {
		t.Fatalf("删除应使用代理上报的现场路径，实际 %+v", got)
	}
}

// TestSyncRecordsOverrideWhenHashUnreported 覆盖 ADR 0047 的连带回归：
// 多落点条目只要有一份被改，代理就报空哈希；覆盖必须照样发生，
// 也必须在审计里留下一条记录。空哈希不能被当成「没改」。
func TestSyncRecordsOverrideWhenHashUnreported(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("cloud\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))
	d := dev("dev-1", "woa")

	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	sent := first.Batch.Files[0]
	goodHash := ssot.SumHex([]byte(sent.Content))
	paths := []string{"/p/.cursor/a.mdc", "/p/.with/a.mdc"}
	if err := f.render.States().RecordAck("dev-1", first.Batch.Revision, []proto.AckResult{
		{ID: sent.ID, OK: true, Hash: goodHash, Paths: paths},
	}); err != nil {
		t.Fatalf("RecordAck: %v", err)
	}

	// 代理报空哈希：多落点里有一份被改过，它保证不了这一条的状态。
	resp, err := f.render.Sync(d, proto.SyncRequest{
		Revision: first.Batch.Revision,
		Manifest: []proto.ManifestEntry{{
			ID: sent.ID, Hash: "", Paths: paths,
			Format: sent.Format, Key: sent.Key,
		}},
	})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if resp.Batch == nil || len(resp.Batch.Files) != 1 {
		t.Fatalf("空哈希应触发补发，实际 %+v", resp.Batch)
	}
	if len(resp.Overrides) != 1 {
		t.Fatalf("空哈希也必须记一条覆盖事件，实际 %d 条", len(resp.Overrides))
	}
	if resp.Overrides[0].ID != sent.ID {
		t.Fatalf("覆盖事件条目错误: %+v", resp.Overrides[0])
	}
	if len(resp.Overrides[0].Paths) != len(paths) {
		t.Fatalf("覆盖事件应带上代理上报的全部落点，实际 %+v", resp.Overrides[0].Paths)
	}
}

// TestSyncFirstTimeEmptyHashIsNotOverride 首次下发就报空哈希时不该记覆盖：
// 那不是「被本地改过」，只是这一条还没有落地记录。
func TestSyncFirstTimeEmptyHashIsNotOverride(t *testing.T) {
	f := newFixture(t)
	f.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "a.mdc", Content: []byte("cloud\n")},
	}, `name: woa
items:
  - kind: rule
    path: a.mdc
    name: rule-a
    planes: [user]
`)
	f.registerDevice(t, dev("dev-1", "woa"))
	d := dev("dev-1", "woa")

	first, err := f.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	if len(first.Overrides) != 0 {
		t.Fatalf("首次下发不应有覆盖记录，实际 %+v", first.Overrides)
	}
}
