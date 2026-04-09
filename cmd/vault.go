package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/shichao402/Dec/pkg/vars"
)

// ========================================
// 共享辅助函数（供 search/list/pull/push 使用）
// ========================================

// repoAssetInfo 仓库中扫描到的资产信息
type repoAssetInfo struct {
	Name  string
	Type  string
	Vault string // 顶层目录名
}

// folderEntry 仓库中的顶层目录
type folderEntry struct {
	name string
	path string
}

// readFolderEntries 读取仓库中所有顶层目录
func readFolderEntries(repoDir string) ([]folderEntry, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, err
	}
	var folders []folderEntry
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			folders = append(folders, folderEntry{
				name: entry.Name(),
				path: filepath.Join(repoDir, entry.Name()),
			})
		}
	}
	return folders, nil
}

// isValidAssetType 检查资产类型是否有效
func isValidAssetType(t string) bool {
	return t == "skill" || t == "rule" || t == "mcp"
}

// typeSubDir 资产类型对应的子目录名
func typeSubDir(itemType string) string {
	switch itemType {
	case "skill":
		return "skills"
	case "rule":
		return "rules"
	case "mcp":
		return "mcp"
	}
	return ""
}

// resolveAssetFile 根据 vault + type + name 解析资产在 repo 中的完整路径
func resolveAssetFile(repoDir, vault, itemType, assetName string) string {
	base := filepath.Join(repoDir, vault, typeSubDir(itemType))
	switch itemType {
	case "skill":
		return filepath.Join(base, assetName)
	case "rule":
		return filepath.Join(base, assetName+".mdc")
	case "mcp":
		return filepath.Join(base, assetName+".json")
	}
	return ""
}

// findAssetInRepo 在整个 repo 中查找资产，返回 vault 名和完整文件路径
func findAssetInRepo(repoDir, itemType, assetName string) (string, string, error) {
	folders, err := readFolderEntries(repoDir)
	if err != nil {
		return "", "", err
	}
	for _, f := range folders {
		fullPath := resolveAssetFile(repoDir, f.name, itemType, assetName)
		if _, err := os.Stat(fullPath); err == nil {
			return f.name, fullPath, nil
		}
	}
	return "", "", fmt.Errorf("未找到 %s '%s'", itemType, assetName)
}

// listFolderAssets 列出一个顶层目录中的所有资产
func listFolderAssets(folderDir, folderName string) []repoAssetInfo {
	var assets []repoAssetInfo
	for _, subDir := range []string{"skills", "rules", "mcp"} {
		dir := filepath.Join(folderDir, subDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Name() == ".gitkeep" {
				continue
			}
			name := e.Name()
			assetType := subDir
			if subDir == "rules" {
				assetType = "rule"
				name = strings.TrimSuffix(name, ".mdc")
			} else if subDir == "mcp" {
				name = strings.TrimSuffix(name, ".json")
			} else {
				assetType = "skill"
			}
			assets = append(assets, repoAssetInfo{
				Name:  name,
				Type:  assetType,
				Vault: folderName,
			})
		}
	}
	return assets
}

// managedName 添加 dec- 前缀
func managedName(name string) string {
	if strings.HasPrefix(name, "dec-") {
		return name
	}
	return "dec-" + name
}

// getLocalAssetPath 获取资产在项目中的本地路径
func getLocalAssetPath(itemType, assetName, projectRoot string, ideImpl ide.IDE) string {
	managed := managedName(assetName)
	switch itemType {
	case "skill":
		return filepath.Join(ideImpl.SkillsDir(projectRoot), managed)
	case "rule":
		return filepath.Join(ideImpl.RulesDir(projectRoot), managed+".mdc")
	case "mcp":
		return ideImpl.MCPConfigPath(projectRoot)
	}
	return ""
}

// ========================================
// 事务辅助
// ========================================

func withReadRepoDir(fn func(string) error) error {
	globalConfig, err := config.LoadGlobalConfig()
	if err == nil {
		if err := repo.EnsureConnectedRepoMatches(globalConfig.RepoURL); err != nil {
			return err
		}
	}

	tx, err := repo.NewReadTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()
	return fn(tx.WorkDir())
}

func withWriteRepo(fn func(*repo.Transaction) error) error {
	globalConfig, err := config.LoadGlobalConfig()
	if err == nil {
		if err := repo.EnsureConnectedRepoMatches(globalConfig.RepoURL); err != nil {
			return err
		}
	}

	tx, err := repo.NewWriteTransaction()
	if err != nil {
		return err
	}
	defer tx.Close()
	return fn(tx)
}

// ========================================
// 缓存管理
// ========================================

// getCachePath 获取资产在 .dec/cache/ 中的路径
func getCachePath(projectRoot, vault, itemType, assetName string) string {
	base := filepath.Join(projectRoot, ".dec", "cache", vault, typeSubDir(itemType))
	switch itemType {
	case "skill":
		return filepath.Join(base, assetName)
	case "rule":
		return filepath.Join(base, assetName+".mdc")
	case "mcp":
		return filepath.Join(base, assetName+".json")
	}
	return ""
}

// removeCachedAsset 从 .dec/cache/ 中删除资产
func removeCachedAsset(itemType, assetName, projectRoot, vault string) {
	cachePath := getCachePath(projectRoot, vault, itemType, assetName)
	if cachePath != "" {
		os.RemoveAll(cachePath)
	}
}

// ========================================
// 占位符替换
// ========================================

// substituteAssetVars 对已安装到 IDE 目录的资产执行变量替换
func substituteAssetVars(itemType, assetName, projectRoot string, projectIDEs []ide.IDE, mgr *config.ProjectConfigManager) {
	globalVars, _ := config.LoadGlobalVars()
	projectVars, _ := mgr.LoadVarsConfig()
	projectVarsPath := mgr.GetVarsPath()
	globalVarsPath, _ := config.GetGlobalVarsPath()

	if (globalVars == nil || len(globalVars.Vars) == 0) && (projectVars == nil || len(projectVars.Vars) == 0) {
		if globalVars != nil && globalVars.Assets != nil {
			// 可能有资产级变量，继续
		} else if projectVars != nil && projectVars.Assets != nil {
			// 可能有资产级变量，继续
		} else {
			return
		}
	}

	for _, ideImpl := range projectIDEs {
		ideName := ideImpl.Name()

		switch itemType {
		case "skill":
			localPath := filepath.Join(ideImpl.SkillsDir(projectRoot), managedName(assetName))
			placeholders := vars.ExtractPlaceholdersFromDir(localPath)
			locations := vars.ExtractPlaceholderLocationsFromDir(localPath)
			if len(placeholders) == 0 {
				continue
			}
			resolved := vars.ResolveVars(globalVars, projectVars, itemType, assetName, placeholders)
			_, missing, err := vars.SubstituteDir(localPath, resolved)
			if err != nil {
				fmt.Printf("  ⚠️  变量替换失败 (%s): %v\n", ideName, err)
				continue
			}
			printMissingVars(itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)

		case "rule":
			localPath := filepath.Join(ideImpl.RulesDir(projectRoot), managedName(assetName)+".mdc")
			placeholders := vars.ExtractPlaceholdersFromFile(localPath)
			locations := vars.ExtractPlaceholderLocationsFromFile(localPath)
			if len(placeholders) == 0 {
				continue
			}
			resolved := vars.ResolveVars(globalVars, projectVars, itemType, assetName, placeholders)
			_, missing, err := vars.SubstituteFile(localPath, resolved)
			if err != nil {
				fmt.Printf("  ⚠️  变量替换失败 (%s): %v\n", ideName, err)
				continue
			}
			printMissingVars(itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)

		case "mcp":
			used, missing, locations := substituteMCPVars(assetName, projectRoot, ideImpl, globalVars, projectVars)
			_ = used
			printMissingVars(itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)
		}
	}
}

// substituteMCPVars 对 MCP 配置中的指定条目执行变量替换
func substituteMCPVars(assetName, projectRoot string, ideImpl ide.IDE, globalVars, projectVars *types.VarsConfig) (map[string]string, []string, map[string][]string) {
	managed := managedName(assetName)
	configPath := ideImpl.MCPConfigPath(projectRoot)

	existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
	if err != nil {
		return nil, nil, nil
	}

	server, ok := existingConfig.MCPServers[managed]
	if !ok {
		return nil, nil, nil
	}

	var allContent string
	if server.Env != nil {
		for _, v := range server.Env {
			allContent += v + "\n"
		}
	}
	for _, arg := range server.Args {
		allContent += arg + "\n"
	}
	allContent += server.Command

	placeholders := vars.ExtractPlaceholders(allContent)
	if len(placeholders) == 0 {
		return nil, nil, nil
	}

	locations := make(map[string][]string, len(placeholders))
	for _, placeholder := range placeholders {
		locations[placeholder] = []string{configPath}
	}

	resolved := vars.ResolveVars(globalVars, projectVars, "mcp", assetName, placeholders)

	used := make(map[string]string)
	var missing []string

	if server.Env != nil {
		newEnv := make(map[string]string)
		for k, v := range server.Env {
			newVal, u, m := vars.Substitute(v, resolved)
			newEnv[k] = newVal
			for uk, uv := range u {
				used[uk] = uv
			}
			missing = append(missing, m...)
		}
		server.Env = newEnv
	}

	for i, arg := range server.Args {
		newArg, u, m := vars.Substitute(arg, resolved)
		server.Args[i] = newArg
		for uk, uv := range u {
			used[uk] = uv
		}
		missing = append(missing, m...)
	}

	newCmd, u, m := vars.Substitute(server.Command, resolved)
	server.Command = newCmd
	for uk, uv := range u {
		used[uk] = uv
	}
	missing = append(missing, m...)

	if len(used) > 0 {
		existingConfig.MCPServers[managed] = server
		if err := ideImpl.WriteMCPConfig(projectRoot, existingConfig); err != nil {
			fmt.Printf("  ⚠️  写入 MCP 配置失败: %v\n", err)
		}
	}

	return used, missing, locations
}

// printMissingVars 打印缺失变量的警告（去重）
func printMissingVars(itemType, assetName string, missing []string, locations map[string][]string, projectVarsPath, globalVarsPath string) {
	seen := map[string]bool{}
	for _, m := range missing {
		if seen[m] {
			continue
		}
		fmt.Printf("  ⚠️  变量 {{%s}} 未定义\n", m)
		fmt.Printf("      资产: [%s] %s\n", itemType, assetName)
		for _, location := range formatPlaceholderLocations(locations[m]) {
			fmt.Printf("      来源: %s\n", location)
		}
		fmt.Printf("      项目级: %s -> vars.%s 或 assets.%s.%s.vars.%s\n", projectVarsPath, m, itemType, assetName, m)
		if strings.TrimSpace(globalVarsPath) != "" {
			fmt.Printf("      本机级: %s -> vars.%s 或 assets.%s.%s.vars.%s\n", globalVarsPath, m, itemType, assetName, m)
		}
		seen[m] = true
	}
}

func formatPlaceholderLocations(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	ordered := append([]string(nil), paths...)
	sort.Strings(ordered)

	formatted := make([]string, 0, len(ordered))
	for _, path := range ordered {
		formatted = append(formatted, filepath.Clean(path))
	}
	return formatted
}

// ========================================
// 文件操作
// ========================================

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}
