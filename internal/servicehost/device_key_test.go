package servicehost

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/app"
)

// 设备合成键不得被 projectKey 当成路径处理：
// 若走 filepath.Abs，device:box 会被拼成 <cwd>/device:box，
// 不同目标机在不同工作目录下可能撞键或互斥失效。
func TestProjectKeyPreservesDeviceSyntheticKey(t *testing.T) {
	key := app.DeviceOperationKey(app.RemoteTarget{Alias: "build-box"})

	got := projectKey(key)
	if got != key {
		t.Fatalf("设备键应原样保留，实际 %q（期望 %q）", got, key)
	}
	if filepath.IsAbs(got) {
		t.Fatalf("设备键不得被解析成绝对路径: %q", got)
	}
	if strings.Contains(got, string(filepath.Separator)) {
		t.Fatalf("设备键不得包含路径分隔符: %q", got)
	}
}

// 大小写不同的同一目标必须归一到同一互斥键。
func TestProjectKeyDeviceKeyIsCaseInsensitive(t *testing.T) {
	a := projectKey("device:Build-Box")
	b := projectKey("device:build-box")
	if a != b {
		t.Fatalf("同一设备应归一为同一键: %q vs %q", a, b)
	}
}

// 真实项目路径必须仍然走原有的归一化，不被设备键分支误伤。
func TestProjectKeyStillNormalizesRealPaths(t *testing.T) {
	root := t.TempDir()
	if got := projectKey(root); !filepath.IsAbs(got) {
		t.Fatalf("真实路径应被归一为绝对路径: %q", got)
	}
}

// 同一台目标机不得被并发置备；不同目标机可以并行。
func TestOperationBrokerDeviceProvisionMutualExclusion(t *testing.T) {
	broker := newOperationBroker()
	box := app.DeviceOperationKey(app.RemoteTarget{Alias: "build-box"})
	other := app.DeviceOperationKey(app.RemoteTarget{Alias: "other-box"})

	if _, err := broker.start(box, "provision_remote_host", "console-1", "console"); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.start(box, "provision_remote_host", "mcp-1", "mcp"); err == nil {
		t.Fatal("同一目标机的第二次置备应返回 busy")
	}
	if _, err := broker.start(other, "provision_remote_host", "mcp-1", "mcp"); err != nil {
		t.Fatalf("不同目标机不应互斥: %v", err)
	}
}

// 置备的合成键不能拖住真实项目的操作。
func TestDeviceProvisionDoesNotBlockProjectOperations(t *testing.T) {
	broker := newOperationBroker()
	device := app.DeviceOperationKey(app.RemoteTarget{Host: "build.example"})

	if _, err := broker.start(device, "provision_remote_host", "console", "console"); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.start(t.TempDir(), "pull", "tui", "tui"); err != nil {
		t.Fatalf("项目操作不应被设备置备阻塞: %v", err)
	}
}

// ensureProjectRepaired 不得把设备合成键当项目路径去跑兼容修复。
func TestEnsureProjectRepairedSkipsDeviceKey(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	server := &Server{}
	device := app.DeviceOperationKey(app.RemoteTarget{Alias: "build-box"})

	server.ensureProjectRepaired(device, nil)

	if _, loaded := server.repairedProjects.Load(projectKey(device)); loaded {
		t.Fatal("设备键不应进入项目修复记录")
	}
}
