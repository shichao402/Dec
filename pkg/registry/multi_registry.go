package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/downloader"
	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/types"
)

// MultiRegistryManager 多注册表管理器
// 支持 local（本地开发）、test（测试）、official（正式）三个注册表
// 优先级：local > test > official
type MultiRegistryManager struct {
	downloader *downloader.Downloader
	registries map[types.RegistryType]*types.PackRegistry
}

// NewMultiRegistryManager 创建多注册表管理器
func NewMultiRegistryManager() *MultiRegistryManager {
	return &MultiRegistryManager{
		downloader: downloader.NewDownloader(),
		registries: make(map[types.RegistryType]*types.PackRegistry),
	}
}

// ========================================
// 加载注册表
// ========================================

// Load 加载所有注册表
func (m *MultiRegistryManager) Load() error {
	// 加载本地开发注册表
	if err := m.loadRegistry(types.RegistryTypeLocal); err != nil {
		// 本地注册表不存在是正常的
		if !os.IsNotExist(err) {
			return fmt.Errorf("加载本地注册表失败: %w", err)
		}
	}

	// 加载测试注册表
	if err := m.loadRegistry(types.RegistryTypeTest); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("加载测试注册表失败: %w", err)
		}
	}

	// 加载正式注册表
	if err := m.loadRegistry(types.RegistryTypeOfficial); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("加载正式注册表失败: %w", err)
		}
	}

	return nil
}

// loadRegistry 加载指定类型的注册表
func (m *MultiRegistryManager) loadRegistry(regType types.RegistryType) error {
	path, err := m.getRegistryPath(regType)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var registry types.PackRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return fmt.Errorf("解析注册表失败: %w", err)
	}

	m.registries[regType] = &registry
	return nil
}

// getRegistryPath 获取注册表文件路径
func (m *MultiRegistryManager) getRegistryPath(regType types.RegistryType) (string, error) {
	switch regType {
	case types.RegistryTypeLocal:
		return paths.GetLocalRegistryPath()
	case types.RegistryTypeTest:
		return paths.GetTestRegistryPath()
	case types.RegistryTypeOfficial:
		return paths.GetOfficialRegistryPath()
	default:
		return "", fmt.Errorf("未知的注册表类型: %s", regType)
	}
}

// ========================================
// 更新注册表
// ========================================

// UpdateOfficial 更新正式注册表
func (m *MultiRegistryManager) UpdateOfficial() error {
	fmt.Println("🔄 更新包索引...")

	registryPath, err := paths.GetOfficialRegistryPath()
	if err != nil {
		return fmt.Errorf("获取注册表路径失败: %w", err)
	}

	// 确保目录存在
	registryDir, err := paths.GetRegistryDir()
	if err != nil {
		return err
	}
	if err := paths.EnsureDir(registryDir); err != nil {
		return err
	}

	// 添加时间戳绕过 CDN 缓存
	registryURL := fmt.Sprintf("%s?t=%d", config.GetRegistryURL(), time.Now().Unix())
	m.downloader.SetShowProgress(true)
	if err := m.downloader.DownloadFile(registryURL, registryPath); err != nil {
		return fmt.Errorf("下载注册表失败: %w", err)
	}

	// 重新加载
	if err := m.loadRegistry(types.RegistryTypeOfficial); err != nil {
		return fmt.Errorf("加载注册表失败: %w", err)
	}

	count := 0
	if reg := m.registries[types.RegistryTypeOfficial]; reg != nil {
		count = len(reg.Packs)
	}
	fmt.Printf("✅ 包索引更新完成，共 %d 个包\n", count)
	return nil
}

// ========================================
// 查询包
// ========================================

// ResolvePack 解析包，按优先级查找
// 优先级：local > test > official
func (m *MultiRegistryManager) ResolvePack(name string) *types.ResolvedPack {
	// 1. 先查本地开发注册表
	if pack := m.findInRegistry(types.RegistryTypeLocal, name); pack != nil {
		return pack
	}

	// 2. 再查测试注册表
	if pack := m.findInRegistry(types.RegistryTypeTest, name); pack != nil {
		return pack
	}

	// 3. 最后查正式注册表
	if pack := m.findInRegistry(types.RegistryTypeOfficial, name); pack != nil {
		return pack
	}

	return nil
}

// findInRegistry 在指定注册表中查找包
func (m *MultiRegistryManager) findInRegistry(regType types.RegistryType, name string) *types.ResolvedPack {
	registry, ok := m.registries[regType]
	if !ok || registry == nil {
		return nil
	}

	if meta, exists := registry.Packs[name]; exists {
		resolved := &types.ResolvedPack{
			PackMetadata: meta,
			Source:       regType,
		}

		// 设置安装路径
		if meta.Type == types.PackTypeMCP {
			if path, err := paths.GetMCPPackPath(name); err == nil {
				resolved.InstallPath = path
				if _, err := os.Stat(path); err == nil {
					resolved.IsInstalled = true
				}
			}
		} else {
			if path, err := paths.GetRulePackPath(name); err == nil {
				resolved.InstallPath = path
				if _, err := os.Stat(path); err == nil {
					resolved.IsInstalled = true
				}
			}
		}

		return resolved
	}

	return nil
}

// ListAllPacks 列出所有包（去重，按优先级）
func (m *MultiRegistryManager) ListAllPacks() []*types.ResolvedPack {
	seen := make(map[string]bool)
	var result []*types.ResolvedPack

	// 按优先级顺序遍历
	for _, regType := range []types.RegistryType{
		types.RegistryTypeLocal,
		types.RegistryTypeTest,
		types.RegistryTypeOfficial,
	} {
		registry, ok := m.registries[regType]
		if !ok || registry == nil {
			continue
		}

		for name, meta := range registry.Packs {
			if seen[name] {
				continue
			}
			seen[name] = true

			resolved := &types.ResolvedPack{
				PackMetadata: meta,
				Source:       regType,
			}

			// 设置安装路径和状态
			if meta.Type == types.PackTypeMCP {
				if path, err := paths.GetMCPPackPath(name); err == nil {
					resolved.InstallPath = path
					if _, err := os.Stat(path); err == nil {
						resolved.IsInstalled = true
					}
				}
			} else {
				if path, err := paths.GetRulePackPath(name); err == nil {
					resolved.InstallPath = path
					if _, err := os.Stat(path); err == nil {
						resolved.IsInstalled = true
					}
				}
			}

			result = append(result, resolved)
		}
	}

	return result
}

// SearchPacks 搜索包
func (m *MultiRegistryManager) SearchPacks(keyword string) []*types.ResolvedPack {
	keyword = strings.ToLower(keyword)
	var results []*types.ResolvedPack

	for _, pack := range m.ListAllPacks() {
		if m.matchKeyword(pack, keyword) {
			results = append(results, pack)
		}
	}

	return results
}

// matchKeyword 检查包是否匹配关键词
func (m *MultiRegistryManager) matchKeyword(pack *types.ResolvedPack, keyword string) bool {
	if strings.Contains(strings.ToLower(pack.Name), keyword) {
		return true
	}
	if strings.Contains(strings.ToLower(pack.Description), keyword) {
		return true
	}
	return false
}

// ========================================
// 本地开发注册表管理（dec link / unlink）
// ========================================

// LinkPack 链接本地开发包
func (m *MultiRegistryManager) LinkPack(name string, localPath string, version string, packType string) error {
	// 确保本地注册表存在
	if m.registries[types.RegistryTypeLocal] == nil {
		m.registries[types.RegistryTypeLocal] = &types.PackRegistry{
			Version: "1",
			Packs:   make(map[string]types.PackMetadata),
		}
	}

	registry := m.registries[types.RegistryTypeLocal]
	registry.Packs[name] = types.PackMetadata{
		Name:      name,
		Type:      packType,
		Version:   version,
		LocalPath: localPath,
		LinkedAt:  time.Now().Format(time.RFC3339),
	}
	registry.UpdatedAt = time.Now().Format(time.RFC3339)

	return m.saveRegistry(types.RegistryTypeLocal)
}

// UnlinkPack 移除本地链接
func (m *MultiRegistryManager) UnlinkPack(name string) error {
	registry := m.registries[types.RegistryTypeLocal]
	if registry == nil {
		return fmt.Errorf("包 %s 未链接", name)
	}

	if _, exists := registry.Packs[name]; !exists {
		return fmt.Errorf("包 %s 未链接", name)
	}

	delete(registry.Packs, name)
	registry.UpdatedAt = time.Now().Format(time.RFC3339)

	return m.saveRegistry(types.RegistryTypeLocal)
}

// UnlinkAll 移除所有本地链接
func (m *MultiRegistryManager) UnlinkAll() error {
	registry := m.registries[types.RegistryTypeLocal]
	if registry == nil {
		return nil
	}

	registry.Packs = make(map[string]types.PackMetadata)
	registry.UpdatedAt = time.Now().Format(time.RFC3339)

	return m.saveRegistry(types.RegistryTypeLocal)
}

// ListLinkedPacks 列出所有本地链接的包
func (m *MultiRegistryManager) ListLinkedPacks() []types.PackMetadata {
	registry := m.registries[types.RegistryTypeLocal]
	if registry == nil {
		return nil
	}

	var result []types.PackMetadata
	for _, meta := range registry.Packs {
		result = append(result, meta)
	}
	return result
}

// ========================================
// 保存注册表
// ========================================

// saveRegistry 保存指定类型的注册表
func (m *MultiRegistryManager) saveRegistry(regType types.RegistryType) error {
	registry := m.registries[regType]
	if registry == nil {
		return nil
	}

	path, err := m.getRegistryPath(regType)
	if err != nil {
		return err
	}

	// 确保目录存在
	registryDir, err := paths.GetRegistryDir()
	if err != nil {
		return err
	}
	if err := paths.EnsureDir(registryDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// ========================================
// 辅助方法
// ========================================

// HasLocalCache 检查是否有本地缓存
func (m *MultiRegistryManager) HasLocalCache() bool {
	path, err := paths.GetOfficialRegistryPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// GetRegistry 获取指定类型的注册表
func (m *MultiRegistryManager) GetRegistry(regType types.RegistryType) *types.PackRegistry {
	return m.registries[regType]
}
