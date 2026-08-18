package tui

import (
	"os"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shichao402/Dec/internal/diag"
)

// DEC_DIAG_KEYS=1 打开按键/渲染诊断，写入 diag.StartupLogPath()。
//
// 界面「卡住」有两种完全不同的成因，肉眼分不出来：按键根本没进程序（终端处于选择模式、
// 输入 reader 掉了），或按键进了但 Update/View 阻塞导致不重绘。日志里 key 与 view 是否
// 成对出现即可判定，不必再猜。默认关闭，避免污染启动日志。
var diagKeysEnabled = strings.TrimSpace(os.Getenv("DEC_DIAG_KEYS")) == "1"

var diagFrameSeq atomic.Uint64

func (m model) diagKeyReceived(msg tea.KeyMsg) {
	if !diagKeysEnabled {
		return
	}
	page := ""
	if m.pageIndex >= 0 && m.pageIndex < len(m.pages) {
		page = m.pages[m.pageIndex]
	}
	diag.StartupLog("key %q page=%s update=%s restart=%s push=%s remove=%s bootstrap=%s updating=%v pulling=%v",
		msg.String(), page, m.updateStage, m.serverRestartStage, m.pushStage, m.removeStage,
		m.repoBootstrapStage, m.updatingBinary, m.runningPull)
}

func diagViewBegin() uint64 {
	if !diagKeysEnabled {
		return 0
	}
	seq := diagFrameSeq.Add(1)
	diag.StartupLog("view #%d begin", seq)
	return seq
}

func diagViewEnd(seq uint64) {
	if !diagKeysEnabled {
		return
	}
	diag.StartupLog("view #%d end", seq)
}
