package registry

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/bundle"
	"github.com/shichao402/Dec/internal/types"
)

// SnapshotAsset 是注册表当前快照里的一条 Git 资产，不含文件正文。
type SnapshotAsset struct {
	Visibility string
	Plane      string
	Type       string
	Name       string
}

// ProjectSnapshot 是 registry 分支上一个产品目录的身份和资产名单。
type ProjectSnapshot struct {
	OriginRepo string
	Tags       []string
	// SecretsPlane 是提供方声明的密钥平面（ADR 0035）；空 = 未声明。
	SecretsPlane string
	Assets       []SnapshotAsset
}

// ReadProjects 读取 registry 工作树里每个产品的 provider.yaml 与资产名单。
func ReadProjects(root string) (map[string]ProjectSnapshot, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ProjectSnapshot)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if !types.IsValidProjectName(name) {
			continue
		}
		projectDir := filepath.Join(root, name)
		meta, err := LoadProviderMeta(projectDir)
		if err != nil {
			return nil, err
		}
		assets, err := readProjectAssets(projectDir)
		if err != nil {
			return nil, err
		}
		out[name] = ProjectSnapshot{
			OriginRepo:   meta.OriginRepo,
			Tags:         append([]string(nil), meta.Tags...),
			SecretsPlane: strings.TrimSpace(meta.SecretsPlane),
			Assets:       assets,
		}
	}
	return out, nil
}

func readProjectAssets(projectDir string) ([]SnapshotAsset, error) {
	var assets []SnapshotAsset
	seen := map[string]struct{}{}
	err := filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == projectDir {
			return nil
		}
		rel, err := filepath.Rel(projectDir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 4 {
			return nil
		}
		kind, ok := bundle.KindByDir(parts[2])
		if !ok {
			return nil
		}
		visibility := types.AssetVisibility(parts[0])
		plane := types.AssetPlane(parts[1])
		if visibility != types.AssetVisibilityPublic && visibility != types.AssetVisibilityPrivate {
			return nil
		}
		if plane != types.AssetPlaneGlobal && plane != types.AssetPlaneLocal {
			return nil
		}
		entryName := parts[3]
		var assetName string
		if kind.DirEntries {
			if !d.IsDir() || len(parts) != 4 {
				return nil
			}
			assetName = entryName
		} else {
			if d.IsDir() || len(parts) != 4 {
				return nil
			}
			if kind.Suffix != "" && !strings.HasSuffix(entryName, kind.Suffix) {
				return nil
			}
			assetName = bundle.AssetEntryName(kind, entryName)
		}
		if assetName == "" || assetName == "." || assetName == ".." {
			return nil
		}
		key := string(visibility) + "/" + string(plane) + "/" + kind.Type + "/" + assetName
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}
		assets = append(assets, SnapshotAsset{
			Visibility: string(visibility),
			Plane:      string(plane),
			Type:       kind.Type,
			Name:       assetName,
		})
		if kind.DirEntries && d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(assets, func(i, j int) bool {
		if assets[i].Type != assets[j].Type {
			return assets[i].Type < assets[j].Type
		}
		return assets[i].Name < assets[j].Name
	})
	return assets, nil
}
