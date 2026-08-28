package compat

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if runtime.GOOS == "windows" {
		t.Setenv("HOMEDRIVE", filepath.VolumeName(home))
		t.Setenv("HOMEPATH", strings.TrimPrefix(home, filepath.VolumeName(home)))
	}
	return home
}

func TestRepairOnStartup_RemovesLegacyDecConfigDir(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	legacyDir := filepath.Join(root, ".dec", "config")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"architecture.json", "project.json", "technology.yaml"} {
		if err := os.WriteFile(filepath.Join(legacyDir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 现行配置：同级文件，必须保留。
	configYAML := filepath.Join(root, ".dec", "config.yaml")
	if err := os.WriteFile(configYAML, []byte("version: v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	notes := RepairOnStartup(root)
	if len(notes) != 1 || !strings.Contains(notes[0], ".dec/config") {
		t.Fatalf("notes = %#v", notes)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy dir 应已删除, err=%v", err)
	}
	if _, err := os.Stat(configYAML); err != nil {
		t.Fatalf("config.yaml 应保留: %v", err)
	}
}

func TestRepairOnStartup_IdempotentWhenAbsent(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".dec"), 0o755); err != nil {
		t.Fatal(err)
	}
	notes := RepairOnStartup(root)
	if len(notes) != 0 {
		t.Fatalf("无遗留文件时 notes 应为空, got %#v", notes)
	}
	notes = RepairOnStartup(root)
	if len(notes) != 0 {
		t.Fatalf("第二次仍应为空, got %#v", notes)
	}
}

func TestRepairOnStartup_EmptyRoot(t *testing.T) {
	isolateHome(t)
	if notes := RepairOnStartup(""); notes != nil {
		t.Fatalf("空 root 且无退役副本时 notes 应为空, got %#v", notes)
	}
	if notes := RepairOnStartup("   "); len(notes) != 0 {
		t.Fatalf("空白 root 且无退役副本时 notes 应为空, got %#v", notes)
	}
}

func TestRepairOnStartup_RemovesRetiredCLISkillCopies(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	userCopy := filepath.Join(home, ".cursor", "skills", "dec-cli-skill")
	projectCopy := filepath.Join(root, ".cursor", "skills", "dec-cli-skill")
	keep := filepath.Join(home, ".cursor", "skills", "dec-tencent-cloud")
	for _, dir := range []string{userCopy, projectCopy, keep} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	notes := RepairOnStartup(root)
	if len(notes) != 2 {
		t.Fatalf("应清理用户级+项目级各一份, notes=%#v", notes)
	}
	if _, err := os.Stat(userCopy); !os.IsNotExist(err) {
		t.Fatalf("用户级 dec-cli-skill 应已删除, err=%v", err)
	}
	if _, err := os.Stat(projectCopy); !os.IsNotExist(err) {
		t.Fatalf("项目级 dec-cli-skill 应已删除, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(keep, "SKILL.md")); err != nil {
		t.Fatalf("无关 skill 应保留: %v", err)
	}

	if notes := RepairOnStartup(root); len(notes) != 0 {
		t.Fatalf("幂等：第二次 notes 应为空, got %#v", notes)
	}
}

func TestRepairOnStartup_EmptyRootStillClearsUserCopies(t *testing.T) {
	home := isolateHome(t)
	userCopy := filepath.Join(home, ".cursor", "skills", "dec-cli-skill")
	if err := os.MkdirAll(userCopy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userCopy, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := RepairOnStartup("")
	if len(notes) != 1 {
		t.Fatalf("空 projectRoot 仍应清用户级副本, notes=%#v", notes)
	}
	if _, err := os.Stat(userCopy); !os.IsNotExist(err) {
		t.Fatalf("用户级副本应已删除, err=%v", err)
	}
}

func TestRepairOnStartup_LeavesNonDirConfigAlone(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	decDir := filepath.Join(root, ".dec")
	if err := os.MkdirAll(decDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 极端情况：名为 config 的普通文件（不是目录）。
	configPath := filepath.Join(decDir, "config")
	if err := os.WriteFile(configPath, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := RepairOnStartup(root)
	if len(notes) != 0 {
		t.Fatalf("非目录 config 不应被删, notes=%#v", notes)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("非目录 config 应保留: %v", err)
	}
}

func TestRepairOnStartup_PurgesLegacyPLayoutOnce(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("DEC_HOME", home)
	project := t.TempDir()
	legacy := []string{
		filepath.Join(home, "cache", "cnb"),
		filepath.Join(home, "secrets", "bundles", "cnb"),
		filepath.Join(project, ".dec", "cache", "dec"),
		filepath.Join(project, ".secrets", "bundles", "dec"),
		filepath.Join(project, ".dec"),
	}
	for _, dir := range legacy[:4] {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, ".dec", "config.yaml"), []byte("version: v2\nproject_name: Dec\nenabled_bundles:\n  - dec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := RepairOnStartup(project)
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "已删除") && !strings.Contains(joined, "cache") {
		t.Fatalf("notes = %#v", notes)
	}
	for _, dir := range []string{
		filepath.Join(home, "cache"),
		filepath.Join(home, "secrets", "bundles"),
		filepath.Join(project, ".dec", "cache"),
		filepath.Join(project, ".secrets", "bundles"),
	} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s 应已删除, err=%v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".dec", "config.yaml")); err != nil {
		t.Fatalf("项目配置应保留: %v", err)
	}
	notes = RepairOnStartup(project)
	if len(notes) != 0 {
		t.Fatalf("第二次应为空, got %#v", notes)
	}
}

// 集成测试凭据放在 .secrets/dec/integration/ 下，但它不是 P 落地内容。
// 启动清理误删会让 live 测试退回人工 web unlock。
func TestRepairOnStartup_KeepsIntegrationAuthWhilePurgingSecrets(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("DEC_HOME", home)
	project := t.TempDir()

	authPath := filepath.Join(project, filepath.FromSlash(secrets.IntegrationAuthRel))
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte("email: t@example.com\npassword: pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	devicePath := filepath.Join(project, filepath.FromSlash(secrets.IntegrationDecHomeRel), "secrets", "device.json")
	if err := os.MkdirAll(filepath.Dir(devicePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devicePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 同一个 P 名下的真实落地内容仍必须被清掉。
	stale := filepath.Join(project, ".secrets", "dec", ".env", "app.env")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("TOKEN=old"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(project, ".secrets", "relkit", ".env", "x.env")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("A=1"), 0o600); err != nil {
		t.Fatal(err)
	}

	RepairOnStartup(project)

	if _, err := os.Stat(authPath); err != nil {
		t.Fatalf("集成凭据必须保留: %v", err)
	}
	if _, err := os.Stat(devicePath); err != nil {
		t.Fatalf("隔离 DEC_HOME 必须保留: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".secrets", "dec", ".env")); !os.IsNotExist(err) {
		t.Fatalf("同 P 下的旧落地内容应删除, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".secrets", "relkit")); !os.IsNotExist(err) {
		t.Fatalf("其它 P 落地内容应删除, err=%v", err)
	}
}

func TestRepairOnStartup_UserPlaneMarkerDoesNotSkipProjectCleanup(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("DEC_HOME", home)
	project := t.TempDir()

	// 用户平面先启动并写机器 marker。
	RepairOnStartup("")

	stale := filepath.Join(project, ".secrets", "bundles", "dec", "old.env")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("OLD=1"), 0o600); err != nil {
		t.Fatal(err)
	}

	notes := RepairOnStartup(project)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("机器 marker 不得跳过项目清理, err=%v notes=%#v", err, notes)
	}
	mgr := config.NewProjectConfigManager(project)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatalf("读工作区配置: %v", err)
	}
	if cfg.LayoutVersion != types.LocalLayoutVersion {
		t.Fatalf("layout_version = %d, want %d", cfg.LayoutVersion, types.LocalLayoutVersion)
	}
}
