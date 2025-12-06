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

	m.downloader.SetShowProgress(true)
	if err := m.downloader.DownloadFile(config.GetRegistryURL(), registryPath); err != nil {
		return fmt.Errorf("下载 registry 失败: %w", err)
	}

	// 加载 registry
	if err := m.loadRegistry(); err != nil {
		return fmt.Errorf("加载 registry 失败: %w", err)
	}

	// 更新所有包的 manifest 缓存
	fmt.Println("🔄 更新包信息...")
	for _, item := range m.registry.Packages {
		if err := m.updateManifest(item); err != nil {
			fmt.Printf("  ⚠️  更新 %s 失败: %v\n", item.Name, err)
			continue
		}
		fmt.Printf("  ✅ %s\n", item.Name)
	}

	fmt.Println("✅ 包索引更新完成")
	return nil
}

// Load 加载本地缓存的 registry 和 manifests
func (m *Manager) Load() error {
	if err := m.loadRegistry(); err != nil {
		return err
	}
	return m.loadManifests()
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
				Version:  "1",
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

// loadManifests 加载所有缓存的 manifest
func (m *Manager) loadManifests() error {
	if m.registry == nil {
		return nil
	}

	for _, item := range m.registry.Packages {
		manifestPath, err := paths.GetManifestPath(item.Name)
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

		m.manifests[item.Name] = &cached
	}

	return nil
}

// updateManifest 更新单个包的 manifest 缓存
func (m *Manager) updateManifest(item types.RegistryItem) error {
	manifestPath, err := paths.GetManifestPath(item.Name)
	if err != nil {
		return err
	}

	// 下载 manifest
	m.downloader.SetShowProgress(false)
	if err := m.downloader.DownloadFile(item.ManifestURL, manifestPath+".tmp"); err != nil {
		return err
	}

	// 解析 manifest
	data, err := os.ReadFile(manifestPath + ".tmp")
	if err != nil {
		return err
	}

	var manifest types.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		_ = os.Remove(manifestPath + ".tmp")
		return fmt.Errorf("解析 manifest 失败: %w", err)
	}

	// 创建带缓存时间的 manifest
	cached := types.CachedManifest{
		Manifest: manifest,
		CachedAt: time.Now().Format(time.RFC3339),
	}

	// 保存缓存
	cachedData, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		_ = os.Remove(manifestPath + ".tmp")
		return err
	}

	if err := os.WriteFile(manifestPath, cachedData, 0644); err != nil {
		_ = os.Remove(manifestPath + ".tmp")
		return err
	}

	_ = os.Remove(manifestPath + ".tmp")
	m.manifests[item.Name] = &cached
	return nil
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
func (m *Manager) AddPackage(name, manifestURL string) error {
	if m.registry == nil {
		m.registry = &types.Registry{
			Version:  "1",
			Packages: []types.RegistryItem{},
		}
	}

	// 检查是否已存在
	for i, item := range m.registry.Packages {
		if item.Name == name {
			// 更新
			m.registry.Packages[i].ManifestURL = manifestURL
			return m.saveRegistry()
		}
	}

	// 添加新包
	m.registry.Packages = append(m.registry.Packages, types.RegistryItem{
		Name:        name,
		ManifestURL: manifestURL,
	})

	return m.saveRegistry()
}

// RemovePackage 从 registry 移除包
func (m *Manager) RemovePackage(name string) error {
	if m.registry == nil {
		return nil
	}

	for i, item := range m.registry.Packages {
		if item.Name == name {
			m.registry.Packages = append(m.registry.Packages[:i], m.registry.Packages[i+1:]...)
			return m.saveRegistry()
		}
	}

	return nil
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
