package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

// AutoInitManagedProjectResult 描述按私仓同名自动初始化的结果。
type AutoInitManagedProjectResult struct {
	Root        string
	HomeProject string
	Initialized bool
	Skipped     bool
	Reason      string
}

var (
	reLowerDigitUpper = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	reAcronymWord     = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	reNonAlnum        = regexp.MustCompile(`[^a-z0-9]+`)
)

// SuggestProjectName 把目录 basename 收成小写 kebab-case，规则与 Console suggestProjectName 一致。
func SuggestProjectName(root string) string {
	name := filepath.Base(strings.TrimSpace(root))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return ""
	}
	name = reLowerDigitUpper.ReplaceAllString(name, `${1}-${2}`)
	name = reAcronymWord.ReplaceAllString(name, `${1}-${2}`)
	name = strings.ToLower(name)
	name = reNonAlnum.ReplaceAllString(name, "-")
	return strings.Trim(name, "-")
}

// AutoInitManagedProject 若私仓存在与目录名 kebab 同名的家项目则初始化并绑定；否则跳过且不写盘。
func AutoInitManagedProject(projectRoot string, reporter Reporter) (*AutoInitManagedProjectResult, error) {
	reporter = defaultReporter(reporter)
	normalized, err := config.NormalizeManagedProjectRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	result := &AutoInitManagedProjectResult{Root: normalized}

	mgr := config.NewProjectConfigManager(normalized)
	if mgr.Exists() {
		cfg, loadErr := mgr.LoadProjectConfig()
		if loadErr == nil && cfg != nil && strings.TrimSpace(cfg.ProjectName) != "" {
			result.Skipped = true
			result.Initialized = true
			result.HomeProject = strings.TrimSpace(cfg.ProjectName)
			result.Reason = "already initialized"
			return result, nil
		}
	}

	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return nil, fmt.Errorf("仓库未连接\n\n请先到 Settings 页配置 Repo URL")
	}

	candidate := SuggestProjectName(normalized)
	if candidate == "" || !types.IsValidPName(candidate) {
		result.Skipped = true
		result.Reason = "no matching home project"
		return result, nil
	}

	available := map[string]*pmodel.Loaded{}
	if err := withLocalReadRepoDir(func(repoDir string) error {
		var scanErr error
		available, scanErr = pmodel.Scan(repoDir)
		return scanErr
	}); err != nil {
		return nil, err
	}
	if _, ok := available[candidate]; !ok {
		result.Skipped = true
		result.Reason = "no matching home project"
		emit(reporter, EventInfo, "projects.auto_init",
			fmt.Sprintf("跳过 %s：私仓无同名家项目 %q", normalized, candidate), nil)
		return result, nil
	}

	if _, err := PrepareProjectConfigInit(normalized, reporter); err != nil {
		return nil, err
	}
	if _, err := BindManagedProject(normalized, candidate); err != nil {
		return nil, err
	}

	result.Initialized = true
	result.HomeProject = candidate
	emit(reporter, EventInfo, "projects.auto_init",
		fmt.Sprintf("已自动初始化 %s → %s", normalized, candidate), nil)
	return result, nil
}
