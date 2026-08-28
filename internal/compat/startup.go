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
	"github.com/shichao402/Dec/internal/types"
)

// RepairOnStartup 对 projectRoot 做启动修复，返回人类可读说明（可空）。
// 用户平面（projectRoot 为空）仍会跑机器级条目；项目专属条目在有根目录时才跑。
// 任何 I/O 错误只记入说明，不返回 error。
func RepairOnStartup(projectRoot string) []string {
	root := strings.TrimSpace(projectRoot)
	var notes []string
	notes = append(notes, removeRetiredIDESkillCopies(root)...)
	notes = append(notes, applyLayoutVersion(root)...)
	if root == "" {
		return notes
	}
	notes = append(notes, removeLegacyDecConfigDir(root)...)
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

func applyLayoutVersion(projectRoot string) []string {
	home, err := repo.GetRootDir()
	if err != nil {
		return []string{fmt.Sprintf("跳过布局版本检查：%v", err)}
	}
	var notes []string
	if cfg, err := config.LoadGlobalConfig(); err == nil {
		if cfg.LayoutVersion > types.LocalLayoutVersion {
			notes = append(notes, fmt.Sprintf("本机 layout_version %d 新于当前 Dec，请升级", cfg.LayoutVersion))
		} else if cfg.LayoutVersion < types.LocalLayoutVersion {
			notes = append(notes, purgeMachineDerived(home)...)
			cfg.LayoutVersion = types.LocalLayoutVersion
			if err := config.SaveGlobalConfig(cfg); err != nil {
				notes = append(notes, fmt.Sprintf("写入本机 layout_version 失败：%v", err))
			}
		}
	} else {
		notes = append(notes, fmt.Sprintf("跳过本机布局检查：%v", err))
	}
	if strings.TrimSpace(projectRoot) == "" {
		return notes
	}
	mgr := config.NewProjectConfigManager(projectRoot)
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		return append(notes, fmt.Sprintf("跳过工作区布局检查：%v", err))
	}
	if cfg.LayoutVersion > types.LocalLayoutVersion {
		return append(notes, fmt.Sprintf("工作区 layout_version %d 新于当前 Dec，请升级", cfg.LayoutVersion))
	}
	if cfg.LayoutVersion < types.LocalLayoutVersion {
		purged := purgeProjectDerived(projectRoot)
		notes = append(notes, purged...)
		cfg.LayoutVersion = types.LocalLayoutVersion
		if err := mgr.SaveProjectConfig(cfg); err != nil {
			notes = append(notes, fmt.Sprintf("写入工作区 layout_version 失败：%v", err))
		} else if len(purged) > 0 {
			notes = append(notes, "工作区布局已更新，请到同步页 Pull")
		}
	}
	return notes
}

func purgeMachineDerived(home string) []string {
	var notes []string
	for _, p := range []string{filepath.Join(home, "cache"), filepath.Join(home, "secrets")} {
		if p == filepath.Join(home, "secrets") {
			if note := purgeExceptKeepDevice(p); note != "" {
				notes = append(notes, note)
			}
			continue
		}
		if note := removeTreeIfExistsSilent(p); note != "" {
			notes = append(notes, note)
		}
	}
	return notes
}

func purgeExceptKeepDevice(secretsDir string) string {
	device := filepath.Join(secretsDir, "device.json")
	_, note := purgeExcept(secretsDir, []string{device})
	return note
}

func purgeProjectDerived(projectRoot string) []string {
	var notes []string
	if note := removeTreeIfExistsSilent(filepath.Join(projectRoot, ".dec", "cache")); note != "" {
		notes = append(notes, note)
	}
	if note := purgeSecretsRoot(projectRoot); note != "" {
		notes = append(notes, note)
	}
	return notes
}

// purgeSecretsRoot 清空项目 .secrets/ 下的旧落地内容，但保留：
//   - 可同步的集成测试账号文件；
//   - 不可同步的本机隔离 DEC_HOME（含 device remember token）。
func purgeSecretsRoot(projectRoot string) string {
	root := filepath.Join(projectRoot, secrets.SecretsRootDir)
	keeps := []string{
		filepath.Join(projectRoot, filepath.FromSlash(secrets.IntegrationAuthRel)),
		filepath.Join(projectRoot, filepath.FromSlash(secrets.IntegrationDecHomeRel)),
	}
	removed, note := purgeExcept(root, keeps)
	if note != "" {
		return note
	}
	if !removed {
		return ""
	}
	return fmt.Sprintf("已清理 %s（保留集成凭据与本机设备状态）", root)
}

// purgeExcept 删除 dir 下所有内容，但保留通往 keeps 的路径。
func purgeExcept(dir string, keeps []string) (bool, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, ""
		}
		return false, fmt.Sprintf("跳过清理 %s：%v", dir, err)
	}
	removed := false
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		exactKeep := false
		var childKeeps []string
		for _, keep := range keeps {
			if path == keep {
				exactKeep = true
				break
			}
			if strings.HasPrefix(keep, path+string(filepath.Separator)) {
				childKeeps = append(childKeeps, keep)
			}
		}
		if exactKeep {
			continue
		}
		if len(childKeeps) > 0 {
			childRemoved, note := purgeExcept(path, childKeeps)
			if note != "" {
				return removed, note
			}
			if childRemoved {
				removed = true
			}
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return removed, fmt.Sprintf("清理 %s 失败：%v", path, err)
		}
		removed = true
	}
	return removed, ""
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
