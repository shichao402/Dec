package config

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/shichao402/Dec/internal/types"
)

var managedDevicesMu sync.Mutex

// NormalizeManagedDeviceAlias 规范化受管设备别名。
//
// 别名是设备清单与 device:<alias> 互斥键的人类入口，不是文件路径。
func NormalizeManagedDeviceAlias(alias string) (string, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", fmt.Errorf("设备别名不能为空")
	}
	if strings.ContainsAny(alias, "\r\n") {
		return "", fmt.Errorf("设备别名不能包含换行")
	}
	return alias, nil
}

// NormalizeSSHTarget 只保存系统 ssh 可直接接受的目标引用。
//
// 目标可以是 ~/.ssh/config 的 Host 别名、主机名或 user@host；私钥内容和口令
// 永远不进配置。自定义端口应放进 ssh_config，避免在多层配置中重复一套 SSH 语义。
func NormalizeSSHTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("SSH 目标不能为空")
	}
	if strings.HasPrefix(target, "-") || strings.ContainsAny(target, "\r\n\t ") {
		return "", fmt.Errorf("SSH 目标 %q 无效；请填写 ssh_config 别名、主机名或 user@host", target)
	}
	return target, nil
}

func normalizeDeviceTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func normalizeManagedDevice(item types.ManagedDevice) (types.ManagedDevice, error) {
	alias, err := NormalizeManagedDeviceAlias(item.Alias)
	if err != nil {
		return types.ManagedDevice{}, err
	}
	sshTarget, err := NormalizeSSHTarget(item.SSHTarget)
	if err != nil {
		return types.ManagedDevice{}, err
	}
	item.Alias = alias
	item.SSHTarget = sshTarget
	item.ManagementListen = strings.TrimSpace(item.ManagementListen)
	item.Tags = normalizeDeviceTags(item.Tags)
	item.ProvisionedVersion = strings.TrimSpace(item.ProvisionedVersion)
	return item, nil
}

func ListManagedDevices() ([]types.ManagedDevice, error) {
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	result := make([]types.ManagedDevice, 0, len(cfg.ManagedDevices))
	for _, raw := range cfg.ManagedDevices {
		item, normErr := normalizeManagedDevice(raw)
		if normErr != nil {
			continue
		}
		key := strings.ToLower(item.Alias)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Alias) < strings.ToLower(result[j].Alias)
	})
	return result, nil
}

// RegisterManagedDevice 按别名 upsert 设备登记。
func RegisterManagedDevice(item types.ManagedDevice) (types.ManagedDevice, error) {
	managedDevicesMu.Lock()
	defer managedDevicesMu.Unlock()

	normalized, err := normalizeManagedDevice(item)
	if err != nil {
		return types.ManagedDevice{}, err
	}
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return types.ManagedDevice{}, err
	}
	for i := range cfg.ManagedDevices {
		existing, normErr := normalizeManagedDevice(cfg.ManagedDevices[i])
		if normErr == nil && (strings.EqualFold(existing.Alias, normalized.Alias) ||
			strings.EqualFold(existing.SSHTarget, normalized.SSHTarget)) {
			if item.Tags == nil {
				normalized.Tags = existing.Tags
			}
			if strings.TrimSpace(item.ProvisionedVersion) == "" {
				normalized.ProvisionedVersion = existing.ProvisionedVersion
			}
			cfg.ManagedDevices[i] = normalized
			return normalized, SaveGlobalConfig(cfg)
		}
	}
	cfg.ManagedDevices = append(cfg.ManagedDevices, normalized)
	return normalized, SaveGlobalConfig(cfg)
}

// RemoveManagedDevice 只删除本机登记，不连接远端、不卸载程序、不删除 ~/.dec。
func RemoveManagedDevice(alias string) (bool, error) {
	managedDevicesMu.Lock()
	defer managedDevicesMu.Unlock()

	normalized, err := NormalizeManagedDeviceAlias(alias)
	if err != nil {
		return false, err
	}
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return false, err
	}
	out := cfg.ManagedDevices[:0]
	removed := false
	for _, item := range cfg.ManagedDevices {
		existing, normErr := NormalizeManagedDeviceAlias(item.Alias)
		if normErr == nil && strings.EqualFold(existing, normalized) {
			removed = true
			continue
		}
		out = append(out, item)
	}
	if !removed {
		return false, nil
	}
	cfg.ManagedDevices = out
	return true, SaveGlobalConfig(cfg)
}
