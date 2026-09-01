package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/shichao402/Dec/internal/types"
)

// ManagementListenResult 描述一次 management_listen 幂等写入的结果。
type ManagementListenResult struct {
	// Path 是被写入（或已符合预期而未写入）的配置文件路径。
	Path string
	// Addr 是写入后生效的监听地址。
	Addr string
	// Previous 是写入前的值，空表示此前未配置。
	Previous string
	// Changed 为 false 表示已是目标值，本次未落盘。
	Changed bool
}

// EnsureManagementListen 幂等地把 management_listen 写成 addr。
//
// 走 LoadGlobalConfig / SaveGlobalConfig 而非手写 YAML：kind/version/layout_version
// 的补全与旧配置合并只有那一份实现，绕过它必然在 schema 升级后漂移（ADR 0017、0019）。
//
// 校验规则与 servicehost.loadListenSettings 对齐：非 loopback 地址必须已配置
// 管理 TLS 证书对，否则服务端启动时才会失败——那时人已经在等连接了。
func EnsureManagementListen(addr string) (*ManagementListenResult, error) {
	normalized, err := NormalizeManagementListen(addr)
	if err != nil {
		return nil, err
	}

	path, err := GetGlobalConfigPath()
	if err != nil {
		return nil, err
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &types.GlobalConfig{}
	}

	previous := strings.TrimSpace(cfg.ManagementListen)
	result := &ManagementListenResult{Path: path, Addr: normalized, Previous: previous}
	if previous == normalized {
		return result, nil
	}

	host, _, err := splitManagementHostPort(normalized)
	if err != nil {
		return nil, err
	}
	if !isLoopbackManagementHost(host) {
		cert := strings.TrimSpace(cfg.ManagementTLSCert)
		key := strings.TrimSpace(cfg.ManagementTLSKey)
		if cert == "" || key == "" {
			return nil, fmt.Errorf("非 loopback 的 management_listen %q 必须先配置 management_tls_cert 与 management_tls_key", normalized)
		}
	}

	cfg.ManagementListen = normalized
	if err := SaveGlobalConfig(cfg); err != nil {
		return nil, err
	}
	result.Changed = true
	return result, nil
}

// NormalizeManagementListen 校验并规范化监听地址。
//
// 只做等价规范化（去空白），不替换 host：把 localhost 静默改写成 127.0.0.1
// 会让人在配置文件里看到自己没写过的值。
func NormalizeManagementListen(addr string) (string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", fmt.Errorf("management_listen 不能为空")
	}
	host, port, err := splitManagementHostPort(trimmed)
	if err != nil {
		return "", fmt.Errorf("management_listen %q 无效: %w", addr, err)
	}
	if strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("management_listen %q 缺少主机部分", addr)
	}
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", fmt.Errorf("management_listen %q 的端口无效", addr)
		}
	}
	return trimmed, nil
}

// splitManagementHostPort 与 servicehost 的同名逻辑保持一致：允许无端口形式。
func splitManagementHostPort(addr string) (host, port string, err error) {
	if !strings.Contains(addr, ":") {
		return addr, "", nil
	}
	return net.SplitHostPort(addr)
}

func isLoopbackManagementHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
