package app

import (
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/pmodel"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/types"
)

type SaveProjectTagsInput struct {
	Name string
	Tags []string
}

type SaveProjectTagsResult struct {
	Name      string
	Tags      []string
	Committed bool
}

// SaveProjectTags 把项目标签写进私仓 <name>/dec.yaml 并推送。
// 标签不改变启用平面；global 只表示推荐作为 Global 资产导入。
func (PWriter) SaveProjectTags(in SaveProjectTagsInput, reporter Reporter) (*SaveProjectTagsResult, error) {
	return saveProjectTags(in, reporter)
}

func saveProjectTags(in SaveProjectTagsInput, reporter Reporter) (*SaveProjectTagsResult, error) {
	reporter = defaultReporter(reporter)
	name := strings.TrimSpace(in.Name)
	if !types.IsValidProjectName(name) {
		return nil, fmt.Errorf("项目名 %q 非法，必须为小写 kebab-case", in.Name)
	}
	tags, err := pmodel.NormalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	result := &SaveProjectTagsResult{Name: name, Tags: tags}
	err = withAppWriteRepo(func(tx *repo.Transaction) error {
		loaded, loadErr := pmodel.Load(tx.WorkDir(), name)
		if loadErr != nil {
			return fmt.Errorf("加载项目 %q 失败: %w", name, loadErr)
		}
		manifest := loaded.Manifest
		if tagsEqual(manifest.Tags, tags) {
			result.Tags = append([]string(nil), manifest.Tags...)
			emit(reporter, EventInfo, "p.tags", fmt.Sprintf("项目 %s 标签未变化", name), nil)
			return nil
		}
		manifest.Tags = tags
		if err := pmodel.SaveManifest(tx.WorkDir(), manifest); err != nil {
			return err
		}
		committed, err := tx.CommitAndPush("p: update " + name + " tags")
		if err != nil {
			return err
		}
		result.Committed = committed
		result.Tags = append([]string(nil), tags...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Committed {
		emit(reporter, EventInfo, "p.tags", fmt.Sprintf("项目 %s 标签已推送到私仓：%s", name, strings.Join(result.Tags, ", ")), nil)
	}
	return result, nil
}

func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
