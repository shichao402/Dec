package agent

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTrayPlistKeepAliveOnlyOnCrash(t *testing.T) {
	p := trayPlist("/opt/dec/dec-tray", TrayOptions{StateDir: "/home/u/.dec-agent", LogDir: "/home/u/.dec-agent/logs"})
	var doc struct{ XMLName xml.Name }
	if err := xml.Unmarshal([]byte(strings.SplitN(p, "\n", 3)[2]), &doc); err != nil || doc.XMLName.Local != "plist" {
		// DOCTYPE 行之后应是合法 XML。
		t.Fatalf("plist 不是合法 XML: %v", err)
	}
	for _, want := range []string{
		"<string>com.dec.tray</string>",
		"<string>/opt/dec/dec-tray</string>",
		"<key>SuccessfulExit</key><false/>",
		"<key>RunAtLoad</key><true/>",
		"<string>Aqua</string>",
		"<key>DEC_AGENT_STATE</key><string>/home/u/.dec-agent</string>",
		"/home/u/.dec-agent/logs/agent.log",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist 缺少 %q", want)
		}
	}
	// 「退出」是零退出：KeepAlive 不能是无条件 true，否则会被立即拉起（需求 1.5）。
	if strings.Contains(p, "<key>KeepAlive</key><true/>") {
		t.Error("托盘 KeepAlive 不应无条件保活")
	}
}

func TestTrayPlistEscapesPaths(t *testing.T) {
	p := trayPlist("/a&b/<x>/dec-tray", TrayOptions{StateDir: "/s", LogDir: "/s/logs"})
	if !strings.Contains(p, "/a&amp;b/&lt;x&gt;/dec-tray") {
		t.Errorf("路径未转义: %s", p)
	}
}

func TestWindowsTaskXML(t *testing.T) {
	x := windowsTaskXML("Dec 托盘图标", `C:\Program Files\Dec\dec-tray.exe`, "", `CORP\firo`)
	var task struct {
		Settings struct {
			ExecutionTimeLimit string
			RestartOnFailure   struct{ Interval string }
		}
		Principals struct {
			Principal struct{ UserId, RunLevel, LogonType string }
		}
		Triggers struct {
			LogonTrigger struct{ UserId string }
		}
		Actions struct {
			Exec struct{ Command, Arguments string }
		}
	}
	dec := xml.NewDecoder(strings.NewReader(strings.Replace(x, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)))
	if err := dec.Decode(&task); err != nil {
		t.Fatalf("任务 XML 不合法: %v\n%s", err, x)
	}
	if task.Settings.ExecutionTimeLimit != "PT0S" {
		t.Errorf("常驻任务必须关掉执行时限，得到 %q", task.Settings.ExecutionTimeLimit)
	}
	if task.Settings.RestartOnFailure.Interval == "" {
		t.Error("缺少失败重试")
	}
	if task.Principals.Principal.RunLevel != "LeastPrivilege" {
		t.Errorf("必须是用户级权限，得到 %q", task.Principals.Principal.RunLevel)
	}
	if task.Triggers.LogonTrigger.UserId != `CORP\firo` || task.Principals.Principal.UserId != `CORP\firo` {
		t.Error("登录触发与运行身份应限定为当前用户")
	}
	if task.Actions.Exec.Command != `C:\Program Files\Dec\dec-tray.exe` || task.Actions.Exec.Arguments != "" {
		t.Errorf("Exec 不符: %+v", task.Actions.Exec)
	}
	if strings.Contains(x, "<Arguments>") {
		t.Error("无参数时不应输出空 Arguments")
	}
}

func TestWindowsExecDefaultStateRunsDirectly(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join("C:", "Users", "u", "AppData", "Local"))
	def := filepath.Join(os.Getenv("LOCALAPPDATA"), "dec-agent")
	cmd, args := windowsExec(`C:\Dec\dec-agent.exe`, []string{"run"}, def)
	if cmd != `C:\Dec\dec-agent.exe` || args != "run" {
		t.Errorf("默认状态目录应直接启动，得到 %q %q", cmd, args)
	}
}

func TestWindowsExecCustomStateUsesCmd(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\L`)
	cmd, args := windowsExec(`C:\Program Files\Dec\dec-agent.exe`, []string{"run"}, `D:\dec state`)
	if cmd != "cmd.exe" {
		t.Fatalf("自定义状态目录需经 cmd.exe 设置环境变量，得到 %q", cmd)
	}
	want := `/c "set "DEC_AGENT_STATE=D:\dec state" && "C:\Program Files\Dec\dec-agent.exe" run"`
	if args != want {
		t.Errorf("参数\n得到 %s\n想要 %s", args, want)
	}
}

func TestEncodeUTF16LE(t *testing.T) {
	b := encodeUTF16LE("A托")
	if b[0] != 0xFF || b[1] != 0xFE {
		t.Fatal("缺少 UTF-16LE BOM")
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
	}
	if got := string(utf16.Decode(u)); got != "A托" {
		t.Errorf("解码得到 %q", got)
	}
}

func TestFindTrayBinary(t *testing.T) {
	dir := t.TempDir()
	agentBin := filepath.Join(dir, "dec-agent")
	if err := os.WriteFile(agentBin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := FindTrayBinary(agentBin); err == nil {
		t.Fatal("没有托盘时应报错，由调用方降级为告警")
	}
	tray := filepath.Join(dir, TrayBinaryName())
	if err := os.WriteFile(tray, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindTrayBinary(agentBin)
	if err != nil || got != tray {
		t.Fatalf("得到 %q, %v", got, err)
	}
}

func TestFindTrayBinaryAcceptsReleaseName(t *testing.T) {
	dir := t.TempDir()
	agentBin := filepath.Join(dir, "dec-agent")
	if err := os.WriteFile(agentBin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, trayReleaseName())
	if err := os.WriteFile(p, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindTrayBinary(agentBin)
	if err != nil || got != p {
		t.Fatalf("得到 %q, %v", got, err)
	}
}

func TestFindTrayBinaryFollowsSymlink(t *testing.T) {
	real := t.TempDir()
	link := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "dec-agent"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, TrayBinaryName()), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	ln := filepath.Join(link, "dec-agent")
	if err := os.Symlink(filepath.Join(real, "dec-agent"), ln); err != nil {
		t.Skip("当前环境不支持符号链接")
	}
	got, err := FindTrayBinary(ln)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(filepath.Join(real, TrayBinaryName())); got != want && got != filepath.Join(real, TrayBinaryName()) {
		t.Errorf("得到 %q", got)
	}
}

func TestWriteAddrFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAddrFile(dir, "127.0.0.1:9999"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, AddrFileName))
	if err != nil || strings.TrimSpace(string(b)) != "127.0.0.1:9999" {
		t.Fatalf("得到 %q, %v", b, err)
	}
}
