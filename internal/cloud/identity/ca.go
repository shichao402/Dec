// Package identity 实现设备身份、证书签发与吊销。
//
// 身份模型（需求 18）刻意做得很薄：没有用户名、没有口令、没有注册流程。
// 一台设备能访问云端，当且仅当它持有一张未过期、未被吊销的证书。
//
// 服务端自己是一套自签的 CA：启动时若没有身份密钥就生成一份，
// 私钥加密后放进 SSOT 仓库（随冷备一起备份），证书本身也放进去。
// 客户端信任的是你在配置里指定的域名加公共 CA 证书（需求 2.6），
// 所以服务端身份密钥只承担“签发设备证书”的职责，不承担“证明服务器是谁”。
package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// 证书有效期。
const (
	// DeviceCertTTL 是设备证书的有效期。
	// 选 90 天是因为它对一台长期在线的机器足够长，而续签又足够频繁，
	// 能在设备被吊销后最迟一个季度内自然过期兜底。
	DeviceCertTTL = 90 * 24 * time.Hour
	// RenewBefore 是允许续签的提前量。
	RenewBefore = 7 * 24 * time.Hour
	// ClockSkew 是校验时钟偏移的容忍度。
	ClockSkew = 2 * time.Minute
)

var (
	// ErrCertExpired 表示证书已过期。
	ErrCertExpired = errors.New("certificate expired")
	// ErrCertInvalid 表示证书无法解析或签名不对。
	ErrCertInvalid = errors.New("certificate invalid")
	// ErrCertRevoked 表示证书对应的设备已被吊销。
	ErrCertRevoked = errors.New("certificate revoked")
	// ErrKeyMismatch 表示私钥与证书不匹配。
	ErrKeyMismatch = errors.New("key mismatch")
)

// CA 是服务端的签发身份。
type CA struct {
	key  *ecdsa.PrivateKey
	cert *x509.Certificate
	// certPEM 是证书的 PEM 编码，随设备证书一起发给代理，便于排障。
	certPEM []byte
}

// NewCA 创建一套新的服务端身份。
//
// 主体名固定为 dec-cloud，避免把域名写进证书：域名只在部署时确定，
// 而重新签发设备证书不该依赖域名是否变更。
func NewCA() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成服务端身份密钥失败: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "dec-cloud", Organization: []string{"Dec"}},
		NotBefore:             now.Add(-ClockSkew),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("签发服务端身份证书失败: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{
		key:     key,
		cert:    cert,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}, nil
}

// LoadCA 用已有的私钥和证书重建签发身份。
func LoadCA(keyPEM, certPEM []byte) (*CA, error) {
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("%w: 服务端身份私钥无法解析", ErrCertInvalid)
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCertInvalid, err)
	}
	key, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: 服务端身份私钥类型不支持", ErrCertInvalid)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("%w: 服务端身份证书无法解析", ErrCertInvalid)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCertInvalid, err)
	}
	return &CA{key: key, cert: cert, certPEM: pem.EncodeToMemory(certBlock)}, nil
}

// KeyPEM 返回 PKCS#8 编码的私钥，供加密后存入 SSOT。
func (ca *CA) KeyPEM() ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(ca.key)
	if err != nil {
		return nil, fmt.Errorf("序列化服务端身份私钥失败: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// CertPEM 返回服务端身份证书。
func (ca *CA) CertPEM() []byte { return append([]byte(nil), ca.certPEM...) }

// Issued 是一张签发好的设备证书。
type Issued struct {
	CertPEM     []byte
	Serial      string
	Fingerprint string
	NotAfter    time.Time
}

// SignDevice 为设备公钥签发一张证书。
//
// deviceID 同时作为证书主体名和 SAN，这样服务端从 TLS 层就能直接拿到设备身份，
// 不必再查额外的映射表。
func (ca *CA) SignDevice(deviceID string, pub *ecdsa.PublicKey, notAfter time.Time) (*Issued, error) {
	if ca == nil || ca.key == nil {
		return nil, fmt.Errorf("%w: 服务端身份未加载", ErrCertInvalid)
	}
	if pub == nil {
		return nil, fmt.Errorf("%w: 设备公钥为空", ErrCertInvalid)
	}
	if deviceID == "" {
		return nil, fmt.Errorf("%w: 设备 ID 为空", ErrCertInvalid)
	}
	if notAfter.IsZero() {
		notAfter = time.Now().UTC().Add(DeviceCertTTL)
	}
	// X.509 的时间精度是秒，签发结果也截断到秒，避免调用方比对失败。
	notAfter = notAfter.UTC().Truncate(time.Second)
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: deviceID, Organization: []string{"Dec Device"}},
		// SAN 里放设备 ID，服务端从对端证书直接读身份。
		DNSNames:              []string{deviceID},
		NotBefore:             time.Now().UTC().Add(-ClockSkew),
		NotAfter:              notAfter.UTC(),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, pub, ca.key)
	if err != nil {
		return nil, fmt.Errorf("签发设备证书失败: %w", err)
	}
	fp, err := PublicKeyFingerprint(pub)
	if err != nil {
		return nil, err
	}
	return &Issued{
		CertPEM:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		Serial:      serial.Text(16),
		Fingerprint: fp,
		NotAfter:    notAfter.UTC(),
	}, nil
}

// VerifyDevice 校验一张设备证书链，返回设备 ID 与公钥指纹。
//
// 只做证书本身的校验（由调用方传入当前时间与吊销状态），
// 吊销判断交给上层，因为吊销表存在 SSOT 里。
func (ca *CA) VerifyDevice(certPEM []byte, now time.Time) (deviceID, serial string, notAfter time.Time, err error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", "", time.Time{}, fmt.Errorf("%w: 证书无法解析", ErrCertInvalid)
	}
	cert, perr := x509.ParseCertificate(block.Bytes)
	if perr != nil {
		return "", "", time.Time{}, fmt.Errorf("%w: %v", ErrCertInvalid, perr)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	if _, verr := cert.Verify(x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); verr != nil {
		var invalid x509.CertificateInvalidError
		if errors.As(verr, &invalid) && invalid.Reason == x509.Expired {
			return "", "", cert.NotAfter, fmt.Errorf("%w: %v", ErrCertExpired, verr)
		}
		return "", "", time.Time{}, fmt.Errorf("%w: %v", ErrCertInvalid, verr)
	}
	if len(cert.DNSNames) == 0 {
		return "", "", time.Time{}, fmt.Errorf("%w: 证书里没有设备标识", ErrCertInvalid)
	}
	return cert.DNSNames[0], cert.SerialNumber.Text(16), cert.NotAfter, nil
}

// ParsePublicKey 从 PEM 解析设备公钥。
func ParsePublicKey(pubPEM []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pubPEM)
	if block == nil {
		return nil, fmt.Errorf("%w: 公钥无法解析", ErrCertInvalid)
	}
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if ecdsaPub, ok := pub.(*ecdsa.PublicKey); ok {
			return ecdsaPub, nil
		}
		return nil, fmt.Errorf("%w: 只支持 ECDSA 公钥", ErrCertInvalid)
	}
	// 兼容 PKCS#1 风格的 EC 公钥。
	if ecdsaPub, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return &ecdsaPub.PublicKey, nil
	}
	return nil, fmt.Errorf("%w: 公钥格式不支持", ErrCertInvalid)
}

// PublicKeyFingerprint 返回公钥的 SHA-256 指纹（十六进制）。
func PublicKeyFingerprint(pub *ecdsa.PublicKey) (string, error) {
	if pub == nil {
		return "", fmt.Errorf("%w: 公钥为空", ErrCertInvalid)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("序列化公钥失败: %w", err)
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("生成证书序列号失败: %w", err)
	}
	return serial, nil
}
