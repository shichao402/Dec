package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/types"
)

// ProjectSettingsState 描述 Project 页所需的项目级 IDE 选择状态。
//
// SelectedIDEs 保留项目配置中的原始值；len==0 表示当前处于"继承全局"状态，
// OverrideActive 为 false。GlobalIDEs 用于 UI 在继承模式下展示全局默认选择。
// EffectiveIDEs 来自 config.ResolveEffectiveIDEs，是最终生效的 IDE 列表。
type ProjectSettingsState struct {
	ProjectRoot        string
	ConfigPath         string
	VarsPath           string
	VarsFileReady      bool
	ProjectConfigReady bool
	AvailableIDEs      []string
	SelectedIDEs       []string
	OverrideActive     bool
	GlobalIDEs         []string
	EffectiveIDEs      []string
	IDEWarnings        []string
}

// SaveProjectSettingsInput 描述一次项目级 IDE 覆盖的写入请求。
//
// ClearOverride=true 表示删除 ProjectConfig.IDEs 字段（回落到全局）；此时 IDEs 会被忽略。
// ClearOverride=false 时 IDEs 至少需要有一个合法值，否则返回错误。
type SaveProjectSettingsInput struct {
	ProjectRoot   string
	IDEs          []string
	ClearOverride bool
}

// SaveProjectSettingsResult 报告保存后的项目级 IDE 状态。
type SaveProjectSettingsResult struct {
	ConfigPath     string
	SelectedIDEs   []string
	OverrideActive bool
	EffectiveIDEs  []string
	Warnings       []string
}

// LoadProjectSettings 加载项目级 IDE 设置。即使 .dec/config.yaml 不存在也能返回一个可展示的状态。
func LoadProjectSettings(projectRoot string, reporter Reporter) (*ProjectSettingsState, error) {
	reporter = defaultReporter(reporter)

	if strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	state := &ProjectSettingsState{
		ProjectRoot:        projectRoot,
		ConfigPath:         filepath.Join(mgr.GetDecDir(), "config.yaml"),
		VarsPath:           mgr.GetVarsPath(),
		ProjectConfigReady: mgr.Exists(),
	}

	availableIDEs := ide.List()
	sort.Strings(availableIDEs)
	state.AvailableIDEs = availableIDEs

	var projectConfig *types.ProjectConfig
	if state.ProjectConfigReady {
		loaded, err := mgr.LoadProjectConfig()
		if err != nil {
			return nil, err
		}
		projectConfig = loaded
	}

	if projectConfig != nil && len(normalizedProjectIDEs(projectConfig.IDEs)) > 0 {
		state.SelectedIDEs = append([]string(nil), projectConfig.IDEs...)
		state.OverrideActive = true
	}

	globalConfig, err := config.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	if len(globalConfig.IDEs) > 0 {
		state.GlobalIDEs = append([]string(nil), globalConfig.IDEs...)
	}

	selection, err := config.ResolveEffectiveIDEs(projectConfig)
	if err != nil {
		return nil, err
	}
	state.EffectiveIDEs = append([]string(nil), selection.IDEs...)
	state.IDEWarnings = append([]string(nil), selection.Warnings...)

	if _, err := os.Stat(state.VarsPath); err == nil {
		state.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查项目变量文件失败: %w", err)
	}

	emit(reporter, EventInfo, "project_settings.load", "项目级设置已加载", nil)
	return state, nil
}

// SaveProjectSettings 写入项目级 IDE 覆盖。ClearOverride=true 时删除 ProjectConfig.IDEs 字段。
func SaveProjectSettings(input SaveProjectSettingsInput, reporter Reporter) (*SaveProjectSettingsResult, error) {
	reporter = defaultReporter(reporter)

	if strings.TrimSpace(input.ProjectRoot) == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}

	var targetIDEs []string
	if !input.ClearOverride {
		sanitized, err := sanitizeIDESelection(input.IDEs)
		if err != nil {
			return nil, err
		}
		if len(sanitized) == 0 {
			return nil, fmt.Errorf("至少选择一个 IDE 或使用清除覆盖回落全局")
		}
		targetIDEs = sanitized
	}

	mgr := config.NewProjectConfigManager(input.ProjectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, fmt.Errorf("加载项目配置失败: %w", err)
	}

	if input.ClearOverride {
		projectConfig.IDEs = nil
	} else {
		projectConfig.IDEs = append([]string(nil), targetIDEs...)
	}

	if err := mgr.SaveProjectConfig(projectConfig); err != nil {
		return nil, fmt.Errorf("保存项目配置失败: %w", err)
	}

	// 重新从磁盘读取，确保返回结果反映序列化后的真实状态。
	reloaded, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, fmt.Errorf("重新加载项目配置失败: %w", err)
	}

	selection, err := config.ResolveEffectiveIDEs(reloaded)
	if err != nil {
		return nil, err
	}

	result := &SaveProjectSettingsResult{
		ConfigPath:    filepath.Join(mgr.GetDecDir(), "config.yaml"),
		EffectiveIDEs: append([]string(nil), selection.IDEs...),
		Warnings:      append([]string(nil), selection.Warnings...),
	}
	if len(reloaded.IDEs) > 0 {
		result.SelectedIDEs = append([]string(nil), reloaded.IDEs...)
		result.OverrideActive = true
	}

	if input.ClearOverride {
		emit(reporter, EventInfo, "project_settings.save", "已清除项目级 IDE 覆盖", nil)
	} else {
		emit(reporter, EventInfo, "project_settings.save", "已保存项目级 IDE 覆盖", nil)
	}
	return result, nil
}

// normalizedProjectIDEs 过滤空白项，用于判断 ProjectConfig.IDEs 是否“实质为空”。
func normalizedProjectIDEs(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}
