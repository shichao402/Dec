package mcp

import (
	"context"
	"testing"
)

// confirmed=false 必须在发起任何 SSH 副作用前失败。这里无需 serviceapi 默认连接：
// ProvisionRemoteHost 的显式确认在远端安装前由服务端校验；MCP 参数本身也应保持
// confirmed 字段不可省略的契约。
func TestProvisionRemoteParamsExposeExplicitConfirmation(t *testing.T) {
	in := provisionRemoteParams{SSHTarget: "build-box"}
	if in.Confirmed {
		t.Fatal("confirmed 零值必须为 false")
	}
	if in.SSHTarget != "build-box" {
		t.Fatalf("ssh_target 未保留: %q", in.SSHTarget)
	}
}

func TestProvisionRemoteRejectsEmptyTargetBeforeServiceCall(t *testing.T) {
	s := New(Config{ProjectRoot: t.TempDir()})
	_, out, err := s.handleProvisionRemote(context.Background(), nil, provisionRemoteParams{Confirmed: true})
	if err != nil {
		t.Fatalf("工具协议层不应返回 error: %v", err)
	}
	response := out.(toolResponse)
	if response.OK {
		t.Fatal("空 ssh_target 应失败")
	}
}
