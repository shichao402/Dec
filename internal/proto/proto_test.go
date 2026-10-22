package proto

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestVersionMethodsStable 固定协议里永不破坏兼容的方法集合。
//
// 这个测试的存在本身就是约束：如果某次改动想把 list_assets 之类的方法塞进
// VersionMethods，测试会失败，迫使人先想清楚要不要提升协议版本
// （需求 19.7：上报版本和获取要求版本必须永远可用；初始化、登记和恢复同理，
// 否则一台未登记的旧代理会彻底无法接入）。
func TestVersionMethodsStable(t *testing.T) {
	want := map[Method]bool{
		MethodHello: true,
		// initialize 必须永久可用：全新服务只有它能建立根密钥。
		// 把它移出清单，会让「刚部署完的服务端」无法接入任何设备。
		MethodInitialize: true,
		MethodEnroll:     true,
		MethodRecover:    true,
	}
	if len(VersionMethods) != len(want) {
		t.Fatalf("VersionMethods 数量变化: %d，期望 %d", len(VersionMethods), len(want))
	}
	for m := range want {
		if !VersionMethods[m] {
			t.Fatalf("VersionMethods 缺少 %s", m)
		}
	}
	for m := range VersionMethods {
		if !want[m] {
			t.Fatalf("VersionMethods 多出 %s，新增方法要先确认它真的永不破坏兼容", m)
		}
	}
}

func TestNoCertMethodsSubsetOfVersionMethods(t *testing.T) {
	// 无证书方法一定也必须是永不变更的方法，否则旧代理连登记都做不到。
	for m := range NoCertMethods {
		if !VersionMethods[m] {
			t.Fatalf("%s 无证书可访问，但不在 VersionMethods 里", m)
		}
	}
}

// TestOptionalCertMethodsIncludeHello 锁定 hello 的证书可选语义。
//
// hello 是旧代理唯一的自救通道（需求 19.7）：证书过期、未登记、版本落后
// 这三种情况都必须能问到要求版本，所以它既不能要求证书，也不能它自己
// 依赖任何需要身份的信息。
func TestOptionalCertMethodsIncludeHello(t *testing.T) {
	if !OptionalCertMethods[MethodHello] {
		t.Fatal("hello 必须是证书可选方法")
	}
	for m := range OptionalCertMethods {
		if NoCertMethods[m] {
			t.Fatalf("%s 同时在免证书与证书可选清单里，语义不明", m)
		}
		if !VersionMethods[m] {
			t.Fatalf("%s 证书可选，但不在 VersionMethods 里", m)
		}
	}
}

func TestEveryKnownMethodDecodes(t *testing.T) {
	for m := range methods {
		if !Known(m) {
			t.Fatalf("Known(%s) 为 false", m)
		}
		req := &Request{Method: m}
		payload, err := DecodePayload(req)
		if err != nil {
			t.Fatalf("%s 解析空 payload 失败: %v", m, err)
		}
		if payload == nil {
			t.Fatalf("%s 解析出 nil", m)
		}
	}
	if Known(Method("nope")) {
		t.Fatal("未知方法不应被识别")
	}
	if _, err := DecodePayload(&Request{Method: Method("nope")}); !errors.Is(err, ErrUnknownMethod) {
		t.Fatalf("期望 ErrUnknownMethod，实际 %v", err)
	}
}

func TestBadPayload(t *testing.T) {
	req := &Request{Method: MethodSync, Payload: []byte("{not json")}
	if _, err := DecodePayload(req); !errors.Is(err, ErrBadPayload) {
		t.Fatalf("期望 ErrBadPayload，实际 %v", err)
	}
}

func TestRoundTripPayload(t *testing.T) {
	want := &SyncRequest{
		Revision: "abc123",
		Manifest: []ManifestEntry{{ID: "asset:woa:skill/x", Hash: "sha256:00", Format: FormatWhole}},
		Projects: []LocalProject{{ID: "p1", Path: "/Users/x/work/demo"}},
	}
	req, err := WithPayload(MethodSync, SourceAgent, want)
	if err != nil {
		t.Fatalf("WithPayload: %v", err)
	}
	decoded, err := DecodePayload(req)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	got, ok := decoded.(*SyncRequest)
	if !ok {
		t.Fatalf("类型错误: %T", decoded)
	}
	if got.Revision != want.Revision || len(got.Manifest) != 1 || got.Manifest[0].ID != want.Manifest[0].ID {
		t.Fatalf("往返不一致: %+v", got)
	}
	if len(got.Projects) != 1 || got.Projects[0].Path != want.Projects[0].Path {
		t.Fatalf("项目往返不一致: %+v", got.Projects)
	}
}

func TestResponseHelpers(t *testing.T) {
	ok := OK("id1", []byte(`{"a":1}`))
	if !ok.OK || ok.ID != "id1" || ok.Code != "" {
		t.Fatalf("OK 构造错误: %+v", ok)
	}
	fail := Fail("id2", ErrVersionMismatch, "need upgrade")
	if fail.OK || fail.Code != ErrVersionMismatch {
		t.Fatalf("Fail 构造错误: %+v", fail)
	}
}

// TestWireCompatForward 保证响应结构对未知字段是宽容的，
// 这样服务端新增字段不会让旧代理解析失败。
func TestWireCompatForward(t *testing.T) {
	raw := []byte(`{"id":"x","ok":true,"payload":null,"future_field":123}`)
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("旧代理应能忽略未知字段: %v", err)
	}
	if !resp.OK {
		t.Fatal("解析结果错误")
	}

	// 请求侧同理。
	reqRaw := []byte(`{"id":"y","protocol":1,"method":"hello","payload":null,"unknown":true}`)
	var req Request
	if err := json.Unmarshal(reqRaw, &req); err != nil {
		t.Fatalf("服务端应能忽略未知字段: %v", err)
	}
	if req.Method != MethodHello {
		t.Fatalf("method 解析错误: %s", req.Method)
	}
}
