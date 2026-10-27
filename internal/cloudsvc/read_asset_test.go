package cloudsvc

import (
	"encoding/json"
	"testing"

	"github.com/shichao402/Dec/internal/proto"
)

// 资产浏览（需求 14.1）。
//
// 管理面要能看内容，而 list_assets 只给概要。这里锁定 read_asset 的两个关键性质：
// 能读到完整内容，且不把密钥明文带出来。

func TestReadAssetReturnsContent(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	write := &proto.Request{ID: "w1", Protocol: proto.Version, Method: proto.MethodWriteAsset, Source: proto.SourceAdmin}
	if err := write.EncodePayload(&proto.AssetPayload{
		Bundle: "woa",
		Items: []proto.AssetItem{
			{Path: "style.md", Content: []byte("# style\n")},
			{Path: "demo/SKILL.md", Content: []byte("# demo\n")},
		},
		Message: "初始内容",
	}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(write, cert); !resp.OK {
		t.Fatalf("写入失败: %s", resp.Error)
	}

	read := &proto.Request{ID: "r1", Protocol: proto.Version, Method: proto.MethodReadAsset, Source: proto.SourceAdmin}
	if err := read.EncodePayload(&proto.ReadAssetRequest{Bundle: "woa"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(read, cert)
	if !resp.OK {
		t.Fatalf("读取失败: %s", resp.Error)
	}
	var out proto.ReadAssetResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.Bundle != "woa" {
		t.Fatalf("bundle 为 %q", out.Bundle)
	}
	if len(out.Files) != 2 {
		t.Fatalf("文件数 %d，期望 2：%+v", len(out.Files), out.Files)
	}
	// manifest 也要带回来：管理面靠它区分 skill 与 rule。
	if len(out.Items) != 2 {
		t.Fatalf("条目数 %d，期望 2：%+v", len(out.Items), out.Items)
	}
	kinds := map[string]string{}
	for _, it := range out.Items {
		kinds[it.Path] = it.Kind
	}
	if kinds["demo"] != "skill" {
		t.Fatalf("demo 应是 skill，实际 %q", kinds["demo"])
	}
	if kinds["style.md"] != "rule" {
		t.Fatalf("style.md 应是 rule，实际 %q", kinds["style.md"])
	}
}

// TestReadAssetKeepsSecretPlaceholder 验证浏览资产不会带出密钥明文。
//
// 资产内容里的 `{{secret:name}}` 是占位符，渲染时才内联。
// 如果 read_asset 顺手把它替换掉，管理面的每一次浏览都等于一次明文读取，
// 而这条路径不会记“显式查看明文”的审计。
func TestReadAssetKeepsSecretPlaceholder(t *testing.T) {
	h := newHarnessWithRootKey(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	put := &proto.Request{ID: "s1", Protocol: proto.Version, Method: proto.MethodPutSecret, Source: proto.SourceAdmin}
	if err := put.EncodePayload(&proto.SecretPayload{Name: "token", Value: "super-secret-value"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(put, cert); !resp.OK {
		t.Fatalf("写密钥失败: %s", resp.Error)
	}

	write := &proto.Request{ID: "w1", Protocol: proto.Version, Method: proto.MethodWriteAsset, Source: proto.SourceAdmin}
	if err := write.EncodePayload(&proto.AssetPayload{
		Bundle: "woa",
		Items: []proto.AssetItem{
			{Path: "mcp.md", Content: []byte("token={{secret:token}}\n")},
		},
		Message: "引用密钥",
	}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(write, cert); !resp.OK {
		t.Fatalf("写资产失败: %s", resp.Error)
	}

	read := &proto.Request{ID: "r1", Protocol: proto.Version, Method: proto.MethodReadAsset, Source: proto.SourceAdmin}
	if err := read.EncodePayload(&proto.ReadAssetRequest{Bundle: "woa"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(read, cert)
	if !resp.OK {
		t.Fatalf("读取失败: %s", resp.Error)
	}
	var out proto.ReadAssetResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(out.Files) != 1 {
		t.Fatalf("文件数 %d", len(out.Files))
	}
	content := string(out.Files[0].Content)
	if content != "token={{secret:token}}\n" {
		t.Fatalf("内容应保持占位符，实际 %q", content)
	}
	if contains(string(out.Files[0].Content), "super-secret-value") {
		t.Fatal("浏览资产不应带出密钥明文")
	}
}

// TestReadAssetRevisionWorksAsBase 验证 read_asset 给出的 revision 可以直接
// 回传给 edit_asset_file 做乐观并发（ADR 0053）。
//
// 以前这里返回的是 manifest.yaml 里的 revision 字段，通常为空，调用方
// 读完拿不到可用的 base，乐观并发形同虚设。
func TestReadAssetRevisionWorksAsBase(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	write := &proto.Request{ID: "w1", Protocol: proto.Version, Method: proto.MethodWriteAsset, Source: proto.SourceAdmin}
	if err := write.EncodePayload(&proto.AssetPayload{
		Bundle:  "woa",
		Items:   []proto.AssetItem{{Path: "demo/SKILL.md", Content: []byte("# demo\nold line\n")}},
		Message: "初始内容",
	}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(write, cert); !resp.OK {
		t.Fatalf("写入失败: %s", resp.Error)
	}

	read := &proto.Request{ID: "r1", Protocol: proto.Version, Method: proto.MethodReadAsset, Source: proto.SourceAdmin}
	if err := read.EncodePayload(&proto.ReadAssetRequest{Bundle: "woa"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(read, cert)
	if !resp.OK {
		t.Fatalf("读取失败: %s", resp.Error)
	}
	var out proto.ReadAssetResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.Revision == "" {
		t.Fatal("read_asset 应返回非空 revision")
	}

	edit := func(id, oldS, newS string) *proto.Response {
		req := &proto.Request{ID: id, Protocol: proto.Version, Method: proto.MethodEditAssetFile, Source: proto.SourceAdmin}
		if err := req.EncodePayload(&proto.EditAssetFileRequest{
			Bundle: "woa", Path: "demo/SKILL.md",
			OldString: oldS, NewString: newS, BaseRevision: out.Revision,
		}); err != nil {
			t.Fatalf("构造载荷失败: %v", err)
		}
		return h.postTLS(req, cert)
	}
	if resp := edit("e1", "old line", "new line"); !resp.OK {
		t.Fatalf("用读到的 revision 编辑应成功: %s", resp.Error)
	}
	// 同一个 base 再写一次：中间已经有了新提交，必须被拒。
	resp = edit("e2", "new line", "newer line")
	if resp.OK {
		t.Fatal("base_revision 过期时编辑应被拒绝")
	}
}

// TestReadAssetRejectsEmptyBundle 验证空 bundle 名被拒绝。
func TestReadAssetRejectsEmptyBundle(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := &proto.Request{ID: "r1", Protocol: proto.Version, Method: proto.MethodReadAsset, Source: proto.SourceAdmin}
	if err := req.EncodePayload(&proto.ReadAssetRequest{Bundle: "  "}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("空 bundle 应被拒绝")
	}
	if resp.Code != proto.ErrInvalid {
		t.Fatalf("错误码为 %q，期望 invalid", resp.Code)
	}
}

// contains 是简易子串判断。
func contains(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
