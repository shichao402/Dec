package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/assets"
	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/secrets"
	"github.com/shichao402/Dec/pkg/types"
)

type GlobalSettingsState struct {
	ConfigPath             string
	VarsPath               string
	VarsFileReady          bool
	RepoConnected          bool
	RepoURL                string
	ConnectedRepoURL       string
	AvailableIDEs          []string
	SelectedIDEs           []string
	EffectiveIDEs          []string
	IDEWarnings            []string
	ConfiguredEditor       string
	ConnectedBarePath      string
	AvailableSecretBundles []string // Settings 候选：vault ∪ known ∪ BW ∪ 已启用（语义：本机 bundle）
	UserEnabledBundles     []string // 本机启用的 Dec bundle 短名（user_enabled_bundles）
	SecretsConfigPath      string
	BitwardenSessionReady  bool
}

type ConnectRepoResult struct {
	RepoURL    string
	ConfigPath string
	BareRepo   string
}

type SaveGlobalSettingsInput struct {
	RepoURL            string
	IDEs               []string
	UserEnabledBundles []string // nil = 不改 secrets 用户级启用；非 nil（含空切片）= 写回
}

type SaveGlobalSettingsResult struct {
	RepoURL            string
	IDEs               []string
	UserEnabledBundles []string
	ConfigPath         string
	VarsPath           string
	VarsCreated        bool
	BareRepo           string
	InstallWarnings    []string
	SecretsConfigPath  string
	CreatedVaultBundles []string // 本次为用户级启用新建的 vault 占位
}

func LoadGlobalSettings(reporter Reporter) (*GlobalSettingsState, error) {
	reporter = defaultReporter(reporter)

	configPath, err := config.GetGlobalConfigPath()
	if err != nil {
		return nil, fmt.Errorf("获取全局配置路径失败: %w", err)
	}
	varsPath, err := config.GetGlobalVarsPath()
	if err != nil {
		return nil, fmt.Errorf("获取本机变量路径失败: %w", err)
	}
	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}

	state := &GlobalSettingsState{
		ConfigPath:       configPath,
		VarsPath:         varsPath,
		ConfiguredEditor: strings.TrimSpace(globalConfig.Editor),
	}

	availableIDEs := ide.List()
	sort.Strings(availableIDEs)
	state.AvailableIDEs = append(state.AvailableIDEs, availableIDEs...)

	selection, err := config.ResolveEffectiveIDEs(nil)
	if err != nil {
		return nil, err
	}
	state.EffectiveIDEs = append(state.EffectiveIDEs, selection.IDEs...)
	state.IDEWarnings = append(state.IDEWarnings, selection.Warnings...)
	if len(globalConfig.IDEs) > 0 {
		state.SelectedIDEs = append(state.SelectedIDEs, globalConfig.IDEs...)
	} else {
		state.SelectedIDEs = append(state.SelectedIDEs, selection.IDEs...)
	}

	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	state.RepoConnected = connected
	state.RepoURL = strings.TrimSpace(globalConfig.RepoURL)
	if connected {
		remoteURL, err := repo.GetBareRemoteURL()
		if err != nil {
			return nil, fmt.Errorf("读取当前远端失败: %w", err)
		}
		barePath, err := repo.GetBareRepoDir()
		if err != nil {
			return nil, fmt.Errorf("读取 bare repo 路径失败: %w", err)
		}
		state.ConnectedRepoURL = remoteURL
		state.ConnectedBarePath = barePath
		if strings.TrimSpace(state.RepoURL) == "" {
			state.RepoURL = remoteURL
		}
	}

	if _, err := os.Stat(state.VarsPath); err == nil {
		state.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查本机变量模板失败: %w", err)
	}

	if err := attachUserSecretBundleSettings(state, reporter); err != nil {
		return nil, err
	}

	emit(reporter, EventInfo, "settings.load", "全局设置已加载", nil)
	return state, nil
}

func attachUserSecretBundleSettings(state *GlobalSettingsState, reporter Reporter) error {
	secretsPath, err := secrets.ConfigPath()
	if err != nil {
		return fmt.Errorf("获取 secrets 配置路径失败: %w", err)
	}
	state.SecretsConfigPath = secretsPath
	state.BitwardenSessionReady = secrets.HasSession() && secrets.HasUserKey()

	cfg, err := secrets.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载 secrets 配置失败: %w", err)
	}
	state.UserEnabledBundles = cfg.UserEnabledBundleNames()

	client := secretsClientFactory()
	var remoteNames []string
	if state.BitwardenSessionReady && client != nil {
		names, listErr := client.ListSecretBundleNames(context.Background())
		if listErr != nil {
			emit(reporter, EventWarn, "settings.secrets",
				fmt.Sprintf("枚举 Bitwarden secret bundles 失败（仍展示本机与 vault 候选）: %v", listErr), nil)
		} else {
			remoteNames = names
			if err := secrets.RememberSecretBundles(names); err != nil {
				emit(reporter, EventWarn, "settings.secrets",
					fmt.Sprintf("写入 known_secret_bundles 失败: %v", err), nil)
			} else if refreshed, loadErr := secrets.LoadConfig(); loadErr == nil {
				cfg = refreshed
			}
		}
	}

	state.AvailableSecretBundles = listUserSecretBundleCandidates(
		state.UserEnabledBundles,
		cfg.KnownSecretBundleNames(),
		remoteNames,
		reporter,
	)
	return nil
}

// listUserSecretBundleCandidates 合并 vault / known / 已启用 / 本次远端枚举。
func listUserSecretBundleCandidates(userEnabled, known, remote []string, reporter Reporter) []string {
	reporter = defaultReporter(reporter)
	parts := [][]string{userEnabled, known, remote}
	if vaultNames := listConnectedVaultBundleNames(reporter); len(vaultNames) > 0 {
		parts = append(parts, vaultNames)
	}
	merged := make([]string, 0)
	for _, part := range parts {
		merged = append(merged, part...)
	}
	names := secrets.NormalizeBundleNames(merged)
	sort.Strings(names)
	return names
}

func listConnectedVaultBundleNames(reporter Reporter) []string {
	connected, err := repo.IsConnected()
	if err != nil || !connected {
		return nil
	}
	tx, err := repo.NewReadTransaction()
	if err != nil {
		emit(reporter, EventWarn, "settings.secrets",
			fmt.Sprintf("打开仓库只读事务失败，Settings 将不展示 vault bundle: %v", err), nil)
		return nil
	}
	defer tx.Close()

	resolved, err := resolveDesiredAssets(nil, tx.WorkDir(), reporter)
	if err != nil {
		emit(reporter, EventWarn, "settings.secrets",
			fmt.Sprintf("扫描 vault bundles 失败: %v", err), nil)
		return nil
	}
	names := make([]string, 0, len(resolved.Bundles))
	for _, bo := range resolved.Bundles {
		names = append(names, bo.Name)
	}
	return names
}

func ConnectRepo(repoURL string, reporter Reporter) (*ConnectRepoResult, error) {
	reporter = defaultReporter(reporter)
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil, fmt.Errorf("仓库地址不能为空")
	}

	emit(reporter, EventInfo, "settings.repo", "开始连接仓库", &Progress{Phase: "connect", Current: 1, Total: 2})
	if err := repo.Connect(repoURL); err != nil {
		return nil, err
	}

	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("加载全局配置失败: %w", err)
	}
	globalConfig.RepoURL = repoURL
	if err := config.SaveGlobalConfig(globalConfig); err != nil {
		return nil, fmt.Errorf("仓库已连接，但保存全局配置失败: %w", err)
	}

	configPath, err := config.GetGlobalConfigPath()
	if err != nil {
		return nil, fmt.Errorf("获取全局配置路径失败: %w", err)
	}
	bareRepo, err := repo.GetBareRepoDir()
	if err != nil {
		return nil, fmt.Errorf("获取 bare repo 路径失败: %w", err)
	}

	emit(reporter, EventInfo, "settings.repo", "仓库连接完成", &Progress{Phase: "connect", Current: 2, Total: 2})
	return &ConnectRepoResult{
		RepoURL:    repoURL,
		ConfigPath: configPath,
		BareRepo:   bareRepo,
	}, nil
}

func SaveGlobalSettings(input SaveGlobalSettingsInput, reporter Reporter) (*SaveGlobalSettingsResult, error) {
	reporter = defaultReporter(reporter)

	var targetIDEs []string
	var err error
	if input.IDEs == nil {
		targetIDEs = ide.List()
		sort.Strings(targetIDEs)
	} else {
		targetIDEs, err = sanitizeIDESelection(input.IDEs)
		if err != nil {
			return nil, err
		}
		if len(targetIDEs) == 0 {
			return nil, fmt.Errorf("至少选择一个 IDE")
		}
	}

	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("加载全局配置失败: %w", err)
	}

	targetRepoURL, err := resolveRepoURLForGlobalSettings(strings.TrimSpace(input.RepoURL), globalConfig)
	if err != nil {
		return nil, err
	}

	emit(reporter, EventInfo, "settings.save", "开始保存全局设置", &Progress{Phase: "save", Current: 1, Total: 3})

	var savedUserBundles []string
	var secretsConfigPath string
	// 先写用户级 secrets，避免后续 repo.Connect 失败导致勾选丢失。
	if input.UserEnabledBundles != nil {
		secretsCfg, err := secrets.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("加载 secrets 配置失败: %w", err)
		}
		secretsCfg.UserEnabledBundles = secrets.NormalizeBundleNames(input.UserEnabledBundles)
		if err := secrets.SaveConfig(secretsCfg); err != nil {
			return nil, fmt.Errorf("保存用户级 secret bundles 失败: %w", err)
		}
		savedUserBundles = secretsCfg.UserEnabledBundleNames()
		if path, err := secrets.ConfigPath(); err == nil {
			secretsConfigPath = path
		}
	}

	if err := repo.Connect(targetRepoURL); err != nil {
		return nil, err
	}

	result := &SaveGlobalSettingsResult{
		RepoURL:            targetRepoURL,
		IDEs:               append([]string(nil), targetIDEs...),
		UserEnabledBundles: savedUserBundles,
		SecretsConfigPath:  secretsConfigPath,
	}
	result.InstallWarnings = append(result.InstallWarnings, EnsureBuiltinIDEAssets(targetIDEs, reporter)...)

	globalConfig.RepoURL = targetRepoURL
	globalConfig.IDEs = append([]string(nil), targetIDEs...)
	if err := config.SaveGlobalConfig(globalConfig); err != nil {
		return nil, fmt.Errorf("保存全局配置失败: %w", err)
	}

	configPath, err := config.GetGlobalConfigPath()
	if err != nil {
		return nil, fmt.Errorf("获取全局配置路径失败: %w", err)
	}
	varsCreated, err := config.EnsureGlobalVarsTemplate()
	if err != nil {
		return nil, fmt.Errorf("写入本机变量定义模板失败: %w", err)
	}
	varsPath, err := config.GetGlobalVarsPath()
	if err != nil {
		return nil, fmt.Errorf("获取本机变量定义路径失败: %w", err)
	}
	bareRepo, err := repo.GetBareRepoDir()
	if err != nil {
		return nil, fmt.Errorf("获取 bare repo 路径失败: %w", err)
	}

	result.ConfigPath = configPath
	result.VarsPath = varsPath
	result.VarsCreated = varsCreated
	result.BareRepo = bareRepo

	if len(savedUserBundles) > 0 {
		created, err := ensureVaultBundlesForUserEnable(savedUserBundles, reporter)
		if err != nil {
			return nil, err
		}
		result.CreatedVaultBundles = created
	}

	emit(reporter, EventInfo, "settings.save", "已写入全局配置与本机变量模板", &Progress{Phase: "save", Current: 3, Total: 3})
	return result, nil
}

func resolveRepoURLForGlobalSettings(inputRepoURL string, globalConfig *types.GlobalConfig) (string, error) {
	if strings.TrimSpace(inputRepoURL) != "" {
		return strings.TrimSpace(inputRepoURL), nil
	}
	if globalConfig != nil && strings.TrimSpace(globalConfig.RepoURL) != "" {
		return strings.TrimSpace(globalConfig.RepoURL), nil
	}

	connected, err := repo.IsConnected()
	if err != nil {
		return "", fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return "", fmt.Errorf("仓库未连接\n\n请先到 Settings 页配置 Repo URL")
	}

	remoteURL, err := repo.GetBareRemoteURL()
	if err != nil {
		return "", fmt.Errorf("读取当前远端失败: %w", err)
	}
	if strings.TrimSpace(remoteURL) == "" {
		return "", fmt.Errorf("当前仓库远端为空，请先到 Settings 页配置 Repo URL")
	}
	return remoteURL, nil
}

func sanitizeIDESelection(ideNames []string) ([]string, error) {
	seen := make(map[string]struct{}, len(ideNames))
	result := make([]string, 0, len(ideNames))
	for _, ideName := range ideNames {
		name := strings.TrimSpace(ideName)
		if name == "" {
			continue
		}
		if err := ValidateIDEName(name); err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func ValidateIDEName(ideName string) error {
	if ide.IsValid(ideName) {
		return nil
	}
	validIDEs := ide.List()
	sort.Strings(validIDEs)
	return fmt.Errorf("不支持的 IDE: %s (支持: %s)", ideName, strings.Join(validIDEs, ", "))
}

// EnsureBuiltinIDEAssets 为已选 IDE 同步内置 skills / rules / dec MCP（幂等）。
// TUI 启动与 Settings 保存时调用，避免「IDE 已勾选但未按 s 保存」导致 MCP 缺失。
func EnsureBuiltinIDEAssets(ideNames []string, reporter Reporter) []string {
	reporter = defaultReporter(reporter)
	var warnings []string
	for _, ideName := range ideNames {
		name := strings.TrimSpace(ideName)
		if name == "" {
			continue
		}
		if err := InstallBuiltinAssetsForIDE(name); err != nil {
			warning := fmt.Sprintf("%s: %s", name, err)
			warnings = append(warnings, warning)
			emit(reporter, EventWarn, "settings.install", warning, nil)
			continue
		}
		emit(reporter, EventInfo, "settings.install", fmt.Sprintf("已为 %s 同步内置资产", name), nil)
	}
	return warnings
}

// InstallBuiltinAssetsForIDE 为指定 IDE 安装 Dec 跟随分发的内置资产。
func InstallBuiltinAssetsForIDE(ideName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户主目录失败: %w", err)
	}

	ideImpl := ide.Get(ideName)
	userRoot := ideImpl.UserRootDir(homeDir)
	bundle := assets.GlobalAssets()

	if err := installBuiltinSkills(filepath.Join(userRoot, "skills"), bundle.Skills); err != nil {
		return fmt.Errorf("安装内置 skills 失败: %w", err)
	}
	if err := installBuiltinRules(filepath.Join(userRoot, "rules"), bundle.Rules); err != nil {
		return fmt.Errorf("安装内置 rules 失败: %w", err)
	}
	if err := installBuiltinMCPs(ideName, homeDir, bundle.MCPs); err != nil {
		return err
	}

	return nil
}

func installBuiltinSkills(skillsDir string, skills []assets.SkillAsset) error {
	if len(skills) == 0 {
		return nil
	}

	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return fmt.Errorf("创建 skills 目录失败: %w", err)
	}

	for _, skill := range skills {
		skillDir := filepath.Join(skillsDir, skill.Name)
		if err := os.RemoveAll(skillDir); err != nil {
			return fmt.Errorf("清理 skill %s 目录失败: %w", skill.Name, err)
		}
		if err := writeBuiltinFiles(skillDir, skill.Files); err != nil {
			return fmt.Errorf("安装 skill %s 失败: %w", skill.Name, err)
		}
	}

	return nil
}

func installBuiltinRules(rulesDir string, rules []assets.RuleAsset) error {
	if len(rules) == 0 {
		return nil
	}

	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("创建 rules 目录失败: %w", err)
	}

	for _, rule := range rules {
		rulePath := filepath.Join(rulesDir, rule.Name+".mdc")
		if err := os.WriteFile(rulePath, rule.Content, 0644); err != nil {
			return fmt.Errorf("写入 rule %s 失败: %w", rule.Name, err)
		}
	}

	return nil
}

// builtinDecMCPServerName 是 Dec 自身 MCP server 在 IDE 配置中的条目名。
const builtinDecMCPServerName = "dec"

func installBuiltinMCPs(ideName, homeDir string, mcps []assets.MCPAsset) error {
	if len(mcps) == 0 {
		return nil
	}

	configRoot := builtinMCPConfigRoot(ideName, homeDir)
	configPath := ide.Get(ideName).MCPConfigPath(configRoot)

	for _, asset := range mcps {
		var server types.MCPServer
		if err := json.Unmarshal(asset.Content, &server); err != nil {
			return fmt.Errorf("解析内置 MCP %s 失败: %w", asset.Name, err)
		}
		serverName := builtinDecMCPServerName
		if asset.Name != "dec" {
			serverName = managedName(asset.Name)
		}
		if isCodexIDE(ideName) {
			if err := mergeCodexBuiltinMCPEntry(ideName, configRoot, serverName, server); err != nil {
				return err
			}
			continue
		}
		if err := mergeJSONBuiltinMCPEntry(configPath, serverName, server); err != nil {
			return fmt.Errorf("写入 MCP 配置失败: %w", err)
		}
	}
	return nil
}

func isCodexIDE(ideName string) bool {
	return ideName == "codex" || ideName == "codex-internal"
}

func mergeCodexBuiltinMCPEntry(ideName, configRoot, serverName string, server types.MCPServer) error {
	ideImpl := ide.Get(ideName)
	existing, err := ideImpl.LoadMCPConfig(configRoot)
	if err != nil {
		return fmt.Errorf("加载 MCP 配置失败: %w", err)
	}
	if existing.MCPServers == nil {
		existing.MCPServers = make(map[string]types.MCPServer)
	}
	existing.MCPServers[serverName] = server
	return ideImpl.WriteMCPConfig(configRoot, existing)
}

// mergeJSONBuiltinMCPEntry 仅合并 dec MCP 条目，保留其它 server 的未知字段（如 transportType）。
func mergeJSONBuiltinMCPEntry(configPath, serverName string, server types.MCPServer) error {
	serverData, err := json.Marshal(server)
	if err != nil {
		return err
	}

	var root map[string]json.RawMessage
	if existing, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(existing, &root); err != nil {
			return fmt.Errorf("解析 MCP 配置失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if root == nil {
		root = make(map[string]json.RawMessage)
	}

	var servers map[string]json.RawMessage
	if raw, ok := root["mcpServers"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &servers)
	}
	if servers == nil {
		servers = make(map[string]json.RawMessage)
	}
	servers[serverName] = json.RawMessage(serverData)

	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	root["mcpServers"] = serversRaw

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(configPath, append(out, '\n'), 0644)
}

// builtinMCPConfigRoot 返回传给 IDE.MCPConfigPath 的根路径。
// MCPConfigPath 以 workspace / 用户主目录为根，再拼 .cursor/mcp.json 等路径。
func builtinMCPConfigRoot(_ string, homeDir string) string {
	return homeDir
}

func writeBuiltinFiles(rootDir string, files []assets.FileAsset) error {
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return err
	}

	for _, file := range files {
		fullPath := filepath.Join(rootDir, filepath.FromSlash(file.RelPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, file.Content, 0644); err != nil {
			return err
		}
	}

	return nil
}
