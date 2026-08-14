package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
)

// LandingCandidate 是一条待落地的 secrets 文件及其 SyncTarget 归属。
type LandingCandidate struct {
	Folder       string // Bitwarden folder
	LocalRoot    string // SyncTarget.LocalRoot
	RelativePath string // 相对 LocalRoot
	Plane        SyncPlane
}

// ValidateLandingPaths 在写盘之前校验全部落地路径。
//
// 校验：非法 note 路径、跨 folder 撞同一路径、逃出 LocalRoot、
// 落入 .dec/、符号链接逃逸、git 跟踪、项目 `.secrets/` 未被忽略。
func ValidateLandingPaths(projectRoot string, candidates []LandingCandidate) error {
	var projectCands, machineCands []LandingCandidate
	for _, c := range candidates {
		if c.Plane == SyncPlaneMachine {
			machineCands = append(machineCands, c)
		} else {
			projectCands = append(projectCands, c)
		}
	}

	if len(projectCands) > 0 {
		if err := ValidateSecretsRootIgnored(projectRoot); err != nil {
			return err
		}
		owners := make(map[string]string, len(projectCands))
		projectRels := make([]string, 0, len(projectCands))
		for _, candidate := range projectCands {
			noteRel, err := normalizeSyncRelPath(candidate.RelativePath)
			if err != nil {
				return err
			}
			root := strings.Trim(filepath.ToSlash(candidate.LocalRoot), "/")
			if root == "" {
				return fmt.Errorf("LandingCandidate.LocalRoot 不能为空")
			}
			projectRel := path.Join(root, noteRel)
			folder := strings.TrimSpace(candidate.Folder)
			if prev, ok := owners[projectRel]; ok {
				if prev != folder {
					return fmt.Errorf(
						"secrets 落地路径冲突: %s 同时由 Bitwarden folder %q 与 %q 管理，请在 Bitwarden 中改掉其中一个 Note 名",
						projectRel, prev, folder)
				}
				continue
			}
			owners[projectRel] = folder
			projectRels = append(projectRels, projectRel)

			if !strings.HasPrefix(projectRel, SecretsRootDir+"/") && projectRel != SecretsRootDir {
				return fmt.Errorf("secrets 落地路径必须位于 %s/ 下: %s", SecretsRootDir, projectRel)
			}
		}
		if err := ValidateNoOverlap(projectRoot, projectRels); err != nil {
			return err
		}
		if err := validateNoSymlinkEscape(projectRoot, projectRels); err != nil {
			return err
		}
		if err := validateNotGitTracked(projectRoot, projectRels); err != nil {
			return err
		}
	}

	if len(machineCands) > 0 {
		machineRoot, err := MachineSecretsRoot()
		if err != nil {
			return err
		}
		owners := make(map[string]string, len(machineCands))
		for _, candidate := range machineCands {
			noteRel, err := normalizeSyncRelPath(candidate.RelativePath)
			if err != nil {
				return err
			}
			root := strings.Trim(filepath.ToSlash(candidate.LocalRoot), "/")
			if root == "" {
				return fmt.Errorf("LandingCandidate.LocalRoot 不能为空")
			}
			if !strings.HasPrefix(root, MachineBundleSecretsRelPrefix+"/") && root != MachineBundleSecretsRelPrefix {
				return fmt.Errorf("机器级 secrets 落地路径必须位于 %s/ 下: %s", MachineBundleSecretsRelPrefix, root)
			}
			display := path.Join(".dec/secrets", root, noteRel)
			folder := strings.TrimSpace(candidate.Folder)
			if prev, ok := owners[display]; ok {
				if prev != folder {
					return fmt.Errorf(
						"secrets 落地路径冲突: %s 同时由 Bitwarden folder %q 与 %q 管理",
						display, prev, folder)
				}
				continue
			}
			owners[display] = folder

			abs := filepath.Join(machineRoot, filepath.FromSlash(root), filepath.FromSlash(noteRel))
			ancestor, err := existingAncestor(filepath.Dir(abs))
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("解析 %s 失败: %w", display, err)
			}
			machineEval, err := filepath.EvalSymlinks(machineRoot)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			if !isWithin(machineEval, resolved) {
				return fmt.Errorf("机器级 secrets 路径 %s 经符号链接逃出 ~/.dec/secrets: %s", display, resolved)
			}
		}
	}
	return nil
}

// ValidateSecretsRootIgnored 要求 `.secrets/` 整树被 gitignore（若在 git 工作区内）。
// 非 git 仓库或尚未初始化 .secrets 时直接通过。
func ValidateSecretsRootIgnored(projectRoot string) error {
	if !isGitWorkTree(projectRoot) {
		return nil
	}
	// 确保规则存在后再校验，避免「先 pull 才写 gitignore」的鸡生蛋。
	_ = EnsureSecretsGitignore(projectRoot)

	probe := filepath.ToSlash(path.Join(SecretsRootDir, ".dec-ignore-probe"))
	unignored := UnignoredLandingPaths(projectRoot, []string{probe})
	if len(unignored) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%s/ 未被 .gitignore 忽略；请在 .gitignore 中加入 /%s/ 后再同步 secrets",
		SecretsRootDir, SecretsRootDir)
}

func validateNoSymlinkEscape(projectRoot string, relPaths []string) error {
	root, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("解析项目根 %s 失败: %w", projectRoot, err)
	}

	for _, rel := range relPaths {
		target := filepath.Join(projectRoot, filepath.FromSlash(rel))
		ancestor, err := existingAncestor(filepath.Dir(target))
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("解析 %s 失败: %w", rel, err)
		}
		if !isWithin(root, resolved) {
			return fmt.Errorf("secrets 落地路径 %s 经符号链接指向项目外: %s", rel, resolved)
		}
	}
	return nil
}

func existingAncestor(dir string) (string, error) {
	for {
		if _, err := os.Lstat(dir); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("检查目录 %s 失败: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir, nil
		}
		dir = parent
	}
}

func isWithin(root, target string) bool {
	if root == target {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validateNotGitTracked(projectRoot string, relPaths []string) error {
	tracked, err := gitTrackedPaths(projectRoot, relPaths)
	if err != nil || len(tracked) == 0 {
		return err
	}
	sort.Strings(tracked)
	return fmt.Errorf(
		"以下 secrets 落地路径已被 git 跟踪，写入会把密钥暴露在版本库中: %s\n"+
			"  请先 git rm --cached 这些文件并确保 /%s/ 在 .gitignore 中，再重新 pull",
		strings.Join(tracked, ", "), SecretsRootDir)
}

func gitTrackedPaths(projectRoot string, relPaths []string) ([]string, error) {
	if len(relPaths) == 0 || !isGitWorkTree(projectRoot) {
		return nil, nil
	}
	args := append([]string{"ls-files", "-z", "--"}, relPaths...)
	out, err := runGit(projectRoot, args...)
	if err != nil {
		return nil, nil
	}
	return splitNulTerminated(out), nil
}

// UnignoredLandingPaths 返回未被 .gitignore 忽略的落地路径。
func UnignoredLandingPaths(projectRoot string, relPaths []string) []string {
	if len(relPaths) == 0 || !isGitWorkTree(projectRoot) {
		return nil
	}

	cmd := sysproc.Command("git", "check-ignore", "-z", "--stdin")
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader(strings.Join(relPaths, "\x00"))
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			return nil
		}
	}

	ignored := make(map[string]struct{})
	for _, p := range splitNulTerminated(out) {
		ignored[filepath.ToSlash(p)] = struct{}{}
	}

	var unignored []string
	for _, rel := range relPaths {
		key := filepath.ToSlash(rel)
		if _, ok := ignored[key]; !ok {
			unignored = append(unignored, key)
		}
	}
	sort.Strings(unignored)
	return unignored
}

func isGitWorkTree(projectRoot string) bool {
	out, err := runGit(projectRoot, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func runGit(dir string, args ...string) ([]byte, error) {
	cmd := sysproc.Command("git", args...)
	cmd.Dir = dir
	return cmd.Output()
}

func splitNulTerminated(out []byte) []string {
	var result []string
	for _, part := range strings.Split(string(out), "\x00") {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, filepath.ToSlash(part))
		}
	}
	return result
}
