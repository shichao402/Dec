package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/types"
)

// Installer 负责安装工具集
type Installer struct {
	ToolsetsDir string // 工具集安装目录
	WorkDir     string // 工作目录（项目根目录）
}

// NewInstaller 创建新的安装器
func NewInstaller(toolsetsDir, workDir string) *Installer {
	return &Installer{
		ToolsetsDir: toolsetsDir,
		WorkDir:     workDir,
	}
}

// InstallToolset 安装指定的工具集
func (i *Installer) InstallToolset(toolsetInfo *types.ToolsetInfo) error {
	fmt.Printf("📦 开始安装工具集: %s\n", toolsetInfo.DisplayName)
	
	// 1. 作为 Git 子模块安装
	submodulePath := filepath.Join(i.ToolsetsDir, toolsetInfo.Name)
	if err := i.installAsSubmodule(toolsetInfo.GitHubURL, submodulePath); err != nil {
		return fmt.Errorf("安装子模块失败: %w", err)
	}
	
	// 2. 读取 toolset.json
	toolsetPath := filepath.Join(submodulePath, "toolset.json")
	toolset, err := i.loadToolset(toolsetPath)
	if err != nil {
		return fmt.Errorf("读取 toolset.json 失败: %w", err)
	}
	
	// 3. 执行安装（拷贝文件）
	if err := i.copyFiles(toolset, submodulePath); err != nil {
		return fmt.Errorf("拷贝文件失败: %w", err)
	}
	
	fmt.Printf("✅ 工具集 %s 安装完成\n", toolsetInfo.DisplayName)
	return nil
}

// installAsSubmodule 将 GitHub 仓库作为子模块安装
func (i *Installer) installAsSubmodule(githubURL, targetPath string) error {
	// 确保 toolsets 目录存在
	if err := os.MkdirAll(i.ToolsetsDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	
	// 检查是否已经是子模块
	if _, err := os.Stat(targetPath); err == nil {
		fmt.Printf("  ℹ️  子模块已存在，更新中...\n")
		// 更新子模块
		cmd := exec.Command("git", "submodule", "update", "--init", "--recursive", targetPath)
		cmd.Dir = i.WorkDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("更新子模块失败: %w", err)
		}
		return nil
	}
	
	// 添加子模块
	fmt.Printf("  📥 添加 Git 子模块: %s\n", githubURL)
	cmd := exec.Command("git", "submodule", "add", githubURL, targetPath)
	cmd.Dir = i.WorkDir
	if err := cmd.Run(); err != nil {
		// 如果添加失败，可能是 .gitmodules 不存在，尝试直接克隆
		fmt.Printf("  ⚠️  子模块添加失败，尝试直接克隆...\n")
		return i.cloneRepository(githubURL, targetPath)
	}
	
	return nil
}

// cloneRepository 直接克隆仓库（当不是 Git 仓库时使用）
func (i *Installer) cloneRepository(githubURL, targetPath string) error {
	cmd := exec.Command("git", "clone", githubURL, targetPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("克隆仓库失败: %w", err)
	}
	return nil
}

// loadToolset 加载 toolset.json
func (i *Installer) loadToolset(toolsetPath string) (*types.Toolset, error) {
	data, err := os.ReadFile(toolsetPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	
	var toolset types.Toolset
	if err := json.Unmarshal(data, &toolset); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	
	return &toolset, nil
}

// copyFiles 根据 install.targets 拷贝文件
func (i *Installer) copyFiles(toolset *types.Toolset, sourceDir string) error {
	if len(toolset.Install.Targets) == 0 {
		fmt.Printf("  ℹ️  没有需要安装的文件\n")
		return nil
	}
	
	for targetPath, target := range toolset.Install.Targets {
		if err := i.copyTarget(targetPath, target, sourceDir); err != nil {
			return fmt.Errorf("拷贝目标 %s 失败: %w", targetPath, err)
		}
	}
	
	return nil
}

// copyTarget 拷贝单个安装目标
func (i *Installer) copyTarget(targetPath string, target types.InstallTarget, sourceDir string) error {
	// 解析目标路径（相对于工作目录）
	fullTargetPath := filepath.Join(i.WorkDir, targetPath)
	
	// 解析源路径
	sourcePath := filepath.Join(sourceDir, target.Source)
	
	// 检查源路径是否存在
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return fmt.Errorf("源路径不存在: %s", sourcePath)
	}
	
	// 确保目标目录存在
	if err := os.MkdirAll(fullTargetPath, 0755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}
	
	// 处理文件模式
	if len(target.Files) == 0 {
		// 如果没有指定文件，拷贝整个目录
		return i.copyDirectory(sourcePath, fullTargetPath, target)
	}
	
	// 拷贝指定文件
	for _, filePattern := range target.Files {
		if err := i.copyFilesByPattern(sourcePath, fullTargetPath, filePattern, target); err != nil {
			return err
		}
	}
	
	return nil
}

// copyDirectory 拷贝整个目录
func (i *Installer) copyDirectory(source, target string, config types.InstallTarget) error {
	// 检查源目录是否存在
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("源目录不存在: %w", err)
	}
	
	if !sourceInfo.IsDir() {
		return fmt.Errorf("源路径不是目录: %s", source)
	}
	
	fmt.Printf("  📋 拷贝目录: %s -> %s\n", source, target)
	
	// 使用简单的递归拷贝
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// 计算相对路径
		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		
		// 跳过根目录本身
		if relPath == "." {
			return nil
		}
		
		targetPath := filepath.Join(target, relPath)
		
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		
		// 检查是否需要覆盖
		if !config.Overwrite {
			if _, err := os.Stat(targetPath); err == nil {
				fmt.Printf("    ⏭️  跳过已存在文件: %s\n", relPath)
				return nil
			}
		}
		
		// 拷贝文件
		return i.copyFile(path, targetPath, config.Executable)
	})
}

// copyFilesByPattern 根据模式拷贝文件
func (i *Installer) copyFilesByPattern(sourceDir, targetDir, pattern string, config types.InstallTarget) error {
	// 简单的通配符匹配（支持 *）
	if strings.Contains(pattern, "*") {
		return i.copyFilesByGlob(sourceDir, targetDir, pattern, config)
	}
	
	// 单个文件
	sourcePath := filepath.Join(sourceDir, pattern)
	targetPath := filepath.Join(targetDir, pattern)
	
	// 检查源文件是否存在
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		fmt.Printf("    ⚠️  源文件不存在: %s\n", sourcePath)
		return nil
	}
	
	// 检查是否需要覆盖
	if !config.Overwrite {
		if _, err := os.Stat(targetPath); err == nil {
			fmt.Printf("    ⏭️  跳过已存在文件: %s\n", pattern)
			return nil
		}
	}
	
	fmt.Printf("  📄 拷贝文件: %s -> %s\n", pattern, targetPath)
	return i.copyFile(sourcePath, targetPath, config.Executable)
}

// copyFilesByGlob 使用 glob 模式拷贝文件
func (i *Installer) copyFilesByGlob(sourceDir, targetDir, pattern string, config types.InstallTarget) error {
	matches, err := filepath.Glob(filepath.Join(sourceDir, pattern))
	if err != nil {
		return err
	}
	
	if len(matches) == 0 {
		fmt.Printf("    ⚠️  没有匹配的文件: %s\n", pattern)
		return nil
	}
	
	// 如果可执行文件且匹配多个文件，尝试选择平台特定的文件
	if config.Executable && len(matches) > 1 {
		platformFile := i.selectPlatformFile(matches)
		if platformFile != "" {
			matches = []string{platformFile}
		}
	}
	
	for _, match := range matches {
		relPath, err := filepath.Rel(sourceDir, match)
		if err != nil {
			return err
		}
		
		// 如果是可执行文件且是平台特定文件，使用基础名称
		targetFileName := relPath
		if config.Executable && i.isPlatformSpecificFile(match) {
			// 提取基础名称（去掉平台后缀）
			baseName := i.getBaseExecutableName(match)
			if baseName != "" {
				targetFileName = baseName
			}
		}
		
		targetPath := filepath.Join(targetDir, targetFileName)
		
		// 检查是否需要覆盖
		if !config.Overwrite {
			if _, err := os.Stat(targetPath); err == nil {
				fmt.Printf("    ⏭️  跳过已存在文件: %s\n", targetFileName)
				continue
			}
		}
		
		fmt.Printf("  📄 拷贝文件: %s -> %s\n", relPath, targetPath)
		if err := i.copyFile(match, targetPath, config.Executable); err != nil {
			return err
		}
	}
	
	return nil
}

// selectPlatformFile 选择当前平台的特定文件
func (i *Installer) selectPlatformFile(files []string) string {
	platform := i.getPlatformSuffix()
	
	for _, file := range files {
		if strings.Contains(file, platform) {
			return file
		}
	}
	
	// 如果没有找到平台特定文件，返回第一个非平台特定文件
	for _, file := range files {
		if !i.isPlatformSpecificFile(file) {
			return file
		}
	}
	
	// 如果都是平台特定文件，返回第一个
	if len(files) > 0 {
		return files[0]
	}
	
	return ""
}

// isPlatformSpecificFile 检查文件是否是平台特定的
func (i *Installer) isPlatformSpecificFile(file string) bool {
	platforms := []string{
		"darwin-amd64", "darwin-arm64",
		"linux-amd64", "linux-arm64",
		"windows-amd64",
	}
	
	fileName := filepath.Base(file)
	for _, platform := range platforms {
		if strings.Contains(fileName, platform) {
			return true
		}
	}
	
	return false
}

// getPlatformSuffix 获取当前平台的标识符
func (i *Installer) getPlatformSuffix() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	
	// 标准化平台名称
	if goos == "darwin" {
		goos = "darwin"
	} else if goos == "windows" {
		goos = "windows"
	}
	
	return fmt.Sprintf("%s-%s", goos, goarch)
}

// getBaseExecutableName 从平台特定文件名提取基础名称
func (i *Installer) getBaseExecutableName(file string) string {
	fileName := filepath.Base(file)
	
	// 移除平台后缀
	platforms := []string{
		"-darwin-amd64", "-darwin-arm64",
		"-linux-amd64", "-linux-arm64",
		"-windows-amd64",
		".exe",
	}
	
	result := fileName
	for _, platform := range platforms {
		if strings.HasSuffix(result, platform) {
			result = strings.TrimSuffix(result, platform)
			break
		}
	}
	
	return result
}

// copyFile 拷贝单个文件
func (i *Installer) copyFile(source, target string, executable bool) error {
	// 确保目标目录存在
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	
	// 读取源文件
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	
	// 写入目标文件
	mode := os.FileMode(0644)
	if executable {
		mode = 0755
	}
	
	if err := os.WriteFile(target, data, mode); err != nil {
		return err
	}
	
	return nil
}

