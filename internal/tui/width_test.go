package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/shichao402/Dec/pkg/app"
)

// widthBaselines are the responsive width reference columns we guarantee.
// These map to 风险 3（中文宽字符与终端宽度导致布局错位）的回归守护基线。
var widthBaselines = []int{60, 80, 100, 140}

// assertNoLineOverflowsWidth 断言渲染结果中每一行的显示宽度都 <= expected。
// 为了避开 lipgloss 向 status bar / cards 写入的 ANSI 背景填充（会在尾部
// 生成看似多余的空白），这里只检查 lipgloss.Width，它已经内部使用
// runewidth 正确度量中文宽字符与 emoji。
func assertNoLineOverflowsWidth(t *testing.T, label string, view string, expected int) {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		// 使用 lipgloss.Width 一致地度量，保持「统一走 lipgloss」的约束（docs 风险 3）。
		got := lipgloss.Width(line)
		if got > expected {
			t.Fatalf(
				"%s: 第 %d 行宽度 %d 超过期望 %d\n行内容: %q",
				label, i+1, got, expected, line,
			)
		}
	}
}

func homeModelAtWidth(width int) model {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.width = width
	m.height = 36
	m.overview = &app.ProjectOverview{
		ProjectRoot:        "/tmp/dec-project",
		RepoConnected:      true,
		RepoRemoteURL:      "git@github.com:demo/dec.git",
		ProjectConfigPath:  "/tmp/dec-project/.dec/config.yaml",
		ProjectConfigReady: true,
		VarsPath:           "/tmp/dec-project/.dec/vars.yaml",
		VarsFileReady:      true,
		AvailableCount:     5,
		EnabledCount:       2,
		IDEs:               []string{"codex", "cursor"},
		Editor:             "code --wait",
	}
	return m
}

// 针对 home/assets/run/settings 四个主要页面，在 60/80/100/140 列下
// 断言渲染结果逐行宽度不超过终端宽度。
func TestViewAtBaselineWidths_HomeNoOverflow(t *testing.T) {
	for _, width := range widthBaselines {
		width := width
		t.Run(widthLabel(width), func(t *testing.T) {
			m := homeModelAtWidth(width)
			view := m.View()
			assertNoLineOverflowsWidth(t, "Home", view, width)
		})
	}
}

func TestViewAtBaselineWidths_AssetsNoOverflow(t *testing.T) {
	// 含中文 vault / name 的资产条目，验证宽字符不会撑爆列宽。
	assets := &app.AssetSelectionState{
		ExistingConfig: true,
		ConfigPath:     "/tmp/dec-project/.dec/config.yaml",
		VarsPath:       "/tmp/dec-project/.dec/vars.yaml",
		Items: []app.AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
			{Name: "cli-release-rules", Type: "rule", Vault: "cli", Enabled: false},
			{Name: "中文名称资产", Type: "skill", Vault: "中文仓库", Enabled: true},
		},
	}

	for _, width := range widthBaselines {
		width := width
		t.Run(widthLabel(width), func(t *testing.T) {
			m := homeModelAtWidth(width)
			m.pageIndex = 1 // Assets
			m.assets = assets
			m.normalizeAssetCursor()
			view := m.View()
			assertNoLineOverflowsWidth(t, "Assets", view, width)
		})
	}
}

func TestViewAtBaselineWidths_RunNoOverflow(t *testing.T) {
	for _, width := range widthBaselines {
		width := width
		t.Run(widthLabel(width), func(t *testing.T) {
			m := homeModelAtWidth(width)
			m.pageIndex = 3 // Run
			m.runProgress = &app.Progress{Phase: "pull", Current: 1, Total: 2}
			m.runResult = &app.PullProjectAssetsResult{
				RequestedCount: 2,
				PulledCount:    1,
				FailedCount:    1,
				EffectiveIDEs:  []string{"cursor"},
				VersionCommit:  "abc123",
			}
			m.runEvents = []string{"开始拉取资产", "完成汇总"}
			view := m.View()
			assertNoLineOverflowsWidth(t, "Run", view, width)
		})
	}
}

func TestViewAtBaselineWidths_SettingsNoOverflow(t *testing.T) {
	for _, width := range widthBaselines {
		width := width
		t.Run(widthLabel(width), func(t *testing.T) {
			m := homeModelAtWidth(width)
			m.pageIndex = 4 // Settings
			m.settings = &app.GlobalSettingsState{
				ConfigPath:       "/tmp/.dec/config.yaml",
				VarsPath:         "/tmp/.dec/local/vars.yaml",
				RepoConnected:    true,
				RepoURL:          "git@github.com:demo/dec.git",
				ConnectedRepoURL: "git@github.com:demo/dec.git",
				AvailableIDEs:    []string{"codex", "cursor"},
				SelectedIDEs:     []string{"cursor"},
				EffectiveIDEs:    []string{"cursor"},
			}
			m.settingsRepoInput = m.settings.RepoURL
			m.settingsSelectedIDEs = []string{"cursor"}
			m.normalizeSettingsCursor()
			view := m.View()
			assertNoLineOverflowsWidth(t, "Settings", view, width)
		})
	}
}

// TestStatusBarDropsLeftHintOnOverflow 验证状态栏在 left+right 已超出宽度时，
// 会丢掉左侧快捷键提示以保留右侧承载的页面状态，而不是静默截断。
func TestStatusBarDropsLeftHintOnOverflow(t *testing.T) {
	m := homeModelAtWidth(60)
	m.pageIndex = 3 // Run：右侧会携带 pull 阶段状态
	m.runProgress = &app.Progress{Phase: "pull", Current: 1, Total: 2}

	bar := m.renderStatusBar(60)
	// 右侧带 "pull 1/2" 的状态信息必须保留
	if !strings.Contains(bar, "pull 1/2") {
		t.Fatalf("窄终端下状态栏应保留右侧页面状态，实际：%q", bar)
	}
	// 宽度不能超过 60
	if w := lipgloss.Width(bar); w > 60 {
		t.Fatalf("状态栏渲染宽度 %d 超过期望 60，内容：%q", w, bar)
	}
}

// TestStatusBarKeepsBothSidesWhenFits 验证常规宽度下左右两侧都保留。
func TestStatusBarKeepsBothSidesWhenFits(t *testing.T) {
	m := homeModelAtWidth(120)
	bar := m.renderStatusBar(120)
	if !strings.Contains(bar, "q quit") {
		t.Fatalf("常规宽度下状态栏左侧快捷键提示应保留：%q", bar)
	}
	if !strings.Contains(bar, "page Home") {
		t.Fatalf("常规宽度下状态栏右侧页面状态应保留：%q", bar)
	}
}

func widthLabel(width int) string {
	return "width_" + itoa(width)
}

func itoa(n int) string {
	// 避免引入 strconv 仅为单测格式化。
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
