package legacy

import (
	"errors"
	"fmt"
	"os"

	"github.com/shichao402/Dec/internal/sysproc"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Processes 结束指定路径下的程序进程（需求 5.3）。
type Processes interface {
	// StopWithin 结束可执行文件位于 root 内（或就是 root）的进程，返回被结束的进程描述。
	StopWithin(root string) ([]string, error)
}

// SystemProcesses 返回本机实现。Windows 上旧版没有常驻进程，直接不做。
func SystemProcesses() Processes {
	if runtime.GOOS == "windows" {
		return noProcesses{}
	}
	return &psProcesses{list: listPS, grace: 3 * time.Second, self: os.Getpid()}
}

type noProcesses struct{}

func (noProcesses) StopWithin(string) ([]string, error) { return nil, nil }

type proc struct {
	pid  int
	args string
}

type psProcesses struct {
	list  func() ([]proc, error)
	grace time.Duration
	self  int
}

// listPS 用 ps 取进程的完整命令行。macOS 与 Linux 都支持 -axo pid=,args=。
func listPS() ([]proc, error) {
	out, err := sysproc.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps：%w", err)
	}
	var ps []proc
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		i := strings.IndexByte(line, ' ')
		if i < 0 {
			continue
		}
		pid, err := strconv.Atoi(line[:i])
		if err != nil {
			continue
		}
		ps = append(ps, proc{pid: pid, args: strings.TrimSpace(line[i+1:])})
	}
	return ps, nil
}

// runningWithin 判断命令行的程序路径是否落在 root 内。
// 只看命令行开头，不看参数：参数里出现这个路径的进程（比如正在看日志的 tail）不该被结束。
func runningWithin(args, root string) bool {
	root = filepath.Clean(root)
	return args == root || strings.HasPrefix(args, root+" ") || strings.HasPrefix(args, root+string(filepath.Separator))
}

func (p *psProcesses) StopWithin(root string) ([]string, error) {
	ps, err := p.list()
	if err != nil {
		return nil, err
	}
	var targets []proc
	for _, pr := range ps {
		if pr.pid != p.self && pr.pid > 1 && runningWithin(pr.args, root) {
			targets = append(targets, pr)
		}
	}
	var stopped []string
	for _, pr := range targets {
		if err := stopPID(pr.pid, p.grace); err != nil {
			return stopped, fmt.Errorf("%d %s：%w", pr.pid, pr.args, err)
		}
		stopped = append(stopped, fmt.Sprintf("%d %s", pr.pid, pr.args))
	}
	return stopped, nil
}

// stopPID 先 SIGTERM，宽限期内没退出再 SIGKILL。
func stopPID(pid int, grace time.Duration) error {
	pr, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := pr.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if pr.Signal(syscall.Signal(0)) != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := pr.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
