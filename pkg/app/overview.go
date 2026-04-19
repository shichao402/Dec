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
	IDEs               []string
	IDEWarnings        []string
	Editor             string
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
