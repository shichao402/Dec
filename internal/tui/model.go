package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/diag"
	"github.com/shichao402/Dec/internal/editor"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/serviceapi"
	"github.com/shichao402/Dec/internal/update"
)

type overviewLoadedMsg struct {
	overview *app.ProjectOverview
	err      error
	loadGen  uint64
}

// shellRefreshKickMsg 把 refresh 从 Init 挪进 Update，确保 beginParts 写入进正式 model。
type shellRefreshKickMsg struct{}

type vaultInferenceLoadedMsg struct {
	vaultInference *app.VaultProjectInference
	err            error
}

type overviewVaultEnrichedMsg struct {
	bundles              []app.BundleOverview
	availableBundleCount int
	err                  error
}

type vaultProjectAppliedMsg struct {
	result  *app.VaultProjectAutoApplyResult
	err     error
	loadGen uint64
}

type assetsLoadedMsg struct {
	state   *app.AssetSelectionState
	err     error
	loadGen uint64
}

type assetsSavedMsg struct {
	result *app.SaveBundleSelectionResult
	err    error
}

type settingsLoadedMsg struct {
	state   *app.GlobalSettingsState
	err     error
	loadGen uint64
}

type settingsSavedMsg struct {
	result *app.SaveGlobalSettingsResult
	err    error
}

type repoBootstrapEventMsg struct{ event app.OperationEvent }
type repoBootstrapPreparedMsg struct {
	result *app.PrepareRepoGCMBootstrapResult
	err    error
}
type repoBootstrapAppliedMsg struct {
	result *app.ApplyRepoGCMBootstrapResult
	err    error
}

type builtinAssetsEnsuredMsg struct {
	warnings []string
	loadGen  uint64
}

type projectSettingsLoadedMsg struct {
	state   *app.ProjectSettingsState
	err     error
	loadGen uint64
}

type projectSettingsSavedMsg struct {
	result *app.SaveProjectSettingsResult
	err    error
}

type projectConfigInitializedMsg struct {
	result  *app.ConfigInitPreparation
	err     error
	loadGen uint64
}

type localProjectEnsuredMsg struct {
	result  *app.ConfigInitPreparation
	err     error
	loadGen uint64
}

type projectVarsLoadedMsg struct {
	view    *app.ProjectVarsView
	err     error
	loadGen uint64
	solo    bool // true: 独立 vars 重载；false: shell refresh 分片
}

type projectVarsEditedMsg struct {
	err error
}

type globalVarsLoadedMsg struct {
	view    *app.GlobalVarsView
	err     error
	loadGen uint64
}

type globalVarsEditedMsg struct {
	err error
}

type pushPreviewLoadedMsg struct {
	preview *app.PushProjectAssetsPreview
	err     error
	loadGen uint64
}

// focusContext 描述 TUI 的空间导航焦点层级。
// 左右键（h/l）在该层级间进入/退出；上下键（j/k）在同级内移动。
//   - focusSidebar：侧栏导航（Home/Assets/...），j/k 切换页，l 进入内容区
//   - focusContent：当前页内容列表，j/k 移动光标，h 退回侧栏，l 进入更深（如展开 bundle）
//   - focusBundleExpanded：bundle 已展开，j/k 在成员间移动，h 折叠并退回内容区
type focusContext string

const (
	focusSidebar        focusContext = "sidebar"
	focusContent        focusContext = "content"
	focusBundleExpanded focusContext = "bundleExpanded"
)

// Settings 列表固定行（IDE / bundles 之前）。
const (
	settingsRowRepo          = 0
	settingsRowIdleTimeout   = 1
	settingsRowRestartServer = 2
	settingsRowGlobalVars    = 3
	settingsFixedRowCount    = 4
)

type runEventMsg struct {
	event app.OperationEvent
}

type runCompletedMsg struct {
	result     *app.PullProjectAssetsResult
	pushResult *app.PushProjectAssetsResult
	err        error
}

type activeOperationPolledMsg struct {
	active      bool
	operationID string
	operation   string
	facade      string
	err         error
}

type observedOperationEventMsg struct {
	event app.OperationEvent
}

type observedOperationDoneMsg struct {
	err error
}

type removeEventMsg struct {
	event app.OperationEvent
}

type removeCompletedMsg struct {
	result *app.RemoveBundleResult
	err    error
}

type updateCheckedMsg struct {
	result *update.CheckResult
	err    error
}

type updateDoneMsg struct {
	targetVersion string
	err           error
}

var runPullOperation = func(ctx context.Context, projectRoot string, reporter app.Reporter) (*app.PullProjectAssetsResult, error) {
	return serviceapi.PullProjectAssets(ctx, projectRoot, reporter)
}

var runPushOperation = func(ctx context.Context, workspace app.Workspace, reporter app.Reporter) (*app.PushProjectAssetsResult, error) {
	return serviceapi.PushWorkspaceAssets(ctx, workspace, reporter)
}

var runRemoveOperation = func(input app.RemoveBundleInput, reporter app.Reporter) (*app.RemoveBundleResult, error) {
	return serviceapi.RemoveBundle(context.Background(), input, reporter)
}

var previewPushOperation = func(workspace app.Workspace) (*app.PushProjectAssetsPreview, error) {
	return serviceapi.PreviewPushWorkspaceAssets(context.Background(), workspace, nil)
}

var loadGlobalSettingsOperation = func(reporter app.Reporter) (*app.GlobalSettingsState, error) {
	return serviceapi.LoadGlobalSettings(reporter)
}

var saveGlobalSettingsOperation = func(input app.SaveGlobalSettingsInput, reporter app.Reporter) (*app.SaveGlobalSettingsResult, error) {
	return serviceapi.SaveGlobalSettings(input, reporter)
}

var prepareRepoGCMBootstrapOperation = func(ctx context.Context, repoURL string, reporter app.Reporter) (*app.PrepareRepoGCMBootstrapResult, error) {
	return serviceapi.PrepareRepoGCMBootstrap(ctx, repoURL, reporter)
}

var applyRepoGCMBootstrapOperation = func(ctx context.Context, input app.ApplyRepoGCMBootstrapInput, reporter app.Reporter) (*app.ApplyRepoGCMBootstrapResult, error) {
	return serviceapi.ApplyRepoGCMBootstrap(ctx, input, reporter)
}

var ensureBuiltinIDEAssetsOperation = func(ideNames []string, reporter app.Reporter) []string {
	warnings, err := serviceapi.EnsureBuiltinIDEAssets(ideNames, reporter)
	if err != nil {
		return []string{err.Error()}
	}
	return warnings
}

var loadProjectSettingsOperation = func(projectRoot string, reporter app.Reporter) (*app.ProjectSettingsState, error) {
	return serviceapi.LoadProjectSettings(projectRoot, reporter)
}

var saveProjectSettingsOperation = func(input app.SaveProjectSettingsInput, reporter app.Reporter) (*app.SaveProjectSettingsResult, error) {
	return serviceapi.SaveProjectSettings(input, reporter)
}

var prepareProjectConfigInitOperation = func(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
	return serviceapi.PrepareProjectConfigInit(projectRoot, reporter)
}

var ensureLocalProjectConfigOperation = func(projectRoot string, reporter app.Reporter) (*app.ConfigInitPreparation, error) {
	return serviceapi.EnsureLocalProjectConfig(projectRoot, reporter)
}

var inferVaultProjectOperation = func(projectRoot string, reporter app.Reporter) (*app.VaultProjectInference, error) {
	return serviceapi.InferVaultProject(projectRoot, reporter)
}

var applyVaultProjectOperation = func(projectRoot string, reporter app.Reporter) (*app.VaultProjectAutoApplyResult, error) {
	return serviceapi.ApplyVaultProject(projectRoot, reporter)
}

var loadProjectVarsViewOperation = func(projectRoot string) (*app.ProjectVarsView, error) {
	return serviceapi.LoadProjectVarsView(projectRoot)
}

var ensureProjectVarsFileOperation = func(projectRoot string) (*app.EnsureProjectVarsFileResult, error) {
	return serviceapi.EnsureProjectVarsFile(projectRoot)
}

var loadGlobalVarsViewOperation = func() (*app.GlobalVarsView, error) {
	return serviceapi.LoadGlobalVarsView()
}

var ensureGlobalVarsFileOperation = func() (*app.EnsureGlobalVarsFileResult, error) {
	return serviceapi.EnsureGlobalVarsFile()
}

var updateCheckOperation = func(currentVersion string) (*update.CheckResult, error) {
	return update.Check(currentVersion)
}

var updateDoUpdateOperation = func(currentVersion, latestVersion string) error {
	return update.DoUpdate(currentVersion, latestVersion)
}

var updateNetworkHelp = func() string {
	return update.NetworkHelp()
}

type model struct {
	projectRoot    string
	plane          app.WorkspacePlane
	currentVersion string
	pages          []string
	pageIndex      int
	remoteOpen     bool
	width          int
	height         int
	overview       *app.ProjectOverview
	overviewErr    error
	assets         *app.AssetSelectionState
	assetsErr      error
	settings       *app.GlobalSettingsState
	settingsErr    error
	logs           []string
	assetTree      TreeList
	// bundleSelection 是 TUI 内对 ProjectConfig.EnabledBundles 的镜像。
	// 进入 Bundles 页后：从 assets.Bundles[i].Enabled==true 初始化；保存时传给 SaveEnabledBundles。
	bundleSelection  []string
	assetFilter      string
	assetFilterInput bool
	assetsDirty      bool
	savingAssets     bool
	// "bundle" 表示只显示 bundle 节点（以及它们展开的成员）；其他值只影响单资产行的过滤。
	settingsCursor              int
	settingsDirty               bool
	savingSettings              bool
	settingsRepoInput           string
	settingsRepoEditing         bool
	settingsIdleTimeoutInput    string
	settingsIdleTimeoutEditing  bool
	settingsSelectedIDEs        []string
	repoBootstrapStage          string // "", "confirm", "loading", "select", "applying"
	repoBootstrapSource         string // "settings" | "run"：决定成功后重试 Settings 保存还是 Run pull
	repoBootstrapHost           string
	repoBootstrapError          string
	repoBootstrapCandidates     []app.RepoGCMCandidate
	repoBootstrapCursor         int
	repoBootstrapStream         chan tea.Msg
	repoBootstrapCancel         context.CancelFunc
	projectSettings             *app.ProjectSettingsState
	projectSettingsErr          error
	projectSettingsCursor       int
	projectSettingsDirty        bool
	savingProjectSettings       bool
	projectSettingsOverride     bool
	projectSettingsSelectedIDEs []string
	lastInitResult              *app.ConfigInitPreparation
	lastInitErr                 error
	projectVars                 *app.ProjectVarsView
	projectVarsErr              error
	lastEditErr                 error
	runningPull                 bool
	runProgress                 *app.Progress
	runEvents                   []string
	runPinLine                  string
	runShowHelp                 bool
	runResult                   *app.PullProjectAssetsResult
	pushResult                  *app.PushProjectAssetsResult
	runErr                      error
	runStream                   <-chan tea.Msg
	runCtx                      context.Context
	runCancel                   context.CancelFunc
	runMode                     string // "pull" | "push" | "remove" | "update"
	observedOperationID         string
	observedOperationFacade     string
	observedStream              <-chan tea.Msg
	removeStage                 string // "", "select", "confirm", "running"
	removeCursor                int
	removeFilter                string
	removeFilterInput           bool
	removeTarget                *app.AssetBundleOption
	runningRemove               bool
	removeResult                *app.RemoveBundleResult
	removeErr                   error
	pushStage                   string // "", "loading", "summary", "confirm", "running"
	pushPreview                 *app.PushProjectAssetsPreview
	pushPreviewErr              error
	pushPreviewLoad             asyncLoad
	updateStage                 string // "", "checking", "result", "confirm", "running", "done"
	updateResult                *update.CheckResult
	updateErr                   error
	updateDoneVersion           string
	updatingBinary              bool
	deleteCandidates            []app.DeleteCandidate
	deleteTree                  TreeList
	deleteFilter                string
	deleteFilterInput           bool
	deleteStage                 string // "", "list", "summary", "confirm", "typed", "running"
	deleteCandidatesLoaded      bool
	deleteLoad                  asyncLoad // 跨页飞行的候选列表加载（切页不取消）
	deleteLoadErr               error
	deleteIncludeRemote         bool
	runningDelete               bool
	deleteResult                *app.DeleteProjectResult
	deleteErr                   error
	deleteTypedInput            string
	deleteTypedSpec             app.DeleteTypedConfirmSpec
	remoteNoteEdit              *app.RemoteNoteEditSession
	remoteSSHEdit               *app.RemoteSSHHostsEditSession
	remoteRegisterSess          *app.RemoteRegisterSession
	remoteRegisterPending       bool       // n 登记：Esc 可退出等待，迟到结果只记日志
	shellRefresh                asyncBatch // overview/assets/settings/projectSettings/projectVars
	projectVarsLoad             asyncLoad  // 独立重载 .dec/vars.yaml
	globalVarsLoad              asyncLoad  // 独立重载 ~/.dec/local/vars.yaml
	builtinAssetsLoad           asyncLoad  // 同步内置 IDE assets
	localProjectLoad            asyncLoad  // 生成本地 project 配置
	vaultApplyLoad              asyncLoad  // 应用推断的 vault project
	projectInitLoad             asyncLoad  // Project 页扫描仓库
	// configInitMode 为 true 时表示由 dec config init 拉起：聚焦 Assets/bundle 视图，保存后退出。
	configInitMode bool
	// vaultInference Home 页待确认的 vault project 推断（来自目录名匹配）。
	vaultInference *app.VaultProjectInference
	// vaultInferenceDismissed 用户本次会话内已拒绝推断，刷新前不再提示。
	vaultInferenceDismissed bool
	// localProjectInitConfirm Home 页等待用户确认是否在当前目录创建 .dec/config.yaml。
	// 未显式按 y 前绝不落盘，避免误开目录时污染该目录。
	localProjectInitConfirm bool
	// vaultAutoApplyNotice Home 页展示 vault 应用成功提示（仅本次会话内最近一次）。
	vaultAutoApplyNotice string
	// focus 是当前键盘交互上下文（侧栏 / 内容 / bundle 成员）。
	focus focusContext
	// addSecretStage 是 Project/Remote 页「登记新 secret」的阶段；空串表示流程未开启。
	addSecretStage         string
	addSecretPathInput     string
	addSecretContentPath   string // Remote：显式本地路径
	addSecretSourceMode    string // Remote：Processor 声明的来源模式
	addSecretTargets       []app.SecretTargetOption
	addSecretTargetsLoad   asyncLoad // Project 页候选归属枚举（禁止同步调用）
	addSecretTargetIdx     int
	addSecretResult        *app.AddSecretResult
	addSecretErr           error
	addSecretRemoteMode    bool              // true = Remote 登记（归属为远端 P 地址）
	addSecretPName         string            // Remote：光标反推出的 P 名，或 N 手输的新 P
	addSecretPlane         secrets.SyncPlane // Remote：归属平面（N 时可切换）
	addSecretScopeNew      bool              // Remote：P 由用户手输（可能尚不存在）
	addSecretScopeCheckGen uint64
	addSecretTypeIdx       int    // Remote：Processor 表索引
	addSecretInitialBody   string // Remote temp 预填正文
	// addSecretNotice 是表单内可见的校验/说明行。登记表单整页渲染，日志区不可见，
	// 校验失败只 pushLog 会让用户以为按键无效。
	addSecretNotice string
	createStage     string // "", "kind", "name"
	createKindIdx   int
	createName      string
	createShared    bool

	serverVersion                  string
	serverVersionMismatch          bool
	serverVersionMismatchDismissed bool
	serverRestartStage             string // "", "confirm", "running", "done"
	serverRestartReason            string // "mismatch" | "manual" | "update"
	serverRestartErr               error
	restartingServer               bool
}

func newModel(projectRoot, currentVersion string) model {
	return newModelWithOptions(projectRoot, currentVersion, RunOptions{})
}

func newModelWithOptions(projectRoot, currentVersion string, opts RunOptions) model {
	logs := []string{
		"TUI shell ready",
		"Loading project overview...",
		"Loading asset selection...",
		"Loading global settings...",
	}
	if opts.ConfigInitMode {
		logs = []string{
			"项目配置初始化",
			"选择 bundle 后按 s 保存并退出",
			"Loading asset selection...",
		}
	}
	m := model{
		projectRoot:    projectRoot,
		plane:          app.NewWorkspace(opts.Plane, projectRoot).EffectivePlane(),
		currentVersion: currentVersion,
		pages:          defaultPages(),
		configInitMode: opts.ConfigInitMode,
		focus:          focusContent,
		logs:           logs,
	}
	if opts.ConfigInitMode {
		m.pageIndex = int(pageImport)
		m.focus = focusContent
	}
	return m
}

func (m model) workspace() app.Workspace {
	return app.NewWorkspace(m.plane, m.projectRoot)
}

func (m model) Init() tea.Cmd {
	diag.StartupLog("TUI Init projectRoot=%q log=%s", m.projectRoot, diag.StartupLogPath())
	// 不能在 Init 里直接 refreshCmd：Update 是值接收者，Init 无法带回
	// beginParts 对 shellRefresh.gen 的写入；会导致 overview gen=1 撞上 model.gen=0 被 DROPPED。
	return func() tea.Msg { return shellRefreshKickMsg{} }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncTreeViewports()
		return m, nil
	case deleteLoadedMsg:
		if !m.deleteLoad.finish(msg.loadGen) {
			return m, nil
		}
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		m.deleteCandidatesLoaded = true
		m.deleteIncludeRemote = msg.includeRemote
		m.deleteCandidates = msg.candidates
		m.deleteLoadErr = msg.err
		m.deleteTree = TreeList{}
		m.rebuildDeleteTree()
		for _, line := range msg.logs {
			m.pushLog(line)
		}
		if msg.err != nil {
			m.pushLog("Delete 列表加载失败: " + msg.err.Error())
		} else {
			m.pushLog(fmt.Sprintf("Delete 列表已加载：%d 项", len(msg.candidates)))
		}
		return m, nil
	case deleteEventMsg:
		m.recordRunEvent(msg.event)
		if m.runStream != nil {
			return m, waitRunMsg(m.runStream)
		}
		return m, nil
	case deleteCompletedMsg:
		m.runningDelete = false
		m.runStream = nil
		if m.runCancel != nil {
			m.runCancel()
			m.runCancel = nil
		}
		m.runCtx = nil
		m.deleteStage = ""
		m.deleteResult = msg.result
		m.deleteErr = msg.err
		m.deleteCandidatesLoaded = false
		if msg.err != nil {
			m.pushLog("Delete failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Delete finished: dec %d · secrets %d · ssh %d · bundle %d",
				msg.result.DecDeleted, msg.result.SecretsDeleted, msg.result.SSHKeysDeleted, msg.result.BundlesDeleted))
		}
		return m, tea.Batch(m.refreshCmd(), m.startDeleteCandidatesLoad(m.deleteIncludeRemote, true))
	case shellRefreshKickMsg:
		diag.StartupLog("shellRefreshKickMsg → refreshCmd")
		return m, tea.Batch(m.refreshCmd(), pollActiveOperationCmd(m.projectRoot, 0), probeServerVersionCmd())
	case serverVersionMsg:
		if msg.err != nil {
			m.pushLog("探测服务版本失败: " + msg.err.Error())
			return m, nil
		}
		m.serverVersion = msg.serverVersion
		m.serverVersionMismatch = msg.mismatch
		if msg.mismatch && !m.serverVersionMismatchDismissed && m.serverRestartStage == "" {
			m.beginServerRestartConfirm("mismatch")
			m.pushLog(fmt.Sprintf("服务版本不一致: client=%s server=%s", msg.clientVersion, msg.serverVersion))
		}
		return m, nil
	case serverRestartDoneMsg:
		m.restartingServer = false
		m.serverRestartErr = msg.err
		m.serverRestartStage = "done"
		if msg.err != nil {
			m.pushLog("重启 dec-server 失败: " + msg.err.Error())
			return m, nil
		}
		m.serverVersion = msg.serverVersion
		m.serverVersionMismatch = VersionsMismatch(m.currentVersion, msg.serverVersion)
		m.serverVersionMismatchDismissed = false
		m.pushLog(fmt.Sprintf("dec-server 已重启: %s", fallbackValue(msg.serverVersion, "未知")))
		if msg.reason == "update" {
			m.serverRestartStage = ""
		}
		return m, nil
	case activeOperationPolledMsg:
		if msg.err == nil && msg.active && !m.runningPull && !m.runningRemove && m.observedOperationID != msg.operationID {
			stream := make(chan tea.Msg, 64)
			m.observedOperationID = msg.operationID
			m.observedOperationFacade = msg.facade
			m.observedStream = stream
			m.runMode = msg.operation
			m.runProgress = nil
			m.runEvents = nil
			m.pushLog(fmt.Sprintf("%s 正在执行 %s，已开始旁观进度", msg.facade, msg.operation))
			return m, tea.Batch(
				startWatchOperationCmd(m.projectRoot, msg.operationID, stream),
				waitRunMsg(stream),
				pollActiveOperationCmd(m.projectRoot, time.Second),
			)
		}
		if !msg.active && m.observedOperationID != "" && m.observedStream == nil {
			m.observedOperationID = ""
			m.observedOperationFacade = ""
		}
		return m, pollActiveOperationCmd(m.projectRoot, time.Second)
	case observedOperationEventMsg:
		m.recordRunEvent(msg.event)
		if m.observedStream != nil {
			return m, waitRunMsg(m.observedStream)
		}
		return m, nil
	case observedOperationDoneMsg:
		m.observedStream = nil
		if msg.err != nil {
			m.pushLog("旁观操作结束: " + msg.err.Error())
		} else {
			m.pushLog("旁观操作已完成")
		}
		m.observedOperationID = ""
		m.observedOperationFacade = ""
		return m, m.refreshCmd()
	case overviewLoadedMsg:
		if !m.shellRefresh.acceptPart(msg.loadGen) {
			diag.StartupLog("overviewLoadedMsg DROPPED msgGen=%d modelGen=%d pending=%d loading=%v",
				msg.loadGen, m.shellRefresh.gen, m.shellRefresh.pending, m.shellRefresh.loading)
			return m, nil
		}
		m.overview = msg.overview
		m.overviewErr = msg.err
		if msg.err != nil {
			diag.StartupLog("overviewLoadedMsg err=%v overviewNil=%v", msg.err, msg.overview == nil)
			m.pushLog("Overview load failed: " + msg.err.Error())
			return m, nil
		}
		diag.StartupLog("overviewLoadedMsg applied enabled=%d available=%d repoConnected=%v",
			msg.overview.EnabledBundleCount, msg.overview.AvailableBundleCount, msg.overview.RepoConnected)
		if msg.overview.ProjectConfigReady {
			m.localProjectInitConfirm = false
		}
		m.pushLog(fmt.Sprintf("Overview loaded: %d enabled bundles (vault scan deferred)", msg.overview.EnabledBundleCount))
		var cmds []tea.Cmd
		// 用户平面没有项目根（workspace.Root 为空），vault project 推断无从谈起；
		// 照常发起会让服务端拿空 projectRoot 去读 cwd 下的 .dec/config.yaml。
		if m.plane != app.WorkspaceUser {
			cmds = append(cmds, loadVaultInferenceCmd(m.projectRoot))
		}
		if msg.overview.RepoConnected {
			cmds = append(cmds, enrichWorkspaceOverviewVaultCmd(m.workspace()))
		}
		return m, tea.Batch(cmds...)
	case vaultInferenceLoadedMsg:
		if msg.err != nil {
			diag.StartupLog("vaultInferenceLoadedMsg err=%v", msg.err)
			m.pushLog("Vault project infer failed: " + msg.err.Error())
			if m.overview != nil && !m.overview.ProjectConfigReady {
				m.localProjectInitConfirm = true
				m.pushLog("当前目录尚未初始化；等待用户确认后再创建 .dec/config.yaml")
			}
			return m, nil
		}
		m.vaultInference = msg.vaultInference
		if msg.vaultInference != nil {
			m.localProjectInitConfirm = false
			diag.StartupLog("vaultInferenceLoadedMsg hit project=%s bundles=%d", msg.vaultInference.ProjectName, len(msg.vaultInference.EnabledBundles))
			m.vaultInferenceDismissed = false
			m.pushLog(fmt.Sprintf("Vault project inferred from directory: %s (%d bundles)", msg.vaultInference.ProjectName, len(msg.vaultInference.EnabledBundles)))
			return m, nil
		}
		diag.StartupLog("vaultInferenceLoadedMsg nil (no match)")
		if m.overview != nil && !m.overview.ProjectConfigReady {
			m.localProjectInitConfirm = true
			m.pushLog("无 vault project 匹配；等待用户确认后再初始化当前目录")
		}
		return m, nil
	case overviewVaultEnrichedMsg:
		if msg.err != nil {
			diag.StartupLog("overviewVaultEnrichedMsg err=%v", msg.err)
			m.pushLog("Overview vault enrich failed: " + msg.err.Error())
			return m, nil
		}
		if m.overview == nil {
			diag.StartupLog("overviewVaultEnrichedMsg skipped (overview still nil)")
			return m, nil
		}
		m.overview.Bundles = msg.bundles
		m.overview.AvailableBundleCount = msg.availableBundleCount
		m.overview.EnabledProjects = nil
		m.overview.RequiredProjects = nil
		for _, item := range msg.bundles {
			if item.Model != "p" {
				continue
			}
			m.overview.Model = "p"
			if item.Home {
				m.overview.HomeProject = item.Name
			}
			if item.Enabled {
				m.overview.EnabledProjects = append(m.overview.EnabledProjects, item.Name)
			}
			if item.Required {
				m.overview.RequiredProjects = append(m.overview.RequiredProjects, item.Name)
			}
		}
		if m.overview.Model == "p" {
			m.overview.EnabledBundleCount = len(m.overview.EnabledProjects)
		}
		diag.StartupLog("overviewVaultEnrichedMsg applied available=%d", msg.availableBundleCount)
		m.pushLog(fmt.Sprintf("Overview vault bundles ready: %d available", msg.availableBundleCount))
		return m, nil
	case vaultProjectAppliedMsg:
		if !m.vaultApplyLoad.finish(msg.loadGen) {
			return m, nil
		}
		if msg.err != nil {
			m.pushLog("Vault project apply failed: " + msg.err.Error())
			return m, nil
		}
		m.vaultInference = nil
		m.vaultInferenceDismissed = false
		m.localProjectInitConfirm = false
		if msg.result != nil && msg.result.Applied {
			name := msg.result.ProjectName
			m.vaultAutoApplyNotice = fmt.Sprintf("已从 vault 应用 project %s", name)
			m.pushLog(m.vaultAutoApplyNotice)
			if msg.result.VarsCreated {
				m.pushLog("Project vars template created: .dec/vars.yaml")
			}
		}
		return m, m.refreshCmd()
	case assetsLoadedMsg:
		if !m.shellRefresh.acceptPart(msg.loadGen) {
			return m, nil
		}
		m.assets = msg.state
		m.assetsErr = msg.err
		m.savingAssets = false
		m.assetsDirty = false
		if msg.err != nil {
			m.pushLog("Asset selection load failed: " + msg.err.Error())
			return m, nil
		}
		// 用磁盘上的 bundle 启用态重置本地镜像。Save 时会根据此列表（可能已被用户编辑过）写回。
		m.bundleSelection = nil
		if msg.state != nil {
			for _, bo := range msg.state.Bundles {
				if bo.Enabled {
					m.bundleSelection = append(m.bundleSelection, bo.Name)
				}
			}
		}
		m.normalizeAssetCursor()
		if msg.state != nil {
			m.pushLog(fmt.Sprintf("Bundle selection loaded: %d bundles", len(msg.state.Bundles)))
		}
		return m, nil
	case assetsSavedMsg:
		m.savingAssets = false
		if msg.err != nil {
			m.pushLog("Bundle selection save failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Bundle selection saved: %d bundles", msg.result.EnabledBundleCount))
			// 被拒的勾选不进 enabled_bundles；不说出来就是「勾了却没生效」。
			for _, rejected := range msg.result.RejectedBundles {
				m.pushLog("未启用: " + rejected)
			}
		}
		if m.configInitMode {
			m.pushLog("项目配置已保存，退出初始化")
			return m, tea.Quit
		}
		return m, m.refreshCmd()
	case settingsLoadedMsg:
		if !m.shellRefresh.acceptPart(msg.loadGen) {
			return m, nil
		}
		m.settings = msg.state
		m.settingsErr = msg.err
		m.savingSettings = false
		m.settingsRepoEditing = false
		m.settingsIdleTimeoutEditing = false
		m.settingsDirty = false
		if msg.err != nil {
			m.pushLog("Global settings load failed: " + msg.err.Error())
			return m, nil
		}
		if msg.state != nil {
			m.settingsRepoInput = msg.state.RepoURL
			m.settingsIdleTimeoutInput = msg.state.ServerIdleTimeout
			m.settingsSelectedIDEs = cloneStrings(msg.state.SelectedIDEs)
			m.normalizeSettingsCursor()
			m.syncSettingsDirty()
			m.pushLog(fmt.Sprintf("Global settings loaded: %d IDEs, %d user bundles",
				len(m.settingsSelectedIDEs), m.settingsUserBundleCount()))
			if msg.state.RepoConnected && len(m.settingsSelectedIDEs) > 0 && !m.builtinAssetsLoad.busy() {
				gen := m.builtinAssetsLoad.beginGen()
				return m, ensureBuiltinIDEAssetsCmd(cloneStrings(m.settingsSelectedIDEs), gen)
			}
		}
		return m, nil
	case builtinAssetsEnsuredMsg:
		if !m.builtinAssetsLoad.finish(msg.loadGen) {
			return m, nil
		}
		for _, warning := range msg.warnings {
			m.pushLog("Install warning: " + warning)
		}
		if len(msg.warnings) == 0 {
			m.pushLog("Builtin IDE assets synced (skills + dec MCP)")
		}
		return m, nil
	case settingsSavedMsg:
		m.savingSettings = false
		if msg.err != nil {
			m.pushLog("Global settings save failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil && msg.result.RepoAuthRequired {
			m.enterRepoBootstrapConfirm("settings", msg.result.RepoHost, msg.result.ConnectError)
			m.pushLog(fmt.Sprintf("仓库 %s 需要认证；等待确认是否从 Bitwarden 查找 GCM", msg.result.RepoHost))
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Global settings saved: %d IDEs, %d user bundles",
				len(msg.result.IDEs), len(msg.result.EnabledBundles)))
			for _, name := range msg.result.CreatedVaultBundles {
				m.pushLog("Created vault placeholder bundle: " + name)
			}
			for _, warning := range msg.result.InstallWarnings {
				m.pushLog("Install warning: " + warning)
			}
		}
		return m, m.refreshCmd()
	case repoBootstrapEventMsg:
		if strings.TrimSpace(msg.event.Message) != "" {
			m.pushLog(msg.event.Message)
		}
		if m.repoBootstrapStream != nil {
			return m, waitRunMsg(m.repoBootstrapStream)
		}
		return m, nil
	case repoBootstrapPreparedMsg:
		cancelled := m.repoBootstrapStage == ""
		m.finishRepoBootstrapOperation()
		if cancelled {
			return m, nil
		}
		if msg.err != nil {
			m.repoBootstrapStage = "confirm"
			m.repoBootstrapError = msg.err.Error()
			m.pushLog("仓库认证 bootstrap 准备失败: " + msg.err.Error())
			return m, nil
		}
		m.repoBootstrapCandidates = nil
		if msg.result != nil {
			m.repoBootstrapHost = msg.result.RepoHost
			m.repoBootstrapCandidates = append([]app.RepoGCMCandidate(nil), msg.result.Candidates...)
		}
		m.repoBootstrapCursor = 0
		if len(m.repoBootstrapCandidates) == 0 {
			m.repoBootstrapStage = "confirm"
			m.repoBootstrapError = "Bitwarden 中没有匹配该仓库 host 的 .gcm/* Note"
			m.pushLog(m.repoBootstrapError)
			return m, nil
		}
		m.repoBootstrapStage = "select"
		m.repoBootstrapError = ""
		m.pushLog(fmt.Sprintf("找到 %d 个 GCM 候选，请选择", len(m.repoBootstrapCandidates)))
		return m, nil
	case repoBootstrapAppliedMsg:
		cancelled := m.repoBootstrapStage == ""
		source := m.repoBootstrapSource
		m.finishRepoBootstrapOperation()
		if cancelled {
			return m, nil
		}
		if msg.err != nil {
			m.repoBootstrapStage = "select"
			m.repoBootstrapError = msg.err.Error()
			m.pushLog("应用 GCM 失败: " + msg.err.Error())
			return m, nil
		}
		m.clearRepoBootstrap()
		if source == "run" {
			m.pushLog("GCM 已应用并验证通过，正在重试 pull")
			return m, m.startPullRun()
		}
		m.pushLog("GCM 已应用并验证通过，正在重试保存 Settings")
		m.savingSettings = true
		return m, saveSettingsCmd(strings.TrimSpace(m.settingsRepoInput), strings.TrimSpace(m.settingsIdleTimeoutInput), cloneStrings(m.settingsSelectedIDEs))
	case projectSettingsLoadedMsg:
		if !m.shellRefresh.acceptPart(msg.loadGen) {
			return m, nil
		}
		m.projectSettings = msg.state
		m.projectSettingsErr = msg.err
		m.savingProjectSettings = false
		m.projectSettingsDirty = false
		if msg.err != nil {
			m.pushLog("Project settings load failed: " + msg.err.Error())
			return m, nil
		}
		if msg.state != nil {
			m.projectSettingsOverride = msg.state.OverrideActive
			m.projectSettingsSelectedIDEs = cloneStrings(msg.state.SelectedIDEs)
			m.normalizeProjectSettingsCursor()
			m.syncProjectSettingsDirty()
			if msg.state.OverrideActive {
				m.pushLog(fmt.Sprintf("Project settings loaded: %d IDE overrides", len(m.projectSettingsSelectedIDEs)))
			} else {
				m.pushLog("Project settings loaded: inheriting global IDEs")
			}
		}
		return m, nil
	case projectSettingsSavedMsg:
		m.savingProjectSettings = false
		if msg.err != nil {
			m.pushLog("Project settings save failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			if msg.result.OverrideActive {
				m.pushLog(fmt.Sprintf("Project settings saved: %d IDE overrides", len(msg.result.SelectedIDEs)))
			} else {
				m.pushLog("Project settings saved: cleared override, inheriting global")
			}
		}
		return m, m.refreshCmd()
	case localProjectEnsuredMsg:
		if !m.localProjectLoad.finish(msg.loadGen) {
			return m, nil
		}
		m.localProjectInitConfirm = false
		if msg.err != nil {
			m.pushLog("本地 project 生成失败: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			if msg.result.ExistingConfig {
				m.pushLog("本地 project 配置已存在")
			} else {
				m.pushLog("已生成本地 project 配置，下一步到 Bundles 页勾选 bundle")
			}
			if msg.result.VarsCreated {
				m.pushLog("Project vars template created: .dec/vars.yaml")
			}
		}
		return m, m.refreshCmd()
	case projectConfigInitializedMsg:
		if !m.projectInitLoad.finish(msg.loadGen) {
			return m, nil
		}
		m.lastInitResult = msg.result
		m.lastInitErr = msg.err
		if msg.err != nil {
			m.pushLog("Project config init failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			if msg.result.ProjectConfig == nil {
				m.pushLog(fmt.Sprintf("Project config init: 仓库暂无资产 (AssetCount=%d)", msg.result.AssetCount))
			} else if msg.result.ExistingConfig {
				m.pushLog(fmt.Sprintf("Project config refreshed: %d assets available", msg.result.AssetCount))
			} else {
				m.pushLog(fmt.Sprintf("Project config initialized: %d assets available", msg.result.AssetCount))
			}
			if msg.result.VarsCreated {
				m.pushLog("Project vars template created: .dec/vars.yaml")
			}
		}
		return m, m.refreshCmd()
	case projectVarsLoadedMsg:
		if msg.solo {
			if !m.projectVarsLoad.finish(msg.loadGen) {
				return m, nil
			}
		} else if !m.shellRefresh.acceptPart(msg.loadGen) {
			return m, nil
		}
		m.projectVars = msg.view
		m.projectVarsErr = msg.err
		if msg.err != nil {
			m.pushLog("Project vars load failed: " + msg.err.Error())
			return m, nil
		}
		if msg.view != nil {
			missing := msg.view.MissingPlaceholders()
			m.pushLog(fmt.Sprintf("Project vars loaded: %d used / %d missing", len(msg.view.UsedPlaceholders), len(missing)))
		}
		return m, nil
	case projectVarsEditedMsg:
		m.lastEditErr = msg.err
		if msg.err != nil {
			m.pushLog("Editor exited with error: " + msg.err.Error())
		} else {
			m.pushLog("Editor session finished; reloading project vars")
		}
		gen := m.projectVarsLoad.beginGen()
		return m, loadProjectVarsCmd(m.projectRoot, gen, true)
	case globalVarsEditedMsg:
		m.lastEditErr = msg.err
		if msg.err != nil {
			m.pushLog("Editor exited with error: " + msg.err.Error())
		} else {
			m.pushLog("Editor session finished; reloading global vars")
		}
		gen := m.globalVarsLoad.beginGen()
		return m, loadGlobalVarsCmd(gen)
	case globalVarsLoadedMsg:
		if !m.globalVarsLoad.finish(msg.loadGen) {
			return m, nil
		}
		if msg.err != nil {
			m.pushLog("Global vars load failed: " + msg.err.Error())
			return m, nil
		}
		if msg.view != nil && m.settings != nil {
			m.settings.VarsPath = msg.view.VarsPath
			m.settings.VarsFileReady = msg.view.VarsFileReady
			if strings.TrimSpace(msg.view.EditorCommand) != "" {
				m.settings.ConfiguredEditor = msg.view.EditorCommand
			}
			m.pushLog(fmt.Sprintf("Global vars loaded: %d keys, ready=%v", len(msg.view.Vars), msg.view.VarsFileReady))
		}
		return m, nil
	case remoteEditPreparedMsg:
		return m, m.handleRemoteEditPrepared(msg)
	case remoteEditEditorClosedMsg:
		return m, m.handleRemoteEditEditorClosed(msg)
	case remoteEditDoneMsg:
		return m, m.handleRemoteEditDone(msg)
	case remoteRegisterPreparedMsg:
		return m, m.handleRemoteRegisterPrepared(msg)
	case remoteRegisterEditorClosedMsg:
		return m, m.handleRemoteRegisterEditorClosed(msg)
	case remoteScopeValidatedMsg:
		m.applyRemoteScopeValidation(msg)
		return m, nil
	case filePickedMsg:
		return m, m.handleFilePicked(msg)
	case pushPreviewLoadedMsg:
		if !m.pushPreviewLoad.finish(msg.loadGen) {
			return m, nil
		}
		m.pushPreview = msg.preview
		m.pushPreviewErr = msg.err
		m.pushStage = "summary"
		m.pushResult = nil
		m.runErr = nil
		if msg.err != nil {
			m.pushLog("Push 预览失败: " + msg.err.Error())
		} else if msg.preview != nil {
			m.pushLog(fmt.Sprintf("Push 确认页已打开：%d 个 enabled bundle", msg.preview.EnabledBundleCount))
		}
		return m, nil
	case createLocalAssetDoneMsg:
		if msg.err != nil {
			m.pushLog("新建失败: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog("已写入 " + msg.result.Path)
		}
		return m, m.refreshCmd()
	case addSecretTargetsMsg:
		m.applyAddSecretTargets(msg)
		return m, nil
	case addSecretDoneMsg:
		// 用户 Esc 退出等待后又开了新一轮表单：迟到的结果只落日志，不覆盖新表单。
		stale := m.addSecretStage != "" && m.addSecretStage != addSecretStageRunning
		if !stale {
			m.addSecretStage = ""
			m.addSecretResult = msg.result
			m.addSecretErr = msg.err
		}
		m.remoteRegisterPending = false
		m.remoteRegisterSess = nil
		m.clearRemoteEditSession()
		for _, line := range msg.logs {
			m.pushLog(line)
		}
		if stale {
			if msg.err != nil {
				m.pushLog("上一轮登记失败: " + msg.err.Error())
			} else {
				m.pushLog("上一轮登记已完成")
			}
			return m, nil
		}
		if msg.err != nil {
			m.pushLog("登记 secret 失败: " + msg.err.Error())
			return m, nil
		}
		if m.addSecretRemoteMode || m.isRemotePage() {
			m.pushLog("Remote 登记已保存")
			return m, m.startDeleteCandidatesLoad(true, true)
		}
		return m, nil
	case runEventMsg:
		m.recordRunEvent(msg.event)
		if m.runStream != nil {
			return m, waitRunMsg(m.runStream)
		}
		return m, nil
	case runCompletedMsg:
		m.runningPull = false
		m.runStream = nil
		if m.runCancel != nil {
			m.runCancel()
			m.runCancel = nil
		}
		m.runCtx = nil
		if m.runMode == "push" {
			m.pushResult = msg.pushResult
			m.runResult = nil
			m.pushStage = ""
		} else {
			m.runResult = msg.result
			m.pushResult = nil
		}
		m.runErr = msg.err
		if msg.err != nil {
			errText := msg.err.Error()
			if m.runMode == "push" {
				m.pushLog("Run push failed: " + app.StripRepoAuthMarker(errText))
			} else {
				m.pushLog("Run pull failed: " + app.StripRepoAuthMarker(errText))
				// 凭证过期是 Run 页最常见的「环依赖」触发点：不依赖 Settings 换 URL，直接进 bootstrap。
				if app.IsRepoAuthRequiredMessage(errText) {
					m.enterRepoBootstrapConfirm("run", m.bootstrapRepoHost(), app.StripRepoAuthMarker(errText))
					m.pushLog(fmt.Sprintf("仓库 %s 需要认证；等待确认是否从 Bitwarden 查找 GCM", fallbackValue(m.repoBootstrapHost, "<unknown>")))
				}
			}
			return m, nil
		}
		if m.runMode == "push" && msg.pushResult != nil {
			m.pushLog(fmt.Sprintf("Run push finished: dec %d · secrets created %d / updated %d",
				msg.pushResult.DecPushedCount, msg.pushResult.SecretsCreatedCount, msg.pushResult.SecretsUpdatedCount))
		} else if msg.result != nil {
			secretsMsg := fmt.Sprintf("secrets %d files · %d ssh", msg.result.SecretsNoteCount, msg.result.SecretsSSHKeyCount)
			if msg.result.SecretsSkippedReason != "" && msg.result.SecretsNoteCount == 0 && msg.result.SecretsSSHKeyCount == 0 {
				secretsMsg = "secrets skipped: " + msg.result.SecretsSkippedReason
			}
			m.pushLog(fmt.Sprintf("Run pull finished: %d pulled / %d failed · %s",
				msg.result.PulledCount, msg.result.FailedCount, secretsMsg))
			for _, warning := range msg.result.NonFatalWarnings {
				m.pushLog("Pull warning: " + warning)
			}
		}
		return m, m.refreshCmd()
	case removeEventMsg:
		m.recordRunEvent(msg.event)
		if m.runStream != nil {
			return m, waitRunMsg(m.runStream)
		}
		return m, nil
	case removeCompletedMsg:
		m.runningRemove = false
		m.runStream = nil
		m.removeResult = msg.result
		m.removeErr = msg.err
		m.removeStage = ""
		m.removeTarget = nil
		if msg.err != nil {
			m.pushLog("Run remove failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result != nil {
			m.pushLog(fmt.Sprintf("Run remove finished: bundle %s (%d members)", msg.result.BundleName, msg.result.MemberCount))
		}
		return m, m.refreshCmd()
	case updateCheckedMsg:
		m.updateResult = msg.result
		m.updateErr = msg.err
		if msg.err != nil {
			m.updateStage = "done"
			m.pushLog("Update check failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result == nil {
			m.updateStage = "done"
			m.pushLog("Update check returned empty result")
			return m, nil
		}
		if !msg.result.NeedUpdate {
			m.updateStage = "done"
			m.pushLog(fmt.Sprintf("Already up to date: %s", msg.result.CurrentVersion))
			return m, nil
		}
		m.updateStage = "confirm"
		m.pushLog(fmt.Sprintf("New version available: %s -> %s", msg.result.CurrentVersion, msg.result.LatestVersion))
		return m, nil
	case updateDoneMsg:
		m.updatingBinary = false
		m.updateErr = msg.err
		m.updateDoneVersion = msg.targetVersion
		m.updateStage = "done"
		if msg.err != nil {
			m.pushLog("Update failed: " + msg.err.Error())
			return m, nil
		}
		m.pushLog(fmt.Sprintf("Update succeeded: %s", msg.targetVersion))
		m.currentVersion = msg.targetVersion
		m.serverRestartReason = "update"
		return m, m.startServerRestart()
	case tea.KeyMsg:
		m.diagKeyReceived(msg)
		if m.serverRestartStage == "confirm" || m.serverRestartStage == "running" || m.serverRestartStage == "done" {
			return m.handleServerRestartKey(msg)
		}
		if routedModel, routedCmd, routed := m.routeDeletePageKey(msg); routed {
			return routedModel, routedCmd
		}
		if m.assetFilterInput && m.isBundlesPage() {
			return m.handleAssetFilterInput(msg)
		}
		if m.repoBootstrapStage != "" {
			return m.handleRepoBootstrapKey(msg)
		}
		if m.settingsRepoEditing && m.isSettingsPage() {
			return m.handleSettingsRepoInput(msg)
		}
		if m.settingsIdleTimeoutEditing && m.isSettingsPage() {
			return m.handleSettingsIdleTimeoutInput(msg)
		}
		if m.removeFilterInput && m.isRunPage() {
			return m.handleRemoveFilterInput(msg)
		}
		if m.createStage != "" && m.isProjectPage() {
			return m.handleCreateLocalAssetKey(msg)
		}
		if m.addSecretStage != "" && (m.isProjectSettings() || m.isRemotePage()) {
			return m.handleAddSecretKey(msg)
		}
		if m.isRunPage() && m.pushStage != "" && !m.runningPull {
			return m.handlePushStageKey(msg)
		}
		if m.isRunPage() && m.removeStage != "" && !m.runningRemove {
			return m.handleRemoveStageKey(msg)
		}
		if m.isRunPage() && m.updateStage != "" && !m.updatingBinary {
			return m.handleUpdateStageKey(msg)
		}
		if m.isHomePage() && m.hasVaultInferencePrompt() {
			return m.handleVaultInferenceKey(msg)
		}
		if (m.isHomePage() || m.isProjectSettings()) && m.localProjectInitConfirm {
			return m.handleLocalProjectInitConfirmKey(msg)
		}
		if m.isHomePage() && m.vaultApplyLoad.busy() {
			return m, nil
		}
		if m.isRunPage() && m.runningPull && msg.String() == "esc" {
			if m.runCancel != nil {
				m.runCancel()
				m.pushLog("Run pull cancel requested")
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.pushLog("Exit requested")
			return m, tea.Quit
		case "tab":
			fromPage := m.pages[m.pageIndex]
			m.pageIndex = (m.pageIndex + 1) % len(m.pages)
			m.focus = focusContent
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, m.onPageChanged(fromPage)
		case "shift+tab":
			fromPage := m.pages[m.pageIndex]
			m.pageIndex = (m.pageIndex - 1 + len(m.pages)) % len(m.pages)
			m.focus = focusContent
			m.pushLog("Switched to " + m.pages[m.pageIndex])
			return m, m.onPageChanged(fromPage)
		case "l", "right":
			return m.handleHorizontalNav(1)
		case "h", "left":
			return m.handleHorizontalNav(-1)
		case "j", "down":
			return m.handleVerticalNav(1)
		case "k", "up":
			return m.handleVerticalNav(-1)
		case "pgdown", "ctrl+d":
			return m.handleTreePageNav(1)
		case "pgup", "ctrl+u":
			return m.handleTreePageNav(-1)
		case "ctrl+r":
			if !m.remoteOpen {
				m.remoteOpen = true
				m.focus = focusContent
				m.pushLog("Open remote")
				return m, m.startDeleteCandidatesLoad(true, false)
			}
			return m, nil
		case "r":
			m.pushLog("Refreshing project overview, assets, and global settings")
			return m, m.refreshCmd()
		case "/":
			if m.isBundlesPage() && m.focus != focusSidebar {
				m.assetFilterInput = true
				m.pushLog("Asset filter input opened")
			}
			return m, nil
		case "c":
			if m.isBundlesPage() && m.focus != focusSidebar && strings.TrimSpace(m.assetFilter) != "" {
				m.assetFilter = ""
				m.normalizeAssetCursor()
				m.pushLog("Asset filter cleared")
				return m, nil
			}
			if m.isProjectSettings() && !m.savingProjectSettings && m.projectSettings != nil && m.projectSettingsErr == nil {
				m.clearProjectOverride()
				return m, nil
			}
			return m, nil
		case " ", "enter":
			if m.isBundlesPage() && !m.savingAssets && m.focus != focusSidebar {
				if msg.String() == "enter" && m.assetsCursorOnBundle() {
					if m.assetTree.CursorExpanded() {
						m.collapseCurrentBundle()
					} else {
						m.expandCurrentBundle()
					}
					return m, nil
				}
				m.toggleCurrentAsset()
				return m, nil
			}
			if m.isProjectSettings() && !m.savingProjectSettings && m.projectSettings != nil && m.projectSettingsErr == nil && m.focus != focusSidebar {
				if m.projectSettingsCursor == 0 {
					m.toggleProjectOverride()
				} else {
					m.toggleCurrentProjectIDE()
				}
				return m, nil
			}
			if m.isSettingsPage() && !m.savingSettings && m.focus != focusSidebar {
				if m.settingsCursor == settingsRowRepo {
					if msg.String() == "enter" {
						m.beginSettingsRepoEdit()
					}
				} else if m.settingsCursor == settingsRowIdleTimeout {
					if msg.String() == "enter" {
						m.beginSettingsIdleTimeoutEdit()
					}
				} else if m.settingsCursor == settingsRowRestartServer {
					if msg.String() == "enter" {
						m.beginServerRestartConfirm("manual")
						m.pushLog("确认重启 dec-server？")
					}
				} else if m.settingsCursor == settingsRowGlobalVars {
					return m, m.openGlobalVarsEditor()
				} else if m.settingsCursorIDEIndex() >= 0 {
					m.toggleCurrentSettingsIDE()
				}
				return m, nil
			}
			return m, nil
		case "e":
			if m.isSettingsPage() && !m.savingSettings && m.settings != nil {
				switch m.settingsCursor {
				case settingsRowRepo:
					m.beginSettingsRepoEdit()
				case settingsRowIdleTimeout:
					m.beginSettingsIdleTimeoutEdit()
				case settingsRowGlobalVars:
					return m, m.openGlobalVarsEditor()
				}
				return m, nil
			}
			if m.isProjectSettings() && !m.savingProjectSettings && !m.projectInitLoad.busy() {
				editorCmd := ""
				if m.projectVars != nil {
					editorCmd = m.projectVars.EditorCommand
				} else if m.overview != nil {
					editorCmd = m.overview.Editor
				}
				m.pushLog("Opening external editor for .dec/vars.yaml")
				return m, openProjectVarsEditorCmd(m.projectRoot, editorCmd)
			}
			return m, nil
		case "s":
			if m.isBundlesPage() && !m.savingAssets && m.assets != nil && m.assetsErr == nil {
				m.savingAssets = true
				m.pushLog("Saving bundle selection")
				return m, saveWorkspaceAssetsCmd(m.workspace(), cloneStrings(m.bundleSelection))
			}
			if m.isSettingsPage() && !m.savingSettings && m.settings != nil && m.settingsErr == nil {
				m.savingSettings = true
				m.pushLog("Saving global settings")
				return m, saveSettingsCmd(strings.TrimSpace(m.settingsRepoInput), strings.TrimSpace(m.settingsIdleTimeoutInput), cloneStrings(m.settingsSelectedIDEs))
			}
			if m.isProjectSettings() && !m.savingProjectSettings && m.projectSettings != nil && m.projectSettingsErr == nil {
				if m.projectSettingsOverride && len(normalizedStringList(m.projectSettingsSelectedIDEs)) == 0 {
					m.pushLog("覆盖模式下至少选择一个 IDE，或按 c 切回继承模式")
					return m, nil
				}
				m.savingProjectSettings = true
				clearOverride := !m.projectSettingsOverride
				if clearOverride {
					m.pushLog("Saving project settings: clear override")
				} else {
					m.pushLog("Saving project settings: override")
				}
				return m, saveProjectSettingsCmd(m.projectRoot, clearOverride, cloneStrings(m.projectSettingsSelectedIDEs))
			}
			if m.isRunPage() && !m.runningPull && !m.runningRemove && !m.updatingBinary && m.updateStage == "" {
				return m, m.startPullRun()
			}
			return m, nil
		case "i":
			if m.isProjectSettings() && m.projectSettings != nil && m.projectSettingsErr == nil && !m.projectSettings.ProjectConfigReady {
				if m.projectInitLoad.busy() || m.savingProjectSettings || m.localProjectLoad.busy() {
					return m, nil
				}
				m.localProjectInitConfirm = true
				m.pushLog("请确认当前目录无误后再初始化")
				return m, nil
			}
			return m, nil
		case "A":
			if m.isProjectSettings() && m.projectSettings != nil && m.projectSettingsErr == nil {
				if !m.projectSettings.ProjectConfigReady {
					m.pushLog("登记 secret 需要先有 .dec/config.yaml，按 i 在 Project 页生成本地 project")
					return m, nil
				}
				return m, m.beginAddSecret()
			}
			return m, nil
		case "n":
			if m.isRemotePage() {
				return m, m.beginRemoteRegisterAtCursor()
			}
			if m.isProjectPage() {
				return m.beginCreateLocalAsset()
			}
			return m, nil
		case "R":
			if m.isProjectSettings() && m.projectSettings != nil && m.projectSettingsErr == nil && m.projectSettings.ProjectConfigReady {
				if m.projectInitLoad.busy() || m.savingProjectSettings {
					return m, nil
				}
				if m.overview == nil || !m.overview.RepoConnected {
					m.pushLog("刷新 available 需要先连接仓库，请切到 Settings 页配置 Repo URL")
					return m, nil
				}
				gen := m.projectInitLoad.beginGen()
				m.lastInitResult = nil
				m.lastInitErr = nil
				m.pushLog("Refreshing project available assets (扫描仓库)...")
				return m, initProjectConfigCmd(m.projectRoot, gen)
			}
			return m, nil
		case "p":
			if m.isRunPage() && !m.runningPull && !m.runningRemove && m.pushStage == "" && !m.updatingBinary && m.updateStage == "" {
				return m, m.startPullRun()
			}
			return m, nil
		case "P":
			if m.isRunPage() && !m.runningPull && !m.runningRemove && m.pushStage == "" && !m.updatingBinary && m.updateStage == "" {
				return m, m.beginPushConfirmation()
			}
			return m, nil
		case "u":
			if m.isRunPage() && !m.runningPull && !m.runningRemove && m.pushStage == "" && !m.updatingBinary && m.removeStage == "" {
				return m, m.startUpdateCheck()
			}
			return m, nil
		case "?":
			if m.isRunPage() && m.removeStage == "" && m.pushStage == "" && !m.removeFilterInput {
				m.runShowHelp = !m.runShowHelp
			}
			return m, nil
		}
	}

	return m, nil
}

// handleHorizontalNav 实现左右键（h/l）的空间进入/退出模型。
func (m model) handleHorizontalNav(direction int) (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusSidebar:
		if direction > 0 {
			m.focus = focusContent
			m.pushLog("进入内容区")
		}
		return m, nil
	case focusContent:
		if direction < 0 {
			if m.isBundlesPage() && m.assetsCursorOnBundle() && m.assetTree.CursorExpanded() {
				m.collapseCurrentBundle()
				return m, nil
			}
			if m.isRemotePage() && m.deleteTree.CollapseAtCursor() {
				m.pushLog("Remote 折叠目录")
				return m, nil
			}
			return m, nil
		}
		if m.isRemotePage() && direction > 0 {
			if m.deleteTree.CursorOnExpandable() && !m.deleteTree.CursorExpanded() {
				m.deleteTree.ExpandAtCursor()
				m.pushLog("Remote 展开目录")
				return m, nil
			}
			return m, nil
		}
		if m.isBundlesPage() && m.focus == focusContent && direction > 0 {
			m.expandAssetAtCursor()
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

// handleVerticalNav 实现上下键（j/k）在侧栏或内容区的移动。
func (m model) handleVerticalNav(delta int) (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusSidebar:
		fromPage := m.pages[m.pageIndex]
		if delta > 0 {
			m.pageIndex = (m.pageIndex + 1) % len(m.pages)
		} else {
			m.pageIndex = (m.pageIndex - 1 + len(m.pages)) % len(m.pages)
		}
		m.pushLog("Switched to " + m.pages[m.pageIndex])
		return m, m.onPageChanged(fromPage)
	case focusContent:
		if m.isBundlesPage() {
			if m.canNavigateAssets() {
				m.syncTreeViewports()
				m.moveAssetCursor(delta)
			}
			return m, nil
		}
		if m.isSettingsPage() && m.settings != nil {
			if m.canNavigateSettings() {
				m.moveSettingsCursor(delta)
			}
			return m, nil
		}
		if m.isProjectSettings() {
			if m.canNavigateProjectSettings() {
				m.moveProjectSettingsCursor(delta)
			}
			return m, nil
		}
		if m.isDeletePage() && m.focus == focusContent {
			m.syncTreeViewports()
			m.deleteTree.MoveCursor(delta)
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

// handleTreePageNav 为 Bundles / Remote 树列表翻页。
func (m model) handleTreePageNav(dir int) (tea.Model, tea.Cmd) {
	if m.focus != focusContent {
		return m, nil
	}
	m.syncTreeViewports()
	if m.isBundlesPage() && m.canNavigateAssets() {
		m.assetTree.PageCursor(dir)
		return m, nil
	}
	if m.isDeletePage() {
		m.deleteTree.PageCursor(dir)
		return m, nil
	}
	return m, nil
}

const refreshPartCount = 5

func (m *model) refreshCmd() tea.Cmd {
	gen := m.shellRefresh.beginParts(refreshPartCount)
	diag.StartupLog("refreshCmd start gen=%d parts=%d", gen, refreshPartCount)
	projectSettingsCmd := loadProjectSettingsCmd(m.projectRoot, gen)
	projectVarsCmd := loadProjectVarsCmd(m.projectRoot, gen, false)
	if m.plane == app.WorkspaceUser {
		projectSettingsCmd = func() tea.Msg { return projectSettingsLoadedMsg{loadGen: gen} }
		projectVarsCmd = func() tea.Msg { return projectVarsLoadedMsg{loadGen: gen} }
	}
	return tea.Batch(
		loadWorkspaceOverviewCmd(m.workspace(), gen),
		loadWorkspaceAssetsCmd(m.workspace(), gen),
		loadSettingsCmd(gen),
		projectSettingsCmd,
		projectVarsCmd,
	)
}

func loadOverviewCmd(projectRoot string, loadGen uint64) tea.Cmd {
	return loadWorkspaceOverviewCmd(app.NewWorkspace(app.WorkspaceProject, projectRoot), loadGen)
}

func loadWorkspaceOverviewCmd(workspace app.Workspace, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		done := diag.StartupSpan(fmt.Sprintf("loadOverviewCmd gen=%d skipVault", loadGen))
		overview, err := serviceapi.LoadWorkspaceOverview(workspace, false)
		if err != nil {
			done(fmt.Sprintf("err=%v", err))
		} else if overview == nil {
			done("overview=nil")
		} else {
			done(fmt.Sprintf("ok enabled=%d connected=%v", overview.EnabledBundleCount, overview.RepoConnected))
		}
		return overviewLoadedMsg{overview: overview, err: err, loadGen: loadGen}
	}
}

func loadVaultInferenceCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		done := diag.StartupSpan("loadVaultInferenceCmd")
		inference, err := inferVaultProjectOperation(projectRoot, nil)
		if err != nil {
			done(fmt.Sprintf("err=%v", err))
		} else if inference == nil {
			done("nil")
		} else {
			done(fmt.Sprintf("project=%s bundles=%d", inference.ProjectName, len(inference.EnabledBundles)))
		}
		return vaultInferenceLoadedMsg{vaultInference: inference, err: err}
	}
}

func enrichOverviewVaultCmd(projectRoot string) tea.Cmd {
	return enrichWorkspaceOverviewVaultCmd(app.NewWorkspace(app.WorkspaceProject, projectRoot))
}

func enrichWorkspaceOverviewVaultCmd(workspace app.Workspace) tea.Cmd {
	return func() tea.Msg {
		done := diag.StartupSpan("enrichOverviewVaultCmd")
		overview, err := serviceapi.LoadWorkspaceOverview(workspace, true)
		if err != nil {
			done(fmt.Sprintf("err=%v", err))
			return overviewVaultEnrichedMsg{err: err}
		}
		if overview == nil {
			done("overview=nil")
			return overviewVaultEnrichedMsg{}
		}
		done(fmt.Sprintf("available=%d", overview.AvailableBundleCount))
		return overviewVaultEnrichedMsg{
			bundles:              overview.Bundles,
			availableBundleCount: overview.AvailableBundleCount,
		}
	}
}

func applyVaultProjectCmd(projectRoot string, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		result, err := applyVaultProjectOperation(projectRoot, nil)
		return vaultProjectAppliedMsg{result: result, err: err, loadGen: loadGen}
	}
}

func loadAssetsCmd(projectRoot string, loadGen uint64) tea.Cmd {
	return loadWorkspaceAssetsCmd(app.NewWorkspace(app.WorkspaceProject, projectRoot), loadGen)
}

func loadWorkspaceAssetsCmd(workspace app.Workspace, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		done := diag.StartupSpan(fmt.Sprintf("loadAssetsCmd gen=%d", loadGen))
		state, err := serviceapi.LoadWorkspaceAssetSelection(workspace, nil)
		if err != nil {
			done(fmt.Sprintf("err=%v", err))
		} else if state == nil {
			done("state=nil")
		} else {
			done(fmt.Sprintf("ok bundles=%d", len(state.Bundles)))
		}
		return assetsLoadedMsg{state: state, err: err, loadGen: loadGen}
	}
}

func saveAssetsCmd(projectRoot string, bundles []string) tea.Cmd {
	return saveWorkspaceAssetsCmd(app.NewWorkspace(app.WorkspaceProject, projectRoot), bundles)
}

func saveWorkspaceAssetsCmd(workspace app.Workspace, bundles []string) tea.Cmd {
	return func() tea.Msg {
		result, err := serviceapi.SaveWorkspaceEnabledBundles(workspace, bundles, nil)
		return assetsSavedMsg{result: result, err: err}
	}
}

func loadSettingsCmd(loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		done := diag.StartupSpan(fmt.Sprintf("loadSettingsCmd gen=%d", loadGen))
		state, err := loadGlobalSettingsOperation(nil)
		if err != nil {
			done(fmt.Sprintf("err=%v", err))
		} else {
			done("ok")
		}
		return settingsLoadedMsg{state: state, err: err, loadGen: loadGen}
	}
}

func saveSettingsCmd(repoURL, idleTimeout string, ides []string) tea.Cmd {
	return func() tea.Msg {
		result, err := saveGlobalSettingsOperation(app.SaveGlobalSettingsInput{
			RepoURL:           repoURL,
			ServerIdleTimeout: idleTimeout,
			IDEs:              cloneStrings(ides),
			// EnabledBundles 留 nil：用户平面启用列表只由 Bundles 页写入。
			// 传非 nil（含空切片）会按「清空」语义覆盖 GlobalConfig.EnabledBundles。
		}, nil)
		return settingsSavedMsg{result: result, err: err}
	}
}

func startPrepareRepoBootstrapCmd(ctx context.Context, repoURL string, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := prepareRepoGCMBootstrapOperation(ctx, repoURL, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- repoBootstrapEventMsg{event: event}
			}))
			stream <- repoBootstrapPreparedMsg{result: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func startApplyRepoBootstrapCmd(ctx context.Context, input app.ApplyRepoGCMBootstrapInput, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := applyRepoGCMBootstrapOperation(ctx, input, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- repoBootstrapEventMsg{event: event}
			}))
			stream <- repoBootstrapAppliedMsg{result: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func ensureBuiltinIDEAssetsCmd(ideNames []string, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		return builtinAssetsEnsuredMsg{warnings: ensureBuiltinIDEAssetsOperation(cloneStrings(ideNames), nil), loadGen: loadGen}
	}
}

func loadProjectSettingsCmd(projectRoot string, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		done := diag.StartupSpan(fmt.Sprintf("loadProjectSettingsCmd gen=%d", loadGen))
		state, err := loadProjectSettingsOperation(projectRoot, nil)
		if err != nil {
			done(fmt.Sprintf("err=%v", err))
		} else {
			done("ok")
		}
		return projectSettingsLoadedMsg{state: state, err: err, loadGen: loadGen}
	}
}

func saveProjectSettingsCmd(projectRoot string, clearOverride bool, ides []string) tea.Cmd {
	return func() tea.Msg {
		result, err := saveProjectSettingsOperation(app.SaveProjectSettingsInput{
			ProjectRoot:   projectRoot,
			IDEs:          cloneStrings(ides),
			ClearOverride: clearOverride,
		}, nil)
		return projectSettingsSavedMsg{result: result, err: err}
	}
}

func initProjectConfigCmd(projectRoot string, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		result, err := prepareProjectConfigInitOperation(projectRoot, nil)
		return projectConfigInitializedMsg{result: result, err: err, loadGen: loadGen}
	}
}

func ensureLocalProjectCmd(projectRoot string, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		result, err := ensureLocalProjectConfigOperation(projectRoot, nil)
		return localProjectEnsuredMsg{result: result, err: err, loadGen: loadGen}
	}
}

func loadProjectVarsCmd(projectRoot string, loadGen uint64, solo bool) tea.Cmd {
	return func() tea.Msg {
		done := diag.StartupSpan(fmt.Sprintf("loadProjectVarsCmd gen=%d solo=%v", loadGen, solo))
		view, err := loadProjectVarsViewOperation(projectRoot)
		if err != nil {
			done(fmt.Sprintf("err=%v", err))
		} else {
			done("ok")
		}
		return projectVarsLoadedMsg{view: view, err: err, loadGen: loadGen, solo: solo}
	}
}

// openProjectVarsEditorCmd 挂起 TUI 用 tea.ExecProcess 拉起外部编辑器编辑 .dec/vars.yaml。
// 文件不存在时先落模板（不覆盖已有内容）。编辑器退出后推送 projectVarsEditedMsg 触发刷新。
func openProjectVarsEditorCmd(projectRoot, editorCmd string) tea.Cmd {
	ensured, err := ensureProjectVarsFileOperation(projectRoot)
	if err != nil {
		return func() tea.Msg {
			return projectVarsEditedMsg{err: err}
		}
	}
	cmd, err := editor.BuildCommand(ensured.Path, editorCmd)
	if err != nil {
		return func() tea.Msg {
			return projectVarsEditedMsg{err: err}
		}
	}
	return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		return projectVarsEditedMsg{err: runErr}
	})
}

func loadGlobalVarsCmd(loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		view, err := loadGlobalVarsViewOperation()
		return globalVarsLoadedMsg{view: view, err: err, loadGen: loadGen}
	}
}

// openGlobalVarsEditorCmd 挂起 TUI 用 tea.ExecProcess 拉起外部编辑器编辑 ~/.dec/local/vars.yaml。
func openGlobalVarsEditorCmd(editorCmd string) tea.Cmd {
	ensured, err := ensureGlobalVarsFileOperation()
	if err != nil {
		return func() tea.Msg {
			return globalVarsEditedMsg{err: err}
		}
	}
	cmd, err := editor.BuildCommand(ensured.Path, editorCmd)
	if err != nil {
		return func() tea.Msg {
			return globalVarsEditedMsg{err: err}
		}
	}
	return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		return globalVarsEditedMsg{err: runErr}
	})
}

func (m *model) openGlobalVarsEditor() tea.Cmd {
	editorCmd := ""
	if m.settings != nil {
		editorCmd = m.settings.ConfiguredEditor
	}
	m.pushLog("Opening external editor for ~/.dec/local/vars.yaml")
	return openGlobalVarsEditorCmd(editorCmd)
}

func startPullRunCmd(ctx context.Context, projectRoot string, stream chan<- tea.Msg) tea.Cmd {
	return startWorkspacePullRunCmd(ctx, app.NewWorkspace(app.WorkspaceProject, projectRoot), stream)
}

func startWorkspacePullRunCmd(ctx context.Context, workspace app.Workspace, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			var result *app.PullProjectAssetsResult
			var err error
			reporter := app.ReporterFunc(func(event app.OperationEvent) {
				stream <- runEventMsg{event: event}
			})
			if workspace.EffectivePlane() == app.WorkspaceUser {
				result, err = serviceapi.PullWorkspaceAssets(ctx, workspace, reporter)
			} else {
				result, err = runPullOperation(ctx, workspace.Root, reporter)
			}
			stream <- runCompletedMsg{result: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func startPushRunCmd(ctx context.Context, workspace app.Workspace, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := runPushOperation(ctx, workspace, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- runEventMsg{event: event}
			}))
			stream <- runCompletedMsg{pushResult: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func waitRunMsg(stream <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-stream
		if !ok {
			return nil
		}
		return msg
	}
}

func pollActiveOperationCmd(projectRoot string, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		if delay > 0 {
			time.Sleep(delay)
		}
		api, err := serviceapi.Default()
		if err != nil {
			return activeOperationPolledMsg{err: err}
		}
		active, err := api.GetActiveOperation(context.Background(), projectRoot)
		if err != nil {
			return activeOperationPolledMsg{err: err}
		}
		return activeOperationPolledMsg{
			active:      active != nil && active.Active,
			operationID: active.GetOperationId(),
			operation:   active.GetOperation(),
			facade:      active.GetFacade(),
		}
	}
}

func startWatchOperationCmd(projectRoot, operationID string, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			api, err := serviceapi.Default()
			if err == nil {
				err = api.WatchOperation(context.Background(), projectRoot, operationID, app.ReporterFunc(func(event app.OperationEvent) {
					stream <- observedOperationEventMsg{event: event}
				}))
			}
			stream <- observedOperationDoneMsg{err: err}
			close(stream)
		}()
		return nil
	}
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}

func (m model) handleRepoBootstrapKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.repoBootstrapStage {
	case "confirm":
		switch msg.String() {
		case "y", "enter":
			ctx, cancel := context.WithCancel(context.Background())
			stream := make(chan tea.Msg)
			m.repoBootstrapStage = "loading"
			m.repoBootstrapError = ""
			m.repoBootstrapCancel = cancel
			m.repoBootstrapStream = stream
			return m, tea.Batch(
				startPrepareRepoBootstrapCmd(ctx, m.bootstrapRepoURL(), stream),
				waitRunMsg(stream),
			)
		case "n", "esc":
			cancelledFrom := m.repoBootstrapSource
			m.clearRepoBootstrap()
			if cancelledFrom == "run" {
				m.pushLog("已取消私仓 GCM bootstrap；可先到 Remote 页登记 .gcm Note 后再 pull")
			} else {
				m.pushLog("已取消私仓 GCM bootstrap；Settings 未保存")
			}
			return m, nil
		}
	case "select":
		switch msg.String() {
		case "up", "k":
			if m.repoBootstrapCursor > 0 {
				m.repoBootstrapCursor--
			}
		case "down", "j":
			if m.repoBootstrapCursor+1 < len(m.repoBootstrapCandidates) {
				m.repoBootstrapCursor++
			}
		case "enter", "y":
			if len(m.repoBootstrapCandidates) == 0 {
				return m, nil
			}
			candidate := m.repoBootstrapCandidates[m.repoBootstrapCursor]
			ctx, cancel := context.WithCancel(context.Background())
			stream := make(chan tea.Msg)
			m.repoBootstrapStage = "applying"
			m.repoBootstrapError = ""
			m.repoBootstrapCancel = cancel
			m.repoBootstrapStream = stream
			input := app.ApplyRepoGCMBootstrapInput{
				RepoURL: m.bootstrapRepoURL(), Address: candidate.Address, NotePath: candidate.NotePath,
			}
			return m, tea.Batch(startApplyRepoBootstrapCmd(ctx, input, stream), waitRunMsg(stream))
		case "n", "esc":
			cancelledFrom := m.repoBootstrapSource
			m.clearRepoBootstrap()
			if cancelledFrom == "run" {
				m.pushLog("已取消私仓 GCM bootstrap；可先到 Remote 页登记 .gcm Note 后再 pull")
			} else {
				m.pushLog("已取消私仓 GCM bootstrap；Settings 未保存")
			}
		}
	case "loading", "applying":
		if msg.String() == "esc" {
			if m.repoBootstrapCancel != nil {
				m.repoBootstrapCancel()
			}
			m.repoBootstrapStage = ""
			m.pushLog("已请求取消私仓 GCM bootstrap")
		}
	}
	return m, nil
}

func (m *model) finishRepoBootstrapOperation() {
	if m.repoBootstrapCancel != nil {
		m.repoBootstrapCancel()
	}
	m.repoBootstrapCancel = nil
	m.repoBootstrapStream = nil
}

func (m *model) clearRepoBootstrap() {
	m.finishRepoBootstrapOperation()
	m.repoBootstrapStage = ""
	m.repoBootstrapSource = ""
	m.repoBootstrapHost = ""
	m.repoBootstrapError = ""
	m.repoBootstrapCandidates = nil
	m.repoBootstrapCursor = 0
}

func (m *model) enterRepoBootstrapConfirm(source, host, errMsg string) {
	m.repoBootstrapStage = "confirm"
	m.repoBootstrapSource = source
	m.repoBootstrapHost = strings.TrimSpace(host)
	m.repoBootstrapError = strings.TrimSpace(errMsg)
	m.repoBootstrapCandidates = nil
	m.repoBootstrapCursor = 0
}

// bootstrapRepoURL 优先用 Settings 输入/缓存，其次 overview 远端 URL；都空则交给服务端 resolve。
func (m model) bootstrapRepoURL() string {
	if u := strings.TrimSpace(m.settingsRepoInput); u != "" {
		return u
	}
	if m.settings != nil {
		if u := strings.TrimSpace(m.settings.RepoURL); u != "" {
			return u
		}
		if u := strings.TrimSpace(m.settings.ConnectedRepoURL); u != "" {
			return u
		}
	}
	if m.overview != nil {
		if u := strings.TrimSpace(m.overview.RepoRemoteURL); u != "" {
			return u
		}
	}
	return ""
}

func (m model) bootstrapRepoHost() string {
	if host := strings.TrimSpace(m.repoBootstrapHost); host != "" {
		return host
	}
	url := m.bootstrapRepoURL()
	if url == "" {
		return ""
	}
	if parsed := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://"); parsed != "" {
		if slash := strings.IndexByte(parsed, '/'); slash >= 0 {
			return strings.ToLower(parsed[:slash])
		}
		return strings.ToLower(parsed)
	}
	return ""
}

func (m model) handleAssetFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.assetFilterInput = false
		m.pushLog("Asset filter input closed")
		return m, nil
	case tea.KeyEnter:
		m.assetFilterInput = false
		m.normalizeAssetCursor()
		m.pushLog("Asset filter applied: " + m.currentAssetFilterLabel())
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.assetFilter = trimLastRune(m.assetFilter)
		m.normalizeAssetCursor()
		return m, nil
	}

	if len(msg.Runes) > 0 && !msg.Alt {
		m.assetFilter += string(msg.Runes)
		m.normalizeAssetCursor()
	}
	return m, nil
}

func (m model) handleSettingsRepoInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.settingsRepoEditing = false
		m.syncSettingsDirty()
		m.pushLog("Repo URL input closed")
		return m, nil
	case tea.KeyEnter:
		m.settingsRepoEditing = false
		m.syncSettingsDirty()
		m.pushLog("Repo URL updated: " + fallbackValue(strings.TrimSpace(m.settingsRepoInput), "<empty>"))
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.settingsRepoInput = trimLastRune(m.settingsRepoInput)
		m.syncSettingsDirty()
		return m, nil
	}

	if len(msg.Runes) > 0 && !msg.Alt {
		m.settingsRepoInput += string(msg.Runes)
		m.syncSettingsDirty()
	}
	return m, nil
}

func (m model) handleSettingsIdleTimeoutInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.settingsIdleTimeoutEditing = false
		m.syncSettingsDirty()
		return m, nil
	case tea.KeyEnter:
		m.settingsIdleTimeoutEditing = false
		m.syncSettingsDirty()
		m.pushLog("服务空闲超时已更新: " + fallbackValue(strings.TrimSpace(m.settingsIdleTimeoutInput), "30m"))
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.settingsIdleTimeoutInput = trimLastRune(m.settingsIdleTimeoutInput)
		m.syncSettingsDirty()
		return m, nil
	}
	if len(msg.Runes) > 0 && !msg.Alt {
		m.settingsIdleTimeoutInput += string(msg.Runes)
		m.syncSettingsDirty()
	}
	return m, nil
}

func (m *model) startPullRun() tea.Cmd {
	if m.observedOperationID != "" {
		m.pushLog("当前 project 已有操作进行中，不能重复 pull/push")
		return nil
	}
	stream := make(chan tea.Msg, 64)
	ctx, cancel := context.WithCancel(context.Background())
	m.runningPull = true
	m.runMode = "pull"
	m.runProgress = nil
	m.runEvents = nil
	m.runPinLine = ""
	m.runResult = nil
	m.pushResult = nil
	m.runErr = nil
	m.runStream = stream
	m.runCtx = ctx
	m.runCancel = cancel
	m.pushLog("Run page started pull")
	return tea.Batch(startWorkspacePullRunCmd(ctx, m.workspace(), stream), waitRunMsg(stream))
}

func (m *model) beginPushConfirmation() tea.Cmd {
	if m.observedOperationID != "" {
		m.pushLog("当前 project 已有操作进行中，不能重复 pull/push")
		return nil
	}
	if m.pushPreviewLoad.busy() {
		return nil
	}
	gen := m.pushPreviewLoad.beginGen()
	m.pushStage = "loading"
	m.pushPreview = nil
	m.pushPreviewErr = nil
	m.pushResult = nil
	m.runErr = nil
	m.pushLog("Push 预览加载中…")
	return loadPushPreviewCmd(m.workspace(), gen)
}

func loadPushPreviewCmd(workspace app.Workspace, loadGen uint64) tea.Cmd {
	return func() tea.Msg {
		preview, err := previewPushOperation(workspace)
		return pushPreviewLoadedMsg{preview: preview, err: err, loadGen: loadGen}
	}
}

func (m model) handlePushStageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.pushStage {
	case "loading":
		switch msg.String() {
		case "n", "esc":
			m.pushPreviewLoad.clear()
			m.pushStage = ""
			m.pushLog("Push 预览已取消")
			return m, nil
		}
		return m, nil
	case "summary":
		return m.handlePushSummaryKey(msg)
	case "confirm":
		return m.handlePushConfirmKey(msg)
	}
	return m, nil
}

func (m model) handlePushSummaryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.pushPreviewErr != nil {
			return m, nil
		}
		m.pushStage = "confirm"
		m.pushLog("Push 进入最终确认")
		return m, nil
	case "n", "esc":
		m.pushStage = ""
		m.pushPreview = nil
		m.pushPreviewErr = nil
		m.pushLog("Push 已取消")
		return m, nil
	}
	return m, nil
}

func (m model) handlePushConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.pushStage = "running"
		m.pushPreview = nil
		m.pushPreviewErr = nil
		return m, m.startPushRun()
	case "n", "esc":
		m.pushStage = "summary"
		m.pushLog("Push 最终确认已取消，返回摘要")
		return m, nil
	}
	return m, nil
}

func (m *model) startPushRun() tea.Cmd {
	stream := make(chan tea.Msg, 64)
	ctx, cancel := context.WithCancel(context.Background())
	m.runningPull = true
	m.runMode = "push"
	m.runProgress = nil
	m.runEvents = nil
	m.runPinLine = ""
	m.runResult = nil
	m.pushResult = nil
	m.runErr = nil
	m.runStream = stream
	m.runCtx = ctx
	m.runCancel = cancel
	m.pushLog("Run page started push")
	return tea.Batch(startPushRunCmd(ctx, m.workspace(), stream), waitRunMsg(stream))
}

func (m *model) beginRemoveSelection() {
	if m.assets == nil || len(app.ListEnabledBundles(m.assets)) == 0 {
		m.pushLog("没有可删除的已启用 bundle（enabled_bundles 为空）")
		return
	}
	m.removeStage = "select"
	m.removeCursor = 0
	m.removeFilter = ""
	m.removeFilterInput = false
	m.removeTarget = nil
	m.removeResult = nil
	m.removeErr = nil
	m.pushLog("Remove 选择器已打开")
}

func (m model) enabledRemoveBundles() []app.AssetBundleOption {
	if m.assets == nil {
		return nil
	}
	bundles := app.ListEnabledBundles(m.assets)
	filter := strings.ToLower(strings.TrimSpace(m.removeFilter))
	if filter == "" {
		return bundles
	}
	filtered := make([]app.AssetBundleOption, 0, len(bundles))
	for _, bo := range bundles {
		haystack := strings.ToLower(strings.Join([]string{bo.Name, bo.Vault, bo.Description}, " "))
		if strings.Contains(haystack, filter) {
			filtered = append(filtered, bo)
		}
	}
	return filtered
}

func (m model) handleRemoveStageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.removeStage {
	case "select":
		return m.handleRemoveSelectKey(msg)
	case "confirm":
		return m.handleRemoveConfirmKey(msg)
	}
	return m, nil
}

func (m model) handleRemoveSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	bundles := m.enabledRemoveBundles()
	switch msg.String() {
	case "esc":
		m.removeStage = ""
		m.removeTarget = nil
		m.removeFilter = ""
		m.pushLog("Remove 选择已取消")
		return m, nil
	case "/":
		m.removeFilterInput = true
		m.pushLog("Remove 筛选输入已打开")
		return m, nil
	case "c":
		if strings.TrimSpace(m.removeFilter) != "" {
			m.removeFilter = ""
			if m.removeCursor >= len(m.enabledRemoveBundles()) {
				m.removeCursor = 0
			}
			m.pushLog("Remove 筛选已清空")
		}
		return m, nil
	case "j", "down":
		if len(bundles) == 0 {
			return m, nil
		}
		m.removeCursor++
		if m.removeCursor >= len(bundles) {
			m.removeCursor = len(bundles) - 1
		}
		return m, nil
	case "k", "up":
		if len(bundles) == 0 {
			return m, nil
		}
		m.removeCursor--
		if m.removeCursor < 0 {
			m.removeCursor = 0
		}
		return m, nil
	case "enter", " ":
		if len(bundles) == 0 {
			return m, nil
		}
		if m.removeCursor < 0 || m.removeCursor >= len(bundles) {
			m.removeCursor = 0
		}
		target := bundles[m.removeCursor]
		m.removeTarget = &target
		m.removeStage = "confirm"
		m.pushLog(fmt.Sprintf("Remove 目标选中: bundle %s (%d 成员)", target.Name, len(target.Members)))
		return m, nil
	}
	return m, nil
}

func (m model) handleRemoveConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if m.removeTarget == nil {
			m.removeStage = ""
			return m, nil
		}
		m.removeStage = "running"
		return m, m.startRemoveRun()
	case "n", "esc":
		m.removeStage = "select"
		m.removeTarget = nil
		m.pushLog("Remove 取消，返回选择器")
		return m, nil
	}
	return m, nil
}

func (m model) handleRemoveFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.removeFilterInput = false
		m.pushLog("Remove 筛选输入关闭")
		return m, nil
	case tea.KeyEnter:
		m.removeFilterInput = false
		m.removeCursor = 0
		m.pushLog("Remove 筛选应用: " + m.currentRemoveFilterLabel())
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.removeFilter = trimLastRune(m.removeFilter)
		return m, nil
	}

	if len(msg.Runes) > 0 && !msg.Alt {
		m.removeFilter += string(msg.Runes)
	}
	return m, nil
}

func (m *model) startRemoveRun() tea.Cmd {
	if m.removeTarget == nil {
		return nil
	}
	stream := make(chan tea.Msg, 64)
	m.runningRemove = true
	m.runMode = "remove"
	m.runProgress = nil
	m.runEvents = nil
	m.runPinLine = ""
	m.runResult = nil
	m.runErr = nil
	m.removeResult = nil
	m.removeErr = nil
	m.runStream = stream
	input := app.RemoveBundleInput{
		ProjectRoot: m.projectRoot,
		Plane:       m.plane,
		BundleName:  m.removeTarget.Name,
		Members:     append([]app.AssetSelectionItem(nil), m.removeTarget.Members...),
		Confirmed:   true,
	}
	m.pushLog(fmt.Sprintf("Run page started remove: bundle %s", input.BundleName))
	return tea.Batch(startRemoveRunCmd(input, stream), waitRunMsg(stream))
}

func startRemoveRunCmd(input app.RemoveBundleInput, stream chan<- tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			result, err := runRemoveOperation(input, app.ReporterFunc(func(event app.OperationEvent) {
				stream <- removeEventMsg{event: event}
			}))
			stream <- removeCompletedMsg{result: result, err: err}
			close(stream)
		}()
		return nil
	}
}

func (m *model) startUpdateCheck() tea.Cmd {
	m.runMode = "update"
	m.updateStage = "checking"
	m.updateResult = nil
	m.updateErr = nil
	m.updateDoneVersion = ""
	m.pushLog("Run page started update check")
	currentVersion := m.currentVersion
	return func() tea.Msg {
		result, err := updateCheckOperation(currentVersion)
		return updateCheckedMsg{result: result, err: err}
	}
}

func (m *model) startUpdateApply() tea.Cmd {
	m.updateStage = "running"
	m.updatingBinary = true
	m.pushLog("Run page started update apply")
	currentVersion := m.currentVersion
	target := ""
	if m.updateResult != nil {
		target = m.updateResult.LatestVersion
	}
	return func() tea.Msg {
		err := updateDoUpdateOperation(currentVersion, target)
		return updateDoneMsg{targetVersion: target, err: err}
	}
}

func (m model) handleVaultInferenceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.vaultInference == nil {
			return m, nil
		}
		gen := m.vaultApplyLoad.beginGen()
		m.pushLog(fmt.Sprintf("Applying inferred vault project %s...", m.vaultInference.ProjectName))
		return m, applyVaultProjectCmd(m.projectRoot, gen)
	case "n", "esc":
		m.vaultInferenceDismissed = true
		m.pushLog("已跳过 vault project 推断")
		if m.overview != nil && !m.overview.ProjectConfigReady {
			m.localProjectInitConfirm = true
			m.pushLog("等待确认后再初始化当前目录")
		}
		return m, nil
	}
	return m, nil
}

func (m model) handleLocalProjectInitConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.localProjectLoad.busy() {
			return m, nil
		}
		m.localProjectInitConfirm = false
		gen := m.localProjectLoad.beginGen()
		m.pushLog("用户已确认，初始化当前目录...")
		return m, ensureLocalProjectCmd(m.projectRoot, gen)
	case "n", "esc":
		m.localProjectInitConfirm = false
		m.pushLog("已取消初始化；当前目录未写入任何 Dec 配置")
		return m, nil
	}
	return m, nil
}

func (m model) handleUpdateStageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.updateStage {
	case "confirm":
		switch msg.String() {
		case "y":
			return m, m.startUpdateApply()
		case "n", "esc":
			m.updateStage = ""
			m.updateResult = nil
			m.updateErr = nil
			m.updateDoneVersion = ""
			m.pushLog("Update 取消")
			return m, nil
		}
	case "done":
		switch msg.String() {
		case "esc", "enter", " ", "q":
			m.updateStage = ""
			m.updateResult = nil
			m.updateErr = nil
			m.updateDoneVersion = ""
			m.pushLog("Update 面板关闭")
			return m, nil
		}
	case "checking", "running":
		// 忙碌状态忽略所有输入，避免并发操作
		return m, nil
	}
	return m, nil
}

func (m model) currentRemoveFilterLabel() string {
	filter := strings.TrimSpace(m.removeFilter)
	if filter == "" {
		return "<none>"
	}
	return filter
}

func (m *model) recordRunEvent(event app.OperationEvent) {
	if event.Progress != nil {
		progress := *event.Progress
		m.runProgress = &progress
	}
	m.updateRunPinLine(event)
	for _, line := range splitRunMessage(event.Message) {
		m.runEvents = append(m.runEvents, line)
		if len(m.runEvents) > 12 {
			m.runEvents = append([]string(nil), m.runEvents[len(m.runEvents)-12:]...)
		}
		m.pushLog(line)
	}
}

func splitRunMessage(message string) []string {
	normalized := strings.ReplaceAll(strings.TrimSpace(message), "\r\n", "\n")
	if normalized == "" {
		return nil
	}
	parts := strings.Split(normalized, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func (m *model) pushLog(line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	m.logs = append(m.logs, trimmed)
	if len(m.logs) > 8 {
		m.logs = append([]string(nil), m.logs[len(m.logs)-8:]...)
	}
}

func (m model) renderHomePage(width int) string {
	if m.overviewErr != nil {
		return shellWarnStyle.Render("Failed to load overview") + "\n\n" + m.overviewErr.Error()
	}
	if m.overview == nil {
		return shellMutedStyle.Render("Loading project overview...")
	}

	lines := []string{}
	if notice := strings.TrimSpace(m.vaultAutoApplyNotice); notice != "" {
		lines = append(lines, shellGoodStyle.Render(notice))
	}
	if m.vaultApplyLoad.busy() {
		lines = append(lines, shellMutedStyle.Render("正在从 vault 应用 project..."))
	} else if m.hasVaultInferencePrompt() {
		inf := m.vaultInference
		subject := fmt.Sprintf("Bundles (%d): %s", len(inf.EnabledBundles), formatInferenceBundleNames(inf.EnabledBundles))
		title := "检测到项目配置，是否应用？"
		if inf.Model == "p" {
			title = "检测到同名家 P，是否绑定？"
			subject = fmt.Sprintf("家 P: %s · requires (%d): %s", inf.HomeProject, len(inf.RequiredProjects), formatInferenceBundleNames(inf.RequiredProjects))
		}
		lines = append(lines,
			shellWarnStyle.Render(title),
			fmt.Sprintf("项目: %s", inf.ProjectName),
			subject,
			shellMutedStyle.Render("y/Enter 应用 · n 跳过"),
			"",
		)
	} else if m.localProjectInitConfirm {
		lines = append(lines,
			shellWarnStyle.Render("当前目录尚未初始化，是否创建 Dec 项目配置？"),
			fmt.Sprintf("目录: %s", m.projectRoot),
			shellWarnStyle.Render("将写入: .dec/config.yaml（可能同时创建 .dec/vars.yaml）"),
			shellMutedStyle.Render("y/Enter 确认初始化 · n/Esc 保持目录不变"),
			"",
		)
	}

	next := suggestNextAction(m.overview, m.hasVaultInferencePrompt(), m.localProjectInitConfirm)
	if m.createStage != "" {
		return m.renderCreateLocalAsset(width)
	}
	lines = append(lines,
		shellAccentStyle.Render("下一步"),
		next,
		"",
		shellTitleStyle.Render("项目状态"),
		fmt.Sprintf("项目名: %s", formatProjectNameDisplay(m.overview)),
		fmt.Sprintf("仓库: %s · %s", formatReady(m.overview.RepoConnected, "已连接", "未连接"), fallbackValue(m.overview.RepoRemoteURL, "未连接")),
		fmt.Sprintf("配置: %s · 变量: %s", formatReady(m.overview.ProjectConfigReady, "已初始化", "未初始化"), formatReady(m.overview.VarsFileReady, "已存在", "未生成")),
		func() string {
			if m.overview.Model == "p" {
				if m.plane == app.WorkspaceUser {
					return fmt.Sprintf("P: 可选 %d 个 · 用户已启用 %d 个", countOverviewAvailableBundles(m.overview), countOverviewEnabledBundles(m.overview))
				}
				return fmt.Sprintf("家 P: %s · requires %d 个", fallbackValue(m.overview.HomeProject, "<未绑定>"), len(m.overview.RequiredProjects))
			}
			return fmt.Sprintf("Bundle: 可选 %d 个 · 已启用 %d 个", countOverviewAvailableBundles(m.overview), countOverviewEnabledBundles(m.overview))
		}(),
		fmt.Sprintf("IDE: %s · 编辑器: %s", fallbackValue(strings.Join(m.overview.IDEs, ", "), "<none>"), fallbackValue(m.overview.Editor, "未配置")),
	)
	if warn := formatWarnings(m.overview.IDEWarnings); !strings.HasSuffix(warn, "无") {
		lines = append(lines, warn)
	}
	return wrapLines(width, lines)
}

func (m model) renderBundlesPage(width, height int) string {
	if m.assetsErr != nil {
		return shellWarnStyle.Render("无法加载 bundle 选择") + "\n\n" + m.assetsErr.Error()
	}
	if m.assets == nil {
		return shellMutedStyle.Render("Loading bundle selection...")
	}

	summary := []string{}
	if m.configInitMode {
		summary = append(summary, shellTitleStyle.Render("项目初始化 — 绑定家 P 并选择直接 requires"))
	}
	status := fmt.Sprintf("%d/%d 项目已启用", len(m.bundleSelection), len(m.assets.Bundles))
	if m.assets.Model == "p" {
		status = fmt.Sprintf("%d/%d P 已选择（H=家 P，其余为直接 requires）", len(m.bundleSelection), len(m.assets.Bundles))
	}
	if m.plane == app.WorkspaceUser {
		status = fmt.Sprintf("%d/%d 用户 P 已启用", len(m.bundleSelection), len(m.assets.Bundles))
	}
	if filter := m.currentAssetFilterLabel(); filter != "<none>" {
		status += " · 筛选: " + filter
	}
	summary = append(summary, status)
	if m.assetsDirty {
		summary = append(summary, shellWarnStyle.Render("有未保存修改，按 s 保存"))
	}
	if m.assetFilterInput {
		summary = append(summary, shellMutedStyle.Render("筛选输入中：Enter 应用 · Esc 退出"))
	}
	if !m.assets.ExistingConfig {
		summary = append(summary, shellMutedStyle.Render("首次保存会创建 .dec/config.yaml"))
	}

	rows := m.assetTreeVisibleCount()
	if len(m.assets.Bundles) == 0 {
		return wrapLines(width, append(summary, "仓库中还没有可选 P。"))
	}
	if rows == 0 {
		return wrapLines(width, append(summary, "当前筛选没有结果。"))
	}

	listBudget := height - len(summary)
	if listBudget < 3 {
		listBudget = 3
	}
	// split 时列表占半屏高度约等于剩余行（标题+窗口）
	list := m.renderAssetList(listBudget)
	detail := m.renderAssetDetails()
	return joinSections(wrapLines(width, summary), renderSplitPane(width, list, detail))
}

func (m model) renderProjectPage(width int) string {
	if m.projectSettingsErr != nil {
		return shellWarnStyle.Render("Failed to load project settings") + "\n\n" + m.projectSettingsErr.Error()
	}
	if m.projectSettings == nil {
		if m.overviewErr != nil {
			return shellWarnStyle.Render("Failed to load project details") + "\n\n" + m.overviewErr.Error()
		}
		return shellMutedStyle.Render("Loading project settings...")
	}

	modeLabel := "继承全局"
	if m.projectSettingsOverride {
		modeLabel = "项目显式覆盖"
	}

	summary := []string{
		fmt.Sprintf("IDE 模式: %s · 生效: %s", modeLabel, fallbackValue(strings.Join(projectEffectivePreview(m.projectSettings, m.projectSettingsOverride, m.projectSettingsSelectedIDEs), ", "), "<none>")),
	}
	if m.overview != nil {
		summary = append(summary, fmt.Sprintf("已启用 bundle (%d): %s", countOverviewEnabledBundles(m.overview), formatOverviewEnabledBundleNames(m.overview)))
	}
	if !m.projectSettings.ProjectConfigReady {
		summary = append(summary, shellMutedStyle.Render("尚未初始化 .dec/config.yaml，按 i 发起初始化确认。"))
	} else if m.overview != nil {
		summary = append(summary, fmt.Sprintf("project_name: %s", formatProjectNameDisplay(m.overview)))
	}
	if m.projectSettingsDirty {
		summary = append(summary, shellWarnStyle.Render("有未保存修改，按 s 保存"))
	}
	if m.savingProjectSettings {
		summary = append(summary, shellWarnStyle.Render("正在保存项目设置..."))
	}
	if m.lastEditErr != nil {
		summary = append(summary, shellWarnStyle.Render("编辑器返回错误: "+m.lastEditErr.Error()))
	}
	if outcome := m.renderAddSecretOutcome(); outcome != "" {
		summary = append(summary, outcome)
	}
	if warn := formatWarnings(m.projectSettings.IDEWarnings); !strings.HasSuffix(warn, "无") {
		summary = append(summary, warn)
	}

	list := m.renderProjectSettingsList()
	detail := m.renderProjectSettingsDetails()
	parts := []string{wrapLines(width, summary), renderSplitPane(width, list, detail)}
	if m.localProjectInitConfirm {
		parts = append([]string{strings.Join([]string{
			shellWarnStyle.Render("确认初始化当前目录？"),
			fmt.Sprintf("目录: %s", m.projectRoot),
			shellWarnStyle.Render("将写入: .dec/config.yaml（可能同时创建 .dec/vars.yaml）"),
			shellMutedStyle.Render("y/Enter 确认初始化 · n/Esc 保持目录不变"),
		}, "\n")}, parts...)
	}
	if m.addSecretStage != "" {
		parts = append(parts, m.renderAddSecretBlock())
	}
	if varsBlock := m.renderProjectVarsBlock(); varsBlock != "" {
		parts = append(parts, varsBlock)
	}
	return joinSections(parts...)
}

func (m model) renderProjectSettingsList() string {
	lines := []string{shellTitleStyle.Render("项目 IDE")}
	override := m.projectSettingsOverride
	checked := " "
	if override {
		checked = "x"
	}
	overrideLine := fmt.Sprintf("%s [%s] 覆盖全局 IDE", settingsCursorMarker(m.projectSettingsCursor == 0 && m.focus != focusSidebar), checked)
	switch {
	case m.projectSettingsCursor == 0 && m.focus != focusSidebar:
		lines = append(lines, shellSelectedRow.Render(overrideLine))
	case override:
		lines = append(lines, shellEnabledRow.Render(overrideLine))
	default:
		lines = append(lines, shellLogStyle.Render(overrideLine))
	}
	if m.projectSettings == nil {
		return strings.Join(lines, "\n")
	}
	for idx, ideName := range m.projectSettings.AvailableIDEs {
		selected := override && settingsContainsIDE(m.projectSettingsSelectedIDEs, ideName)
		mark := " "
		if selected {
			mark = "x"
		}
		line := fmt.Sprintf("%s [%s] %s", settingsCursorMarker(m.projectSettingsCursor == idx+1 && m.focus != focusSidebar), mark, ideName)
		switch {
		case m.projectSettingsCursor == idx+1 && m.focus != focusSidebar:
			lines = append(lines, shellSelectedRow.Render(line))
		case selected:
			lines = append(lines, shellEnabledRow.Render(line))
		default:
			lines = append(lines, shellLogStyle.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) renderProjectSettingsDetails() string {
	lines := []string{shellTitleStyle.Render("详情")}
	if m.projectSettings == nil {
		return strings.Join(lines, "\n")
	}
	if m.projectSettingsCursor == 0 {
		lines = append(lines,
			fmt.Sprintf("模式: %s", formatReady(m.projectSettingsOverride, "项目覆盖", "继承全局")),
			fmt.Sprintf("全局默认: %s", fallbackValue(strings.Join(normalizedStringList(m.projectSettings.GlobalIDEs), ", "), "<未配置>")),
			fmt.Sprintf("生效 IDE: %s", fallbackValue(strings.Join(projectEffectivePreview(m.projectSettings, m.projectSettingsOverride, m.projectSettingsSelectedIDEs), ", "), "<none>")),
			shellMutedStyle.Render("space 切换 · c 恢复继承"),
		)
	} else {
		ideName := m.currentProjectSettingsIDEName()
		state := "未选中"
		if m.projectSettingsOverride && settingsContainsIDE(m.projectSettingsSelectedIDEs, ideName) {
			state = "已选中"
		}
		lines = append(lines,
			fmt.Sprintf("IDE: %s", ideName),
			fmt.Sprintf("状态: %s", state),
		)
		if !m.projectSettingsOverride {
			lines = append(lines, shellMutedStyle.Render("先在首行开启项目覆盖"))
		} else {
			lines = append(lines, shellMutedStyle.Render("space 切换"))
		}
	}
	return strings.Join(lines, "\n")
}

// projectEffectivePreview 返回本地编辑态下应当生效的 IDE 列表。
// 覆盖模式下使用本地选择；继承模式下展示 state.EffectiveIDEs（已由 ResolveEffectiveIDEs 解析过）。
func projectEffectivePreview(state *app.ProjectSettingsState, override bool, selected []string) []string {
	if override {
		return normalizedStringList(selected)
	}
	if state == nil {
		return nil
	}
	if len(state.GlobalIDEs) > 0 {
		return normalizedStringList(state.GlobalIDEs)
	}
	return normalizedStringList(state.EffectiveIDEs)
}

// renderProjectVarsBlock 渲染 Project 页下方的 "项目变量" 区块。
// 纯只读；写入通过 `e` 键挂起 TUI 用外部编辑器完成。
func (m model) renderProjectVarsBlock() string {
	lines := []string{shellTitleStyle.Render("项目变量")}

	if m.projectVarsErr != nil {
		lines = append(lines, shellWarnStyle.Render("加载变量失败: "+m.projectVarsErr.Error()))
		return strings.Join(lines, "\n")
	}

	if m.projectVars == nil {
		lines = append(lines, shellMutedStyle.Render("Loading project vars..."))
		return strings.Join(lines, "\n")
	}

	view := m.projectVars
	fileLine := fmt.Sprintf("%s", compactPath(view.VarsPath, 48))
	if view.VarsFileReady {
		fileLine += shellMutedStyle.Render(" (已存在)")
	} else {
		fileLine += shellWarnStyle.Render(" (未生成)")
	}
	lines = append(lines, fileLine)
	lines = append(lines, shellMutedStyle.Render(fmt.Sprintf("编辑器: %s · e 打开外部编辑器", fallbackValue(view.EditorCommand, "vim"))))
	lines = append(lines, shellMutedStyle.Render("A 登记 secret（相对 .secrets 同步根）"))

	for _, w := range view.Warnings {
		lines = append(lines, shellWarnStyle.Render(w))
	}

	if !view.CacheExists {
		lines = append(lines, shellMutedStyle.Render(".dec/cache 尚不存在：请先到 Run 页执行 pull"))
	}
	if len(view.UsedPlaceholders) == 0 {
		if view.CacheExists {
			lines = append(lines, shellMutedStyle.Render("当前资产中未检测到 {{VAR_NAME}} 占位符。"))
		}
		return strings.Join(lines, "\n")
	}

	lines = append(lines, fmt.Sprintf("占位符: %d · 缺失: %d", len(view.UsedPlaceholders), len(view.MissingPlaceholders())))
	const maxPlaceholders = 8
	shown := view.UsedPlaceholders
	if len(shown) > maxPlaceholders {
		shown = shown[:maxPlaceholders]
	}
	for _, name := range shown {
		status := view.ResolvedVars[name]
		var row string
		switch status.Source {
		case app.PlaceholderSourceProject:
			row = fmt.Sprintf("  %s = %s  (project)", name, truncateVarValue(status.Value))
			row = shellEnabledRow.Render(row)
		case app.PlaceholderSourceGlobal:
			row = fmt.Sprintf("  %s = %s  (global)", name, truncateVarValue(status.Value))
			row = shellMutedStyle.Render(row)
		default:
			row = shellWarnStyle.Render(fmt.Sprintf("  %s = <缺失>  (missing)", name))
		}
		lines = append(lines, row)
	}
	if len(view.UsedPlaceholders) > maxPlaceholders {
		lines = append(lines, shellMutedStyle.Render(fmt.Sprintf("  … 另有 %d 个占位符", len(view.UsedPlaceholders)-maxPlaceholders)))
	}

	return strings.Join(lines, "\n")
}

// truncateVarValue 把过长的变量值截断显示，避免一行撑破区块。
func truncateVarValue(v string) string {
	const maxW = 40
	if lipgloss.Width(v) <= maxW {
		return v
	}
	runes := []rune(v)
	if len(runes) <= maxW-1 {
		return v
	}
	return string(runes[:maxW-1]) + "…"
}

func (m model) renderRunPage(width int) string {
	if m.repoBootstrapStage != "" {
		sections := []string{
			m.renderRunHeader(),
			m.renderRepoBootstrapBlock(),
		}
		if m.runErr != nil {
			sections = append(sections, "", shellWarnStyle.Render("Pull 错误: "+app.StripRepoAuthMarker(m.runErr.Error())))
		}
		return wrapLines(width, sections)
	}
	if m.pushStage == "loading" || m.pushStage == "summary" || m.pushStage == "confirm" {
		return m.renderRunPushPage(width)
	}
	if m.removeStage == "select" || m.removeStage == "confirm" {
		return m.renderRunRemovePage(width)
	}

	sections := []string{
		m.renderRunHeader(),
		m.renderRunActionBar(),
	}
	sections = append(sections, m.renderRunStateBlock(width)...)

	if m.runShowHelp {
		sections = append(sections, m.renderRunHelpPanel()...)
	}

	if m.updateStage != "" {
		sections = append(sections, "")
		sections = append(sections, m.renderUpdatePanel()...)
	}

	if len(m.runEvents) > 0 {
		sections = append(sections, "", shellTitleStyle.Render("事件"))
		const maxEvents = 6
		start := 0
		if len(m.runEvents) > maxEvents {
			start = len(m.runEvents) - maxEvents
		}
		for _, line := range m.runEvents[start:] {
			sections = append(sections, formatRunLogLine(line))
		}
	}

	return wrapLines(width, sections)
}

func (m model) renderRunHeader() string {
	mode := "就绪"
	switch {
	case m.observedOperationID != "":
		mode = fmt.Sprintf("%s 正在 %s（旁观）", m.observedOperationFacade, strings.ToUpper(m.runMode))
	case m.runningPull && m.runMode == "push":
		mode = "Push 执行中"
	case m.runningPull:
		mode = "Pull 执行中"
	case m.runningRemove:
		mode = "Remove 执行中"
	case m.updatingBinary:
		mode = "Update 执行中"
	case m.runErr != nil && m.runMode == "push":
		mode = "Push 失败"
	case m.runErr != nil:
		mode = "Pull 失败"
	case m.pushResult != nil:
		mode = "Push 完成"
	case m.runResult != nil:
		mode = "Pull 完成"
	case m.removeErr != nil:
		mode = "Remove 失败"
	case m.removeResult != nil:
		mode = "Remove 完成"
	}
	return shellTitleStyle.Render("Run · " + mode)
}

func (m model) renderRunActionBar() string {
	switch {
	case m.observedOperationID != "":
		return shellMutedStyle.Render("该 project 写操作已锁定 · 正在旁观进度 · ? 帮助")
	case m.runningPull && m.runMode == "push":
		return shellMutedStyle.Render("Esc 取消 push  ·  ? 帮助")
	case m.runningPull:
		return shellMutedStyle.Render("Esc 取消 pull  ·  ? 帮助")
	case m.runningRemove, m.updatingBinary:
		return shellMutedStyle.Render("? 帮助")
	default:
		return shellMutedStyle.Render("p Pull  ·  P Push  ·  u Update  ·  ? 帮助")
	}
}

func (m model) renderRunStateBlock(width int) []string {
	if m.runningPull || m.runningRemove || m.observedOperationID != "" {
		return m.renderRunActiveBlock(width)
	}
	if m.runResult == nil && m.pushResult == nil && m.runErr == nil && m.removeResult == nil && m.removeErr == nil {
		return m.renderRunIdleGuide()
	}
	lines := m.renderRunLastResult()
	if plan := m.renderPullPlanLines(); len(plan) > 0 {
		lines = append(lines, "")
		lines = append(lines, plan...)
	}
	return lines
}

// renderPullPlanLines 说明「按下 p 会拉取什么」。
//
// 计划一律按磁盘上的 enabled_bundles 计算（也就是 m.assets 里的 Enabled 标记），
// 因为 pull 读的是磁盘配置；Bundles 页里刚勾还没保存的改动只作为警告提示，
// 避免用户以为勾选立刻生效——这正是"勾了 bundle 却没拉到"的来源。
func (m model) renderPullPlanLines() []string {
	if m.assets == nil {
		return nil
	}
	enabled := app.ListEnabledBundles(m.assets)
	names := make([]string, 0, len(enabled))
	for _, bo := range enabled {
		names = append(names, bo.Name)
	}

	lines := []string{shellTitleStyle.Render("本次 Pull 计划")}
	if len(names) == 0 {
		emptyLabel := "⚠ 当前无启用 bundle，请先到 Bundles 页勾选并按 s 保存"
		if m.assets.Model == "p" {
			emptyLabel = "⚠ 当前无可用 P，请先到 Bundles 页选择并按 s 保存"
		}
		lines = append(lines, shellWarnStyle.Render(emptyLabel))
	} else {
		label := "P"
		if m.assets.Model != "p" {
			label = "bundle"
		}
		lines = append(lines, fmt.Sprintf("Dec  %s: %s（→ .dec/cache/）", label,
			strings.Join(names, ", ")))
	}
	if secretsPlan := m.renderSecretsSyncPlanLines(); len(secretsPlan) > 0 {
		lines = append(lines, secretsPlan...)
	}
	if pending := m.pendingBundleChanges(); pending != "" {
		lines = append(lines, shellWarnStyle.Render("⚠ Bundles 页有未保存的勾选（"+pending+"），按 s 保存后才会生效"))
	}
	return lines
}

func (m model) renderSecretsSyncPlanLines() []string {
	if m.overview == nil || !m.overview.ProjectConfigReady {
		return nil
	}
	targets, err := serviceapi.ListSecretSyncTargets(m.projectRoot)
	if err != nil || len(targets) == 0 {
		return []string{shellMutedStyle.Render("Secrets  无同步目标（请启用 bundle）")}
	}
	lines := []string{shellMutedStyle.Render("Secrets  拉取以下目标（不清理本地文件）：")}
	for _, t := range targets {
		lines = append(lines, fmt.Sprintf("  · %s", t.Label))
	}
	return lines
}

// pendingBundleChanges 描述 TUI 内 bundleSelection 与磁盘 enabled_bundles 的差异。
// 无差异时返回空串。
func (m model) pendingBundleChanges() string {
	if m.assets == nil || !m.assetsDirty {
		return ""
	}
	onDisk := make(map[string]struct{})
	for _, bo := range m.assets.Bundles {
		if bo.Enabled {
			onDisk[bo.Name] = struct{}{}
		}
	}
	selected := make(map[string]struct{}, len(m.bundleSelection))
	for _, name := range m.bundleSelection {
		selected[name] = struct{}{}
	}

	var added, removed []string
	for _, bo := range m.assets.Bundles {
		_, wasEnabled := onDisk[bo.Name]
		_, nowEnabled := selected[bo.Name]
		switch {
		case nowEnabled && !wasEnabled:
			added = append(added, "+"+bo.Name)
		case wasEnabled && !nowEnabled:
			removed = append(removed, "-"+bo.Name)
		}
	}
	changes := append(added, removed...)
	if len(changes) == 0 {
		return ""
	}
	return strings.Join(changes, " ")
}

func (m model) renderRunActiveBlock(width int) []string {
	lines := make([]string, 0, 3)
	if m.runProgress != nil {
		phase := runPhaseLabel(m.runProgress.Phase)
		bar := renderRunProgressBar(m.runProgress.Current, m.runProgress.Total, progressBarWidth(width))
		lines = append(lines, fmt.Sprintf("阶段  %s  %s  %d/%d", phase, bar, m.runProgress.Current, m.runProgress.Total))
	} else {
		lines = append(lines, shellMutedStyle.Render("阶段  准备中…"))
	}
	if pin := strings.TrimSpace(m.runPinLine); pin != "" {
		lines = append(lines, shellWarnStyle.Render("提示  "+pin))
	}
	return lines
}

func (m model) renderRunIdleGuide() []string {
	lines := []string{}
	lines = append(lines, m.renderPullPlanLines()...)
	lines = append(lines, shellMutedStyle.Render("上次  尚无操作记录"))
	return lines
}

func (m model) renderRunLastResult() []string {
	lines := []string{shellTitleStyle.Render("上次结果")}
	if m.runResult != nil {
		lines = append(lines, fmt.Sprintf("Pull  请求 %d · 成功 %d · 失败 %d",
			m.runResult.RequestedCount, m.runResult.PulledCount, m.runResult.FailedCount))
		if m.runResult.Model == "p" {
			lines = append(lines,
				fmt.Sprintf("项目  绑定 %s · 引入 %s", fallbackValue(m.runResult.HomeProject, "<本机>"), fallbackValue(strings.Join(m.runResult.RequiredProjects, ", "), "<none>")),
				fmt.Sprintf("象限  public/global %d · private/global %d · public/local %d · private/local %d",
					m.runResult.Quadrants["public/global"], m.runResult.Quadrants["private/global"],
					m.runResult.Quadrants["public/local"], m.runResult.Quadrants["private/local"]))
			if len(m.runResult.MissingProjects) > 0 {
				lines = append(lines, shellWarnStyle.Render("缺失 P: "+strings.Join(m.runResult.MissingProjects, ", ")))
			}
		}
		// 一排 0 本身不解释任何事情；跳过原因是唯一能说明「为什么没拉」的那句话。
		if reason := strings.TrimSpace(m.runResult.SkippedReason); reason != "" && m.runResult.PulledCount == 0 {
			lines = append(lines, shellWarnStyle.Render("Dec   "+reason))
		}
		secretsLine := fmt.Sprintf("Secrets  落地 %d 个文件 · %d 个 SSH Key", m.runResult.SecretsNoteCount, m.runResult.SecretsSSHKeyCount)
		if m.runResult.SecretsSkippedReason != "" && m.runResult.SecretsNoteCount == 0 && m.runResult.SecretsSSHKeyCount == 0 {
			secretsLine = "Secrets  " + m.runResult.SecretsSkippedReason
		}
		orphanN := len(m.runResult.CleanedAssets) + len(m.runResult.OrphanSecretPaths) + len(m.runResult.OrphanSSHKeys)
		if orphanN > 0 {
			secretsLine += fmt.Sprintf(" · 清理 %d 项孤儿", orphanN)
		}
		lines = append(lines, secretsLine)
		if orphanN > 0 && len(m.runResult.OrphanSecretPaths)+len(m.runResult.OrphanSSHKeys) > 0 {
			for _, p := range m.runResult.OrphanSecretPaths {
				lines = append(lines, shellMutedStyle.Render("  - "+p))
			}
			for _, k := range m.runResult.OrphanSSHKeys {
				lines = append(lines, shellMutedStyle.Render("  - ssh:"+k))
			}
		}
		if grouped := formatRunEventsBySyncTarget(m.runEvents); len(grouped) > 0 {
			lines = append(lines, grouped...)
		}
		lines = append(lines, fmt.Sprintf("IDE   %s", fallbackValue(strings.Join(m.runResult.EffectiveIDEs, ", "), "<none>")))
		if strings.TrimSpace(m.runResult.VersionCommit) != "" {
			lines = append(lines, fmt.Sprintf("Commit %s", m.runResult.VersionCommit))
		}
		for _, warning := range m.runResult.NonFatalWarnings {
			lines = append(lines, shellWarnStyle.Render("⚠ "+warning))
		}
	}
	if m.pushResult != nil {
		if m.pushResult.DecPushedCount > 0 || m.pushResult.DecSkippedReason != "" {
			decLine := fmt.Sprintf("Dec   推送 %d 项", m.pushResult.DecPushedCount)
			if m.pushResult.DecSkippedReason != "" && m.pushResult.DecPushedCount == 0 {
				decLine = "Dec   " + m.pushResult.DecSkippedReason
			}
			lines = append(lines, decLine)
		}
		secretsLine := fmt.Sprintf("Secrets  新建 %d · 更新 %d", m.pushResult.SecretsCreatedCount, m.pushResult.SecretsUpdatedCount)
		if m.pushResult.SecretsSkippedReason != "" && m.pushResult.SecretsCreatedCount+m.pushResult.SecretsUpdatedCount == 0 {
			secretsLine = "Secrets  " + m.pushResult.SecretsSkippedReason
		} else if m.pushResult.SecretsCreatedCount+m.pushResult.SecretsUpdatedCount > 0 {
			secretsLine += "（未删除远端项）"
		}
		lines = append(lines, secretsLine)
		if grouped := formatRunEventsBySyncTarget(m.runEvents); len(grouped) > 0 {
			lines = append(lines, grouped...)
		}
		if strings.TrimSpace(m.pushResult.VersionCommit) != "" {
			lines = append(lines, fmt.Sprintf("Commit %s", m.pushResult.VersionCommit))
		}
	}
	if m.runErr != nil {
		label := "Pull 错误"
		if m.runMode == "push" {
			label = "Push 错误"
		}
		lines = append(lines, shellWarnStyle.Render(label+": "+app.StripRepoAuthMarker(m.runErr.Error())))
	}
	if m.removeResult != nil {
		lines = append(lines, fmt.Sprintf("Remove  bundle %s · %d 成员", m.removeResult.BundleName, m.removeResult.MemberCount))
		if strings.TrimSpace(m.removeResult.VersionCommit) != "" {
			lines = append(lines, fmt.Sprintf("Remove Commit %s", m.removeResult.VersionCommit))
		}
	}
	if m.removeErr != nil {
		lines = append(lines, shellWarnStyle.Render("Remove 错误: "+m.removeErr.Error()))
	}
	return lines
}

func (m model) renderRunHelpPanel() []string {
	return []string{
		"",
		shellTitleStyle.Render("快捷键"),
		shellMutedStyle.Render("p / s  执行 pull"),
		shellMutedStyle.Render("P      推送到远端（两次确认）"),
		shellMutedStyle.Render("删除 / 编辑远端请切到 Remote 页（侧栏 Run 之后）"),
		shellMutedStyle.Render("u      检查并自更新 dec"),
		shellMutedStyle.Render("r      刷新项目概览"),
		shellMutedStyle.Render("Esc    取消进行中的 pull / push"),
		shellMutedStyle.Render("?      开关此帮助"),
	}
}

func (m model) renderRunPushPage(width int) string {
	lines := []string{
		shellTitleStyle.Render("Run · Push"),
		fmt.Sprintf("状态 %s", m.runStatusLabel()),
	}
	switch m.pushStage {
	case "loading":
		lines = append(lines, shellMutedStyle.Render("正在生成 Push 预览… Esc 取消"))
	case "summary":
		lines = append(lines, shellMutedStyle.Render("操作  y/Enter 继续 · n/Esc 取消"))
		lines = append(lines, "")
		lines = append(lines, m.renderPushSummary()...)
	case "confirm":
		lines = append(lines, shellMutedStyle.Render("操作  y 确认推送 · n/Esc 返回摘要"))
		lines = append(lines, "")
		lines = append(lines, m.renderPushConfirm()...)
	}
	return wrapLines(width, lines)
}

func (m model) renderPushSummary() []string {
	lines := []string{shellTitleStyle.Render("Push 摘要")}
	if m.pushPreviewErr != nil {
		lines = append(lines, shellWarnStyle.Render("预览失败: "+m.pushPreviewErr.Error()))
		lines = append(lines, shellMutedStyle.Render("按 n/esc 取消"))
		return lines
	}
	if m.pushPreview == nil {
		lines = append(lines, shellWarnStyle.Render("无预览数据，按 esc 取消。"))
		return lines
	}
	p := m.pushPreview
	lines = append(lines, fmt.Sprintf("Enabled bundles: %d", p.EnabledBundleCount))
	if len(p.EnabledBundleNames) > 0 {
		lines = append(lines, fmt.Sprintf("  %s", strings.Join(p.EnabledBundleNames, ", ")))
	}
	if p.ProjectSecretsName != "" {
		lines = append(lines, fmt.Sprintf("Project secrets: %s", p.ProjectSecretsName))
	}
	secretsLine := fmt.Sprintf("Secrets  %d 个目标", p.SecretsTargetCount)
	if !p.BitwardenConfigured {
		secretsLine += "（Bitwarden 未配置，将跳过）"
	}
	lines = append(lines, secretsLine)
	if p.DecHasChanges {
		lines = append(lines, fmt.Sprintf("Dec cache  有变更（约 %d 项待推送）", p.DecCandidateCount))
		for _, ch := range p.Changes {
			lines = append(lines, fmt.Sprintf("  %s  %s  %s", ch.Op, ch.Path, ch.Quadrant))
		}
	} else if p.DecSkippedReason != "" {
		lines = append(lines, fmt.Sprintf("Dec cache  %s", p.DecSkippedReason))
	} else {
		lines = append(lines, "Dec cache  无本地变更")
	}
	return lines
}

func (m model) renderPushConfirm() []string {
	lines := []string{shellTitleStyle.Render("Push 最终确认")}
	lines = append(lines,
		shellWarnStyle.Render("将更新 Dec Git vault 与 Bitwarden。"),
		shellMutedStyle.Render("Dec 变更将提交并推送；Secrets 只新建或更新，不删除。"),
		shellMutedStyle.Render("y 确认 · n/Esc 返回"),
	)
	return lines
}

func (m model) renderRunRemovePage(width int) string {
	lines := []string{
		shellTitleStyle.Render("Run · Remove"),
		fmt.Sprintf("状态 %s", m.runStatusLabel()),
	}
	switch m.removeStage {
	case "select":
		lines = append(lines, "")
		lines = append(lines, m.renderRemoveSelect()...)
	case "confirm":
		lines = append(lines, "")
		lines = append(lines, m.renderRemoveConfirm()...)
	}
	return wrapLines(width, lines)
}

func (m *model) updateRunPinLine(event app.OperationEvent) {
	msg := strings.TrimSpace(event.Message)
	if msg == "" {
		return
	}
	switch {
	case strings.Contains(msg, "解锁页:"):
		m.runPinLine = msg
	case strings.Contains(msg, "Bitwarden 未解锁"):
		m.runPinLine = msg
	case strings.Contains(msg, "Bitwarden 已解锁"):
		m.runPinLine = msg
	case event.Scope == "push.secrets" && strings.Contains(msg, "解锁页:"):
		m.runPinLine = msg
	case event.Level == app.EventWarn || event.Level == app.EventError:
		m.runPinLine = msg
	case event.Progress != nil && event.Progress.Phase == "done":
		m.runPinLine = msg
	}
}

func runPhaseLabel(phase string) string {
	switch phase {
	case "pull":
		return "Dec cache"
	case "dec":
		return "Dec 推送"
	case "secrets":
		return "Secrets"
	case "install":
		return "IDE 安装"
	case "done":
		return "完成"
	case "validate":
		return "路径校验"
	default:
		if phase == "" {
			return "进行中"
		}
		return phase
	}
}

func progressBarWidth(pageWidth int) int {
	w := pageWidth / 3
	if w < 10 {
		w = 10
	}
	if w > 24 {
		w = 24
	}
	return w
}

func renderRunProgressBar(current, total, width int) string {
	if total <= 0 {
		return "[----]"
	}
	if width < 8 {
		width = 8
	}
	innerWidth := width - 2
	filled := current * innerWidth / total
	if filled > innerWidth {
		filled = innerWidth
	}
	if current > 0 && filled == 0 {
		filled = 1
	}
	inner := strings.Repeat("=", filled) + strings.Repeat("-", innerWidth-filled)
	return "[" + inner + "]"
}

func formatRunLogLine(line string) string {
	if isRunImportantLine(line) {
		return shellWarnStyle.Render("▸ " + line)
	}
	return shellLogStyle.Render("· " + line)
}

func isRunImportantLine(line string) bool {
	return strings.Contains(line, "[auth]") ||
		strings.Contains(line, "解锁页:") ||
		strings.Contains(line, "Bitwarden 未解锁") ||
		strings.Contains(line, "Bitwarden 已解锁") ||
		strings.Contains(line, "Bitwarden 未配置") ||
		strings.Contains(line, "跳过 secrets") ||
		strings.Contains(line, "拉取 project secrets") ||
		strings.Contains(line, "拉取 secrets bundle") ||
		strings.Contains(line, "推送 project secrets") ||
		strings.Contains(line, "推送 secrets bundle") ||
		(strings.Contains(line, "清理") && strings.Contains(line, "孤儿")) ||
		strings.Contains(line, "无法自动打开") ||
		strings.Contains(line, "失败") ||
		strings.Contains(line, "⚠")
}

func formatRunEventsBySyncTarget(events []string) []string {
	var out []string
	for _, line := range events {
		if strings.Contains(line, "拉取 project secrets") ||
			strings.Contains(line, "拉取 secrets bundle") ||
			strings.Contains(line, "推送 project secrets") ||
			strings.Contains(line, "推送 secrets bundle") ||
			strings.Contains(line, "→ .secrets/") {
			out = append(out, shellMutedStyle.Render("  "+line))
		}
	}
	return out
}

func (m model) renderUpdatePanel() []string {
	lines := []string{shellTitleStyle.Render("Update")}
	switch m.updateStage {
	case "checking":
		lines = append(lines, shellMutedStyle.Render(fmt.Sprintf("检查更新中... 当前版本: %s", fallbackValue(m.currentVersion, "未知"))))
	case "confirm":
		if m.updateResult == nil {
			lines = append(lines, shellWarnStyle.Render("检查结果缺失，按 n/esc 返回"))
			return lines
		}
		lines = append(lines,
			fmt.Sprintf("当前版本: %s", m.updateResult.CurrentVersion),
			fmt.Sprintf("远端版本: %s", m.updateResult.LatestVersion),
			shellWarnStyle.Render("⚠️  自更新会替换当前 dec 二进制，属不可逆操作。"),
			shellMutedStyle.Render("按 y 确认下载并覆盖 · n/esc 取消"),
		)
	case "running":
		target := ""
		if m.updateResult != nil {
			target = m.updateResult.LatestVersion
		}
		lines = append(lines, shellMutedStyle.Render(fmt.Sprintf("正在下载并替换二进制到 %s ...", fallbackValue(target, "最新版本"))))
	case "done":
		if m.updateErr != nil {
			lines = append(lines, shellWarnStyle.Render("更新失败: "+m.updateErr.Error()))
			for _, helpLine := range strings.Split(updateNetworkHelp(), "\n") {
				if helpLine == "" {
					continue
				}
				lines = append(lines, shellMutedStyle.Render(helpLine))
			}
			lines = append(lines, shellMutedStyle.Render("按 esc/enter 关闭面板"))
			return lines
		}
		if m.updateResult != nil && !m.updateResult.NeedUpdate {
			lines = append(lines,
				shellGoodStyle.Render(fmt.Sprintf("已是最新版本: %s", m.updateResult.CurrentVersion)),
				shellMutedStyle.Render("按 esc/enter 关闭面板"),
			)
			return lines
		}
		target := m.updateDoneVersion
		if target == "" && m.updateResult != nil {
			target = m.updateResult.LatestVersion
		}
		lines = append(lines,
			shellGoodStyle.Render(fmt.Sprintf("更新成功！已更新到 %s", fallbackValue(target, "最新版本"))),
			shellMutedStyle.Render("按 esc/enter 关闭面板。新版本将在下次启动 dec 时生效。"),
		)
	}
	return lines
}

func (m model) renderRemoveSelect() []string {
	bundles := m.enabledRemoveBundles()
	lines := []string{shellTitleStyle.Render("Remove 选择器")}
	lines = append(lines, fmt.Sprintf("筛选: %s · 共 %d 个 bundle", m.currentRemoveFilterLabel(), len(bundles)))
	if m.removeFilterInput {
		lines = append(lines, shellMutedStyle.Render("输入筛选 · Enter 应用 · Esc 退出"))
	} else {
		lines = append(lines, shellMutedStyle.Render("j/k 移动 · Enter 选择 · / 筛选 · c 清空 · Esc 返回"))
	}
	if len(bundles) == 0 {
		if m.assets != nil && len(app.ListEnabledBundles(m.assets)) == 0 {
			lines = append(lines, shellWarnStyle.Render("当前没有已启用的 bundle。"))
			lines = append(lines, shellMutedStyle.Render("请先在 Bundles 页勾选 bundle 并保存，再执行 pull。"))
		} else {
			lines = append(lines, shellWarnStyle.Render("没有匹配筛选条件的 bundle。"))
			lines = append(lines, shellMutedStyle.Render("按 c 清空筛选，或 esc 退出。"))
		}
		return lines
	}

	for i, bo := range bundles {
		marker := " "
		if i == m.removeCursor {
			marker = ">"
		}
		vault := bo.Vault
		if vault == "" {
			vault = bo.Name
		}
		line := fmt.Sprintf("  %s %s / %s · %d 成员", marker, bo.Name, vault, len(bo.Members))
		if i == m.removeCursor {
			lines = append(lines, shellSelectedRow.Render(line))
		} else {
			lines = append(lines, shellLogStyle.Render(line))
		}
	}
	return lines
}

func (m model) renderRemoveConfirm() []string {
	lines := []string{shellTitleStyle.Render("Remove 确认")}
	if m.removeTarget == nil {
		lines = append(lines, shellWarnStyle.Render("未选择 bundle，按 esc 返回。"))
		return lines
	}
	vault := m.removeTarget.Vault
	if vault == "" {
		vault = m.removeTarget.Name
	}
	lines = append(lines,
		fmt.Sprintf("Bundle: %s", m.removeTarget.Name),
		fmt.Sprintf("Vault: %s", vault),
		fmt.Sprintf("成员数: %d", len(m.removeTarget.Members)),
		shellWarnStyle.Render("将删除远端 bundle、本地 cache 和 IDE 文件，并取消启用。"),
		shellMutedStyle.Render("Bitwarden secrets 不会删除。"),
		shellMutedStyle.Render("y 确认 · n/Esc 返回"),
	)
	return lines
}

func (m model) renderSettingsPage(width int) string {
	if m.settingsErr != nil {
		return shellWarnStyle.Render("无法加载全局设置") + "\n\n" + m.settingsErr.Error()
	}
	if m.settings == nil {
		return shellMutedStyle.Render("Loading global settings...")
	}

	summary := []string{}
	if m.settingsDirty {
		summary = append(summary, shellWarnStyle.Render("有未保存修改，按 s 保存"))
	}
	if m.serverVersionMismatch {
		summary = append(summary, shellWarnStyle.Render(fmt.Sprintf(
			"服务版本不一致 (client %s / server %s)；可在下方重启",
			fallbackValue(m.currentVersion, "?"), fallbackValue(m.serverVersion, "?"))))
	}
	if m.settingsRepoEditing {
		summary = append(summary, shellMutedStyle.Render("Repo URL 输入中：Enter 应用 · Esc 退出"))
	}
	if m.savingSettings {
		summary = append(summary, shellWarnStyle.Render("正在保存全局设置..."))
	}
	if warn := formatWarnings(m.settings.IDEWarnings); !strings.HasSuffix(warn, "无") {
		summary = append(summary, warn)
	}

	list := m.renderSettingsList()
	detail := m.renderSettingsDetails()
	if m.repoBootstrapStage != "" {
		return joinSections(m.renderRepoBootstrapBlock(), wrapLines(width, summary), renderSplitPane(width, list, detail))
	}
	if len(summary) == 0 {
		return renderSplitPane(width, list, detail)
	}
	return joinSections(wrapLines(width, summary), renderSplitPane(width, list, detail))
}

func (m model) renderRepoBootstrapBlock() string {
	lines := []string{shellWarnStyle.Render("私仓认证 · Bitwarden GCM Bootstrap")}
	switch m.repoBootstrapStage {
	case "confirm":
		lines = append(lines,
			fmt.Sprintf("仓库主机: %s", fallbackValue(m.repoBootstrapHost, "<unknown>")),
			"Git HTTPS 认证失败。是否从 Bitwarden 查找 host 匹配的 .gcm/* Note？",
			shellMutedStyle.Render("只复用现有 Note 与 GCM Processor；token 不返回 TUI、不另行落盘。"),
			shellMutedStyle.Render("y/Enter 查找 · n/Esc 取消"),
		)
	case "loading":
		lines = append(lines,
			fmt.Sprintf("仓库主机: %s", fallbackValue(m.repoBootstrapHost, "<unknown>")),
			shellMutedStyle.Render("正在解锁/扫描 Bitwarden… · Esc 取消"),
		)
	case "select":
		lines = append(lines, fmt.Sprintf("仓库主机: %s · 请选择 GCM Note", fallbackValue(m.repoBootstrapHost, "<unknown>")))
		for i, candidate := range m.repoBootstrapCandidates {
			marker := "  "
			if i == m.repoBootstrapCursor {
				marker = "> "
			}
			line := fmt.Sprintf("%s%s/%s · user=%s", marker, candidate.Address, candidate.NotePath, candidate.Username)
			if candidate.Unmanaged {
				line += " · 不属于任何 bundle，pull 不维护；请迁移到 bundle/<名>"
			}
			if i == m.repoBootstrapCursor {
				lines = append(lines, shellSelectedRow.Render(line))
			} else {
				lines = append(lines, line)
			}
		}
		lines = append(lines, shellMutedStyle.Render("↑/↓ 选择 · Enter 应用并验证 · n/Esc 取消"))
	case "applying":
		lines = append(lines, shellMutedStyle.Render("正在应用 GCM 并验证仓库访问… · Esc 取消"))
	}
	if msg := strings.TrimSpace(m.repoBootstrapError); msg != "" {
		lines = append(lines, shellWarnStyle.Render(msg))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderSettingsList() string {
	lines := []string{shellTitleStyle.Render("全局设置")}
	repoLine := fmt.Sprintf("%s Repo URL: %s", settingsCursorMarker(m.settingsCursor == settingsRowRepo && m.focus != focusSidebar), fallbackValue(strings.TrimSpace(m.settingsRepoInput), "<none>"))
	if m.settingsCursor == settingsRowRepo && m.focus != focusSidebar {
		lines = append(lines, shellSelectedRow.Render(repoLine))
	} else {
		lines = append(lines, shellLogStyle.Render(repoLine))
	}
	idleLine := fmt.Sprintf("%s 服务空闲超时: %s", settingsCursorMarker(m.settingsCursor == settingsRowIdleTimeout && m.focus != focusSidebar), fallbackValue(strings.TrimSpace(m.settingsIdleTimeoutInput), "30m"))
	if m.settingsCursor == settingsRowIdleTimeout && m.focus != focusSidebar {
		lines = append(lines, shellSelectedRow.Render(idleLine))
	} else {
		lines = append(lines, shellLogStyle.Render(idleLine))
	}
	restartMark := ""
	if m.serverVersionMismatch {
		restartMark = " (!)"
	}
	restartLine := fmt.Sprintf("%s 重启 dec-server%s", settingsCursorMarker(m.settingsCursor == settingsRowRestartServer && m.focus != focusSidebar), restartMark)
	if m.settingsCursor == settingsRowRestartServer && m.focus != focusSidebar {
		lines = append(lines, shellSelectedRow.Render(restartLine))
	} else if m.serverVersionMismatch {
		lines = append(lines, shellWarnStyle.Render(restartLine))
	} else {
		lines = append(lines, shellLogStyle.Render(restartLine))
	}
	varsReady := ""
	if m.settings.VarsFileReady {
		varsReady = " (已存在)"
	} else if strings.TrimSpace(m.settings.VarsPath) != "" {
		varsReady = " (未生成)"
	}
	varsLine := fmt.Sprintf("%s 本机变量 · e 外部编辑%s", settingsCursorMarker(m.settingsCursor == settingsRowGlobalVars && m.focus != focusSidebar), varsReady)
	if m.settingsCursor == settingsRowGlobalVars && m.focus != focusSidebar {
		lines = append(lines, shellSelectedRow.Render(varsLine))
	} else {
		lines = append(lines, shellLogStyle.Render(varsLine))
	}
	for idx, ideName := range m.settings.AvailableIDEs {
		selected := settingsContainsIDE(m.settingsSelectedIDEs, ideName)
		checked := " "
		if selected {
			checked = "x"
		}
		row := settingsFixedRowCount + idx
		line := fmt.Sprintf("%s [%s] %s", settingsCursorMarker(m.settingsCursor == row && m.focus != focusSidebar), checked, ideName)
		switch {
		case m.settingsCursor == row && m.focus != focusSidebar:
			lines = append(lines, shellSelectedRow.Render(line))
		case selected:
			lines = append(lines, shellEnabledRow.Render(line))
		default:
			lines = append(lines, shellLogStyle.Render(line))
		}
	}
	// 本机项目启用只在 `dec --global` 的引入页管理：
	// 两处写同一份 GlobalConfig.EnabledBundles 会互相覆盖（各自基于加载时快照）。
	lines = append(lines, "", shellMutedStyle.Render(
		fmt.Sprintf("本机项目：已启用 %d 个 · 在 dec --global 的引入页管理", len(normalizedStringList(m.settings.EnabledBundles)))))
	return strings.Join(lines, "\n")
}

func (m model) renderSettingsDetails() string {
	lines := []string{shellTitleStyle.Render("详情")}
	switch {
	case m.settingsCursor == settingsRowRepo:
		lines = append(lines,
			fmt.Sprintf("状态: %s", formatReady(m.settings.RepoConnected, "已连接", "未连接")),
			fmt.Sprintf("远端: %s", fallbackValue(m.settings.ConnectedRepoURL, "未连接")),
			shellMutedStyle.Render("Enter 编辑"),
		)
	case m.settingsCursor == settingsRowIdleTimeout:
		lines = append(lines,
			fmt.Sprintf("超时: %s", fallbackValue(strings.TrimSpace(m.settingsIdleTimeoutInput), "30m")),
			"客户端全部断开后计时；超时退出服务并清除 session。",
			shellMutedStyle.Render("格式: 15m / 1h · Enter 编辑"),
		)
	case m.settingsCursor == settingsRowRestartServer:
		match := "一致"
		if m.serverVersionMismatch {
			match = "不一致"
		} else if strings.TrimSpace(m.serverVersion) == "" {
			match = "未知"
		}
		lines = append(lines,
			fmt.Sprintf("客户端: %s", fallbackValue(m.currentVersion, "未知")),
			fmt.Sprintf("服务端: %s", fallbackValue(m.serverVersion, "未知")),
			fmt.Sprintf("版本: %s", match),
			shellWarnStyle.Render("重启会清除 Bitwarden session。"),
			shellMutedStyle.Render("Enter 确认重启"),
		)
	case m.settingsCursor == settingsRowGlobalVars:
		path := fallbackValue(m.settings.VarsPath, "~/.dec/local/vars.yaml")
		ready := "未生成"
		if m.settings.VarsFileReady {
			ready = "已存在"
		}
		lines = append(lines,
			fmt.Sprintf("路径: %s", compactPath(path, 48)),
			fmt.Sprintf("状态: %s", ready),
			shellMutedStyle.Render("上方表单写入 config.yaml；本项编辑机器级变量。"),
			shellMutedStyle.Render(fmt.Sprintf("编辑器: %s · e/Enter 打开", fallbackValue(m.settings.ConfiguredEditor, "vim"))),
		)
	case m.settingsCursorIDEIndex() >= 0:
		ideName := m.currentSettingsIDEName()
		lines = append(lines,
			fmt.Sprintf("IDE: %s", ideName),
			"启用后同步用户级 Dec Skill 与 MCP。",
			shellMutedStyle.Render("space 切换"),
		)
	default:
		lines = append(lines,
			fmt.Sprintf("用户 bundles: 已启用 %d 个", m.settingsUserBundleCount()),
			fmt.Sprintf("Bitwarden: %s", formatReady(m.settings.BitwardenSessionReady, "已解锁", "未解锁")),
			"启用列表由用户平面维护，与项目 bundles 隔离。",
			shellMutedStyle.Render("在 dec --global 的引入页勾选并保存。"),
		)
	}
	return strings.Join(lines, "\n")
}

func (m model) renderAssetList(listBudget int) string {
	lines := []string{shellTitleStyle.Render("Bundle 列表")}
	mm := m
	mm.refreshAssetTree()
	allRows := mm.assetTree.VisibleRows()
	if len(allRows) == 0 {
		return strings.Join(lines, "\n")
	}
	vp := listBudget - 1 // 标题
	if vp < 1 {
		vp = 1
	}
	scrollHint := len(allRows) > vp
	if scrollHint && vp > 1 {
		vp--
	}
	mm.assetTree.SetViewport(vp)
	window := mm.assetTree.WindowRows()
	if scrollHint {
		lines = append(lines, shellMutedStyle.Render(fmt.Sprintf("%d–%d / %d · PgUp/PgDn",
			mm.assetTree.Offset+1, mm.assetTree.Offset+len(window), len(allRows))))
	}
	for i, tr := range window {
		abs := mm.assetTree.Offset + i
		marker := " "
		if m.focus != focusSidebar && abs == mm.assetTree.Cursor {
			marker = ">"
		}
		bundleEnabled := false
		if p, ok := tr.Node.Payload.(assetTreePayload); ok {
			bundleEnabled = p.bundleEnabled
		}
		line := renderAssetTreeLine(tr, &mm.assetTree, marker, bundleEnabled)
		var styled string
		if m.focus != focusSidebar && abs == mm.assetTree.Cursor {
			styled = shellSelectedRow.Render(line)
		} else if p, ok := tr.Node.Payload.(assetTreePayload); ok && p.kind == assetRowBundle && p.bundleEnabled {
			styled = shellEnabledRow.Render(line)
		} else {
			styled = shellLogStyle.Render(line)
		}
		lines = append(lines, styled)
	}
	return strings.Join(lines, "\n")
}

// renderAssetRowLine 生成单行文本。
func (m model) renderAssetRowLine(row assetRow, marker string) string {
	bo := m.assets.Bundles[row.bundleIndex]
	if row.kind == assetRowBundleMember {
		mb := bo.Members[row.memberIndex]
		if bo.Model == "p" {
			return fmt.Sprintf("%s   ↳ %s/%s · %s / %s", marker, mb.Visibility, mb.Plane, mb.Type, mb.Name)
		}
		return fmt.Sprintf("%s   ↳ %s / %s / %s", marker, mb.Type, mb.Vault, mb.Name)
	}
	checked := " "
	if row.bundleEnabled {
		checked = "x"
	}
	if bo.Home {
		checked = "H"
	}
	if bo.OtherPlane {
		return fmt.Sprintf("%s [-]   %s · 属于项目平面", marker, bo.Name)
	}
	if bo.SecretsOnly {
		if len(bo.Members) > 0 {
			return fmt.Sprintf("%s [%s]   %s · %s · %d 个成员", marker, checked, bo.Name, secretsOnlyBundleHint(bo), len(bo.Members))
		}
		return fmt.Sprintf("%s [%s]   %s · %s", marker, checked, bo.Name, secretsOnlyBundleHint(bo))
	}
	arrow := "▸"
	if m.assetTree.Expanded[assetBundleNodeID(bo.Name)] {
		arrow = "▾"
	}
	label := bo.Name
	if bo.Name != bo.Vault {
		label = fmt.Sprintf("%s (%s)", bo.Name, bo.Vault)
	}
	return fmt.Sprintf("%s [%s] %s %s · %d 个成员", marker, checked, arrow, label, len(bo.Members))
}

func (m model) renderAssetDetails() string {
	lines := []string{shellTitleStyle.Render("详情")}
	if m.assets != nil {
		if p, ok := m.assetPayloadAtCursor(); ok {
			switch p.kind {
			case assetRowBundle:
				bo := m.assets.Bundles[p.bundleIndex]
				if bo.Model == "p" {
					role := "可引用 P"
					if bo.Home {
						role = "家 P（固定）"
					} else if bo.Required {
						role = "直接 requires"
					} else if m.plane == app.WorkspaceUser && bo.Enabled {
						role = "用户已启用"
					}
					lines = append(lines,
						fmt.Sprintf("项目: %s", bo.Name),
						fmt.Sprintf("角色: %s", role),
						fmt.Sprintf("public/global: %d", bo.Quadrants["public/global"]),
						fmt.Sprintf("private/global: %d", bo.Quadrants["private/global"]),
						fmt.Sprintf("public/local: %d", bo.Quadrants["public/local"]),
						fmt.Sprintf("private/local: %d", bo.Quadrants["private/local"]),
					)
					return strings.Join(lines, "\n")
				}
				lines = append(lines,
					fmt.Sprintf("Bundle: %s", bo.Name),
				)
				if bo.Vault != "" && bo.Vault != bo.Name {
					lines = append(lines, fmt.Sprintf("Vault: %s", bo.Vault))
				}
				if bo.OtherPlane {
					lines = append(lines,
						shellMutedStyle.Render("仓库里已有该 bundle，但 scope 不是 user，属于项目平面。"),
						shellMutedStyle.Render("不能在此启用：要移到用户平面，需先显式改 vault manifest 的 scope，"),
						shellMutedStyle.Render("并确认没有 project 还在引用它（否则那些项目会拉不到资产）。"),
					)
					return strings.Join(lines, "\n")
				}
				if bo.SecretsOnly {
					switch {
					case bo.RemoteMissing:
						lines = append(lines,
							shellMutedStyle.Render("Bitwarden 与仓库都没有该 bundle：这是本机残留的启用记录。"),
							shellMutedStyle.Render("取消勾选并保存即可清掉；继续留着只会拉到空内容。"),
						)
					case bo.RemoteUnverified:
						lines = append(lines,
							shellMutedStyle.Render("本机记录里有该 bundle；本次未核对 Bitwarden（无 session 或枚举失败）。"),
							shellMutedStyle.Render("勾选并保存后会创建 scope: user 的 bundle 声明。"),
						)
					default:
						lines = append(lines,
							shellMutedStyle.Render("Bitwarden 里已有同名 secrets；仓库尚未登记该 bundle。"),
							shellMutedStyle.Render("勾选并保存后会创建 scope: user 的 bundle 声明。"),
						)
					}
					if m.assetTree.Expanded[assetBundleNodeID(bo.Name)] && len(bo.Members) > 0 {
						lines = append(lines, "", shellTitleStyle.Render("成员列表"))
						for _, mb := range bo.Members {
							lines = append(lines, fmt.Sprintf("  · %s / %s", mb.Type, mb.Name))
						}
					} else if m.focus == focusContent && len(bo.Members) > 0 {
						lines = append(lines, shellMutedStyle.Render("按 l 或 Enter 展开查看成员"))
					}
					return strings.Join(lines, "\n")
				}
				if m.assetTree.Expanded[assetBundleNodeID(bo.Name)] {
					lines = append(lines, "", shellTitleStyle.Render("成员列表"))
					for _, mb := range bo.Members {
						lines = append(lines, fmt.Sprintf("  · %s / %s / %s", mb.Type, mb.Vault, mb.Name))
					}
				} else if m.focus == focusContent {
					lines = append(lines, shellMutedStyle.Render("按 l 或 Enter 展开查看成员"))
				}
				if bo.Description != "" {
					lines = append(lines, "", bo.Description)
				}
				return strings.Join(lines, "\n")
			case assetRowBundleMember:
				bo := m.assets.Bundles[p.bundleIndex]
				mb := bo.Members[p.memberIndex]
				lines = append(lines,
					fmt.Sprintf("Bundle: %s", bo.Name),
					fmt.Sprintf("Type: %s", mb.Type),
					fmt.Sprintf("Vault: %s", mb.Vault),
					fmt.Sprintf("Name: %s", mb.Name),
					shellMutedStyle.Render("Bundle 成员，只读"),
				)
				return strings.Join(lines, "\n")
			}
		} else if row, ok := m.assetTreeRowAtCursor(); ok {
			lines = append(lines,
				fmt.Sprintf("目录: %s", row.Node.Label),
				shellMutedStyle.Render("按 l 展开 · h 折叠"),
			)
		}
	}
	if len(lines) == 1 {
		if item, ok := m.currentAssetItem(); ok {
			lines = append(lines,
				fmt.Sprintf("Vault: %s", item.Vault),
				fmt.Sprintf("Type: %s", item.Type),
				fmt.Sprintf("Name: %s", item.Name),
			)
		} else {
			lines = append(lines, "当前没有匹配的 bundle。")
		}
	}

	if m.savingAssets {
		lines = append(lines, "", shellWarnStyle.Render("正在保存 bundle 选择..."))
	}
	return strings.Join(lines, "\n")
}

func (m model) currentSettingsIDEName() string {
	idx := m.settingsCursorIDEIndex()
	if idx < 0 || m.settings == nil || idx >= len(m.settings.AvailableIDEs) {
		return ""
	}
	return m.settings.AvailableIDEs[idx]
}

func (m model) settingsCursorIDEIndex() int {
	if m.settings == nil || m.settingsCursor < settingsFixedRowCount {
		return -1
	}
	idx := m.settingsCursor - settingsFixedRowCount
	if idx < 0 || idx >= len(m.settings.AvailableIDEs) {
		return -1
	}
	return idx
}

func (m model) currentAssetItem() (app.AssetSelectionItem, bool) {
	if m.assets == nil {
		return app.AssetSelectionItem{}, false
	}
	p, ok := m.assetPayloadAtCursor()
	if !ok {
		return app.AssetSelectionItem{}, false
	}
	if p.kind == assetRowBundleMember {
		bo := m.assets.Bundles[p.bundleIndex]
		return bo.Members[p.memberIndex], true
	}
	// bundle 节点本身没有对应的 AssetSelectionItem。返回 false 让 Detail 面板走 bundle 分支。
	return app.AssetSelectionItem{}, false
}

// ------ Bundle-aware row model for Assets page ------

type assetRowKind int

const (
	assetRowBundle assetRowKind = iota
	assetRowBundleMember
)

// assetRow 描述 Bundles 页一行可见条目；光标索引以 assetTree.VisibleRows() 为准。
type assetRow struct {
	kind          assetRowKind
	bundleIndex   int // kind == assetRowBundle / assetRowBundleMember 时有效
	memberIndex   int // kind == assetRowBundleMember 时有效
	bundleEnabled bool
}

// bundleSelected 返回当前 TUI 侧 bundleSelection 是否包含该 bundle。
func (m model) bundleSelected(name string) bool {
	for _, n := range m.bundleSelection {
		if n == name {
			return true
		}
	}
	return false
}

func (m model) canNavigateSettings() bool {
	return m.settings != nil && m.settingsRowCount() > 0
}

func (m model) settingsIDECount() int {
	return len(normalizedStringList(m.settingsSelectedIDEs))
}

// settingsUserBundleCount 只用于展示：用户平面启用列表由 Bundles 页写入，Settings 不再编辑。
func (m model) settingsUserBundleCount() int {
	if m.settings == nil {
		return 0
	}
	return len(normalizedStringList(m.settings.EnabledBundles))
}

// settingsCountsLabel 是 Settings 顶栏/底栏共用的计数文案（SSOT）。
func (m model) settingsCountsLabel() string {
	return fmt.Sprintf("%d IDEs, %d bundles", m.settingsIDECount(), m.settingsUserBundleCount())
}

func (m model) settingsRowCount() int {
	if m.settings == nil {
		return 0
	}
	return settingsFixedRowCount + len(m.settings.AvailableIDEs)
}

func (m model) canNavigateAssets() bool {
	return m.assets != nil && m.assetTreeVisibleCount() > 0
}

func (m *model) normalizeAssetCursor() {
	m.refreshAssetTree()
	m.assetTree.normalizeCursor()
}

func (m *model) moveAssetCursor(delta int) {
	m.refreshAssetTree()
	m.assetTree.MoveCursor(delta)
}

func (m *model) normalizeSettingsCursor() {
	if m.settingsRowCount() == 0 {
		m.settingsCursor = 0
		return
	}
	if m.settingsCursor < 0 || m.settingsCursor >= m.settingsRowCount() {
		m.settingsCursor = 0
	}
}

func (m *model) moveSettingsCursor(delta int) {
	if !m.canNavigateSettings() {
		return
	}
	m.normalizeSettingsCursor()
	m.settingsCursor += delta
	if m.settingsCursor < 0 {
		m.settingsCursor = 0
	}
	if m.settingsCursor >= m.settingsRowCount() {
		m.settingsCursor = m.settingsRowCount() - 1
	}
}

func (m *model) beginSettingsRepoEdit() {
	if m.settings == nil {
		return
	}
	m.settingsCursor = settingsRowRepo
	m.settingsRepoEditing = true
	m.pushLog("Repo URL input opened")
}

func (m *model) beginSettingsIdleTimeoutEdit() {
	if m.settings == nil {
		return
	}
	m.settingsCursor = settingsRowIdleTimeout
	m.settingsIdleTimeoutEditing = true
	m.pushLog("服务空闲超时输入已打开")
}

func (m *model) toggleCurrentSettingsIDE() {
	ideName := m.currentSettingsIDEName()
	if strings.TrimSpace(ideName) == "" {
		return
	}
	if settingsContainsIDE(m.settingsSelectedIDEs, ideName) {
		m.settingsSelectedIDEs = settingsRemoveIDE(m.settingsSelectedIDEs, ideName)
		m.pushLog("IDE disabled: " + ideName)
	} else {
		m.settingsSelectedIDEs = append(m.settingsSelectedIDEs, ideName)
		m.pushLog("IDE enabled: " + ideName)
	}
	m.syncSettingsDirty()
}

func (m *model) syncSettingsDirty() {
	if m.settings == nil {
		m.settingsDirty = false
		return
	}
	currentRepo := strings.TrimSpace(m.settingsRepoInput)
	loadedRepo := strings.TrimSpace(m.settings.RepoURL)
	m.settingsDirty = currentRepo != loadedRepo ||
		strings.TrimSpace(m.settingsIdleTimeoutInput) != strings.TrimSpace(m.settings.ServerIdleTimeout) ||
		!equalNormalizedStrings(m.settingsSelectedIDEs, m.settings.SelectedIDEs)
}

func settingsContainsIDE(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func settingsRemoveIDE(values []string, target string) []string {
	target = strings.TrimSpace(target)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			continue
		}
		result = append(result, value)
	}
	return result
}

func equalNormalizedStrings(left, right []string) bool {
	leftNorm := normalizedStringList(left)
	rightNorm := normalizedStringList(right)
	if len(leftNorm) != len(rightNorm) {
		return false
	}
	for idx := range leftNorm {
		if leftNorm[idx] != rightNorm[idx] {
			return false
		}
	}
	return true
}

func normalizedStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func settingsEffectivePreview(state *app.GlobalSettingsState, selected []string) []string {
	if len(selected) > 0 {
		return normalizedStringList(selected)
	}
	if state == nil {
		return nil
	}
	if len(state.EffectiveIDEs) > 0 {
		return normalizedStringList(state.EffectiveIDEs)
	}
	return normalizedStringList(state.SelectedIDEs)
}

func settingsCursorMarker(active bool) string {
	if active {
		return ">"
	}
	return " "
}

// ------- Project Settings helpers -------

func (m model) canNavigateProjectSettings() bool {
	return m.projectSettings != nil && m.projectSettingsRowCount() > 0
}

func (m model) projectSettingsRowCount() int {
	if m.projectSettings == nil {
		return 0
	}
	return 1 + len(m.projectSettings.AvailableIDEs)
}

func (m *model) normalizeProjectSettingsCursor() {
	if m.projectSettingsRowCount() == 0 {
		m.projectSettingsCursor = 0
		return
	}
	if m.projectSettingsCursor < 0 || m.projectSettingsCursor >= m.projectSettingsRowCount() {
		m.projectSettingsCursor = 0
	}
}

func (m *model) moveProjectSettingsCursor(delta int) {
	if !m.canNavigateProjectSettings() {
		return
	}
	m.normalizeProjectSettingsCursor()
	m.projectSettingsCursor += delta
	if m.projectSettingsCursor < 0 {
		m.projectSettingsCursor = 0
	}
	if m.projectSettingsCursor >= m.projectSettingsRowCount() {
		m.projectSettingsCursor = m.projectSettingsRowCount() - 1
	}
}

func (m model) currentProjectSettingsIDEName() string {
	if m.projectSettings == nil || m.projectSettingsCursor <= 0 {
		return ""
	}
	idx := m.projectSettingsCursor - 1
	if idx < 0 || idx >= len(m.projectSettings.AvailableIDEs) {
		return ""
	}
	return m.projectSettings.AvailableIDEs[idx]
}

// toggleProjectOverride 切换 "覆盖/继承" 模式。首次开启覆盖时，预填当前生效 IDE。
func (m *model) toggleProjectOverride() {
	if m.projectSettings == nil {
		return
	}
	m.projectSettingsOverride = !m.projectSettingsOverride
	if m.projectSettingsOverride {
		if len(m.projectSettingsSelectedIDEs) == 0 {
			m.projectSettingsSelectedIDEs = cloneStrings(m.projectSettings.EffectiveIDEs)
		}
		m.pushLog("Project override enabled")
	} else {
		m.pushLog("Project override disabled (will inherit global on save)")
	}
	m.syncProjectSettingsDirty()
}

// toggleCurrentProjectIDE 在覆盖模式下切换光标所在的 IDE。继承模式下仅记录日志。
func (m *model) toggleCurrentProjectIDE() {
	ideName := m.currentProjectSettingsIDEName()
	if strings.TrimSpace(ideName) == "" {
		return
	}
	if !m.projectSettingsOverride {
		m.pushLog("当前处于继承模式，按 space 在第一行切换到覆盖模式后再选择 IDE")
		return
	}
	if settingsContainsIDE(m.projectSettingsSelectedIDEs, ideName) {
		m.projectSettingsSelectedIDEs = settingsRemoveIDE(m.projectSettingsSelectedIDEs, ideName)
		m.pushLog("Project IDE disabled: " + ideName)
	} else {
		m.projectSettingsSelectedIDEs = append(m.projectSettingsSelectedIDEs, ideName)
		m.pushLog("Project IDE enabled: " + ideName)
	}
	m.syncProjectSettingsDirty()
}

// clearProjectOverride 立即切回继承态，并清空本地选择。
func (m *model) clearProjectOverride() {
	if m.projectSettings == nil {
		return
	}
	m.projectSettingsOverride = false
	m.projectSettingsSelectedIDEs = nil
	m.pushLog("Project override cleared (will inherit global on save)")
	m.syncProjectSettingsDirty()
}

func (m *model) syncProjectSettingsDirty() {
	if m.projectSettings == nil {
		m.projectSettingsDirty = false
		return
	}
	if m.projectSettingsOverride != m.projectSettings.OverrideActive {
		m.projectSettingsDirty = true
		return
	}
	if !m.projectSettingsOverride {
		// 同属继承态；本地选择无意义。
		m.projectSettingsDirty = false
		return
	}
	m.projectSettingsDirty = !equalNormalizedStrings(m.projectSettingsSelectedIDEs, m.projectSettings.SelectedIDEs)
}

func (m *model) toggleCurrentAsset() {
	if m.assets == nil {
		return
	}
	p, ok := m.assetPayloadAtCursor()
	if !ok {
		return
	}
	switch p.kind {
	case assetRowBundle:
		bo := m.assets.Bundles[p.bundleIndex]
		if bo.Home {
			m.pushLog(fmt.Sprintf("%s 是当前工作区家 P，不能在 Bundles 页取消；请在 Home 重新绑定", bo.Name))
			return
		}
		if bo.OtherPlane {
			m.pushLog(fmt.Sprintf("%s 属于项目平面，不能在此启用：需先在仓库显式改 bundle.yaml 的 scope", bo.Name))
			return
		}
		if m.bundleSelected(bo.Name) {
			next := make([]string, 0, len(m.bundleSelection))
			for _, n := range m.bundleSelection {
				if n != bo.Name {
					next = append(next, n)
				}
			}
			m.bundleSelection = next
			m.pushLog("Bundle disabled: " + bo.Name)
		} else {
			m.bundleSelection = append(m.bundleSelection, bo.Name)
			m.pushLog("Bundle enabled: " + bo.Name)
		}
		m.assetsDirty = true
	case assetRowBundleMember:
		bo := m.assets.Bundles[p.bundleIndex]
		mb := bo.Members[p.memberIndex]
		m.pushLog(fmt.Sprintf("Member 由 bundle %q 带入，无法单独切换：%s / %s / %s", bo.Name, mb.Vault, mb.Type, mb.Name))
	}
}

// countSelectedBundleMembers 统计当前勾选 bundle 展开后的成员数（跨 bundle 去重）。
func (m model) countSelectedBundleMembers() int {
	if m.assets == nil {
		return 0
	}
	seen := make(map[string]struct{})
	for _, bo := range m.assets.Bundles {
		if !m.bundleSelected(bo.Name) {
			continue
		}
		for _, mb := range bo.Members {
			seen[mb.Type+"\x00"+mb.Vault+"\x00"+mb.Name] = struct{}{}
		}
	}
	return len(seen)
}

// assetsCursorOnBundle 判断当前光标是否落在 bundle 节点行（不是成员行）。
func (m model) assetsCursorOnBundle() bool {
	p, ok := m.assetPayloadAtCursor()
	return ok && p.kind == assetRowBundle
}

func (m *model) expandAssetAtCursor() {
	if m.assets == nil {
		return
	}
	m.refreshAssetTree()
	row, ok := m.assetTree.currentRow()
	if !ok || !treeNodeExpandable(row.Node) {
		return
	}
	m.assetTree.Expanded[row.Node.ID] = true
	if p, ok := row.Node.Payload.(assetTreePayload); ok && p.kind == assetRowBundle {
		bo := m.assets.Bundles[p.bundleIndex]
		nodeID := assetBundleNodeID(bo.Name)
		seen := make(map[string]struct{})
		for _, mb := range bo.Members {
			if mb.Type == app.AssetMemberTypeSecret {
				segs := secretsParentSegments(mb.Name)
				if len(segs) > 0 {
					m.assetTree.Expanded[nodeID+"/"+segs[0]] = true
				}
				continue
			}
			sub := assetTypeSubDir(mb.Type)
			if _, dup := seen[sub]; dup {
				continue
			}
			seen[sub] = struct{}{}
			m.assetTree.Expanded[nodeID+"/"+sub] = true
		}
		m.pushLog("Bundle 展开: " + bo.Name)
	} else {
		m.pushLog("展开: " + row.Node.Label)
	}
	m.refreshAssetTree()
}

// expandCurrentBundle 展开 bundle 及其类型子目录。
func (m *model) expandCurrentBundle() {
	if !m.assetsCursorOnBundle() {
		return
	}
	m.expandAssetAtCursor()
}

// collapseCurrentBundle 折叠当前 bundle 节点（光标可在 bundle 或成员行）。
func (m *model) collapseCurrentBundle() {
	if m.assets == nil {
		return
	}
	m.refreshAssetTree()
	if m.assetTree.CollapseAtCursor() {
		name := ""
		if p, ok := m.assetPayloadAtCursor(); ok && p.bundleIndex < len(m.assets.Bundles) {
			name = m.assets.Bundles[p.bundleIndex].Name
		}
		m.pushLog("Bundle 折叠: " + name)
		m.refreshAssetTree()
	}
}

func (m model) currentAssetFilterLabel() string {
	filter := strings.TrimSpace(m.assetFilter)
	if filter == "" {
		return "<none>"
	}
	return filter
}

func (m model) hasVaultInferencePrompt() bool {
	return m.vaultInference != nil && !m.vaultInferenceDismissed && !m.vaultApplyLoad.busy()
}

func (m model) currentPage() string {
	if m.pageIndex < 0 || m.pageIndex >= len(m.pages) {
		return ""
	}
	return m.pages[m.pageIndex]
}

func (m model) isProjectSettings() bool {
	return m.isSettingsPage() && m.plane != app.WorkspaceUser && m.plane != app.WorkspaceGlobal
}

func (m model) currentSummary() string {
	if busy := m.ioBusyLabel(); busy != "" {
		return busy
	}
	if m.overviewErr != nil {
		return "Overview unavailable"
	}
	if m.isRemotePage() {
		if m.deleteLoadErr != nil {
			return "Remote list unavailable"
		}
		if m.deleteStage == "summary" {
			return "Confirming delete (summary)"
		}
		if m.deleteStage == "confirm" {
			return "Confirming delete (final)"
		}
		if m.deleteCandidatesLoaded {
			return fmt.Sprintf("Remote ready, %d items", len(m.deleteCandidates))
		}
		return "Remote page ready"
	}
	if m.isBundlesPage() {
		if m.assetsErr != nil {
			return "Bundle selection unavailable"
		}
		if m.assets == nil {
			return "Loading bundle selection"
		}
		if m.assetsDirty {
			return fmt.Sprintf("Unsaved bundles: %d/%d enabled", len(m.bundleSelection), len(m.assets.Bundles))
		}
		return fmt.Sprintf("Bundles ready, %d/%d enabled", len(m.bundleSelection), len(m.assets.Bundles))
	}
	if m.isSettingsPage() {
		if m.settingsErr != nil {
			return "Global settings unavailable"
		}
		if m.settings == nil {
			return "Loading global settings"
		}
		if m.repoBootstrapStage != "" {
			return "Private repo authentication: " + m.repoBootstrapStage
		}
		if m.savingSettings {
			return "Saving global settings"
		}
		if m.settingsRepoEditing {
			return "Editing repo URL"
		}
		if m.settingsDirty {
			return "Unsaved settings: " + m.settingsCountsLabel()
		}
		return "Settings ready, " + m.settingsCountsLabel()
	}
	if m.isRunPage() {
		if m.repoBootstrapStage != "" {
			return "Private repo authentication: " + m.repoBootstrapStage
		}
		if m.runningPull {
			if m.runMode == "push" {
				return "Push running"
			}
			return "Pull running"
		}
		if m.runningRemove {
			return "Remove running"
		}
		if m.updatingBinary {
			return "Update running"
		}
		if m.removeStage == "select" {
			return "Selecting bundle to remove"
		}
		if m.removeStage == "confirm" {
			return "Confirming remove"
		}
		if m.pushStage == "loading" {
			return "Loading push preview"
		}
		if m.pushStage == "summary" {
			return "Confirming push (summary)"
		}
		if m.pushStage == "confirm" {
			return "Confirming push (final)"
		}
		if m.updateStage == "checking" {
			return "Checking for updates"
		}
		if m.updateStage == "confirm" {
			return "Confirming update"
		}
		if m.updateStage == "done" {
			if m.updateErr != nil {
				return "Last update failed"
			}
			if m.updateResult != nil && !m.updateResult.NeedUpdate {
				return "Already up to date"
			}
			return "Last update succeeded"
		}
		if m.runErr != nil {
			if m.runMode == "push" {
				return "Last push failed"
			}
			return "Last pull failed"
		}
		if m.removeErr != nil {
			return "Last remove failed"
		}
		if m.runResult != nil {
			return fmt.Sprintf("Last pull: %d ok / %d failed", m.runResult.PulledCount, m.runResult.FailedCount)
		}
		if m.pushResult != nil {
			return fmt.Sprintf("Last push: dec %d · secrets +%d/~%d",
				m.pushResult.DecPushedCount, m.pushResult.SecretsCreatedCount, m.pushResult.SecretsUpdatedCount)
		}
		if m.removeResult != nil {
			return fmt.Sprintf("Last remove: bundle %s", m.removeResult.BundleName)
		}
		return "Run page ready"
	}
	if m.isProjectSettings() {
		if m.projectSettingsErr != nil {
			return "Project settings unavailable"
		}
		if m.projectSettings == nil {
			return "Loading project settings"
		}
		if m.savingProjectSettings {
			return "Saving project settings"
		}
		if m.projectSettingsDirty {
			return "Unsaved project IDE settings"
		}
		mode := "inherit"
		if m.projectSettingsOverride {
			mode = "override"
		}
		return fmt.Sprintf("Project ready, IDE %s", mode)
	}
	if m.overview == nil {
		return "Loading project state"
	}
	if m.vaultApplyLoad.busy() {
		return "Applying inferred vault project"
	}
	if m.hasVaultInferencePrompt() {
		return fmt.Sprintf("Vault project %s inferred, awaiting confirmation", m.vaultInference.ProjectName)
	}
	if m.localProjectInitConfirm {
		return "Project config missing, awaiting explicit initialization confirmation"
	}
	if !m.overview.RepoConnected {
		return "Repository not connected yet"
	}
	if !m.overview.ProjectConfigReady {
		return "Project config missing"
	}
	return fmt.Sprintf("Repo ready, %d bundles enabled", countOverviewEnabledBundles(m.overview))
}

func formatProjectNameDisplay(overview *app.ProjectOverview) string {
	if overview == nil {
		return "<unknown>"
	}
	name := strings.TrimSpace(overview.ProjectName)
	if name == "" {
		return "<未设置>"
	}
	if !overview.ProjectNameFromConfig {
		return fmt.Sprintf("%s（未写入配置）", name)
	}
	return name
}

// countOverviewAvailableBundles 优先按扫描到的 bundle 列表计数；仓库未连接或扫描失败时
// Bundles 为空，退回 config 层记录的计数。
func countOverviewAvailableBundles(overview *app.ProjectOverview) int {
	if overview == nil {
		return 0
	}
	if len(overview.Bundles) > 0 {
		return len(overview.Bundles)
	}
	return overview.AvailableBundleCount
}

func countOverviewEnabledBundles(overview *app.ProjectOverview) int {
	if overview == nil {
		return 0
	}
	if len(overview.Bundles) > 0 {
		n := 0
		for _, b := range overview.Bundles {
			if b.Enabled {
				n++
			}
		}
		return n
	}
	return overview.EnabledBundleCount
}

func formatOverviewEnabledBundleNames(overview *app.ProjectOverview) string {
	if overview == nil {
		return "<无>"
	}
	var names []string
	for _, b := range overview.Bundles {
		if b.Enabled {
			names = append(names, b.Name)
		}
	}
	if len(names) == 0 {
		if overview.EnabledBundleCount > 0 {
			return fmt.Sprintf("<%d 个，详情见 Bundles 页>", overview.EnabledBundleCount)
		}
		return "<无>"
	}
	return strings.Join(names, ", ")
}

func suggestNextAction(overview *app.ProjectOverview, vaultInferencePending, localInitPending bool) string {
	if overview == nil {
		return "正在加载项目概览…"
	}
	if !overview.RepoConnected {
		return "到设置页配置 Repo URL"
	}
	if vaultInferencePending {
		return "确认检测到的项目配置：y 应用 · n 跳过"
	}
	if localInitPending {
		return "确认当前目录是否正确：y 初始化 · n 保持不变"
	}
	if !overview.ProjectConfigReady {
		return "在项目页确认后初始化绑定项目"
	}
	if countOverviewEnabledBundles(overview) == 0 {
		return "到引入页勾选并保存"
	}
	return "到同步页按 p 拉取"
}

func formatInferenceBundleNames(bundles []string) string {
	if len(bundles) == 0 {
		return "<无>"
	}
	return strings.Join(bundles, ", ")
}

func (m model) runStatusLabel() string {
	switch {
	case m.runningPull:
		return shellGoodStyle.Render("执行中")
	case m.runningRemove:
		return shellGoodStyle.Render("删除中")
	case m.updatingBinary:
		return shellGoodStyle.Render("更新中")
	case m.runErr != nil:
		return shellWarnStyle.Render("失败")
	case m.removeErr != nil:
		return shellWarnStyle.Render("Remove 失败")
	case m.updateErr != nil:
		return shellWarnStyle.Render("Update 失败")
	case m.removeStage == "select":
		return shellWarnStyle.Render("Remove · 选择 bundle")
	case m.removeStage == "confirm":
		return shellWarnStyle.Render("Remove · 确认删除")
	case m.pushStage == "summary":
		return shellWarnStyle.Render("Push · 摘要确认")
	case m.pushStage == "confirm":
		return shellWarnStyle.Render("Push · 最终确认")
	case m.updateStage == "checking":
		return shellMutedStyle.Render("Update 检查中")
	case m.updateStage == "confirm":
		return shellWarnStyle.Render("Update 确认中")
	case m.updateStage == "done":
		if m.updateErr != nil {
			return shellWarnStyle.Render("Update 失败")
		}
		return shellGoodStyle.Render("Update 已完成")
	case m.pushResult != nil:
		return shellGoodStyle.Render("Push 已完成")
	case m.runResult != nil:
		return shellGoodStyle.Render("已完成")
	case m.removeResult != nil:
		return shellGoodStyle.Render("Remove 已完成")
	default:
		return shellMutedStyle.Render("空闲")
	}
}

func formatWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return "IDE 警告: 无"
	}
	return "IDE 警告: " + strings.Join(warnings, " | ")
}

func formatReady(ok bool, readyText, pendingText string) string {
	if ok {
		return shellGoodStyle.Render(readyText)
	}
	return shellWarnStyle.Render(pendingText)
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
