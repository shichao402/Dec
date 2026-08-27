// Package compat 集中存放启动期兼容性修复。
//
// 设计意图：入口先跑一遍、修完再进主逻辑；单项要轻量幂等，失败不阻断启动。
// 内部可以乱写；旧修复项失去价值后集中删掉即可。
package compat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/ide"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
)

// RepairOnStartup 对 projectRoot 做启动修复，返回人类可读说明（可空）。
// 用户平面（projectRoot 为空）仍会跑机器级条目；项目专属条目在有根目录时才跑。
// 任何 I/O 错误只记入说明，不返回 error。
func RepairOnStartup(projectRoot string) []string {
	root := strings.TrimSpace(projectRoot)
	var notes []string
	notes = append(notes, removeRetiredIDESkillCopies(root)...)
	notes = append(notes, purgeLegacyPLayout(root)...)
	if root == "" {
		return notes
	}
	notes = append(notes, removeLegacyDecConfigDir(root)...)
	notes = append(notes, migrateSecretsConfig()...)
	notes = append(notes, migrateLegacyLocalSecretsNotes(root)...)
	return notes
}

// retiredIDESkillDirs 启动时从各 IDE skills 目录摘掉的托管副本名。
// 条目失效后删掉对应名字或整段函数即可。
var retiredIDESkillDirs = []string{
	"dec-cli-skill",
}

func removeRetiredIDESkillCopies(projectRoot string) []string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err != nil {
			return []string{fmt.Sprintf("跳过退役 skill 清理：%v", err)}
		}
		return nil
	}

	var notes []string
	seen := map[string]struct{}{}
	for _, name := range ide.List() {
		impl := ide.Get(name)
		dirs := []string{impl.SkillsDirForPlane(ide.PlaneUser, "", home)}
		if projectRoot != "" {
			dirs = append(dirs, impl.SkillsDirForPlane(ide.PlaneProject, projectRoot, home))
		}
		for _, skillsDir := range dirs {
			for _, retired := range retiredIDESkillDirs {
				target := filepath.Join(skillsDir, retired)
				if _, ok := seen[target]; ok {
					continue
				}
				seen[target] = struct{}{}
				if note := removeTreeIfExists(target); note != "" {
					notes = append(notes, note)
				}
			}
		}
	}
	return notes
}

func removeTreeIfExists(path string) string {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return fmt.Sprintf("跳过清理 %s：%v", path, err)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Sprintf("清理 %s 失败：%v", path, err)
	}
	return fmt.Sprintf("已删除退役 IDE 副本 %s", path)
}

func migrateSecretsConfig() []string {
	changed, err := secrets.MigrateConfigIfNeeded()
	if err != nil {
		return []string{fmt.Sprintf("secrets 配置迁移跳过：%v", err)}
	}
	if !changed {
		return nil
	}
	return []string{"已迁移 secrets 配置中的废弃 folder 字段"}
}

func migrateLegacyLocalSecretsNotes(projectRoot string) []string {
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	enabled := []string{}
	if err == nil && projectConfig != nil {
		enabled = append(enabled, projectConfig.EnabledBundles...)
	}
	if userBundles, cfgErr := config.UserEnabledBundles(); cfgErr == nil {
		enabled = secrets.NormalizeBundleNames(append(enabled, userBundles...))
	}
	items, err := secrets.DefaultLegacyLocalMigrations(enabled)
	if err != nil {
		return []string{fmt.Sprintf("本地 secrets 路径迁移跳过：%v", err)}
	}
	if len(items) == 0 {
		return nil
	}
	res, err := secrets.MigrateLegacyLocalSecrets(projectRoot, items, false)
	if err != nil {
		return []string{fmt.Sprintf("本地 secrets 路径迁移跳过：%v", err)}
	}
	if res == nil || len(res.Moved) == 0 {
		return nil
	}
	notes := make([]string, 0, len(res.Moved))
	for _, p := range res.Moved {
		notes = append(notes, fmt.Sprintf("已迁移本地 secret → %s", p))
	}
	return notes
}

// removeLegacyDecConfigDir 删除早期 .dec/config/ 目录（内含 project/technology/packs
// 等 json/yaml）。现行配置是同级文件 .dec/config.yaml，与该目录无关。
func removeLegacyDecConfigDir(projectRoot string) []string {
	dir := filepath.Join(projectRoot, ".dec", "config")
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []string{fmt.Sprintf("跳过清理 .dec/config：%v", err)}
	}
	if !info.IsDir() {
		// 同名非目录（极少见）不动，避免误伤。
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return []string{fmt.Sprintf("清理 .dec/config 失败：%v", err)}
	}
	return []string{"已删除早期目录 .dec/config/"}
}

const pLayoutPurgeMarker = "p-layout-local-purged-v1"

func purgeLegacyPLayout(projectRoot string) []string {
	home, err := repo.GetRootDir()
	if err != nil {
		return []string{fmt.Sprintf("跳过旧版本地清理：%v", err)}
	}
	marker := filepath.Join(home, "state", pLayoutPurgeMarker)
	if _, err := os.Lstat(marker); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return []string{fmt.Sprintf("跳过旧版本地清理：%v", err)}
	}

	var notes []string
	changed := false
	remove := func(path, label string) {
		if note := removeTreeIfExistsSilent(path); note != "" {
			notes = append(notes, note)
			changed = true
			return
		}
		_ = label
	}
	remove(filepath.Join(home, "cache"), "用户 cache")
	remove(filepath.Join(home, "secrets", "bundles"), "用户 secrets/bundles")
	remove(filepath.Join(home, "migrations"), "迁移日志")
	if projectRoot != "" {
		remove(filepath.Join(projectRoot, ".dec", "cache"), "项目 cache")
		if note := purgeSecretsRoot(projectRoot); note != "" {
			notes = append(notes, note)
			changed = true
		}
	}

	if cfg, err := config.LoadGlobalConfig(); err == nil && (len(cfg.EnabledBundles) > 0 || len(cfg.EnabledProjects) > 0) {
		cfg.EnabledBundles = nil
		cfg.EnabledProjects = nil
		if err := config.SaveGlobalConfig(cfg); err != nil {
			notes = append(notes, fmt.Sprintf("清空用户启用列表失败：%v", err))
		} else {
			notes = append(notes, "已清空用户平面启用列表")
			changed = true
		}
	}
	if projectRoot != "" {
		mgr := config.NewProjectConfigManager(projectRoot)
		if cfg, err := mgr.LoadProjectConfig(); err == nil && len(cfg.EnabledBundles) > 0 {
			cfg.EnabledBundles = nil
			if err := mgr.SaveProjectConfig(cfg); err != nil {
				notes = append(notes, fmt.Sprintf("清空项目启用列表失败：%v", err))
			} else {
				notes = append(notes, "已清空项目平面启用列表")
				changed = true
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		return append(notes, fmt.Sprintf("写入清理标记失败：%v", err))
	}
	if err := os.WriteFile(marker, []byte("1\n"), 0o600); err != nil {
		return append(notes, fmt.Sprintf("写入清理标记失败：%v", err))
	}
	if changed {
		notes = append(notes, "已清理旧版本地资产；请在 Bundles 页重新选择 P，并在 Run 页 Pull")
	}
	return notes
}

// purgeSecretsRoot 清空项目 .secrets/ 下的旧落地内容，但保留集成测试凭据目录。
// 后者不是任何 P 的落地内容，删掉会让 live 测试重新要人工解锁。
func purgeSecretsRoot(projectRoot string) string {
	root := filepath.Join(projectRoot, secrets.SecretsRootDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return fmt.Sprintf("跳过清理 %s：%v", root, err)
	}
	keep := filepath.Join(projectRoot, filepath.FromSlash(secrets.IntegrationDirRel))
	removed := false
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if path == keep || strings.HasPrefix(keep, path+string(filepath.Separator)) {
			if note := purgeExcept(path, keep); note != "" {
				return note
			}
			removed = true
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Sprintf("清理 %s 失败：%v", path, err)
		}
		removed = true
	}
	if !removed {
		return ""
	}
	return fmt.Sprintf("已清理 %s（保留 %s）", root, secrets.IntegrationDirRel)
}

// purgeExcept 删除 dir 下所有内容，但保留通往 keep 的那条路径。
func purgeExcept(dir, keep string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return fmt.Sprintf("跳过清理 %s：%v", dir, err)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if path == keep || strings.HasPrefix(keep, path+string(filepath.Separator)) {
			if path != keep {
				if note := purgeExcept(path, keep); note != "" {
					return note
				}
			}
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Sprintf("清理 %s 失败：%v", path, err)
		}
	}
	return ""
}

func removeTreeIfExistsSilent(path string) string {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return fmt.Sprintf("跳过清理 %s：%v", path, err)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Sprintf("清理 %s 失败：%v", path, err)
	}
	return fmt.Sprintf("已删除 %s", path)
}
