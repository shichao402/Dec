package app

import (
	"strings"
	"sync/atomic"
)

// runtimeVersion 是本进程所属运行时套件的版本，由 dec-server 启动时登记一次。
var runtimeVersion atomic.Value

// SetRuntimeVersion 登记当前运行时版本。空串表示未知，内置 MCP 条目此时不写版本标记。
func SetRuntimeVersion(version string) {
	runtimeVersion.Store(strings.TrimSpace(version))
}

// RuntimeVersion 返回已登记的运行时版本；未登记时为空串。
func RuntimeVersion() string {
	version, _ := runtimeVersion.Load().(string)
	return version
}
