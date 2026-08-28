package unlock

import (
	"net"
	"os"
	"strings"
	"testing"
)

func TestCaptureRequestDetailsIncludesProcessAndStack(t *testing.T) {
	details := captureRequestDetails(RequestContext{
		Source: "push.secrets", Facade: "mcp", ClientID: "mcp-123",
		Operation: "push", OperationID: "op-456",
		ProjectRoot: `D:\workspace\GitHub\Dec`, WorkspacePlane: "project",
	})
	if details.ID == "" || !strings.HasPrefix(details.ID, "auth-") {
		t.Fatalf("request ID = %q", details.ID)
	}
	if details.PID != os.Getpid() || details.PPID != os.Getppid() {
		t.Fatalf("进程标识错误: pid=%d ppid=%d", details.PID, details.PPID)
	}
	if details.Executable == "" || details.WorkingDir == "" || details.RequestedAt == "" {
		t.Fatalf("缺少进程诊断信息: %#v", details)
	}
	if details.Hostname == "" || details.IPs == "" || details.MACs == "" {
		t.Fatalf("缺少本机网络诊断信息: hostname=%q ips=%q macs=%q", details.Hostname, details.IPs, details.MACs)
	}
	for _, want := range []string{"push.secrets", "mcp", "mcp-123", "push", "op-456", "project"} {
		if !strings.Contains(strings.Join([]string{
			details.Source, details.Facade, details.ClientID, details.Operation,
			details.OperationID, details.WorkspacePlane,
		}, "\n"), want) {
			t.Fatalf("缺少请求上下文 %q: %#v", want, details)
		}
	}
	if !strings.Contains(details.CallStack, "TestCaptureRequestDetailsIncludesProcessAndStack") {
		t.Fatalf("调用栈未包含发起点:\n%s", details.CallStack)
	}
}

func TestFormatHardwareAddrSkipsEmptyAndZero(t *testing.T) {
	if got := formatHardwareAddr(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	if got := formatHardwareAddr(net.HardwareAddr{0, 0, 0, 0, 0, 0}); got != "" {
		t.Fatalf("zero = %q", got)
	}
	if got := formatHardwareAddr(net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}); got != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("got %q", got)
	}
}
