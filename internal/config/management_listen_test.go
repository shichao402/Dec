package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureManagementListenWritesAndIsIdempotent(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	first, err := EnsureManagementListen("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if !first.Changed {
		t.Fatalf("首次写入应标记 Changed")
	}
	if first.Previous != "" {
		t.Fatalf("首次写入前不应有旧值: %q", first.Previous)
	}
	if first.Addr != "127.0.0.1:47653" {
		t.Fatalf("监听地址不符: %q", first.Addr)
	}

	data, err := os.ReadFile(filepath.Join(decHome, "config.yaml"))
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !strings.Contains(string(data), "management_listen: 127.0.0.1:47653") {
		t.Fatalf("配置未落盘:\n%s", data)
	}

	second, err := EnsureManagementListen("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("重复写入失败: %v", err)
	}
	if second.Changed {
		t.Fatalf("已是目标值时不应再落盘")
	}
}

// 幂等写入必须保留其余字段：远端可能已配了 repo_url / enabled_projects，
// 置备把它们冲掉等于毁掉那台机器的配置。
func TestEnsureManagementListenPreservesOtherFields(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	configPath := filepath.Join(decHome, "config.yaml")
	original := "kind: global\nversion: 1\nrepo_url: https://example.com/vault.git\nides:\n  - cursor\nenabled_projects:\n  - alpha\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	if _, err := EnsureManagementListen("127.0.0.1:47653"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}
	if cfg.RepoURL != "https://example.com/vault.git" {
		t.Fatalf("repo_url 被冲掉: %q", cfg.RepoURL)
	}
	if len(cfg.IDEs) != 1 || cfg.IDEs[0] != "cursor" {
		t.Fatalf("ides 被冲掉: %v", cfg.IDEs)
	}
	if len(cfg.EnabledProjects) != 1 || cfg.EnabledProjects[0] != "alpha" {
		t.Fatalf("enabled_projects 被冲掉: %v", cfg.EnabledProjects)
	}
	if cfg.ManagementListen != "127.0.0.1:47653" {
		t.Fatalf("management_listen 未写入: %q", cfg.ManagementListen)
	}
}

func TestEnsureManagementListenReportsPrevious(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	if err := os.WriteFile(filepath.Join(decHome, "config.yaml"),
		[]byte("kind: global\nversion: 1\nmanagement_listen: 127.0.0.1:12345\n"), 0o644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	result, err := EnsureManagementListen("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if !result.Changed {
		t.Fatalf("端口变化时应落盘")
	}
	if result.Previous != "127.0.0.1:12345" {
		t.Fatalf("旧值未上报: %q", result.Previous)
	}
}

// 非 loopback 地址在没有 TLS 证书对时必须当场拒绝，而不是等 dec-server
// 启动失败——那时人已经在等连接了（与 servicehost.loadListenSettings 对齐）。
func TestEnsureManagementListenRejectsNonLoopbackWithoutTLS(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	if _, err := EnsureManagementListen("0.0.0.0:8443"); err == nil {
		t.Fatalf("非 loopback 且无 TLS 应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(decHome, "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("被拒绝时不应落盘")
	}
}

func TestEnsureManagementListenAllowsNonLoopbackWithTLS(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	if err := os.WriteFile(filepath.Join(decHome, "config.yaml"),
		[]byte("kind: global\nversion: 1\nmanagement_tls_cert: /tmp/c.pem\nmanagement_tls_key: /tmp/k.pem\n"), 0o644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	if _, err := EnsureManagementListen("0.0.0.0:8443"); err != nil {
		t.Fatalf("已配 TLS 时应允许: %v", err)
	}
}

func TestNormalizeManagementListenRejectsInvalid(t *testing.T) {
	cases := []string{"", "   ", "127.0.0.1:0", "127.0.0.1:70000", "127.0.0.1:abc", ":47653"}
	for _, raw := range cases {
		if _, err := NormalizeManagementListen(raw); err == nil {
			t.Fatalf("%q 应被拒绝", raw)
		}
	}
}

func TestNormalizeManagementListenKeepsHostVerbatim(t *testing.T) {
	got, err := NormalizeManagementListen("  localhost:47653  ")
	if err != nil {
		t.Fatalf("localhost 应被接受: %v", err)
	}
	if got != "localhost:47653" {
		t.Fatalf("不应改写 host: %q", got)
	}
}
