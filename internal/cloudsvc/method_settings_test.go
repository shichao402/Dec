package cloudsvc

import (
	"testing"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 服务端全局设置（自动更新检查间隔）。
//
// 权威值只在云端：代理每次握手读到它，本地不落盘。这几条用例守住
// "默认值可用、写入有边界、越界被拒、权限只给管理面"这四件事。

// newSettingsServer 构造一个最小服务端，只用于读写设置。
func newSettingsServer(t *testing.T) *Server {
	t.Helper()
	srv, err := New(Config{
		Domain:        "dec.test",
		Listen:        "127.0.0.1:0",
		DataDir:       t.TempDir(),
		ServerVersion: "v2.0.0",
		AcceptAny:     true,
	})
	if err != nil {
		t.Fatalf("构造服务端失败: %v", err)
	}
	t.Cleanup(srv.closeAll)
	return srv
}

// TestSettingsDefaultsWhenFileMissing 验证没写过设置时返回默认间隔。
//
// settings.json 是后加的文件，存量部署里不存在。此时必须给出 1 小时
// 而不是 0：0 会让代理按"服务端没给值"处理，虽然也退化成默认，
// 但管理面会显示一个没有意义的数字。
func TestSettingsDefaultsWhenFileMissing(t *testing.T) {
	srv := newSettingsServer(t)
	got, err := ssot.ReadSettings(srv.store)
	if err != nil {
		t.Fatalf("读取默认设置失败: %v", err)
	}
	if got.UpdateCheckIntervalSec != ssot.DefaultUpdateCheckIntervalSec {
		t.Fatalf("默认间隔应为 %d，实际 %d",
			ssot.DefaultUpdateCheckIntervalSec, got.UpdateCheckIntervalSec)
	}
}

// TestSettingsWriteThenRead 验证写入后读回一致，且落进 SSOT 提交历史。
func TestSettingsWriteThenRead(t *testing.T) {
	srv := newSettingsServer(t)
	if _, err := ssot.SetSettings(srv.store, ssot.Settings{
		UpdateCheckIntervalSec: 1800,
	}, ssot.Author{Name: "test"}); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}
	got, err := ssot.ReadSettings(srv.store)
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got.UpdateCheckIntervalSec != 1800 {
		t.Fatalf("间隔应为 1800，实际 %d", got.UpdateCheckIntervalSec)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("写入后应记录修改时间")
	}
	// 设置必须真的进仓库：冷备推的是 git 历史，只写工作树等于没备份。
	if _, err := srv.store.FileAt(srv.store.Head(), ssot.FileSettings); err != nil {
		t.Fatalf("设置未进入 SSOT 提交：%v", err)
	}
}

// TestSettingsRejectsOutOfRange 验证越界值被拒而不是被悄悄夹取。
//
// 夹取会让人以为"设置生效了"，实际生效的是另一个数——那种偏差只有
// 等到发版那天才会被发现。
func TestSettingsRejectsOutOfRange(t *testing.T) {
	srv := newSettingsServer(t)
	for _, sec := range []int{0, -1, 60, ssot.MaxUpdateCheckIntervalSec + 1} {
		if _, err := ssot.SetSettings(srv.store, ssot.Settings{
			UpdateCheckIntervalSec: sec,
		}, ssot.Author{Name: "test"}); err == nil {
			t.Fatalf("间隔 %d 应被拒绝", sec)
		}
	}
}

// TestSettingsReadClampsCorruptFile 验证读到越界值时收敛到合法区间。
//
// 文件可能被手改过。这里不能报错：代理每次握手都要读它，
// 一个坏数字不该让全部设备停止检查更新。
func TestSettingsReadClampsCorruptFile(t *testing.T) {
	srv := newSettingsServer(t)
	// 绕过 SetSettings 直接写文件，模拟手改。
	if _, err := srv.store.Update(ssot.Author{Name: "test"}, func(tx *ssot.Tx) (string, error) {
		return "手改设置", tx.WriteJSON(ssot.FileSettings, ssot.Settings{
			SchemaVersion:          ssot.SchemaVersion,
			UpdateCheckIntervalSec: 5,
		})
	}); err != nil {
		t.Fatalf("写坏值失败: %v", err)
	}
	got, err := ssot.ReadSettings(srv.store)
	if err != nil {
		t.Fatalf("读越界值不该报错：%v", err)
	}
	if got.UpdateCheckIntervalSec != ssot.MinUpdateCheckIntervalSec {
		t.Fatalf("越界值应收敛到下限 %d，实际 %d",
			ssot.MinUpdateCheckIntervalSec, got.UpdateCheckIntervalSec)
	}
}

// TestSettingsWriteRequiresAdmin 验证 MCP 来源不能改全局设置。
//
// 这个值决定全部设备的更新节奏。让 MCP 能改它，等于让一次提示注入
// 就能把检查间隔拉到 24 小时，或者压到极限去放大下载流量。
func TestSettingsWriteRequiresAdmin(t *testing.T) {
	srv := newSettingsServer(t)
	sec := 1800
	resp := srv.mSettings(&proto.Request{
		ID:     "t1",
		Method: proto.MethodSettings,
		Source: proto.SourceMCP,
	}, &proto.SettingsRequest{UpdateCheckIntervalSec: &sec}, ssot.Device{ID: "dev1", Kind: ssot.DeviceMine})
	if resp.OK || resp.Code != proto.ErrForbidden {
		t.Fatalf("MCP 来源改设置应被拒绝，实际 ok=%v code=%v", resp.OK, resp.Code)
	}

	// 管理面可以改。
	resp = srv.mSettings(&proto.Request{
		ID:     "t2",
		Method: proto.MethodSettings,
		Source: proto.SourceAdmin,
	}, &proto.SettingsRequest{UpdateCheckIntervalSec: &sec}, ssot.Device{ID: "dev1", Kind: ssot.DeviceMine})
	if !resp.OK {
		t.Fatalf("管理面改设置应成功，实际 code=%v err=%s", resp.Code, resp.Error)
	}
}

// TestSettingsReadAllowedForAgent 验证代理来源能读设置。
//
// 代理不直接调这个方法（它靠 hello 拿间隔），但状态页与排障会用到；
// 更重要的是这条用例锁住"读不设限"这个决定，避免以后有人顺手加上写权限的检查。
func TestSettingsReadAllowedForAgent(t *testing.T) {
	srv := newSettingsServer(t)
	resp := srv.mSettings(&proto.Request{
		ID:     "t3",
		Method: proto.MethodSettings,
		Source: proto.SourceAgent,
	}, &proto.SettingsRequest{}, ssot.Device{ID: "dev1"})
	if !resp.OK {
		t.Fatalf("代理来源读设置应放行，实际 code=%v err=%s", resp.Code, resp.Error)
	}
}

// TestHelloCarriesUpdateInterval 验证握手下发当前间隔。
//
// 这是间隔能真正生效的唯一通道：代理不落盘，每次启动都靠握手拿到值。
// 若这里漏了字段，功能表现为"设置改了但设备节奏不变"。
func TestHelloCarriesUpdateInterval(t *testing.T) {
	srv := newSettingsServer(t)
	if _, err := ssot.SetSettings(srv.store, ssot.Settings{
		UpdateCheckIntervalSec: 7200,
	}, ssot.Author{Name: "test"}); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}
	if got := srv.updateCheckIntervalSec(); got != 7200 {
		t.Fatalf("握手应下发 7200，实际 %d", got)
	}
}

// TestHelloUpdateIntervalZeroOnReadFailure 验证读设置失败时下发 0。
//
// 0 让代理继续用自己的默认值。反过来若是让握手失败，一个设置文件的问题
// 会升级成"全部设备无法连接"——那比"用了默认间隔"严重得多。
func TestHelloUpdateIntervalZeroOnReadFailure(t *testing.T) {
	srv := newSettingsServer(t)
	// 写一个坏 JSON，让解析失败。
	if _, err := srv.store.Update(ssot.Author{Name: "test"}, func(tx *ssot.Tx) (string, error) {
		return "写坏文件", tx.WriteFile(ssot.FileSettings, []byte("{ not json"), 0o600)
	}); err != nil {
		t.Fatalf("写坏文件失败: %v", err)
	}
	if got := srv.updateCheckIntervalSec(); got != 0 {
		t.Fatalf("读失败时应下发 0，实际 %d", got)
	}
}
