package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/repo"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/spf13/cobra"
)

var pullVersion string

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "拉取已配置的资产到项目",
	Long: `根据 .dec/config.yaml 中 enabled 列表，拉取所有已启用的资产。

pull 会：
1. 校验 enabled 中的资产是否在 available 中存在
2. 清理不再启用的旧资产
3. 从远程仓库拉取资产
4. 缓存到 .dec/cache/
5. 替换环境变量后安装到 IDE 目录

版本回退：
  dec pull --version <commit|tag>   # 拉取指定版本的资产

示例：
  dec pull                          # 拉取所有已启用的资产
  dec pull --version abc123         # 拉取指定版本`,
	RunE: runPull,
}

func runPull(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	mgr := config.NewProjectConfigManager(cwd)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return err
	}

	if projectConfig.Enabled.IsEmpty() {
		fmt.Println("config.yaml 中没有已启用的资产")
		fmt.Println("\n运行 dec config init 选择需要的资产")
		return nil
	}

	// 校验 enabled 中的资产是否在 available 中存在
	enabledAssets := projectConfig.Enabled.All()
	var validAssets []types.TypedAssetRef
	if projectConfig.Available != nil && !projectConfig.Available.IsEmpty() {
		var warnings []string
		for _, asset := range enabledAssets {
			if projectConfig.Available.FindAsset(asset.Type, asset.Name) == nil {
				warnings = append(warnings, fmt.Sprintf("  ⚠️  [%-5s] %s — 不在 available 中（可能拼写错误或已被删除）", asset.Type, asset.Name))
			} else {
				validAssets = append(validAssets, asset)
			}
		}
		if len(warnings) > 0 {
			fmt.Println("配置校验:")
			for _, w := range warnings {
				fmt.Println(w)
			}
			fmt.Println()
		}
	} else {
		// 没有 available 则跳过校验，全部尝试拉取
		validAssets = enabledAssets
	}

	if len(validAssets) == 0 {
		fmt.Println("没有有效的已启用资产可拉取")
		return nil
	}

	ideNames, err := config.GetEffectiveIDEs(projectConfig)
	if err != nil {
		ideNames = []string{"cursor"}
	}

	// 清理不再启用的旧资产
	cleanupRemovedAssets(cwd, enabledAssets, ideNames)

	// 创建事务
	createTx := func() (*repo.Transaction, error) {
		if pullVersion != "" {
			return repo.NewReadTransactionAt(pullVersion)
		}
		return repo.NewReadTransaction()
	}

	tx, err := createTx()
	if err != nil {
		return err
	}
	defer tx.Close()

	repoDir := tx.WorkDir()
	pulled := 0
	failed := 0

	fmt.Printf("📥 拉取 %d 个已启用资产...\n\n", len(validAssets))

	for _, asset := range validAssets {
		fullPath := resolveAssetFile(repoDir, asset.Vault, asset.Type, asset.Name)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			fmt.Printf("  ⚠️  [%-5s] %s (vault: %s) — 远程不存在\n", asset.Type, asset.Name, asset.Vault)
			failed++
			continue
		}

		// 缓存到 .dec/cache/
		cachePath := getCachePath(cwd, asset.Vault, asset.Type, asset.Name)
		switch asset.Type {
		case "skill":
			_ = copyDir(fullPath, cachePath)
		case "rule", "mcp":
			_ = copyFile(fullPath, cachePath)
		}

		// 安装到 IDE
		if err := installAssetToIDEs(asset.Type, asset.Name, fullPath, cwd, ideNames); err != nil {
			fmt.Printf("  ⚠️  [%-5s] %s (%v)\n", asset.Type, asset.Name, err)
			failed++
			continue
		}

		// 占位符替换
		substituteAssetVars(asset.Type, asset.Name, cwd, ideNames, mgr)

		fmt.Printf("  ✅ [%-5s] %s  (vault: %s)\n", asset.Type, asset.Name, asset.Vault)
		pulled++
	}

	// 记录版本
	commitHash := tx.CommitHash()
	if commitHash != "" {
		saveVersionMeta(cwd, commitHash)
	}

	fmt.Printf("\n✅ 完成：%d 个资产已拉取", pulled)
	if failed > 0 {
		fmt.Printf("，%d 个失败", failed)
	}
	fmt.Printf(" (IDE: %s)\n", strings.Join(ideNames, ", "))

	return nil
}

// cleanupRemovedAssets 清理不再启用的旧资产（对比 cache 目录 vs 当前 enabled）
func cleanupRemovedAssets(projectRoot string, enabledAssets []types.TypedAssetRef, ideNames []string) {
	cacheDir := filepath.Join(projectRoot, ".dec", "cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return
	}

	// 构建 enabled 集合：key = "type:name"
	enabledSet := make(map[string]bool)
	for _, a := range enabledAssets {
		enabledSet[a.Type+":"+a.Name] = true
	}

	// 遍历 cache 目录，找出不在 enabled 中的资产
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
			for _, e := range entries {
				name := e.Name()
				assetType := sub
				if sub == "rules" {
					assetType = "rule"
					name = strings.TrimSuffix(name, ".mdc")
				} else if sub == "mcp" {
					name = strings.TrimSuffix(name, ".json")
				} else {
					assetType = "skill"
				}

				key := assetType + ":" + name
				if !enabledSet[key] {
					// 从 IDE 中移除
					for _, ideName := range ideNames {
						ideImpl := ide.Get(ideName)
						removeAssetFromIDE(assetType, name, projectRoot, ideImpl)
					}
					// 删除缓存
					os.RemoveAll(filepath.Join(subDir, e.Name()))
					removed = append(removed, fmt.Sprintf("[%-5s] %s", assetType, name))
				}
			}
		}
	}

	if len(removed) > 0 {
		fmt.Printf("🧹 清理 %d 个不再启用的资产:\n", len(removed))
		for _, r := range removed {
			fmt.Printf("  %s\n", r)
		}
		fmt.Println()
	}
}

// saveVersionMeta 保存版本元数据到 .dec/.version
func saveVersionMeta(projectRoot, commitHash string) {
	versionPath := filepath.Join(projectRoot, ".dec", ".version")
	content := fmt.Sprintf("commit: %s\npulled_at: \"%s\"\n", commitHash, time.Now().Format(time.RFC3339))
	_ = os.MkdirAll(filepath.Dir(versionPath), 0755)
	_ = os.WriteFile(versionPath, []byte(content), 0644)
}

// installAssetToIDEs 将资产安装到多个 IDE
func installAssetToIDEs(itemType, assetName, srcPath, projectRoot string, ideNames []string) error {
	installed := make([]ide.IDE, 0, len(ideNames))

	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)
		if err := installAssetToIDE(itemType, assetName, srcPath, projectRoot, ideImpl); err != nil {
			rollbackErrors := rollbackInstalledAsset(itemType, assetName, projectRoot, installed)
			if len(rollbackErrors) > 0 {
				return fmt.Errorf("安装到 %s 失败: %v；回滚失败: %s", ideName, err, strings.Join(rollbackErrors, "; "))
			}
			return fmt.Errorf("安装到 %s 失败: %w", ideName, err)
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
	}
	return nil
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
	}
	return false, nil
}

func init() {
	pullCmd.Flags().StringVar(&pullVersion, "version", "", "拉取指定版本（commit hash 或 tag）")
	RootCmd.AddCommand(pullCmd)
}
