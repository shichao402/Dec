package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// runtimeGeneration 是本进程所属运行时套件的内容代号，由 dec-server 启动时登记一次。
var runtimeGeneration atomic.Value

// SetRuntimeGeneration 登记当前运行时内容代号。空串表示未知，内置 MCP 条目此时不写标记。
func SetRuntimeGeneration(generation string) {
	runtimeGeneration.Store(strings.TrimSpace(generation))
}

// RuntimeGeneration 返回已登记的运行时内容代号；未登记时为空串。
func RuntimeGeneration() string {
	generation, _ := runtimeGeneration.Load().(string)
	return generation
}

// DetectRuntimeGeneration 用 dec-mcp 二进制内容生成稳定代号。
//
// 只用版本号无法覆盖开发期的同版本重新构建；时间戳又会让同一安装包重复安装时
// 无谓重启。二进制摘要同时满足「内容变化才变化」和「相同内容保持幂等」。
func DetectRuntimeGeneration(version string) string {
	executable, err := os.Executable()
	if err != nil {
		return strings.TrimSpace(version)
	}
	return runtimeGenerationForExecutable(version, executable)
}

func runtimeGenerationForExecutable(version, executable string) string {
	version = strings.TrimSpace(version)
	digest, err := sha256File(filepath.Join(filepath.Dir(executable), suiteBinaryName("dec-mcp", runtime.GOOS)))
	if err != nil || len(digest) < 16 {
		return version
	}
	if version == "" {
		return digest[:16]
	}
	return version + "+" + digest[:16]
}
