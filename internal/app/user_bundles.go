package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
	"gopkg.in/yaml.v3"
)

// userEnableRejection 记录一个未能提升为 user scope 的 bundle 及原因。
type userEnableRejection struct {
	Name   string
	Reason string
}

// userEnableRepair 汇总一次用户平面启用修复的结果。
type userEnableRepair struct {
	// Created 是本次新建的占位 bundle 名。
	Created []string
	// Upgraded 是缺省 scope 被推断写回为 user 的 bundle 名。
	Upgraded []string
	// Rejected 是不能在用户平面启用的名字；调用方必须把它们排除在 enabled_bundles 之外。
	Rejected []userEnableRejection
}

func (r *userEnableRepair) rejectedNames() []string {
	names := make([]string, 0, len(r.Rejected))
	for _, rj := range r.Rejected {
		names = append(names, rj.Name)
	}
	return names
}

// ensureVaultBundlesForUserEnable 是用户平面启用路径上的 RepairOn*：
// 缺包时创建 scope: user 占位；缺省 scope 且无 project 引用时按 ADR 0009 迁移期推断写回 user。
//
// 显式 scope: project 的包一律不改写（ADR 0013）：那是 manifest 作者的明确声明，
// 静默改写会让所有引用它的 project 因平面隔离而突然拉不到资产。缺省 scope 若被任何
// projects/*.yaml 引用，同样按「有 project 在用」处理，不推断为 user。
func ensureVaultBundlesForUserEnable(names []string, reporter Reporter) (*userEnableRepair, error) {
	reporter = defaultReporter(reporter)
	names = secrets.NormalizeBundleNames(names)
	if len(names) == 0 {
		return &userEnableRepair{}, nil
	}

	connected, err := repo.IsConnected()
	if err != nil {
		return nil, fmt.Errorf("检查仓库连接失败: %w", err)
	}
	if !connected {
		return nil, fmt.Errorf("仓库未连接，无法创建个人 bundle")
	}

	tx, err := repo.NewWriteTransaction()
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	projectRefs, err := listVaultProjectBundleRefs(tx.WorkDir())
	if err != nil {
		return nil, err
	}

	repair := &userEnableRepair{}
	created := make([]string, 0)
	updated := make([]string, 0)
	for _, name := range names {
		manifestRel := types.VaultBundleManifestPath(name)
		manifestAbs := filepath.Join(tx.WorkDir(), filepath.FromSlash(manifestRel))
		if data, readErr := os.ReadFile(manifestAbs); readErr == nil {
			b, explicitScope, parseErr := yamlBundleNameScope(data)
			switch {
			case parseErr != nil:
				repair.Rejected = append(repair.Rejected, userEnableRejection{
					Name: name, Reason: "manifest 无法解析: " + parseErr.Error(),
				})
			case b.Scope == types.BundleScopeUser:
				// 已是用户平面，无需修复。
			case explicitScope:
				repair.Rejected = append(repair.Rejected, userEnableRejection{
					Name: name, Reason: "manifest 显式声明 scope: project",
				})
			case len(projectRefs[name]) > 0:
				repair.Rejected = append(repair.Rejected, userEnableRejection{
					Name:   name,
					Reason: "被 project 声明引用: " + strings.Join(projectRefs[name], ", "),
				})
			default:
				b.Scope = types.BundleScopeUser
				if writeErr := writeBundleManifest(manifestAbs, b); writeErr != nil {
					return nil, writeErr
				}
				updated = append(updated, name)
				emit(reporter, EventInfo, "settings.vault",
					fmt.Sprintf("仓库 bundle %q 缺 scope 且无 project 引用，已按用户平面补写 scope: user", name), nil)
			}
			continue
		} else if !os.IsNotExist(readErr) {
			return nil, fmt.Errorf("检查 vault bundle %q 失败: %w", name, readErr)
		}
		if err := os.MkdirAll(filepath.Dir(manifestAbs), 0755); err != nil {
			return nil, err
		}
		body, err := yaml.Marshal(types.Bundle{
			Name:        name,
			Scope:       types.BundleScopeUser,
			Description: "user-plane placeholder (ADR 0009)",
			Members:     []string{},
		})
		if err != nil {
			return nil, err
		}
		header := "# Dec bundle（用户平面启用时自动创建的占位；可后续补充 members）\n"
		if err := os.WriteFile(manifestAbs, append([]byte(header), body...), 0644); err != nil {
			return nil, fmt.Errorf("写入 %s 失败: %w", manifestRel, err)
		}
		created = append(created, name)
		emit(reporter, EventInfo, "settings.vault", fmt.Sprintf("已在仓库创建占位 bundle %q", name), nil)
	}

	repair.Created = created
	repair.Upgraded = updated
	for _, rj := range repair.Rejected {
		emit(reporter, EventWarn, "settings.vault",
			fmt.Sprintf("bundle %q 不能在用户平面启用：%s", rj.Name, rj.Reason), nil)
	}

	if len(created) == 0 && len(updated) == 0 {
		return repair, nil
	}

	parts := make([]string, 0, 2)
	if len(created) > 0 {
		parts = append(parts, "add placeholders: "+strings.Join(created, ", "))
	}
	if len(updated) > 0 {
		parts = append(parts, "mark scope=user: "+strings.Join(updated, ", "))
	}
	msg := "chore(bundles): user-plane repair — " + strings.Join(parts, "; ")
	if _, err := tx.CommitAndPush(msg); err != nil {
		return nil, fmt.Errorf("推送个人 bundle 修复失败: %w", err)
	}
	_ = secrets.RememberSecretBundles(append(append([]string{}, created...), updated...))
	return repair, nil
}

// listVaultProjectBundleRefs 返回 vault projects/*.yaml 中每个 bundle 被哪些 project 引用。
// 用于判断一个缺省 scope 的 bundle 是否已有 project 在用（ADR 0013）。
func listVaultProjectBundleRefs(repoDir string) (map[string][]string, error) {
	refs := make(map[string][]string)
	if strings.TrimSpace(repoDir) == "" {
		return refs, nil
	}
	projectsDir := filepath.Join(repoDir, types.VaultProjectsDir)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return refs, nil
		}
		return nil, fmt.Errorf("读取 vault projects 失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), types.VaultProjectFileExt) {
			continue
		}
		path := filepath.Join(projectsDir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", path, readErr)
		}
		var project types.Project
		if unmarshalErr := yaml.Unmarshal(data, &project); unmarshalErr != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", path, unmarshalErr)
		}
		projectName := strings.TrimSpace(project.Name)
		if projectName == "" {
			projectName = strings.TrimSuffix(entry.Name(), types.VaultProjectFileExt)
		}
		for _, raw := range project.Bundles {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			refs[name] = append(refs[name], projectName)
		}
	}
	for name := range refs {
		sort.Strings(refs[name])
	}
	return refs, nil
}

// yamlBundleNameScope 解析已有 manifest；保留 members/description，只规范化 name/scope。
// explicit 报告 scope 是否由 manifest 显式声明：缺省与显式 project 的处理方式不同（ADR 0013），
// 归一化后无法再区分，因此必须单独返回。
func yamlBundleNameScope(data []byte) (b types.Bundle, explicit bool, err error) {
	var raw types.Bundle
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return types.Bundle{}, false, err
	}
	raw.Name = strings.TrimSpace(raw.Name)
	scope := types.BundleScope(strings.TrimSpace(string(raw.Scope)))
	switch scope {
	case types.BundleScopeUser, types.BundleScopeProject:
		raw.Scope = scope
		return raw, true, nil
	case "":
		raw.Scope = types.BundleScopeProject
		return raw, false, nil
	default:
		return types.Bundle{}, false, fmt.Errorf("bundle 范围 %q 无效（仅允许 project 或 user）", raw.Scope)
	}
}

func writeBundleManifest(path string, b types.Bundle) error {
	body, err := yaml.Marshal(b)
	if err != nil {
		return err
	}
	header := "# Dec bundle\n"
	return os.WriteFile(path, append([]byte(header), body...), 0644)
}

func bundleOverviewNames(bundles []BundleOverview) []string {
	names := make([]string, 0, len(bundles))
	for _, bo := range bundles {
		names = append(names, bo.Name)
	}
	return names
}
