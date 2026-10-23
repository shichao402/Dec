package cloudsvc

import (
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// sync 审计降噪的边界测试。
//
// 背景（2026-10-23 生产故障）：说明行导致的无限重写把审计刷到 8.4 万条
// sync 记录，真正有用的信息被淹没。降噪的目的不是"少记日志"，
// 而是让"每轮都重发"这种异常在日志量级上就看得出来。
//
// 所以这些用例要守住的不是"能少记"，而是"不能多记也不能少记"：
// 内容变了必须记，设备不同必须记，只是时间过去了同一批内容还是不必记。

// TestSyncSignatureIgnoresRevision 验证同一批内容的指纹与 revision 无关。
//
// 幂等重发时 revision 可能因为别的设备写入而变，但推出去的内容完全相同。
// 如果指纹里带了 revision，降噪就失效了——故障场景下每轮 revision 都可能
// 不同，日志会照旧被刷爆。
func TestSyncSignatureIgnoresRevision(t *testing.T) {
	a := &proto.Batch{
		Revision: "rev-1",
		Files:    []proto.File{{ID: "asset:core:a.md", Content: "x", Key: ""}},
	}
	b := &proto.Batch{
		Revision: "rev-2",
		Files:    []proto.File{{ID: "asset:core:a.md", Content: "x", Key: ""}},
	}
	if syncSignature("dev-1", a) != syncSignature("dev-1", b) {
		t.Fatal("只差 revision 的两批内容应视为同一指纹")
	}
}

// TestSyncSignatureDistinguishes 验证指纹对真正该区分的维度敏感。
func TestSyncSignatureDistinguishes(t *testing.T) {
	base := &proto.Batch{
		Files: []proto.File{{ID: "asset:core:a.md", Content: "x"}},
	}
	baseSig := syncSignature("dev-1", base)

	cases := map[string]string{
		"内容变了": syncSignature("dev-1", &proto.Batch{
			Files: []proto.File{{ID: "asset:core:a.md", Content: "y"}},
		}),
		"文件变了": syncSignature("dev-1", &proto.Batch{
			Files: []proto.File{{ID: "asset:core:b.md", Content: "x"}},
		}),
		"设备变了": syncSignature("dev-2", base),
		"多了删除": syncSignature("dev-1", &proto.Batch{
			Files:   []proto.File{{ID: "asset:core:a.md", Content: "x"}},
			Deletes: []proto.File{{ID: "asset:core:old.md"}},
		}),
	}
	for name, got := range cases {
		if got == baseSig {
			t.Fatalf("%s 之后指纹不应相同", name)
		}
	}
}

// TestSuppressSyncDedupes 验证同一签名在时间窗内只放行第一次。
func TestSuppressSyncDedupes(t *testing.T) {
	s := &Server{}
	b := &proto.Batch{Files: []proto.File{{ID: "asset:core:a.md", Content: "x"}}}
	sig := syncSignature("dev-1", b)

	if s.suppressSync(sig) {
		t.Fatal("第一次下发应记审计")
	}
	if !s.suppressSync(sig) {
		t.Fatal("窗口内重复下发应被降噪")
	}
	if !s.suppressSync(sig) {
		t.Fatal("窗口内第三次下发仍应被降噪")
	}
}

// TestSuppressSyncReopensAfterQuiet 验证窗口过去之后重新记录。
//
// 这条很重要：降噪不能变成"永久只记一次"。设备每天正常对账一次，
// 每天都该留一条痕迹，否则审计里会看不出这台设备还活着。
func TestSuppressSyncReopensAfterQuiet(t *testing.T) {
	s := &Server{}
	sig := "sig"
	// 直接放入一条已经过期的记录，模拟时间流逝。
	s.syncLogged = map[string]time.Time{sig: time.Now().Add(-syncQuiet - time.Minute)}

	if s.suppressSync(sig) {
		t.Fatal("超过静默期后应重新记录")
	}
}

// TestSuppressSyncDifferentDevices 验证降噪按设备隔离。
//
// 同一个内容推给两台设备是两条独立的事实，审计要能回答
// "这份内容送到过哪些机器"，合并会直接毁掉这个能力。
func TestSuppressSyncDifferentDevices(t *testing.T) {
	s := &Server{}
	b := &proto.Batch{Files: []proto.File{{ID: "asset:core:a.md", Content: "x"}}}

	if s.suppressSync(syncSignature("dev-1", b)) {
		t.Fatal("dev-1 首次应记录")
	}
	if s.suppressSync(syncSignature("dev-2", b)) {
		t.Fatal("dev-2 首次应记录，不应被 dev-1 的记录影响")
	}
}

// TestSuppressSyncPrunesStale 验证过期条目会被清理。
//
// 服务可能连续运行几个月，内容指纹随内容变化不断新增，
// 不清理会让这张表无限增长——正是这次故障的另一种形状。
func TestSuppressSyncPrunesStale(t *testing.T) {
	s := &Server{}
	old := time.Now().Add(-syncQuiet * 8)
	s.syncLogged = map[string]time.Time{}
	for i := 0; i < 600; i++ {
		s.syncLogged[string(rune('a'+i%26))+string(rune('a'+i/26))] = old
	}
	// 触发一次写入，顺带清理。
	s.suppressSync("fresh")

	if len(s.syncLogged) > 600 {
		t.Fatalf("过期条目应被清理，实际 %d 条", len(s.syncLogged))
	}
	for sig, at := range s.syncLogged {
		if sig == "fresh" {
			continue
		}
		if time.Since(at) > syncQuiet*4 {
			t.Fatalf("超期条目 %q 仍留在表中", sig)
		}
	}
}
