package downloader

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/paths"
)

// Downloader 负责下载和解压包
type Downloader struct {
	client    *http.Client
	useCache  bool
	showProgress bool
}

// NewDownloader 创建新的下载器
func NewDownloader() *Downloader {
	return &Downloader{
		client:    &http.Client{},
		useCache:  true,
		showProgress: true,
	}
}

// SetUseCache 设置是否使用缓存
func (d *Downloader) SetUseCache(use bool) {
	d.useCache = use
}

// SetShowProgress 设置是否显示进度
func (d *Downloader) SetShowProgress(show bool) {
	d.showProgress = show
}

// DownloadResult 下载结果
type DownloadResult struct {
	FilePath   string // 下载文件的本地路径
	FromCache  bool   // 是否来自缓存
	Size       int64  // 文件大小
}

// Download 下载文件到缓存目录
// 如果缓存存在且 SHA256 匹配，直接返回缓存路径
func (d *Downloader) Download(url, packageName, version, expectedSHA256 string) (*DownloadResult, error) {
	// 获取缓存路径
	cachePath, err := paths.GetPackageCachePath(packageName, version)
	if err != nil {
		return nil, fmt.Errorf("获取缓存路径失败: %w", err)
	}

	// 检查缓存
	if d.useCache {
		if info, err := os.Stat(cachePath); err == nil {
			// 验证缓存文件的 SHA256
			if expectedSHA256 != "" {
				actualSHA256, err := d.calculateFileSHA256(cachePath)
				if err == nil && actualSHA256 == strings.ToLower(expectedSHA256) {
					if d.showProgress {
						fmt.Printf("  📦 使用缓存: %s\n", filepath.Base(cachePath))
					}
					return &DownloadResult{
						FilePath:  cachePath,
						FromCache: true,
						Size:      info.Size(),
					}, nil
				}
				// SHA256 不匹配，删除缓存重新下载
				_ = os.Remove(cachePath)
			} else {
				// 没有提供 SHA256，直接使用缓存
				return &DownloadResult{
					FilePath:  cachePath,
					FromCache: true,
					Size:      info.Size(),
				}, nil
			}
		}
	}

	// 确保缓存目录存在
	if err := paths.EnsureDir(filepath.Dir(cachePath)); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %w", err)
	}

	// 下载文件
	if d.showProgress {
		fmt.Printf("  📥 下载: %s\n", url)
	}

	resp, err := d.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	// 创建临时文件
	tempFile, err := os.CreateTemp(filepath.Dir(cachePath), "download-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath) // 清理临时文件
	}()

	// 同时计算 SHA256 和写入文件
	hasher := sha256.New()
	writer := io.MultiWriter(tempFile, hasher)

	size, err := io.Copy(writer, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}
	_ = tempFile.Close()

	// 验证 SHA256
	actualSHA256 := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA256 != "" && actualSHA256 != strings.ToLower(expectedSHA256) {
		return nil, fmt.Errorf("SHA256 校验失败\n  期望: %s\n  实际: %s", expectedSHA256, actualSHA256)
	}

	// 移动到最终位置
	if err := os.Rename(tempPath, cachePath); err != nil {
		// 如果 rename 失败（跨文件系统），尝试复制
		if err := d.copyFile(tempPath, cachePath); err != nil {
			return nil, fmt.Errorf("保存文件失败: %w", err)
		}
	}

	if d.showProgress {
		fmt.Printf("  ✅ 下载完成 (%.2f MB)\n", float64(size)/1024/1024)
	}

	return &DownloadResult{
		FilePath:  cachePath,
		FromCache: false,
		Size:      size,
	}, nil
}

// Extract 解压 tar.gz 文件到指定目录
func (d *Downloader) Extract(tarballPath, destDir string) error {
	if d.showProgress {
		fmt.Printf("  📂 解压到: %s\n", destDir)
	}

	// 打开 tarball
	file, err := os.Open(tarballPath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer func() { _ = file.Close() }()

	// 创建 gzip reader
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("创建 gzip reader 失败: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	// 创建 tar reader
	tarReader := tar.NewReader(gzReader)

	// 确保目标目录存在
	if err := paths.EnsureDir(destDir); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}

	// 解压文件
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 tar 失败: %w", err)
		}

		// 安全检查：防止路径遍历攻击
		targetPath := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)) {
			return fmt.Errorf("非法路径: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}

		case tar.TypeReg:
			// 确保父目录存在
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("创建父目录失败: %w", err)
			}

			// 创建文件
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("创建文件失败: %w", err)
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				_ = outFile.Close()
				return fmt.Errorf("写入文件失败: %w", err)
			}
			_ = outFile.Close()

		case tar.TypeSymlink:
			// 创建符号链接
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				// 忽略符号链接错误（Windows 可能不支持）
				if d.showProgress {
					fmt.Printf("  ⚠️  跳过符号链接: %s\n", header.Name)
				}
			}
		}
	}

	if d.showProgress {
		fmt.Printf("  ✅ 解压完成\n")
	}

	return nil
}

// DownloadAndExtract 下载并解压包
func (d *Downloader) DownloadAndExtract(url, packageName, version, expectedSHA256, destDir string) error {
	// 下载
	result, err := d.Download(url, packageName, version, expectedSHA256)
	if err != nil {
		return err
	}

	// 解压
	return d.Extract(result.FilePath, destDir)
}

// DownloadFile 下载文件到指定路径（用于下载 registry 等小文件）
func (d *Downloader) DownloadFile(url, destPath string) error {
	if d.showProgress {
		fmt.Printf("  📥 下载: %s\n", url)
	}

	resp, err := d.client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	// 确保目录存在
	if err := paths.EnsureDir(filepath.Dir(destPath)); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 创建文件
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	return nil
}

// calculateFileSHA256 计算文件的 SHA256
func (d *Downloader) calculateFileSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// copyFile 复制文件
func (d *Downloader) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
