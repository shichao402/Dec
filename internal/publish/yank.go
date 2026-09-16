package publish

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/registry"
	"github.com/shichao402/Dec/internal/types"
)

type YankOptions struct {
	Project     string
	Ref         string
	Undo        bool
	GitToken    string
	RegistryURL string
}

func Yank(ctx context.Context, opts YankOptions) error {
	project := strings.TrimSpace(opts.Project)
	ref := strings.TrimSpace(opts.Ref)
	if !types.IsValidProjectName(project) || ref == "" || ref == types.RequiresLatest {
		return fmt.Errorf("需要合法 --project 与精确 --ref")
	}
	tag, err := registry.Tag(project, ref)
	if err != nil {
		return err
	}
	url := strings.TrimSpace(opts.RegistryURL)
	if url == "" {
		url = registry.DefaultURL
	}
	work, err := os.MkdirTemp("", "dec-registry-yank-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	env := registry.TokenEnv(opts.GitToken)
	if err := checkoutRegistry(ctx, work, url, env); err != nil {
		return err
	}
	if _, err := registry.Git(ctx, work, "rev-parse", "-q", "--verify", tag+"^{commit}"); err != nil && !opts.Undo {
		return fmt.Errorf("tag %s 不存在，无法 yank", tag)
	}
	y, err := registry.LoadYanked(work)
	if err != nil {
		return err
	}
	if opts.Undo {
		y.Remove(project, ref)
	} else {
		y.Add(project, ref)
	}
	if err := registry.WriteYanked(work, y); err != nil {
		return err
	}
	if _, err := registry.Git(ctx, work, "config", "user.email", "dec-registry@local"); err != nil {
		return err
	}
	if _, err := registry.Git(ctx, work, "config", "user.name", "dec-registry"); err != nil {
		return err
	}
	if _, err := registry.Git(ctx, work, "add", "yanked.yaml"); err != nil {
		return err
	}
	msg := "yank " + tag
	if opts.Undo {
		msg = "unyank " + tag
	}
	if _, err := registry.Git(ctx, work, "commit", "-m", msg); err != nil {
		return err
	}
	_, err = registry.GitEnv(ctx, work, env, "push", "origin", "HEAD:refs/heads/"+registry.Branch)
	return err
}

type PurgeOptions struct {
	Project     string
	Ref         string
	Confirm     string
	GitToken    string
	RegistryURL string
}

func Purge(ctx context.Context, opts PurgeOptions) error {
	project := strings.TrimSpace(opts.Project)
	ref := strings.TrimSpace(opts.Ref)
	tag, err := registry.Tag(project, ref)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.Confirm) != tag {
		return fmt.Errorf("purge 必须 --confirm %s", tag)
	}
	url := strings.TrimSpace(opts.RegistryURL)
	if url == "" {
		url = registry.DefaultURL
	}
	work, err := os.MkdirTemp("", "dec-registry-purge-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	env := registry.TokenEnv(opts.GitToken)
	if err := checkoutRegistry(ctx, work, url, env); err != nil {
		return err
	}
	y, err := registry.LoadYanked(work)
	if err != nil {
		return err
	}
	y.Remove(project, ref)
	if err := registry.WriteYanked(work, y); err != nil {
		return err
	}
	if _, err := registry.Git(ctx, work, "config", "user.email", "dec-registry@local"); err != nil {
		return err
	}
	if _, err := registry.Git(ctx, work, "config", "user.name", "dec-registry"); err != nil {
		return err
	}
	if _, err := registry.Git(ctx, work, "add", "yanked.yaml"); err != nil {
		return err
	}
	_, _ = registry.Git(ctx, work, "commit", "-m", "purge "+tag)
	_, _ = registry.GitEnv(ctx, work, env, "push", "origin", "HEAD:refs/heads/"+registry.Branch)
	if _, err := registry.GitEnv(ctx, work, env, "push", "origin", ":refs/tags/"+tag); err != nil {
		return fmt.Errorf("删除远端 tag: %w", err)
	}
	return nil
}
