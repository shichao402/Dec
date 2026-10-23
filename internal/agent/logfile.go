package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 代理日志（需求 5）。
//
// 只有一个日志文件 logs/agent.log。以前 launchd 把 stdout 与 stderr 分别
// 重定向到 agent.log 与 agent.err.log，而所有日志都走 stderr，于是出现
// 「一个恒为空、另一个承载全部内容」的两个文件，排查时要先猜看哪个。
//
// 这里由代理自己写文件并按大小轮转，系统服务的重定向只用来兜住 panic
// 这类代理自己来不及写的输出，并且指向同一个文件。
const (
	// LogFileName 是唯一的日志文件名。
	LogFileName = "agent.log"
	// defaultLogMaxBytes 是单个日志文件的轮转阈值。
	defaultLogMaxBytes = 5 << 20
	// defaultLogKeep 是保留的历史文件份数（agent.log.1 ... agent.log.N）。
	defaultLogKeep = 3
)

// LogPath 返回状态目录下的日志文件路径。
func LogPath(stateDir string) string {
	return filepath.Join(stateDir, "logs", LogFileName)
}

// LogFile 是带轮转的日志写入器，可并发使用。
type LogFile struct {
	path     string
	maxBytes int64
	keep     int
	// fallback 是文件不可写时的退路（需求 5.5）。
	fallback io.Writer
	// echo 非空时每条日志额外写一份，用于在终端里前台运行时也能看到。
	echo io.Writer

	mu   sync.Mutex
	f    *os.File
	size int64
	now  func() time.Time
}

// LogFileOptions 是日志写入器参数。
type LogFileOptions struct {
	// Path 是日志文件路径。
	Path string
	// MaxBytes 是轮转阈值，<=0 用默认值。
	MaxBytes int64
	// Keep 是保留的历史份数，<=0 用默认值。
	Keep int
	// Fallback 是文件不可写时的输出，为空用 os.Stderr。
	Fallback io.Writer
	// Echo 非空时额外输出一份。
	Echo io.Writer
}

// OpenLogFile 打开日志写入器。
//
// 打开失败不返回错误：日志不能成为代理起不来的理由，写不进文件就退回
// Fallback，并在 Fallback 上说明一次原因。
func OpenLogFile(opts LogFileOptions) *LogFile {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultLogMaxBytes
	}
	if opts.Keep <= 0 {
		opts.Keep = defaultLogKeep
	}
	if opts.Fallback == nil {
		opts.Fallback = os.Stderr
	}
	l := &LogFile{
		path:     opts.Path,
		maxBytes: opts.MaxBytes,
		keep:     opts.Keep,
		fallback: opts.Fallback,
		echo:     opts.Echo,
		now:      time.Now,
	}
	if err := l.open(); err != nil {
		fmt.Fprintf(l.fallback, "[dec-agent] 日志文件不可写，改为输出到标准错误: %v\n", err)
	}
	return l
}

// Path 返回日志文件路径。
func (l *LogFile) Path() string { return l.path }

// open 打开（或创建）日志文件并读取当前大小。调用方持锁或尚未并发。
func (l *LogFile) open() error {
	if l.path == "" {
		return fmt.Errorf("未指定日志路径")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	l.f = f
	l.size = info.Size()
	return nil
}

// Printf 写一条带时间戳的日志。
func (l *LogFile) Printf(format string, args ...any) {
	line := fmt.Sprintf("%s [dec-agent] %s\n",
		l.now().Format("2006-01-02T15:04:05.000Z07:00"), fmt.Sprintf(format, args...))
	_, _ = l.Write([]byte(line))
}

// Write 实现 io.Writer。
func (l *LogFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.echo != nil {
		_, _ = l.echo.Write(p)
	}
	if l.f == nil {
		// 文件此前不可写：每次都再试一次，目录权限修好后自动恢复。
		if err := l.open(); err != nil {
			return l.fallback.Write(p)
		}
	}
	if l.size+int64(len(p)) > l.maxBytes && l.size > 0 {
		l.rotate()
	}
	if l.f == nil {
		return l.fallback.Write(p)
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	if err != nil {
		_ = l.f.Close()
		l.f = nil
		return l.fallback.Write(p)
	}
	return n, nil
}

// rotate 把 agent.log 依次挪成 agent.log.1 ... agent.log.N。调用方持锁。
//
// 轮转失败只影响历史份数，不影响继续写：最坏情况是在原文件上接着追加。
func (l *LogFile) rotate() {
	_ = l.f.Close()
	l.f = nil
	_ = os.Remove(fmt.Sprintf("%s.%d", l.path, l.keep))
	for i := l.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
	if err := l.open(); err != nil {
		l.f = nil
	}
}

// Close 关闭日志文件。
func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
