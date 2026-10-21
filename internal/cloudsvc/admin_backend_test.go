package cloudsvc

import (
	"encoding/json"
	"testing"

	"github.com/shichao402/Dec/internal/proto"
)

// overrides 的载荷解析（需求 6.2、14.1）。
//
// 这里锁的是一处曾经真实存在的缺陷：方法登记表给 overrides 构造的是空结构体，
// 而实现里按 map 去取 device_id，两边对不上，于是“查指定设备的覆盖记录”这条
// 路径永远静默退化成“查自己”。它不会报错，只会在管理面上少显示一台设备。

// TestOverridesAcceptsDeviceID 验证 device_id 真的能被解析出来。
func TestOverridesAcceptsDeviceID(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")
	h.registerDevice("dev-b", "NUC")

	req := &proto.Request{ID: "o1", Protocol: proto.Version, Method: proto.MethodOverrides, Source: proto.SourceAdmin}
	if err := req.EncodePayload(&proto.OverridesRequest{DeviceID: "dev-b"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if !resp.OK {
		t.Fatalf("查询失败: %s", resp.Error)
	}
	var out proto.OverridesResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.DeviceID != "dev-b" {
		t.Fatalf("查询目标为 %q，期望 dev-b（device_id 没被解析出来）", out.DeviceID)
	}
}

// TestOverridesDefaultsToSelf 验证不带参数时查的是当前设备。
func TestOverridesDefaultsToSelf(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	req := &proto.Request{ID: "o1", Protocol: proto.Version, Method: proto.MethodOverrides, Source: proto.SourceAgent}
	resp := h.postTLS(req, cert)
	if !resp.OK {
		t.Fatalf("查询失败: %s", resp.Error)
	}
	var out proto.OverridesResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.DeviceID != "dev-a" {
		t.Fatalf("默认目标为 %q，期望 dev-a", out.DeviceID)
	}
}

// TestMCPCannotReadOtherDeviceOverrides 验证 MCP 越权查看别的设备被拒绝。
//
// 覆盖记录含落点的绝对路径，等同于这台机器的目录结构。
// MCP 能看自己的（对账需要），但不该顺手把别的机器的路径也读出来。
func TestMCPCannotReadOtherDeviceOverrides(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")
	h.registerDevice("dev-b", "NUC")

	req := &proto.Request{ID: "o1", Protocol: proto.Version, Method: proto.MethodOverrides, Source: proto.SourceMCP}
	if err := req.EncodePayload(&proto.OverridesRequest{DeviceID: "dev-b"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(req, cert)
	if resp.OK {
		t.Fatal("MCP 不应查看其它设备的覆盖记录")
	}
	if resp.Code != proto.ErrForbidden {
		t.Fatalf("错误码为 %q，期望 forbidden", resp.Code)
	}
}

// TestReadAssetReturnsManifest 验证 manifest 原文被带回来。
//
// 管理面保存一个文件时要把整包写回，manifest 不原样带回去的话，
// 服务端会按文件树重新推断，planes 与 host 这些声明会被悄悄改掉。
func TestReadAssetReturnsManifest(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	// 只声明 project 平面：如果被重新推断，会变回默认的 user。
	manifest := "name: woa\nitems:\n  - kind: rule\n    path: a.md\n    name: a\n    planes: [project]\n"
	write := &proto.Request{ID: "w1", Protocol: proto.Version, Method: proto.MethodWriteAsset, Source: proto.SourceAdmin}
	if err := write.EncodePayload(&proto.AssetPayload{
		Bundle:   "woa",
		Items:    []proto.AssetItem{{Path: "a.md", Content: []byte("x")}},
		Manifest: []byte(manifest),
		Message:  "只声明项目平面",
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
	if len(out.Manifest) == 0 {
		t.Fatal("read_asset 应带回 manifest 原文")
	}
	if !contains(string(out.Manifest), "project") {
		t.Fatalf("manifest 内容不对：%s", out.Manifest)
	}
	if len(out.Files) != 1 || out.Files[0].Path != "a.md" {
		t.Fatalf("文件列表不对：%+v", out.Files)
	}
}

// TestReadAssetKeepsExecutable 验证可执行位能原样读回。
//
// 上传链路用 Executable 表达“这个脚本要能跑”。读的时候丢掉这个信息，
// 管理面保存一次就会把它降级成普通文件，而且不会有任何提示。
func TestReadAssetKeepsExecutable(t *testing.T) {
	h := newHarness(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	write := &proto.Request{ID: "w1", Protocol: proto.Version, Method: proto.MethodWriteAsset, Source: proto.SourceAdmin}
	if err := write.EncodePayload(&proto.AssetPayload{
		Bundle: "woa",
		Items: []proto.AssetItem{
			{Path: "run.sh", Content: []byte("#!/bin/sh\n"), Executable: true},
			{Path: "a.md", Content: []byte("x")},
		},
		Message: "带一个脚本",
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
	got := map[string]bool{}
	for _, f := range out.Files {
		got[f.Path] = f.Executable
	}
	if !got["run.sh"] {
		t.Fatal("run.sh 的可执行位丢失")
	}
	if got["a.md"] {
		t.Fatal("a.md 不该是可执行")
	}
}

// TestReadSecretRecordsReason 验证查看理由真的进了审计。
//
// 事后回看审计时，“为什么要看”比“谁看了”更难重建。
func TestReadSecretRecordsReason(t *testing.T) {
	h := newHarnessWithRootKey(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")

	put := &proto.Request{ID: "s1", Protocol: proto.Version, Method: proto.MethodPutSecret, Source: proto.SourceAdmin}
	if err := put.EncodePayload(&proto.SecretPayload{Name: "token", Value: "v"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	if resp := h.postTLS(put, cert); !resp.OK {
		t.Fatalf("写密钥失败: %s", resp.Error)
	}

	read := &proto.Request{ID: "r1", Protocol: proto.Version, Method: proto.MethodReadSecret, Source: proto.SourceAdmin}
	if err := read.EncodePayload(&proto.ReadSecretRequest{Name: "token", Reason: "核对上游是否轮换"}); err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	resp := h.postTLS(read, cert)
	if !resp.OK {
		t.Fatalf("读取失败: %s", resp.Error)
	}
	var out proto.ReadSecretResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if out.Reason != "核对上游是否轮换" {
		t.Fatalf("理由为 %q，期望原样回显", out.Reason)
	}
}
