package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
)

// cleanupFixture 描述一次测试里铺好的本机落点，供两个档位的断言复用。
type cleanupFixture struct {
	home    string
	decHome string
	project string
	mcpPath string
	sshConf string
	gitConf string
	markers map[string]string
}

func setupCleanupFixture(t *testing.T) cleanupFixture {
	t.Helper()
	home := t.TempDir()
	decHome := filepath.Join(home, ".dec")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DEC_HOME", decHome)

	project := filepath.Join(home, "work")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := config.RegisterManagedProject(project, "work"); err != nil {
		t.Fatal(err)
	}
	mustWriteCleanupFile(t, filepath.Join(decHome, "bin", "dec-server"), "runtime")
	mustWriteCleanupFile(t, filepath.Join(decHome, "cache", "asset"), "cache")
	mustWriteCleanupFile(t, filepath.Join(decHome, "repo.git", "HEAD"), "ref: refs/heads/main\n")
	mustWriteCleanupFile(t, filepath.Join(decHome, "pruned", "old", "asset.md"), "pruned\n")
	mustWriteCleanupFile(t, filepath.Join(decHome, "secrets", "device.json"), `{"device_identifier":"keep"}`)
	mustWriteCleanupFile(t, filepath.Join(decHome, "secrets", "p", ".env", "global.env"), "GLOBAL=1\n")
	mustWriteCleanupFile(t, filepath.Join(project, ".dec", "state", "snapshot.json"), "{}\n")
	mustWriteCleanupFile(t, filepath.Join(project, ".dec", "cache", "index.json"), "{}\n")
	mustWriteCleanupFile(t, filepath.Join(project, ".secrets", "p", ".env", "x.env"), "X=1\n")
	mustWriteCleanupFile(t, filepath.Join(project, filepath.FromSlash(".secrets/dec/integration/bitwarden.yaml")), "email: keep\n")
	mustWriteCleanupFile(t, filepath.Join(project, filepath.FromSlash(".secrets/.integration/dec-home/secrets/device.json")), `{"keep":true}`)
	mustWriteCleanupFile(t, filepath.Join(home, ".cursor", "skills", "dec-owned", "SKILL.md"), "# owned\n")
	mustWriteCleanupFile(t, filepath.Join(home, ".cursor", "skills", "user-owned", "SKILL.md"), "# user\n")
	mustWriteCleanupFile(t, filepath.Join(home, ".ssh", "dec_ed25519"), "key\n")

	mcpPath := filepath.Join(home, ".cursor", "mcp.json")
	mcp := map[string]any{
		"mcpServers": map[string]any{
			"dec":  map[string]any{"command": "dec-mcp"},
			"user": map[string]any{"command": "user-mcp"},
		},
		"other": true,
	}
	data, _ := json.Marshal(mcp)
	mustWriteCleanupFile(t, mcpPath, string(data))

	sshConf := filepath.Join(home, ".ssh", "config")
	mustWriteCleanupFile(t, sshConf, "Include config.d/dec.conf\n\nHost personal\n  User me\n")
	mustWriteCleanupFile(t, filepath.Join(home, ".ssh", "config.d", "dec.conf"), "Host managed\n")

	gitConf := filepath.Join(home, ".gitconfig")
	mustWriteCleanupFile(t, gitConf, "[user]\n\tname = me\n")

	return cleanupFixture{
		home: home, decHome: decHome, project: project,
		mcpPath: mcpPath, sshConf: sshConf, gitConf: gitConf,
	}
}

func TestCleanupLocalInstallationPreservesRuntimeAndSharedConfig(t *testing.T) {
	f := setupCleanupFixture(t)

	preview, err := PreviewLocalCleanup(CleanupTierFull)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) == 0 {
		t.Fatal("预览应发现清理项")
	}

	result, err := CleanupLocalInstallation(context.Background(), LocalCleanupInput{Confirmed: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != CleanupTierFull {
		t.Fatalf("缺省档位应为 full: %s", result.Tier)
	}
	if len(result.Deleted) == 0 || len(result.Modified) == 0 {
		t.Fatalf("清理结果不完整: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(f.decHome, "bin", "dec-server")); err != nil {
		t.Fatalf("运行时应保留: %v", err)
	}
	for _, path := range []string{
		filepath.Join(f.decHome, "cache", "asset"),
		filepath.Join(f.decHome, "config.yaml"),
		filepath.Join(f.decHome, "repo.git"),
		filepath.Join(f.decHome, "secrets"),
		filepath.Join(f.project, ".dec", "state", "snapshot.json"),
		filepath.Join(f.project, ".secrets", "p"),
		filepath.Join(f.home, ".cursor", "skills", "dec-owned"),
		filepath.Join(f.home, ".ssh", "config.d", "dec.conf"),
		filepath.Join(f.home, ".ssh", "dec_ed25519"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("彻底清理应删除 %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.home, ".cursor", "skills", "user-owned", "SKILL.md")); err != nil {
		t.Fatalf("用户 Skill 应保留: %v", err)
	}
	for _, path := range []string{
		filepath.Join(f.project, filepath.FromSlash(".secrets/dec/integration/bitwarden.yaml")),
		filepath.Join(f.project, filepath.FromSlash(".secrets/.integration/dec-home/secrets/device.json")),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("受保护文件应保留 %s: %v", path, err)
		}
	}
	mcpData, err := os.ReadFile(f.mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mcpData), `"dec"`) || !strings.Contains(string(mcpData), `"user"`) ||
		!strings.Contains(string(mcpData), `"other"`) {
		t.Fatalf("共享 MCP 配置清理错误: %s", mcpData)
	}
	sshData, err := os.ReadFile(f.sshConf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sshData), "dec.conf") || !strings.Contains(string(sshData), "Host personal") {
		t.Fatalf("SSH 主配置清理错误: %s", sshData)
	}
}

func TestCleanupLocalInstallationRequiresConfirmation(t *testing.T) {
	setupCleanupFixture(t)
	if _, err := CleanupLocalInstallation(context.Background(), LocalCleanupInput{}, nil); err == nil {
		t.Fatal("未确认不应执行清理")
	}
}

// TestLightCleanupKeepsConfigAndCredentials 是核心约束：轻量清理不触碰配置与凭据面。
func TestLightCleanupKeepsConfigAndCredentials(t *testing.T) {
	f := setupCleanupFixture(t)

	result, err := CleanupLocalInstallation(context.Background(), LocalCleanupInput{
		Confirmed: true, Tier: string(CleanupTierLight),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != CleanupTierLight {
		t.Fatalf("结果应携带档位: %s", result.Tier)
	}
	for _, path := range []string{
		filepath.Join(f.decHome, "config.yaml"),
		filepath.Join(f.decHome, "secrets"),
		filepath.Join(f.decHome, "pruned"),
		filepath.Join(f.decHome, "secrets", "device.json"),
		filepath.Join(f.project, ".dec", "state", "snapshot.json"),
		filepath.Join(f.project, ".secrets", "p", ".env", "x.env"),
		filepath.Join(f.home, ".ssh", "dec_ed25519"),
		filepath.Join(f.home, ".ssh", "config.d", "dec.conf"),
		filepath.Join(f.gitConf),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("轻量清理应保留 %s: %v", path, err)
		}
	}
	sshData, err := os.ReadFile(f.sshConf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sshData), "dec.conf") {
		t.Fatalf("轻量清理不应移除 SSH Include: %s", sshData)
	}
	if len(result.Revoked) != 0 {
		t.Fatalf("轻量清理不应撤销凭据: %v", result.Revoked)
	}
	if result.NextAction == "" || len(result.Preserved) == 0 {
		t.Fatalf("轻量清理结果应说明保留面与下一步: %#v", result)
	}
}

// TestLightCleanupRemovesPullProducts 断言轻量清理确实清掉了可重新拉取的产物。
func TestLightCleanupRemovesPullProducts(t *testing.T) {
	f := setupCleanupFixture(t)

	if _, err := CleanupLocalInstallation(context.Background(), LocalCleanupInput{
		Confirmed: true, Tier: string(CleanupTierLight),
	}, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(f.home, ".cursor", "skills", "dec-owned"),
		filepath.Join(f.decHome, "cache"),
		filepath.Join(f.decHome, "repo.git"),
		filepath.Join(f.project, ".dec", "cache", "index.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("轻量清理应删除 %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.decHome, "bin", "dec-server")); err != nil {
		t.Fatalf("运行时应保留: %v", err)
	}
	mcpData, err := os.ReadFile(f.mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mcpData), `"dec"`) || !strings.Contains(string(mcpData), `"user"`) {
		t.Fatalf("应只移除 MCP 中的 Dec 条目: %s", mcpData)
	}
}

// TestPreviewReflectsTier 断言预览在同一组落点上按档位给出不同的删除面与保留面。
func TestPreviewReflectsTier(t *testing.T) {
	setupCleanupFixture(t)

	light, err := PreviewLocalCleanup(CleanupTierLight)
	if err != nil {
		t.Fatal(err)
	}
	full, err := PreviewLocalCleanup(CleanupTierFull)
	if err != nil {
		t.Fatal(err)
	}
	if light.Tier != CleanupTierLight || full.Tier != CleanupTierFull {
		t.Fatalf("预览应携带档位: %s / %s", light.Tier, full.Tier)
	}
	if len(light.Items) >= len(full.Items) {
		t.Fatalf("轻量清理的删除面应小于彻底清理: light %d full %d", len(light.Items), len(full.Items))
	}
	if len(light.KeptByTier) == 0 {
		t.Fatal("轻量清理预览应列出改选彻底清理才会删的条目")
	}
	for _, item := range light.Items {
		if isKeptByLight(item.Category, item.Path) {
			t.Fatalf("轻量清理预览不应包含保留类落点: %#v", item)
		}
	}
	// 预览必须只读。
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".cursor", "skills", "dec-owned")); err != nil {
		t.Fatalf("预览不应删除文件: %v", err)
	}
}

// TestPreviewReturnsExplicitConclusionWhenNothingToClean 覆盖空结果时的明确结论。
func TestPreviewReturnsExplicitConclusionWhenNothingToClean(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DEC_HOME", filepath.Join(home, ".dec"))
	if err := os.MkdirAll(filepath.Join(home, ".dec", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewLocalCleanup(CleanupTierLight)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Items == nil {
		t.Fatal("无可清理项时应返回空切片而不是 nil")
	}
	if len(preview.Items) != 0 {
		t.Fatalf("不应发现可清理项: %#v", preview.Items)
	}
}

func TestCleanupRejectsUnknownTier(t *testing.T) {
	setupCleanupFixture(t)
	if _, err := CleanupLocalInstallation(context.Background(), LocalCleanupInput{
		Confirmed: true, Tier: "medium",
	}, nil); err == nil {
		t.Fatal("非法档位应拒绝执行")
	}
	if _, err := ParseCleanupTier("partial"); err == nil {
		t.Fatal("非法档位不应回退到任一档位")
	}
}

func TestParseCleanupTierDefaultsToFull(t *testing.T) {
	for _, raw := range []string{"", "  ", "FULL", "Full"} {
		tier, err := ParseCleanupTier(raw)
		if err != nil {
			t.Fatalf("%q 解析失败: %v", raw, err)
		}
		if tier != CleanupTierFull {
			t.Fatalf("%q 应解析为 full，实际 %s", raw, tier)
		}
	}
	light, err := ParseCleanupTier("light")
	if err != nil || light != CleanupTierLight {
		t.Fatalf("light 解析错误: %v %s", err, light)
	}
	if CleanupTierLight.ConfirmPhrase() == CleanupTierFull.ConfirmPhrase() {
		t.Fatal("两档确认词必须区分")
	}
}

func mustWriteCleanupFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
