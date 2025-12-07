// Package setup 负责 CursorToolset 的初始化和文档同步
//
// 设计原则：
//   - 单一职责：只负责检测状态变化并同步文档
//   - 事件驱动：通过检查 .state/version 和 .state/docs_version 判断是否需要更新
//   - 幂等性：多次调用结果一致
//
// 调用时机：
//   - 在 RootCmd.PersistentPreRun 中自动调用
//   - 用户无感知，自动完成
package setup

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/firoyang/CursorToolset/pkg/config"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/state"
)

// docFiles 需要同步的文档文件列表
var docFiles = []string{
	"package-dev-guide.md",
	"release-workflow-template.yml",
}

// EnsureDocs 确保文档是最新的
// 检查 .state/version 和 .state/docs_version，如果不一致则更新文档
func EnsureDocs() error {
	needUpdate, version := state.NeedDocsUpdate()
	if !needUpdate {
		return nil
	}

	// 静默更新，不打扰用户
	if err := syncDocs(version); err != nil {
		// 文档更新失败不应阻止用户使用，只记录警告
		// 可以考虑在 verbose 模式下输出
		return nil
	}

	// 更新文档版本状态
	_ = state.SetDocsVersion(version)
	return nil
}

// syncDocs 同步文档文件
func syncDocs(version string) error {
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return err
	}

	docsDir := filepath.Join(rootDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return err
	}

	cfg := config.GetSystemConfig()
	baseURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/resources/public",
		cfg.RepoOwner, cfg.RepoName, version)

	for _, filename := range docFiles {
		url := fmt.Sprintf("%s/%s", baseURL, filename)
		destPath := filepath.Join(docsDir, filename)

		if err := downloadFile(url, destPath); err != nil {
			// 单个文件失败不影响其他文件
			continue
		}
	}

	return nil
}

// downloadFile 下载文件到指定路径
func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return os.WriteFile(destPath, data, 0644)
}

// GetDocFiles 获取文档文件列表（供其他模块使用）
func GetDocFiles() []string {
	return docFiles
}
