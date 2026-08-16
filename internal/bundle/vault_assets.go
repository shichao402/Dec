package bundle

import "strings"

// VaultAssetKind 描述 Git Vault bundles/<name>/ 下的一类资产目录。
//
// 这是「按目录扫资产」的单一真相源：新增资产类型时先改这里，
// 再同步 schema/dec/v1 AssetType 与文档；扫描 / 合成 / 清理应复用本表。
type VaultAssetKind struct {
	// Dir 是 vault 内目录名（复数或固定名），也是 bundle members 的路径前缀。
	Dir string
	// Type 是归一化单数类型（skill / command / rule / mcp）。
	Type string
	// DirEntries 为 true 时每个资产是子目录；为 false 时是带 Suffix 的单文件。
	DirEntries bool
	// Suffix 仅文件型资产使用（如 .mdc / .json）。
	Suffix string
	// Aliases 是额外可接受的类型别名（如 mcps）；Dir 与 Type 始终可互认。
	Aliases []string
}

// VaultAssetKinds 是 vault 支持的全部资产目录清单。
// 顺序稳定，便于枚举与测试断言。
var VaultAssetKinds = []VaultAssetKind{
	{Dir: "skills", Type: "skill", DirEntries: true},
	{Dir: "commands", Type: "command", DirEntries: true},
	{Dir: "rules", Type: "rule", DirEntries: false, Suffix: ".mdc"},
	{Dir: "mcp", Type: "mcp", DirEntries: false, Suffix: ".json", Aliases: []string{"mcps"}},
}

// VaultAssetDirs 返回全部 vault 资产目录名（skills / commands / rules / mcp）。
func VaultAssetDirs() []string {
	dirs := make([]string, len(VaultAssetKinds))
	for i, k := range VaultAssetKinds {
		dirs[i] = k.Dir
	}
	return dirs
}

// TypeSubDir 把归一化类型映射到 vault / cache 子目录名；未知类型返回空串。
func TypeSubDir(itemType string) string {
	if k, ok := kindByType(itemType); ok {
		return k.Dir
	}
	return ""
}

// DirToType 把目录前缀映射到归一化类型；未知目录返回空串。
func DirToType(dir string) string {
	if k, ok := kindByDir(dir); ok {
		return k.Type
	}
	return ""
}

// IsKnownType 判断是否为支持的归一化资产类型。
func IsKnownType(itemType string) bool {
	_, ok := kindByType(itemType)
	return ok
}

// AssetEntryName 从目录条目名得到资产短名（去掉 Suffix）。
func AssetEntryName(k VaultAssetKind, entryName string) string {
	if k.Suffix == "" {
		return entryName
	}
	return strings.TrimSuffix(entryName, k.Suffix)
}

// AssetFileName 把资产短名还原为文件名（目录型资产原样返回）。
func AssetFileName(k VaultAssetKind, assetName string) string {
	if k.DirEntries || k.Suffix == "" {
		return assetName
	}
	return assetName + k.Suffix
}

// KindByType 按归一化类型查找 VaultAssetKind。
func KindByType(itemType string) (VaultAssetKind, bool) {
	return kindByType(itemType)
}

// KindByDir 按目录名前缀查找 VaultAssetKind。
func KindByDir(dir string) (VaultAssetKind, bool) {
	return kindByDir(dir)
}

func kindByType(itemType string) (VaultAssetKind, bool) {
	itemType = strings.TrimSpace(itemType)
	for _, k := range VaultAssetKinds {
		if k.Type == itemType {
			return k, true
		}
	}
	return VaultAssetKind{}, false
}

func kindByDir(dir string) (VaultAssetKind, bool) {
	dir = strings.TrimSpace(dir)
	for _, k := range VaultAssetKinds {
		if k.Dir == dir {
			return k, true
		}
	}
	return VaultAssetKind{}, false
}

func memberTypesFromKinds() []string {
	out := make([]string, len(VaultAssetKinds))
	for i, k := range VaultAssetKinds {
		out[i] = k.Type
	}
	return out
}
