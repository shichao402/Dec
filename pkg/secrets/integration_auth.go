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

// IntegrationDecHomeRel 是集成 / live 测试专用的隔离 DEC_HOME（勿提交 git）。
// 复用同一目录可以保留 device.json 的 remember token，避免每次重跑都触发 2FA。
const IntegrationDecHomeRel = ".secrets/dec/integration/dec-home"

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

// ApplyIntegrationAuth 为集成 / live 测试准备 Bitwarden 认证：
//   - 将 DEC_HOME 指向仓库内的隔离目录，避免读写开发者真实的 ~/.dec；
//   - 在该隔离目录中写入集成账号的 server_url / email；
//   - 设置 DEC_BW_PASSWORD（若尚未设置）。
//
// 隔离是必需的：真实 ~/.dec 里通常是开发者本人的账号邮箱，沿用会造成
// 「真实邮箱 + 测试密码」的错配，登录成功时更会把测试数据写进真实 vault。
func ApplyIntegrationAuth(projectRoot string) (*IntegrationAuth, error) {
	auth, err := LoadIntegrationAuth(projectRoot)
	if err != nil {
		return nil, err
	}
	if auth == nil {
		return nil, nil
	}

	decHome := filepath.Join(projectRoot, IntegrationDecHomeRel)
	if err := os.MkdirAll(decHome, 0700); err != nil {
		return nil, fmt.Errorf("创建集成测试 DEC_HOME 失败: %w", err)
	}
	if err := os.Setenv("DEC_HOME", decHome); err != nil {
		return nil, fmt.Errorf("设置集成测试 DEC_HOME 失败: %w", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.ServerURL != auth.ServerURL || cfg.Email != auth.Email {
		cfg.ServerURL = auth.ServerURL
		cfg.Email = auth.Email
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
