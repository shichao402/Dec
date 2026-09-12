// Package mcp 提供 Dec 的 MCP（stdio）接口层。
//
// 架构约束：dec-mcp 是 Console 的 stdio 适配器；业务与 Bitwarden session
// 由目标 dec-server 持有，当前连接与 SSH 隧道由唯一 Console 实例持有（ADR 0025）。
package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/types"
)

// Config 配置 Dec MCP Server。
type Config struct {
	ClientVersion string
	Gateway       Gateway
}

// Server 持有 MCP 服务状态。
type Server struct {
	cfg Config
	gw  Gateway
}

// New 创建 Dec MCP Server。
func New(cfg Config) *Server {
	return &Server{cfg: cfg, gw: cfg.Gateway}
}

func (s *Server) gateway() Gateway {
	if s.gw != nil {
		return s.gw
	}
	return s.cfg.Gateway
}

// Register 向 MCP server 注册全部 Dec tools。
func (s *Server) Register(mcpServer *mcp.Server) {
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_console_status",
		Description: "查看 Console 网关与当前连接（是否已连、是否已解锁、当前设备）。操作作用在 Console 当前目标上。",
	}, s.handleConsoleStatus)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_connections",
		Description: "列出 Console 已保存的连接（本机 / SSH / 远端）。不返回密码。",
	}, s.handleListConnections)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_connect",
		Description: "切换 Console 当前连接。传 id 使用已保存连接；或 kind=local 连本机。不接受 SSH 密码，使用已存凭据 / ssh-agent。",
	}, s.handleConnect)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_managed_projects",
		Description: "列出当前目标设备上已登记的受管项目（路径、标签、是否已初始化）。",
	}, s.handleListManagedProjects)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_managed_devices",
		Description: "列出当前目标上已登记的受管 SSH 设备（只读）。",
	}, s.handleListManagedDevices)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_register_managed_project",
		Description: "把本地目录登记为当前目标上的受管项目。",
	}, s.handleRegisterManagedProject)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_create_local_asset",
		Description: "在指定平面创建本地资产（skill/rule/mcp/command 或私密类型）。",
	}, s.handleCreateLocalAsset)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_status",
		Description: "查看某平面的 Dec 状态（仓库连接、绑定项目、requires 与 public/private × global/local 四象限；plane=local|global）。",
	}, s.handleStatus)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_connect_repo",
		Description: "连接 Dec 资产 Git 仓库（全局配置，写入目标设备 ~/.dec/config.yaml）",
	}, s.handleConnectRepo)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_init_project",
		Description: "初始化指定项目并绑定家项目（需已连接仓库）",
	}, s.handleInitProject)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_list_assets",
		Description: "列出某平面启用的项目、直接 requires 与四象限成员（plane=local|global|both）。",
	}, s.handleListAssets)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_set_assets",
		Description: "设置项目选择。project 更新家项目的直接 requires；user 写 enabled_projects。工具名保持兼容。",
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
		Description: "列出某平面可删除的项目四象限资产与 private secrets；legacy bundle 仍以兼容节点出现。",
	}, s.handleListDeleteCandidates)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_delete",
		Description: "删除选中的项目资产、private secrets 或 legacy bundle（需 confirmed=true）。",
	}, s.handleDelete)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "dec_provision_remote",
		Description: "通过系统 SSH 向 Linux/macOS 远端置备 Dec、配置固定 loopback 监听并登记设备。首次置备是远程代码执行，必须 confirmed=true。",
	}, s.handleProvisionRemote)
}

// Run 启动 stdio MCP Server。
func Run(ctx context.Context, cfg Config) error {
	ctx, stopWatchers := withExitWatchers(ctx)
	defer stopWatchers()

	if cfg.Gateway == nil {
		clientID := fmt.Sprintf("mcp-%d", os.Getpid())
		gw, err := WaitForGateway(ctx, clientID)
		if err != nil {
			return err
		}
		cfg.Gateway = gw
	}

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

func (s *Server) workspace(plane app.WorkspacePlane, projectRoot string) (app.Workspace, error) {
	root := strings.TrimSpace(projectRoot)
	if isUnexpandedPlaceholder(root) {
		root = ""
	}
	if plane == app.WorkspaceGlobal {
		return app.NewWorkspace(plane, ""), nil
	}
	if root == "" {
		return app.Workspace{}, fmt.Errorf("plane=local 需要 project_root。请先用 dec_list_managed_projects 选定受管项目，或传入绝对路径")
	}
	return app.NewWorkspace(plane, root), nil
}

func isUnexpandedPlaceholder(s string) bool {
	return strings.Contains(s, "${")
}

func (s *Server) fromRPC(res *RPCResult, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return toolFail(err, nil)
	}
	if res == nil {
		return toolFail(fmt.Errorf("Console 网关无响应"), nil)
	}
	if !res.OK && res.Error != "" {
		return toolFail(fmt.Errorf("%s", res.Error), res.logs())
	}
	return toolOK(res.data(), res.logs())
}

func (s *Server) invokeWS(ctx context.Context, method string, ws app.Workspace, payload any) (*mcp.CallToolResult, any, error) {
	gw := s.gateway()
	if gw == nil {
		return toolFail(fmt.Errorf("尚未连接 Dec Console"), nil)
	}
	return s.fromRPC(gw.Invoke(ctx, method, ws.Root, string(ws.EffectivePlane()), payload))
}

func (s *Server) runWS(ctx context.Context, operation string, ws app.Workspace, payload any) (*mcp.CallToolResult, any, error) {
	gw := s.gateway()
	if gw == nil {
		return toolFail(fmt.Errorf("尚未连接 Dec Console"), nil)
	}
	return s.fromRPC(gw.Run(ctx, operation, ws.Root, string(ws.EffectivePlane()), payload))
}

type emptyParams struct{}

func (s *Server) handleConsoleStatus(ctx context.Context, _ *mcp.CallToolRequest, _ emptyParams) (*mcp.CallToolResult, any, error) {
	gw := s.gateway()
	if gw == nil {
		return toolFail(fmt.Errorf("尚未连接 Dec Console"), nil)
	}
	hello, err := gw.Hello(ctx)
	if err != nil {
		return toolFail(err, nil)
	}
	return toolOK(hello, nil)
}

func (s *Server) handleListConnections(ctx context.Context, _ *mcp.CallToolRequest, _ emptyParams) (*mcp.CallToolResult, any, error) {
	gw := s.gateway()
	if gw == nil {
		return toolFail(fmt.Errorf("尚未连接 Dec Console"), nil)
	}
	out, err := gw.Connections(ctx)
	if err != nil {
		return toolFail(err, nil)
	}
	return toolOK(out, nil)
}

type connectParams struct {
	ID            string `json:"id,omitempty" jsonschema:"已保存连接 id；优先于 kind"`
	Kind          string `json:"kind,omitempty" jsonschema:"local | ssh | remote；无 id 时使用"`
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"`
	SSHHost       string `json:"ssh_host,omitempty"`
	SSHUser       string `json:"ssh_user,omitempty"`
	TLS           bool   `json:"tls,omitempty"`
	TLSServerName string `json:"tls_server_name,omitempty"`
}

func (s *Server) handleConnect(ctx context.Context, _ *mcp.CallToolRequest, in connectParams) (*mcp.CallToolResult, any, error) {
	gw := s.gateway()
	if gw == nil {
		return toolFail(fmt.Errorf("尚未连接 Dec Console"), nil)
	}
	req := map[string]any{
		"id":              in.ID,
		"kind":            in.Kind,
		"host":            in.Host,
		"port":            in.Port,
		"ssh_host":        in.SSHHost,
		"ssh_user":        in.SSHUser,
		"tls":             in.TLS,
		"tls_server_name": in.TLSServerName,
	}
	hello, err := gw.Connect(ctx, req)
	if err != nil {
		return toolFail(err, nil)
	}
	return toolOK(hello, nil)
}

func (s *Server) handleListManagedProjects(ctx context.Context, _ *mcp.CallToolRequest, _ emptyParams) (*mcp.CallToolResult, any, error) {
	return s.invokeWS(ctx, "list_managed_projects", app.NewWorkspace(app.WorkspaceGlobal, ""), nil)
}

func (s *Server) handleListManagedDevices(ctx context.Context, _ *mcp.CallToolRequest, _ emptyParams) (*mcp.CallToolResult, any, error) {
	return s.invokeWS(ctx, "list_managed_devices", app.NewWorkspace(app.WorkspaceGlobal, ""), nil)
}

type registerManagedProjectParams struct {
	Root  string `json:"root" jsonschema:"项目根目录绝对路径"`
	Label string `json:"label,omitempty" jsonschema:"显示名，可空"`
}

func (s *Server) handleRegisterManagedProject(ctx context.Context, _ *mcp.CallToolRequest, in registerManagedProjectParams) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Root) == "" {
		return toolFail(fmt.Errorf("root 不能为空"), nil)
	}
	return s.invokeWS(ctx, "register_managed_project", app.NewWorkspace(app.WorkspaceGlobal, ""), map[string]any{
		"Root":  in.Root,
		"Label": in.Label,
	})
}

type createLocalAssetParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane=local 必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global。留空默认 local；不支持 both。"`
	Project     string `json:"project" jsonschema:"vault 项目名"`
	Kind        string `json:"kind" jsonschema:"skill|rule|mcp|command 或私密类型 id"`
	Name        string `json:"name" jsonschema:"资产名"`
	Visibility  string `json:"visibility,omitempty" jsonschema:"public|private，默认 private"`
	AssetPlane  string `json:"asset_plane,omitempty" jsonschema:"象限平面 user|project；global 平面会强制 global"`
}

func (s *Server) handleCreateLocalAsset(ctx context.Context, _ *mcp.CallToolRequest, in createLocalAssetParams) (*mcp.CallToolResult, any, error) {
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return toolFail(err, nil)
	}
	ws, err := s.workspace(plane, in.ProjectRoot)
	if err != nil {
		return toolFail(err, nil)
	}
	return s.invokeWS(ctx, "create_local_asset", ws, map[string]any{
		"Project":    in.Project,
		"Kind":       in.Kind,
		"Name":       in.Name,
		"Visibility": in.Visibility,
		"Plane":      in.AssetPlane,
	})
}

type statusParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane=local 必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local（本仓库）、global（本机）。留空默认 local；不支持 both。project/user 为旧别名。"`
}

func (s *Server) handleStatus(ctx context.Context, _ *mcp.CallToolRequest, in statusParams) (*mcp.CallToolResult, any, error) {
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return toolFail(err, nil)
	}
	ws, err := s.workspace(plane, in.ProjectRoot)
	if err != nil {
		return toolFail(err, nil)
	}
	tool, data, callErr := s.invokeWS(ctx, "load_project_overview", ws, map[string]any{"IncludeVaultBundles": true})
	if callErr != nil {
		return tool, data, callErr
	}
	resp, ok := data.(toolResponse)
	if !ok || !resp.OK {
		return tool, data, callErr
	}
	out := map[string]any{"plane": string(plane), "project": resp.Data}
	if gw := s.gateway(); gw != nil {
		if active, activeErr := gw.ActiveOperation(ctx, ws.Root); activeErr == nil && active != nil {
			out["active_operation"] = active
		}
	}
	return toolOK(out, resp.Logs)
}

type connectRepoParams struct {
	RepoURL string `json:"repo_url" jsonschema:"Dec Git 仓库 URL"`
}

func (s *Server) handleConnectRepo(ctx context.Context, _ *mcp.CallToolRequest, in connectRepoParams) (*mcp.CallToolResult, any, error) {
	return s.invokeWS(ctx, "connect_repo", app.NewWorkspace(app.WorkspaceGlobal, ""), map[string]any{
		"RepoURL": in.RepoURL,
	})
}

type initProjectParams struct {
	ProjectRoot       string `json:"project_root" jsonschema:"要初始化的项目根目录"`
	ApplyVaultProject bool   `json:"apply_vault_project,omitempty" jsonschema:"若 vault 存在同名 projects/<name>.yaml 则自动应用其 bundle"`
}

func (s *Server) handleInitProject(ctx context.Context, _ *mcp.CallToolRequest, in initProjectParams) (*mcp.CallToolResult, any, error) {
	ws, err := s.workspace(app.WorkspaceLocal, in.ProjectRoot)
	if err != nil {
		return toolFail(err, nil)
	}
	gw := s.gateway()
	if gw == nil {
		return toolFail(fmt.Errorf("尚未连接 Dec Console"), nil)
	}
	prepared, prepErr := gw.Invoke(ctx, "prepare_project_config_init", ws.Root, "local", nil)
	if prepErr != nil || (prepared != nil && !prepared.OK && prepared.Error != "") {
		return s.fromRPC(prepared, prepErr)
	}
	out := map[string]any{"init": prepared.data()}
	logs := prepared.logs()
	if in.ApplyVaultProject {
		applied, applyErr := s.gateway().Invoke(ctx, "apply_vault_project", ws.Root, "local", nil)
		if applyErr != nil || (applied != nil && !applied.OK && applied.Error != "") {
			return s.fromRPC(applied, applyErr)
		}
		out["vault_apply"] = applied.data()
		logs = append(logs, applied.logs()...)
	}
	return toolOK(out, logs)
}

type listAssetsParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane=local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handleListAssets(ctx context.Context, _ *mcp.CallToolRequest, in listAssetsParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, in.ProjectRoot, func(ctx context.Context, ws app.Workspace, _ app.Reporter) (any, error) {
		res, err := s.gateway().Invoke(ctx, "load_asset_selection", ws.Root, string(ws.EffectivePlane()), nil)
		if err != nil {
			return nil, err
		}
		if res != nil && !res.OK && res.Error != "" {
			return nil, fmt.Errorf("%s", res.Error)
		}
		return res.data(), nil
	})
}

type setAssetsParams struct {
	ProjectRoot     string   `json:"project_root,omitempty" jsonschema:"项目根；plane=local 必填"`
	EnabledProjects []string `json:"enabled_projects,omitempty" jsonschema:"项目名称列表；project 中表示家项目及直接 requires，user 中表示启用项目"`
	EnabledBundles  []string `json:"enabled_bundles,omitempty" jsonschema:"兼容字段；enabled_projects 未提供时使用"`
	Plane           string   `json:"plane,omitempty" jsonschema:"作用平面：local|global（旧名 project|user）。留空默认 local；不支持 both。"`
}

func (s *Server) handleSetAssets(ctx context.Context, _ *mcp.CallToolRequest, in setAssetsParams) (*mcp.CallToolResult, any, error) {
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return toolFail(err, nil)
	}
	ws, err := s.workspace(plane, in.ProjectRoot)
	if err != nil {
		return toolFail(err, nil)
	}
	names := in.EnabledProjects
	if names == nil {
		names = in.EnabledBundles
	}
	return s.invokeWS(ctx, "save_enabled_bundles", ws, map[string]any{
		"EnabledProjects": names,
		"EnabledBundles":  names,
	})
}

type pullParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane=local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handlePull(ctx context.Context, _ *mcp.CallToolRequest, in pullParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, in.ProjectRoot, func(ctx context.Context, ws app.Workspace, _ app.Reporter) (any, error) {
		res, err := s.gateway().Run(ctx, "pull", ws.Root, string(ws.EffectivePlane()), nil)
		if err != nil {
			return nil, err
		}
		if res != nil && !res.OK && res.Error != "" {
			return nil, fmt.Errorf("%s", res.Error)
		}
		return res.data(), nil
	})
}

type pushParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane=local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handlePush(ctx context.Context, _ *mcp.CallToolRequest, in pushParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, in.ProjectRoot, func(ctx context.Context, ws app.Workspace, _ app.Reporter) (any, error) {
		res, err := s.gateway().Run(ctx, "push", ws.Root, string(ws.EffectivePlane()), nil)
		if err != nil {
			return nil, err
		}
		if res != nil && !res.OK && res.Error != "" {
			return nil, fmt.Errorf("%s", res.Error)
		}
		return res.data(), nil
	})
}

type previewPushParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane=local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handlePreviewPush(ctx context.Context, _ *mcp.CallToolRequest, in previewPushParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, in.ProjectRoot, func(ctx context.Context, ws app.Workspace, _ app.Reporter) (any, error) {
		res, err := s.gateway().Run(ctx, "preview_push", ws.Root, string(ws.EffectivePlane()), nil)
		if err != nil {
			return nil, err
		}
		if res != nil && !res.OK && res.Error != "" {
			return nil, fmt.Errorf("%s", res.Error)
		}
		return res.data(), nil
	})
}

type listSecretsParams struct {
	ProjectRoot   string `json:"project_root,omitempty" jsonschema:"项目根；plane=local 或 both 时必填"`
	IncludeRemote *bool  `json:"include_remote,omitempty" jsonschema:"是否检查 Bitwarden 远端存在性（默认 true，可能要求在 Console 解锁）"`
	Plane         string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handleListSecrets(ctx context.Context, _ *mcp.CallToolRequest, in listSecretsParams) (*mcp.CallToolResult, any, error) {
	includeRemote := true
	if in.IncludeRemote != nil {
		includeRemote = *in.IncludeRemote
	}
	return s.dispatchPlanes(ctx, in.Plane, in.ProjectRoot, func(ctx context.Context, ws app.Workspace, _ app.Reporter) (any, error) {
		res, err := s.gateway().Invoke(ctx, "list_secrets", ws.Root, string(ws.EffectivePlane()), map[string]any{
			"IncludeRemote": includeRemote,
		})
		if err != nil {
			return nil, err
		}
		if res != nil && !res.OK && res.Error != "" {
			return nil, fmt.Errorf("%s", res.Error)
		}
		return res.data(), nil
	})
}

type listDeleteCandidatesParams struct {
	ProjectRoot string `json:"project_root,omitempty" jsonschema:"项目根；plane=local 或 both 时必填"`
	Plane       string `json:"plane,omitempty" jsonschema:"作用平面：local|global|both（旧名 project|user）。留空默认 local。"`
}

func (s *Server) handleListDeleteCandidates(ctx context.Context, _ *mcp.CallToolRequest, in listDeleteCandidatesParams) (*mcp.CallToolResult, any, error) {
	return s.dispatchPlanes(ctx, in.Plane, in.ProjectRoot, func(ctx context.Context, ws app.Workspace, _ app.Reporter) (any, error) {
		res, err := s.gateway().Invoke(ctx, "list_delete_candidates", ws.Root, string(ws.EffectivePlane()), map[string]any{
			"IncludeRemote": true,
		})
		if err != nil {
			return nil, err
		}
		if res != nil && !res.OK && res.Error != "" {
			return nil, fmt.Errorf("%s", res.Error)
		}
		return res.data(), nil
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
	ProjectName   string `json:"project_name,omitempty" jsonschema:"项目名；bundle_name 为兼容字段"`
	Visibility    string `json:"visibility,omitempty" jsonschema:"项目资产象限：public|private"`
	AssetPlane    string `json:"asset_plane,omitempty" jsonschema:"项目资产象限：user|project"`
}

type deleteParams struct {
	ProjectRoot string            `json:"project_root,omitempty" jsonschema:"项目根；plane=local 必填"`
	Items       []deleteItemInput `json:"items" jsonschema:"要删除的条目"`
	Confirmed   bool              `json:"confirmed" jsonschema:"必须为 true 才会执行删除"`
	Plane       string            `json:"plane,omitempty" jsonschema:"作用平面：local|global（旧名 project|user）。留空默认 local；不支持 both。候选项须来自同平面的 dec_list_delete_candidates。"`
}

func (s *Server) handleDelete(ctx context.Context, _ *mcp.CallToolRequest, in deleteParams) (*mcp.CallToolResult, any, error) {
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return toolFail(err, nil)
	}
	ws, err := s.workspace(plane, in.ProjectRoot)
	if err != nil {
		return toolFail(err, nil)
	}
	items := make([]map[string]any, 0, len(in.Items))
	for _, item := range in.Items {
		items = append(items, map[string]any{
			"Kind":          item.Kind,
			"Type":          item.Type,
			"Name":          item.Name,
			"Vault":         item.Vault,
			"SecretPath":    item.SecretPath,
			"SecretsBundle": item.SecretsBundle,
			"BundleName":    item.BundleName,
			"ProjectName":   item.ProjectName,
			"Visibility":    types.AssetVisibility(item.Visibility),
			"AssetPlane":    types.AssetPlane(item.AssetPlane),
		})
	}
	return s.runWS(ctx, "delete", ws, map[string]any{
		"Items":     items,
		"Confirmed": in.Confirmed,
	})
}

type provisionRemoteParams struct {
	Alias     string   `json:"alias" jsonschema:"受管设备别名；留空时使用 ssh_target"`
	SSHTarget string   `json:"ssh_target" jsonschema:"系统 ssh 目标：Host 别名、主机名、user@host，或 host:36000；凭据由 ~/.ssh/config / ssh-agent 提供"`
	Tags      []string `json:"tags,omitempty" jsonschema:"设备标签，仅用于登记分类"`
	Branch    string   `json:"branch,omitempty" jsonschema:"安装分支，留空默认 main"`
	Confirmed bool     `json:"confirmed" jsonschema:"首次置备会远程执行安装脚本，必须显式设为 true"`
}

func (s *Server) handleProvisionRemote(ctx context.Context, _ *mcp.CallToolRequest, in provisionRemoteParams) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.SSHTarget) == "" {
		return toolFail(fmt.Errorf("ssh_target 不能为空"), nil)
	}
	root := app.DeviceOperationKey(app.RemoteTarget{Alias: strings.TrimSpace(in.SSHTarget)})
	return s.runWS(ctx, "provision_remote_host", app.NewWorkspace(app.WorkspaceGlobal, root), map[string]any{
		"Alias":     in.Alias,
		"Target":    map[string]any{"Alias": strings.TrimSpace(in.SSHTarget)},
		"Tags":      in.Tags,
		"Branch":    in.Branch,
		"Confirmed": in.Confirmed,
	})
}
