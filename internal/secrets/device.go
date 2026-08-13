package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeviceFile 持久化设备信任信息（非 access session）。
// identifier 用于 Bitwarden deviceIdentifier；two_factor_remember 为「记住此设备」令牌。
type DeviceFile struct {
	Identifier        string            `json:"identifier"`
	TwoFactorRemember map[string]string `json:"two_factor_remember,omitempty"`
}

// DevicePath 返回 ~/.dec/secrets/device.json 路径。
func DevicePath() (string, error) {
	dir, err := secretsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "device.json"), nil
}

// LoadDeviceFile 读取设备信任文件；不存在时返回空结构。
func LoadDeviceFile() (*DeviceFile, error) {
	path, err := DevicePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DeviceFile{}, nil
		}
		return nil, fmt.Errorf("读取 device.json 失败: %w", err)
	}
	var f DeviceFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("解析 device.json 失败: %w", err)
	}
	if f.TwoFactorRemember == nil {
		f.TwoFactorRemember = map[string]string{}
	}
	return &f, nil
}

// SaveDeviceFile 写入设备信任文件。
func SaveDeviceFile(f *DeviceFile) error {
	if f == nil {
		return fmt.Errorf("device file 不能为空")
	}
	if f.TwoFactorRemember == nil {
		f.TwoFactorRemember = map[string]string{}
	}
	path, err := DevicePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建 secrets 目录失败: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("写入 device.json 失败: %w", err)
	}
	return nil
}

// EnsureDeviceIdentifier 返回持久化 deviceIdentifier，不存在时生成并落盘。
func EnsureDeviceIdentifier() (string, error) {
	f, err := LoadDeviceFile()
	if err != nil {
		return "", err
	}
	if id := strings.TrimSpace(f.Identifier); id != "" {
		return id, nil
	}
	f.Identifier = newDeviceID()
	if err := SaveDeviceFile(f); err != nil {
		return "", err
	}
	return f.Identifier, nil
}

// RememberToken 读取指定邮箱的 2FA 记住设备令牌。
func RememberToken(email string) (string, error) {
	f, err := LoadDeviceFile()
	if err != nil {
		return "", err
	}
	return f.TwoFactorRemember[strings.TrimSpace(email)], nil
}

// SetRememberToken 保存 2FA 记住设备令牌。
func SetRememberToken(email, token string) error {
	email = strings.TrimSpace(email)
	token = strings.TrimSpace(token)
	if email == "" || token == "" {
		return fmt.Errorf("email 与 remember token 不能为空")
	}
	f, err := LoadDeviceFile()
	if err != nil {
		return err
	}
	if f.TwoFactorRemember == nil {
		f.TwoFactorRemember = map[string]string{}
	}
	f.TwoFactorRemember[email] = token
	return SaveDeviceFile(f)
}

// KnownEmail 返回用于 web unlock 预填的邮箱：优先 config，其次 device.json 中记住设备的邮箱。
func KnownEmail() string {
	cfg, err := LoadConfig()
	if err == nil && cfg != nil {
		if email := strings.TrimSpace(cfg.Email); email != "" && !isPlaceholderEmail(email) {
			return email
		}
	}
	dev, err := LoadDeviceFile()
	if err == nil && dev != nil {
		for email := range dev.TwoFactorRemember {
			if e := strings.TrimSpace(email); e != "" {
				return e
			}
		}
	}
	if cfg != nil {
		return strings.TrimSpace(cfg.Email)
	}
	return ""
}

func isPlaceholderEmail(email string) bool {
	switch strings.ToLower(strings.TrimSpace(email)) {
	case "", "user@example.com", "you@example.com":
		return true
	default:
		return false
	}
}

// ClearRememberToken 清除指定邮箱的记住设备令牌（令牌失效时）。
func ClearRememberToken(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	f, err := LoadDeviceFile()
	if err != nil {
		return err
	}
	if len(f.TwoFactorRemember) == 0 {
		return nil
	}
	delete(f.TwoFactorRemember, email)
	return SaveDeviceFile(f)
}

func newDeviceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
