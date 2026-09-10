package sysproc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 允许直接调 os/exec 的文件：包装器自身、本守卫（正文含被检测的字面量），
// 以及需要用户看见窗口的入口。internal/service/client.go 自行设置
// detachedProcessAttributes()。
var directExecAllowed = map[string]bool{
	"internal/sysproc/sysproc.go":            true,
	"internal/sysproc/guard_test.go":         true,
	"internal/consoleopen/launch_windows.go": true,
	"internal/consoleopen/launch_unix.go":    true,
	"internal/editor/editor.go":              true,
	"internal/service/client.go":             true,
}

// TestNoDirectExecCommand 守住「Windows 上不弹控制台窗口」这条线。
//
// dec-server 与 go test 的测试二进制都可能没有自己的 console，此时每个用
// exec.Command 起的控制台程序（git / ssh / ssh-keygen）都会新建一个控制台。
// Win11 默认终端是 Windows Terminal，新建控制台就是一个弹到前台的窗口，会
// 直接抢走用户的键盘焦点。sysproc.Command 用 CREATE_NO_WINDOW 消除这一点。
//
// 测试代码同样受约束：`go test ./...` 会拉起成百上千个 git 进程。
func TestNoDirectExecCommand(t *testing.T) {
	root := repoRoot(t)
	var offenders []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			// third_party 是 gitignore 的外部 sparse checkout，不是 Dec 的源码。
			case ".git", "node_modules", "vendor", "target", "dist", "third_party":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if directExecAllowed[rel] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, "exec.Command(") || strings.Contains(line, "exec.CommandContext(") {
				offenders = append(offenders, rel+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历仓库失败: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("以下位置直接用 os/exec 起子进程，Windows 上会弹控制台窗口，请改用 sysproc.Command / sysproc.CommandContext：\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("未找到 go.mod")
		}
		dir = parent
	}
}
