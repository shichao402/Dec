package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/app"
)

// __service-setup 由置备经 SSH 非交互调用，那时没有 TTY。
// 若它不在内部白名单里，Execute 的 TTY 分流会把它当普通参数处理。
func TestServiceSetupIsInternalCLIArgs(t *testing.T) {
	if !isInternalCLIArgs([]string{"__service-setup"}) {
		t.Fatalf("__service-setup 必须走内部 CLI 分流")
	}
	if !isInternalCLIArgs([]string{"__service-setup", "--listen", "127.0.0.1:1"}) {
		t.Fatalf("带参数时同样应走内部 CLI 分流")
	}
}

func TestServiceSetupWritesDefaultListen(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	var stdout, stderr bytes.Buffer
	RootCmd.SetArgs([]string{"__service-setup"})
	RootCmd.SetOut(&stdout)
	RootCmd.SetErr(&stderr)
	t.Cleanup(func() {
		RootCmd.SetArgs(nil)
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
	})

	if err := RootCmd.Execute(); err != nil {
		t.Fatalf("执行失败: %v (stderr=%s)", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "service-setup=ok") {
		t.Fatalf("缺少成功标记:\n%s", out)
	}
	if !strings.Contains(out, "listen="+app.RemoteProvisionListen) {
		t.Fatalf("默认监听地址不符:\n%s", out)
	}
	if !strings.Contains(out, "changed=true") {
		t.Fatalf("首次写入应报告 changed=true:\n%s", out)
	}

	data, err := os.ReadFile(filepath.Join(decHome, "config.yaml"))
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if !strings.Contains(string(data), app.RemoteProvisionListen) {
		t.Fatalf("配置未落盘:\n%s", data)
	}
}

func TestServiceSetupIsIdempotent(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)

	run := func() string {
		var stdout bytes.Buffer
		RootCmd.SetArgs([]string{"__service-setup"})
		RootCmd.SetOut(&stdout)
		RootCmd.SetErr(&stdout)
		if err := RootCmd.Execute(); err != nil {
			t.Fatalf("执行失败: %v", err)
		}
		return stdout.String()
	}
	t.Cleanup(func() {
		RootCmd.SetArgs(nil)
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
	})

	run()
	second := run()
	if !strings.Contains(second, "changed=false") {
		t.Fatalf("重复执行应报告 changed=false:\n%s", second)
	}
}
