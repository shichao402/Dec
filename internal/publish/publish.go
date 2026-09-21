package publish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

type Options struct {
	ProjectRoot string
	Ref         string
	GitToken    string
	RegistryURL string
}

type Result struct {
	Project string
	Tag     string
	Commit  string
	Idempotent bool
}

func Publish(ctx context.Context, opts Options) (*Result, error) {
	root := strings.TrimSpace(opts.ProjectRoot)
	ref := strings.TrimSpace(opts.Ref)
	if root == "" || ref == "" {
		return nil, fmt.Errorf("需要 --project-root 与 --ref")
	}
	mgr := config.NewProjectConfigManager(root)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	project := strings.TrimSpace(cfg.ProjectName)
	if !types.IsValidProjectName(project) {
		return nil, fmt.Errorf("项目配置缺少合法 project_name")
	}
	if len(cfg.Provides) == 0 {
		return nil, fmt.Errorf("没有 provides，无内容可发布")
	}
	tag, err := registry.Tag(project, ref)
	if err != nil {
		return nil, err
	}
	url := strings.TrimSpace(opts.RegistryURL)
	if url == "" {
		url = registry.DefaultURL
	}

	work, err := os.MkdirTemp("", "dec-registry-publish-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	env := registry.TokenEnv(opts.GitToken)
	if err := checkoutRegistry(ctx, work, url, env); err != nil {
		return nil, err
	}

	existing, err := registry.GitEnv(ctx, work, env, "rev-parse", "-q", "--verify", tag+"^{commit}")
	if err == nil && existing != "" {
		tmpSnap, err := os.MkdirTemp("", "dec-snapshot-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmpSnap)
		if err := writeSnapshot(root, tmpSnap, project, cfg); err != nil {
			return nil, err
		}
		localHash, err := hashDir(filepath.Join(tmpSnap, project))
		if err != nil {
			return nil, err
		}
		remoteHash, err := hashRefTree(ctx, work, tag, project)
		if err != nil {
			return nil, err
		}
		if localHash == remoteHash {
			return &Result{Project: project, Tag: tag, Commit: existing, Idempotent: true}, nil
		}
		return nil, fmt.Errorf("tag %s 已存在且内容不同，拒绝改写", tag)
	}

	if err := writeSnapshot(root, work, project, cfg); err != nil {
		return nil, err
	}

	if _, err := registry.Git(ctx, work, "add", "-A"); err != nil {
		return nil, err
	}
	if _, err := registry.Git(ctx, work, "config", "user.email", "dec-registry@local"); err != nil {
		return nil, err
	}
	if _, err := registry.Git(ctx, work, "config", "user.name", "dec-registry"); err != nil {
		return nil, err
	}
	if _, err := registry.Git(ctx, work, "commit", "--allow-empty", "-m", "publish "+tag); err != nil {
		return nil, err
	}
	if _, err := registry.Git(ctx, work, "tag", tag); err != nil {
		return nil, err
	}
	commit, err := registry.Git(ctx, work, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if _, err := registry.GitEnv(ctx, work, env, "push", "origin", "HEAD:refs/heads/"+registry.Branch); err != nil {
		return nil, err
	}
	if _, err := registry.GitEnv(ctx, work, env, "push", "origin", "refs/tags/"+tag); err != nil {
		return nil, err
	}
	return &Result{Project: project, Tag: tag, Commit: commit}, nil
}

func checkoutRegistry(ctx context.Context, work, url string, env []string) error {
	if _, err := registry.GitEnv(ctx, "", env, "clone", "--branch", registry.Branch, "--single-branch", url, work); err != nil {
		if _, err2 := registry.GitEnv(ctx, "", env, "clone", "--no-checkout", url, work); err2 != nil {
			return fmt.Errorf("克隆注册表失败: %w", err)
		}
		if _, err := registry.Git(ctx, work, "checkout", "--orphan", registry.Branch); err != nil {
			return err
		}
	}
	_, _ = registry.GitEnv(ctx, work, env, "fetch", "origin", "refs/tags/"+registry.TagPrefix+"*:refs/tags/"+registry.TagPrefix+"*")
	return nil
}

func writeSnapshot(projectRoot, registryRoot, project string, cfg *types.ProjectConfig) error {
	for _, key := range sortedProvideKeys(cfg) {
		item := cfg.Provides[key]
		target, err := config.ProjectProvideTarget(project, item)
		if err != nil {
			return err
		}
		src := filepath.Join(projectRoot, filepath.FromSlash(item.Source))
		dst := filepath.Join(registryRoot, filepath.FromSlash(target))
		if err := copyPath(src, dst); err != nil {
			return fmt.Errorf("复制 %s: %w", item.Source, err)
		}
	}
	origin := strings.TrimSpace(cfg.OriginRepo)
	if origin == "" {
		origin = detectOriginRepo(projectRoot)
	}
	return registry.WriteProviderMeta(filepath.Join(registryRoot, project), registry.ProviderMeta{OriginRepo: origin})
}

func detectOriginRepo(projectRoot string) string {
	cmd := exec.Command("git", "-C", projectRoot, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(out))
	if https, convErr := repo.HTTPSRemoteURL(raw); convErr == nil {
		return https
	}
	return raw
}

func hashRefTree(ctx context.Context, repoDir, spec, project string) (string, error) {
	tmp, err := os.MkdirTemp("", "dec-registry-tree-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if _, err := registry.Git(ctx, repoDir, "--work-tree", tmp, "checkout", spec, "--", "."); err != nil {
		return "", err
	}
	return hashDir(filepath.Join(tmp, project))
}

func hashDir(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		_, _ = io.WriteString(h, filepath.ToSlash(rel)+"\n")
		_, _ = h.Write(b)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sortedProvideKeys(cfg *types.ProjectConfig) []string {
	keys := make([]string, 0, len(cfg.Provides))
	for k := range cfg.Provides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return copyFile(src, dst)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return copyFile(p, out)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
