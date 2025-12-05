package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/firoyang/CursorToolset/pkg/types"
	"github.com/spf13/cobra"
)

var (
	packOutput  string
	packVerify  bool
	packExclude []string
)

var packCmd = &cobra.Command{
	Use:   "pack [package-dir]",
	Short: "标准化打包工具集包",
	Long: `标准化打包工具集包，生成符合规范的 tar.gz 文件并计算 SHA256。

功能：
  - 验证 toolset.json 配置是否符合规范
  - 自动排除不需要的文件（.git、.DS_Store 等）
  - 生成 tar.gz 压缩包
  - 计算并显示 SHA256 校验和
  - 可选：更新 toolset.json 中的 sha256 字段

示例：
  # 打包当前目录
  cursortoolset pack

  # 打包指定目录
  cursortoolset pack ./my-toolset

  # 指定输出文件名
  cursortoolset pack --output my-toolset-1.0.0.tar.gz

  # 打包并自动更新 toolset.json 中的 sha256
  cursortoolset pack --verify`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPack,
}

func init() {
	packCmd.Flags().StringVarP(&packOutput, "output", "o", "", "输出文件名（默认：<name>-<version>.tar.gz）")
	packCmd.Flags().BoolVarP(&packVerify, "verify", "v", false, "验证并更新 toolset.json 中的 sha256")
	packCmd.Flags().StringArrayVarP(&packExclude, "exclude", "e", []string{}, "额外排除的文件或目录")
	RootCmd.AddCommand(packCmd)
}

func runPack(cmd *cobra.Command, args []string) error {
	// 确定要打包的目录
	packageDir := "."
	if len(args) > 0 {
		packageDir = args[0]
	}

	// 转换为绝对路径
	absDir, err := filepath.Abs(packageDir)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}

	fmt.Printf("📦 标准化打包工具集包\n")
	fmt.Printf("   目录: %s\n\n", absDir)

	// 1. 验证 toolset.json
	manifestPath := filepath.Join(absDir, "toolset.json")
	manifest, err := loadAndValidateManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("验证 toolset.json 失败: %w", err)
	}

	fmt.Printf("✅ 验证通过: %s v%s\n\n", manifest.Name, manifest.Version)

	// 2. 确定输出文件名和路径
	outputFile := packOutput
	if outputFile == "" {
		outputFile = fmt.Sprintf("%s-%s.tar.gz", manifest.Name, manifest.Version)
	}

	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	// 如果输出文件是相对路径，将其放在当前工作目录下
	if !filepath.IsAbs(outputFile) {
		outputFile = filepath.Join(cwd, outputFile)
	}

	// 确保输出文件不在要打包的目录内（避免递归打包）
	// 只有当打包目录不是当前目录时才检查
	if absDir != cwd && strings.HasPrefix(outputFile, absDir+string(filepath.Separator)) {
		return fmt.Errorf("输出文件不能在要打包的目录内: %s", outputFile)
	}

	// 3. 收集要打包的文件
	files, err := collectFiles(absDir, manifest)
	if err != nil {
		return fmt.Errorf("收集文件失败: %w", err)
	}

	fmt.Printf("📋 收集到 %d 个文件\n\n", len(files))

	// 4. 创建 tar.gz
	fmt.Printf("🔨 创建压缩包: %s\n", outputFile)
	if err := createTarGz(absDir, outputFile, files); err != nil {
		return fmt.Errorf("创建压缩包失败: %w", err)
	}

	// 5. 计算 SHA256
	fmt.Printf("\n🔐 计算 SHA256...\n")
	sha256sum, err := calculateSHA256(outputFile)
	if err != nil {
		return fmt.Errorf("计算 SHA256 失败: %w", err)
	}

	// 6. 显示结果
	fileInfo, _ := os.Stat(outputFile)
	fmt.Printf("\n✅ 打包完成！\n\n")
	fmt.Printf("📦 文件: %s\n", outputFile)
	fmt.Printf("📏 大小: %s\n", formatSize(fileInfo.Size()))
	fmt.Printf("🔐 SHA256: %s\n", sha256sum)

	// 7. 可选：验证并更新 toolset.json
	if packVerify {
		fmt.Printf("\n🔄 更新 toolset.json 中的 sha256...\n")
		if err := updateManifestSHA256(manifestPath, sha256sum); err != nil {
			fmt.Printf("⚠️  更新失败: %v\n", err)
		} else {
			fmt.Printf("✅ 已更新 toolset.json\n")
		}
	}

	// 8. 显示使用提示
	fmt.Printf("\n💡 下一步：\n")
	fmt.Printf("   1. 在 GitHub 创建 Release (v%s)\n", manifest.Version)
	fmt.Printf("   2. 上传 %s 到 Release\n", outputFile)
	fmt.Printf("   3. 复制 SHA256 到 toolset.json 的 dist.sha256 字段\n")
	if !packVerify {
		fmt.Printf("\n   或使用 --verify 自动更新: cursortoolset pack --verify\n")
	}

	return nil
}

// loadAndValidateManifest 加载并验证 manifest
func loadAndValidateManifest(path string) (*types.Manifest, error) {
	// 读取文件
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("toolset.json 不存在，请先运行 'cursortoolset init'")
		}
		return nil, err
	}

	// 解析 JSON
	var manifest types.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("JSON 格式错误: %w", err)
	}

	// 验证必填字段
	if manifest.Name == "" {
		return nil, fmt.Errorf("name 字段不能为空")
	}
	if manifest.Version == "" {
		return nil, fmt.Errorf("version 字段不能为空")
	}

	// 验证包名格式
	if err := validatePackageName(manifest.Name); err != nil {
		return nil, err
	}

	// 验证版本号格式（简单的语义化版本检查）
	if !isValidVersion(manifest.Version) {
		return nil, fmt.Errorf("version 格式不正确，应为 MAJOR.MINOR.PATCH 格式，例如 1.0.0")
	}

	// 验证 dist 字段
	if manifest.Dist.Tarball == "" {
		fmt.Printf("⚠️  警告: dist.tarball 为空，建议填写下载地址\n")
	}

	return &manifest, nil
}

// isValidVersion 验证版本号格式
func isValidVersion(version string) bool {
	// 简单验证：major.minor.patch
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}

	for _, part := range parts {
		if part == "" {
			return false
		}
		// 检查是否全为数字
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}

	return true
}

// collectFiles 收集要打包的文件
func collectFiles(baseDir string, manifest *types.Manifest) ([]string, error) {
	var files []string

	// 默认排除的文件和目录
	defaultExcludes := []string{
		".git",
		".gitignore",
		".DS_Store",
		"Thumbs.db",
		"node_modules",
		".idea",
		".vscode",
		"*.swp",
		"*.swo",
		"*.log",
		"dist",
		"*.tar.gz",
		"*.zip",
	}

	// 合并用户指定的排除项
	excludes := append(defaultExcludes, packExclude...)

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 获取相对路径
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}

		// 跳过根目录
		if relPath == "." {
			return nil
		}

		// 检查是否应该排除
		if shouldExclude(relPath, info, excludes) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// 只收集文件
		if !info.IsDir() {
			files = append(files, relPath)
		}

		return nil
	})

	return files, err
}

// shouldExclude 判断是否应该排除
func shouldExclude(path string, info os.FileInfo, excludes []string) bool {
	// 规范化路径（使用 / 分隔符）
	path = filepath.ToSlash(path)

	for _, exclude := range excludes {
		exclude = filepath.ToSlash(exclude)

		// 精确匹配
		if path == exclude {
			return true
		}

		// 目录匹配
		if info.IsDir() && strings.HasPrefix(path, exclude+"/") {
			return true
		}

		// 通配符匹配（简单实现）
		if strings.HasPrefix(exclude, "*.") {
			ext := exclude[1:]
			if strings.HasSuffix(path, ext) {
				return true
			}
		}

		// 路径前缀匹配
		if strings.HasPrefix(path, exclude+"/") {
			return true
		}
	}

	return false
}

// createTarGz 创建 tar.gz 文件
func createTarGz(baseDir, outputFile string, files []string) error {
	// 创建输出文件
	outFile, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer outFile.Close()

	// 创建 gzip writer
	gzipWriter := gzip.NewWriter(outFile)
	defer gzipWriter.Close()

	// 创建 tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// 添加文件到 tar
	for _, file := range files {
		if err := addFileToTar(tarWriter, baseDir, file); err != nil {
			return fmt.Errorf("添加文件 %s 失败: %w", file, err)
		}
	}

	return nil
}

// addFileToTar 添加文件到 tar
func addFileToTar(tw *tar.Writer, baseDir, file string) error {
	fullPath := filepath.Join(baseDir, file)

	// 获取文件信息
	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}

	// 创建 tar header
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}

	// 使用相对路径作为 tar 中的文件名（使用 / 分隔符）
	header.Name = filepath.ToSlash(file)

	// 写入 header
	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	// 如果是目录，不需要写入内容
	if info.IsDir() {
		return nil
	}

	// 打开并复制文件内容
	f, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(tw, f); err != nil {
		return err
	}

	return nil
}

// calculateSHA256 计算文件的 SHA256
func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// updateManifestSHA256 更新 manifest 中的 sha256
func updateManifestSHA256(manifestPath, sha256sum string) error {
	// 读取文件
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}

	// 解析为 map（保留原始格式和顺序）
	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}

	// 更新 dist.sha256
	if dist, ok := manifest["dist"].(map[string]interface{}); ok {
		dist["sha256"] = sha256sum
	} else {
		manifest["dist"] = map[string]interface{}{
			"sha256": sha256sum,
		}
	}

	// 重新序列化（保持格式化）
	newData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	// 写回文件
	return os.WriteFile(manifestPath, newData, 0644)
}

// formatSize 格式化文件大小
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}
