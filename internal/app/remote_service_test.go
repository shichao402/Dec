package app

import (
	"strings"
	"testing"
)

// remoteServerStatusScript 的 alive 判定必须用 kill -0 而非「文件存在」：
// 进程被 kill -9 后 server.json 会残留（只有正常退出才清理），
// 那时报 running 会让上层跳过拉起、去连一个已经没人监听的端口。
func TestRemoteServerStatusScriptChecksProcessLiveness(t *testing.T) {
	if !strings.Contains(remoteServerStatusScript, "kill -0") {
		t.Fatalf("状态脚本必须用 kill -0 确认进程存活，否则残留 server.json 会被误判为运行中")
	}
	if !strings.Contains(remoteServerStatusScript, "alive=0") {
		t.Fatalf("状态脚本必须能报告未运行")
	}
}

// SSH 会话结束时会向进程组发 SIGHUP，只靠 & 放后台的进程会跟着死。
func TestRemoteSpawnScriptDetachesFromSession(t *testing.T) {
	if !strings.Contains(remoteSpawnScript, "setsid") {
		t.Fatalf("拉起脚本应优先用 setsid 脱离会话")
	}
	if !strings.Contains(remoteSpawnScript, "nohup") {
		t.Fatalf("拉起脚本应用 nohup 兜底无 setsid 的机器")
	}
	if !strings.Contains(remoteSpawnScript, "< /dev/null") {
		t.Fatalf("拉起脚本必须切断 stdin，否则 SSH 会话不会结束")
	}
}

// 非交互 SSH 的 PATH 往往不含 ~/.dec/bin，必须显式走绝对路径。
func TestRemoteScriptsUseExplicitBinaryPath(t *testing.T) {
	for name, script := range map[string]string{
		"setup": remoteServiceSetupScript,
		"spawn": remoteSpawnScript,
	} {
		if !strings.Contains(script, "${DEC_HOME:-$HOME/.dec}") {
			t.Fatalf("%s 脚本应遵循 DEC_HOME 覆盖", name)
		}
		if !strings.Contains(script, "/bin/dec") {
			t.Fatalf("%s 脚本应显式使用 ~/.dec/bin 下的产物", name)
		}
	}
}

func TestRemoteServiceSetupScriptInvokesInternalCommand(t *testing.T) {
	if !strings.Contains(remoteServiceSetupScript, "__service-setup") {
		t.Fatalf("配置脚本必须调用远端自己的 __service-setup，不能在 Go 侧手写 YAML")
	}
}

func TestParsePositiveInt(t *testing.T) {
	cases := map[string]int{
		"1234":  1234,
		" 88 ":  88,
		"":      0,
		"12a":   0,
		"-5":    0,
		"0":     0,
		"99999": 99999,
	}
	for raw, want := range cases {
		if got := parsePositiveInt(raw); got != want {
			t.Fatalf("parsePositiveInt(%q) = %d, 期望 %d", raw, got, want)
		}
	}
}

func TestLastMeaningfulLine(t *testing.T) {
	if got := lastMeaningfulLine("first\nsecond\n\n  \n"); got != "second" {
		t.Fatalf("应取最后一行有效内容, 实际 %q", got)
	}
	if got := lastMeaningfulLine("   \n\n"); got != "远端无输出" {
		t.Fatalf("空输出应有兜底文案, 实际 %q", got)
	}
}

// 置备默认必须把端口配上：装完二进制却没配端口的机器仍然连不上，
// 置备只做一半没有意义。
func TestProvisionDefaultsToConfiguringListen(t *testing.T) {
	var in ProvisionRemoteHostInput
	if in.SkipConfigure {
		t.Fatalf("SkipConfigure 的零值必须为 false")
	}
}

func TestRemoteProvisionListenMatchesPort(t *testing.T) {
	if !strings.HasSuffix(RemoteProvisionListen, "47653") {
		t.Fatalf("监听地址与端口常量不一致: %q", RemoteProvisionListen)
	}
	if RemoteProvisionPort != 47653 {
		t.Fatalf("端口常量被改动: %d", RemoteProvisionPort)
	}
}
