package agent

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// 本机证书的落盘（agent-tray-access 需求 4.5）。
//
// 证书与私钥落在代理自己的状态目录 <state>/tls/ 下，不进入任何 IDE 或项目
// 落点，因此不走落点白名单。私钥文件权限 0600，目录 0700。
//
//	tls/cert.pem   证书链
//	tls/key.pem    私钥
//	tls/meta.json  域名、序列号、到期时间（便于不解析证书就判断状态）
//
// 写入顺序是先 key 后 cert 最后 meta：读取方以 meta 的序列号为准，
// 中途失败时 meta 仍指向旧证书，下一轮同步会重新下发。

const (
	tlsDirName  = "tls"
	tlsCertFile = "cert.pem"
	tlsKeyFile  = "key.pem"
	tlsMetaFile = "meta.json"
)

// LocalTLSMeta 是本机证书的元数据。
type LocalTLSMeta struct {
	Domain   string    `json:"domain"`
	Serial   string    `json:"serial"`
	NotAfter time.Time `json:"not_after"`
}

// LocalTLSStore 管理 <state>/tls/。
type LocalTLSStore struct {
	dir string
	mu  sync.Mutex
}

// NewLocalTLSStore 返回状态目录下的证书存储。
func NewLocalTLSStore(stateDir string) *LocalTLSStore {
	return &LocalTLSStore{dir: filepath.Join(stateDir, tlsDirName)}
}

// Dir 返回证书目录。
func (s *LocalTLSStore) Dir() string { return s.dir }

// Meta 返回当前证书元数据；没有证书时返回 (nil, nil)。
func (s *LocalTLSStore) Meta() (*LocalTLSMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metaLocked()
}

func (s *LocalTLSStore) metaLocked() (*LocalTLSMeta, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, tlsMetaFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var m LocalTLSMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("解析本机证书元数据失败: %w", err)
	}
	return &m, nil
}

// Serial 返回当前证书序列号，没有或读取失败时返回空串（云端会重新下发）。
func (s *LocalTLSStore) Serial() string {
	m, err := s.Meta()
	if err != nil || m == nil {
		return ""
	}
	return m.Serial
}

// Save 校验并落盘一份证书。证书与私钥不匹配时拒绝写入。
func (s *LocalTLSStore) Save(b *proto.LocalTLSBundle) error {
	if b == nil {
		return nil
	}
	if b.Serial == "" {
		return errors.New("本机证书缺少序列号")
	}
	if _, err := tls.X509KeyPair([]byte(b.CertPEM), []byte(b.KeyPEM)); err != nil {
		return fmt.Errorf("本机证书与私钥不匹配: %w", err)
	}
	meta, err := json.MarshalIndent(LocalTLSMeta{Domain: b.Domain, Serial: b.Serial, NotAfter: b.NotAfter}, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("创建证书目录失败: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(s.dir, tlsKeyFile), []byte(b.KeyPEM), 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(s.dir, tlsCertFile), []byte(b.CertPEM), 0o644); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.dir, tlsMetaFile), meta, 0o644)
}

// Load 读取证书，供 HTTPS 监听使用；没有证书时返回 (nil, nil, nil)。
func (s *LocalTLSStore) Load() (*tls.Certificate, *LocalTLSMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.metaLocked()
	if err != nil || meta == nil {
		return nil, nil, err
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(s.dir, tlsCertFile), filepath.Join(s.dir, tlsKeyFile))
	if err != nil {
		return nil, meta, fmt.Errorf("加载本机证书失败: %w", err)
	}
	return &cert, meta, nil
}

// Remove 删除本机证书（卸载时调用）。
func (s *LocalTLSStore) Remove() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.dir)
}
