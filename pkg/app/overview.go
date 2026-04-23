package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
)

type ProjectOverview struct {
	ProjectRoot        string
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
