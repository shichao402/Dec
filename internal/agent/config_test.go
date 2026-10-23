package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsPrimaryStateDir 守住「只有默认实例写宿主配置」这条边界。
//
// 这条边界的来源是一次真实事故：用 DEC_AGENT_STATE 指向临时目录起了一个
// 调试实例（端口 18787），它启动时照常写 IDE 的 MCP 配置，把正在服役那个
// 实例的 8787 条目改成了 18787，三个 IDE 的 MCP 入口同时失效。
//
// 状态目录能隔离数据，但宿主配置在用户主目录下，不属于状态目录，
// 所以必须靠显式判断拦住。
func TestIsPrimaryStateDir(t *testing.T) {
	// 用临时 HOME 把「默认目录」变成一个已知路径，避免依赖真实用户主目录。
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("DEC_AGENT_STATE", "")
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("USERPROFILE", "")

	def := filepath.Join(tmp, ".dec-agent")
	if !IsPrimaryStateDir(def) {
		t.Errorf("默认状态目录 %q 应被认作服役实例", def)
	}

	// 尾部斜杠、相对路径绕行都不该改变判定结果。
	if !IsPrimaryStateDir(def + string(os.PathSeparator)) {
		t.Errorf("默认状态目录带尾斜杠也应被认作服役实例")
	}

	for _, other := range []string{
		filepath.Join(tmp, ".dec-agent-2"),
		filepath.Join(tmp, "dec-test"),
		"/tmp/dec-test",
	} {
		if IsPrimaryStateDir(other) {
			t.Errorf("非默认目录 %q 不该被认作服役实例", other)
		}
	}
}

// TestDefaultStateDirHonoursEnv 守住 DEC_AGENT_STATE 的优先级。
//
// 服务定义（launchd plist、systemd unit）靠这个环境变量把状态目录传进来，
// 它必须能覆盖平台默认值，否则非默认安装会读错目录。
func TestDefaultStateDirHonoursEnv(t *testing.T) {
	tmp := t.TempDir()
	custom := filepath.Join(tmp, "custom-state")
	t.Setenv("DEC_AGENT_STATE", custom)

	got, err := DefaultStateDir()
	if err != nil {
		t.Fatalf("取默认状态目录失败: %v", err)
	}
	if got != custom {
		t.Errorf("DEC_AGENT_STATE 应覆盖默认值：期望 %q，实际 %q", custom, got)
	}

	// 同时确认它不会污染服役实例的判定。
	if IsPrimaryStateDir(custom) {
		t.Errorf("显式指定的自定义目录不该被认作服役实例")
	}
}
