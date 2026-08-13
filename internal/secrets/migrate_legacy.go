package secrets

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LegacyLocalSecret 描述一条从旧消费者路径迁入 SyncTarget 的映射。
type LegacyLocalSecret struct {
	OldProjectRel string // 旧：项目根相对路径
	Target        SyncTarget
	NoteRel       string // 新：相对 LocalRoot
}

// DefaultLegacyLocalMigrations 返回常见旧 mise/env 路径 → `.secrets/**/env/*.env` 的映射。
// 不覆盖已存在的目标文件；调用方决定是否删除旧文件。
func DefaultLegacyLocalMigrations(enabledBundles []string) ([]LegacyLocalSecret, error) {
	var out []LegacyLocalSecret
	for _, name := range enabledBundles {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		target, err := NewBundleSyncTarget(name, "")
		if err != nil {
			return nil, err
		}
		candidates := []string{
			path.Join(".config/mise/conf.d", name+".toml"),
			path.Join(".config/mise/conf.d", name+".env"),
			path.Join("mise/conf.d", name+".toml"),
		}
		for _, old := range candidates {
			out = append(out, LegacyLocalSecret{
				OldProjectRel: old,
				Target:        target,
				NoteRel:       path.Join("env", name+".env"),
			})
		}
	}
	return out, nil
}

// MigrateLegacyLocalResult 是一次本地迁移结果。
type MigrateLegacyLocalResult struct {
	Moved   []string // 已迁入的项目根相对新路径
	Skipped []string // 旧文件不存在或目标已存在
	Removed []string // 已删除的旧路径
}

// MigrateLegacyLocalSecrets 把旧消费者路径文件迁入 `.secrets` 同步根。
// toml 内容若含 [env] 表，尽量转成 dotenv（KEY=VALUE）；否则原样写入 .env 并可能无法被 LoadEnv 使用。
// removeOld=true 时在成功写入后删除旧文件。
func MigrateLegacyLocalSecrets(projectRoot string, items []LegacyLocalSecret, removeOld bool) (*MigrateLegacyLocalResult, error) {
	res := &MigrateLegacyLocalResult{}
	if err := EnsureSecretsGitignore(projectRoot); err != nil {
		return nil, err
	}
	for _, item := range items {
		oldAbs := filepath.Join(projectRoot, filepath.FromSlash(item.OldProjectRel))
		raw, err := os.ReadFile(oldAbs)
		if err != nil {
			if os.IsNotExist(err) {
				res.Skipped = append(res.Skipped, item.OldProjectRel)
				continue
			}
			return nil, err
		}
		newAbs, err := AbsolutePath(projectRoot, item.Target, item.NoteRel)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(newAbs); err == nil {
			res.Skipped = append(res.Skipped, item.NoteRel+" (exists)")
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		content := convertLegacyEnvBytes(raw, item.OldProjectRel)
		if err := os.MkdirAll(filepath.Dir(newAbs), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(newAbs, content, 0600); err != nil {
			return nil, err
		}
		projectRel, _ := ProjectRelPath(item.Target, item.NoteRel)
		res.Moved = append(res.Moved, projectRel)
		if removeOld {
			if err := os.Remove(oldAbs); err != nil && !os.IsNotExist(err) {
				return res, fmt.Errorf("删除旧文件 %s 失败: %w", item.OldProjectRel, err)
			}
			res.Removed = append(res.Removed, item.OldProjectRel)
		}
	}
	return res, nil
}

func convertLegacyEnvBytes(raw []byte, oldRel string) []byte {
	text := string(raw)
	if !strings.HasSuffix(strings.ToLower(oldRel), ".toml") {
		return raw
	}
	// 极简：提取 [env] 段的 KEY = "value" / KEY = 'value' / KEY = value
	lines := strings.Split(text, "\n")
	inEnv := false
	var out []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			inEnv = trim == "[env]" || strings.HasPrefix(trim, "[env.")
			continue
		}
		if !inEnv || trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		eq := strings.IndexByte(trim, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(trim[:eq])
		val := strings.TrimSpace(trim[eq+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		out = append(out, key+"="+val)
	}
	if len(out) == 0 {
		return raw
	}
	return []byte(strings.Join(out, "\n") + "\n")
}
