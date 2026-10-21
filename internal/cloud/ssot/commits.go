package ssot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitPaths 返回某个提交改动过的全部路径。
//
// 用于历史查询：一个提交算“改动过某个对象”，当且仅当它改过的路径落在该对象的路径前缀下。
// 对根提交（没有父提交）返回整棵树的所有文件，否则返回与第一父提交的差异。
func (s *Store) CommitPaths(commit string) ([]string, error) {
	c, err := s.repo.CommitObject(plumbing.NewHash(commit))
	if err != nil {
		return nil, fmt.Errorf("%w: commit %s", ErrNotFound, commit)
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, fmt.Errorf("读取提交树失败: %w", err)
	}

	parent, parentErr := c.Parent(0)
	if parentErr != nil {
		// 根提交：整棵树新增。
		var paths []string
		walker := object.NewTreeWalker(tree, true, nil)
		defer walker.Close()
		for {
			name, entry, err := walker.Next()
			if err != nil {
				break
			}
			if entry.Mode.IsFile() {
				paths = append(paths, name)
			}
		}
		sort.Strings(paths)
		return paths, nil
	}

	parentTree, err := parent.Tree()
	if err != nil {
		return nil, fmt.Errorf("读取父提交树失败: %w", err)
	}
	changes, err := object.DiffTree(parentTree, tree)
	if err != nil {
		return nil, fmt.Errorf("计算提交差异失败: %w", err)
	}
	seen := map[string]bool{}
	var paths []string
	for _, ch := range changes {
		for _, name := range []string{ch.From.Name, ch.To.Name} {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// TreeFiles 返回某个提交里的全部文件路径。
func (s *Store) TreeFiles(commit string) ([]string, error) {
	c, err := s.repo.CommitObject(plumbing.NewHash(commit))
	if err != nil {
		return nil, fmt.Errorf("%w: commit %s", ErrNotFound, commit)
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	var paths []string
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if err != nil {
			break
		}
		if entry.Mode.IsFile() {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// HasCommit 报告提交是否存在于本仓库。
func (s *Store) HasCommit(commit string) bool {
	if len(commit) != 40 || strings.Trim(commit, "0123456789abcdef") != "" {
		return false
	}
	_, err := s.repo.CommitObject(plumbing.NewHash(commit))
	return err == nil
}
