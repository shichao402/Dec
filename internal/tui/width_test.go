package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/shichao402/Dec/internal/app"
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
		// 使用 lipgloss.Width 一致地度量，保持「统一走 lipgloss」的约束（TUI_ARCHITECTURE 风险 3）。
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
		ProjectRoot:          "/tmp/dec-project",
		RepoConnected:        true,
		RepoRemoteURL:        "git@github.com:demo/dec.git",
		ProjectConfigPath:    "/tmp/dec-project/.dec/config.yaml",
		ProjectConfigReady:   true,
		VarsPath:             "/tmp/dec-project/.dec/vars.yaml",
		VarsFileReady:        true,
		AvailableBundleCount: 5,
		EnabledBundleCount:   2,
		IDEs:                 []string{"codex", "cursor"},
		Editor:               "code --wait",
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
	// 含中文 vault / name 的成员条目，验证宽字符不会撑爆列宽。
	assets := &app.AssetSelectionState{
		ExistingConfig: true,
		ConfigPath:     "/tmp/dec-project/.dec/config.yaml",
		VarsPath:       "/tmp/dec-project/.dec/vars.yaml",
		Bundles: []app.AssetBundleOption{
			{
				Name:        "default",
				Vault:       "default",
				Description: "default 资产包",
				Enabled:     true,
				Members: []app.AssetSelectionItem{
					{Name: "project-workflow", Type: "skill", Vault: "default"},
					{Name: "cli-release-rules", Type: "rule", Vault: "cli"},
				},
			},
			{
				Name:        "中文包",
				Vault:       "中文仓库",
				Description: "含宽字符的 bundle",
				Members: []app.AssetSelectionItem{
					{Name: "中文名称资产", Type: "skill", Vault: "中文仓库"},
				},
			},
		},
	}

	for _, width := range widthBaselines {
		width := width
		t.Run(widthLabel(width), func(t *testing.T) {
			m := homeModelAtWidth(width)
			m.pageIndex = 1 // Bundles
			m.assets = assets
			m.normalizeAssetCursor()
			view := m.View()
			assertNoLineOverflowsWidth(t, "Bundles", view, width)
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
			m.pageIndex = 5 // Settings
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

func TestViewAtBaselineWidths_ProjectNoOverflow(t *testing.T) {
	for _, width := range widthBaselines {
		width := width
		t.Run(widthLabel(width), func(t *testing.T) {
			m := homeModelAtWidth(width)
			m.pageIndex = 2
			m.focus = focusContent
			m.projectSettings = &app.ProjectSettingsState{
				ProjectRoot:        "/tmp/dec-project",
				ConfigPath:         "/tmp/dec-project/.dec/config.yaml",
				VarsPath:           "/tmp/dec-project/.dec/vars.yaml",
				ProjectConfigReady: true,
				AvailableIDEs:      []string{"codex", "cursor", "claude"},
				GlobalIDEs:         []string{"cursor"},
				EffectiveIDEs:      []string{"cursor"},
			}
			m.projectVars = &app.ProjectVarsView{
				VarsPath:      "/tmp/dec-project/.dec/vars.yaml",
				VarsFileReady: true,
				EditorCommand: "code --wait",
				CacheExists:   true,
				UsedPlaceholders: []string{
					"API_TOKEN", "DB_HOST", "DB_PASSWORD", "SSH_KEY",
					"LONG_PATH_VALUE_THAT_SHOULD_TRUNCATE_NICELY",
				},
				ResolvedVars: map[string]app.PlaceholderStatus{
					"API_TOKEN":   {Source: app.PlaceholderSourceProject, Value: "secret"},
					"DB_HOST":     {Source: app.PlaceholderSourceGlobal, Value: "localhost"},
					"DB_PASSWORD": {},
					"SSH_KEY":     {Source: app.PlaceholderSourceProject, Value: strings.Repeat("x", 80)},
					"LONG_PATH_VALUE_THAT_SHOULD_TRUNCATE_NICELY": {Source: app.PlaceholderSourceProject, Value: "/very/long/path/" + strings.Repeat("seg/", 20)},
				},
			}
			m.normalizeProjectSettingsCursor()
			view := m.View()
			assertNoLineOverflowsWidth(t, "Project", view, width)
			assertNoViewExceedsHeight(t, "Project", view, m.height)
		})
	}
}

func TestViewAtBaselineHeights_NoVerticalOverflow(t *testing.T) {
	pages := []struct {
		name  string
		build func(width int) model
	}{
		{"Home", homeModelAtWidth},
		{"Bundles", func(width int) model {
			m := homeModelAtWidth(width)
			m.pageIndex = 1
			m.assets = &app.AssetSelectionState{
				ExistingConfig: true,
				Bundles: []app.AssetBundleOption{
					{Name: "default", Vault: "default", Enabled: true, Members: []app.AssetSelectionItem{{Name: "a", Type: "skill", Vault: "default"}}},
					{Name: "cli", Vault: "cli", Members: []app.AssetSelectionItem{{Name: "b", Type: "rule", Vault: "cli"}}},
				},
			}
			m.bundleSelection = []string{"default"}
			m.normalizeAssetCursor()
			return m
		}},
		{"Project", func(width int) model {
			m := homeModelAtWidth(width)
			m.pageIndex = 2
			m.projectSettings = &app.ProjectSettingsState{
				ProjectRoot: "/tmp/dec-project", ConfigPath: "/tmp/dec-project/.dec/config.yaml",
				VarsPath: "/tmp/dec-project/.dec/vars.yaml", ProjectConfigReady: true,
				AvailableIDEs: []string{"codex", "cursor"}, GlobalIDEs: []string{"cursor"}, EffectiveIDEs: []string{"cursor"},
			}
			m.projectVars = &app.ProjectVarsView{VarsPath: "/tmp/dec-project/.dec/vars.yaml", VarsFileReady: true}
			m.normalizeProjectSettingsCursor()
			return m
		}},
		{"Run", func(width int) model {
			m := homeModelAtWidth(width)
			m.pageIndex = 3
			m.runResult = &app.PullProjectAssetsResult{RequestedCount: 2, PulledCount: 1, FailedCount: 1, EffectiveIDEs: []string{"cursor"}}
			m.runEvents = []string{"a", "b", "c", "d", "e", "f", "g", "h"}
			return m
		}},
		{"Settings", func(width int) model {
			m := homeModelAtWidth(width)
			m.pageIndex = 5
			m.settings = &app.GlobalSettingsState{
				ConfigPath: "/tmp/.dec/config.yaml", VarsPath: "/tmp/.dec/local/vars.yaml",
				RepoConnected: true, RepoURL: "git@github.com:demo/dec.git", ConnectedRepoURL: "git@github.com:demo/dec.git",
				AvailableIDEs: []string{"codex", "cursor"}, SelectedIDEs: []string{"cursor"}, EffectiveIDEs: []string{"cursor"},
			}
			m.settingsRepoInput = m.settings.RepoURL
			m.settingsSelectedIDEs = []string{"cursor"}
			m.normalizeSettingsCursor()
			return m
		}},
	}
	for _, page := range pages {
		page := page
		t.Run(page.name, func(t *testing.T) {
			for _, width := range []int{80, 100, 140} {
				width := width
				t.Run(widthLabel(width), func(t *testing.T) {
					m := page.build(width)
					view := m.View()
					assertNoViewExceedsHeight(t, page.name, view, m.height)
					assertNoLineOverflowsWidth(t, page.name, view, width)
				})
			}
		})
	}
}

func assertNoViewExceedsHeight(t *testing.T, label string, view string, expected int) {
	t.Helper()
	got := lipgloss.Height(view)
	if got > expected {
		t.Fatalf("%s: 视图高度 %d 超过期望 %d\n%s", label, got, expected, view)
	}
}

// TestStatusBarDropsLeftHintOnOverflow 验证状态栏在 left+right 已超出宽度时，
// 会丢掉左侧快捷键提示以保留右侧承载的页面状态，而不是静默截断。
func TestStatusBarDropsLeftHintOnOverflow(t *testing.T) {
	m := homeModelAtWidth(60)
	m.pageIndex = 3 // Run：右侧会携带 pull 阶段状态
	m.runningPull = true
	m.runProgress = &app.Progress{Phase: "pull", Current: 1, Total: 2}

	bar := m.renderStatusBar(60)
	// 右侧带 "Dec cache 1/2" 的状态信息必须保留
	if !strings.Contains(bar, "Dec cache 1/2") {
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
