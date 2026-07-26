package unlock

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// 环境变量：控制是否允许启动需要人工输入主密码的 web unlock。
const (
	// EnvAllowWebUnlock 设为 1 时强制允许（真机手工验证用）。
	EnvAllowWebUnlock = "DEC_ALLOW_WEB_UNLOCK"
	// EnvNoWebUnlock 设为 1 时强制禁止，用于传递给测试拉起的子进程。
	EnvNoWebUnlock = "DEC_NO_WEB_UNLOCK"
)

// ErrWebUnlockBlocked 表示当前进程禁止交互式 web unlock。
var ErrWebUnlockBlocked = errors.New(
	"web unlock 已禁用（测试环境不得弹出浏览器要求人工输入主密码）：" +
		"请提供 " + integrationAuthHint + " 或设置 DEC_BW_PASSWORD；" +
		"确需人工解锁时设置 " + EnvAllowWebUnlock + "=1")

// integrationAuthHint 与 secrets.IntegrationAuthRel 保持一致，
// 此处内联字符串以避免 unlock 反向依赖 secrets 包。
const integrationAuthHint = ".secrets/dec/integration/bitwarden.yaml"

// WebUnlockAllowed 报告当前进程是否允许启动交互式 web unlock。
//
// 测试默认禁止：go test 构建的二进制、以及测试拉起的子进程（通过
// EnvNoWebUnlock 传递）都不能弹浏览器，否则会打断无人值守的测试并要求人工输入。
func WebUnlockAllowed() bool {
	if envFlagEnabled(EnvAllowWebUnlock) {
		return true
	}
	if envFlagEnabled(EnvNoWebUnlock) {
		return false
	}
	return !testing.Testing()
}

func envFlagEnabled(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) == "1"
}
