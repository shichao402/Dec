package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/shichao402/Dec/pkg/app"
)

// updateSnapshots 通过 `-update` flag 重建 testdata 下的 golden 文件。
// 用法：
//
//	go test ./internal/tui/ -run TestSnapshot -update
var updateSnapshots = flag.Bool("update", false, "regenerate TUI golden snapshots")

// snapshotWidths 是需要守护的渲染宽度基线，与 width_test.go 的 widthBaselines
// 取子集，避开 60 列（文案在极窄宽度下依赖动态裁剪，不适合做内容快照）。
var snapshotWidths = []int{80, 100, 140}

// snapshotHeight 是一个足够高的值，保证 View 能渲染出完整内容，不被高度裁剪。
const snapshotHeight = 40

func init() {
	// 锁定为 Ascii profile，确保 snapshot 不受当前终端颜色能力影响。
	// 这是 lipgloss 推荐的测试做法，对 width_test.go 等基于
	// lipgloss.Width 的测试无副作用。
	lipgloss.SetColorProfile(termenv.Ascii)
}

// sanitizeView 规范化渲染结果，使 golden 文件稳定：
//   - 剥除行尾空白（lipgloss 会用空格做背景填充）
//   - 剥除尾部空行（不同页面收尾方式不同）
//   - 统一换行符
func sanitizeView(view string) string {
	lines := strings.Split(strings.ReplaceAll(view, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	// 去掉末尾空行
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n") + "\n"
}

// assertSnapshot 对比 view 与 testdata/snapshots/<name>.txt，不一致则 fail。
// 指定 `-update` 时则写回 golden 文件。
func assertSnapshot(t *testing.T, name, view string) {
	t.Helper()
	got := sanitizeView(view)

	path := filepath.Join("testdata", "snapshots", name+".txt")
	if *updateSnapshots {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("创建 snapshot 目录失败: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("写 snapshot 失败: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 snapshot %s 失败: %v\n提示：首次运行请用 `go test ./internal/tui/ -run TestSnapshot -update` 生成 golden 文件", path, err)
	}
	if string(want) != got {
		t.Fatalf("snapshot %s 不匹配。\n要更新 golden 文件请运行：\n  go test ./internal/tui/ -run TestSnapshot -update\n\n--- got ---\n%s\n--- want ---\n%s", name, got, string(want))
	}
}

// snapshotHomeModel 构造一个与 width_test.homeModelAtWidth 对齐的 Home 状态。
func snapshotHomeModel(width int) model {
	m := newModel("/tmp/dec-project", "v1.0.0")
	m.width = width
	m.height = snapshotHeight
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

func snapshotAssetsModel(width int) model {
	m := snapshotHomeModel(width)
	m.pageIndex = 1
	m.assets = &app.AssetSelectionState{
		ExistingConfig: true,
		ConfigPath:     "/tmp/dec-project/.dec/config.yaml",
		VarsPath:       "/tmp/dec-project/.dec/vars.yaml",
		Items: []app.AssetSelectionItem{
			{Name: "project-workflow", Type: "skill", Vault: "default", Enabled: true},
			{Name: "cli-release-rules", Type: "rule", Vault: "cli", Enabled: false},
		},
	}
	m.normalizeAssetCursor()
	return m
}

func snapshotRunModel(width int) model {
	m := snapshotHomeModel(width)
	m.pageIndex = 3
	m.runProgress = &app.Progress{Phase: "pull", Current: 1, Total: 2}
	m.runResult = &app.PullProjectAssetsResult{
		RequestedCount: 2,
		PulledCount:    1,
		FailedCount:    1,
		EffectiveIDEs:  []string{"cursor"},
		VersionCommit:  "abc123",
	}
	m.runEvents = []string{"开始拉取", "完成汇总"}
	return m
}

func snapshotSettingsModel(width int) model {
	m := snapshotHomeModel(width)
	m.pageIndex = 4
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
	return m
}

// TestSnapshotHome 固定 Home 页在基线宽度下的完整渲染。
func TestSnapshotHome(t *testing.T) {
	for _, w := range snapshotWidths {
		w := w
		t.Run(widthLabel(w), func(t *testing.T) {
			m := snapshotHomeModel(w)
			assertSnapshot(t, "home_"+widthLabel(w), m.View())
		})
	}
}

// TestSnapshotAssets 固定 Assets 页在基线宽度下的完整渲染。
func TestSnapshotAssets(t *testing.T) {
	for _, w := range snapshotWidths {
		w := w
		t.Run(widthLabel(w), func(t *testing.T) {
			m := snapshotAssetsModel(w)
			assertSnapshot(t, "assets_"+widthLabel(w), m.View())
		})
	}
}

// TestSnapshotRun 固定 Run 页在基线宽度下的完整渲染。
func TestSnapshotRun(t *testing.T) {
	for _, w := range snapshotWidths {
		w := w
		t.Run(widthLabel(w), func(t *testing.T) {
			m := snapshotRunModel(w)
			assertSnapshot(t, "run_"+widthLabel(w), m.View())
		})
	}
}

// TestSnapshotSettings 固定 Settings 页在基线宽度下的完整渲染。
func TestSnapshotSettings(t *testing.T) {
	for _, w := range snapshotWidths {
		w := w
		t.Run(widthLabel(w), func(t *testing.T) {
			m := snapshotSettingsModel(w)
			assertSnapshot(t, "settings_"+widthLabel(w), m.View())
		})
	}
}
