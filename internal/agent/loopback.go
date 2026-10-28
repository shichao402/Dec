package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// 回环入口（需求 2.10、13.1、13.2、18.10）。
//
// 只绑 127.0.0.1，只做两项不增加使用摩擦的检查：
//
//  1. Host 头的主机名必须是 127.0.0.1、localhost 或 *.localhost，
//     端口必须是代理自己监听的端口，防 DNS rebinding；
//  2. 请求带了 Origin 头时必须是代理自己的地址，防其他网页跨站调用。
//
// 不设 token、不设一次性链接、不设 CSRF token：本机进程被视为可信，
// 本机失陷就等于这台设备失陷（已定决策 10）。
const (
	// loopbackHost 是唯一绑定地址。
	loopbackHost = "127.0.0.1"
)

// LoopbackOptions 是回环服务的构造参数。
type LoopbackOptions struct {
	// Port 是监听端口，0 表示由系统分配。
	Port int
	// Status 返回状态快照。
	Status func() Status
	// Call 转发一次协议调用，src 决定服务端的权限判定。
	Call func(r *http.Request, m string, src string, payload json.RawMessage) (json.RawMessage, error)
	// MCP 是 /mcp 的处理入口（需求 1.1）。
	//
	// 这里交出去的是完整的 http.Handler，而不是一个「转发一次 JSON」的回调：
	// MCP 是有状态的协议（会话、SSE 流、通知），只靠一个 JSON 进 JSON 出
	// 的函数无法表达握手与流式响应。协议层由官方 SDK 实现，本包只负责
	// 把它挂在正确的路径上，并确保 Host 与 Origin 守卫仍然在外层。
	MCP http.Handler
	// Enroll 处理登记。
	Enroll func(r *http.Request, in LoopbackEnrollInput) (*EnrollResult, error)
	// Recover 处理恢复（根密钥与服务端 CA，ADR 0052）。
	Recover func(r *http.Request, in LoopbackEnrollInput) (*RecoverOutcome, error)
	// Reenroll 用本机原设备密钥重新登记（服务端 CA 重建后）。
	Reenroll func(r *http.Request, in LoopbackEnrollInput) (*EnrollResult, error)
	// Projects 返回本机项目登记。
	Projects func() []Project
	// SaveProjects 保存项目登记。
	SaveProjects func([]Project) error
	// Configure 设置服务地址。
	Configure func(url string) error
	// Logf 是日志出口。
	Logf func(format string, args ...any)
	// LogPath 是日志文件路径，供托盘「查看日志」使用（agent-tray-access 需求 5.3）。
	LogPath string
	// LegacyScan 执行一次只读的旧版残留扫描（legacy-cleanup 需求 3.1），为空表示不提供。
	// 返回值直接序列化给页面；包类型不出现在这里，免得 agent 包反向依赖 legacy。
	LegacyScan func() (any, error)
}

// LoopbackEnrollInput 是页面提交的登记参数。
type LoopbackEnrollInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTP     string `json:"totp,omitempty"`
	Device   string `json:"device_name,omitempty"`
	// DeclareCALost 仅恢复时使用：用户确认服务端 CA 无法找回（ADR 0052）。
	DeclareCALost bool `json:"declare_ca_lost,omitempty"`
}

// Loopback 是本机 HTTP 服务。
type Loopback struct {
	opts LoopbackOptions
	addr string
	srv  *http.Server
	ln   net.Listener
}

// NewLoopback 构造回环服务。
func NewLoopback(opts LoopbackOptions) *Loopback {
	return &Loopback{opts: opts}
}

// Start 开始监听，返回实际地址。
func (l *Loopback) Start() (string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", loopbackHost, l.opts.Port))
	if err != nil {
		return "", fmt.Errorf("监听回环端口失败: %w", err)
	}
	l.ln = ln
	l.addr = ln.Addr().String()
	l.srv = &http.Server{
		Handler:           l.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = l.srv.Serve(ln) }()
	return l.addr, nil
}

// Addr 返回监听地址。
func (l *Loopback) Addr() string { return l.addr }

// Close 停止服务。
func (l *Loopback) Close() error {
	if l.srv == nil {
		return nil
	}
	return l.srv.Close()
}

// Handler 返回处理器，便于测试直接注入。
func (l *Loopback) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", l.handleIndex)
	mux.HandleFunc("/admin", l.handleAdmin)
	mux.HandleFunc("/admin.js", l.handleAdminJS)
	mux.HandleFunc("/api/status", l.handleStatus)
	mux.HandleFunc("/api/configure", l.handleConfigure)
	mux.HandleFunc("/api/enroll", l.handleEnroll)
	mux.HandleFunc("/api/recover", l.handleRecover)
	mux.HandleFunc("/api/reenroll", l.handleReenroll)
	mux.HandleFunc("/api/projects", l.handleProjects)
	mux.HandleFunc("/api/call", l.handleCall)
	mux.HandleFunc("/api/tools/legacy/scan", l.handleLegacyScan)
	mux.Handle("/mcp", l.handleMCP())
	return l.guard(mux)
}

// handleMCP 返回 /mcp 的处理器（需求 1.1、1.9）。
//
// 协议由官方 SDK 的 Streamable HTTP handler 实现，本地完成 initialize 与
// notifications/initialized，不再把它们转发给云端的 initialize（首次初始化）方法。
// Host 与 Origin 守卫在 guard 里包在整个 mux 外层，因此这里同样生效：
// 不带 Origin 头的本机客户端能连，带外部 Origin 的请求会被 403。
func (l *Loopback) handleMCP() http.Handler {
	if l.opts.MCP != nil {
		return l.opts.MCP
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "mcp 未启用", http.StatusNotImplemented)
	})
}

// guard 做 Host 与 Origin 校验（需求 2.10）。
func (l *Loopback) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.hostOK(r.Host) {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !l.originOK(origin) {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowedHostname 报告主机名是否属于本机入口。
//
// 白名单固定为三类：127.0.0.1、localhost、*.localhost。
// *.localhost 按 RFC 6761 只能解析到回环，浏览器也会强制如此，
// 所以接受它与接受 localhost 等价。比较不区分大小写，并去掉末尾的根点。
func allowedHostname(h string) bool {
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	switch {
	case h == loopbackHost, h == "localhost":
		return true
	case strings.HasSuffix(h, ".localhost"):
		label := strings.TrimSuffix(h, ".localhost")
		if label == "" {
			return false
		}
		for _, seg := range strings.Split(label, ".") {
			if seg == "" {
				return false
			}
		}
		return true
	}
	return false
}

// hostOK 校验 Host 头。
//
// 主机名必须在 allowedHostname 白名单内，端口必须是自己监听的端口。
// 网页把攻击者域名解析到 127.0.0.1（DNS rebinding）时，浏览器带的 Host
// 是那个域名，不在白名单里，因此进不来。代理只监听一个高位端口，
// Host 不带端口（隐含 80/443）一律拒绝。
func (l *Loopback) hostOK(host string) bool {
	if host == "" || l.port() == "" {
		return false
	}
	h, p, err := net.SplitHostPort(host)
	if err != nil || p != l.port() {
		return false
	}
	return allowedHostname(h)
}

// originOK 校验 Origin 头。本机页面只经 HTTP 回环端口打开。
func (l *Loopback) originOK(origin string) bool {
	if !strings.HasPrefix(origin, "http://") {
		return false
	}
	return l.hostOK(strings.TrimPrefix(origin, "http://"))
}

// port 返回监听端口。
func (l *Loopback) port() string {
	if l.addr == "" {
		return ""
	}
	_, p, err := net.SplitHostPort(l.addr)
	if err != nil {
		return ""
	}
	return p
}

// handleIndex 返回本机页面。
func (l *Loopback) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

// handleAdmin 返回管理面页面（需求 14.1、14.2）。
//
// 页面本身不含任何数据：一切内容都靠 /api/call 现取。这样页面可以被缓存、
// 被截图、被投屏而不会顺带泄漏资产与密钥。
func (l *Loopback) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 页面内容随二进制一起升级，缓存住就等于升级后人还看着旧页面。
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(adminHTML))
}

// handleAdminJS 返回管理面脚本。
//
// 用 /admin.js 而不是内联：内联脚本会被页面的 CSP 与转义规则牵连，
// 而这里的内容里有大量引号与反引号，外置更不容易出错。
func (l *Loopback) handleAdminJS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	// 脚本与二进制同版本，缓存住会出现“页面已更新、脚本还是旧的”这种半套状态。
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(adminJS))
}

// handleStatus 返回状态（需求 19.5 的“本机页面提示”）。
// handleLegacyScan 返回旧版残留的只读扫描结果（legacy-cleanup 需求 3.1）。
//
// 只读，不改任何文件，所以用 GET；执行清理另有接口（任务 6）。
func (l *Loopback) handleLegacyScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if l.opts.LegacyScan == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "未启用"})
		return
	}
	out, err := l.opts.LegacyScan()
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (l *Loopback) handleStatus(w http.ResponseWriter, r *http.Request) {
	out := l.localInfo()
	if l.opts.Status == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	st := l.opts.Status()
	for k, v := range map[string]any{
		"server_url":        st.ServerURL,
		"version":           st.Version,
		"device_id":         st.DeviceID,
		"aligned":           st.Aligned,
		"server_version":    st.ServerVersion,
		"required":          st.RequiredVer,
		"maintenance":       st.Maintenance,
		"last_sync":         st.LastSync,
		"last_error":        st.LastError,
		"revision":          st.Revision,
		"download_url":      st.DownloadURL,
		"ca_state":          st.CAState,
		"ca_backup_pending": st.CABackupPending,
		"error_code":        st.ErrorCode,
	} {
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}

// handleConfigure 设置服务地址。
func (l *Loopback) handleConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		ServerURL string `json:"server_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if l.opts.Configure == nil {
		http.Error(w, "unsupported", http.StatusNotImplemented)
		return
	}
	if err := l.opts.Configure(in.ServerURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleEnroll 处理登记。
func (l *Loopback) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in LoopbackEnrollInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if l.opts.Enroll == nil {
		http.Error(w, "unsupported", http.StatusNotImplemented)
		return
	}
	res, err := l.opts.Enroll(r, in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_id":  res.DeviceID,
		"not_after":  res.NotAfter,
		"is_first":   res.IsFirst,
		"server_url": res.ServerURL,
	})
}

// handleRecover 处理恢复。
func (l *Loopback) handleRecover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in LoopbackEnrollInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if l.opts.Recover == nil {
		http.Error(w, "unsupported", http.StatusNotImplemented)
		return
	}
	res, err := l.opts.Recover(r, in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
			"code":  string(CodeOf(err)),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_id":         res.DeviceID,
		"not_after":         res.NotAfter,
		"enrolled":          res.Enrolled,
		"root_key_restored": res.RootKeyRestored,
		"ca_restored":       res.CARestored,
		"ca_rebuilt":        res.CARebuilt,
		"reenroll_devices":  res.ReenrollDevices,
	})
}

// handleReenroll 用本机原设备密钥重新登记（服务端 CA 重建后，ADR 0052）。
func (l *Loopback) handleReenroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in LoopbackEnrollInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if l.opts.Reenroll == nil {
		http.Error(w, "unsupported", http.StatusNotImplemented)
		return
	}
	res, err := l.opts.Reenroll(r, in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "code": string(CodeOf(err))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_id": res.DeviceID,
		"not_after": res.NotAfter,
	})
}

// handleProjects 读写本机项目登记。
func (l *Loopback) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var out []Project
		if l.opts.Projects != nil {
			out = l.opts.Projects()
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": out})
	case http.MethodPost:
		var in []Project
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if l.opts.SaveProjects == nil {
			http.Error(w, "unsupported", http.StatusNotImplemented)
			return
		}
		if err := l.opts.SaveProjects(in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCall 转发一次协议调用。
//
// 来源由路径决定，调用方不能自己声明：管理面转发固定为 admin，
// MCP 入口固定为 mcp。这样“MCP 读不到明文”这类规则无法被绕过（已决策 12）。
func (l *Loopback) handleCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Method  string          `json:"method"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if l.opts.Call == nil {
		http.Error(w, "unsupported", http.StatusNotImplemented)
		return
	}
	out, err := l.opts.Call(r, in.Method, string(protoSourceAdmin), in.Payload)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// writeJSON 输出 JSON。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
