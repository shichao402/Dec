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
	"github.com/shichao402/Dec/internal/secrets"
)

// RepairOnStartup 对 projectRoot 做启动修复，返回人类可读说明（可空）。
// projectRoot 为空时直接返回；任何 I/O 错误只记入说明，不返回 error。
func RepairOnStartup(projectRoot string) []string {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return nil
	}

	var notes []string
	notes = append(notes, removeLegacyDecConfigDir(root)...)
	notes = append(notes, migrateSecretsConfig()...)
	notes = append(notes, migrateLegacyLocalSecretsNotes(root)...)
	return notes
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
