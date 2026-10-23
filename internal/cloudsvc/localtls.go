package cloudsvc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/cloud/alert"
	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/localcert"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 本机证书 local.dec.firoyang.com 的接线（agent-tray-access 需求 4.4、4.6）。
//
// DNSPod 凭据是一条普通的 Dec 密钥（默认名 dnspod-token），每次签发时现取：
// 服务启动时根密钥可能还没加载，用户也可能随时更换凭据。凭据只在云端解密，
// 不进入任何下发内容。

// localACMEAccountFile 是本机证书专用 ACME 账户密钥的路径。
//
// 与服务自身证书（autocert 缓存）分开：两者的生命周期与失效影响不同，
// 混用会让一方的账户问题牵连另一方。
func localACMEAccountFile(dataDir string) string {
	return filepath.Join(certDir(dataDir), "local-acme-account.key")
}

// newLocalTLS 构造本机证书管理器。LocalTLSSecret 为空时返回关闭状态的管理器。
func (s *Server) newLocalTLS() *localcert.Manager {
	opts := localcert.Options{
		Store:   s.store,
		Domain:  s.cfg.LocalTLSDomain,
		RootKey: s.rootKeyOf,
		OnIssued: func() {
			// 新证书写入 SSOT 后唤醒长轮询，让在线设备立刻拿到（需求 4.5）。
			s.NotifyChanged()
		},
		OnFailure: func(err error) {
			s.alerts.Raise(alert.KindLocalTLS, "本机证书签发失败", err.Error())
		},
		Logf: s.logf,
	}
	if s.cfg.LocalTLSSecret != "" && !s.cfg.AcceptAny {
		opts.Issue = s.issueLocalCert
	}
	return localcert.New(opts)
}

// issueLocalCert 用 DNSPod 凭据与 ACME DNS-01 签发一张新证书。
func (s *Server) issueLocalCert(ctx context.Context, domain string) (*localcert.Issued, error) {
	key := s.rootKeyOf()
	if key == nil {
		return nil, localcert.ErrNotConfigured
	}
	raw, err := s.secrets.GetSecret(key, s.cfg.LocalTLSSecret)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, fmt.Errorf("%w：缺少密钥 %q（DNSPod API Token，形如 ID,Token）", localcert.ErrNotConfigured, s.cfg.LocalTLSSecret)
		}
		return nil, err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return nil, fmt.Errorf("%w：密钥 %q 为空", localcert.ErrNotConfigured, s.cfg.LocalTLSSecret)
	}
	account, err := loadOrCreateACMEKey(localACMEAccountFile(s.cfg.DataDir))
	if err != nil {
		return nil, err
	}
	dns, kind, err := localcert.DNSPodFor(token, s.cfg.LocalTLSZone)
	if err != nil {
		return nil, fmt.Errorf("%w：密钥 %q %v", localcert.ErrNotConfigured, s.cfg.LocalTLSSecret, err)
	}
	s.logf("本机证书签发使用 %s 凭据", kind)
	// 需求 4.3：local 子域必须指向 127.0.0.1。凭据只有云端能解密，部署脚本
	// 拿不到，所以放在这里：每次签发、续期都顺手校正一次，记录被误删或被
	// 泛解析覆盖时会自愈。失败不阻断签发——证书照样有用，只是解析需要人看。
	if changed, err := dns.EnsureA(ctx, domain, "127.0.0.1"); err != nil {
		s.logf("校正 %s 的 A 记录失败: %v", domain, err)
	} else if changed {
		s.logf("已把 %s 的 A 记录校正为 127.0.0.1", domain)
	}
	issuer := &localcert.ACMEIssuer{
		DirectoryURL:    s.cfg.ACMEDirectory,
		AccountKey:      account,
		Email:           s.cfg.ACMEEmail,
		DNS:             dns,
		PropagationWait: 30 * time.Second,
		Logf:            s.logf,
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return issuer.Issue(ctx, domain)
}

// LocalTLS 返回本机证书管理器，供下发链路读取当前证书。
func (s *Server) LocalTLS() *localcert.Manager { return s.localTLS }

// localTLSFor 返回需要下发给设备的本机证书（agent-tray-access 需求 4.5）。
//
// 设备已持有同一序列号时返回 nil；取不到证书（未签发、根密钥未加载、
// 解密失败）时也返回 nil：下发链路不能因为证书问题影响资产同步。
func (s *Server) localTLSFor(deviceSerial string) *proto.LocalTLSBundle {
	if s.localTLS == nil {
		return nil
	}
	cur, err := s.localTLS.Current()
	if err != nil {
		s.logf("读取本机证书失败，本轮不下发: %v", err)
		return nil
	}
	if cur == nil || cur.Serial == deviceSerial {
		return nil
	}
	return &proto.LocalTLSBundle{
		Domain:   cur.Domain,
		Serial:   cur.Serial,
		NotAfter: cur.NotAfter,
		CertPEM:  string(cur.CertPEM),
		KeyPEM:   string(cur.KeyPEM),
	}
}

// recordLocalTLS 记录一次本机证书下发。私钥分发是敏感事件，必须留痕。
func (s *Server) recordLocalTLS(dev ssot.Device, b *proto.LocalTLSBundle) {
	if b == nil || s.audit == nil {
		return
	}
	if err := s.audit.Append(audit.Entry{
		Event:    audit.EventSync,
		DeviceID: dev.ID,
		Source:   string(proto.SourceAgent),
		Subject:  "local-tls:" + b.Serial,
		Detail:   "下发本机证书 " + b.Domain + "，到期 " + b.NotAfter.UTC().Format(time.RFC3339),
		OK:       true,
	}); err != nil {
		s.logf("写审计失败: %v", err)
	}
}

// markLocalTLSRekey 在设备吊销后标记换钥（需求 4.6）。
//
// 失败只记日志：吊销本身已经生效，换钥最迟在下一次到期续期时发生。
func (s *Server) markLocalTLSRekey(deviceID string) {
	if s.localTLS == nil {
		return
	}
	if err := s.localTLS.MarkRekey("吊销设备 " + deviceID); err != nil {
		s.logf("标记本机证书换钥失败: %v", err)
		return
	}
	if s.localTLS.Enabled() {
		go func() {
			if _, err := s.localTLS.RenewIfNeeded(s.ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logf("吊销后换钥签发失败，将由后台重试: %v", err)
			}
		}()
	}
}

// loadOrCreateACMEKey 加载或生成 ACME 账户密钥（P-256，0600）。
func loadOrCreateACMEKey(path string) (crypto.Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("ACME 账户密钥 %s 格式错误", path)
		}
		k, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析 ACME 账户密钥失败: %w", err)
		}
		return k, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return k, nil
}
