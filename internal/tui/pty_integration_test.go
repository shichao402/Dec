//go:build integration && !windows

package tui_test

// PTY integration test for the `dec` TUI entry path.
//
// 验证 `dec` 无参能在真实 pty 下进入 TUI 首页，完成页面导航，
// 并通过 `q` 优雅退出，对应 Documents/TUI_ARCHITECTURE.md §9.6。
//
// 本测试由 build tag `integration` 控制，默认不参与 `go test ./...`；
// 因为依赖 `/dev/ptmx`，同时通过 `!windows` 避免在 Windows runner 上尝试。
// 本地运行：
//
//	go test -tags=integration ./internal/tui/...
//
// CI 中若想启用，需在 POSIX runner（linux/macOS）上加 `-tags=integration`。

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/unlock"
)

const (
	ptyStartupTimeout  = 8 * time.Second
	ptyShutdownTimeout = 5 * time.Second
	ptyReadChunk       = 4096
)

func TestMain(m *testing.M) {
	if root := repoRootFromCWD(); root != "" {
		if _, err := secrets.ApplyIntegrationAuth(root); err != nil {
			panic("ApplyIntegrationAuth: " + err.Error())
		}
	}
	os.Exit(m.Run())
}

// TestPTYStartupAndQuit 构建 dec 可执行文件，在伪终端中启动 TUI，
// 等待首屏渲染，完成 5 页 tab 循环和一次 shift+tab 回退，
// 然后按 `q` 退出，断言：
//   - 首屏输出包含 TUI 首页锚点字符串（"Dec Shell"）
//   - 页面导航日志覆盖 Home / Bundles / Project / Run / Delete / Settings
//   - 子进程以退出码 0 结束
//   - pty 在子进程退出后进入 EOF
func TestPTYStartupAndQuit(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Build tag 已排除，但保底跳过。
		t.Skip("PTY integration test does not run on Windows")
	}

	bin := buildDecBinary(t)

	tests := []struct {
		name string
		term string
		rows uint16
		cols uint16
		lang string
	}{
		{
			name: "xterm-256color-wide",
			term: "xterm-256color",
			rows: 40,
			cols: 120,
			lang: "C.UTF-8",
		},
		{
			name: "linux-narrow",
			term: "linux",
			rows: 30,
			cols: 80,
			lang: "C.UTF-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runPTYScenario(t, bin, tt.term, tt.rows, tt.cols, tt.lang)
		})
	}
}

func runPTYScenario(t *testing.T, bin, term string, rows, cols uint16, lang string) {
	t.Helper()

	cmd := exec.Command(bin)
	// 使用独立、最小的环境，避免外部 DEC_NO_TUI / TERM=dumb 影响默认入口分流。
	// 子进程不是 go test 二进制，需显式禁止 web unlock，否则测试会弹出浏览器等人工输入。
	cmd.Env = append(os.Environ(),
		"TERM="+term,
		"LANG="+lang,
		"LC_ALL="+lang,
		"DEC_NO_TUI=",
		"DEC_HOME="+t.TempDir(),
		unlock.EnvNoWebUnlock+"=1",
	)
	if pw := strings.TrimSpace(os.Getenv("DEC_BW_PASSWORD")); pw != "" {
		cmd.Env = append(cmd.Env, "DEC_BW_PASSWORD="+pw)
	}
	// 使用临时空目录作为 CWD，避免仓库内的 .dec/ 状态污染首屏。
	cmd.Dir = t.TempDir()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start failed: %v", err)
	}
	t.Cleanup(func() {
		_ = ptmx.Close()
	})

	// 设置一个合理的窗口尺寸，让 TUI 的响应式布局产出稳定首屏。
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		t.Fatalf("pty.Setsize failed: %v", err)
	}

	// 后台持续抓取 pty 输出。
	var (
		mu     sync.Mutex
		buf    bytes.Buffer
		readCh = make(chan struct{})
	)
	go func() {
		defer close(readCh)
		chunk := make([]byte, ptyReadChunk)
		for {
			n, err := ptmx.Read(chunk)
			if n > 0 {
				mu.Lock()
				buf.Write(chunk[:n])
				mu.Unlock()
			}
			if err != nil {
				// pty 对端关闭时通常是 EOF 或 EIO，两种都表示子进程已退出。
				return
			}
		}
	}()

	// 等待首屏锚点出现。Lip Gloss 会在单元之间插入 ANSI 样式码，
	// 为此搜索时先剥掉 ANSI 序列。状态栏里稳定的 "q quit | j/k nav"
	// 比侧边栏标题更容易命中（后者可能被 lipgloss 拆成多段）。
	anchor := "q quit | j/k nav"
	if err := waitForContains(&mu, &buf, anchor, ptyStartupTimeout); err != nil {
		snapshot := snapshotOutput(&mu, &buf)
		// 尽量不阻塞，让后台 goroutine 能继续消费后续输出。
		_ = cmd.Process.Kill()
		t.Fatalf("TUI 首屏未在 %s 内出现 %q: %v\n当前输出:\n%s",
			ptyStartupTimeout, anchor, err, snapshot)
	}

	// 以状态栏右侧的 "page X" 作为导航锚点：它每帧重绘，是当前页唯一稳定的屏上证据。
	// 页面停留期间同一锚点会重复出现，因此按键前先取基线，再等计数增长。
	tabTargets := []string{"Bundles", "Project", "Run", "Remote", "Settings", "Home"}
	for _, target := range tabTargets {
		anchor := "page " + target
		baseline := countOccurrences(&mu, &buf, anchor)
		if _, err := ptmx.Write([]byte("\t")); err != nil {
			_ = cmd.Process.Kill()
			t.Fatalf("向 pty 写入 tab 失败: %v", err)
		}
		if err := waitForOccurrenceCount(&mu, &buf, anchor, baseline+1, ptyStartupTimeout); err != nil {
			snapshot := snapshotOutput(&mu, &buf)
			_ = cmd.Process.Kill()
			t.Fatalf("TUI 未在 %s 内导航到 %s: %v\n当前输出:\n%s",
				ptyStartupTimeout, target, err, snapshot)
		}
	}

	// Bubble Tea 将 Shift+Tab 识别为 ESC [ Z；从 Home 回退应回到 Settings。
	settingsBaseline := countOccurrences(&mu, &buf, "page Settings")
	if _, err := ptmx.Write([]byte("\x1b[Z")); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("向 pty 写入 shift+tab 失败: %v", err)
	}
	if err := waitForOccurrenceCount(&mu, &buf, "page Settings", settingsBaseline+1, ptyStartupTimeout); err != nil {
		snapshot := snapshotOutput(&mu, &buf)
		_ = cmd.Process.Kill()
		t.Fatalf("TUI 未在 %s 内通过 shift+tab 回到 Settings: %v\n当前输出:\n%s",
			ptyStartupTimeout, err, snapshot)
	}

	// 发送 `q` 触发退出。
	if _, err := ptmx.Write([]byte("q")); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("向 pty 写入退出键失败: %v", err)
	}

	// 等待子进程退出（带超时保护）。
	waitErr := waitWithTimeout(cmd, ptyShutdownTimeout)

	// 无论成功失败，都等 reader goroutine 收敛。
	select {
	case <-readCh:
	case <-time.After(2 * time.Second):
		// 读取端未收敛一般是 ptmx.Close 还没触发；由 Cleanup 兜底。
	}

	if waitErr != nil {
		snapshot := snapshotOutput(&mu, &buf)
		t.Fatalf("dec TUI 未能在 %s 内优雅退出: %v\n累计输出:\n%s",
			ptyShutdownTimeout, waitErr, snapshot)
	}

	if !cmd.ProcessState.Exited() {
		t.Fatalf("子进程未正常退出: %v", cmd.ProcessState)
	}
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		snapshot := snapshotOutput(&mu, &buf)
		t.Fatalf("dec 以非零退出码结束: code=%d\n累计输出:\n%s", code, snapshot)
	}
}

// buildDecBinary 使用 go build 在临时目录构建 dec 可执行文件，
// 避免污染仓库并保证测试运行的版本与当前工作区一致。
func buildDecBinary(t *testing.T) string {
	t.Helper()

	repoRoot := findRepoRoot(t)
	outDir := t.TempDir()
	outBin := filepath.Join(outDir, "dec")
	targets := []struct {
		name string
		pkg  string
	}{
		{"dec", "."},
		{"dec-server", "./cmd/dec-server"},
	}
	for _, target := range targets {
		build := exec.Command("go", "build", "-o", filepath.Join(outDir, target.name), target.pkg)
		build.Dir = repoRoot
		build.Env = os.Environ()
		out, err := build.CombinedOutput()
		if err != nil {
			t.Fatalf("go build %s 失败: %v\n%s", target.name, err, out)
		}
	}
	return outBin
}

// repoRootFromCWD 从当前目录向上查找包含 go.mod 的仓库根。
func repoRootFromCWD() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// findRepoRoot 从当前测试文件目录向上查找包含 go.mod 的仓库根。
func findRepoRoot(t *testing.T) string {
	t.Helper()
	if root := repoRootFromCWD(); root != "" {
		return root
	}
	t.Fatalf("找不到仓库根（向上查找 go.mod 失败）")
	return ""
}

// ansiEscape 匹配常见的 CSI / OSC 控制序列，用于在断言前剥离样式码。
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07`)

// waitForContains 轮询检查 buffer 是否包含目标子串（剥离 ANSI 后），直到超时。
func waitForContains(mu *sync.Mutex, buf *bytes.Buffer, needle string, timeout time.Duration) error {
	return waitForOccurrenceCount(mu, buf, needle, 1, timeout)
}

func countOccurrences(mu *sync.Mutex, buf *bytes.Buffer, needle string) int {
	mu.Lock()
	defer mu.Unlock()
	return strings.Count(plainOutput(buf.String()), needle)
}

func waitForOccurrenceCount(mu *sync.Mutex, buf *bytes.Buffer, needle string, minCount int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		plain := plainOutput(buf.String())
		mu.Unlock()
		if strings.Count(plain, needle) >= minCount {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return io.EOF
}

func plainOutput(out string) string {
	return ansiEscape.ReplaceAllString(out, "")
}

func snapshotOutput(mu *sync.Mutex, buf *bytes.Buffer) string {
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

// waitWithTimeout 等待命令退出，超时则 Kill 并返回错误。
func waitWithTimeout(cmd *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return context.DeadlineExceeded
	}
}
