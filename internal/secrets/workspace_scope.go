package secrets

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
)

const (
	projectGCMBlockBegin = "# BEGIN DEC PROJECT GCM"
	projectGCMBlockEnd   = "# END DEC PROJECT GCM"
	projectSSHBlockBegin = "# BEGIN DEC PROJECT SSH"
	projectSSHBlockEnd   = "# END DEC PROJECT SSH"
)

// ProjectCredentialPaths 是 project 平面凭据定向的可检查落点。
type ProjectCredentialPaths struct {
	ID          string
	GitFragment string
	SSHFragment string
}

// ProjectCredentialScopePaths 返回项目专属 Git/SSH fragment 路径，不写盘。
func ProjectCredentialScopePaths(projectRoot string) (ProjectCredentialPaths, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return ProjectCredentialPaths{}, err
	}
	sum := sha256.Sum256([]byte(root))
	id := fmt.Sprintf("%x", sum[:8])
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ProjectCredentialPaths{}, fmt.Errorf("解析用户配置目录失败: %w", err)
	}
	sshDir, err := SSHDir()
	if err != nil {
		return ProjectCredentialPaths{}, err
	}
	return ProjectCredentialPaths{
		ID:          id,
		GitFragment: filepath.Join(configDir, "dec", "gitconfig.d", "project-"+id+".conf"),
		SSHFragment: filepath.Join(sshDir, "config.d", "dec-project-"+id+".conf"),
	}, nil
}

func canonicalProjectRoot(projectRoot string) (string, error) {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return "", fmt.Errorf("project 平面凭据需要 projectRoot")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("解析 projectRoot 失败: %w", err)
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs, nil
}

func gitdirCondition(projectRoot string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	return "gitdir/i:" + strings.TrimRight(filepath.ToSlash(root), "/") + "/.git", nil
}

func ensureProjectGitInclude(projectRoot, fragmentPath string) error {
	condition, err := gitdirCondition(projectRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fragmentPath), 0700); err != nil {
		return fmt.Errorf("创建 project Git config.d 失败: %w", err)
	}
	key := "includeIf." + condition + ".path"
	cmd := sysproc.Command("git", "config", "--global", "--replace-all", key, filepath.ToSlash(fragmentPath))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("登记 project Git includeIf 失败: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeProjectGitInclude(projectRoot string) error {
	condition, err := gitdirCondition(projectRoot)
	if err != nil {
		return err
	}
	key := "includeIf." + condition + ".path"
	cmd := sysproc.Command("git", "config", "--global", "--unset-all", key)
	if err := cmd.Run(); err != nil {
		// git config --unset-all 在 key 不存在时返回 5；撤销必须幂等。
		return nil
	}
	return nil
}

// CleanupProjectCredentialScope 撤销项目 Git includeIf，并删除 Dec 独占的 Git/SSH fragments。
func CleanupProjectCredentialScope(projectRoot string) error {
	paths, err := ProjectCredentialScopePaths(projectRoot)
	if err != nil {
		return err
	}
	if err := removeProjectGitInclude(projectRoot); err != nil {
		return err
	}
	for _, path := range []string{paths.GitFragment, paths.SSHFragment} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// UpsertProjectGitBlock 原子更新项目 Git fragment 中一个 Dec 管理块，并确保全局
// includeIf.gitdir 精确指向该工作区。OpenSSH 不参与此条件判断。
func UpsertProjectGitBlock(projectRoot, begin, end, body string) error {
	paths, err := ProjectCredentialScopePaths(projectRoot)
	if err != nil {
		return err
	}
	raw, err := readFileOrEmpty(paths.GitFragment)
	if err != nil {
		return err
	}
	updated, err := replaceManagedTextBlock(raw, begin, end, body)
	if err != nil {
		return err
	}
	if strings.TrimSpace(updated) == "" {
		if err := os.Remove(paths.GitFragment); err != nil && !os.IsNotExist(err) {
			return err
		}
		return removeProjectGitInclude(projectRoot)
	}
	if err := os.MkdirAll(filepath.Dir(paths.GitFragment), 0700); err != nil {
		return fmt.Errorf("创建 project Git config.d 失败: %w", err)
	}
	if err := writeSecureFile(paths.GitFragment, []byte(updated), 0600); err != nil {
		return err
	}
	return ensureProjectGitInclude(projectRoot, paths.GitFragment)
}

func replaceManagedTextBlock(raw, begin, end, body string) (string, error) {
	start := strings.Index(raw, begin)
	if start >= 0 {
		after := strings.Index(raw[start+len(begin):], end)
		if after < 0 {
			return "", fmt.Errorf("managed fragment 中有 %s 但缺少 %s", begin, end)
		}
		stop := start + len(begin) + after + len(end)
		for stop < len(raw) && (raw[stop] == '\r' || raw[stop] == '\n') {
			stop++
		}
		raw = raw[:start] + raw[stop:]
	}
	raw = strings.TrimSpace(raw)
	body = strings.TrimSpace(body)
	if body == "" {
		if raw == "" {
			return "", nil
		}
		return raw + "\n", nil
	}
	block := begin + "\n" + body + "\n" + end
	if raw == "" {
		return block + "\n", nil
	}
	return raw + "\n\n" + block + "\n", nil
}
