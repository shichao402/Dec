package update

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestManualInstallCommand(t *testing.T) {
	linuxCmd := manualInstallCommand("linux", false)
	if linuxCmd != "curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.sh | bash" {
		t.Fatalf("linux 安装命令错误: %s", linuxCmd)
	}

	windowsCmd := manualInstallCommand("windows", false)
	if windowsCmd != "iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.ps1 | iex" {
		t.Fatalf("windows 安装命令错误: %s", windowsCmd)
	}
}

func TestMirrorInstallCommandUsesCDN(t *testing.T) {
	linuxCmd := manualInstallCommand("linux", true)
	if linuxCmd != "curl -fsSL https://cdn.jsdelivr.net/gh/shichao402/Dec@ReleaseLatest/scripts/install.sh | bash" {
		t.Fatalf("linux 镜像安装命令错误: %s", linuxCmd)
	}

	windowsCmd := manualInstallCommand("windows", true)
	if windowsCmd != "iwr -useb https://cdn.jsdelivr.net/gh/shichao402/Dec@ReleaseLatest/scripts/install.ps1 | iex" {
		t.Fatalf("windows 镜像安装命令错误: %s", windowsCmd)
	}
}

func TestNetworkHelpMentionsMirrorAndProxy(t *testing.T) {
	help := NetworkHelp()
	for _, want := range []string{"cdn.jsdelivr.net", "raw.githubusercontent.com", "HTTPS_PROXY"} {
		if !strings.Contains(help, want) {
			t.Fatalf("排障建议缺少 %q:\n%s", want, help)
		}
	}
}

func TestVersionSourcesCoverMirrorAndAPI(t *testing.T) {
	sources := versionSources()
	if len(sources) < 3 {
		t.Fatalf("版本来源数量 = %d, 期望至少 3 个", len(sources))
	}

	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.name)
	}
	want := []string{"raw.githubusercontent.com", "cdn.jsdelivr.net", "api.github.com"}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("第 %d 个来源 = %q, 期望 %q（顺序: %v）", i, names[i], name, names)
		}
	}
}

func TestParseVersionJSON(t *testing.T) {
	got, err := parseVersionJSON([]byte(`{"version":"v1.2.3"}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got != "v1.2.3" {
		t.Fatalf("version = %q, 期望 v1.2.3", got)
	}

	if _, err := parseVersionJSON([]byte("not json")); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

func TestParseReleaseTag(t *testing.T) {
	got, err := parseReleaseTag([]byte(`{"tag_name":"v9.9.9","name":"release"}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got != "v9.9.9" {
		t.Fatalf("tag = %q, 期望 v9.9.9", got)
	}
}

func TestFetchVersionFromRejectsBlankVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"  "}`)
	}))
	defer server.Close()

	_, err := fetchVersionFrom(versionSource{name: "test", url: server.URL, parse: parseVersionJSON})
	if err == nil || !strings.Contains(err.Error(), "远程版本号为空") {
		t.Fatalf("空版本号应报错, 实际 err = %v", err)
	}
}

func TestFetchVersionFromReportsHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := fetchVersionFrom(versionSource{name: "test", url: server.URL, parse: parseVersionJSON})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("404 应报错, 实际 err = %v", err)
	}
}

func TestFetchFromSourcesFallsBackToNextSource(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v2.0.0"}`)
	}))
	defer healthy.Close()

	latest, err := fetchFromSources([]versionSource{
		{name: "failing", url: failing.URL, parse: parseVersionJSON},
		{name: "healthy", url: healthy.URL, parse: parseReleaseTag},
	})
	if err != nil {
		t.Fatalf("应回退到可用来源, err = %v", err)
	}
	if latest != "v2.0.0" {
		t.Fatalf("version = %q, 期望 v2.0.0", latest)
	}
}

func TestFetchFromSourcesAggregatesFailures(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	_, err := fetchFromSources([]versionSource{
		{name: "first", url: failing.URL, parse: parseVersionJSON},
		{name: "second", url: failing.URL, parse: parseVersionJSON},
	})
	if err == nil {
		t.Fatal("全部来源失败时应返回错误")
	}
	for _, want := range []string{"已尝试 2 个来源", "first", "second", "HTTP 500"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("聚合错误缺少 %q:\n%v", want, err)
		}
	}
}

func TestDescribeRequestErrorStripsURL(t *testing.T) {
	timeoutErr := &url.Error{
		Op:  "Get",
		URL: "https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/version.json",
		Err: timeoutError{},
	}
	got := describeRequestError(timeoutErr)
	if !strings.Contains(got, "请求超时") {
		t.Fatalf("超时描述 = %q, 期望包含 请求超时", got)
	}
	if strings.Contains(got, "raw.githubusercontent.com") {
		t.Fatalf("超时描述不应重复 URL: %q", got)
	}

	plainErr := &url.Error{Op: "Get", URL: "https://example.com", Err: errors.New("connection reset by peer")}
	if got := describeRequestError(plainErr); got != "connection reset by peer" {
		t.Fatalf("非超时描述 = %q", got)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "context deadline exceeded" }
func (timeoutError) Timeout() bool { return true }

func TestShouldCheckBacksOffAfterFailure(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	if !ShouldCheck() {
		t.Fatal("无状态文件时应执行检查")
	}

	now := time.Now()
	mustSaveState(t, &CheckState{LastCheck: now, LatestVersion: "v1.0.0", LastAttempt: now})
	if ShouldCheck() {
		t.Fatal("距上次成功检查不足 24 小时，不应再检查")
	}

	stale := now.Add(-48 * time.Hour)
	mustSaveState(t, &CheckState{LastCheck: stale, LatestVersion: "v1.0.0", LastAttempt: now.Add(-10 * time.Minute)})
	if ShouldCheck() {
		t.Fatal("最近一次检查失败，应按 retryInterval 退避")
	}

	mustSaveState(t, &CheckState{LastCheck: stale, LatestVersion: "v1.0.0", LastAttempt: now.Add(-2 * time.Hour)})
	if !ShouldCheck() {
		t.Fatal("超过退避间隔后应重新检查")
	}
}

func TestRecordFailedAttemptKeepsCachedVersion(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())

	lastCheck := time.Now().Add(-48 * time.Hour)
	mustSaveState(t, &CheckState{LastCheck: lastCheck, LatestVersion: "v1.5.0"})

	recordFailedAttempt()

	state, err := loadState()
	if err != nil {
		t.Fatalf("读取状态失败: %v", err)
	}
	if state.LatestVersion != "v1.5.0" {
		t.Fatalf("失败不应清掉缓存版本, LatestVersion = %q", state.LatestVersion)
	}
	if !state.LastCheck.Equal(lastCheck) {
		t.Fatalf("失败不应刷新 LastCheck, 实际 %v", state.LastCheck)
	}
	if state.LastAttempt.IsZero() {
		t.Fatal("失败后应记录 LastAttempt")
	}
}

// CheckBackground 在 TUI 启动路径上被调用，必须立刻从缓存返回，
// 网络请求只能发生在后台，否则网络不通时启动会卡住。
func TestCheckBackgroundReturnsFromCacheWithoutWaitingForRefresh(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	mustSaveState(t, &CheckState{
		LastCheck:     time.Now().Add(-48 * time.Hour),
		LatestVersion: "v2.0.0",
	})

	started := make(chan struct{})
	finished := make(chan struct{})
	old := refreshStateFn
	defer func() {
		<-started
		<-finished
		refreshStateFn = old
	}()
	refreshStateFn = func() {
		close(started)
		time.Sleep(300 * time.Millisecond)
		close(finished)
	}

	begin := time.Now()
	result := CheckBackground("v1.0.0")
	elapsed := time.Since(begin)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("CheckBackground 耗时 %v, 不应等待后台刷新", elapsed)
	}
	if result == nil || result.LatestVersion != "v2.0.0" {
		t.Fatalf("result = %#v, 期望使用缓存的 v2.0.0", result)
	}
}

func TestCheckBackgroundReturnsNilWhenCacheIsCurrent(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	mustSaveState(t, &CheckState{
		LastCheck:     time.Now(),
		LatestVersion: "v1.0.0",
		LastAttempt:   time.Now(),
	})

	if result := CheckBackground("v1.0.0"); result != nil {
		t.Fatalf("已是最新版本时应返回 nil, 实际 %#v", result)
	}
}

func mustSaveState(t *testing.T, state *CheckState) {
	t.Helper()
	if err := saveState(state); err != nil {
		t.Fatalf("写入状态失败: %v", err)
	}
}
