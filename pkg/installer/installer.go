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
	
	// 1. 克隆或下载工具集
	toolsetPath := filepath.Join(i.ToolsetsDir, toolsetInfo.Name)
	if err := i.cloneOrDownload(toolsetInfo.GitHubURL, toolsetPath); err != nil {
		return fmt.Errorf("下载工具集失败: %w", err)
	}
	
	// 2. 读取 toolset.json
	toolsetConfigPath := filepath.Join(toolsetPath, "toolset.json")
	toolset, err := i.loadToolset(toolsetConfigPath)
	if err != nil {
		return fmt.Errorf("读取 toolset.json 失败: %w", err)
	}
	
	// 3. 执行安装（拷贝文件）
	if err := i.copyFiles(toolset, toolsetPath); err != nil {
		return fmt.Errorf("拷贝文件失败: %w", err)
	}
	
	fmt.Printf("✅ 工具集 %s 安装完成\n", toolsetInfo.DisplayName)
	return nil
}

// cloneOrDownload 克隆或下载工具集到指定目录
func (i *Installer) cloneOrDownload(sourceURL, targetPath string) error {
	// 确保 toolsets 目录存在
	if err := os.MkdirAll(i.ToolsetsDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	
	// 检查目标目录是否已存在
	if _, err := os.Stat(targetPath); err == nil {
		fmt.Printf("  ℹ️  工具集已存在，更新中...\n")
		// 进入目录并拉取最新代码
		cmd := exec.Command("git", "pull")
		cmd.Dir = targetPath
		if err := cmd.Run(); err != nil {
			fmt.Printf("  ⚠️  更新失败，将重新克隆...\n")
			// 删除旧目录
			if err := os.RemoveAll(targetPath); err != nil {
				return fmt.Errorf("删除旧目录失败: %w", err)
			}
		} else {
			fmt.Printf("  ✅ 更新成功\n")
			return nil
		}
	}
	
	// 克隆仓库
	fmt.Printf("  📥 克隆工具集: %s\n", sourceURL)
	cmd := exec.Command("git", "clone", sourceURL, targetPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("克隆失败: %w", err)
	}
	
	fmt.Printf("  ✅ 克隆成功\n")
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
		fmt.Printf("  ⚠️  跳过目标 %s：源路径不存在 (%s)\n", targetPath, sourcePath)
		fmt.Printf("      提示：可能需要先构建工具。请查看工具集文档。\n")
		return nil // 不返回错误，允许继续安装其他目标
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
	hasMatchedFiles := false
	for _, filePattern := range target.Files {
		matched, err := i.copyFilesByPattern(sourcePath, fullTargetPath, filePattern, target)
		if err != nil {
			return err
		}
		if matched {
			hasMatchedFiles = true
		}
	}
	
	// 如果没有匹配到任何文件，给出提示
	if !hasMatchedFiles && len(target.Files) > 0 {
		fmt.Printf("  ⚠️  目标 %s：没有匹配到文件 (模式: %v)\n", targetPath, target.Files)
		fmt.Printf("      提示：可能需要先构建工具或检查文件模式。\n")
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

// copyFilesByPattern 根据模式拷贝文件，返回是否成功匹配到文件
func (i *Installer) copyFilesByPattern(sourceDir, targetDir, pattern string, config types.InstallTarget) (bool, error) {
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
		return false, nil
	}
	
	// 检查是否需要覆盖
	if !config.Overwrite {
		if _, err := os.Stat(targetPath); err == nil {
			fmt.Printf("    ⏭️  跳过已存在文件: %s\n", pattern)
			return true, nil
		}
	}
	
	fmt.Printf("  📄 拷贝文件: %s -> %s\n", pattern, targetPath)
	return true, i.copyFile(sourcePath, targetPath, config.Executable)
}

// copyFilesByGlob 使用 glob 模式拷贝文件，返回是否成功匹配到文件
func (i *Installer) copyFilesByGlob(sourceDir, targetDir, pattern string, config types.InstallTarget) (bool, error) {
	matches, err := filepath.Glob(filepath.Join(sourceDir, pattern))
	if err != nil {
		return false, err
	}
	
	if len(matches) == 0 {
		fmt.Printf("    ⚠️  没有匹配的文件: %s\n", pattern)
		return false, nil
	}
	
	// 如果可执行文件且匹配多个文件，尝试选择平台特定的文件
	if config.Executable && len(matches) > 1 {
		platformFile := i.selectPlatformFile(matches)
		if platformFile != "" {
			matches = []string{platformFile}
		}
	}
	
	copiedCount := 0
	for _, match := range matches {
		relPath, err := filepath.Rel(sourceDir, match)
		if err != nil {
			return false, err
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
				copiedCount++
				continue
			}
		}
		
		fmt.Printf("  📄 拷贝文件: %s -> %s\n", relPath, targetPath)
		if err := i.copyFile(match, targetPath, config.Executable); err != nil {
			return false, err
		}
		copiedCount++
	}
	
	return copiedCount > 0, nil
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

