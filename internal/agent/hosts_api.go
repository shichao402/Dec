package agent

import (
	"fmt"
	"os"
)

// 宿主同步的对外入口（需求 2）。

// SyncHosts 把 `dec` 条目写入所有已安装宿主的用户级 MCP 配置。
//
// 代理启动时调用一次，端口变化时再调用一次（需求 2.4）：
// 这两个时机覆盖了「用户新装了 Cursor」与「本机端口被占用后改用别的端口」
// 两种会让条目失效的情况。
func SyncHosts(addr string) []HostResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return []HostResult{{
			Name:   "全部",
			Status: HostFailed,
			Reason: fmt.Sprintf("取用户主目录失败：%v", err),
		}}
	}
	return syncHosts(DefaultHosts(home), MCPServerURL(addr))
}

// RemoveHosts 从所有宿主里移除 `dec` 条目（需求 2.5）。
func RemoveHosts() []HostResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return []HostResult{{
			Name:   "全部",
			Status: HostFailed,
			Reason: fmt.Sprintf("取用户主目录失败：%v", err),
		}}
	}
	return removeHosts(DefaultHosts(home))
}

// InspectHosts 读出所有宿主当前的 `dec` 条目，不做修改（需求 2.6）。
func InspectHosts() []HostResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return []HostResult{{
			Name:   "全部",
			Status: HostFailed,
			Reason: fmt.Sprintf("取用户主目录失败：%v", err),
		}}
	}
	return inspectHosts(DefaultHosts(home))
}

// MCPConfigPaths 返回各宿主的配置文件路径，供文档与排障对照。
func MCPConfigPaths() []HostResult {
	return InspectHosts()
}

// String 让人读得懂状态。
func (s HostStatus) String() string {
	switch s {
	case HostWritten:
		return "已写入"
	case HostRemoved:
		return "已移除"
	case HostSkipped:
		return "已跳过"
	case HostFailed:
		return "失败"
	}
	return "未知"
}
