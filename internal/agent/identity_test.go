package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentitySaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewIdentityStore(dir)

	if store.Has() {
		t.Fatal("空状态目录不应有身份")
	}
	if _, err := store.Load(); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("空状态目录应返回 ErrNoIdentity，实际 %v", err)
	}

	priv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	id := &Identity{DeviceID: "dev-1", CertPEM: "cert-pem", KeyPEM: priv}
	if err := store.Save(id); err != nil {
		t.Fatalf("保存身份失败: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("读取身份失败: %v", err)
	}
	if got.DeviceID != "dev-1" || got.CertPEM != "cert-pem" || got.KeyPEM != priv {
		t.Fatalf("身份内容不符：%+v", got)
	}
}

func TestIdentityFilesAre0600(t *testing.T) {
	dir := t.TempDir()
	store := NewIdentityStore(dir)
	priv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	if err := store.Save(&Identity{DeviceID: "dev-1", CertPEM: "cert", KeyPEM: priv}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	for _, p := range []string{IdentityFile(dir), identityKeyFile(dir)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("读取 %q 失败: %v", p, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%q 权限 %o，期望 600", p, got)
		}
	}
	// 身份目录本身也要收紧：里面的文件名是固定的，目录可读就没意义了。
	info, err := os.Stat(identityDir(dir))
	if err != nil {
		t.Fatalf("读取身份目录失败: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("身份目录权限 %o，期望 700", got)
	}
}

func TestIdentityFileOmitsPrivateKey(t *testing.T) {
	dir := t.TempDir()
	store := NewIdentityStore(dir)
	priv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	if err := store.Save(&Identity{DeviceID: "dev-1", CertPEM: "cert", KeyPEM: priv}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	data, err := os.ReadFile(IdentityFile(dir))
	if err != nil {
		t.Fatalf("读取身份文件失败: %v", err)
	}
	// 私钥只存一处：身份文件里不该再有副本。
	if string(data) == "" || containsKey(data) {
		t.Fatalf("身份文件不应包含私钥：%s", data)
	}
}

// containsKey 判断身份文件里是否混进了私钥字段。
func containsKey(data []byte) bool {
	for _, needle := range []string{"key_pem", "PRIVATE KEY"} {
		if len(data) > 0 && indexOf(data, needle) >= 0 {
			return true
		}
	}
	return false
}

// indexOf 是简易的字节子串查找，避免为一行判断引入 strings 依赖歧义。
func indexOf(haystack []byte, needle string) int {
	n := len(needle)
	if n == 0 || len(haystack) < n {
		return -1
	}
	for i := 0; i+n <= len(haystack); i++ {
		if string(haystack[i:i+n]) == needle {
			return i
		}
	}
	return -1
}

func TestIdentityClear(t *testing.T) {
	dir := t.TempDir()
	store := NewIdentityStore(dir)
	priv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	if err := store.Save(&Identity{DeviceID: "dev-1", CertPEM: "cert", KeyPEM: priv}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("清除失败: %v", err)
	}
	if store.Has() {
		t.Fatal("清除后不应还有身份")
	}
	// 重复清除必须幂等：卸载流程会无条件清一次。
	if err := store.Clear(); err != nil {
		t.Fatalf("重复清除应幂等: %v", err)
	}
}

func TestIdentityRejectsIncomplete(t *testing.T) {
	dir := t.TempDir()
	store := NewIdentityStore(dir)
	for _, id := range []*Identity{
		nil,
		{DeviceID: "", CertPEM: "c", KeyPEM: "k"},
		{DeviceID: "d", CertPEM: "", KeyPEM: "k"},
		{DeviceID: "d", CertPEM: "c", KeyPEM: ""},
	} {
		if err := store.Save(id); err == nil {
			t.Fatalf("不完整身份应被拒绝：%+v", id)
		}
	}
}

func TestIdentityLoad2ReturnsNilWithoutIdentity(t *testing.T) {
	store := NewIdentityStore(t.TempDir())
	if got := store.Load2(); got != nil {
		t.Fatalf("没有身份时应返回 nil，实际 %+v", got)
	}
}

func TestConfigRoundTripAndHTTPSOnly(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("读取空配置失败: %v", err)
	}
	if cfg.ServerURL != "" {
		t.Fatalf("空配置的服务地址应为空，实际 %q", cfg.ServerURL)
	}
	// 严格 TLS 的入口：地址写错要在这里就挡住，而不是等到握手失败。
	if err := SaveConfig(dir, &Config{ServerURL: "http://dec.test"}); err == nil {
		t.Fatal("http 地址应被拒绝")
	}
	if err := SaveConfig(dir, &Config{ServerURL: "dec.test"}); err == nil {
		t.Fatal("缺少 scheme 的地址应被拒绝")
	}
	if err := SaveConfig(dir, &Config{ServerURL: "https://dec.test/"}); err != nil {
		t.Fatalf("https 地址应被接受: %v", err)
	}
	cfg, err = LoadConfig(dir)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if cfg.ServerURL != "https://dec.test" {
		t.Fatalf("尾部斜杠应被去掉，实际 %q", cfg.ServerURL)
	}
	info, err := os.Stat(ConfigFile(dir))
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("配置权限 %o，期望 600", got)
	}
}

func TestProjectsRequireAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	if err := SaveProjects(dir, []Project{{ID: "p1", Path: "relative/demo"}}); err == nil {
		t.Fatal("相对路径的项目应被拒绝")
	}
	if err := SaveProjects(dir, []Project{{ID: "", Path: "/tmp/a"}}); err == nil {
		t.Fatal("空 ID 应被拒绝")
	}
	if err := SaveProjects(dir, []Project{
		{ID: "p1", Path: "/tmp/a"},
		{ID: "p1", Path: "/tmp/b"},
	}); err == nil {
		t.Fatal("重复 ID 应被拒绝")
	}
}

func TestProjectsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := []Project{
		{ID: "p2", Path: "/tmp/b/"},
		{ID: "p1", Path: "/tmp/a"},
	}
	if err := SaveProjects(dir, in); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	got, err := LoadProjects(dir)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("项目数 %d，期望 2", len(got))
	}
	// 排序稳定，便于页面展示与上报。
	if got[0].ID != "p1" || got[1].ID != "p2" {
		t.Fatalf("项目未按 ID 排序：%+v", got)
	}
	// 路径应被清理掉尾部斜杠。
	if got[1].Path != filepath.Clean("/tmp/b") {
		t.Fatalf("路径未规范化：%q", got[1].Path)
	}
	info, err := os.Stat(projectsFile(dir))
	if err != nil {
		t.Fatalf("读取项目文件失败: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("项目登记权限 %o，期望 600", perm)
	}
}

func TestProjectsEmptyWhenMissing(t *testing.T) {
	got, err := LoadProjects(t.TempDir())
	if err != nil {
		t.Fatalf("缺少文件不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("缺少文件应返回空列表，实际 %+v", got)
	}
}

func TestDefaultStateDirHonorsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEC_AGENT_STATE", dir)
	got, err := DefaultStateDir()
	if err != nil {
		t.Fatalf("取状态目录失败: %v", err)
	}
	if got != dir {
		t.Fatalf("状态目录 %q，期望 %q", got, dir)
	}
}
