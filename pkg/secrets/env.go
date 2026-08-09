package secrets

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// LoadEnvForBundle 读取指定 bundle + project 的 env/*.env。
// 加载顺序：bundle → project（后者覆盖前者）；同一文件内重复键报错；
// 两个不同 bundle 不应在同一次调用中出现（本函数只接受一个 bundle）。
func LoadEnvForBundle(projectRoot, bundleName string) (map[string]string, error) {
	out := make(map[string]string)

	loadRoot := func(localRoot string) error {
		envDir := filepath.Join(projectRoot, filepath.FromSlash(localRoot), "env")
		entries, err := os.ReadDir(envDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		fileOwned := make(map[string]string) // key -> file
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			name := ent.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".env") {
				continue
			}
			abs := filepath.Join(envDir, name)
			vars, err := parseDotEnvFile(abs)
			if err != nil {
				return fmt.Errorf("%s: %w", path.Join(localRoot, "env", name), err)
			}
			for k, v := range vars {
				if prev, ok := fileOwned[k]; ok {
					return fmt.Errorf("环境变量 %s 在 %s 与 %s 中重复定义", k, prev, path.Join(localRoot, "env", name))
				}
				fileOwned[k] = path.Join(localRoot, "env", name)
				out[k] = v
			}
		}
		return nil
	}

	bundleName = strings.TrimSpace(bundleName)
	if bundleName != "" {
		target, err := NewBundleSyncTarget(bundleName, "")
		if err != nil {
			return nil, err
		}
		if err := loadRoot(target.LocalRoot); err != nil {
			return nil, err
		}
	}
	if err := loadRoot(ProjectSecretsLocalRel); err != nil {
		return nil, err
	}
	return out, nil
}

func parseDotEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vars := make(map[string]string)
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, err := parseDotEnvLine(line)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行: %w", lineNo, err)
		}
		if _, dup := vars[key]; dup {
			return nil, fmt.Errorf("第 %d 行: 键 %s 重复", lineNo, key)
		}
		vars[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return vars, nil
}

func parseDotEnvLine(line string) (string, string, error) {
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return "", "", fmt.Errorf("需要 KEY=VALUE 格式")
	}
	key := strings.TrimSpace(line[:eq])
	if key == "" || strings.ContainsFunc(key, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	}) {
		return "", "", fmt.Errorf("非法变量名 %q", key)
	}
	val := line[eq+1:]
	if strings.Contains(val, "\n") {
		return "", "", fmt.Errorf("不支持多行值")
	}
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return key, val, nil
}

// EnsureSecretsGitignore 确保 /.secrets/ 出现在项目 .gitignore 中。
func EnsureSecretsGitignore(projectRoot string) error {
	gi := filepath.Join(projectRoot, ".gitignore")
	data, err := os.ReadFile(gi)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	needle := "/" + SecretsRootDir + "/"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == needle || trimmed == SecretsRootDir+"/" || trimmed == "/"+SecretsRootDir || trimmed == SecretsRootDir {
			return nil
		}
	}
	var b strings.Builder
	b.WriteString(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n# Dec secrets sync root\n")
	b.WriteString(needle)
	b.WriteByte('\n')
	return os.WriteFile(gi, []byte(b.String()), 0644)
}
