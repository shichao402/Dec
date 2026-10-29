package agent

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"sync"
)

// LegacyTools 是「工具」分区里旧版残留清理的操作（legacy-cleanup 需求 1、3、6）。
//
// 返回值直接序列化给页面；包类型不出现在这里，免得 agent 包依赖 legacy。
// 所有接口只挂在回环管理面上，沿用 Host/Origin 守卫，不经云端、不暴露为 MCP 工具（需求 1.2）。
type LegacyTools struct {
	// Scan 只读扫描。
	Scan func() (any, error)
	// Execute 按页面确认过的扫描指纹执行；代理自己重扫，指纹不一致时拒绝。
	Execute func(fingerprint string) (any, error)
	// ExecuteStream 与 Execute 相同，但把过程进度实时写给 w。
	//
	// 清理要跑几分钟，中途没有反馈的话用户只能靠猜，于是重复点击——
	// 而重复的那次不会并发执行，只会排在上一次后面再跑一遍，删同一批文件。
	// 这个接口让页面能显示「归档 3/12」，从根上消掉重复点击的动机。
	// 为空时回退到 Execute，页面仍可用，只是没有过程进度。
	ExecuteStream func(fingerprint string, w ProgressWriter) (any, error)
	// Archives 列出归档；Archive 读一份归档的条目。
	Archives func() (any, error)
	Archive  func(id string) (any, error)
	// Restore 还原；indexes 为空表示整份还原。
	Restore func(id string, indexes []int) (any, error)
	// DeleteArchive 立即删除一份归档。
	DeleteArchive func(id string) error
}

// ProgressWriter 把执行过程写给调用方。
//
// 只暴露这一个方法，HTTP 层怎么承载（SSE、分块、日志）由实现决定，
// legacy 包不需要知道。
type ProgressWriter interface {
	// Send 发一次进度。实现必须能安全地被长时间调用，
	// 且不需要调用方做缓冲：每次都应该是独立可展示的一帧。
	Send(ProgressEvent) error
}

// ProgressEvent 是发给页面的一帧进度。字段名与页面 JS 侧一致。
type ProgressEvent struct {
	// Step 是阶段名：归档 / 清理 / 提交。
	Step string `json:"step"`
	// Done、Total 是这一阶段的进度；Total 为 0 表示总数未知，不该显示百分比。
	Done  int    `json:"done"`
	Total int    `json:"total"`
	// Detail 是可选的补充，例如正在处理的路径。
	Detail string `json:"detail,omitempty"`
}

// 工具接口的路径。新增工具时在这里挂自己的一组路由，不需要改别的分区（需求 1.3）。
const (
	toolLegacyScan     = "/api/tools/legacy/scan"
	toolLegacyExecute  = "/api/tools/legacy/execute"
	toolLegacyArchives = "/api/tools/legacy/archives"
	toolLegacyArchive  = "/api/tools/legacy/archive"
	toolLegacyRestore  = "/api/tools/legacy/restore"
	toolLegacyDelete   = "/api/tools/legacy/delete"
	// execute/stream 与 execute 做同一件事，只是把过程进度用 SSE 推给页面。
	toolLegacyExecuteStream = "/api/tools/legacy/execute/stream"
)

// sseWriter 把进度帧写成一个 SSE 流。
//
// 每帧都立刻 Flush：缓冲起来的话，页面要等整个清理结束才一次性收到，
// 那就和没有进度一样。
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func (s *sseWriter) Send(ev ProgressEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// mountExecuteStream 挂上流式执行端点。
//
// 用 POST + SSE 而不是 GET + EventSource：执行会删文件，必须是写语义，
// 而且指纹在请求体里，不该出现在 URL（URL 会进代理日志）。
// 页面侧用 fetch 读这个流，EventSource 发不了 POST。
func (l *Loopback) mountExecuteStream(mux *http.ServeMux) {
	mux.HandleFunc(toolLegacyExecuteStream, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		t := l.opts.Legacy
		if t == nil || t.ExecuteStream == nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "未启用"})
			return
		}
		var in struct {
			Fingerprint string `json:"fingerprint"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体不是合法 JSON"})
			return
		}

		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		// X-Accel-Buffering 挡住反向代理的缓冲；这里是回环直连，但页面可能被
		// 从别的端口转发过来，带上更稳。
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		if flusher != nil {
			flusher.Flush()
		}

		pw := &sseWriter{w: w, flusher: flusher}
		out, err := t.ExecuteStream(in.Fingerprint, pw)
		if err != nil {
			// 流已经开始，没法再改状态码，只能把错误作为一帧发出去。
			_ = pw.Send(ProgressEvent{Step: "error", Detail: err.Error()})
			return
		}
		b, err := json.Marshal(out)
		if err != nil {
			_ = pw.Send(ProgressEvent{Step: "error", Detail: err.Error()})
			return
		}
		// 结果用独立事件名发出，与进度帧区分：页面只认 done 这一帧收尾。
		_, _ = fmt.Fprintf(w, "event: result\ndata: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	})
}

func (l *Loopback) mountTools(mux *http.ServeMux) {
	t := l.opts.Legacy
	read := func(fn func(r *http.Request) (any, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if t == nil {
				writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "未启用"})
				return
			}
			out, err := fn(r)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, out)
		}
	}
	// write 包装会动文件的接口：只收 POST + JSON。
	// 跨站表单只能发 form/text 类型；要求 application/json 让浏览器必须先做预检，
	// 这在 Origin 守卫之外再挡一层（Origin 头缺失的老浏览器场景）。
	write := func(fn func(body json.RawMessage) (any, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
			if t == nil {
				writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "未启用"})
				return
			}
			var body json.RawMessage
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
				return
			}
			out, err := fn(body)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, out)
		}
	}
	type idReq struct {
		ID          string `json:"id"`
		Indexes     []int  `json:"indexes"`
		Fingerprint string `json:"fingerprint"`
	}
	decode := func(body json.RawMessage) (idReq, error) {
		var in idReq
		err := json.Unmarshal(body, &in)
		return in, err
	}

	mux.HandleFunc(toolLegacyScan, read(func(*http.Request) (any, error) { return t.Scan() }))
	l.mountExecuteStream(mux)
	mux.HandleFunc(toolLegacyArchives, read(func(*http.Request) (any, error) {
		list, err := t.Archives()
		return map[string]any{"archives": list}, err
	}))
	mux.HandleFunc(toolLegacyArchive, read(func(r *http.Request) (any, error) {
		return t.Archive(r.URL.Query().Get("id"))
	}))
	mux.HandleFunc(toolLegacyExecute, write(func(b json.RawMessage) (any, error) {
		in, err := decode(b)
		if err != nil {
			return nil, err
		}
		return t.Execute(in.Fingerprint)
	}))
	mux.HandleFunc(toolLegacyRestore, write(func(b json.RawMessage) (any, error) {
		in, err := decode(b)
		if err != nil {
			return nil, err
		}
		return t.Restore(in.ID, in.Indexes)
	}))
	mux.HandleFunc(toolLegacyDelete, write(func(b json.RawMessage) (any, error) {
		in, err := decode(b)
		if err != nil {
			return nil, err
		}
		if err := t.DeleteArchive(in.ID); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	}))
}
