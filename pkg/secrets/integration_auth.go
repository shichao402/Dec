package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// IntegrationAuthRel 是项目内集成测试 Bitwarden 凭据的相对路径（勿提交 git）。
const IntegrationAuthRel = ".secrets/dec/integration/bitwarden.yaml"

// IntegrationAuth 描述集成 / live 测试用的 Bitwarden 账号（专用测试账户，2FA 关闭）。
type IntegrationAuth struct {
	ServerURL string `yaml:"server_url"`
	Email     string `yaml:"email"`
	Password  string `yaml:"password"`
}

// IntegrationAuthPath 返回项目根下集成测试凭据文件的绝对路径。
func IntegrationAuthPath(projectRoot string) string {
	return filepath.Join(projectRoot, IntegrationAuthRel)
}

// IsIntegrationAuthRelWithinBundle 判断 bundle 内相对路径是否为本地测试凭据（不参与 push/pull）。
func IsIntegrationAuthRelWithinBundle(relWithinBundle string) bool {
	rel := filepath.ToSlash(strings.TrimSpace(relWithinBundle))
	return rel == "integration/bitwarden.yaml" || strings.HasPrefix(rel, "integration/")
}

// LoadIntegrationAuth 读取项目 `.secrets/dec/integration/bitwarden.yaml`。
func LoadIntegrationAuth(projectRoot string) (*IntegrationAuth, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}
	path := IntegrationAuthPath(projectRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取集成测试 Bitwarden 凭据失败: %w", err)
	}
	auth := &IntegrationAuth{}
	if err := yaml.Unmarshal(data, auth); err != nil {
		return nil, fmt.Errorf("解析集成测试 Bitwarden 凭据失败: %w", err)
	}
	auth.Email = strings.TrimSpace(auth.Email)
	auth.Password = strings.TrimSpace(auth.Password)
	auth.ServerURL = strings.TrimSpace(auth.ServerURL)
	if auth.Email == "" || auth.Password == "" {
		return nil, fmt.Errorf("集成测试 Bitwarden 凭据缺少 email 或 password")
	}
	if auth.ServerURL == "" {
		auth.ServerURL = DefaultServerURL
	}
	return auth, nil
}

// FindProjectRootWithIntegrationAuth 从 cwd 向上查找含集成测试凭据的项目根。
func FindProjectRootWithIntegrationAuth() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 32; i++ {
		if _, statErr := os.Stat(IntegrationAuthPath(dir)); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// ApplyIntegrationAuth 将项目内集成测试凭据同步到进程环境与本机 secrets 配置。
// 写入 ~/.dec/secrets/config.yaml 的 email；设置 DEC_BW_PASSWORD（若尚未设置）。
func ApplyIntegrationAuth(projectRoot string) (*IntegrationAuth, error) {
	auth, err := LoadIntegrationAuth(projectRoot)
	if err != nil {
		return nil, err
	}
	if auth == nil {
		return nil, nil
	}

	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	changed := false
	if auth.ServerURL != "" && cfg.ServerURL != auth.ServerURL {
		cfg.ServerURL = auth.ServerURL
		changed = true
	}
	if cfg.Email == "" || isPlaceholderEmail(cfg.Email) {
		cfg.Email = auth.Email
		changed = true
	}
	if changed {
		if err := SaveConfig(cfg); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(os.Getenv("DEC_BW_PASSWORD")) == "" {
		if err := os.Setenv("DEC_BW_PASSWORD", auth.Password); err != nil {
			return nil, fmt.Errorf("设置 DEC_BW_PASSWORD 失败: %w", err)
		}
	}
	return auth, nil
}

func integrationAuthPassword() string {
	if password := strings.TrimSpace(os.Getenv("DEC_BW_PASSWORD")); password != "" {
		return password
	}
	root := FindProjectRootWithIntegrationAuth()
	if root == "" {
		return ""
	}
	auth, err := LoadIntegrationAuth(root)
	if err != nil || auth == nil {
		return ""
	}
	return auth.Password
}

func ensureIntegrationEmailConfigured() error {
	if email := KnownEmail(); email != "" && !isPlaceholderEmail(email) {
		return nil
	}
	root := FindProjectRootWithIntegrationAuth()
	if root == "" {
		return nil
	}
	auth, err := LoadIntegrationAuth(root)
	if err != nil || auth == nil {
		return err
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if cfg.Email != "" && !isPlaceholderEmail(cfg.Email) {
		return nil
	}
	cfg.Email = auth.Email
	if auth.ServerURL != "" {
		cfg.ServerURL = auth.ServerURL
	}
	return SaveConfig(cfg)
}
