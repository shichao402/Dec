package config

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Fetcher 包获取器
type Fetcher struct {
	config *GlobalConfig
}

// NewFetcher 创建新的包获取器
func NewFetcher() (*Fetcher, error) {
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	return &Fetcher{config: cfg}, nil
}

// GetDownloadURL 获取下载 URL
// 对于 GitHub 仓库，构造 archive 下载链接
func (f *Fetcher) GetDownloadURL() string {
	source := f.config.PackagesSource
	version := f.config.PackagesVersion

	// 处理 GitHub URL
	if strings.Contains(source, "github.com") {
		// 移除 .git 后缀
		source = strings.TrimSuffix(source, ".git")

		// 对于 latest，使用 main 分支
		ref := version
		if version == "latest" {
			ref = "main"
		}

		// GitHub archive URL 格式: https://github.com/owner/repo/archive/refs/heads/main.zip
		// 或者 https://github.com/owner/repo/archive/refs/tags/v1.0.0.zip
		if version == "latest" {
			return fmt.Sprintf("%s/archive/refs/heads/%s.zip", source, ref)
		}
		return fmt.Sprintf("%s/archive/refs/tags/%s.zip", source, ref)
	}

	// 其他 URL 直接返回
	return source
}

// FetchPackages 从远程获取包并解压到缓存目录
func (f *Fetcher) FetchPackages() error {
	// 获取缓存目录
	cacheDir, err := GetPackagesCacheDir()
	if err != nil {
		return fmt.Errorf("获取缓存目录失败: %w", err)
	}

	// 如果缓存目录已存在，先删除
	if _, err := os.Stat(cacheDir); err == nil {
		if err := os.RemoveAll(cacheDir); err != nil {
			return fmt.Errorf("清理旧缓存失败: %w", err)
		}
	}

	// 创建缓存目录
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("创建缓存目录失败: %w", err)
	}

	// 下载 URL
	downloadURL := f.GetDownloadURL()
	fmt.Printf("正在从 %s 下载包...\n", downloadURL)

	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "dec-packages-*.zip")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 下载文件
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	// 写入临时文件
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("保存文件失败: %w", err)
	}

	// 解压文件
	fmt.Println("正在解压包...")
	if err := f.unzip(tmpFile.Name(), cacheDir); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	fmt.Printf("包已更新到: %s\n", cacheDir)
	return nil
}

// unzip 解压 ZIP 文件
func (f *Fetcher) unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	// GitHub archive 会包含一个顶层目录（如 dec-packages-main）
	// 我们需要跳过这个目录，直接解压内容
	var topLevelDir string

	for _, file := range r.File {
		// 找到顶层目录
		parts := strings.Split(file.Name, "/")
		if len(parts) > 0 && topLevelDir == "" {
			topLevelDir = parts[0]
		}

		// 跳过顶层目录本身
		if file.Name == topLevelDir+"/" {
			continue
		}

		// 移除顶层目录前缀
		relPath := strings.TrimPrefix(file.Name, topLevelDir+"/")
		if relPath == "" {
			continue
		}

		fpath := filepath.Join(dest, relPath)

		// 检查路径安全性（防止 zip slip 攻击）
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("非法文件路径: %s", fpath)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, os.ModePerm); err != nil {
				return err
			}
			continue
		}

		// 确保父目录存在
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		// 创建文件
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}

		rc, err := file.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

// GetVersion 获取当前配置的版本
func (f *Fetcher) GetVersion() string {
	return f.config.PackagesVersion
}

// GetSource 获取当前配置的包源
func (f *Fetcher) GetSource() string {
	return f.config.PackagesSource
}
