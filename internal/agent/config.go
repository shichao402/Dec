package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// 代理的本机配置（需求 2.9）。
//
// 代理只保存两样状态：服务地址和设备身份。配置里刻意不放别的东西——
// 放进去的每一项都会变成“换机器要重新配”的负担，而代理的设计目标是
// 下载即用、只需一个地址。

// Config 是代理配置。
type Config struct {
	// ServerURL 是服务地址。
	ServerURL string `json:"server_url"`
	// DeviceName 是上报给服务端的可读名字。
	DeviceName string `json:"device_name,omitempty"`
	// UpdatedAt 是最后修改时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// ConfigFile 返回配置文件路径。
func ConfigFile(stateDir string) string { return filepath.Join(stateDir, "config.json") }

// DefaultStateDir 返回默认状态目录。
//
// macOS 与 Linux 用 ~/.dec-agent，Windows 用 %LOCALAPPDATA%\dec-agent：
// 前者是个人工具的习惯位置，后者避开 %APPDATA% 的漫游语义
// （设备身份不该跟着漫游配置文件跑到别的机器上）。
func DefaultStateDir() (string, error) {
	if dir := os.Getenv("DEC_AGENT_STATE"); dir != "" {
		return dir, nil
	}
	return PlatformStateDir()
}

// PlatformStateDir 返回平台默认状态目录，忽略 DEC_AGENT_STATE。
//
// 用来判断「这是不是正在服役的那个实例」：宿主配置（IDE 的 MCP 条目）
// 是全局的，只有一个实例能拥有它，所以需要区分默认实例与隔离实例。
func PlatformStateDir() (string, error) {
	if os.Getenv("LOCALAPPDATA") != "" && os.Getenv("USERPROFILE") != "" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "dec-agent"), nil
	}
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dec-agent"), nil
}

// IsPrimaryStateDir 报告 dir 是否是本机的默认服役状态目录。
//
// 隔离实例（自定义 DEC_AGENT_STATE、临时调试目录）返回 false，
// 调用方据此避免改写全局宿主配置。
func IsPrimaryStateDir(dir string) bool {
	def, err := PlatformStateDir()
	if err != nil || def == "" {
		return false
	}
	absDir, err1 := filepath.Abs(dir)
	absDef, err2 := filepath.Abs(def)
	if err1 != nil || err2 != nil {
		return false
	}
	return absDir == absDef
}

// LoadConfig 读取配置。不存在时返回空配置。
func LoadConfig(stateDir string) (*Config, error) {
	data, err := readFileOrEmpty(ConfigFile(stateDir))
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	if len(data) == 0 {
		return &Config{}, nil
	}
	var c Config
	if err := jsonUnmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	return &c, nil
}

// SaveConfig 写入配置。
func SaveConfig(stateDir string, c *Config) error {
	if c == nil {
		return errors.New("配置为空")
	}
	if c.ServerURL == "" {
		return ErrNotConfigured
	}
	base, err := normalizeBaseURL(c.ServerURL)
	if err != nil {
		return err
	}
	c.ServerURL = base
	c.UpdatedAt = time.Now().UTC()
	data, err := jsonMarshalIndent(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}
	return writeFileAtomic(ConfigFile(stateDir), data, 0o600)
}
