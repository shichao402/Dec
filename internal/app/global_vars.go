package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/config"
)

// GlobalVarsView 提供 Settings「本机变量」所需的只读数据。
// 写入完全交给外部编辑器，本函数不修改任何文件。
type GlobalVarsView struct {
	VarsPath      string
	VarsFileReady bool
	Vars          map[string]string
	EditorCommand string
	Warnings      []string
}

// LoadGlobalVarsView 读取 ~/.dec/local/vars.yaml，返回只读视图。
func LoadGlobalVarsView() (*GlobalVarsView, error) {
	varsPath, err := config.GetGlobalVarsPath()
	if err != nil {
		return nil, fmt.Errorf("获取本机变量路径失败: %w", err)
	}

	view := &GlobalVarsView{
		VarsPath: varsPath,
		Vars:     map[string]string{},
	}

	if _, err := os.Stat(view.VarsPath); err == nil {
		view.VarsFileReady = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查本机变量文件失败: %w", err)
	}

	globalVars, err := config.LoadGlobalVars()
	if err != nil {
		view.Warnings = append(view.Warnings, fmt.Sprintf("解析 %s 失败: %v", view.VarsPath, err))
	} else if globalVars != nil && globalVars.Vars != nil {
		for k, v := range globalVars.Vars {
			view.Vars[k] = v
		}
	}

	editorCmd, err := config.GetEffectiveEditor(nil)
	if err != nil {
		view.Warnings = append(view.Warnings, fmt.Sprintf("解析编辑器命令失败: %v", err))
	} else {
		view.EditorCommand = editorCmd
	}

	return view, nil
}

// EnsureGlobalVarsFileResult 报告模板落地结果；Created=true 表示本次创建了新文件。
type EnsureGlobalVarsFileResult struct {
	Path    string
	Created bool
}

// EnsureGlobalVarsFile 确保 ~/.dec/local/vars.yaml 模板存在，不覆盖已有内容。
// 供 Settings「本机变量」按 e 打开外部编辑器前复用。
func EnsureGlobalVarsFile() (*EnsureGlobalVarsFileResult, error) {
	created, err := config.EnsureGlobalVarsTemplate()
	if err != nil {
		return nil, err
	}
	varsPath, err := config.GetGlobalVarsPath()
	if err != nil {
		return nil, fmt.Errorf("获取本机变量路径失败: %w", err)
	}
	if strings.TrimSpace(varsPath) == "" {
		return nil, fmt.Errorf("本机变量路径为空")
	}
	return &EnsureGlobalVarsFileResult{
		Path:    varsPath,
		Created: created,
	}, nil
}
