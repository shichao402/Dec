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

// LoadEnvForP 精确读取一个 P 在一个 plane 的单层 .env；不合并 requires、
// 不跨 user/project，也不回退旧 bundles 布局。
func LoadEnvForP(projectRoot, pName string, plane SyncPlane) (map[string]string, error) {
	target, err := NewPSyncTarget(pName, plane)
	if err != nil {
		return nil, err
	}
	abs, err := ResolveAbsDir(projectRoot, target)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	envDir := filepath.Join(abs, TypeDirEnv)
	entries, err := os.ReadDir(envDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	owned := make(map[string]string)
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".env") {
			continue
		}
		file := filepath.Join(envDir, ent.Name())
		vars, err := parseDotEnvFile(file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path.Join(target.LocalRoot, TypeDirEnv, ent.Name()), err)
		}
		for key, value := range vars {
			display := path.Join(target.LocalRoot, TypeDirEnv, ent.Name())
			if previous, ok := owned[key]; ok {
				return nil, fmt.Errorf("环境变量 %s 在 %s 与 %s 中重复定义", key, previous, display)
			}
			owned[key] = display
			out[key] = value
		}
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
