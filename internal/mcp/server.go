// Package mcp 提供 Dec 的 MCP（stdio）接口层。
//
// 架构约束：dec-mcp 是薄门面；业务与 Bitwarden session 由本机 dec-server 持有。
package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/serviceapi"
)

// Config 配置 Dec MCP Server。
type Config struct {
	ProjectRoot   string
	ClientVersion string
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
		Description: "查看某平面的 Dec 状态（仓库连接、配置、bundle 概览；plane=project|user）。plane=user 看个人跨项目平面。",
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
		Description: "列出某平面已启用的 bundle 及成员（plane=project|user|both）。想知道「当前项目/我个人装了什么、能改什么」先调它。",
	}, s.handleListAssets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_set_assets",
		Description: "设置某平面的 bundle 启用列表（写 enabled_bundles）。plane=project 写 <project>/.dec/config.yaml；plane=user 写 ~/.dec/config.yaml（个人跨项目复用）。改完通常再 dec_pull 同一平面落地。",
	}, s.handleSetAssets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_pull",
		Description: "拉取并安装某平面已启用的 Dec bundle 与 secrets（plane=project|user|both）。project 装进 <project> 内 IDE 目录，user 装进 ~ 用户级 IDE 目录。secrets 失败不阻断公开资产，走部分成功 + 警告。",
	}, s.handlePull)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_push",
		Description: "把某平面的本地改动推回远端（plane=project|user|both）：Dec 资产推 Git，secrets 推 Bitwarden。改了项目内 token 用 plane=project；改了个人凭据/SSH 用 plane=user；两边都改过用 both。",
	}, s.handlePush)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_preview_push",
		Description: "预览某平面 push 将涉及的 Dec 与 secrets 变更（plane=project|user|both，不写远端）。推之前先 preview 确认范围。",
	}, s.handlePreviewPush)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_secrets",
		Description: "列出某平面私密资产元数据（路径、本地/远端存在性；plane=project|user|both）。绝不返回 token/密钥/正文。",
	}, s.handleListSecrets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_delete_candidates",
		Description: "列出某平面可删除的 Dec 资产、secrets 与 bundle（plane=project|user|both）。删除前先用它拿候选。",
	}, s.handleListDeleteCandidates)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_delete",
		Description: "删除选中的 Dec 资产、secrets 或 bundle（需 confirmed=true）。plane=project|user，一次只作用一个平面，不支持 both。",
	}, s.handleDelete)
}

// Run 启动 stdio MCP Server。
func Run(ctx context.Context, cfg Config) error {
	ctx, stopWatchers := withExitWatchers(ctx)
	defer stopWatchers()

	cfg.ProjectRoot = resolveProjectRoot(cfg.ProjectRoot)
	if cfg.ProjectRoot == "" {
		return fmt.Errorf("无法确定项目根目录：请设置 --project-root 或 DEC_PROJECT_ROOT")
	}
	api, err := serviceapi.Connect(ctx, "mcp", fmt.Sprintf("mcp-%d", os.Getpid()), cfg.ClientVersion)
	if err != nil {
		return fmt.Errorf("连接 dec-server 失败: %w", err)
	}
	if api.VersionMismatch() {
		fmt.Fprintf(os.Stderr, "dec-mcp: 服务版本 %s 与客户端 %s 不一致，正在重启 dec-server…\n",
			api.ServerVersion(), api.ClientVersion())
		if err := api.RestartServer(ctx, serviceapi.RestartOptions{
			Reason:      "version-mismatch",
			ProjectRoot: cfg.ProjectRoot,
		}); err != nil {
			return fmt.Errorf("重启版本不匹配的 dec-server 失败: %w", err)
		}
	}
	defer api.Close()
	serviceapi.SetDefault(api)

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

type statusParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：project（项目平面）、user（用户平面，等价 dec --user）。留空默认 project；不支持 both。"`
}

func (s *Server) handleStatus(ctx context.Context, _ *mcp.CallToolRequest, in statusParams) (*mcp.CallToolResult, any, error) {
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return toolFail(err, nil)
	}
	ws := app.NewWorkspace(plane, s.projectRoot())
	overview, err := serviceapi.LoadWorkspaceOverview(ws, true)
	if err != nil {
		return toolFail(err, nil)
	}
	data := map[string]any{"plane": string(plane), "project": overview}
	if api, apiErr := serviceapi.Default(); apiErr == nil {
		if active, activeErr := api.GetActiveOperation(ctx, s.projectRoot()); activeErr == nil && active != nil && active.Active {
			data["active_operation"] = active
		}
	}
	return toolOK(data, nil)
}

type connectRepoParams struct {
	RepoURL string `json:"repo_url" jsonschema:"Dec Git 仓库 URL"`
}

func (s *Server) handleConnectRepo(ctx context.Context, _ *mcp.CallToolRequest, in connectRepoParams) (*mcp.CallToolResult, any, error) {
	reporter, logs := newCollector()
	result, err := serviceapi.ConnectRepo(in.RepoURL, reporter)
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
	prepared, err := serviceapi.PrepareProjectConfigInit(s.projectRoot(), reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	out := map[string]any{"init": prepared}
	if in.ApplyVaultProject {
		applied, applyErr := serviceapi.ApplyVaultProject(s.projectRoot(), reporter)
		if applyErr != nil {
			return toolFail(applyErr, logs())
		}
		out["vault_apply"] = applied
	}
	return toolOK(out, logs())
}

type listAssetsParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：project|user|both。留空默认 project。"`
}

func (s *Server) handleListAssets(ctx context.Context, _ *mcp.CallToolRequest, in listAssetsParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(_ context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.LoadWorkspaceAssetSelection(ws, reporter)
	})
}

type setAssetsParams struct {
	EnabledBundles []string `json:"enabled_bundles" jsonschema:"启用的 bundle 名称列表；传空数组表示全部取消"`
	Plane          string   `json:"plane,omitempty" jsonschema:"作用平面：project（写项目 enabled_bundles）、user（写用户 enabled_bundles）。留空默认 project；不支持 both。"`
}

func (s *Server) handleSetAssets(ctx context.Context, _ *mcp.CallToolRequest, in setAssetsParams) (*mcp.CallToolResult, any, error) {
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return toolFail(err, nil)
	}
	reporter, logs := newCollector()
	result, err := serviceapi.SaveWorkspaceEnabledBundles(app.NewWorkspace(plane, s.projectRoot()), in.EnabledBundles, reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}

type pullParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：project|user|both。留空默认 project。"`
}

func (s *Server) handlePull(ctx context.Context, _ *mcp.CallToolRequest, in pullParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.PullWorkspaceAssets(ctx, ws, reporter)
	})
}

type pushParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：project|user|both。留空默认 project。"`
}

func (s *Server) handlePush(ctx context.Context, _ *mcp.CallToolRequest, in pushParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.PushWorkspaceAssets(ctx, ws, reporter)
	})
}

type previewPushParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：project|user|both。留空默认 project。"`
}

func (s *Server) handlePreviewPush(ctx context.Context, _ *mcp.CallToolRequest, in previewPushParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.PreviewPushWorkspaceAssets(ctx, ws, reporter)
	})
}

type listSecretsParams struct {
	IncludeRemote *bool  `json:"include_remote,omitempty" jsonschema:"是否检查 Bitwarden 远端存在性（默认 true，可能触发 web unlock）"`
	Plane         string `json:"plane,omitempty" jsonschema:"作用平面：project|user|both。留空默认 project。"`
}

func (s *Server) handleListSecrets(ctx context.Context, _ *mcp.CallToolRequest, in listSecretsParams) (*mcp.CallToolResult, any, error) {
	includeRemote := true
	if in.IncludeRemote != nil {
		includeRemote = *in.IncludeRemote
	}
	return s.dispatchPlanes(ctx, in.Plane, func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.ListWorkspaceSecretsMetadata(ctx, ws, includeRemote, reporter)
	})
}

type listDeleteCandidatesParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：project|user|both。留空默认 project。"`
}

func (s *Server) handleListDeleteCandidates(ctx context.Context, _ *mcp.CallToolRequest, in listDeleteCandidatesParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.ListWorkspaceDeleteCandidates(ctx, ws, true, reporter)
	})
}

type deleteItemInput struct {
	Kind          string `json:"kind" jsonschema:"dec | secret | bundle"`
	Type          string `json:"type,omitempty"`
	Name          string `json:"name,omitempty"`
	Vault         string `json:"vault,omitempty"`
	SecretPath    string `json:"secret_path,omitempty" jsonschema:"secret：项目根相对落地路径，同时就是 Bitwarden Note 名"`
	SecretsBundle string `json:"secrets_bundle,omitempty" jsonschema:"secret：Bitwarden folder"`
	BundleName    string `json:"bundle_name,omitempty"`
}

type deleteParams struct {
	Items     []deleteItemInput `json:"items" jsonschema:"要删除的条目"`
	Confirmed bool              `json:"confirmed" jsonschema:"必须为 true 才会执行删除"`
	Plane     string            `json:"plane,omitempty" jsonschema:"作用平面：project|user。留空默认 project；不支持 both。候选项须来自同平面的 dec_list_delete_candidates。"`
}

func (s *Server) handleDelete(ctx context.Context, _ *mcp.CallToolRequest, in deleteParams) (*mcp.CallToolResult, any, error) {
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return toolFail(err, nil)
	}
	reporter, logs := newCollector()
	items := make([]app.DeleteSelectionItem, 0, len(in.Items))
	for _, item := range in.Items {
		items = append(items, app.DeleteSelectionItem{
			Kind:          app.DeleteItemKind(item.Kind),
			Type:          item.Type,
			Name:          item.Name,
			Vault:         item.Vault,
			SecretPath:    item.SecretPath,
			SecretsBundle: item.SecretsBundle,
			BundleName:    item.BundleName,
		})
	}
	result, err := serviceapi.DeleteProjectItems(ctx, app.DeleteProjectInput{
		ProjectRoot: s.projectRoot(),
		Plane:       plane,
		Items:       items,
		Confirmed:   in.Confirmed,
	}, reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}
