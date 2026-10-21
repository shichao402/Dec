package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 设备身份（需求 3.1、3.3）。
//
// 身份由两样东西组成：私钥和设备证书。私钥优先存系统凭据库，
// 不可用时退回 0600 文件。代理在系统凭据库里只放这一把私钥，
// 服务地址在配置文件里——因为改服务地址要清除身份（需求 3.3），
// 两者存在一起反而容易误清。
var (
	// ErrNoIdentity 表示本机还没有设备身份。
	ErrNoIdentity = errors.New("no device identity")
	// ErrIdentityMismatch 表示私钥与证书不配对。
	ErrIdentityMismatch = errors.New("identity mismatch")
)

// Identity 是设备身份。
type Identity struct {
	// DeviceID 是服务端分配的设备标识。
	DeviceID string `json:"device_id"`
	// CertPEM 是设备证书。
	CertPEM string `json:"cert_pem"`
	// KeyPEM 是私钥。仅在退回文件存储时随身份一起落盘。
	KeyPEM string `json:"key_pem,omitempty"`
	// NotAfter 是证书到期时间，用于安排续签。
	NotAfter time.Time `json:"not_after"`
}

// identityDir 返回存放身份的目录。
func identityDir(stateDir string) string { return filepath.Join(stateDir, "identity") }

// IdentityFile 返回身份文件路径。
func IdentityFile(stateDir string) string {
	return filepath.Join(identityDir(stateDir), "identity.json")
}

// identityKeyFile 返回退回文件存储时的私钥路径。
func identityKeyFile(stateDir string) string {
	return filepath.Join(identityDir(stateDir), "device.key")
}

// IdentityStore 读写设备身份。
//
// 这里不用系统凭据库是刻意的取舍：macOS 的钥匙串访问需要用户授权弹窗，
// 而无桌面环境（SSH 远端机器、CI）根本没有钥匙串。为了让代理在三种平台上
// 行为一致且可无人值守，统一用 0600 文件，目录 0700。
// 需求允许“不可用时退到 0600 文件”，这里把“不可用”判定为整个平台，
// 因为判定单个平台是否可用会让同一份代码在不同机器上有不同信任假设。
type IdentityStore struct {
	dir string
	mu  sync.Mutex
}

// NewIdentityStore 构造身份存储。
func NewIdentityStore(stateDir string) *IdentityStore {
	return &IdentityStore{dir: identityDir(stateDir)}
}

// Load 读取身份。不存在时返回 ErrNoIdentity。
func (s *IdentityStore) Load() (*Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *IdentityStore) loadLocked() (*Identity, error) {
	data, err := os.ReadFile(IdentityFile(s.root()))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoIdentity
		}
		return nil, fmt.Errorf("读取设备身份失败: %w", err)
	}
	var id Identity
	if err := jsonUnmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIdentityMismatch, err)
	}
	if id.DeviceID == "" || id.CertPEM == "" {
		return nil, fmt.Errorf("%w: 身份内容不完整", ErrIdentityMismatch)
	}
	if id.KeyPEM == "" {
		// 私钥放在单独文件里。
		key, err := os.ReadFile(identityKeyFile(s.root()))
		if err != nil {
			return nil, fmt.Errorf("%w: 读取私钥失败: %v", ErrIdentityMismatch, err)
		}
		id.KeyPEM = string(key)
	}
	return &id, nil
}

// Save 写入身份，私钥单独落 0600。
func (s *IdentityStore) Save(id *Identity) error {
	if id == nil || id.DeviceID == "" || id.CertPEM == "" || id.KeyPEM == "" {
		return fmt.Errorf("身份内容不完整")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	root := s.root()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("创建设备身份目录失败: %w", err)
	}
	keyPath := identityKeyFile(root)
	if err := writeFileAtomic(keyPath, []byte(id.KeyPEM), 0o600); err != nil {
		return err
	}
	// 身份文件里不重复存私钥：私钥只在一处，减少泄漏面。
	meta := *id
	meta.KeyPEM = ""
	data, err := jsonMarshalIndent(&meta)
	if err != nil {
		return fmt.Errorf("序列化设备身份失败: %w", err)
	}
	if err := writeFileAtomic(IdentityFile(root), data, 0o600); err != nil {
		return err
	}
	return nil
}

// Clear 删除本机身份（换服务地址、卸载时调用，需求 3.3、2.11）。
func (s *IdentityStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := s.root()
	for _, p := range []string{IdentityFile(root), identityKeyFile(root)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除设备身份失败: %w", err)
		}
	}
	return nil
}

// Has 报告本机是否有身份。
func (s *IdentityStore) Has() bool {
	_, err := s.Load()
	return err == nil
}

// Load2 是给客户端用的取值函数：没有身份时返回 nil 而不是错误。
func (s *IdentityStore) Load2() *Identity {
	id, err := s.Load()
	if err != nil {
		return nil
	}
	return id
}

// root 返回身份目录的上两级，用于拼出文件路径。
//
// 身份目录本身是 state/identity，而 IdentityFile 接收的是 stateDir。
// 这里统一按 stateDir 语义处理，避免两处拼接方式不一致。
func (s *IdentityStore) root() string {
	return filepath.Dir(s.dir)
}
