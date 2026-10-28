package agent

import (
	"encoding/json"
	"mime"
	"net/http"
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
	// Archives 列出归档；Archive 读一份归档的条目。
	Archives func() (any, error)
	Archive  func(id string) (any, error)
	// Restore 还原；indexes 为空表示整份还原。
	Restore func(id string, indexes []int) (any, error)
	// DeleteArchive 立即删除一份归档。
	DeleteArchive func(id string) error
}

// 工具接口的路径。新增工具时在这里挂自己的一组路由，不需要改别的分区（需求 1.3）。
const (
	toolLegacyScan     = "/api/tools/legacy/scan"
	toolLegacyExecute  = "/api/tools/legacy/execute"
	toolLegacyArchives = "/api/tools/legacy/archives"
	toolLegacyArchive  = "/api/tools/legacy/archive"
	toolLegacyRestore  = "/api/tools/legacy/restore"
	toolLegacyDelete   = "/api/tools/legacy/delete"
)

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
