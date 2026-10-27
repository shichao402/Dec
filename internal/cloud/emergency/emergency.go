// Package emergency 实现服务器本机的应急管理命令（需求 18.12）。
//
// 定位：**Bitwarden 也不可用时的最后手段**。
//
// 正常流程永远走代理：吊销设备在管理面点、登记新设备走 Bitwarden 主密码 + 2FA。
// 那套流程的前提是「用户能登录 Bitwarden」，而这个前提在两种场景下不成立：
// 账户被锁、2FA 设备丢失。那时如果没有任何本机入口，就只能手改仓库文件，
// 而手改 devices.json 会绕过审计与证书一致性检查，留下更难收的烂摊子。
//
// 因此这里只做两件事，都在服务器本机、由能读到数据目录的人执行：
//
//  1. 吊销设备：标记为已吊销，立即生效（服务端每次请求都查登记表）。
//  2. 为指定公钥直接签发证书：让一台新机器能连上，然后由它自己走正常流程。
//
// 刻意不做的事：
//
//   - 不提供「查看或导出根密钥」。那等于开了一条绕过 Bitwarden 的取密钥通道。
//   - 不提供「修改设备类型」「删除设备」等更多能力。能力面越小越安全。
//   - 不做网络访问。只读写本地数据目录，不需要联网也能用。
//   - 不常驻、不监听端口。它是 CLI，跑完退出，没有可被攻击的服务面。
package emergency

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// 数据目录内的相对路径。
//
// 与 cloudsvc 的布局保持一致，但刻意不 import cloudsvc：
// 那个包的 New() 会拉起告警、冷备、审计等一整套后台协程，
// 应急命令只要读写两个文件，不该顺手启动一个常驻服务。
const (
	ssotSubdir  = "ssot"
	certSubdir  = "certs"
	auditSubdir = "audit"
)

// SourceEmergency 是应急操作在审计里使用的来源标记。
//
// 与 admin（管理面）、mcp 都不同，方便事后一眼区分
// 「有人登录服务器敲了命令」和「日常操作」。
const SourceEmergency = "emergency"

// Env 是应急命令需要的最小环境。
type Env struct {
	Store    *ssot.Store
	Registry *identity.Registry
	CA       *identity.CA
	Audit    *audit.Log
	// caErr 是 CA 不可用的原因；只有签发证书需要 CA。
	caErr error
	// AuditWarn 收到审计写入或打开失败的通知。
	//
	// 应急场景下「把设备吊销掉」比「审计一定写成功」更重要，
	// 所以失败不中断主操作；但必须显式说出来，不能静默吞掉。
	AuditWarn func(error)
}

// Open 打开数据目录，装配应急命令所需的最小依赖。
//
// 不需要根密钥：吊销与签发证书只用登记表和 CA 私钥，不解任何密文。
// 这也意味着根密钥丢失、服务处于恢复模式时应急命令依然可用——
// 那恰恰是最需要它的时候。
func Open(dataDir string) (*Env, error) {
	if dataDir == "" {
		return nil, errors.New("缺少数据目录")
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("解析数据目录失败: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("数据目录不可访问: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("数据目录不是一个目录: %s", abs)
	}
	if err := checkOwner(abs, info); err != nil {
		return nil, err
	}

	store, err := ssot.Open(filepath.Join(abs, ssotSubdir))
	if err != nil {
		return nil, fmt.Errorf("打开 SSOT 仓库失败: %w", err)
	}
	// CA 缺失不阻止打开：吊销只改登记表，不需要 CA（ADR 0052）。
	// 服务端 CA 丢失、等待从 Bitwarden 恢复的时候，恰恰最需要能吊销设备。
	ca, caErr := loadCA(filepath.Join(abs, certSubdir), store)

	env := &Env{
		Store:    store,
		Registry: identity.NewRegistry(store),
		CA:       ca,
		caErr:    caErr,
	}
	auditLog, auditErr := audit.Open(filepath.Join(abs, auditSubdir))
	if auditErr != nil {
		env.AuditWarn = func(err error) {
			fmt.Fprintf(os.Stderr, "警告：审计不可用，本次操作没有留痕：%v\n", err)
		}
		env.AuditWarn(auditErr)
	} else {
		env.Audit = auditLog
		env.AuditWarn = func(err error) {
			fmt.Fprintf(os.Stderr, "警告：%v\n", err)
		}
	}
	return env, nil
}

// Close 收尾。
func (e *Env) Close() error {
	if e.Audit != nil {
		return e.Audit.Close()
	}
	return nil
}

// loadCA 读取服务端身份，并与 git 记录的指纹核对（ADR 0052）。
//
// 与服务端启动时的判定共用 identity.LoadLocalCA，但**这里绝不创建**：
// 应急命令跑到一个没有身份的目录上，要么数据目录指错了，要么 CA 正等着
// 从 Bitwarden 恢复；此时自动生成新 CA 会签出一批连不上任何地方的证书，
// 还覆盖掉排查线索。
//
// 指纹也要核对：本机 CA 与 git 记录不一致时签出来的证书，
// 服务端启动后会判为不一致而拒绝业务，签了等于白签。
func loadCA(dir string, store *ssot.Store) (*identity.CA, error) {
	ca, err := identity.LoadLocalCA(dir)
	if err != nil {
		return nil, fmt.Errorf("服务端身份不可用（目录 %s）: %w", dir, err)
	}
	if ca == nil {
		return nil, fmt.Errorf("本机没有服务端 CA 私钥（目录 %s）：应急命令不会生成新的 CA；"+
			"请确认数据目录，或先用 dec-agent recover 从 Bitwarden 取回 CA", dir)
	}
	if rec, ok, rerr := identity.ReadCARecord(store); rerr == nil && ok {
		if fp := identity.CAFingerprint(ca); fp != rec.Fingerprint {
			return nil, fmt.Errorf("本机服务端 CA 指纹 %s 与 git 记录 %s 不一致，拒绝用它签发", fp, rec.Fingerprint)
		}
	}
	return ca, nil
}

// RevokeDevice 吊销一台设备（需求 18.12）。
//
// 返回吊销后的设备记录，便于调用方打印出来核对。
func (e *Env) RevokeDevice(deviceID, reason string) (ssot.Device, error) {
	if deviceID == "" {
		return ssot.Device{}, errors.New("必须指定设备 ID")
	}
	// 先确认存在且当前未吊销：重复吊销不该报「成功」，
	// 否则现场会误判成「我刚吊销了它」，而真实情况是它早就被吊销了。
	dev, err := e.Registry.Device(deviceID)
	if err != nil {
		return ssot.Device{}, err
	}
	if dev.Revoked {
		return ssot.Device{}, fmt.Errorf("设备 %s 早在 %s 就被吊销了",
			deviceID, formatTime(dev.RevokedAt))
	}

	if reason == "" {
		reason = "服务器本机应急命令"
	}
	updated, err := e.Registry.Revoke(deviceID, reason, ssot.Author{
		Name:   "emergency",
		Email:  "emergency@dec.local",
		Source: SourceEmergency,
	})
	if err != nil {
		return ssot.Device{}, err
	}
	e.record(audit.EventRevoke, deviceID, "应急吊销："+reason)
	return updated, nil
}

// IssueCert 为指定公钥直接签发设备证书（需求 18.12）。
//
// 签出来的是**只接收设备**，不是「我的设备」。理由很直接：
// 绕过 2FA 签出来的身份，不该同时具备管理设备、查看明文、轮换根密钥的能力。
// 真需要管理能力，拿这台只接收设备连上之后再走正常登记流程。
func (e *Env) IssueCert(deviceID, pubKeyPEM, name string) (*identity.Issued, ssot.Device, error) {
	if deviceID == "" {
		return nil, ssot.Device{}, errors.New("必须指定设备 ID")
	}
	if pubKeyPEM == "" {
		return nil, ssot.Device{}, errors.New("必须提供设备公钥（PEM）")
	}
	if e.CA == nil {
		return nil, ssot.Device{}, fmt.Errorf("无法签发证书: %w", e.caErr)
	}
	pub, err := identity.ParsePublicKey([]byte(pubKeyPEM))
	if err != nil {
		return nil, ssot.Device{}, fmt.Errorf("公钥不可用: %w", err)
	}

	// 已登记设备不允许借这条路重签：重签会换掉证书序列号，
	// 使原有证书立即失效，等于在用户不知情时踢掉一台正在用的设备。
	existing, err := e.Registry.Device(deviceID)
	switch {
	case err == nil:
		return nil, ssot.Device{}, fmt.Errorf(
			"设备 %s 已存在（%s，吊销=%v）；应急命令不为已登记设备重签证书",
			deviceID, existing.Name, existing.Revoked)
	case !errors.Is(err, ssot.ErrNotFound):
		return nil, ssot.Device{}, err
	}

	issued, err := e.CA.SignDevice(deviceID, pub, time.Time{})
	if err != nil {
		return nil, ssot.Device{}, err
	}
	if name == "" {
		name = deviceID
	}
	dev := ssot.Device{
		ID:           deviceID,
		Name:         name,
		Kind:         ssot.DeviceReceiver,
		Fingerprint:  issued.Fingerprint,
		CertSerial:   issued.Serial,
		CertNotAfter: issued.NotAfter,
		RegisteredAt: time.Now().UTC(),
	}
	if err := e.Registry.Add(dev, ssot.Author{
		Name:   "emergency",
		Email:  "emergency@dec.local",
		Source: SourceEmergency,
	}); err != nil {
		return nil, ssot.Device{}, err
	}
	e.record(audit.EventEnroll, deviceID, "应急签发证书（只接收设备）")
	return issued, dev, nil
}

// ListDevices 列出全部设备，供应急时先看清现状。
func (e *Env) ListDevices() ([]ssot.Device, error) {
	return e.Registry.Devices()
}

// record 写审计。
func (e *Env) record(event audit.Event, subject, detail string) {
	if e.Audit == nil {
		return
	}
	err := e.Audit.Append(audit.Entry{
		At:      time.Now().UTC(),
		Event:   event,
		Source:  SourceEmergency,
		Subject: subject,
		Detail:  detail,
		OK:      true,
	})
	if err != nil && e.AuditWarn != nil {
		e.AuditWarn(fmt.Errorf("写审计失败: %w", err))
	}
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "未知时间"
	}
	return t.UTC().Format(time.RFC3339)
}
