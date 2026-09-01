package app

import (
	"strings"
	"testing"
)

func TestRemoteTargetDestination(t *testing.T) {
	cases := []struct {
		name   string
		target RemoteTarget
		want   string
	}{
		{"别名优先", RemoteTarget{Alias: "build-box", Host: "1.2.3.4", User: "root"}, "build-box"},
		{"用户加主机", RemoteTarget{Host: "1.2.3.4", User: "dev"}, "dev@1.2.3.4"},
		{"仅主机", RemoteTarget{Host: "1.2.3.4"}, "1.2.3.4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.target.SSHDestination(); got != tc.want {
				t.Fatalf("目标 = %q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestRemoteTargetValidate(t *testing.T) {
	if err := (RemoteTarget{}).validate(); err == nil {
		t.Fatal("空目标应报错")
	}
	if err := (RemoteTarget{Host: "h", Port: 70000}).validate(); err == nil {
		t.Fatal("非法端口应报错")
	}
	if err := (RemoteTarget{Alias: "box"}).validate(); err != nil {
		t.Fatalf("仅别名应合法: %v", err)
	}
}

// 未装 Dec 的干净 Linux 机：可置备，无阻断。
func TestProbeCleanLinuxHostIsProvisionable(t *testing.T) {
	probe := parseProbe(t, strings.Join([]string{
		"os=Linux",
		"arch=x86_64",
		"cmd=git",
		"cmd=ssh-keygen",
		"cmd=curl",
		"cmd=bash",
		"home_writable=1",
		"spawn=both",
	}, "\n"))

	if !probe.Supported {
		t.Fatal("Linux 应受支持")
	}
	if len(probe.Blockers) != 0 {
		t.Fatalf("不应有阻断项: %v", probe.Blockers)
	}
	if probe.DecInstalled {
		t.Fatal("四件套缺失时 DecInstalled 应为 false")
	}
	if len(probe.MissingBinaries) != 4 {
		t.Fatalf("应缺 4 个二进制，实际 %v", probe.MissingBinaries)
	}
	if !strings.Contains(probe.NextAction, "可以置备") {
		t.Fatalf("建议动作应为可置备，实际 %q", probe.NextAction)
	}
}

// Windows 目标机必须在探测阶段就明确拒绝，而不是尝试后失败。
func TestProbeWindowsTargetIsRejectedUpfront(t *testing.T) {
	probe := parseProbe(t, "os=MINGW64_NT-10.0\narch=x86_64\ncmd=git\n")

	if probe.Supported {
		t.Fatal("Windows 远端第一版不支持")
	}
	if !hasSubstring(probe.Blockers, "Windows") {
		t.Fatalf("应给出 Windows 阻断项: %v", probe.Blockers)
	}
	if !strings.Contains(probe.NextAction, "Linux / macOS") {
		t.Fatalf("应建议改用受支持系统，实际 %q", probe.NextAction)
	}
}

// 没有 systemd / launchd 不再阻断：远端不做常驻，靠 SSH 按需拉起（ADR 0019 改写后）。
func TestProbeMissingServiceManagerDoesNotBlock(t *testing.T) {
	probe := parseProbe(t, strings.Join([]string{
		"os=Linux",
		"arch=x86_64",
		"cmd=git",
		"cmd=ssh-keygen",
		"cmd=curl",
		"cmd=bash",
		"home_writable=1",
		"spawn=nohup",
	}, "\n"))

	if len(probe.Blockers) != 0 {
		t.Fatalf("缺少常驻方式不应再阻断: %v", probe.Blockers)
	}
	if hasSubstring(probe.Warnings, "linger") {
		t.Fatalf("不应再产生 linger 警告: %v", probe.Warnings)
	}
	if !probe.SpawnCapable {
		t.Fatal("有 nohup 即具备按需拉起能力")
	}
}

// 无法拉起后台进程才是真正的阻断项：按需拉起模型的前提不成立。
func TestProbeMissingSpawnCapabilityBlocks(t *testing.T) {
	probe := parseProbe(t, strings.Join([]string{
		"os=Linux",
		"arch=x86_64",
		"cmd=git",
		"cmd=ssh-keygen",
		"cmd=curl",
		"cmd=bash",
		"home_writable=1",
		"spawn=none",
	}, "\n"))

	if probe.SpawnCapable {
		t.Fatal("spawn=none 时不应具备拉起能力")
	}
	if !hasSubstring(probe.Blockers, "按需拉起") {
		t.Fatalf("应阻断并说明按需拉起前提: %v", probe.Blockers)
	}
}

// 远端服务当前未运行是正常状态，不应被当成问题。
func TestProbeServerNotRunningIsNotABlocker(t *testing.T) {
	probe := parseProbe(t, strings.Join([]string{
		"os=Linux",
		"arch=x86_64",
		"binary=dec",
		"binary=dec-server",
		"binary=dec-mcp",
		"binary=dec-exec",
		"cmd=git",
		"cmd=ssh-keygen",
		"cmd=curl",
		"cmd=bash",
		"home_writable=1",
		"spawn=both",
		"listen=" + RemoteProvisionListen,
	}, "\n"))

	if probe.ServerRunning {
		t.Fatal("未输出 server_running 时应为 false")
	}
	if len(probe.Blockers) != 0 {
		t.Fatalf("服务未运行不应阻断: %v", probe.Blockers)
	}
	if !strings.Contains(probe.NextAction, "按需拉起") {
		t.Fatalf("建议动作应说明会按需拉起，实际 %q", probe.NextAction)
	}
}

func TestProbeMissingCurlAndUnwritableHomeBlock(t *testing.T) {
	probe := parseProbe(t, "os=Linux\narch=x86_64\nspawn=both\n")

	if !hasSubstring(probe.Blockers, "curl") {
		t.Fatalf("缺 curl 应阻断: %v", probe.Blockers)
	}
	if !hasSubstring(probe.Blockers, "不可写") {
		t.Fatalf("~/.dec 不可写应阻断: %v", probe.Blockers)
	}
	if !hasSubstring(probe.Blockers, "git") {
		t.Fatalf("缺 git 应阻断: %v", probe.Blockers)
	}
	if !strings.Contains(probe.NextAction, "阻断") {
		t.Fatalf("建议动作应指向解决阻断，实际 %q", probe.NextAction)
	}
}

// install.sh 是 #!/bin/bash 且用了数组与 <<<，目标机没有 bash 就注入不了。
func TestProbeMissingBashBlocks(t *testing.T) {
	probe := parseProbe(t, strings.Join([]string{
		"os=Linux",
		"arch=x86_64",
		"cmd=git",
		"cmd=ssh-keygen",
		"cmd=curl",
		"home_writable=1",
		"spawn=both",
	}, "\n"))

	if probe.HasBash {
		t.Fatal("未输出 cmd=bash 时应为 false")
	}
	if !hasSubstring(probe.Blockers, "bash") {
		t.Fatalf("缺 bash 应阻断: %v", probe.Blockers)
	}
}

// 已完整置备的机器：无阻断、无需再装。
func TestProbeFullyProvisionedHost(t *testing.T) {
	probe := parseProbe(t, strings.Join([]string{
		"os=Darwin",
		"arch=arm64",
		"binary=dec",
		"binary=dec-server",
		"binary=dec-mcp",
		"binary=dec-exec",
		"dec_version=dec version v1.4.2",
		"cmd=git",
		"cmd=ssh-keygen",
		"cmd=curl",
		"cmd=bash",
		"home_writable=1",
		"spawn=both",
		"listen=" + RemoteProvisionListen,
		"server_running=1",
	}, "\n"))

	if !probe.DecInstalled {
		t.Fatalf("四件套齐全应为已安装，缺失 %v", probe.MissingBinaries)
	}
	if probe.DecVersion != "v1.4.2" {
		t.Fatalf("版本解析错误: %q", probe.DecVersion)
	}
	if !probe.ListenReady {
		t.Fatalf("监听地址应就绪: %q", probe.ManagementListen)
	}
	if !probe.ServerRunning {
		t.Fatal("server_running=1 应被解析")
	}
	if len(probe.Blockers) != 0 {
		t.Fatalf("不应有阻断: %v", probe.Blockers)
	}
	if !strings.Contains(probe.NextAction, "可直接连接") {
		t.Fatalf("应提示可直接连接，实际 %q", probe.NextAction)
	}
}

// 已装但端口与约定不同：提示会被改写，不静默覆盖。
func TestProbeConflictingListenWarns(t *testing.T) {
	probe := parseProbe(t, strings.Join([]string{
		"os=Linux",
		"arch=x86_64",
		"binary=dec",
		"binary=dec-server",
		"binary=dec-mcp",
		"binary=dec-exec",
		"cmd=git",
		"cmd=ssh-keygen",
		"cmd=curl",
		"cmd=bash",
		"home_writable=1",
		"spawn=both",
		"listen=127.0.0.1:59999",
	}, "\n"))

	if probe.ListenReady {
		t.Fatal("端口不同不应视为就绪")
	}
	if !hasSubstring(probe.Warnings, "会改为") {
		t.Fatalf("应提示端口将被改写: %v", probe.Warnings)
	}
	if !strings.Contains(probe.NextAction, "固定监听端口") {
		t.Fatalf("应提示仍需配置监听，实际 %q", probe.NextAction)
	}
}

func TestExtractVersion(t *testing.T) {
	cases := map[string]string{
		"dec version v1.2.3":  "v1.2.3",
		"v0.9.10":             "v0.9.10",
		"":                    "",
		"no version here":     "",
		"dec version v1.2":    "",
		"prefix v10.20.30 tl": "v10.20.30",
	}
	for in, want := range cases {
		if got := extractVersion(in); got != want {
			t.Fatalf("extractVersion(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestSSHArgsUseBatchModeAndPort(t *testing.T) {
	args := sshArgs(RemoteTarget{Host: "h", User: "u", Port: 2222}, "sh -s")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "BatchMode=yes") {
		t.Fatalf("必须禁用交互提示，避免置备挂起: %v", args)
	}
	if !strings.Contains(joined, "-p 2222") {
		t.Fatalf("应传递端口: %v", args)
	}
	if !strings.HasSuffix(joined, "u@h sh -s") {
		t.Fatalf("应以目标与命令结尾: %v", args)
	}
}

func TestSSHArgsOmitsPortWhenUnset(t *testing.T) {
	if joined := strings.Join(sshArgs(RemoteTarget{Alias: "box"}, "sh -s"), " "); strings.Contains(joined, "-p ") {
		t.Fatalf("未指定端口时不应传 -p: %s", joined)
	}
}

func TestSummarizeSSHError(t *testing.T) {
	out := "OpenSSH_9.0\ndebug1: connecting\nPermission denied (publickey).\n"
	if got := summarizeSSHError(out, nil); got != "Permission denied (publickey)." {
		t.Fatalf("应提取关键原因，实际 %q", got)
	}
}

func parseProbe(t *testing.T, out string) *RemoteHostProbe {
	t.Helper()
	probe := &RemoteHostProbe{Reachable: true}
	parseRemoteProbeOutput(out, probe)
	evaluateRemoteProbe(probe)
	return probe
}

func hasSubstring(items []string, want string) bool {
	for _, item := range items {
		if strings.Contains(item, want) {
			return true
		}
	}
	return false
}
