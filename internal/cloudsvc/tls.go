package cloudsvc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// TLS 与 CA 证书的管理。
//
// 两种来源：
//
//  1. 生产：ACME TLS-ALPN-01 自动申请与续期 dec.firoyang.com 的证书。
//     走 ALPN 挑战，不需要额外的 80 端口。公网 443 由 nginx 持有，按 SNI 原样
//     透传 TLS 流到本服务的回环高位端口，acme-tls/1 握手与设备证书都不经 nginx 解密。
//  2. 调试：AcceptAny 时用自签证书，只在 127.0.0.1 上使用。

// 服务端 CA（签发与校验设备证书）不在这里管理。
//
// 它的私钥只存在服务器本机 certs/ca.key 与 Bitwarden 两处，git 只记证书与指纹；
// 缺失时绝不静默重建，而是进入恢复模式等用户从 Bitwarden 取回（ADR 0052）。
// 判定与读写见 identity.ResolveCA，运行期由 Bootstrapper 持有。

// tlsConfig 准备服务端 TLS 配置。
//
// 客户端证书一律“请求但不强制校验”：无证书请求也要能进到协议层，
// 因为登记与恢复在拿到证书之前就要访问服务端（需求 16.3）。
// 真正的身份校验在 deviceFrom 里按方法做。
func (s *Server) tlsConfig() (*tls.Config, error) {
	if s.cfg.AcceptAny {
		cert, err := selfSignedCert(s.cfg.Domain)
		if err != nil {
			return nil, err
		}
		return &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{*cert},
			ClientAuth:   tls.RequestClientCert,
			NextProtos:   []string{"h2", "http/1.1"},
		}, nil
	}
	if s.cfg.Domain == "" {
		return nil, errors.New("生产模式必须配置域名")
	}

	manager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(acmeCacheDir(s.cfg.DataDir)),
		HostPolicy: autocert.HostWhitelist(s.cfg.Domain),
	}
	cfg := manager.TLSConfig()
	// manager.TLSConfig 已经设好 GetCertificate、NextProtos（含 acme-tls/1）与最小版本，
	// 这些是 TLS-ALPN-01 挑战能工作的前提，不能手工重建。
	// 这里只覆盖客户端证书策略，其余保持 autocert 的默认行为。
	cfg.ClientAuth = tls.RequestClientCert
	cfg.ClientCAs = nil
	return cfg, nil
}

// acmeCacheDir 是 ACME 账户与证书缓存目录。
func acmeCacheDir(dataDir string) string { return filepath.Join(dataDir, "acme") }

// selfSignedCert 生成本机调试用的自签证书。
func selfSignedCert(domain string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	if domain == "" {
		domain = "localhost"
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: domain},
		NotBefore:    time.Now().Add(-time.Hour).UTC().Truncate(time.Second),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour).UTC().Truncate(time.Second),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{domain, "localhost"},
		IPAddresses:  nil,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// pemEncodeCert 把 DER 编码的证书转成 PEM。
func pemEncodeCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
