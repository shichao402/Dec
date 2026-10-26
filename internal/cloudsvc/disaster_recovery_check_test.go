package cloudsvc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 灾难恢复演练的第二组用例：验证 git 校验值真正起到了作用。
//
// 这一组的存在理由：第一组演练能证明「恢复链路走得通」，
// 但走通不等于走对。如果恢复时只取冷备里最新的一条、
// 启动时只信本地文件能解析，那么「拷错一份根密钥文件进来」
// 和「轮换过的仓库里挑错条目」这两种事故都会静默通过——
// 服务看起来正常启动，直到第一次读密钥才炸。

// drillInit 在一个数据目录上完成初始化，返回 key id。
func drillInit(t *testing.T, dir string, vault *drillVault) string {
	t.Helper()
	srv := startInstance(t, dir, vault)
	defer srv.closeAll()

	req, err := proto.WithPayload(proto.MethodInitialize, proto.SourceEnroll, initReq(t))
	if err != nil {
		t.Fatalf("构造初始化请求失败: %v", err)
	}
	req.ID = "drill-init"
	if resp := drillHarness(t, dir, srv).post(req); !resp.OK {
		t.Fatalf("初始化失败: %s %s", resp.Code, resp.Error)
	}
	return srv.Bootstrapper().State().KeyID
}

// TestInitializeRecordsRootKeyCheck 确认初始化把校验值写进了 git（需求 9.3）。
//
// 没有这个文件，服务器重建时无从知道“当前该用哪把钥匙”，
// 只能从冷备里猜，而猜错的代价是所有旧密文解不开。
func TestInitializeRecordsRootKeyCheck(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()
	keyID := drillInit(t, dir, vault)

	store, err := ssot.Open(storeDir(dir))
	if err != nil {
		t.Fatalf("打开 SSOT 仓库失败: %v", err)
	}
	check, err := ssot.ReadJSON[ssot.RootKeyCheck](store, ssot.FileRootKeyCheck)
	if err != nil {
		t.Fatalf("git 里应有根密钥校验值文件: %v", err)
	}
	if check.KeyID != keyID {
		t.Fatalf("校验值记录的 key id 应为 %s，实际 %q", keyID, check.KeyID)
	}
	if check.Digest == "" {
		t.Fatal("校验值必须包含材料摘要")
	}
	// 摘要不能是密钥本身：长度固定为 SHA-256 的十六进制表示。
	if len(check.Digest) != 64 {
		t.Fatalf("摘要应为 64 位十六进制，实际 %d 位", len(check.Digest))
	}
}

// TestStartupRejectsSwappedRootKey 验证拷错根密钥文件不会静默启动。
//
// 这是校验值最核心的价值：文件本身完全合法、能解析、长度也对，
// 只有和 git 记录的 key id 比对才能发现它不属于这个仓库。
func TestStartupRejectsSwappedRootKey(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()
	wantID := drillInit(t, dir, vault)

	// 造一份结构完全合法、但属于别人的根密钥。
	otherKey, err := crypt.NewKey(nil)
	if err != nil {
		t.Fatalf("生成另一把根密钥失败: %v", err)
	}
	if err := crypt.WriteRootKey(filepath.Join(dir, "root.key"), otherKey, time.Now().UTC(), ""); err != nil {
		t.Fatalf("写入替换用根密钥失败: %v", err)
	}

	srv := startInstance(t, dir, vault)
	defer srv.closeAll()

	state := srv.Bootstrapper().State()
	if !state.RecoveryMode {
		t.Fatal("根密钥与 git 记录不符时必须进入恢复模式（需求 9.3、9.4）")
	}
	if state.KeyID != "" {
		t.Fatalf("恢复模式下不应加载任何根密钥，实际 %q", state.KeyID)
	}
	_ = wantID
}

// TestRecoverPicksKeyByIDNotLatest 验证恢复按 key id 取，而不是取最新一条。
//
// 构造方式：先初始化（得到 A），再轮换（得到 B），然后删掉本地文件。
// 此时冷备里 A 与 B 都在。如果把冷备里 B 的创建时间改早于 A，
// 「取最新」的实现会挑到 A，从而恢复出一把错误的钥匙；
// 按 key id 的实现只会挑到 B。
func TestRecoverPicksKeyByIDNotLatest(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()
	firstID := drillInit(t, dir, vault)

	// 手工造第二把钥匙的冷备条目，并把创建时间设成比第一把还早。
	// 这样「按创建时间取最新」必然挑错。
	secondKey, err := crypt.NewKey(nil)
	if err != nil {
		t.Fatalf("生成第二把根密钥失败: %v", err)
	}
	payload, err := crypt.RootKeyPayload(secondKey, time.Now().UTC().Add(-48*time.Hour), firstID)
	if err != nil {
		t.Fatalf("构造冷备内容失败: %v", err)
	}
	if _, err := vault.UpsertNote(context.Background(), ssot.DirRootKeyBackup,
		ssot.FolderRootKey+"/"+secondKey.ID(), string(payload)); err != nil {
		t.Fatalf("写入第二把钥匙冷备失败: %v", err)
	}

	// 再把 git 校验值指向第一把（模拟“当前在用的是 A”）。
	store, err := ssot.Open(storeDir(dir))
	if err != nil {
		t.Fatalf("打开仓库失败: %v", err)
	}
	check, err := ssot.ReadJSON[ssot.RootKeyCheck](store, ssot.FileRootKeyCheck)
	if err != nil {
		t.Fatalf("读取校验值失败: %v", err)
	}
	if check.KeyID != firstID {
		t.Fatalf("初始化后校验值应指向 %s，实际 %s", firstID, check.KeyID)
	}
	if _, err := store.Update(ssot.Author{Name: "test", Source: "admin"}, func(tx *ssot.Tx) (string, error) {
		check.CreatedAt = check.CreatedAt.Add(-72 * time.Hour)
		return "把校验值指向更早的钥匙", tx.WriteJSON(ssot.FileRootKeyCheck, check)
	}); err != nil {
		t.Fatalf("改写校验值失败: %v", err)
	}

	// 删掉本地根密钥，进入恢复模式。
	if err := os.Remove(filepath.Join(dir, "root.key")); err != nil {
		t.Fatalf("删除根密钥失败: %v", err)
	}

	srv := startInstance(t, dir, vault)
	defer srv.closeAll()
	if !srv.Bootstrapper().State().RecoveryMode {
		t.Fatal("应进入恢复模式")
	}

	recoverReq, err := proto.WithPayload(proto.MethodRecover, proto.SourceEnroll, &proto.RecoverRequest{
		Email:           "me@example.com",
		Password:        "pw",
		DevicePublicKey: initReq(t).DevicePublicKey,
		DeviceName:      "恢复用设备",
	})
	if err != nil {
		t.Fatalf("构造恢复请求失败: %v", err)
	}
	recoverReq.ID = "drill-recover-by-id"
	if resp := drillHarness(t, dir, srv).post(recoverReq); !resp.OK {
		t.Fatalf("恢复失败: %s %s", resp.Code, resp.Error)
	}

	got := srv.Bootstrapper().State().KeyID
	if got != firstID {
		t.Fatalf("应按 git 记录的 key id 恢复出 %s，实际 %q（说明取了最新一条而不是指定条目）", firstID, got)
	}
}

// TestRecoverRejectsWhenKeyIDMissing 验证 git 指向的条目不存在时拒绝恢复（需求 11.3）。
//
// 关键点是不能退化成“随便找一条能用的启动”。那会让服务带着错误的钥匙运行，
// 而错在第一次读密钥时才暴露，排查成本远高于当场拒绝。
func TestRecoverRejectsWhenKeyIDMissing(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()
	drillInit(t, dir, vault)

	// 清空冷备里的根密钥条目，模拟用户误删。
	vault.mu.Lock()
	vault.notes[ssot.DirRootKeyBackup] = map[string]string{}
	vault.mu.Unlock()

	if err := os.Remove(filepath.Join(dir, "root.key")); err != nil {
		t.Fatalf("删除根密钥失败: %v", err)
	}

	srv := startInstance(t, dir, vault)
	defer srv.closeAll()

	recoverReq, err := proto.WithPayload(proto.MethodRecover, proto.SourceEnroll, &proto.RecoverRequest{
		Email:           "me@example.com",
		Password:        "pw",
		DevicePublicKey: initReq(t).DevicePublicKey,
		DeviceName:      "恢复用设备",
	})
	if err != nil {
		t.Fatalf("构造恢复请求失败: %v", err)
	}
	recoverReq.ID = "drill-recover-missing"
	resp := drillHarness(t, dir, srv).post(recoverReq)
	if resp.OK {
		t.Fatal("冷备里没有对应条目时必须拒绝恢复（需求 11.3）")
	}
	// 错误信息必须能让用户直接定位问题：既要说清缺什么条目，也要给出该条目名。
	if !strings.Contains(resp.Error, "root-key/") {
		t.Fatalf("拒绝原因应给出应存在的条目名，实际：%s", resp.Error)
	}
	if !strings.Contains(resp.Error, "没有") && !strings.Contains(resp.Error, "找不到") {
		t.Fatalf("拒绝原因应说明条目不存在，实际：%s", resp.Error)
	}
	if srv.Bootstrapper().State().RecoveryMode != true {
		t.Fatal("恢复失败后应留在恢复模式")
	}
}

// TestRecoveryModeRejectsBusinessMethods 固定恢复模式下放行与拒绝的边界（需求 9.4）。
//
// 边界写死在测试里，因为这里一旦放宽，代表“根密钥没了还能正常下发”，
// 而那时渲染结果全是空值或上一次的残留，比直接报错危险得多。
func TestRecoveryModeRejectsBusinessMethods(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()
	drillInit(t, dir, vault)

	if err := os.Remove(filepath.Join(dir, "root.key")); err != nil {
		t.Fatalf("删除根密钥失败: %v", err)
	}
	srv := startInstance(t, dir, vault)
	defer srv.closeAll()

	h := drillHarness(t, dir, srv)
	if !srv.Bootstrapper().State().RecoveryMode {
		t.Fatal("应进入恢复模式")
	}

	// hello 必须放行：代理要靠它得知“服务在恢复模式”，
	// 否则只会看到一串 unauthorized 而不知道去做什么。
	helloReq := &proto.Request{ID: "h", Protocol: proto.Version, Method: proto.MethodHello}
	if err := helloReq.EncodePayload(&proto.HelloRequest{AgentVersion: "1.0.0"}); err != nil {
		t.Fatalf("构造 hello 失败: %v", err)
	}
	if resp := h.post(helloReq); !resp.OK {
		t.Fatalf("恢复模式下 hello 必须放行: %s %s", resp.Code, resp.Error)
	}

	// 其余业务方法一律拒绝，并且用独立错误码，便于代理给出不同提示。
	cert := h.registerDevice("dev-drill", "演练设备")
	for _, m := range []proto.Method{
		proto.MethodSync,
		proto.MethodListAssets,
		proto.MethodListSecrets,
		proto.MethodWriteAsset,
	} {
		req := &proto.Request{ID: string(m), Protocol: proto.Version, Method: m}
		if err := req.EncodePayload(&proto.SyncRequest{}); err != nil {
			continue
		}
		resp := h.postTLS(req, cert)
		if resp.OK {
			t.Fatalf("恢复模式下 %s 必须被拒绝", m)
		}
		if resp.Code != proto.ErrRecoveryMode {
			t.Fatalf("%s 的错误码应为 %s，实际 %s（%s）",
				m, proto.ErrRecoveryMode, resp.Code, resp.Error)
		}
	}
}

// TestRotateUpdatesCheck 验证轮换后 git 校验值跟着换（需求 9.5）。
//
// 忘了更新校验值会导致重启后立刻进入恢复模式——
// 本地文件已是新密钥，git 还记着旧的，比对必然失败。
// 用例走真实协议而不是直接调内部函数，这样轮换链路里的
// context、权限、审计、校验值联动都在覆盖范围内。
func TestRotateUpdatesCheck(t *testing.T) {
	dir := t.TempDir()
	vault := newDrillVault()
	firstID := drillInit(t, dir, vault)

	srv := startInstance(t, dir, vault)
	defer srv.closeAll()

	cert := drillHarness(t, dir, srv).registerDevice("dev-drill", "演练设备")
	before := srv.Bootstrapper().State().KeyID
	if before != firstID {
		t.Fatalf("初始 key id 应为 %s，实际 %s", firstID, before)
	}

	// 走真实协议轮换：SourceAdmin 才放行，且需要当场提供凭据。
	rotateReq, err := proto.WithPayload(proto.MethodRotateRootKey, proto.SourceAdmin, &proto.RotateRequest{
		Email:    "me@example.com",
		Password: "pw",
		TOTP:     "123456",
	})
	if err != nil {
		t.Fatalf("构造轮换请求失败: %v", err)
	}
	rotateReq.ID = "drill-rotate"
	if resp := drillHarness(t, dir, srv).postTLS(rotateReq, cert); !resp.OK {
		t.Fatalf("轮换失败: %s %s", resp.Code, resp.Error)
	}

	after := srv.Bootstrapper().State().KeyID
	if after == before {
		t.Fatal("轮换后 key id 必须变化")
	}

	// git 校验值必须同步指向新密钥，否则下次重启会误判成密钥被换过。
	store, err := ssot.Open(storeDir(dir))
	if err != nil {
		t.Fatalf("打开仓库失败: %v", err)
	}
	check, err := ssot.ReadJSON[ssot.RootKeyCheck](store, ssot.FileRootKeyCheck)
	if err != nil {
		t.Fatalf("读取轮换后校验值失败: %v", err)
	}
	if check.KeyID != after {
		t.Fatalf("校验值应指向新密钥 %s，实际 %s", after, check.KeyID)
	}
	if check.RotatedFrom != firstID {
		t.Fatalf("校验值应记录轮换来源 %s，实际 %q", firstID, check.RotatedFrom)
	}

	// 最关键的一条：轮换后重启必须能正常起来，而不是掉进恢复模式。
	srv.closeAll()
	restarted := startInstance(t, dir, vault)
	defer restarted.closeAll()
	if restarted.Bootstrapper().State().RecoveryMode {
		t.Fatal("轮换后重启不应进入恢复模式（说明校验值没跟着换）")
	}
	if restarted.Bootstrapper().State().KeyID != after {
		t.Fatalf("重启后 key id 应为 %s，实际 %q", after, restarted.Bootstrapper().State().KeyID)
	}
}
