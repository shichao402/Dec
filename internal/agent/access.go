package agent

import (
	"net"
	"os"
	"time"
)

// 本机入口信息（agent-tray-access 需求 2.3、3.2、3.3、4.9）。
//
// 托盘与管理面都靠 /api/status 拿到「现在能从哪里打开管理面」。地址必须由
// 代理按实际监听结果算出来，不能让调用方自己拼 8787：端口可能被参数改掉，
// HTTPS 也可能因为 443 被占用而落到别的端口。

// TLSState 是本机 HTTPS 证书的状态。
type TLSState string

const (
	// TLSNone 表示还没有证书（未下发或未启用）。
	TLSNone TLSState = "none"
	// TLSOK 表示证书有效且距到期超过告警阈值。
	TLSOK TLSState = "ok"
	// TLSExpiring 表示证书有效但距到期不足告警阈值（需求 4.8）。
	TLSExpiring TLSState = "expiring"
	// TLSExpired 表示证书已过期。
	TLSExpired TLSState = "expired"
	// TLSError 表示证书存在但无法加载，或 HTTPS 监听失败。
	TLSError TLSState = "error"
)

// TLSExpiryWarning 是证书到期告警阈值（需求 4.8）。
const TLSExpiryWarning = 14 * 24 * time.Hour

// TLSStatus 是本机 HTTPS 的状态快照。
type TLSStatus struct {
	State    TLSState
	NotAfter time.Time
	// Addr 是 HTTPS 实际监听地址，空表示未监听。
	Addr  string
	Error string
}

// ClassifyTLS 按到期时间给出证书状态。
func ClassifyTLS(notAfter, now time.Time) TLSState {
	switch {
	case notAfter.IsZero():
		return TLSNone
	case !now.Before(notAfter):
		return TLSExpired
	case notAfter.Sub(now) < TLSExpiryWarning:
		return TLSExpiring
	}
	return TLSOK
}

// AccessURL 是一个可用来打开本机页面的地址。
type AccessURL struct {
	URL string `json:"url"`
	// Kind 取值 local-domain、localhost、loopback。
	Kind string `json:"kind"`
	// Available 为 false 表示该地址暂不可用（如证书未下发），Reason 说明原因。
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// AccessURLs 按优先级列出本机页面的访问地址。
//
// httpAddr 是回环 HTTP 的实际监听地址；tls 是 HTTPS 状态。返回的第一个
// Available 项就是首选地址：local 子域可用时优先，否则退回回环地址。
func AccessURLs(httpAddr string, tls TLSStatus) []AccessURL {
	var out []AccessURL
	_, httpPort, _ := net.SplitHostPort(httpAddr)

	local := AccessURL{Kind: "local-domain"}
	switch {
	case tls.Addr != "" && (tls.State == TLSOK || tls.State == TLSExpiring):
		_, p, _ := net.SplitHostPort(tls.Addr)
		if p == "443" || p == "" {
			local.URL = "https://" + LocalDomain
		} else {
			local.URL = "https://" + LocalDomain + ":" + p
		}
		local.Available = true
	default:
		local.URL = "https://" + LocalDomain
		local.Reason = localUnavailableReason(tls)
	}
	out = append(out, local)

	if httpPort != "" {
		out = append(out,
			AccessURL{URL: "http://127.0.0.1:" + httpPort, Kind: "loopback", Available: true},
			AccessURL{URL: "http://dec.localhost:" + httpPort, Kind: "localhost", Available: true},
		)
	}
	return out
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

func localUnavailableReason(tls TLSStatus) string {
	switch tls.State {
	case TLSNone:
		return "证书尚未下发"
	case TLSExpired:
		return "证书已过期"
	case TLSError:
		if tls.Error != "" {
			return tls.Error
		}
		return "HTTPS 不可用"
	}
	if tls.Addr == "" {
		return "HTTPS 未监听"
	}
	return ""
}

// localInfo 汇总 /api/status 里的本机入口字段。
func (l *Loopback) localInfo() map[string]any {
	var tls TLSStatus
	if l.opts.TLS != nil {
		tls = l.opts.TLS()
	}
	if tls.State == "" {
		tls.State = TLSNone
	}
	urls := AccessURLs(l.addr, tls)
	tlsOut := map[string]any{
		"state": tls.State,
		"addr":  tls.Addr,
	}
	if !tls.NotAfter.IsZero() {
		tlsOut["not_after"] = tls.NotAfter
	}
	if tls.Error != "" {
		tlsOut["error"] = tls.Error
	}
	return map[string]any{
		"pid":       os.Getpid(),
		"http_addr": l.addr,
		"urls":      urls,
		"admin_url": PreferredAdminURL(urls),
		"log_path":  l.opts.LogPath,
		"tls":       tlsOut,
	}
}
