package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/vars"
)

// PlaceholderSourceAsset 表示占位符来自 .dec/cache/ 下的资产模板（项目级按资产覆盖）。
// 其余 source 可直接用字符串常量表达，定义 Asset 是因为构造 key 时需要带上 type/name。
const (
	PlaceholderSourceProject = "project"
	PlaceholderSourceGlobal  = "global"
	PlaceholderSourceMissing = "missing"
)

// PlaceholderStatus 描述单个占位符当前的解析结果。
type PlaceholderStatus struct {
	Name   string
	Value  string
	Source string // project | global | missing
}

// ProjectVarsView 提供 Project 页变量区块所需的只读数据。
type ProjectVarsView struct {
	VarsPath         string
	VarsFileReady    bool
	ProjectVars      map[string]string
	GlobalVars       map[string]string
	UsedPlaceholders []string
	ResolvedVars     map[string]PlaceholderStatus
	CacheExists      bool
	EditorCommand    string
	Warnings         []string
}

// LoadProjectVarsView 读取项目级变量定义 + 扫描 .dec/cache/ 里已用占位符，返回只读视图。
// 写入完全交给外部编辑器，本函数不修改任何文件。
func LoadProjectVarsView(projectRoot string) (*ProjectVarsView, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}

	mgr := config.NewProjectConfigManager(projectRoot)
	view := &ProjectVarsView{
		VarsPath:     mgr.GetVarsPath(),
		ProjectVars:  map[string]string{},
		GlobalVars:   map[string]string{},
		ResolvedVars: map[string]PlaceholderStatus{},
	}

	// 项目级 vars.yaml 是否存在
	if _, err := os.Stat(view.VarsPath); err == nil {
		view.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查变量定义文件失败: %w", err)
	}

	projectVars, err := mgr.LoadVarsConfig()
	if err != nil {
		view.Warnings = append(view.Warnings, fmt.Sprintf("解析 %s 失败: %v", view.VarsPath, err))
		projectVars = nil
	}
	if projectVars != nil && projectVars.Vars != nil {
		for k, v := range projectVars.Vars {
			view.ProjectVars[k] = v
		}
	}

	// 机器级 vars
	globalVars, err := config.LoadGlobalVars()
	if err != nil {
		view.Warnings = append(view.Warnings, fmt.Sprintf("读取全局变量失败: %v", err))
	} else if globalVars != nil && globalVars.Vars != nil {
		for k, v := range globalVars.Vars {
			view.GlobalVars[k] = v
		}
	}

	// 扫描 .dec/cache/ 中的占位符（若存在）
	cacheDir := filepath.Join(mgr.GetDecDir(), "cache")
	if info, err := os.Stat(cacheDir); err == nil && info.IsDir() {
		view.CacheExists = true
		placeholders := vars.ExtractPlaceholdersFromDir(cacheDir)
		sort.Strings(placeholders)
		view.UsedPlaceholders = placeholders
	}

	// 解析 resolve 结果（仅限当前用中的占位符）
	for _, name := range view.UsedPlaceholders {
		status := PlaceholderStatus{Name: name, Source: PlaceholderSourceMissing}
		if v, ok := view.ProjectVars[name]; ok {
			status.Value = v
			status.Source = PlaceholderSourceProject
		} else if v, ok := view.GlobalVars[name]; ok {
			status.Value = v
			status.Source = PlaceholderSourceGlobal
		}
		view.ResolvedVars[name] = status
	}

	// 有效 editor 命令
	projectConfig, loadErr := mgr.LoadProjectConfig()
	if loadErr != nil {
		view.Warnings = append(view.Warnings, fmt.Sprintf("加载项目配置失败: %v", loadErr))
	} else {
		editorCmd, err := config.GetEffectiveEditor(projectConfig)
		if err != nil {
			view.Warnings = append(view.Warnings, fmt.Sprintf("解析编辑器命令失败: %v", err))
		} else {
			view.EditorCommand = editorCmd
		}
	}

	return view, nil
}

// EnsureProjectVarsFileResult 报告模板落地的结果，created=true 表示本次调用创建了新文件。
type EnsureProjectVarsFileResult struct {
	Path    string
	Created bool
}

// EnsureProjectVarsFile 确保 .dec/vars.yaml 模板存在，不覆盖已有内容。
// 供 "点 e 时若文件不存在就先落模板再打开编辑器" 的场景复用。
func EnsureProjectVarsFile(projectRoot string) (*EnsureProjectVarsFileResult, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("项目根目录不能为空")
	}
	mgr := config.NewProjectConfigManager(projectRoot)
	created, err := mgr.EnsureVarsConfigTemplate()
	if err != nil {
		return nil, err
	}
	return &EnsureProjectVarsFileResult{
		Path:    mgr.GetVarsPath(),
		Created: created,
	}, nil
}

// MissingPlaceholders 过滤出状态为 missing 的占位符名列表。
func (v *ProjectVarsView) MissingPlaceholders() []string {
	if v == nil {
		return nil
	}
	var missing []string
	for _, name := range v.UsedPlaceholders {
		if status, ok := v.ResolvedVars[name]; ok && status.Source == PlaceholderSourceMissing {
			missing = append(missing, name)
		}
	}
	return missing
}
