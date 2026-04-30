package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/app"
	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/types"
	"github.com/spf13/cobra"
)

var pullVersion string

var execCommand = exec.Command

const defaultMiseLocalTomlContent = `[env]
DEC_PLACEHOLDER = "replace-me"
`

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "拉取已配置的资产到项目",
	Long: `根据 .dec/config.yaml 中 enabled 列表，拉取所有已启用的资产。

pull 会：
1. 校验 enabled 中的资产是否在 available 中存在
2. 清理不再启用的旧资产
3. 从远程仓库拉取资产
4. 缓存到 .dec/cache/<vault>/（这是资产的本地源副本）
5. 替换环境变量后渲染到各 IDE 目录（.codebuddy/rules/、.cursor/rules/、.claude/skills/ 等）

源 / 副本方向：

  远端仓库 ──(dec pull)──▶ .dec/cache/<vault>/ ──(渲染)──▶ .codebuddy/rules/ 等 IDE 副本
                               ▲
                               │ 这里是唯一可编辑的本地源
                               │
  本地修改 ──(编辑)──────────┘──(dec push)──▶ 回流远端

IDE 目录下的 .mdc / SKILL.md 是生成产物，顶部会带「请勿直接编辑」Markdown 注释；
直接改它们会在下次 dec pull 时被覆盖。想修改内容，请改 .dec/cache/<vault>/ 里的源文件，
再 dec push 回流远端，最后 dec pull 验证。

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

	printer := newPullCLIPrinter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	_, err = app.PullProjectAssets(cwd, pullVersion, app.ReporterFunc(printer.Emit))
	return err
}

type pullCLIPrinter struct {
	out               io.Writer
	err               io.Writer
	printedConfigWarn bool
	printedIDEWarn    bool
	printedMigration  bool
	printedCleanup    bool
}

func newPullCLIPrinter(out, err io.Writer) *pullCLIPrinter {
	return &pullCLIPrinter{out: out, err: err}
}

func (p *pullCLIPrinter) Emit(event app.OperationEvent) {
	message := strings.TrimSpace(event.Message)
	if message == "" {
		return
	}

	switch event.Scope {
	case "pull.ide":
		printWarningBlock(p.err, message)
		p.printedIDEWarn = true
	case "pull.validate":
		if !p.printedConfigWarn {
			p.finishIDEWarnings()
			fmt.Fprintln(p.out, "配置校验:")
			p.printedConfigWarn = true
		}
		fmt.Fprintf(p.out, "  ⚠️  %s\n", message)
	case "pull.migrate":
		p.finishPreSections()
		if !p.printedMigration {
			fmt.Fprintln(p.out, "🔄 检测到旧版项目布局，已自动迁移:")
			p.printedMigration = true
		}
		fmt.Fprintf(p.out, "  %s\n", message)
	case "pull.cleanup":
		p.finishPreSections()
		if strings.HasPrefix(message, "🧹") {
			fmt.Fprintln(p.out, message)
			p.printedCleanup = true
			return
		}
		if !p.printedCleanup {
			fmt.Fprintln(p.out, "🧹 清理不再启用的资产:")
			p.printedCleanup = true
		}
		fmt.Fprintf(p.out, "  %s\n", message)
	case "pull.start":
		p.finishPreSections()
		fmt.Fprintf(p.out, "%s\n\n", message)
	case "pull.asset":
		fmt.Fprintf(p.out, "  %s\n", message)
	case "pull.finalize":
		fmt.Fprintln(p.out, message)
	case "pull.finish":
		fmt.Fprintf(p.out, "\n%s\n", message)
	case "pull.prepare":
		if strings.HasPrefix(message, "运行 dec config init") {
			fmt.Fprintf(p.out, "\n%s\n", message)
			return
		}
		fmt.Fprintln(p.out, message)
	case "pull.vars":
		printIndentedEventWarning(p.out, message)
	default:
		fmt.Fprintln(p.out, message)
	}
}

func (p *pullCLIPrinter) finishIDEWarnings() {
	if !p.printedIDEWarn {
		return
	}
	fmt.Fprintln(p.err)
	p.printedIDEWarn = false
}

func (p *pullCLIPrinter) finishConfigWarnings() {
	if !p.printedConfigWarn {
		return
	}
	fmt.Fprintln(p.out)
	p.printedConfigWarn = false
}

func (p *pullCLIPrinter) finishPreSections() {
	p.finishIDEWarnings()
	p.finishConfigWarnings()
}

func printIndentedEventWarning(w io.Writer, message string) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(message), "\r\n", "\n"), "\n")
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if idx == 0 {
			fmt.Fprintf(w, "  ⚠️  %s\n", trimmed)
			continue
		}
		fmt.Fprintf(w, "      %s\n", trimmed)
	}
}

// cleanupRemovedAssets 清理不再启用的旧资产（对比 cache 目录 vs 当前 enabled）
func cleanupRemovedAssets(projectRoot string, enabledAssets []types.TypedAssetRef, projectIDEs []ide.IDE) {
	cacheDir := filepath.Join(projectRoot, ".dec", "cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return
	}

	// 构建 enabled 集合：key = "vault:type:name"
	enabledSet := make(map[string]bool)
	for _, a := range enabledAssets {
		enabledSet[a.Vault+":"+a.Type+":"+a.Name] = true
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

				key := vaultName + ":" + assetType + ":" + name
				if !enabledSet[key] {
					// 从 IDE 中移除
					for _, ideImpl := range projectIDEs {
						removeAssetFromIDE(assetType, name, projectRoot, ideImpl)
					}
					// 删除缓存
					os.RemoveAll(filepath.Join(subDir, e.Name()))
					removed = append(removed, fmt.Sprintf("[%-5s] %s (vault: %s)", assetType, name, vaultName))
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

	cmd := execCommand("mise", "trust", "mise.local.toml")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Errorf("执行 mise trust mise.local.toml 失败: %w: %s", err, msg)
		}
		return fmt.Errorf("执行 mise trust mise.local.toml 失败: %w", err)
	}

	return nil
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

// installAssetToIDEs 将资产安装到多个 IDE
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
