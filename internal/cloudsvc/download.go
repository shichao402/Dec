package cloudsvc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	fmt.Fprintf(&b, "<p>当前要求版本：<code>%s</code>", htmlEscape(rel.RequiredClient))
	fmt.Fprintf(&b, "<span class=\"muted\">（服务端 %s，协议 %d，构建于 %s）</span></p>",
		htmlEscape(rel.Version), rel.Protocol, htmlEscape(rel.BuiltAt))
	b.WriteString("<p>代理必须与服务端要求版本一致，不一致时会被拦住并提示更新。")
	b.WriteString("下载二进制后直接运行，不需要签名文件，也不需要安装包。</p>")

	b.WriteString("<h2>macOS 与 Linux</h2>")
	if have["darwin/arm64"] || have["darwin/amd64"] || have["linux/arm64"] || have["linux/amd64"] {
		b.WriteString("<p>在终端里执行这一行。脚本会判定系统与架构、下载并校验、")
		b.WriteString("引导登记、注册服务，装到 <code>~/.local/bin</code>，不需要 <code>sudo</code>：</p>")
		b.WriteString("<pre>curl -fsSL https://" + htmlEscape(host) + "/install.sh | bash</pre>")
		b.WriteString("<p class=\"muted\">登记需要 Bitwarden 邮箱、主密码与两步验证码，")
		b.WriteString("由 <code>dec-agent</code> 直接向终端读取，不经过脚本，也不进命令行参数。")
		b.WriteString("如果这台设备是本服务的第一台设备，先执行 ")
		b.WriteString("<code>export DEC_INITIALIZE=1</code> 再运行上面的命令。</p>")
	}

	b.WriteString("<h2>Windows</h2>")
	if have["windows/amd64"] || have["windows/arm64"] {
		b.WriteString("<p>在 PowerShell 里执行这一行。脚本会判定架构、下载并校验、登记设备、")
		b.WriteString("注册计划任务，装到 <code>%LOCALAPPDATA%\\Programs\\dec</code>，不需要管理员权限：</p>")
		b.WriteString("<pre>iwr -useb https://" + htmlEscape(host) + "/install.ps1 | iex</pre>")
		b.WriteString("<p class=\"muted\">也可以手工安装：下载对应架构的 <code>dec-agent-windows-*.exe</code>，")
		b.WriteString("重命名为 <code>dec-agent.exe</code>，然后依次执行 ")
		b.WriteString("<code>dec-agent configure --url https://" + htmlEscape(host) + "</code>、<code>dec-agent enroll --name win-desktop</code>、<code>dec-agent install</code>。</p>")
	}

	b.WriteString("<h2>登记</h2>")
	b.WriteString("<p>每个平台的一行命令都会走到登记这一步。第一台设备走 ")
	b.WriteString("<code>initialize</code>，会生成根密钥并写入 Bitwarden 冷备；此后新增设备一律走 ")
	b.WriteString("<code>enroll</code>。两者都需要 Bitwarden 邮箱、主密码与两步验证码，")
	b.WriteString("由 <code>dec-agent</code> 直接向终端读取。</p>")
	b.WriteString("<p class=\"muted\">脚本默认按「已有一台设备」处理。只有当这台是本服务的第一台设备时，")
	b.WriteString("才需要先执行 <code>export DEC_INITIALIZE=1</code>（Windows 用 ")
	b.WriteString("<code>$env:DEC_INITIALIZE=1</code>）再运行安装命令。</p>")
	b.WriteString("<details><summary>手工安装（不想用脚本时）</summary>")
	b.WriteString("<p class=\"muted\">下载下面「全部产物」里对应平台的 ")
	b.WriteString("<code>dec-agent-&lt;os&gt;-&lt;arch&gt;</code>，改名为 <code>dec-agent</code>，</p>")
	b.WriteString("<pre>chmod +x ./dec-agent\n")
	b.WriteString("./dec-agent configure --url https://" + htmlEscape(host) + "\n")
	b.WriteString("./dec-agent enroll --name \"$(hostname)\"\n")
	b.WriteString("./dec-agent install</pre>")
	b.WriteString("</details>")

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
	b.WriteString("<p>macOS：脚本在下载后会自动清掉隔离标记。手工安装时在 Finder 里右键 → 打开，")
	b.WriteString("或执行 <code>xattr -d com.apple.quarantine 文件名</code>。</p>")
	b.WriteString("<p>Windows SmartScreen：点击“更多信息”→“仍要运行”。</p>")

	b.WriteString("<h2>卸载</h2>")
	b.WriteString("<pre>dec-agent uninstall</pre>")
	b.WriteString("<p>会移除系统服务、登录项、所有 Dec 管理的落点与本机设备身份，并在在线时通知云端注销这台设备。</p>")

	b.WriteString("<p class=\"muted\">只保留当前与上一个要求版本。</p>")
	b.WriteString("</body></html>")
	_, _ = w.Write([]byte(b.String()))
}

// handleDownload 提供单个文件（需求 20.1）。
//
// 它们和二进制一起由 CI 放进下载目录，服务端不做任何模板渲染：
// 脚本里唯一的变量（服务地址）默认就是本域名，需要改的人自己设
// DEC_URL 环境变量。服务端一旦参与拼字符串，就等于让公网请求能
// 往一份即将被 `| bash` 执行的内容里注入东西。
var installScripts = map[string]struct {
	name        string // 磁盘上的文件名
	contentType string
}{
	"/install.sh":  {"install.sh", "text/x-shellscript; charset=utf-8"},
	"/install.ps1": {"install.ps1", "text/plain; charset=utf-8"},
}

// handleInstallScript 提供一键安装脚本（无证书可访问）。
//
// 为什么要自托管：装客户端这件事不该依赖第二个站点可达。此前 Windows
// 走 GitHub raw，等于 dec.firoyang.com 可用而 GitHub 不可用时，新设备
// 依然装不上。与二进制同源就没有这个耦合。
func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	entry, ok := installScripts[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "只接受 GET", http.StatusMethodNotAllowed)
		return
	}
	if !s.limiter.allow(clientIP(r)) {
		http.Error(w, "请求过于频繁", http.StatusTooManyRequests)
		return
	}

	// 只认下载目录里的直接文件，且路径来自上面的固定映射：
	// 请求方无法影响文件名，所以不存在穿越。
	data, err := os.ReadFile(filepath.Join(s.downloadDir, entry.name))
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "读取文件失败", http.StatusInternalServerError)
		return
	}

	// 必须是 text/*：浏览器与 curl 都不该把它当成要保存的附件，
	// 否则 `curl | bash` 拿到的是 HTTP 头以外的二进制也没人发现。
	w.Header().Set("Content-Type", entry.contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if _, err := w.Write(data); err != nil {
		s.logf("写安装脚本失败: %v", err)
	}
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
