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
	"github.com/shichao402/Dec/internal/types"
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
		Description: "查看某平面的 Dec 状态（仓库连接、绑定项目、requires 与 public/private × global/local 四象限；plane=local|global）。",
	}, s.handleStatus)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_connect_repo",
		Description: "连接 Dec 资产 Git 仓库（全局配置，写入 ~/.dec/config.yaml）",
	}, s.handleConnectRepo)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_init_project",
		Description: "初始化当前项目并绑定家 P（需已连接仓库）",
	}, s.handleInitProject)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_assets",
		Description: "列出某平面启用的项目、直接 requires 与四象限成员（plane=local|global|both）。",
	}, s.handleListAssets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_set_assets",
		Description: "设置 P 选择。project 更新家 P 的直接 requires；user 写 enabled_projects。工具名保持兼容。",
	}, s.handleSetAssets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_pull",
		Description: "拉取并安装某平面的项目四象限资产与 private secrets（plane=local|global|both）。",
	}, s.handlePull)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_push",
		Description: "把某平面的本地改动推回远端（plane=local|global|both）：Dec 资产推 Git，secrets 推 Bitwarden。本仓库用 plane=local；本机凭据/SSH 用 plane=global；两边都改过用 both。",
	}, s.handlePush)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_preview_push",
		Description: "预览某平面 push 将涉及的 Dec 与 secrets 变更（plane=local|global|both，不写远端）。推之前先 preview 确认范围。",
	}, s.handlePreviewPush)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_secrets",
		Description: "列出某平面私密资产元数据（路径、本地/远端存在性；plane=local|global|both）。绝不返回 token/密钥/正文。",
	}, s.handleListSecrets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_delete_candidates",
		Description: "列出某平面可删除的 P 四象限资产与 private secrets；legacy bundle 仍以兼容节点出现。",
	}, s.handleListDeleteCandidates)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_delete",
		Description: "删除选中的 P 资产、private secrets 或 legacy bundle（需 confirmed=true）。",
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
	stdin := newIdleReader(os.Stdin)
	ctx, stopIdle := watchStdinIdle(ctx, stdin, stdinIdleTimeout)
	defer stopIdle()
	return mcpServer.Run(ctx, &mcp.IOTransport{
		Reader: stdin,
		Writer: nopWriteCloser{os.Stdout},
	})
}

func resolveProjectRoot(flagValue string) string {
	root := strings.TrimSpace(flagValue)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("DEC_PROJECT_ROOT"))
	}
	if isUnexpandedPlaceholder(root) {
		fmt.Fprintf(os.Stderr, "[dec:mcp] --project-root %q 未被宿主展开（Cursor Agent ACP 不会替换 ${workspaceFolder}），改用进程 cwd pid=%d\n",
			root, os.Getpid())
		root = ""
	}
	if root != "" {
		return root
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func isUnexpandedPlaceholder(s string) bool {
	return strings.Contains(s, "${")
}

func (s *Server) projectRoot() string {
	return s.cfg.ProjectRoot
}

type statusParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：local（本仓库）、global（本机，等价 dec --global）。留空默认 local；不支持 both。project/user 为旧别名。"`
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
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handleListAssets(ctx context.Context, _ *mcp.CallToolRequest, in listAssetsParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(_ context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.LoadWorkspaceAssetSelection(ws, reporter)
	})
}

type setAssetsParams struct {
	EnabledProjects []string `json:"enabled_projects,omitempty" jsonschema:"P 名称列表；project 中表示家 P 及直接 requires，user 中表示启用 P"`
	EnabledBundles  []string `json:"enabled_bundles,omitempty" jsonschema:"兼容字段；enabled_projects 未提供时使用"`
	Plane           string   `json:"plane,omitempty" jsonschema:"作用平面：local|global（旧名 project|user）。留空默认 local；不支持 both。"`
}

func (s *Server) handleSetAssets(ctx context.Context, _ *mcp.CallToolRequest, in setAssetsParams) (*mcp.CallToolResult, any, error) {
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return toolFail(err, nil)
	}
	reporter, logs := newCollector()
	names := in.EnabledProjects
	if names == nil {
		names = in.EnabledBundles
	}
	result, err := serviceapi.SaveWorkspaceProjects(app.NewWorkspace(plane, s.projectRoot()), names, reporter)
	if err != nil {
		return toolFail(err, logs())
	}
	return toolOK(result, logs())
}

type pullParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handlePull(ctx context.Context, _ *mcp.CallToolRequest, in pullParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.PullWorkspaceAssets(ctx, ws, reporter)
	})
}

type pushParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handlePush(ctx context.Context, _ *mcp.CallToolRequest, in pushParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.PushWorkspaceAssets(ctx, ws, reporter)
	})
}

type previewPushParams struct {
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handlePreviewPush(ctx context.Context, _ *mcp.CallToolRequest, in previewPushParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, func(ctx context.Context, ws app.Workspace, reporter app.Reporter) (any, error) {
		return serviceapi.PreviewPushWorkspaceAssets(ctx, ws, reporter)
	})
}

type listSecretsParams struct {
	IncludeRemote *bool  `json:"include_remote,omitempty" jsonschema:"是否检查 Bitwarden 远端存在性（默认 true，可能触发 web unlock）"`
	Plane         string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
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
	Plane string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
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
	ProjectName   string `json:"project_name,omitempty" jsonschema:"P 名；bundle_name 为兼容字段"`
	Visibility    string `json:"visibility,omitempty" jsonschema:"P 资产象限：public|private"`
	AssetPlane    string `json:"asset_plane,omitempty" jsonschema:"P 资产象限：user|project"`
}

type deleteParams struct {
	Items     []deleteItemInput `json:"items" jsonschema:"要删除的条目"`
	Confirmed bool              `json:"confirmed" jsonschema:"必须为 true 才会执行删除"`
	Plane     string            `json:"plane,omitempty" jsonschema:"作用平面：local|global（旧名 project|user）。留空默认 local；不支持 both。候选项须来自同平面的 dec_list_delete_candidates。"`
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
			ProjectName:   item.ProjectName,
			Visibility:    types.AssetVisibility(item.Visibility),
			AssetPlane:    types.AssetPlane(item.AssetPlane),
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
