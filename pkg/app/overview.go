package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

type ProjectOverview struct {
	ProjectRoot        string
	// ProjectName 是项目短名。优先取 ProjectConfig.ProjectName；未设置时回退到 filepath.Base(ProjectRoot)。
	// 回退值不会写回 yaml，只用于展示和工具调用。
	ProjectName        string
	// ProjectNameFromConfig 标识 ProjectName 是否直接来自 .dec/config.yaml；为 false 说明是 basename 回退。
	// TUI 等展示层可据此提示用户是否已显式命名。
	ProjectNameFromConfig bool
	RepoConnected      bool
	RepoRemoteURL      string
	ProjectConfigPath  string
	ProjectConfigReady bool
	VarsPath           string
	VarsFileReady      bool
	AvailableCount     int
	EnabledCount       int
	// EnabledBundleCount 记录 project config 中 enabled_bundles 声明的数量。
	// 不代表解析后展开的成员数量，只是 config 层面的引用数。
	EnabledBundleCount int
	// Bundles 在仓库已连接时填充：扫描所有 vault 内的 bundle 声明，标注是否启用。
	// 未连接仓库或仓库读取失败时保持为 nil，调用方应容忍空列表。
	Bundles []BundleOverview
	IDEs        []string
	IDEWarnings []string
	Editor      string
}

func LoadProjectOverview(projectRoot string) (*ProjectOverview, error) {
	overview := &ProjectOverview{ProjectRoot: projectRoot}

	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	overview.RepoConnected = connected
	if connected {
		remoteURL, err := repo.GetBareRemoteURL()
		if err != nil {
			return nil, fmt.Errorf("读取仓库远端失败: %w", err)
		}
		overview.RepoRemoteURL = remoteURL
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	overview.ProjectConfigPath = filepath.Join(mgr.GetDecDir(), "config.yaml")
	overview.VarsPath = mgr.GetVarsPath()
	overview.ProjectConfigReady = mgr.Exists()

	var projectConfig *types.ProjectConfig
	if overview.ProjectConfigReady {
		loaded, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		projectConfig = loaded
		overview.AvailableCount = loaded.Available.Count()
		overview.EnabledCount = loaded.Enabled.Count()
		overview.EnabledBundleCount = len(loaded.EnabledBundles)
	}

	overview.ProjectName, overview.ProjectNameFromConfig = ResolveProjectName(projectRoot, projectConfig)

	// 仓库已连接时扫描 vault 内的 bundle 声明，并根据 EnabledBundles 标记启用状态。
	// 失败时不阻塞 overview（bundle 是增量能力，项目级配置仍应可读）。
	if connected {
		tx, txErr := repo.NewReadTransaction()
		if txErr == nil {
			resolved, resolveErr := resolveDesiredAssets(projectConfig, tx.WorkDir(), nil)
			if resolveErr == nil {
				overview.Bundles = resolved.Bundles
			}
			tx.Close()
		}
	}

	selection, err := config.ResolveEffectiveIDEs(projectConfig)
	if err != nil {
		return nil, err
	}
	overview.IDEs = append([]string(nil), selection.IDEs...)
	overview.IDEWarnings = append([]string(nil), selection.Warnings...)

	editorCmd, err := config.GetEffectiveEditor(projectConfig)
	if err != nil {
		return nil, err
	}
	overview.Editor = editorCmd

	if _, err := os.Stat(overview.VarsPath); err == nil {
		overview.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查项目变量文件失败: %w", err)
	}

	return overview, nil
}

// ResolveProjectName 按优先级解析项目短名：
//   1. ProjectConfig.ProjectName（显式配置）
//   2. filepath.Base(projectRoot)（fallback，不写回 yaml）
//
// 第二个返回值 fromConfig 标识结果是否来自 1。调用方可据此决定是否提示用户显式命名。
// projectRoot 为空时（理论上不该发生）第二段回退到 "unknown"。
func ResolveProjectName(projectRoot string, cfg *types.ProjectConfig) (string, bool) {
	if cfg != nil {
		name := strings.TrimSpace(cfg.ProjectName)
		if name != "" {
			return name, true
		}
	}
	base := strings.TrimSpace(filepath.Base(projectRoot))
	if base == "" || base == "." || base == "/" {
		return "unknown", false
	}
	return base, false
}
