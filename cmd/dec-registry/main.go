package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/shichao402/Dec/internal/contribute"
	"github.com/shichao402/Dec/internal/publish"
)

func main() {
	root := &cobra.Command{Use: "dec-registry", Short: "Dec 官方注册表 CI 工具（非用户面）"}
	root.AddCommand(publishCmd(), yankCmd(), purgeCmd(), contributeCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func publishCmd() *cobra.Command {
	var root, ref, token, url string
	cmd := &cobra.Command{
		Use:   "publish-provides",
		Short: "把当前 tag 树的 provides 发布到 Dec registry 分支",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = os.Getenv("DEC_REGISTRY_TOKEN")
			}
			r, err := publish.Publish(cmd.Context(), publish.Options{
				ProjectRoot: root, Ref: ref, GitToken: token, RegistryURL: url,
			})
			if err != nil {
				return err
			}
			for _, product := range r.Products {
				fmt.Printf("published %s commit=%s idempotent=%v\n", product.Tag, product.Commit, product.Idempotent)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "project-root", "", "提供方仓库根")
	cmd.Flags().StringVar(&ref, "ref", "", "提供方版本，如 v0.3.24")
	cmd.Flags().StringVar(&token, "git-token", "", "写 Dec 仓的 token")
	cmd.Flags().StringVar(&url, "registry-url", "", "覆盖默认 Dec 仓 URL")
	_ = cmd.MarkFlagRequired("project-root")
	_ = cmd.MarkFlagRequired("ref")
	return cmd
}

func yankCmd() *cobra.Command {
	var project, ref, token, url string
	var undo bool
	cmd := &cobra.Command{
		Use:   "yank-provides",
		Short: "yank 或撤销 yank 一个已发布版本",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = os.Getenv("DEC_REGISTRY_TOKEN")
			}
			return publish.Yank(context.Background(), publish.YankOptions{
				Project: project, Ref: ref, Undo: undo, GitToken: token, RegistryURL: url,
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "提供方项目名")
	cmd.Flags().StringVar(&ref, "ref", "", "精确版本")
	cmd.Flags().BoolVar(&undo, "undo", false, "撤销 yank")
	cmd.Flags().StringVar(&token, "git-token", "", "")
	cmd.Flags().StringVar(&url, "registry-url", "", "")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("ref")
	return cmd
}

func purgeCmd() *cobra.Command {
	var project, ref, confirm, token, url string
	cmd := &cobra.Command{
		Use:   "purge-provides",
		Short: "删除官方 tag（仅泄漏场景）",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = os.Getenv("DEC_REGISTRY_TOKEN")
			}
			return publish.Purge(cmd.Context(), publish.PurgeOptions{
				Project: project, Ref: ref, Confirm: confirm, GitToken: token, RegistryURL: url,
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "")
	cmd.Flags().StringVar(&ref, "ref", "", "")
	cmd.Flags().StringVar(&confirm, "confirm", "", "必须等于 registry/<项目>/<版本>")
	cmd.Flags().StringVar(&token, "git-token", "", "")
	cmd.Flags().StringVar(&url, "registry-url", "", "")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("ref")
	_ = cmd.MarkFlagRequired("confirm")
	return cmd
}

func contributeCmd() *cobra.Command {
	var origin, asset, title, body, diff, mode, branch string
	cmd := &cobra.Command{
		Use:   "contribute",
		Short: "把草稿以 PR 或 Issue 提交到提供方源仓（不写 registry）",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := contribute.Propose(cmd.Context(), contribute.Options{
				OriginRepo: origin, Asset: asset, Title: title, Body: body, Diff: diff,
				Mode: contribute.Mode(mode), Branch: branch,
			})
			if err != nil {
				return err
			}
			fmt.Printf("%s %s\n", r.Kind, r.URL)
			return nil
		},
	}
	cmd.Flags().StringVar(&origin, "origin-repo", "", "owner/name")
	cmd.Flags().StringVar(&asset, "asset", "", "")
	cmd.Flags().StringVar(&title, "title", "", "")
	cmd.Flags().StringVar(&body, "body", "", "")
	cmd.Flags().StringVar(&diff, "diff", "", "unified diff")
	cmd.Flags().StringVar(&mode, "mode", "auto", "auto|pr|issue")
	cmd.Flags().StringVar(&branch, "branch", "", "开 PR 时已推送分支")
	_ = cmd.MarkFlagRequired("origin-repo")
	_ = cmd.MarkFlagRequired("diff")
	return cmd
}
