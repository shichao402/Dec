package agent

import (
	"net"
	"os"
)

// 本机入口信息（agent-tray-access 需求 2.3、3.2、3.3）。
//
// 托盘与管理面都靠 /api/status 拿到「现在能从哪里打开管理面」。地址必须由
// 代理按实际监听结果算出来，不能让调用方自己拼端口：端口可能被参数改掉。
//
// 本机入口只有回环 HTTP 一个监听：托盘自带管理面窗口，直接加载这里给出的
// 地址，不再需要自有域名与本机证书。

// DefaultPort 是回环 HTTP 的默认端口。
//
// 取一个偏僻的高位端口：8787/8788 这类整齐的数字常被各种开发服务占用，
// 冲突时代理起不来，托盘和 IDE 里的 MCP 条目也跟着失效。
const DefaultPort = 43679

// AccessURL 是一个可用来打开本机页面的地址。
type AccessURL struct {
	URL string `json:"url"`
	// Kind 取值 loopback、localhost。
	Kind string `json:"kind"`
	// Available 为 false 表示该地址暂不可用，Reason 说明原因。
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// AccessURLs 按优先级列出本机页面的访问地址。
//
// httpAddr 是回环 HTTP 的实际监听地址。回环 IP 排第一：它不依赖任何解析，
// 托盘窗口与浏览器都能直接打开；dec.localhost 只是更好记的别名。
func AccessURLs(httpAddr string) []AccessURL {
	_, httpPort, _ := net.SplitHostPort(httpAddr)
	if httpPort == "" {
		return nil
	}
	return []AccessURL{
		{URL: "http://127.0.0.1:" + httpPort, Kind: "loopback", Available: true},
		{URL: "http://dec.localhost:" + httpPort, Kind: "localhost", Available: true},
	}
}

// PreferredAdminURL 返回首选的管理面地址，没有可用地址时返回空串。
func PreferredAdminURL(urls []AccessURL) string {
	for _, u := range urls {
		if u.Available {
			return u.URL + "/admin"
		}
	}
	return ""
}

// localInfo 汇总 /api/status 里的本机入口字段。
func (l *Loopback) localInfo() map[string]any {
	urls := AccessURLs(l.addr)
	return map[string]any{
		"pid":       os.Getpid(),
		"http_addr": l.addr,
		"urls":      urls,
		"admin_url": PreferredAdminURL(urls),
		"log_path":  l.opts.LogPath,
	}
}
