package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

// CreateRemoteProjectInput 在私仓顶层创建一个新的项目声明。
type CreateRemoteProjectInput struct {
	Name        string
	Title       string
	Description string
}

type CreateRemoteProjectResult struct {
	Name          string
	ManifestPath  string
	Committed     bool
	AlreadyExists bool
}

// CreateRemoteProject 在私仓顶层写入 <name>/dec.yaml 并推送。
// 绑定与 push 都只接受仓库中已存在的项目，新机器上的新项目必须先有这一步。
func CreateRemoteProject(in CreateRemoteProjectInput, reporter Reporter) (*CreateRemoteProjectResult, error) {
	reporter = defaultReporter(reporter)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("项目名不能为空")
	}
	if !types.IsValidProjectName(name) {
		return nil, fmt.Errorf("项目名 %q 非法：只能用小写字母、数字和连字符，例如 agentshelpme", name)
	}

	result := &CreateRemoteProjectResult{
		Name:         name,
		ManifestPath: types.ProjectManifestPath(name),
	}
	err := withAppWriteRepo(func(tx *repo.Transaction) error {
		repoDir := tx.WorkDir()
		if repositoryHasLegacyLayout(repoDir) {
			return fmt.Errorf("远端仍是旧 projects/ 或 bundles/ 结构，先完成一次性项目迁移再新建项目")
		}
		manifest := filepath.Join(repoDir, types.ProjectManifestPath(name))
		if _, statErr := os.Stat(manifest); statErr == nil {
			result.AlreadyExists = true
			emit(reporter, EventInfo, "project.create", fmt.Sprintf("项目 %q 已存在，无需新建", name), nil)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
			return fmt.Errorf("创建项目目录失败: %w", err)
		}
		declaration := types.Project{
			Name:        name,
			Title:       strings.TrimSpace(in.Title),
			Description: strings.TrimSpace(in.Description),
		}
		if declaration.Title == "" {
			declaration.Title = name
		}
		data, err := yaml.Marshal(&declaration)
		if err != nil {
			return fmt.Errorf("序列化项目声明失败: %w", err)
		}
		if err := os.WriteFile(manifest, data, 0o644); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", result.ManifestPath, err)
		}
		emit(reporter, EventInfo, "project.create", fmt.Sprintf("写入 %s", result.ManifestPath), nil)
		committed, err := tx.CommitAndPush(fmt.Sprintf("feat(%s): 新建项目", name))
		if err != nil {
			return err
		}
		result.Committed = committed
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Committed {
		emit(reporter, EventInfo, "project.create", fmt.Sprintf("项目 %q 已推送到私仓", name), nil)
	}
	return result, nil
}
