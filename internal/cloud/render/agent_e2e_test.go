package render_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shichao402/Dec/internal/agent"
	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/render"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 这个文件存在的理由：此前的服务端测试全部自己伪造"代理上报的哈希"，
// 用 ssot.SumHex(下发内容) 当作代理会算出什么。这个假设恰恰是错的——
// 代理当年会在落盘时补一行说明，真实哈希与伪造的哈希永远不等，
// 而对账逻辑本身完全正确，所以所有测试都是绿的。
//
// 2026-10-23 的后果：单台设备每轮对账都判定为"有差异"，把同一批文件
// 反复重写，审计被刷到 8.4 万条、.git 涨到 63M，最终写满磁盘让整机失联。
//
// 所以这里不再伪造哈希：用真实的 agent.Writer 落盘，用真实的
// agent.HashFile 读回，喂给真实的 render.Sync。链路两端都是生产代码。

// testEnv 把服务端与代理放进同一个临时环境。
type testEnv struct {
	store  *ssot.Store
	assets *assets.Writer
	render *render.Renderer
	home   string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	store, err := ssot.Open(filepath.Join(dir, "ssot"))
	if err != nil {
		t.Fatalf("打开 SSOT 失败: %v", err)
	}
	key, err := crypt.NewKey(nil)
	if err != nil {
		t.Fatalf("生成根密钥失败: %v", err)
	}
	r, err := render.New(render.Options{
		Store:    store,
		StateDir: filepath.Join(dir, "state"),
		RootKey:  func() *crypt.Key { return key },
	})
	if err != nil {
		t.Fatalf("构造渲染器失败: %v", err)
	}
	// 代理的落点根与项目根都放在临时目录，避免碰到真实 IDE 配置。
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("建测试主目录失败: %v", err)
	}
	return &testEnv{store: store, assets: assets.NewWriter(store), render: r, home: home}
}

func (e *testEnv) writeBundle(t *testing.T, name string, files []assets.AssetFile, manifest string) {
	t.Helper()
	if _, err := e.assets.WriteAsset(assets.WriteAssetInput{
		Bundle:   name,
		Files:    files,
		Manifest: []byte(manifest),
		Author:   ssot.Author{Name: "dev-1", Source: "mcp"},
	}); err != nil {
		t.Fatalf("写入 bundle 失败: %v", err)
	}
}

func (e *testEnv) registerDevice(t *testing.T, dev ssot.Device) {
	t.Helper()
	if _, err := e.store.Update(ssot.Author{Name: "dev-1", Source: "mcp"}, func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.DeviceTable](tx.Store(), ssot.FileDevices)
		if err != nil {
			return "", err
		}
		table.Devices = append(table.Devices, dev)
		return "登记设备", tx.WriteJSON(ssot.FileDevices, table)
	}); err != nil {
		t.Fatalf("登记设备失败: %v", err)
	}
}

// applyWithRealWriter 用真实代理落盘，然后回执并返回代理下一轮会报的清单。
//
// 关键点：清单里的哈希由 agent.HashFile 从磁盘读回来算，不是复制下发内容。
func applyWithRealWriter(t *testing.T, e *testEnv, batch *proto.Batch) ([]proto.AckResult, []proto.ManifestEntry) {
	t.Helper()
	writer := agent.NewWriter(agent.NewResolver(e.home, nil), e.home)

	var results []proto.AckResult
	var manifest []proto.ManifestEntry
	for _, f := range batch.Files {
		res, err := writer.Apply(f)
		if err != nil {
			t.Fatalf("代理落盘 %s 失败: %v", f.ID, err)
		}
		results = append(results, proto.AckResult{
			ID: f.ID, OK: true, Hash: res.Hash, Paths: res.Paths,
		})
		// 下一轮代理会按上一轮的核对清单现场算哈希。
		h, err := agent.HashFile(res.Paths[0], f.Format, f.Key)
		if err != nil {
			t.Fatalf("代理计算哈希 %s 失败: %v", f.ID, err)
		}
		manifest = append(manifest, proto.ManifestEntry{
			ID: f.ID, Hash: h, Paths: res.Paths, Format: f.Format, Key: f.Key,
		})
	}
	return results, manifest
}

// 核心回归测试：一轮下发 + 真实落盘之后，第二轮必须无差异。
//
// 这就是当初该有却没有的那条测试。它对说明行的位置、代理是否偷改内容、
// 哈希算法两侧是否一致，全部敏感——任何一处漂移都会让第二轮又产生 batch。
func TestSyncConvergesWithRealAgent(t *testing.T) {
	e := newTestEnv(t)
	e.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "demo/SKILL.md", Content: []byte("# Demo\n")},
		{Path: "style.mdc", Content: []byte("---\ndescription: 风格\nglobs: ['*']\n---\nalways be terse\n")},
		{Path: "conf/app.yaml", Content: []byte("key: value\n")},
	}, `name: woa
items:
  - kind: skill
    path: demo
    name: demo-skill
    planes: [user]
  - kind: rule
    path: style.mdc
    name: style
    planes: [user]
  - kind: rule
    path: conf/app.yaml
    name: app-conf
    planes: [user]
`)

	d := ssot.Device{
		ID: "dev-1", Name: "测试机", Kind: ssot.DeviceMine,
		OS: "darwin", Arch: "arm64", UserBundles: []string{"woa"},
	}
	e.registerDevice(t, d)

	first, err := e.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	if first.Batch == nil || len(first.Batch.Files) == 0 {
		t.Fatalf("首次 Sync 应下发内容，实际 %+v", first.Batch)
	}

	results, manifest := applyWithRealWriter(t, e, first.Batch)
	if err := e.render.States().RecordAck(d.ID, first.Batch.Revision, results); err != nil {
		t.Fatalf("记录回执失败: %v", err)
	}

	// 第二轮：代理按真实哈希对账，服务端不该再有任何要下发的东西。
	//
	// 说明行若加在代理侧，这里就会非空——这正是当时故障的表现。
	second, err := e.render.Sync(d, proto.SyncRequest{
		Revision: first.Batch.Revision,
		Manifest: manifest,
	})
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if second.Batch != nil {
		var ids []string
		for _, f := range second.Batch.Files {
			ids = append(ids, f.ID)
		}
		t.Fatalf("落盘后对账应无差异，实际仍要下发 %d 个文件: %v\n"+
			"原因通常是代理写下的字节与收到的字节不一致（说明行被重复添加）",
			len(second.Batch.Files), ids)
	}
	if len(second.Overrides) != 0 {
		t.Fatalf("不应产生覆盖记录，实际 %+v", second.Overrides)
	}
}

// 落点内容必须能通过原始格式解析。
//
// 说明行曾经无条件用 HTML 注释：YAML 里那是非法语法，整份文件解析失败；
// .mdc 带 front matter 时插在前面会让规则失效。这两类问题不会让
// Go 测试失败，只会让用户的 IDE 悄悄不加载配置。
func TestNoticeKeepsFilesParseable(t *testing.T) {
	e := newTestEnv(t)
	e.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "rule.mdc", Content: []byte("---\ndescription: d\nglobs: ['*.go']\n---\nbody\n")},
		{Path: "conf.yaml", Content: []byte("a: 1\n")},
		{Path: "note.md", Content: []byte("# 标题\n")},
	}, `name: woa
items:
  - kind: rule
    path: rule.mdc
    name: mdc-rule
    planes: [user]
  - kind: rule
    path: conf.yaml
    name: yaml-conf
    planes: [user]
  - kind: rule
    path: note.md
    name: md-note
    planes: [user]
`)
	d := ssot.Device{
		ID: "dev-2", Name: "测试机", Kind: ssot.DeviceMine,
		OS: "darwin", Arch: "arm64", UserBundles: []string{"woa"},
	}
	e.registerDevice(t, d)
	resp, err := e.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	for _, f := range resp.Batch.Files {
		switch {
		case filepath.Ext(f.ID) == ".yaml":
			// YAML 注释必须以 # 开头，不能出现 <!--。
			if containsHTMLComment(f.Content) {
				t.Errorf("YAML 落点不应使用 HTML 注释: %q", f.Content)
			}
			if !hasPrefixLine(f.Content, "# ") {
				t.Errorf("YAML 落点的说明应是 # 注释: %q", f.Content)
			}
		case filepath.Ext(f.ID) == ".mdc":
			// front matter 必须在第一行。
			if !hasPrefixLine(f.Content, "---") {
				t.Errorf(".mdc 落点的第一行必须是 front matter 起始标记: %q", f.Content)
			}
		}
	}
}

func containsHTMLComment(s string) bool {
	for i := 0; i+4 <= len(s); i++ {
		if s[i:i+4] == "<!--" {
			return true
		}
	}
	return false
}

func hasPrefixLine(s, prefix string) bool {
	for i := 0; i+len(prefix) <= len(s); i++ {
		if s[i:i+len(prefix)] == prefix {
			return true
		}
		if s[i] == '\n' {
			return false
		}
	}
	return false
}
