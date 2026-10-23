// Package localcert 管理 local.dec.firoyang.com 的证书（agent-tray-access 需求 4.4、4.6）。
//
// 这个子域在公网 DNS 上解析到 127.0.0.1，每台设备的代理都在自己的回环上用它
// 提供 HTTPS。证书由云端统一签发，再经设备通道下发；设备上不保存任何 DNS
// 凭据，新设备登记后自动可用。
//
// 证书与私钥存进 SSOT 的 config/local-tls.json：证书是公开信息，明文保存；
// 私钥用根密钥加密，和其他密钥一样可以安全地进入 git 历史与冷备。
//
// 续期策略：
//
//   - 没有证书、证书距到期不足 RenewBefore，或被标记为需要换钥时续期；
//   - 每次签发都生成全新的私钥；
//   - 设备被吊销时标记需要换钥，下一次检查立即重签，吊销设备手里的旧私钥
//     不会再被任何设备使用（需求 4.6）。
package localcert

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// File 是证书在 SSOT 里的路径。
const File = ssot.DirConfig + "/local-tls.json"

// DefaultDomain 是默认的本机子域。
const DefaultDomain = "local.dec.firoyang.com"

// 默认时间参数。
const (
	DefaultRenewBefore = 30 * 24 * time.Hour
	DefaultCheckEvery  = 12 * time.Hour
	DefaultRetryAfter  = time.Hour
)

// ErrNotConfigured 表示签发所需的配置（如 DNSPod 凭据）还没准备好。
//
// 这是全新部署的正常状态，管理器只记一次日志，不告警。
var ErrNotConfigured = errors.New("本机证书签发未配置")

// Issued 是一次签发的结果。
type Issued struct {
	// CertPEM 是完整证书链（叶子证书在前）。
	CertPEM []byte
	// KeyPEM 是私钥（PKCS#8 PEM）。
	KeyPEM []byte
}

// IssueFunc 为 domain 签发一张新证书，每次调用都必须生成新私钥。
type IssueFunc func(ctx context.Context, domain string) (*Issued, error)

// Cert 是解密后的当前证书。
type Cert struct {
	Domain   string
	CertPEM  []byte
	KeyPEM   []byte
	NotAfter time.Time
	IssuedAt time.Time
	// Serial 是叶子证书序列号（十六进制），代理据此判断是否需要重新落盘。
	Serial string
}

// record 是 config/local-tls.json 的磁盘格式。
type record struct {
	Domain   string        `json:"domain"`
	CertPEM  string        `json:"cert_pem"`
	Key      *crypt.Sealed `json:"key"`
	NotAfter time.Time     `json:"not_after"`
	IssuedAt time.Time     `json:"issued_at"`
	Serial   string        `json:"serial"`
	// RekeyPending 表示有设备被吊销，下一次检查必须换钥重签。
	RekeyPending bool   `json:"rekey_pending,omitempty"`
	RekeyReason  string `json:"rekey_reason,omitempty"`
}

// Options 是管理器参数。
type Options struct {
	Store  *ssot.Store
	Domain string
	// RootKey 返回当前根密钥；恢复模式下为 nil，此时既不签发也不下发。
	RootKey func() *crypt.Key
	// Issue 为 nil 表示本机证书功能关闭。
	Issue IssueFunc

	RenewBefore time.Duration
	CheckEvery  time.Duration
	RetryAfter  time.Duration
	Now         func() time.Time

	// OnIssued 在新证书写入 SSOT 后调用，用于唤醒设备长轮询。
	OnIssued func()
	// OnFailure 在签发失败时调用（ErrNotConfigured 除外），用于告警。
	OnFailure func(err error)
	Logf      func(format string, args ...any)
}

// Manager 负责判断、签发与持久化本机证书。
type Manager struct {
	opts Options
	// mu 串行化签发：后台循环与吊销触发可能同时发生。
	mu sync.Mutex
	// notConfiguredLogged 让「未配置」只记一次日志。
	notConfiguredLogged bool
}

// New 构造管理器。
func New(opts Options) *Manager {
	if opts.Domain == "" {
		opts.Domain = DefaultDomain
	}
	if opts.RenewBefore <= 0 {
		opts.RenewBefore = DefaultRenewBefore
	}
	if opts.CheckEvery <= 0 {
		opts.CheckEvery = DefaultCheckEvery
	}
	if opts.RetryAfter <= 0 {
		opts.RetryAfter = DefaultRetryAfter
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	return &Manager{opts: opts}
}

// Domain 返回管理的域名。
func (m *Manager) Domain() string { return m.opts.Domain }

// Enabled 报告签发功能是否开启。
func (m *Manager) Enabled() bool { return m.opts.Issue != nil }

func (m *Manager) load() (*record, error) {
	rec, err := ssot.ReadJSON[record](m.opts.Store, File)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

// Current 返回解密后的当前证书；没有证书时返回 (nil, nil)。
//
// 根密钥未加载或解密失败时返回错误：调用方（下发链路）应当跳过证书，
// 而不是下发一张没有私钥的证书。
func (m *Manager) Current() (*Cert, error) {
	rec, err := m.load()
	if err != nil || rec == nil {
		return nil, err
	}
	if rec.Domain != m.opts.Domain {
		return nil, nil
	}
	key := m.rootKey()
	if key == nil {
		return nil, errors.New("根密钥未加载")
	}
	keyPEM, err := key.Open(rec.Key)
	if err != nil {
		return nil, fmt.Errorf("解密本机证书私钥失败: %w", err)
	}
	return &Cert{
		Domain:   rec.Domain,
		CertPEM:  []byte(rec.CertPEM),
		KeyPEM:   keyPEM,
		NotAfter: rec.NotAfter,
		IssuedAt: rec.IssuedAt,
		Serial:   rec.Serial,
	}, nil
}

func (m *Manager) rootKey() *crypt.Key {
	if m.opts.RootKey == nil {
		return nil
	}
	return m.opts.RootKey()
}

// NeedsRenewal 报告是否需要签发，并给出原因。
func (m *Manager) NeedsRenewal() (bool, string, error) {
	rec, err := m.load()
	if err != nil {
		return false, "", err
	}
	return m.needsRenewal(rec)
}

func (m *Manager) needsRenewal(rec *record) (bool, string, error) {
	now := m.opts.Now()
	switch {
	case rec == nil:
		return true, "尚无证书", nil
	case rec.Domain != m.opts.Domain:
		return true, "域名变更为 " + m.opts.Domain, nil
	case rec.RekeyPending:
		return true, "需要换钥：" + rec.RekeyReason, nil
	case rec.NotAfter.Sub(now) < m.opts.RenewBefore:
		return true, fmt.Sprintf("将于 %s 到期", rec.NotAfter.UTC().Format(time.RFC3339)), nil
	}
	// 根密钥轮换后旧密文解不开：重签比带着一张无法下发的证书更可靠。
	if key := m.rootKey(); key != nil {
		if _, err := key.Open(rec.Key); err != nil {
			return true, "私钥无法用当前根密钥解密", nil
		}
	}
	return false, "", nil
}

// MarkRekey 标记下一次检查必须换钥重签（需求 4.6）。
//
// 没有证书时什么都不做：下一次签发本来就会生成新私钥。
func (m *Manager) MarkRekey(reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, err := m.load()
	if err != nil || rec == nil {
		return err
	}
	if rec.RekeyPending {
		return nil
	}
	rec.RekeyPending = true
	rec.RekeyReason = reason
	_, err = m.opts.Store.Update(author(), func(tx *ssot.Tx) (string, error) {
		if err := tx.WriteJSON(File, rec); err != nil {
			return "", err
		}
		return "本机证书标记换钥：" + reason, nil
	})
	return err
}

// RenewIfNeeded 在需要时签发新证书并写入 SSOT，返回是否签发。
func (m *Manager) RenewIfNeeded(ctx context.Context) (bool, error) {
	if m.opts.Issue == nil {
		return false, nil
	}
	key := m.rootKey()
	if key == nil {
		// 恢复模式：没有根密钥就无法加密私钥，等恢复完成再签。
		return false, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	rec, err := m.load()
	if err != nil {
		return false, err
	}
	need, reason, err := m.needsRenewal(rec)
	if err != nil || !need {
		return false, err
	}
	m.opts.Logf("本机证书需要签发（%s）：%s", m.opts.Domain, reason)

	// 签发涉及网络与 DNS 传播，不能在 SSOT 写锁内进行。
	issued, err := m.opts.Issue(ctx, m.opts.Domain)
	if err != nil {
		return false, err
	}
	leaf, err := ParseLeaf(issued.CertPEM)
	if err != nil {
		return false, fmt.Errorf("签发结果无法解析: %w", err)
	}
	if _, err := tls.X509KeyPair(issued.CertPEM, issued.KeyPEM); err != nil {
		return false, fmt.Errorf("签发结果的证书与私钥不匹配: %w", err)
	}
	if err := leaf.VerifyHostname(m.opts.Domain); err != nil {
		return false, fmt.Errorf("签发结果不覆盖 %s: %w", m.opts.Domain, err)
	}
	sealed, err := key.Seal(issued.KeyPEM)
	if err != nil {
		return false, err
	}
	next := &record{
		Domain:   m.opts.Domain,
		CertPEM:  string(issued.CertPEM),
		Key:      sealed,
		NotAfter: leaf.NotAfter.UTC(),
		IssuedAt: m.opts.Now().UTC(),
		Serial:   leaf.SerialNumber.Text(16),
	}
	if _, err := m.opts.Store.Update(author(), func(tx *ssot.Tx) (string, error) {
		if err := tx.WriteJSON(File, next); err != nil {
			return "", err
		}
		return fmt.Sprintf("签发本机证书 %s，到期 %s", m.opts.Domain, next.NotAfter.Format(time.RFC3339)), nil
	}); err != nil {
		return false, err
	}
	m.opts.Logf("本机证书已签发：%s，序列号 %s，到期 %s", m.opts.Domain, next.Serial, next.NotAfter.Format(time.RFC3339))
	if m.opts.OnIssued != nil {
		m.opts.OnIssued()
	}
	return true, nil
}

// Run 周期检查并续期，阻塞到 ctx 结束。
//
// 失败后按 RetryAfter 重试，成功或无需续期时按 CheckEvery 再查。
func (m *Manager) Run(ctx context.Context) {
	if m.opts.Issue == nil {
		return
	}
	for {
		wait := m.opts.CheckEvery
		if _, err := m.RenewIfNeeded(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			wait = m.opts.RetryAfter
			m.report(err)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

func (m *Manager) report(err error) {
	if errors.Is(err, ErrNotConfigured) {
		if !m.notConfiguredLogged {
			m.notConfiguredLogged = true
			m.opts.Logf("本机证书暂不签发：%v", err)
		}
		return
	}
	m.opts.Logf("本机证书签发失败：%v", err)
	if m.opts.OnFailure != nil {
		m.opts.OnFailure(err)
	}
}

// ParseLeaf 解析 PEM 证书链里的第一张证书。
func ParseLeaf(chainPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(chainPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("没有找到 PEM 证书")
	}
	return x509.ParseCertificate(block.Bytes)
}

func author() ssot.Author {
	return ssot.Author{Name: "cloud", Email: "cloud@dec.local", Source: "local-tls"}
}
