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

// handleDownloadIndex 是无证书可访问的下载索引页（需求 20.1）。
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
	b.WriteString("<title>Dec Cloud 客户端下载</title>")
	b.WriteString("<style>body{font:14px/1.6 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;")
	b.WriteString("max-width:760px;margin:40px auto;padding:0 20px;color:#222}")
	b.WriteString("code,pre{background:#f4f4f5;border-radius:4px}code{padding:2px 5px}")
	b.WriteString("pre{padding:12px;overflow-x:auto}table{border-collapse:collapse;width:100%}")
	b.WriteString("th,td{text-align:left;padding:6px 10px;border-bottom:1px solid #eee}")
	b.WriteString("h1{font-size:20px}h2{font-size:15px;margin-top:28px}")
	b.WriteString(".muted{color:#888}</style></head><body>")
	b.WriteString("<h1>Dec Cloud 客户端下载</h1>")

	if rel == nil {
		b.WriteString("<p class=\"muted\">尚未上传发布清单。请先完成一次正式部署。</p>")
	} else {
		fmt.Fprintf(&b, "<p>当前要求版本：<code>%s</code>", htmlEscape(rel.RequiredClient))
		fmt.Fprintf(&b, "<span class=\"muted\">（服务端 %s，协议 %d，构建于 %s）</span></p>",
			htmlEscape(rel.Version), rel.Protocol, htmlEscape(rel.BuiltAt))

		b.WriteString("<h2>下载</h2><table><tr><th>平台</th><th>架构</th><th>文件</th><th>大小</th></tr>")
		files := append([]releaseFile(nil), rel.Files...)
		sort.Slice(files, func(i, j int) bool {
			if files[i].OS != files[j].OS {
				return files[i].OS < files[j].OS
			}
			return files[i].Arch < files[j].Arch
		})
		for _, f := range files {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td><code>%s</code></td><td>%s</td></tr>",
				htmlEscape(f.OS), htmlEscape(f.Arch), htmlEscape(f.Name), humanSize(f.Size))
		}
		b.WriteString("</table>")

		b.WriteString("<h2>用命令行下载</h2>")
		b.WriteString("<p>建议用 <code>curl</code> 下载，避免浏览器给文件打上隔离标记：</p>")
		for _, f := range files {
			// 命令必须与代理真实子命令一致：注册系统服务是 `install`，不是 `--install-agent`。
			// 写错的后果是用户照着敲一遍，得到一句「未知命令」，然后卡在第一步。
			fmt.Fprintf(&b, "<pre>curl -fLO https://%s/download/%s\nchmod +x %s\nsudo mv %s /usr/local/bin/dec-agent\n"+
				"dec-agent configure --url https://%s\ndec-agent install\ndec-agent initialize</pre>",
				htmlEscape(host), htmlEscape(f.Name), htmlEscape(f.Name), htmlEscape(f.Name), htmlEscape(host))
			break
		}
		b.WriteString("<p class=\"muted\">第一台设备用 <code>initialize</code>（会生成根密钥并写入 Bitwarden 冷备）；")
		b.WriteString("此后新增设备用 <code>enroll</code>，两者都需要 Bitwarden 主密码与二次验证码。</p>")

		b.WriteString("<h2>被系统拦截时</h2>")
		b.WriteString("<p>客户端二进制不做代码签名（本项目只有一个使用者，签名与公证的成本远大于收益）。</p>")
		b.WriteString("<p>macOS Gatekeeper：用 <code>curl</code> 下载后，在 Finder 里右键 → 打开，")
		b.WriteString("或执行 <code>xattr -d com.apple.quarantine 文件名</code>。</p>")
		b.WriteString("<p>Windows SmartScreen：点击“更多信息”→“仍要运行”。</p>")
	}

	b.WriteString("<p class=\"muted\">只保留当前与上一个要求版本。</p>")
	b.WriteString("</body></html>")
	_, _ = w.Write([]byte(b.String()))
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

// clientIP 取客户端 IP，用 RemoteAddr 而不信任任何转发头：
// 服务端前面没有反代，转发头只会被伪造。
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
