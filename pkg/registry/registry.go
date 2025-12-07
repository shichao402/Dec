package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/firoyang/CursorToolset/pkg/config"
	"github.com/firoyang/CursorToolset/pkg/downloader"
	"github.com/firoyang/CursorToolset/pkg/paths"
	"github.com/firoyang/CursorToolset/pkg/types"
)

// Manager 管理包注册表
type Manager struct {
	downloader *downloader.Downloader
	registry   *types.Registry
	manifests  map[string]*types.CachedManifest // 包名 -> manifest 缓存
}

// NewManager 创建新的 Registry 管理器
func NewManager() *Manager {
	return &Manager{
		downloader: downloader.NewDownloader(),
		manifests:  make(map[string]*types.CachedManifest),
	}
}

// Update 更新本地 registry 缓存
func (m *Manager) Update() error {
	fmt.Println("🔄 更新包索引...")

	// 下载最新的 registry
	registryPath, err := paths.GetRegistryPath()
	if err != nil {
		return fmt.Errorf("获取 registry 路径失败: %w", err)
	}

	// 添加时间戳绕过 CDN 缓存
	registryURL := fmt.Sprintf("%s?t=%d", config.GetRegistryURL(), time.Now().Unix())
	m.downloader.SetShowProgress(true)
	if err := m.downloader.DownloadFile(registryURL, registryPath); err != nil {
		return fmt.Errorf("下载 registry 失败: %w", err)
	}

	// 加载 registry
	if err := m.loadRegistry(); err != nil {
		return fmt.Errorf("加载 registry 失败: %w", err)
	}

	// 从 registry 构建 manifest 缓存（新格式已包含完整信息）
	m.buildManifestsFromRegistry()

	fmt.Printf("✅ 包索引更新完成，共 %d 个包\n", len(m.registry.Packages))
	return nil
}

// Load 加载本地缓存的 registry 和 manifests
func (m *Manager) Load() error {
	if err := m.loadRegistry(); err != nil {
		return err
	}
	// 从 registry 构建 manifest 缓存
	m.buildManifestsFromRegistry()
	return nil
}

// loadRegistry 加载本地 registry
func (m *Manager) loadRegistry() error {
	registryPath, err := paths.GetRegistryPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 首次使用，返回空 registry
			m.registry = &types.Registry{
				Version:  "4",
				Packages: []types.RegistryItem{},
			}
			return nil
		}
		return err
	}

	var registry types.Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return fmt.Errorf("解析 registry 失败: %w", err)
	}

	m.registry = &registry
	return nil
}

// buildManifestsFromRegistry 从 registry 构建 manifest 缓存
// 新格式的 registry 已包含完整的包信息，无需额外下载
func (m *Manager) buildManifestsFromRegistry() {
	if m.registry == nil {
		return
	}

	for _, item := range m.registry.Packages {
		// 跳过没有完整信息的条目（旧格式兼容）
		if item.Name == "" {
			continue
		}

		// 处理相对路径的 tarball URL
		tarball := item.Dist.Tarball
		if tarball != "" && !strings.HasPrefix(tarball, "http") {
			tarball = m.resolveTarballURL(item, tarball, item.Version)
		}

		manifest := types.Manifest{
			Name:        item.Name,
			Version:     item.Version,
			Description: item.Description,
			Author:      item.Author,
			Repository: types.Repository{
				Type: "git",
				URL:  item.Repository,
			},
			Dist: types.Distribution{
				Tarball: tarball,
				SHA256:  item.Dist.SHA256,
				Size:    item.Dist.Size,
			},
		}

		m.manifests[item.Name] = &types.CachedManifest{
			Manifest: manifest,
			CachedAt: m.registry.UpdatedAt,
		}
	}
}

// loadManifests 加载所有缓存的 manifest（保留用于兼容旧数据）
func (m *Manager) loadManifests() error {
	if m.registry == nil {
		return nil
	}

	for _, item := range m.registry.Packages {
		repoName := item.GetRepoName()
		manifestPath, err := paths.GetManifestPath(repoName)
		if err != nil {
			continue
		}

		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		var cached types.CachedManifest
		if err := json.Unmarshal(data, &cached); err != nil {
			continue
		}

		// 使用实际包名作为 key
		m.manifests[cached.Name] = &cached
	}

	return nil
}

// resolveTarballURL 解析 tarball 的完整 URL
// 如果 tarball 是相对路径，根据 repository 组装完整 URL
func (m *Manager) resolveTarballURL(item types.RegistryItem, tarball string, version string) string {
	if item.Repository == "" {
		return tarball
	}
	// https://github.com/user/repo/releases/download/v1.0.0/package-1.0.0.tar.gz
	repoURL := strings.TrimSuffix(item.Repository, "/")
	repoURL = strings.TrimSuffix(repoURL, ".git")
	return fmt.Sprintf("%s/releases/download/v%s/%s", repoURL, version, tarball)
}

// GetRegistry 获取 registry
func (m *Manager) GetRegistry() *types.Registry {
	return m.registry
}

// GetManifest 获取指定包的 manifest
func (m *Manager) GetManifest(packageName string) *types.Manifest {
	if cached, ok := m.manifests[packageName]; ok {
		return &cached.Manifest
	}
	return nil
}

// GetManifestByRepo 根据仓库名查找 manifest
func (m *Manager) GetManifestByRepo(repoName string) *types.Manifest {
	// 遍历所有 manifest，找到匹配的
	for _, cached := range m.manifests {
		// 检查是否是通过这个仓库安装的
		if cached.Repository.URL != "" && strings.Contains(cached.Repository.URL, repoName) {
			return &cached.Manifest
		}
	}
	return nil
}

// GetAllManifests 获取所有缓存的 manifest
func (m *Manager) GetAllManifests() []*types.Manifest {
	var result []*types.Manifest
	for _, cached := range m.manifests {
		result = append(result, &cached.Manifest)
	}
	return result
}

// ListPackages 列出所有可用包
func (m *Manager) ListPackages() []types.RegistryItem {
	if m.registry == nil {
		return nil
	}
	return m.registry.Packages
}

// FindPackage 根据名称查找包
func (m *Manager) FindPackage(name string) *types.Manifest {
	return m.GetManifest(name)
}

// SearchPackages 搜索包
func (m *Manager) SearchPackages(keyword string) []*types.Manifest {
	keyword = strings.ToLower(keyword)
	var results []*types.Manifest

	for _, manifest := range m.manifests {
		if m.matchKeyword(&manifest.Manifest, keyword) {
			results = append(results, &manifest.Manifest)
		}
	}

	return results
}

// matchKeyword 检查 manifest 是否匹配关键词
func (m *Manager) matchKeyword(manifest *types.Manifest, keyword string) bool {
	// 搜索名称
	if strings.Contains(strings.ToLower(manifest.Name), keyword) {
		return true
	}

	// 搜索显示名称
	if strings.Contains(strings.ToLower(manifest.DisplayName), keyword) {
		return true
	}

	// 搜索描述
	if strings.Contains(strings.ToLower(manifest.Description), keyword) {
		return true
	}

	// 搜索关键词
	for _, kw := range manifest.Keywords {
		if strings.Contains(strings.ToLower(kw), keyword) {
			return true
		}
	}

	return false
}

// HasLocalCache 检查是否有本地缓存
func (m *Manager) HasLocalCache() bool {
	registryPath, err := paths.GetRegistryPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(registryPath)
	return err == nil
}

// ========================================
// 发布相关功能（用于管理器维护者）
// ========================================

// AddPackage 添加包到 registry（用于发布）
// 只需要 repository，包名从 manifest 获取
func (m *Manager) AddPackage(repository string) error {
	if m.registry == nil {
		m.registry = &types.Registry{
			Version:  "3",
			Packages: []types.RegistryItem{},
		}
	}

	// 规范化 repository URL
	repository = strings.TrimSuffix(repository, "/")
	repository = strings.TrimSuffix(repository, ".git")

	// 检查是否已存在
	for _, item := range m.registry.Packages {
		existingRepo := strings.TrimSuffix(item.Repository, "/")
		existingRepo = strings.TrimSuffix(existingRepo, ".git")
		if existingRepo == repository {
			return fmt.Errorf("仓库已存在: %s", repository)
		}
	}

	// 添加新包
	m.registry.Packages = append(m.registry.Packages, types.RegistryItem{
		Repository: repository,
	})

	return m.saveRegistry()
}

// RemovePackage 从 registry 移除包（通过仓库地址或包名）
func (m *Manager) RemovePackage(identifier string) error {
	if m.registry == nil {
		return nil
	}

	for i, item := range m.registry.Packages {
		repoName := item.GetRepoName()
		// 匹配仓库名或完整 URL
		if repoName == identifier || item.Repository == identifier || strings.Contains(item.Repository, identifier) {
			m.registry.Packages = append(m.registry.Packages[:i], m.registry.Packages[i+1:]...)
			return m.saveRegistry()
		}
	}

	// 也尝试通过包名匹配
	if manifest := m.FindPackage(identifier); manifest != nil {
		// 找到对应的 registry item
		for i, item := range m.registry.Packages {
			if m.GetManifestByRepo(item.GetRepoName()) == manifest {
				m.registry.Packages = append(m.registry.Packages[:i], m.registry.Packages[i+1:]...)
				return m.saveRegistry()
			}
		}
	}

	return fmt.Errorf("未找到: %s", identifier)
}

// saveRegistry 保存 registry 到本地
func (m *Manager) saveRegistry() error {
	registryPath, err := paths.GetRegistryPath()
	if err != nil {
		return err
	}

	configDir, err := paths.GetConfigDir()
	if err != nil {
		return err
	}
	if err := paths.EnsureDir(configDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(m.registry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(registryPath, data, 0644)
}

// ExportRegistry 导出 registry 为 JSON
func (m *Manager) ExportRegistry() ([]byte, error) {
	if m.registry == nil {
		return nil, fmt.Errorf("registry 未加载")
	}
	return json.MarshalIndent(m.registry, "", "  ")
}
