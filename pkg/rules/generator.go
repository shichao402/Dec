package rules

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/types"
)

// Generator 规则生成器
type Generator struct {
	projectRoot string
	ide         string // 目标 IDE
}

// NewGenerator 创建规则生成器（默认 Cursor）
func NewGenerator(projectRoot string) *Generator {
	return &Generator{projectRoot: projectRoot, ide: "cursor"}
}

// NewGeneratorForIDE 创建指定 IDE 的规则生成器
func NewGeneratorForIDE(projectRoot, ide string) *Generator {
	return &Generator{projectRoot: projectRoot, ide: ide}
}

// RulePackInfo 规则包信息
type RulePackInfo struct {
	Name        string                 // 包名
	InstallPath string                 // 安装路径
	LocalPath   string                 // 本地开发路径（如果是链接的包）
	Pack        *types.Pack            // 包元数据
	UserConfig  map[string]interface{} // 用户配置
}

// GenerateAllRules 生成所有规则文件（核心规则 + 包规则）
func (g *Generator) GenerateAllRules(packs []RulePackInfo, enabledPacks map[string]bool) error {
	rulesDir := paths.GetIDERulesDir(g.projectRoot, g.ide)

	// 确保目录存在
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("创建规则目录失败: %w", err)
	}

	// 清理旧的托管规则（以 dec- 开头的文件）
	if err := g.cleanManagedRules(rulesDir); err != nil {
		return fmt.Errorf("清理旧规则失败: %w", err)
	}

	// 生成核心规则
	if err := g.generateCoreRules(rulesDir); err != nil {
		return fmt.Errorf("生成核心规则失败: %w", err)
	}

	// 生成内置包规则（根据 enabledPacks）
	if err := g.generateBuiltinPackRules(rulesDir, enabledPacks); err != nil {
		return fmt.Errorf("生成内置包规则失败: %w", err)
	}

	// 生成外部包规则
	for _, pack := range packs {
		if err := g.generatePackRules(pack, rulesDir); err != nil {
			return fmt.Errorf("生成规则失败 (%s): %w", pack.Name, err)
		}
	}

	return nil
}

// GenerateRules 生成规则文件（兼容旧接口）
func (g *Generator) GenerateRules(packs []RulePackInfo) error {
	return g.GenerateAllRules(packs, nil)
}

// generateCoreRules 生成核心规则（始终启用）
func (g *Generator) generateCoreRules(rulesDir string) error {
	entries, err := fs.ReadDir(EmbeddedRules, "resources/core")
	if err != nil {
		return fmt.Errorf("读取核心规则目录失败: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mdc") {
			continue
		}

		content, err := fs.ReadFile(EmbeddedRules, "resources/core/"+entry.Name())
		if err != nil {
			return fmt.Errorf("读取核心规则文件失败 (%s): %w", entry.Name(), err)
		}

		// 生成目标文件名（添加 dec- 前缀）
		dstName := fmt.Sprintf("dec-core-%s", entry.Name())
		dstPath := filepath.Join(rulesDir, dstName)

		if err := os.WriteFile(dstPath, content, 0644); err != nil {
			return fmt.Errorf("写入核心规则文件失败 (%s): %w", entry.Name(), err)
		}
	}

	return nil
}

// generateBuiltinPackRules 生成内置包规则（根据配置启用）
func (g *Generator) generateBuiltinPackRules(rulesDir string, enabledPacks map[string]bool) error {
	if enabledPacks == nil {
		return nil
	}

	entries, err := fs.ReadDir(EmbeddedRules, "resources/packs")
	if err != nil {
		return fmt.Errorf("读取包规则目录失败: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mdc") {
			continue
		}

		// 从文件名提取包名（去掉 .mdc 后缀）
		packName := strings.TrimSuffix(entry.Name(), ".mdc")

		// 检查是否启用
		if !enabledPacks[packName] {
			continue
		}

		content, err := fs.ReadFile(EmbeddedRules, "resources/packs/"+entry.Name())
		if err != nil {
			return fmt.Errorf("读取包规则文件失败 (%s): %w", entry.Name(), err)
		}

		// 生成目标文件名（添加 dec- 前缀）
		dstName := fmt.Sprintf("dec-pack-%s", entry.Name())
		dstPath := filepath.Join(rulesDir, dstName)

		if err := os.WriteFile(dstPath, content, 0644); err != nil {
			return fmt.Errorf("写入包规则文件失败 (%s): %w", entry.Name(), err)
		}
	}

	return nil
}

// GetAvailableBuiltinPacks 获取可用的内置规则包列表
func GetAvailableBuiltinPacks() ([]string, error) {
	entries, err := fs.ReadDir(EmbeddedRules, "resources/packs")
	if err != nil {
		return nil, fmt.Errorf("读取包规则目录失败: %w", err)
	}

	var packs []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mdc") {
			continue
		}
		packName := strings.TrimSuffix(entry.Name(), ".mdc")
		packs = append(packs, packName)
	}

	return packs, nil
}

// cleanManagedRules 清理托管的规则文件
func (g *Generator) cleanManagedRules(rulesDir string) error {
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// 只删除以 dec- 开头的文件（Dec 托管的规则）
		if strings.HasPrefix(entry.Name(), "dec-") {
			path := filepath.Join(rulesDir, entry.Name())
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	return nil
}

// generatePackRules 生成单个包的规则
func (g *Generator) generatePackRules(pack RulePackInfo, rulesDir string) error {
	if pack.Pack == nil {
		return nil
	}

	// 确定包的实际路径
	packPath := pack.InstallPath
	if pack.LocalPath != "" {
		packPath = pack.LocalPath
	}

	// 处理规则包的规则文件
	for _, ruleFile := range pack.Pack.Rules {
		srcPath := filepath.Join(packPath, ruleFile)
		
		// 生成目标文件名（添加 dec- 前缀）
		baseName := filepath.Base(ruleFile)
		dstName := fmt.Sprintf("dec-%s-%s", pack.Name, baseName)
		dstPath := filepath.Join(rulesDir, dstName)

		if err := g.copyRuleFile(srcPath, dstPath, pack.UserConfig); err != nil {
			return fmt.Errorf("复制规则文件失败 (%s): %w", ruleFile, err)
		}
	}

	// 处理 MCP 包附带的规则
	for _, attachedRule := range pack.Pack.AttachedRules {
		srcPath := filepath.Join(packPath, attachedRule.File)
		
		baseName := filepath.Base(attachedRule.File)
		dstName := fmt.Sprintf("dec-%s-%s", pack.Name, baseName)
		dstPath := filepath.Join(rulesDir, dstName)

		if err := g.copyRuleFile(srcPath, dstPath, pack.UserConfig); err != nil {
			return fmt.Errorf("复制附带规则失败 (%s): %w", attachedRule.File, err)
		}
	}

	return nil
}

// copyRuleFile 复制规则文件，并进行模板替换
func (g *Generator) copyRuleFile(src, dst string, config map[string]interface{}) error {
	// 读取源文件
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// 进行模板替换
	result := g.renderTemplate(string(content), config)

	// 写入目标文件
	return os.WriteFile(dst, []byte(result), 0644)
}

// renderTemplate 渲染模板，替换配置变量
// 支持 {{config.key}} 格式的变量
func (g *Generator) renderTemplate(content string, config map[string]interface{}) string {
	if config == nil {
		return content
	}

	result := content
	for key, value := range config {
		placeholder := fmt.Sprintf("{{config.%s}}", key)
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", value))
	}

	return result
}

// CopyFile 复制文件
func (g *Generator) CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// ListManagedRules 列出所有托管的规则文件
func (g *Generator) ListManagedRules() ([]string, error) {
	rulesDir := paths.GetIDERulesDir(g.projectRoot, g.ide)

	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var rules []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), "dec-") {
			rules = append(rules, entry.Name())
		}
	}

	return rules, nil
}
