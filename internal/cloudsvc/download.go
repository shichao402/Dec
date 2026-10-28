package cloudsvc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
)

// 客户端下载通道（需求 20）。
//
// 这是公网无证书端点，所以做得尽量窄：
//
//   - 只提供 download 目录下的文件，文件名不含目录分隔符；
//   - 只允许 GET/HEAD；
//   - 有频率限制（需求 20.4）；
//   - 索引页只列出文件与 curl 示例，不暴露任何内部信息。

// release 是发布清单里与服务端相关的部分。
//
// CI 在部署时把这份文件一起上传，服务端只读它，不主动去外部拉取（需求 20.2）。
type release struct {
	Version string `json:"version"`
	// RequiredClient 是要求的代理版本（需求 19.1）。
	RequiredClient string `json:"required_client"`
	// Protocol 是本次发布使用的协议版本。
	Protocol int `json:"protocol"`
	// BuiltAt 是构建时间。
	BuiltAt string `json:"built_at"`
	// Files 是这次发布的全部产物。
	Files []releaseFile `json:"files"`
}

// releaseFile 是一个发布产物。
type releaseFile struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// releaseFile 的存放位置。
func releaseManifestPath(dataDir string) string {
	return filepath.Join(dataDir, "release.json")
}

// loadRelease 读取发布清单。缺失时返回 nil，不报错：
// 开发环境里没有清单是正常的，此时下载页只给出说明。
func (s *Server) loadRelease() *release {
	data, err := os.ReadFile(releaseManifestPath(s.cfg.DataDir))
	if err != nil {
		return nil
	}
	var r release
	if err := json.Unmarshal(data, &r); err != nil {
		s.logf("发布清单解析失败: %v", err)
		return nil
	}
	return &r
}

// handleRoot 把域名根路径接到安装指引页。
//
// 注册的是 "/" 这个 catch-all 模式，所以必须先排除其他路径再把请求转给
// 下载页：否则任何拼错的地址都会返回 200，让人以为端点存在。
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.handleDownloadIndex(w, r)
}

// handleDownloadIndex 是无证书可访问的安装指引页（需求 20.1、20.6）。
//
// 它是这个域名下唯一的公开页面，也是任何一台新设备接入的第一站，
// 所以内容按「照抄就能装好」组织：每个平台一条完整命令块，从下载一直
// 走到登记。只列文件、让用户自己拼命令，等于把出错机会留给第一次接入的人。
func (s *Server) handleDownloadIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "只接受 GET", http.StatusMethodNotAllowed)
		return
	}
	if !s.limiter.allow(clientIP(r)) {
		http.Error(w, "请求过于频繁", http.StatusTooManyRequests)
		return
	}
	rel := s.loadRelease()
	host := s.cfg.Domain
	if host == "" {
		host = r.Host
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	b.WriteString("<title>Dec Cloud 安装指引</title>")
	b.WriteString("<style>body{font:14px/1.6 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;")
	b.WriteString("max-width:820px;margin:40px auto;padding:0 20px;color:#222}")
	b.WriteString("code,pre{background:#f4f4f5;border-radius:4px}code{padding:2px 5px}")
	b.WriteString("pre{padding:12px;overflow-x:auto;white-space:pre-wrap;word-break:break-all}")
	b.WriteString("table{border-collapse:collapse;width:100%}")
	b.WriteString("th,td{text-align:left;padding:6px 10px;border-bottom:1px solid #eee}")
	b.WriteString("h1{font-size:20px}h2{font-size:15px;margin-top:32px;padding-top:8px;border-top:1px solid #eee}")
	b.WriteString("h3{font-size:14px;margin-top:20px}.muted{color:#888}")
	b.WriteString(".step{color:#888;margin-right:6px}</style></head><body>")
	b.WriteString("<h1>Dec Cloud 安装指引</h1>")

	if rel == nil {
		b.WriteString("<p class=\"muted\">尚未上传发布清单。请先完成一次正式部署。</p>")
		b.WriteString("</body></html>")
		_, _ = w.Write([]byte(b.String()))
		return
	}

	// 可选平台取自清单本身：清单里没有的组合不该出现在指引里，
	// 否则用户照着命令敲会拿到 404。
	have := map[string]bool{}
	for _, f := range rel.Files {
		have[f.OS+"/"+f.Arch] = true
	}
	find := func(prefix, osName, arch string) string {
		for _, f := range rel.Files {
			if f.OS == osName && f.Arch == arch && strings.HasPrefix(f.Name, prefix) {
				return f.Name
			}
		}
		return ""
	}

	fmt.Fprintf(&b, "<p>当前要求版本：<code>%s</code>", htmlEscape(rel.RequiredClient))
	fmt.Fprintf(&b, "<span class=\"muted\">（服务端 %s，协议 %d，构建于 %s）</span></p>",
		htmlEscape(rel.Version), rel.Protocol, htmlEscape(rel.BuiltAt))
	b.WriteString("<p>代理必须与服务端要求版本一致，不一致时会被拦住并提示更新。")
	b.WriteString("下载二进制后直接运行，不需要签名文件，也不需要安装包。</p>")

	// 安装路径写用户目录：不需要 sudo，与代理自己的状态目录同属当前用户，
	// 卸载和升级都不会留下需要提权才能删的残留。
	binDir := "~/.local/bin"
	agentName := "dec-agent"

	b.WriteString("<h2>macOS 与 Linux</h2>")
	if arm := find("dec-agent-", "darwin", "arm64"); arm != "" {
		b.WriteString("<h3>macOS（Apple Silicon）</h3>")
		writeUnixSteps(&b, host, arm, binDir, agentName)
	}
	if amd := find("dec-agent-", "darwin", "amd64"); amd != "" {
		b.WriteString("<h3>macOS（Intel）</h3>")
		writeUnixSteps(&b, host, amd, binDir, agentName)
	}
	if arm := find("dec-agent-", "linux", "arm64"); arm != "" {
		b.WriteString("<h3>Linux（arm64）</h3>")
		writeUnixSteps(&b, host, arm, binDir, agentName)
	}
	if amd := find("dec-agent-", "linux", "amd64"); amd != "" {
		b.WriteString("<h3>Linux（amd64）</h3>")
		writeUnixSteps(&b, host, amd, binDir, agentName)
	}

	b.WriteString("<h2>Windows</h2>")
	if have["windows/amd64"] || have["windows/arm64"] {
		b.WriteString("<p>在 PowerShell 里执行。脚本会判定架构、下载并校验、登记设备、注册计划任务，")
		b.WriteString("装到 <code>%LOCALAPPDATA%\\Programs\\dec</code>，不需要管理员权限：</p>")
		b.WriteString("<pre>iwr -useb https://raw.githubusercontent.com/shichao402/Dec/main/scripts/install-agent.ps1 | iex</pre>")
		b.WriteString("<p class=\"muted\">也可以手工安装：下载对应架构的 <code>dec-agent-windows-*.exe</code>，")
		b.WriteString("重命名为 <code>dec-agent.exe</code>，然后依次执行 ")
		b.WriteString("<code>dec-agent configure --url https://" + htmlEscape(host) + "</code>、<code>dec-agent enroll --name win-desktop</code>、<code>dec-agent install</code>。</p>")
	}

	b.WriteString("<h2>登记</h2>")
	b.WriteString("<p>第一台设备用 <code>initialize</code>，会生成根密钥并写入 Bitwarden 冷备；")
	b.WriteString("此后新增设备一律用 <code>enroll</code>。两者都需要 Bitwarden 邮箱、主密码与两步验证码。</p>")
	b.WriteString("<p class=\"muted\">上面的 macOS 与 Linux 命令块已包含 <code>enroll</code>。")
	b.WriteString("如果这台设备是本服务的第一台设备，把 <code>enroll</code> 换成 <code>initialize</code>。</p>")

	b.WriteString("<h2>确认装好了</h2>")
	b.WriteString("<pre>curl -s http://127.0.0.1:43679/api/status</pre>")
	b.WriteString("<p>返回里有 <code>server_url</code>、<code>device_id</code>、<code>aligned</code> 就说明连上了。")
	b.WriteString("<code>aligned</code> 为 <code>false</code> 表示代理版本与服务端要求不符，需要按上面的指引重装当前要求版本。</p>")
	b.WriteString("<p>管理面在 <code>http://127.0.0.1:43679/admin</code>，由本机代理提供，不经过公网。</p>")

	b.WriteString("<h2>全部产物</h2>")
	b.WriteString("<table><tr><th>平台</th><th>架构</th><th>文件</th><th>大小</th></tr>")
	files := append([]releaseFile(nil), rel.Files...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].OS != files[j].OS {
			return files[i].OS < files[j].OS
		}
		if files[i].Arch != files[j].Arch {
			return files[i].Arch < files[j].Arch
		}
		return files[i].Name < files[j].Name
	})
	for _, f := range files {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td><code>%s</code></td><td>%s</td></tr>",
			htmlEscape(f.OS), htmlEscape(f.Arch), htmlEscape(f.Name), humanSize(f.Size))
	}
	b.WriteString("</table>")
	b.WriteString("<p class=\"muted\">托盘（<code>dec-tray-*</code>）放在 <code>dec-agent</code> 同目录，")
	b.WriteString("<code>dec-agent install</code> 会一并注册登录自启动。没有托盘文件时代理照常工作。</p>")

	b.WriteString("<h2>被系统拦截时</h2>")
	b.WriteString("<p>客户端二进制不做代码签名（本项目只有一个使用者，签名与公证的成本远大于收益）。</p>")
	b.WriteString("<p>macOS Gatekeeper：用 <code>curl</code> 下载后，在 Finder 里右键 → 打开，")
	b.WriteString("或执行 <code>xattr -d com.apple.quarantine 文件名</code>。</p>")
	b.WriteString("<p>Windows SmartScreen：点击“更多信息”→“仍要运行”。</p>")

	b.WriteString("<h2>卸载</h2>")
	b.WriteString("<pre>dec-agent uninstall</pre>")
	b.WriteString("<p>会移除系统服务、登录项、所有 Dec 管理的落点与本机设备身份，并在在线时通知云端注销这台设备。</p>")

	b.WriteString("<p class=\"muted\">只保留当前与上一个要求版本。</p>")
	b.WriteString("</body></html>")
	_, _ = w.Write([]byte(b.String()))
}

// writeUnixSteps 写出一段 macOS/Linux 的安装命令。
//
// 每行都用 \n 单独拼，而不是塞进一个长模板串：这些命令是要被照抄的，
// 一旦被 HTML 折成一行就不可读了。
func writeUnixSteps(b *strings.Builder, host, file, binDir, agentName string) {
	dl := "https://" + host + "/download/" + file
	fmt.Fprintf(b, "<pre>mkdir -p %s\n", binDir)
	fmt.Fprintf(b, "curl -fLo %s/%s %s\n", binDir, agentName, dl)
	fmt.Fprintf(b, "chmod +x %s/%s\n", binDir, agentName)
	// 用绝对路径调用，避免用户 PATH 里还没有 binDir 时下一步就失败。
	fmt.Fprintf(b, "%s/%s configure --url https://%s\n", binDir, agentName, host)
	fmt.Fprintf(b, "%s/%s enroll --name \"$(hostname)\"\n", binDir, agentName)
	fmt.Fprintf(b, "%s/%s install</pre>\n", binDir, agentName)
	b.WriteString("<p class=\"muted\">不需要 <code>sudo</code>：代理和它的状态目录都归当前用户。")
	b.WriteString("如果 <code>" + binDir + "</code> 不在 <code>PATH</code> 里，把这一行加到 shell 配置文件：")
	b.WriteString("<code>export PATH=\"$HOME/.local/bin:$PATH\"</code>。</p>")
}

// handleDownload 提供单个文件（需求 20.1）。
//
// 只允许 download 目录下的直接子文件，拒绝任何带分隔符或上跳的路径。
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "只接受 GET", http.StatusMethodNotAllowed)
		return
	}
	if !s.limiter.allow(clientIP(r)) {
		http.Error(w, "请求过于频繁", http.StatusTooManyRequests)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/download/")
	name = trimTrailing(name)
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		http.NotFound(w, r)
		return
	}
	// 只提供发布清单里声明过的文件：目录里可能有临时文件或残留，
	// 白名单比黑名单可靠。
	rel := s.loadRelease()
	if rel == nil {
		http.NotFound(w, r)
		return
	}
	var found *releaseFile
	for i := range rel.Files {
		if rel.Files[i].Name == name {
			found = &rel.Files[i]
			break
		}
	}
	if found == nil {
		http.NotFound(w, r)
		return
	}

	full := filepath.Join(s.downloadDir, name)
	fi, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "读取文件失败", http.StatusInternalServerError)
		return
	}
	if fi.IsDir() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name))
	w.Header().Set("Cache-Control", "no-store")
	// 显式给出校验值，便于下载后核对（清单里已算好，避免边下边算）。
	if found.SHA256 != "" {
		w.Header().Set("X-Checksum-SHA256", found.SHA256)
	}
	// 下载记为审计但不告警（需求 20.4 明确不为此告警）：
	// 这是公网端点，把它做成告警只会让人很快开始忽略所有告警。
	//
	// 这是唯一一条由公网匿名请求触发的审计，所以它必须是访问级：
	// 同一 IP、同一文件的重复下载由日志层按小时聚合（见 audit/aggregate.go），
	// 既满足"访问记入审计"，又不会让一次爬取就把审计仓库撑大。
	if s.audit != nil {
		if err := s.audit.Append(audit.Entry{
			Event:    audit.EventDownload,
			Source:   "public",
			Subject:  name,
			Detail:   fmt.Sprintf("版本 %s，%s", rel.Version, humanSize(fi.Size())),
			RemoteIP: clientIP(r),
			OK:       true,
		}); err != nil {
			s.logf("写审计失败: %v", err)
		}
	}
	http.ServeFile(w, r, full)
}

// clientIP 取客户端 IP。
//
// 生产拓扑是
//
//	公网 443 → nginx stream(ssl_preread + PROXY protocol) → 127.0.0.1:8445
//	→ 剥掉 PROXY 头 → dec-cloud 127.0.0.1:8444
//
// 中间的 8445 那一跳会剥掉 PROXY 头，所以历史上这里拿到的永远是 127.0.0.1。
// 开启 Config.ProxyProtocol 后（生产已开启），listener 层会解开 PROXY 头
// 并把真实来源填进 RemoteAddr，这里就能拿到公网 IP。
//
// 但仍然只认 RemoteAddr、不看转发头。理由是两者性质不同：
// PROXY protocol 头由 nginx 在 TCP 层写入，客户端无法越过它伪造；
// 而 X-Forwarded-For 只是一行 HTTP 头，公网请求可以自己写任何值，
// 信任它等于让请求者自报来源——那比"地址不准"更糟，限流会被直接绕过。
//
// 什么时候会退化成 127.0.0.1：未经 nginx 的本机直连（调试、健康检查），
// 以及 nginx 用 LOCAL 命令发起的连接。这些场景本来就没有真实来源可言。
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}

// rateLimiter 是按 IP 的固定窗口限流器。
//
// 目的只是挡住脚本刷下载，不追求精确。单人使用的服务端不需要更复杂的算法。
type rateLimiter struct {
	mu       sync.Mutex
	hits     map[string][]time.Time
	limit    int
	window   time.Duration
	lastTrim time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		hits:   map[string][]time.Time{},
		limit:  limit,
		window: window,
	}
}

// allow 报告这个 IP 是否还能继续请求。
func (l *rateLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// 每分钟清一次过期条目，避免 map 无界增长。
	if now.Sub(l.lastTrim) > time.Minute {
		cutoff := now.Add(-l.window)
		for k, list := range l.hits {
			kept := list[:0]
			for _, t := range list {
				if t.After(cutoff) {
					kept = append(kept, t)
				}
			}
			if len(kept) == 0 {
				delete(l.hits, k)
				continue
			}
			l.hits[k] = kept
		}
		l.lastTrim = now
	}

	cutoff := now.Add(-l.window)
	list := l.hits[ip]
	kept := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[ip] = kept
		return false
	}
	l.hits[ip] = append(kept, now)
	return true
}

// humanSize 把字节数转成易读大小。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// htmlEscape 做最小转义。文件名来自发布清单，理论上可信，
// 但索引页是公网可见的，仍然要转义。
func htmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;",
	)
	return replacer.Replace(s)
}
