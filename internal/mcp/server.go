// Package mcp 提供 Dec 的 MCP（stdio）接口层。
//
// 架构约束：IDE 启动一次 `dec mcp` 后进程常驻；所有 tool 调用在同一进程内
// 直接调用 pkg/app，禁止 shell out 到 dec CLI 子进程（否则 Bitwarden session 无法复用）。
package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/pkg/app"
)

// Config 配置 Dec MCP Server。
type Config struct {
	ProjectRoot string
}

// Server 持有 MCP 服务状态。
type Server struct {
	cfg Config
}

// New 创建 Dec MCP Server。
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Register 向 MCP server 注册全部 Dec tools。
func (s *Server) Register(mcpServer *mcp.Server) {
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_status",
		Description: "查看当前项目的 Dec 状态（仓库连接、配置、bundle 概览）",
	}, s.handleStatus)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_connect_repo",
		Description: "连接 Dec 资产 Git 仓库（全局配置，写入 ~/.dec/config.yaml）",
	}, s.handleConnectRepo)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_init_project",
		Description: "初始化当前项目的 .dec/config.yaml（需已连接仓库）；可选从 vault project 自动应用 bundle",
	}, s.handleInitProject)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_assets",
		Description: "列出可用 bundle 与单资产及启用状态",
	}, s.handleListAssets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_set_assets",
		Description: "保存 bundle / 单资产启用选择到 .dec/config.yaml",
	}, s.handleSetAssets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_pull",
		Description: "拉取已启用 Dec bundle 与 secrets，并安装到 IDE 目录",
	}, s.handlePull)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_push",
		Description: "推送 .dec/cache/ 与 .secrets/ 变更到远端（Dec Git + Bitwarden）",
	}, s.handlePush)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_preview_push",
		Description: "预览 push 将涉及的 Dec 与 secrets 变更（不写入远端）",
	}, s.handlePreviewPush)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_secrets",
		Description: "列出私密资产元数据（路径、存在性）；绝不返回 token/密钥/文件正文",
	}, s.handleListSecrets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_delete_candidates",
		Description: "列出可删除的 Dec 资产、secrets 与 bundle",
	}, s.handleListDeleteCandidates)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_delete",
		Description: "删除选中的 Dec 资产、secrets 或 bundle（需 confirmed=true）",
	}, s.handleDelete)
}

// Run 启动 stdio MCP Server。
func Run(ctx context.Context, cfg Config) error {
	cfg.ProjectRoot = resolveProjectRoot(cfg.ProjectRoot)
	if cfg.ProjectRoot == "" {
		return fmt.Errorf("无法确定项目根目录：请设置 --project-root 或 DEC_PROJECT_ROOT")
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "dec",
		Version: "1.0.0",
	}, nil)
	s := New(cfg)
	s.Register(mcpServer)
	return mcpServer.Run(ctx, &mcp.StdioTransport{})
}

func resolveProjectRoot(flagValue string) string {
	if root := strings.TrimSpace(flagValue); root != "" {
		return root
	}
	if root := strings.TrimSpace(os.Getenv("DEC_PROJECT_ROOT")); root != "" {
		return root
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func (s *Server) projectRoot() string {
	return s.cfg.ProjectRoot
}

type statusParams struct{}

func (s *Server) handleStatus(ctx context.Context, _ *mcp.CallToolRequest, _ statusParams) (*mcp.CallToolResult, any, error) {
	overview, err := app.LoadProjectOverview(s.projectRoot())
	if err != nil {
		return toolFail(err, nil)
	}
	return toolOK(overview, nil)
}

type connectRepoParams struct {
	RepoURL string `json:"repo_url" jsonschema:"Dec Git 仓库 URL"`
}

func (s *Server) handleConnectRepo(ctx context.Context, _ *mcp.CallToolRequest, in connectRepoParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	result, err := app.ConnectRepo(in.RepoURL, reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}

type initProjectParams struct {
	ApplyVaultProject bool `json:"apply_vault_project,omitempty" jsonschema:"若 vault 存在同名 projects/<name>.yaml 则自动应用其 bundle"`
}

func (s *Server) handleInitProject(ctx context.Context, _ *mcp.CallToolRequest, in initProjectParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	prepared, err := app.PrepareProjectConfigInit(s.projectRoot(), reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	out := map[string]any{"init": prepared}
	if in.ApplyVaultProject {
		applied, applyErr := app.ApplyVaultProject(s.projectRoot(), reporter)
		if applyErr != nil {
			return toolFail(applyErr, logs())
		}
		out["vault_apply"] = applied
	}
	return toolOK(out, logs())
}

type listAssetsParams struct{}

func (s *Server) handleListAssets(ctx context.Context, _ *mcp.CallToolRequest, _ listAssetsParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	state, err := app.LoadAssetSelection(s.projectRoot(), reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(state, logs())
}

type assetItemInput struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Vault   string `json:"vault"`
	Enabled bool   `json:"enabled"`
}

type setAssetsParams struct {
	EnabledBundles []string         `json:"enabled_bundles,omitempty" jsonschema:"启用的 bundle 名称列表；省略表示不修改"`
	Items          []assetItemInput `json:"items,omitempty" jsonschema:"单资产勾选；省略表示从当前配置加载后仅更新 bundle"`
}

func (s *Server) handleSetAssets(ctx context.Context, _ *mcp.CallToolRequest, in setAssetsParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	root := s.projectRoot()

	items := make([]app.AssetSelectionItem, 0)
	if in.Items != nil {
		for _, item := range in.Items {
			items = append(items, app.AssetSelectionItem{
				Name:    item.Name,
				Type:    item.Type,
				Vault:   item.Vault,
				Enabled: item.Enabled,
			})
		}
	} else {
		state, err := app.LoadAssetSelection(root, reporter)
		if err != nil {
			return toolFail(err, logs())
		}
		items = append(items, state.Items...)
	}

	selection := app.AssetSaveSelection{Items: items}
	if in.EnabledBundles != nil {
		selection.EnabledBundles = append([]string(nil), in.EnabledBundles...)
	}

	result, err := app.SaveAssetSelection(root, selection, reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}

type pullParams struct{}

func (s *Server) handlePull(ctx context.Context, _ *mcp.CallToolRequest, _ pullParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	result, err := app.PullProjectAssets(s.toolContext(ctx), s.projectRoot(), "", reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}

type pushParams struct{}

func (s *Server) handlePush(ctx context.Context, _ *mcp.CallToolRequest, _ pushParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	result, err := app.PushProjectAssets(s.toolContext(ctx), s.projectRoot(), reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}

type previewPushParams struct{}

func (s *Server) handlePreviewPush(ctx context.Context, _ *mcp.CallToolRequest, _ previewPushParams) (*mcp.CallToolResult, any, error) {
	result, err := app.PreviewPushProjectAssets(s.projectRoot())
	if err != nil {
		return toolFail(err, nil)
	}
	return toolOK(result, nil)
}

type listSecretsParams struct {
	IncludeRemote *bool `json:"include_remote,omitempty" jsonschema:"是否检查 Bitwarden 远端存在性（默认 true，可能触发 web unlock）"`
}

func (s *Server) handleListSecrets(ctx context.Context, _ *mcp.CallToolRequest, in listSecretsParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	includeRemote := true
	if in.IncludeRemote != nil {
		includeRemote = *in.IncludeRemote
	}
	result, err := app.ListSecretsMetadata(s.toolContext(ctx), s.projectRoot(), includeRemote, reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}

type listDeleteCandidatesParams struct{}

func (s *Server) handleListDeleteCandidates(ctx context.Context, _ *mcp.CallToolRequest, _ listDeleteCandidatesParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	candidates, err := app.ListDeleteCandidates(s.toolContext(ctx), s.projectRoot(), reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(candidates, logs())
}

type deleteItemInput struct {
	Kind            string `json:"kind" jsonschema:"dec | secret | bundle"`
	Type            string `json:"type,omitempty"`
	Name            string `json:"name,omitempty"`
	Vault           string `json:"vault,omitempty"`
	SecretPath      string `json:"secret_path,omitempty"`
	SecretsBundle   string `json:"secrets_bundle,omitempty"`
	RelWithinBundle string `json:"rel_within_bundle,omitempty"`
	BundleName      string `json:"bundle_name,omitempty"`
}

type deleteParams struct {
	Items     []deleteItemInput `json:"items" jsonschema:"要删除的条目"`
	Confirmed bool              `json:"confirmed" jsonschema:"必须为 true 才会执行删除"`
}

func (s *Server) handleDelete(ctx context.Context, _ *mcp.CallToolRequest, in deleteParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	items := make([]app.DeleteSelectionItem, 0, len(in.Items))
	for _, item := range in.Items {
		items = append(items, app.DeleteSelectionItem{
			Kind:            app.DeleteItemKind(item.Kind),
			Type:            item.Type,
			Name:            item.Name,
			Vault:           item.Vault,
			SecretPath:      item.SecretPath,
			SecretsBundle:   item.SecretsBundle,
			RelWithinBundle: item.RelWithinBundle,
			BundleName:      item.BundleName,
		})
	}
	result, err := app.DeleteProjectItems(s.toolContext(ctx), app.DeleteProjectInput{
		ProjectRoot: s.projectRoot(),
		Items:       items,
		Confirmed:   in.Confirmed,
	}, reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}
