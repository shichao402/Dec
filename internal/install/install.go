package install

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/types"
)

type Resolved struct {
	Project string
	Version string
	Tag     string
	Yanked  bool
	Warning string
}

type Options struct {
	CacheDir    string
	RegistryURL string
	GitToken    string
	Requires    types.RequiresSpec
}

func Official(ctx context.Context, opts Options) ([]Resolved, error) {
	req, err := types.NormalizeRequiresSpec(opts.Requires)
	if err != nil {
		return nil, err
	}
	if len(req) == 0 {
		return nil, nil
	}
	url := strings.TrimSpace(opts.RegistryURL)
	if url == "" {
		url = registry.DefaultURL
	}
	env := registry.TokenEnv(opts.GitToken)
	tags, err := registry.ListRemoteTags(ctx, url, env)
	if err != nil {
		return nil, fmt.Errorf("列举注册表 tag: %w", err)
	}
	work, err := os.MkdirTemp("", "dec-registry-install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	if err := cloneRegistry(ctx, work, url, env); err != nil {
		return nil, err
	}
	yanked, err := registry.LoadYanked(work)
	if err != nil {
		return nil, err
	}
	var out []Resolved
	for project, want := range req {
		versions := registry.VersionsFromTags(project, tags)
		tag, version, err := registry.Resolve(project, want, versions, yanked)
		if err != nil {
			if want != types.RequiresLatest {
				return nil, err
			}
			return nil, err
		}
		item := Resolved{Project: project, Version: version, Tag: tag, Yanked: yanked.Contains(project, version)}
		if item.Yanked {
			item.Warning = fmt.Sprintf("%s@%s 已被 yank，仍按精确 requires 安装", project, version)
		}
		if err := materializeTag(ctx, work, opts.CacheDir, project, tag); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func cloneRegistry(ctx context.Context, work, url string, env []string) error {
	if _, err := registry.GitEnv(ctx, "", env, "clone", "--branch", registry.Branch, "--single-branch", url, work); err != nil {
		return fmt.Errorf("克隆注册表失败: %w", err)
	}
	_, _ = registry.GitEnv(ctx, work, env, "fetch", "origin", "refs/tags/"+registry.TagPrefix+"*:refs/tags/"+registry.TagPrefix+"*")
	return nil
}

func materializeTag(ctx context.Context, repoDir, cacheDir, project, tag string) error {
	tmp, err := os.MkdirTemp("", "dec-registry-co-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := registry.Git(ctx, repoDir, "--work-tree", tmp, "checkout", tag, "--", "."); err != nil {
		return fmt.Errorf("检出 %s: %w", tag, err)
	}
	src := filepath.Join(tmp, project)
	dst := filepath.Join(cacheDir, project)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return copyDir(src, dst)
}

// CacheAsset 是 cache 里一份已物化的官方资产。
type CacheAsset struct {
	Project    string
	Visibility types.AssetVisibility
	Plane      types.AssetPlane
	Type       string
	Name       string
	Path       string
}

func ListCache(cacheDir, project string) ([]CacheAsset, error) {
	root := filepath.Join(cacheDir, project)
	var out []CacheAsset
	for _, vis := range []types.AssetVisibility{types.AssetVisibilityPublic, types.AssetVisibilityPrivate} {
		for _, plane := range []types.AssetPlane{types.AssetPlaneGlobal, types.AssetPlaneLocal} {
			for _, kind := range bundle.VaultAssetKinds {
				base := filepath.Join(root, string(vis), string(plane), kind.Dir)
				entries, err := os.ReadDir(base)
				if err != nil {
					if os.IsNotExist(err) {
						continue
					}
					return nil, err
				}
				for _, e := range entries {
					out = append(out, CacheAsset{
						Project: project, Visibility: vis, Plane: plane, Type: kind.Type,
						Name: bundle.AssetEntryName(kind, e.Name()), Path: filepath.Join(base, e.Name()),
					})
				}
			}
		}
	}
	return out, nil
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
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(f, in)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
