package unlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EnvNoWebUnlock 设为 1 时禁止 web unlock，用于测试拉起的子进程
// （子进程不是 test 二进制，自己检测不到测试环境）。
//
// 这里刻意不提供「强制允许」的开关：放行开关会随 shell 环境继承进
// go test 与其子进程，让「测试绝不弹窗」在无人察觉时失效。需要人工解锁
// 的真机验证直接运行 dec 或一次性工具即可，非测试二进制默认就允许。
const EnvNoWebUnlock = "DEC_NO_WEB_UNLOCK"

// ErrWebUnlockBlocked 表示当前进程禁止交互式 web unlock。
var ErrWebUnlockBlocked = errors.New(
	"web unlock 已禁用（测试环境不得弹出浏览器要求人工输入主密码）：" +
		"请提供 " + integrationAuthHint + " 或设置 DEC_BW_PASSWORD")

// integrationAuthHint 与 secrets.IntegrationAuthRel 保持一致，
// 此处内联字符串以避免 unlock 反向依赖 secrets 包。
const integrationAuthHint = ".secrets/dec/integration/bitwarden.yaml"

// WebUnlockAllowed 报告当前进程是否允许启动交互式 web unlock。
//
// 测试二进制内无条件禁止，没有任何环境变量能覆盖这一条。
func WebUnlockAllowed() bool {
	if testing.Testing() {
		return false
	}
	return !envFlagEnabled(EnvNoWebUnlock)
}

func envFlagEnabled(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) == "1"
}

// logWebUnlockDecision 把「是否拉起需要人工输入主密码的解锁页」无条件写到 stderr。
//
// 不走调用方注入的 reporter/OnStatus：那些在测试和子进程里通常为 nil，
// 结果就是进程默默卡在等人输密码，而日志里看不出任何认证痕迹。
func logWebUnlockDecision(allowed bool, detail string) {
	exe := "unknown"
	if path, err := os.Executable(); err == nil {
		exe = filepath.Base(path)
	}
	verdict := "已拦下"
	if allowed {
		verdict = "即将弹出浏览器"
	}
	fmt.Fprintf(os.Stderr,
		"[dec:auth] WEB UNLOCK %s：%s pid=%d exe=%s test_binary=%t %s=%s\n",
		verdict, detail, os.Getpid(), exe, testing.Testing(),
		EnvNoWebUnlock, os.Getenv(EnvNoWebUnlock))
}
