package tui

import (
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/app"
)

func TestIOBusyLabelPriority(t *testing.T) {
	m := model{
		pages:     []string{"项目", "引入", "同步", "设置"},
		pageIndex: 0,
		overview:  &app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true},
	}
	if got := m.ioBusyLabel(); got != "" {
		t.Fatalf("idle ioBusyLabel = %q, want empty", got)
	}

	m.shellRefresh.loading = true
	if got := m.ioBusyLabel(); got != "Refreshing…" {
		t.Fatalf("refresh ioBusyLabel = %q", got)
	}

	m.deleteLoad.loading = true
	if got := m.ioBusyLabel(); got != "Loading remote list…" {
		t.Fatalf("delete load ioBusyLabel = %q", got)
	}

	m.deleteCandidates = []app.DeleteCandidate{{Label: "x"}}
	if got := m.ioBusyLabel(); got != "Refreshing remote list…" {
		t.Fatalf("delete refresh ioBusyLabel = %q", got)
	}

	m.runningPull = true
	m.runMode = "pull"
	if got := m.ioBusyLabel(); got != "Pull running… Esc cancel" {
		t.Fatalf("pull ioBusyLabel = %q", got)
	}
}

func TestStatusBarShowsBusyAndDeleteHints(t *testing.T) {
	m := model{
		pages:     []string{"项目", "引入", "同步", "设置"},
		remoteOpen: true,
		pageIndex: 0,
		overview:  &app.ProjectOverview{RepoConnected: true, ProjectConfigReady: true, EnabledBundleCount: 1},
		width:     120,
	}

	idle := m.renderStatusBar(120)
	if !strings.Contains(idle, "d delete") {
		t.Fatalf("Remote 空闲状态栏应提示 d delete：%q", idle)
	}

	m.deleteLoad.loading = true
	m.deleteCandidates = []app.DeleteCandidate{{Label: "x"}}
	busy := m.renderStatusBar(120)
	if !strings.Contains(busy, "Refreshing remote list") {
		t.Fatalf("Remote 刷新时状态栏应显示 busy：%q", busy)
	}
	if summary := m.currentSummary(); summary != "Refreshing remote list…" {
		t.Fatalf("currentSummary during remote refresh = %q", summary)
	}
}

func TestShellRefreshBatchAccounting(t *testing.T) {
	m := model{pages: []string{"项目"}, pageIndex: 0}
	_ = m.refreshCmd()
	if !m.shellRefresh.busy() || m.shellRefresh.pending != refreshPartCount {
		t.Fatalf("refresh busy=%v pending=%d, want busy pending=%d", m.shellRefresh.busy(), m.shellRefresh.pending, refreshPartCount)
	}
	gen := m.shellRefresh.gen
	for i := 0; i < refreshPartCount; i++ {
		if !m.shellRefresh.acceptPart(gen) {
			t.Fatalf("acceptPart #%d failed", i+1)
		}
	}
	if m.shellRefresh.busy() || m.shellRefresh.pending != 0 {
		t.Fatalf("after %d parts busy=%v pending=%d", refreshPartCount, m.shellRefresh.busy(), m.shellRefresh.pending)
	}
	if m.shellRefresh.acceptPart(gen - 1) {
		t.Fatal("过期 gen 不应 accept")
	}
}
