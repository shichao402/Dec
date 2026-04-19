package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/shichao402/Dec/pkg/vars"
)

var operationsExecCommand = exec.Command

const defaultMiseLocalTomlContent = `[env]
DEC_PLACEHOLDER = "replace-me"
`

type PullProjectAssetsResult struct {
	ProjectRoot         string
	RequestedCount      int
	PulledCount         int
	FailedCount         int
	SkippedReason       string
	EffectiveIDEs       []string
	IDEWarnings         []string
	ValidationWarnings  []string
	MigrationNotes      []string
	CleanedAssets       []string
	VersionCommit       string
	MiseLocalCreated    bool
	GitignoreUpdated    bool
	MiseTrustSucceeded  bool
	NonFatalWarnings    []string
}

func PullProjectAssets(projectRoot, version string, reporter Reporter) (*PullProjectAssetsResult, error) {
	reporter = defaultReporter(reporter)
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}

	result := &PullProjectAssetsResult{ProjectRoot: projectRoot}
	if projectConfig.Enabled.IsEmpty() {
		result.SkippedReason = "config.yaml 中没有已启用的资产"
		emit(reporter, EventInfo, "pull.prepare", result.SkippedReason, nil)
		emit(reporter, EventInfo, "pull.prepare", "运行 dec config init 选择需要的资产", nil)
		return result, nil
	}

	validAssets := projectConfig.Enabled.All()
	if projectConfig.Available != nil && !projectConfig.Available.IsEmpty() {
		validAssets = make([]types.TypedAssetRef, 0, len(projectConfig.Enabled.All()))
		for _, asset := range projectConfig.Enabled.All() {
			if projectConfig.Available.FindAsset(asset.Type, asset.Name, asset.Vault) == nil {
				warning := fmt.Sprintf("[%-5s] %s (vault: %s) — 不在 available 中（可能拼写错误或已被删除）", asset.Type, asset.Name, asset.Vault)
				result.ValidationWarnings = append(result.ValidationWarnings, warning)
				emit(reporter, EventWarn, "pull.validate", warning, nil)
				continue
			}
			validAssets = append(validAssets, asset)
		}
	}

	if len(validAssets) == 0 {
		result.SkippedReason = "没有有效的已启用资产可拉取"
		emit(reporter, EventInfo, "pull.prepare", result.SkippedReason, nil)
		return result, nil
	}
	result.RequestedCount = len(validAssets)

	ideSelection, err := config.ResolveEffectiveIDEs(projectConfig)
	if err != nil {
		return nil, fmt.Errorf("解析有效 IDE 失败: %w", err)
	}
	result.IDEWarnings = append(result.IDEWarnings, ideSelection.Warnings...)
	for _, warning := range ideSelection.Warnings {
		emit(reporter, EventWarn, "pull.ide", warning, nil)
	}

	projectIDEs := uniqueProjectIDEs(projectRoot, ideSelection.IDEs)
	result.EffectiveIDEs = projectIDENames(projectIDEs)

	migrationNotes, err := migrateLegacyProjectLayouts(projectRoot, projectIDEs)
	if err != nil {
		return nil, fmt.Errorf("迁移旧版项目布局失败: %w", err)
	}
	result.MigrationNotes = append(result.MigrationNotes, migrationNotes...)
	for _, note := range migrationNotes {
		emit(reporter, EventInfo, "pull.migrate", note, nil)
	}

	result.CleanedAssets = cleanupRemovedAssets(projectRoot, projectConfig.Enabled.All(), projectIDEs)
	if len(result.CleanedAssets) > 0 {
		emit(reporter, EventInfo, "pull.cleanup", fmt.Sprintf("🧹 清理 %d 个不再启用的资产", len(result.CleanedAssets)), nil)
		for _, asset := range result.CleanedAssets {
			emit(reporter, EventInfo, "pull.cleanup", asset, nil)
		}
	}

	createTx := func() (*repo.Transaction, error) {
		if strings.TrimSpace(version) != "" {
			return repo.NewReadTransactionAt(version)
		}
		return repo.NewReadTransaction()
	}

	tx, err := createTx()
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	repoDir := tx.WorkDir()
	emit(reporter, EventInfo, "pull.start", fmt.Sprintf("📥 拉取 %d 个已启用资产", len(validAssets)), &Progress{Phase: "pull", Current: 0, Total: len(validAssets)})

	for idx, asset := range validAssets {
		progress := &Progress{Phase: "pull", Current: idx + 1, Total: len(validAssets)}
		fullPath := resolveAssetFile(repoDir, asset.Vault, asset.Type, asset.Name)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			result.FailedCount++
			emit(reporter, EventWarn, "pull.asset", fmt.Sprintf("⚠️  [%-5s] %s (vault: %s) — 远程不存在", asset.Type, asset.Name, asset.Vault), progress)
			continue
		}

		cachePath := getCachePath(projectRoot, asset.Vault, asset.Type, asset.Name)
		switch asset.Type {
		case "skill":
			if err := copyDir(fullPath, cachePath); err != nil {
				result.FailedCount++
				emit(reporter, EventWarn, "pull.asset", fmt.Sprintf("⚠️  [%-5s] %s 缓存失败: %v", asset.Type, asset.Name, err), progress)
				continue
			}
		case "rule", "mcp":
			if err := copyFile(fullPath, cachePath); err != nil {
				result.FailedCount++
				emit(reporter, EventWarn, "pull.asset", fmt.Sprintf("⚠️  [%-5s] %s 缓存失败: %v", asset.Type, asset.Name, err), progress)
				continue
			}
		}

		if err := installAssetToIDEs(asset.Type, asset.Name, fullPath, projectRoot, projectIDEs); err != nil {
			result.FailedCount++
			emit(reporter, EventWarn, "pull.asset", fmt.Sprintf("⚠️  [%-5s] %s (%v)", asset.Type, asset.Name, err), progress)
			continue
		}

		substituteAssetVars(asset.Type, asset.Name, projectRoot, projectIDEs, mgr, reporter)

		result.PulledCount++
		emit(reporter, EventInfo, "pull.asset", fmt.Sprintf("✅ [%-5s] %s (vault: %s)", asset.Type, asset.Name, asset.Vault), progress)
	}

	commitHash := tx.CommitHash()
	if commitHash != "" {
		result.VersionCommit = commitHash
		saveVersionMeta(projectRoot, commitHash)
	}

	if result.PulledCount > 0 {
		createdMise, err := ensureMiseLocalTomlFile(projectRoot)
		if createdMise {
			result.MiseLocalCreated = true
			emit(reporter, EventInfo, "pull.finalize", "📝 已创建 mise.local.toml", nil)
		}
		if err != nil {
			result.NonFatalWarnings = append(result.NonFatalWarnings, err.Error())
			emit(reporter, EventWarn, "pull.finalize", err.Error(), nil)
		} else {
			updatedGitignore, err := ensureMiseLocalTomlGitignore(projectRoot)
			if updatedGitignore {
				result.GitignoreUpdated = true
				emit(reporter, EventInfo, "pull.finalize", "🙈 已将 mise.local.toml 加入 .gitignore", nil)
			}
			if err != nil {
				result.NonFatalWarnings = append(result.NonFatalWarnings, err.Error())
				emit(reporter, EventWarn, "pull.finalize", err.Error(), nil)
			} else if err := trustMiseLocalToml(projectRoot); err != nil {
				result.NonFatalWarnings = append(result.NonFatalWarnings, err.Error())
				emit(reporter, EventWarn, "pull.finalize", err.Error(), nil)
			} else {
				result.MiseTrustSucceeded = true
				emit(reporter, EventInfo, "pull.finalize", "🔐 已执行 mise trust mise.local.toml", nil)
			}
		}
	}

	summary := fmt.Sprintf("✅ 完成：%d 个资产已拉取", result.PulledCount)
	if result.FailedCount > 0 {
		summary += fmt.Sprintf("，%d 个失败", result.FailedCount)
	}
	if len(result.EffectiveIDEs) > 0 {
		summary += fmt.Sprintf(" (IDE: %s)", strings.Join(result.EffectiveIDEs, ", "))
	}
	emit(reporter, EventInfo, "pull.finish", summary, &Progress{Phase: "done", Current: len(validAssets), Total: len(validAssets)})

	return result, nil
}

func uniqueProjectIDEs(projectRoot string, ideNames []string) []ide.IDE {
	result := make([]ide.IDE, 0, len(ideNames))
	seen := make(map[string]struct{}, len(ideNames))

	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)
		key := strings.Join([]string{
			filepath.Clean(ideImpl.SkillsDir(projectRoot)),
			filepath.Clean(ideImpl.RulesDir(projectRoot)),
			filepath.Clean(ideImpl.MCPConfigPath(projectRoot)),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ideImpl)
	}

	return result
}

func projectIDENames(projectIDEs []ide.IDE) []string {
	names := make([]string, 0, len(projectIDEs))
	for _, ideImpl := range projectIDEs {
		names = append(names, ideImpl.Name())
	}
	return names
}

func migrateLegacyProjectLayouts(projectRoot string, projectIDEs []ide.IDE) ([]string, error) {
	var notes []string
	needClaude := false
	needCodex := false

	claudeMCPPath := filepath.Join(projectRoot, ".claude", "mcp.json")
	codexMCPPath := filepath.Join(projectRoot, ".codex", "config.toml")
	for _, ideImpl := range projectIDEs {
		switch filepath.Clean(ideImpl.MCPConfigPath(projectRoot)) {
		case claudeMCPPath:
			needClaude = true
		case codexMCPPath:
			needCodex = true
		}
	}

	if needClaude {
		migrated, err := ide.MigrateLegacyClaudeProject(projectRoot)
		if err != nil {
			return nil, err
		}
		notes = append(notes, migrated...)
	}
	if needCodex {
		migrated, err := ide.MigrateLegacyCodexProject(projectRoot)
		if err != nil {
			return nil, err
		}
		notes = append(notes, migrated...)
	}

	return notes, nil
}

func cleanupRemovedAssets(projectRoot string, enabledAssets []types.TypedAssetRef, projectIDEs []ide.IDE) []string {
	cacheDir := filepath.Join(projectRoot, ".dec", "cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil
	}

	enabledSet := make(map[string]bool)
	for _, asset := range enabledAssets {
		enabledSet[asset.Vault+":"+asset.Type+":"+asset.Name] = true
	}

	vaultDirs, _ := os.ReadDir(cacheDir)
	var removed []string
	for _, vaultDir := range vaultDirs {
		if !vaultDir.IsDir() {
			continue
		}
		vaultName := vaultDir.Name()
		for _, sub := range []string{"skills", "rules", "mcp"} {
			subDir := filepath.Join(cacheDir, vaultName, sub)
			entries, err := os.ReadDir(subDir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				name := entry.Name()
				assetType := sub
				if sub == "rules" {
					assetType = "rule"
					name = strings.TrimSuffix(name, ".mdc")
				} else if sub == "mcp" {
					name = strings.TrimSuffix(name, ".json")
				} else {
					assetType = "skill"
				}

				key := vaultName + ":" + assetType + ":" + name
				if enabledSet[key] {
					continue
				}

				for _, ideImpl := range projectIDEs {
					_, _ = removeAssetFromIDE(assetType, name, projectRoot, ideImpl)
				}
				_ = os.RemoveAll(filepath.Join(subDir, entry.Name()))
				removed = append(removed, fmt.Sprintf("[%-5s] %s (vault: %s)", assetType, name, vaultName))
			}
		}
	}

	sort.Strings(removed)
	return removed
}

func typeSubDir(itemType string) string {
	switch itemType {
	case "skill":
		return "skills"
	case "rule":
		return "rules"
	case "mcp":
		return "mcp"
	default:
		return ""
	}
}

func resolveAssetFile(repoDir, vault, itemType, assetName string) string {
	base := filepath.Join(repoDir, vault, typeSubDir(itemType))
	switch itemType {
	case "skill":
		return filepath.Join(base, assetName)
	case "rule":
		return filepath.Join(base, assetName+".mdc")
	case "mcp":
		return filepath.Join(base, assetName+".json")
	default:
		return ""
	}
}

func getCachePath(projectRoot, vault, itemType, assetName string) string {
	base := filepath.Join(projectRoot, ".dec", "cache", vault, typeSubDir(itemType))
	switch itemType {
	case "skill":
		return filepath.Join(base, assetName)
	case "rule":
		return filepath.Join(base, assetName+".mdc")
	case "mcp":
		return filepath.Join(base, assetName+".json")
	default:
		return ""
	}
}

func managedName(name string) string {
	if strings.HasPrefix(name, "dec-") {
		return name
	}
	return "dec-" + name
}

func installAssetToIDEs(itemType, assetName, srcPath, projectRoot string, projectIDEs []ide.IDE) error {
	installed := make([]ide.IDE, 0, len(projectIDEs))

	for _, ideImpl := range projectIDEs {
		if err := installAssetToIDE(itemType, assetName, srcPath, projectRoot, ideImpl); err != nil {
			rollbackErrors := rollbackInstalledAsset(itemType, assetName, projectRoot, installed)
			if len(rollbackErrors) > 0 {
				return fmt.Errorf("安装到 %s 失败: %v；回滚失败: %s", ideImpl.Name(), err, strings.Join(rollbackErrors, "; "))
			}
			return fmt.Errorf("安装到 %s 失败: %w", ideImpl.Name(), err)
		}
		installed = append(installed, ideImpl)
	}

	return nil
}

func rollbackInstalledAsset(itemType, assetName, projectRoot string, installed []ide.IDE) []string {
	var rollbackErrors []string
	for i := len(installed) - 1; i >= 0; i-- {
		ideImpl := installed[i]
		removed, err := removeAssetFromIDE(itemType, assetName, projectRoot, ideImpl)
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: %v", ideImpl.Name(), err))
		} else if !removed {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: 未找到已安装资产", ideImpl.Name()))
		}
	}
	return rollbackErrors
}

func installAssetToIDE(itemType, assetName, srcPath, projectRoot string, ideImpl ide.IDE) error {
	managed := managedName(assetName)

	switch itemType {
	case "skill":
		return copyDir(srcPath, filepath.Join(ideImpl.SkillsDir(projectRoot), managed))
	case "rule":
		destDir := ideImpl.RulesDir(projectRoot)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}
		return copyFile(srcPath, filepath.Join(destDir, managed+".mdc"))
	case "mcp":
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("读取 MCP 配置失败: %w", err)
		}
		var server types.MCPServer
		if err := json.Unmarshal(data, &server); err != nil {
			return fmt.Errorf("解析 MCP 配置失败: %w", err)
		}
		existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
		if err != nil {
			return fmt.Errorf("加载 IDE MCP 配置失败: %w", err)
		}
		if existingConfig.MCPServers == nil {
			existingConfig.MCPServers = make(map[string]types.MCPServer)
		}
		existingConfig.MCPServers[managed] = server
		return ideImpl.WriteMCPConfig(projectRoot, existingConfig)
	default:
		return nil
	}
}

func removeAssetFromIDE(itemType, assetName, projectRoot string, ideImpl ide.IDE) (bool, error) {
	managed := managedName(assetName)

	switch itemType {
	case "skill":
		destDir := filepath.Join(ideImpl.SkillsDir(projectRoot), managed)
		if _, err := os.Stat(destDir); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, os.RemoveAll(destDir)
	case "rule":
		destPath := filepath.Join(ideImpl.RulesDir(projectRoot), managed+".mdc")
		if err := os.Remove(destPath); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, nil
	case "mcp":
		existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
		if err != nil {
			return false, nil
		}
		if _, exists := existingConfig.MCPServers[managed]; !exists {
			return false, nil
		}
		delete(existingConfig.MCPServers, managed)
		return true, ideImpl.WriteMCPConfig(projectRoot, existingConfig)
	default:
		return false, nil
	}
}

func substituteAssetVars(itemType, assetName, projectRoot string, projectIDEs []ide.IDE, mgr *config.ProjectConfigManager, reporter Reporter) {
	globalVars, _ := config.LoadGlobalVars()
	projectVars, _ := mgr.LoadVarsConfig()
	projectVarsPath := mgr.GetVarsPath()
	globalVarsPath, _ := config.GetGlobalVarsPath()

	if (globalVars == nil || len(globalVars.Vars) == 0) && (projectVars == nil || len(projectVars.Vars) == 0) {
		if globalVars != nil && globalVars.Assets != nil {
			// 可能有资产级变量，继续。
		} else if projectVars != nil && projectVars.Assets != nil {
			// 可能有资产级变量，继续。
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
				emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("变量替换失败 (%s): %v", ideName, err), nil)
				continue
			}
			emitMissingVars(reporter, itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)
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
				emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("变量替换失败 (%s): %v", ideName, err), nil)
				continue
			}
			emitMissingVars(reporter, itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)
		case "mcp":
			_, missing, locations := substituteMCPVars(assetName, projectRoot, ideImpl, globalVars, projectVars, reporter)
			emitMissingVars(reporter, itemType, assetName, missing, locations, projectVarsPath, globalVarsPath)
		}
	}
}

func substituteMCPVars(assetName, projectRoot string, ideImpl ide.IDE, globalVars, projectVars *types.VarsConfig, reporter Reporter) (map[string]string, []string, map[string][]string) {
	managed := managedName(assetName)
	configPath := ideImpl.MCPConfigPath(projectRoot)

	existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
	if err != nil {
		emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("加载 MCP 配置失败: %v", err), nil)
		return nil, nil, nil
	}

	server, ok := existingConfig.MCPServers[managed]
	if !ok {
		return nil, nil, nil
	}

	var allContent string
	if server.Env != nil {
		for _, value := range server.Env {
			allContent += value + "\n"
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
		for key, value := range server.Env {
			newVal, usedVars, missingVars := vars.Substitute(value, resolved)
			newEnv[key] = newVal
			for usedKey, usedValue := range usedVars {
				used[usedKey] = usedValue
			}
			missing = append(missing, missingVars...)
		}
		server.Env = newEnv
	}

	for idx, arg := range server.Args {
		newArg, usedVars, missingVars := vars.Substitute(arg, resolved)
		server.Args[idx] = newArg
		for usedKey, usedValue := range usedVars {
			used[usedKey] = usedValue
		}
		missing = append(missing, missingVars...)
	}

	newCommand, usedVars, missingVars := vars.Substitute(server.Command, resolved)
	server.Command = newCommand
	for usedKey, usedValue := range usedVars {
		used[usedKey] = usedValue
	}
	missing = append(missing, missingVars...)

	if len(used) > 0 {
		existingConfig.MCPServers[managed] = server
		if err := ideImpl.WriteMCPConfig(projectRoot, existingConfig); err != nil {
			emit(reporter, EventWarn, "pull.vars", fmt.Sprintf("写入 MCP 配置失败: %v", err), nil)
		}
	}

	return used, missing, locations
}

func emitMissingVars(reporter Reporter, itemType, assetName string, missing []string, locations map[string][]string, projectVarsPath, globalVarsPath string) {
	seen := map[string]bool{}
	for _, placeholder := range missing {
		if seen[placeholder] {
			continue
		}
		lines := []string{
			fmt.Sprintf("变量 {{%s}} 未定义", placeholder),
			fmt.Sprintf("资产: [%s] %s", itemType, assetName),
		}
		for _, location := range formatPlaceholderLocations(locations[placeholder]) {
			lines = append(lines, "来源: "+location)
		}
		lines = append(lines, fmt.Sprintf("项目级: %s -> vars.%s 或 assets.%s.%s.vars.%s", projectVarsPath, placeholder, itemType, assetName, placeholder))
		if strings.TrimSpace(globalVarsPath) != "" {
			lines = append(lines, fmt.Sprintf("本机级: %s -> vars.%s 或 assets.%s.%s.vars.%s", globalVarsPath, placeholder, itemType, assetName, placeholder))
		}
		emit(reporter, EventWarn, "pull.vars", strings.Join(lines, "\n"), nil)
		seen[placeholder] = true
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
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func saveVersionMeta(projectRoot, commitHash string) {
	versionPath := filepath.Join(projectRoot, ".dec", ".version")
	content := fmt.Sprintf("commit: %s\npulled_at: %q\n", commitHash, time.Now().Format(time.RFC3339))
	_ = os.MkdirAll(filepath.Dir(versionPath), 0755)
	_ = os.WriteFile(versionPath, []byte(content), 0644)
}

func ensureMiseLocalTomlFile(projectRoot string) (bool, error) {
	trustFile := filepath.Join(projectRoot, "mise.local.toml")
	if _, err := os.Stat(trustFile); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("检查 mise.local.toml 失败: %w", err)
	}

	if err := os.WriteFile(trustFile, []byte(defaultMiseLocalTomlContent), 0644); err != nil {
		return false, fmt.Errorf("创建 mise.local.toml 失败: %w", err)
	}

	return true, nil
}

func ensureMiseLocalTomlGitignore(projectRoot string) (bool, error) {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("读取 .gitignore 失败: %w", err)
		}
		if err := os.WriteFile(gitignorePath, []byte("mise.local.toml\n"), 0644); err != nil {
			return false, fmt.Errorf("写入 .gitignore 失败: %w", err)
		}
		return true, nil
	}

	content := string(data)
	if gitignoreHasMiseLocalTomlEntry(content) {
		return false, nil
	}

	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "mise.local.toml\n"

	if err := os.WriteFile(gitignorePath, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("更新 .gitignore 失败: %w", err)
	}

	return true, nil
}

func gitignoreHasMiseLocalTomlEntry(content string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "mise.local.toml" || trimmed == "/mise.local.toml" {
			return true
		}
	}
	return false
}

func trustMiseLocalToml(projectRoot string) error {
	trustFile := filepath.Join(projectRoot, "mise.local.toml")
	if _, err := os.Stat(trustFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("未找到 mise.local.toml")
		}
		return fmt.Errorf("检查 mise.local.toml 失败: %w", err)
	}

	cmd := operationsExecCommand("mise", "trust", "mise.local.toml")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("执行 mise trust mise.local.toml 失败: %w: %s", err, message)
		}
		return fmt.Errorf("执行 mise trust mise.local.toml 失败: %w", err)
	}

	return nil
}
