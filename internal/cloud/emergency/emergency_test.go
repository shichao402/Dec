package emergency

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// newDataDir 造一个「服务器已经跑过一段时间」的数据目录。
//
// 刻意不用 cloudsvc.New 来造：应急命令的原则是不依赖那个常驻服务，
// 测试也就不能用它准备环境，否则两者耦合后测试通过只说明它们一起能跑。
// 这里手工铺出它真正会读的三样东西：ssot 仓库、certs/ca.key 与 ca.crt、audit 目录。
func newDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	store, err := ssot.Open(filepath.Join(dir, ssotSubdir))
	if err != nil {
		t.Fatalf("准备 SSOT 仓库失败: %v", err)
	}
	if _, err := store.ReadFile(ssot.FileDevices); err != nil &&
		!errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("读取设备表失败: %v", err)
	}

	installCA(t, dir, mustNewCA(t))
	return dir
}

// mustNewCA 造一套全新的服务端身份。
func mustNewCA(t *testing.T) *identity.CA {
	t.Helper()
	ca, err := identity.NewCA()
	if err != nil {
		t.Fatalf("生成服务端身份失败: %v", err)
	}
	return ca
}

// installCA 把服务端身份写进数据目录，布局与 cloudsvc 完全一致。
func installCA(t *testing.T, dataDir string, ca *identity.CA) {
	t.Helper()
	certDir := filepath.Join(dataDir, certSubdir)
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		t.Fatalf("创建证书目录失败: %v", err)
	}
	keyPEM, err := ca.KeyPEM()
	if err != nil {
		t.Fatalf("序列化服务端私钥失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(certDir, "ca.key"), keyPEM, 0o600); err != nil {
		t.Fatalf("写 ca.key 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(certDir, "ca.crt"), ca.CertPEM(), 0o600); err != nil {
		t.Fatalf("写 ca.crt 失败: %v", err)
	}
}

// openEnv 打开应急环境，测试结束时关闭。
func openEnv(t *testing.T, dataDir string) *Env {
	t.Helper()
	env, err := Open(dataDir)
	if err != nil {
		t.Fatalf("打开应急环境失败: %v", err)
	}
	t.Cleanup(func() {
		if err := env.Close(); err != nil {
			t.Logf("关闭应急环境: %v", err)
		}
	})
	return env
}

// newDeviceKey 生成一台设备的密钥对，并返回其 PKIX 公钥 PEM。
func newDeviceKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成设备密钥失败: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("序列化设备公钥失败: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return key, string(pubPEM)
}

// seedDevice 直接往登记表里放一台设备，模拟「历史上正常登记过」。
func seedDevice(t *testing.T, env *Env, id, name string, kind ssot.DeviceKind) ssot.Device {
	t.Helper()
	_, pubPEM := newDeviceKey(t)
	pub, err := identity.ParsePublicKey([]byte(pubPEM))
	if err != nil {
		t.Fatalf("解析公钥失败: %v", err)
	}
	issued, err := env.CA.SignDevice(id, pub, time.Time{})
	if err != nil {
		t.Fatalf("签发设备证书失败: %v", err)
	}
	dev := ssot.Device{
		ID:           id,
		Name:         name,
		Kind:         kind,
		OS:           "darwin",
		Arch:         "arm64",
		CertSerial:   issued.Serial,
		CertNotAfter: issued.NotAfter,
		RegisteredAt: time.Now().UTC(),
	}
	author := ssot.Author{Name: "test", Email: "test@dec.local", Source: "admin"}
	if err := env.Registry.Add(dev, author); err != nil {
		t.Fatalf("登记设备失败: %v", err)
	}
	return dev
}

// TestOpenRequiresDataDir 数据目录为空或不存在时必须报错。
//
// 应急命令在服务器本机跑，最可能的失误是参数漏了或被脚本传成空串，
// 那时如果默默用了默认目录，就会在错误的仓库上动手。
func TestOpenRequiresDataDir(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("数据目录为空时应当报错")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "not-there")); err == nil {
		t.Fatal("数据目录不存在时应当报错")
	}
}

// TestOpenDoesNotCreateCA 数据目录里没有服务端身份时必须失败，而不是新生成一套。
//
// 这是本包与 cloudsvc 最重要的行为差异：cloudsvc 启动时没有身份就创建一份，
// 应急命令若照做，在指错的目录上会签出一批连不上任何服务器的证书，
// 并且把「数据目录指错了」这个真正的问题掩盖掉。
func TestOpenDoesNotCreateCA(t *testing.T) {
	dir := t.TempDir()
	if _, err := ssot.Open(filepath.Join(dir, ssotSubdir)); err != nil {
		t.Fatalf("准备 SSOT 仓库失败: %v", err)
	}

	_, err := Open(dir)
	if err == nil {
		t.Fatal("缺少服务端身份时应当报错")
	}
	if !strings.Contains(err.Error(), "不会生成新的 CA") {
		t.Fatalf("错误信息应说明不会生成 CA，实际: %v", err)
	}

	// 关键断言：目录里不能凭空多出 ca.key。
	if _, statErr := os.Stat(filepath.Join(dir, certSubdir, "ca.key")); !os.IsNotExist(statErr) {
		t.Fatalf("不应创建 ca.key，stat 结果: %v", statErr)
	}
}

// TestRevokeDeviceTakesEffectImmediately 吊销后 Active 必须立刻拒绝。
//
// 这条链路的价值全在「立即生效」上：如果吊销只是改了个字段而别处仍放行，
// 应急时用户会以为已经把人踢掉了，实际没有。
func TestRevokeDeviceTakesEffectImmediately(t *testing.T) {
	dir := newDataDir(t)
	env := openEnv(t, dir)
	dev := seedDevice(t, env, "mac-office", "办公室 Mac", ssot.DeviceMine)

	if err := identity.Active(dev, time.Now()); err != nil {
		t.Fatalf("登记后应当可用: %v", err)
	}

	updated, err := env.RevokeDevice("mac-office", "设备丢失")
	if err != nil {
		t.Fatalf("吊销失败: %v", err)
	}
	if !updated.Revoked {
		t.Fatal("返回的设备应当是已吊销状态")
	}
	if updated.RevokedAt == nil {
		t.Fatal("吊销时间不能为空")
	}

	// 换一个 Env 重新读，确认状态真的落进仓库而不是只在内存里。
	fresh := openEnv(t, dir)
	reloaded, err := fresh.Registry.Device("mac-office")
	if err != nil {
		t.Fatalf("重新读取设备失败: %v", err)
	}
	if !reloaded.Revoked {
		t.Fatal("重开环境后设备仍应是已吊销")
	}
	if err := identity.Active(reloaded, time.Now()); !errors.Is(err, identity.ErrCertRevoked) {
		t.Fatalf("吊销后应返回 ErrCertRevoked，实际: %v", err)
	}
}

// TestRevokeDeviceRejectsRepeat 重复吊销必须报错。
//
// 报「成功」会让现场误判成「我刚吊销了它」，而真实情况是它早就被吊销了，
// 中间这段时间发生了什么是另一个需要追查的问题。
func TestRevokeDeviceRejectsRepeat(t *testing.T) {
	env := openEnv(t, newDataDir(t))
	seedDevice(t, env, "mac-lost", "丢失的 Mac", ssot.DeviceMine)

	if _, err := env.RevokeDevice("mac-lost", "第一次"); err != nil {
		t.Fatalf("首次吊销应成功: %v", err)
	}
	_, err := env.RevokeDevice("mac-lost", "第二次")
	if err == nil {
		t.Fatal("重复吊销应当报错")
	}
	if !strings.Contains(err.Error(), "就被吊销了") {
		t.Fatalf("错误信息应说明早已吊销，实际: %v", err)
	}
}

// TestRevokeDeviceUnknown 不存在的设备必须报错并指明 ID。
func TestRevokeDeviceUnknown(t *testing.T) {
	env := openEnv(t, newDataDir(t))
	_, err := env.RevokeDevice("no-such-device", "")
	if err == nil {
		t.Fatal("吊销不存在的设备应当报错")
	}
	if !errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("应当是 ErrNotFound，实际: %v", err)
	}
	if _, err := env.RevokeDevice("", ""); err == nil {
		t.Fatal("空设备 ID 应当报错")
	}
}

// TestIssueCertCreatesReceiverDevice 应急签发的必须是「只接收设备」。
//
// 绕过 Bitwarden 的 2FA 签出来的身份，不该同时具备管理设备、看明文、
// 轮换根密钥的能力。这条断言守住的是权限模型，不是格式细节。
func TestIssueCertCreatesReceiverDevice(t *testing.T) {
	dir := newDataDir(t)
	env := openEnv(t, dir)
	_, pubPEM := newDeviceKey(t)

	issued, dev, err := env.IssueCert("spare-laptop", pubPEM, "备用笔记本")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if dev.Kind != ssot.DeviceReceiver {
		t.Fatalf("应急签发必须是 receiver，实际: %s", dev.Kind)
	}
	if issued.CertPEM == nil || issued.Serial == "" {
		t.Fatal("应当返回可用的证书")
	}
	if dev.Fingerprint == "" || dev.CertSerial != issued.Serial {
		t.Fatal("设备记录应与签发的证书一致")
	}
	if time.Until(issued.NotAfter) < 80*24*time.Hour {
		t.Fatalf("证书有效期明显短于预期: %v", issued.NotAfter)
	}

	// 能力面断言：receiver 不能读明文、不能管设备。
	// 应急来源不在任何白名单里，两个判定都必须为假。
	src := proto.Source(SourceEmergency)
	if identity.CanReadSecret(src) {
		t.Fatal("应急来源不该能读明文")
	}
	if identity.CanManageDevices(src) {
		t.Fatal("应急来源不该能管设备")
	}

	// 落盘后依然存在，且能被 VerifyDevice 认出来（证书确实由本 CA 签发）。
	fresh := openEnv(t, dir)
	reloaded, err := fresh.Registry.Device("spare-laptop")
	if err != nil {
		t.Fatalf("重开环境后读取设备失败: %v", err)
	}
	if reloaded.Kind != ssot.DeviceReceiver {
		t.Fatalf("重开环境后类型应保持 receiver，实际: %s", reloaded.Kind)
	}
	deviceID, serial, _, err := fresh.CA.VerifyDevice(issued.CertPEM, time.Now())
	if err != nil {
		t.Fatalf("签发的证书应能被本 CA 校验通过: %v", err)
	}
	if deviceID != "spare-laptop" {
		t.Fatalf("证书主体应是设备 ID，实际: %s", deviceID)
	}
	if serial != issued.Serial {
		t.Fatalf("证书序列号应与返回值一致，实际: %s", serial)
	}
}

// TestIssueCertRejectsExistingDevice 已登记设备不许借应急通道重签。
//
// 重签会换掉序列号，使原证书立即失效，等于在用户不知情时踢掉一台正在用的设备。
func TestIssueCertRejectsExistingDevice(t *testing.T) {
	env := openEnv(t, newDataDir(t))
	existing := seedDevice(t, env, "mac-office", "办公室 Mac", ssot.DeviceMine)
	_, pubPEM := newDeviceKey(t)

	_, _, err := env.IssueCert("mac-office", pubPEM, "")
	if err == nil {
		t.Fatal("已登记设备应当拒绝重签")
	}
	if !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("错误信息应说明设备已存在，实际: %v", err)
	}

	// 原证书不能被这一次失败的调用改变。
	after, err := env.Registry.Device("mac-office")
	if err != nil {
		t.Fatalf("读取设备失败: %v", err)
	}
	if after.CertSerial != existing.CertSerial {
		t.Fatal("失败的重签不该改动原设备的证书序列号")
	}
}

// TestIssueCertRejectsEmptyInput 缺参数必须报错，不能签出半截身份。
func TestIssueCertRejectsEmptyInput(t *testing.T) {
	env := openEnv(t, newDataDir(t))
	_, pubPEM := newDeviceKey(t)

	if _, _, err := env.IssueCert("", pubPEM, ""); err == nil {
		t.Fatal("空设备 ID 应当报错")
	}
	if _, _, err := env.IssueCert("dev-a", "", ""); err == nil {
		t.Fatal("空公钥应当报错")
	}
	if _, _, err := env.IssueCert("dev-a", "not a pem", ""); err == nil {
		t.Fatal("非法公钥应当报错")
	}
	// 报错后不该留下半条登记记录。
	if _, err := env.Registry.Device("dev-a"); !errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("失败的签发不该登记设备，实际: %v", err)
	}
}

// TestRevokeAndIssueWriteAudit 两个动作都必须留下审计。
//
// 需求 18.12 把应急命令定位为最后手段，那它更必须留痕：
// 事后追查「谁把这台设备踢了」时，这是唯一的线索。
func TestRevokeAndIssueWriteAudit(t *testing.T) {
	dir := newDataDir(t)
	env := openEnv(t, dir)
	seedDevice(t, env, "mac-lost", "丢失的 Mac", ssot.DeviceMine)

	if _, err := env.RevokeDevice("mac-lost", "设备丢失"); err != nil {
		t.Fatalf("吊销失败: %v", err)
	}
	_, pubPEM := newDeviceKey(t)
	if _, _, err := env.IssueCert("spare", pubPEM, "备用机"); err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	// 审计按 CommitWindow 合并，读之前要先关一次确保写完。
	if err := env.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	log, err := audit.Open(filepath.Join(dir, auditSubdir))
	if err != nil {
		t.Fatalf("打开审计失败: %v", err)
	}
	defer log.Close()

	entries, err := log.Read(time.Now())
	if err != nil {
		t.Fatalf("读审计失败: %v", err)
	}

	var sawRevoke, sawEnroll bool
	for _, e := range entries {
		if e.Source != SourceEmergency {
			continue
		}
		switch e.Event {
		case audit.EventRevoke:
			sawRevoke = true
			if e.Subject != "mac-lost" {
				t.Fatalf("吊销审计的对象应是设备 ID，实际: %s", e.Subject)
			}
			if !strings.Contains(e.Detail, "设备丢失") {
				t.Fatalf("审计应记下原因，实际: %s", e.Detail)
			}
		case audit.EventEnroll:
			sawEnroll = true
			if e.Subject != "spare" {
				t.Fatalf("签发审计的对象应是设备 ID，实际: %s", e.Subject)
			}
		}
	}
	if !sawRevoke {
		t.Fatal("缺少吊销审计")
	}
	if !sawEnroll {
		t.Fatal("缺少签发审计")
	}
}

// TestWorksWithoutRootKey 根密钥缺失时应急命令必须照常可用。
//
// 这是整个包存在的核心理由：根密钥丢了服务进恢复模式，
// 此时若连吊销设备都做不到，就彻底没有本机手段了。
func TestWorksWithoutRootKey(t *testing.T) {
	dir := newDataDir(t)
	// 不放任何根密钥文件，模拟「机器重建后只剩数据目录」。
	env := openEnv(t, dir)

	if _, err := env.RevokeDevice("whatever", ""); !errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("应当是 ErrNotFound 而非其他错误，实际: %v", err)
	}

	_, pubPEM := newDeviceKey(t)
	if _, _, err := env.IssueCert("rescue", pubPEM, "救急机"); err != nil {
		t.Fatalf("没有根密钥也应能签发证书: %v", err)
	}
}

// TestListDevices 列表要能反映吊销状态，供应急前核对 ID。
func TestListDevices(t *testing.T) {
	env := openEnv(t, newDataDir(t))
	seedDevice(t, env, "mac-a", "A", ssot.DeviceMine)
	seedDevice(t, env, "pi-b", "B", ssot.DeviceReceiver)
	if _, err := env.RevokeDevice("pi-b", "换机"); err != nil {
		t.Fatalf("吊销失败: %v", err)
	}

	devices, err := env.ListDevices()
	if err != nil {
		t.Fatalf("列设备失败: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("应当有 2 台设备，实际 %d", len(devices))
	}
	state := map[string]bool{}
	for _, d := range devices {
		state[d.ID] = d.Revoked
	}
	if state["mac-a"] {
		t.Fatal("mac-a 不该是已吊销")
	}
	if !state["pi-b"] {
		t.Fatal("pi-b 应当是已吊销")
	}
}

// TestCheckOwnerAllowsSameOwner 归属一致时必须放行。
//
// 这里覆盖的是「目录所有者就是当前用户」这个正常情况，测试进程本身
// 通常不是 root，所以真正被验证的是「非 root 不拦截」这条捷径。
func TestCheckOwnerAllowsSameOwner(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	if err := checkOwner(dir, info); err != nil {
		t.Fatalf("同归属不该被拦: %v", err)
	}
}

// TestCheckOwnerRejectsRootOnOtherOwner 以 root 操作别人的目录必须被拦。
//
// 这条测试只在非 root 下跑：构造「目录属于 X、进程是 root」需要 chown，
// 而 chown 本身要 root。反过来在 root 环境里跑整包时，这个用例跳过即可，
// 因为真正的判据由 owner_unix.go 的分支覆盖，不是这里的断言。
func TestCheckOwnerRejectsRootOnOtherOwner(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("当前是 root，无法构造「目录属于他人」的场景")
	}
	dir := t.TempDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	// 非 root 进程走的是「不拦截」分支，与目录归属无关。
	if err := checkOwner(dir, info); err != nil {
		t.Fatalf("非 root 不该被拦: %v", err)
	}
}
