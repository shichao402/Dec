package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// 设备密钥对的生成、证书加载与 TLS 配置（需求 3.1、3.3）。

// GenerateKeyPair 生成 P-256 设备密钥对并返回 PEM 编码的私钥与公钥。
//
// 用 ECDSA P-256 是因为服务端 CA 用它签设备证书：密钥类型必须与服务端一致，
// 否则证书无法用于 TLS 握手。
func GenerateKeyPair() (privPEM, pubPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("生成设备密钥失败: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("编码私钥失败: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("编码公钥失败: %w", err)
	}
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	return privPEM, pubPEM, nil
}

// PublicKeyFromPrivate 从 PEM 私钥导出 PEM 公钥。
//
// 用于"带着原设备密钥重新登记"：服务端 CA 重建后，同一把设备公钥再登记一次，
// 服务端会沿用原设备 ID，订阅与项目登记原样保留（ADR 0052）。
func PublicKeyFromPrivate(privPEM string) (string, error) {
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		return "", fmt.Errorf("设备私钥无法解析")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("设备私钥无法解析: %w", err)
	}
	key, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("设备私钥类型不支持")
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", fmt.Errorf("编码公钥失败: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})), nil
}

// TLSCertificate 把身份转成 TLS 客户端证书。
func (id *Identity) TLSCertificate() (tls.Certificate, error) {
	if id == nil || id.CertPEM == "" || id.KeyPEM == "" {
		return tls.Certificate{}, ErrNoIdentity
	}
	cert, err := tls.X509KeyPair([]byte(id.CertPEM), []byte(id.KeyPEM))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: 证书与私钥不配对: %v", ErrIdentityMismatch, err)
	}
	return cert, nil
}

// Leaf 解析身份里的叶子证书，用于读取到期时间与 SAN。
func (id *Identity) Leaf() (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(id.CertPEM))
	if block == nil {
		return nil, fmt.Errorf("%w: 证书 PEM 无法解析", ErrIdentityMismatch)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIdentityMismatch, err)
	}
	return cert, nil
}

// CAFromEnroll 把登记响应里的 CA 证书存下来，供排障使用。
//
// 这里刻意不把它当作信任根：TLS 校验始终用系统根证书（需求 3.3 的“严格做 TLS 校验”）。
// 信任自己记录的 CA 就等于 TOFU，会让中间人有机会在首次连接时冒充服务端。
func CAFromEnroll(caPEM string) []byte {
	if caPEM == "" {
		return nil
	}
	return []byte(caPEM)
}

// 文件读写小工具。

// writeFileAtomic 原子写入文件，权限按 mode。
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("设置权限失败: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭文件失败: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("替换文件失败: %w", err)
	}
	return nil
}

// readFileOrEmpty 读取文件，不存在时返回 nil。
func readFileOrEmpty(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

func jsonMarshalIndent(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
