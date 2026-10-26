package render_test

import (
	"testing"

	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 这个文件补的是 agent_e2e_test.go 里那条真实链路测试的盲区：
// 它用的三个条目全是 whole 格式，MCP 条目（json_key）一个都没覆盖，
// 而这次的 drift 缺陷恰好只存在于 json_key 路径上。
//
// 症状回顾：设备列表长期 drift=1，同一条 MCP 被反复重发，
// 文件内容其实一直是对的。原因是服务端算「条目在源文件里的原始字节」，
// 代理算「落盘后容器里的字节」，两侧口径不同，永不可能相等。
// 全套测试都是绿的，因为没有任何一条测试把这两侧放在一起跑。

// TestSyncConvergesWithRealAgentOnMCP 是 MCP 条目的收敛测试。
//
// 断言的是第二件事：一轮下发 + 真实落盘 + 真实回执之后，
// 第二轮 Sync 不能再产生 batch，drift 也不能再是正的。
func TestSyncConvergesWithRealAgentOnMCP(t *testing.T) {
	e := newTestEnv(t)
	// 源文件刻意写成缩进两行的真实形状，而不是紧凑单行：
	// 缺陷只有在这种形状下才会暴露，写成紧凑的就是在测一个假场景。
	e.writeBundle(t, "woa", []assets.AssetFile{
		{Path: "mcp/gw.json", Content: []byte("{\n  \"url\": \"https://mcpgw.example.com/x\"\n}\n")},
	}, `name: woa
items:
  - kind: mcp
    path: mcp/gw.json
    name: gw
    planes: [user]
`)

	d := ssot.Device{
		ID: "dev-mcp", Name: "测试机", Kind: ssot.DeviceMine,
		OS: "darwin", Arch: "arm64", UserBundles: []string{"woa"},
	}
	e.registerDevice(t, d)

	first, err := e.render.Sync(d, proto.SyncRequest{})
	if err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	if first.Batch == nil || len(first.Batch.Files) == 0 {
		t.Fatalf("首次 Sync 应下发 MCP 条目，实际 %+v", first.Batch)
	}
	var mcpIDs []string
	for _, f := range first.Batch.Files {
		if f.Format == proto.FormatJSONKey {
			mcpIDs = append(mcpIDs, f.ID)
		}
	}
	if len(mcpIDs) == 0 {
		t.Fatalf("本次下发里没有 json_key 条目，这条测试就没测到东西")
	}

	results, manifest := applyWithRealWriter(t, e, first.Batch)
	if err := e.render.States().RecordAck(d.ID, first.Batch.Revision, results); err != nil {
		t.Fatalf("记录回执失败: %v", err)
	}

	// 第一处断言：第二轮不该再有下发。
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
		t.Fatalf("落盘后 MCP 条目应已收敛，实际仍要下发 %d 个: %v\n"+
			"原因通常是服务端与代理对 json_key 条目算哈希的口径不同",
			len(second.Batch.Files), ids)
	}

	// 第二处断言：管理面看到的 drift 必须是 0。
	// 这一条是用户真正看到的症状，只断言 batch 为空会漏掉它。
	want, err := e.render.RenderDevice(d)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	drift, err := e.render.States().Drift(d.ID, want)
	if err != nil {
		t.Fatalf("计算 drift 失败: %v", err)
	}
	if drift != 0 {
		t.Fatalf("回执之后 drift 必须为 0，实际 %d（设备列表会一直显示有差异）", drift)
	}
}
