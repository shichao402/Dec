package localcert

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
)

// DNS-01 签发（需求 4.4）。
//
// 选 DNS-01 是因为 local.dec.firoyang.com 解析到 127.0.0.1，CA 无法通过
// HTTP-01 或 TLS-ALPN-01 连回来校验；DNS-01 只看 TXT 记录，不要求服务可达。

// DNSProvider 负责发布与清理 ACME 校验用的 TXT 记录。
type DNSProvider interface {
	// Present 发布 fqdn 的 TXT 记录，返回清理函数。
	Present(ctx context.Context, fqdn, value string) (cleanup func(context.Context) error, err error)
}

// ACMEIssuer 用 ACME 协议与 DNS-01 校验签发证书。
type ACMEIssuer struct {
	// DirectoryURL 为空时使用 Let's Encrypt 生产环境。
	DirectoryURL string
	// AccountKey 是 ACME 账户密钥，需长期稳定以免触发账户频率限制。
	AccountKey crypto.Signer
	// Email 是账户联系邮箱，可空。
	Email string
	DNS   DNSProvider
	// PropagationWait 是发布 TXT 后等待 DNS 传播的时间。
	PropagationWait time.Duration
	Logf            func(format string, args ...any)
}

// Issue 签发 domain 的证书，每次生成新的 P-256 私钥。
func (a *ACMEIssuer) Issue(ctx context.Context, domain string) (*Issued, error) {
	if a.AccountKey == nil || a.DNS == nil {
		return nil, ErrNotConfigured
	}
	logf := a.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	dir := a.DirectoryURL
	if dir == "" {
		dir = acme.LetsEncryptURL
	}
	client := &acme.Client{Key: a.AccountKey, DirectoryURL: dir, UserAgent: "dec-cloud"}

	acct := &acme.Account{}
	if a.Email != "" {
		acct.Contact = []string{"mailto:" + a.Email}
	}
	if _, err := client.Register(ctx, acct, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("注册 ACME 账户失败: %w", err)
	}

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return nil, fmt.Errorf("创建订单失败: %w", err)
	}

	var cleanups []func(context.Context) error
	defer func() {
		// 清理不能用已取消的 ctx，否则 TXT 记录会遗留在 DNS 上。
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, c := range cleanups {
			if err := c(cctx); err != nil {
				logf("清理 ACME TXT 记录失败: %v", err)
			}
		}
	}()

	for _, u := range order.AuthzURLs {
		z, err := client.GetAuthorization(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("读取授权失败: %w", err)
		}
		if z.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range z.Challenges {
			if c.Type == "dns-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return nil, fmt.Errorf("CA 没有为 %s 提供 dns-01 校验", z.Identifier.Value)
		}
		value, err := client.DNS01ChallengeRecord(chal.Token)
		if err != nil {
			return nil, err
		}
		fqdn := "_acme-challenge." + strings.TrimPrefix(z.Identifier.Value, "*.")
		cleanup, err := a.DNS.Present(ctx, fqdn, value)
		if err != nil {
			return nil, fmt.Errorf("发布 TXT 记录失败: %w", err)
		}
		cleanups = append(cleanups, cleanup)
		if a.PropagationWait > 0 {
			t := time.NewTimer(a.PropagationWait)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			case <-t.C:
			}
		}
		if _, err := client.Accept(ctx, chal); err != nil {
			return nil, fmt.Errorf("提交校验失败: %w", err)
		}
		if _, err := client.WaitAuthorization(ctx, z.URI); err != nil {
			return nil, fmt.Errorf("校验未通过: %w", err)
		}
	}

	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return nil, fmt.Errorf("订单未就绪: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: domain},
		DNSNames: []string{domain},
	}, key)
	if err != nil {
		return nil, err
	}
	ders, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("获取证书失败: %w", err)
	}
	var chain []byte
	for _, d := range ders {
		chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: d})...)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &Issued{
		CertPEM: chain,
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}, nil
}
