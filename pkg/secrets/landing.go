package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// LandingCandidate 是一条待落地的 secrets 文件及其 Bitwarden folder 归属。
// Folder 用于在冲突报错时指出是哪两个分组撞了同一路径。
type LandingCandidate struct {
	Folder       string
	RelativePath string
}

// ValidateLandingPaths 在写盘之前校验全部落地路径。
//
// 必须先于写盘调用：note 名来自远端，未校验就落盘等于允许远端覆盖项目内任意文件
// （包括 .dec/config.yaml）。校验涵盖非法路径、跨 folder 撞车、.dec/ 重叠、
// 符号链接逃逸与 git 跟踪。
func ValidateLandingPaths(projectRoot string, candidates []LandingCandidate) error {
	owners := make(map[string]string, len(candidates))
	paths := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		rel, err := normalizeProjectRelativePath(candidate.RelativePath)
		if err != nil {
			return err
		}
		folder := strings.TrimSpace(candidate.Folder)
		if prev, ok := owners[rel]; ok {
			if prev != folder {
				return fmt.Errorf(
					"secrets 落地路径冲突: %s 同时由 Bitwarden folder %q 与 %q 管理，请在 Bitwarden 中改掉其中一个 Note 名",
					rel, prev, folder)
			}
			continue
		}
		owners[rel] = folder
		paths = append(paths, rel)
	}

	if err := ValidateNoOverlap(projectRoot, paths); err != nil {
		return err
	}
	if err := validateNoSymlinkEscape(projectRoot, paths); err != nil {
		return err
	}
	return validateNotGitTracked(projectRoot, paths)
}

// validateNoSymlinkEscape 确认落地路径已存在的祖先目录解析后仍在项目内。
// 消费者路径（如 .config/）可能是用户自建的符号链接，dec 不能顺着它写到项目外。
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

// validateNotGitTracked 拒绝把 secrets 落到已被 git 跟踪的路径。
//
// 落地路径就是消费者路径（config/server.yaml、.env.local 等），在被管项目里很可能
// 已经提交过。写进去会让密钥出现在工作区改动里，极易被误 commit。dec 不代改
// .gitignore（有测试锁这个 invariant），所以这里只能硬失败，由用户先处理。
func validateNotGitTracked(projectRoot string, relPaths []string) error {
	tracked, err := gitTrackedPaths(projectRoot, relPaths)
	if err != nil || len(tracked) == 0 {
		return err
	}
	sort.Strings(tracked)
	return fmt.Errorf(
		"以下 secrets 落地路径已被 git 跟踪，写入会把密钥暴露在版本库中: %s\n"+
			"  请先 git rm --cached 这些文件并加入 .gitignore，再重新 pull",
		strings.Join(tracked, ", "))
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

// UnignoredLandingPaths 返回未被 .gitignore 忽略的落地路径，供调用方发出警告。
// 与 validateNotGitTracked 不同，这里只是提醒：文件尚未被跟踪，加 .gitignore 即可。
func UnignoredLandingPaths(projectRoot string, relPaths []string) []string {
	if len(relPaths) == 0 || !isGitWorkTree(projectRoot) {
		return nil
	}

	cmd := exec.Command("git", "check-ignore", "-z", "--stdin")
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader(strings.Join(relPaths, "\x00"))
	out, err := cmd.Output()
	if err != nil {
		// 退出码 1 表示没有任何路径被忽略，不是故障；其他错误按“无法判断”处理。
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			return nil
		}
	}

	ignored := make(map[string]struct{})
	for _, p := range splitNulTerminated(out) {
		ignored[p] = struct{}{}
	}

	var unignored []string
	for _, rel := range relPaths {
		if _, ok := ignored[rel]; !ok {
			unignored = append(unignored, rel)
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
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Output()
}

func splitNulTerminated(out []byte) []string {
	var result []string
	for _, part := range strings.Split(string(out), "\x00") {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
