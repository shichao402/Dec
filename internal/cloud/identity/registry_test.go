package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	store, err := ssot.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return NewRegistry(store)
}

func adminAuthor() ssot.Author { return ssot.Author{Name: "admin", Source: "admin"} }

func sampleDevice(id string) ssot.Device {
	return ssot.Device{
		ID:           id,
		Name:         "设备 " + id,
		Kind:         ssot.DeviceMine,
		OS:           "darwin",
		Arch:         "arm64",
		CertSerial:   "abc",
		CertNotAfter: time.Now().Add(24 * time.Hour),
		RegisteredAt: time.Now().UTC(),
	}
}

func TestRegistryAddAndList(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Add(sampleDevice("d1"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add(sampleDevice("d2"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	devices, err := r.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("设备数错误: %d", len(devices))
	}
	if err := r.Add(sampleDevice("d1"), adminAuthor()); !errors.Is(err, ssot.ErrConflict) {
		t.Fatalf("重复登记应报 ErrConflict，实际 %v", err)
	}
}

func TestRegistryRejectsBadKind(t *testing.T) {
	r := newTestRegistry(t)
	dev := sampleDevice("d1")
	dev.Kind = "root"
	if err := r.Add(dev, adminAuthor()); err == nil {
		t.Fatal("非法设备类型应被拒绝")
	}
}

func TestRegistryRevoke(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Add(sampleDevice("d1"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	dev, err := r.Revoke("d1", "设备丢失", adminAuthor())
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !dev.Revoked || dev.RevokedAt == nil {
		t.Fatalf("吊销状态未写入: %+v", dev)
	}
	if err := Active(dev, time.Now()); !errors.Is(err, ErrCertRevoked) {
		t.Fatalf("吊销后 Active 应报 ErrCertRevoked，实际 %v", err)
	}
	if _, err := r.Revoke("absent", "", adminAuthor()); !errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("吊销不存在的设备应报 ErrNotFound，实际 %v", err)
	}
}

func TestRegistrySetKind(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Add(sampleDevice("d1"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	dev, err := r.SetKind("d1", ssot.DeviceReceiver, adminAuthor())
	if err != nil {
		t.Fatalf("SetKind: %v", err)
	}
	if dev.Kind != ssot.DeviceReceiver {
		t.Fatalf("类型未改: %s", dev.Kind)
	}
	if _, err := r.SetKind("d1", "nope", adminAuthor()); err == nil {
		t.Fatal("非法类型应被拒绝")
	}
}

func TestRegistryRenew(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Add(sampleDevice("d1"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	newExpiry := time.Now().Add(60 * 24 * time.Hour).UTC().Truncate(time.Second)
	dev, err := r.Renew("d1", "serial-2", newExpiry, "fp-2", adminAuthor())
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if dev.CertSerial != "serial-2" || dev.Fingerprint != "fp-2" {
		t.Fatalf("续签结果错误: %+v", dev)
	}
	if !dev.CertNotAfter.Equal(newExpiry) {
		t.Fatalf("过期时间未更新: %v", dev.CertNotAfter)
	}
}

func TestRegistrySeenAndFlush(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Add(sampleDevice("d1"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	r.Seen("d1", "1.2.3")
	// 未 flush 前不应落盘。
	before, err := r.Device("d1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if before.LastSeen != nil || before.ReportedVersion != "" {
		t.Fatalf("未 flush 前不应写入: %+v", before)
	}

	if err := r.FlushSeen(adminAuthor()); err != nil {
		t.Fatalf("FlushSeen: %v", err)
	}
	after, err := r.Device("d1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if after.LastSeen == nil {
		t.Fatal("LastSeen 未写入")
	}
	if after.ReportedVersion != "1.2.3" {
		t.Fatalf("版本未写入: %q", after.ReportedVersion)
	}

	// 无待写内容时 flush 是空操作。
	if err := r.FlushSeen(adminAuthor()); err != nil {
		t.Fatalf("二次 FlushSeen: %v", err)
	}
}

func TestActiveExpiry(t *testing.T) {
	dev := sampleDevice("d1")
	dev.CertNotAfter = time.Now().Add(-time.Hour)
	if err := Active(dev, time.Now()); !errors.Is(err, ErrCertExpired) {
		t.Fatalf("过期设备应报 ErrCertExpired，实际 %v", err)
	}
	// 时钟偏移容忍度内仍算有效。
	dev.CertNotAfter = time.Now().Add(-time.Minute)
	if err := Active(dev, time.Now()); err != nil {
		t.Fatalf("偏移容忍内的设备应有效: %v", err)
	}
}

func TestPermissionRules(t *testing.T) {
	mine := ssot.Device{ID: "d1", Kind: ssot.DeviceMine}
	receiver := ssot.Device{ID: "d2", Kind: ssot.DeviceReceiver}

	if !CanReadSecret(proto.SourceAdmin) || !CanReadSecret(proto.SourceHostCLI) {
		t.Fatal("用户本人的来源应可读明文")
	}
	if CanReadSecret(proto.SourceMCP) || CanReadSecret(proto.SourceAgent) || CanReadSecret(proto.SourceEnroll) {
		t.Fatal("MCP 与代理自身不应可读明文")
	}

	if !CanManageDevices(proto.SourceAdmin) {
		t.Fatal("管理面应可管理设备")
	}
	if CanManageDevices(proto.SourceMCP) || CanManageDevices(proto.SourceAgent) {
		t.Fatal("MCP 与代理不应管理设备")
	}

	if !CanWrite(proto.SourceMCP, mine) || !CanWrite(proto.SourceAdmin, mine) {
		t.Fatal("我的设备应可写")
	}
	if CanWrite(proto.SourceMCP, receiver) {
		t.Fatal("只接收设备不应可写")
	}
	if CanWrite(proto.SourceAgent, mine) || CanWrite(proto.SourceEnroll, mine) {
		t.Fatal("代理自身与登记来源不应可写")
	}
}

func TestEnsureInitialized(t *testing.T) {
	r := newTestRegistry(t)
	ok, err := r.EnsureInitialized()
	if err != nil {
		t.Fatalf("EnsureInitialized: %v", err)
	}
	if ok {
		t.Fatal("空登记表应返回 false")
	}
	if err := r.Add(sampleDevice("d1"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ok, err = r.EnsureInitialized()
	if err != nil {
		t.Fatalf("EnsureInitialized: %v", err)
	}
	if !ok {
		t.Fatal("有设备时应返回 true")
	}
}

func TestRegistryWritesHistory(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.Add(sampleDevice("d1"), adminAuthor()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Revoke("d1", "丢失", adminAuthor()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	hist, err := r.store.History(context.Background(), 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// 初始化提交 + 登记 + 吊销
	if len(hist) < 3 {
		t.Fatalf("历史应包含登记与吊销，实际 %d 条", len(hist))
	}
	if hist[0].Source != "admin" {
		t.Fatalf("来源未记录: %q", hist[0].Source)
	}
}
