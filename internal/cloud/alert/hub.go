// Package alert 实现告警的两条通道（需求 12）。
//
// 通道一：企业微信群机器人 webhook。地址作为 Dec 密钥存储，
// 不写在配置文件里，这样它能跟着资产一起被加密与冷备。
//
// 通道二：在线“我的设备”上的代理系统通知。云端无法主动连代理，
// 所以告警挂在长轮询的 sync 响应里带下去；设备不在线就自然跳过，
// 下次连上后只补最近的，不重放历史。
//
// 同类告警在窗口期内合并：一条告警带 Count，而不是刷几十条一样的。
package alert

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Kind 是告警类别。合并按类别做，所以类别必须稳定、数量有限。
type Kind string

const (
	// KindRecovery 服务进入恢复模式（需求 11.4）。
	KindRecovery Kind = "recovery"
	// KindEnroll 设备登记尝试，成功与失败都告警（需求 18.6）。
	KindEnroll Kind = "enroll"
	// KindBackupFailed 冷备或审计推送失败（需求 15.4）。
	KindBackupFailed Kind = "backup_failed"
	// KindRemoteRewritten 冷备远端历史被改写（需求 15.2）。
	KindRemoteRewritten Kind = "remote_rewritten"
	// KindRenderSecret 渲染时密钥解密失败（需求 4.5）。
	KindRenderSecret Kind = "render_secret"
	// KindDeploy 部署失败或回滚（需求 21.3）。
	KindDeploy Kind = "deploy"
	// KindLocalTLS 本机证书 local.dec.firoyang.com 签发或续期失败（agent-tray-access 需求 4.8）。
	KindLocalTLS Kind = "local_tls"
)

// Alert 是一条告警。
type Alert struct {
	// ID 是稳定标识，代理据此去重。
	ID string `json:"id"`
	// Seq 是递增序号，设备用它判断“这条我看过没有”。
	Seq uint64 `json:"seq"`
	// Kind 是类别。
	Kind Kind `json:"kind"`
	// Title 是一句话标题，直接拿去弹系统通知。
	Title string `json:"title"`
	// Detail 是补充信息。绝不放密钥明文与密码。
	Detail string `json:"detail,omitempty"`
	// At 是首次发生时间。
	At time.Time `json:"at"`
	// Count 是窗口期内合并的次数。
	Count int `json:"count"`
}

// Options 是告警中心的构造参数。
type Options struct {
	// WebhookSecret 返回企业微信 webhook 地址。
	//
	// 做成函数是为了每次发送时才去解密：服务启动时根密钥可能还没加载，
	// 而且用户随时可以换地址，缓存住反而会发出到旧群。
	WebhookSecret func() (string, error)
	// MergeWindow 是同类告警的合并窗口，默认 5 分钟。
	MergeWindow time.Duration
	// Keep 是内存里保留的告警条数，默认 200。
	Keep int
	// Client 用于发送 webhook，默认 10 秒超时。
	Client *http.Client
	// Logf 是日志出口。
	Logf func(format string, args ...any)
}

// Hub 是告警中心。
type Hub struct {
	opts Options

	mu     sync.Mutex
	seq    uint64
	items  []Alert
	cursor map[string]uint64
	lastAt map[Kind]time.Time

	// sendCh 把 webhook 发送放到后台，避免业务路径被网络请求拖住。
	sendCh chan Alert
}

// New 构造告警中心并启动后台发送协程。
//
// ctx 结束时不关闭 sendCh：调用方可能还在 Raise，关闭会 panic。
// 发送协程靠 ctx 退出，句柄随之被回收。
func New(ctx context.Context, opts Options) *Hub {
	if opts.MergeWindow <= 0 {
		opts.MergeWindow = 5 * time.Minute
	}
	if opts.Keep <= 0 {
		opts.Keep = 200
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	h := &Hub{
		opts:   opts,
		cursor: map[string]uint64{},
		lastAt: map[Kind]time.Time{},
		sendCh: make(chan Alert, 64),
	}
	go h.run(ctx)
	return h
}

// Raise 记录一条告警，并在需要时异步发出 webhook。
//
// 返回最终生效的告警（合并时是同一条，Count 已加一）。
func (h *Hub) Raise(kind Kind, title, detail string) Alert {
	h.mu.Lock()
	now := time.Now().UTC()

	// 合并窗口内同类告警：累加计数，不新建条目，也不重发 webhook。
	if last, ok := h.lastAt[kind]; ok && now.Sub(last) < h.opts.MergeWindow {
		for i := len(h.items) - 1; i >= 0; i-- {
			if h.items[i].Kind != kind {
				continue
			}
			h.items[i].Count++
			h.items[i].At = now
			out := h.items[i]
			h.mu.Unlock()
			return out
		}
	}

	h.seq++
	item := Alert{
		ID:     "al-" + h.seqID(),
		Seq:    h.seq,
		Kind:   kind,
		Title:  title,
		Detail: detail,
		At:     now,
		Count:  1,
	}
	h.items = append(h.items, item)
	if len(h.items) > h.opts.Keep {
		h.items = h.items[len(h.items)-h.opts.Keep:]
	}
	h.lastAt[kind] = now
	h.mu.Unlock()

	select {
	case h.sendCh <- item:
	default:
		// 队列满说明 webhook 长期不通，丢弃这一条而不是阻塞业务。
		h.opts.Logf("告警发送队列已满，丢弃告警: %s", item.Title)
	}
	return item
}

// Pending 返回该设备尚未看到的告警，并推进它的游标。
//
// 游标存在内存里：进程重启后从“当前时刻”重新开始，不重放历史。
// 这是有意的取舍——重启后重放几十条旧告警只会让人把通知关掉。
func (h *Hub) Pending(deviceID string) []Alert {
	if deviceID == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cur := h.cursor[deviceID]
	var out []Alert
	for _, it := range h.items {
		if it.Seq > cur {
			out = append(out, it)
		}
	}
	if len(out) > 0 {
		h.cursor[deviceID] = h.seq
	}
	return out
}

// Recent 返回最近的告警，管理面用它展示（需求 14.1）。
func (h *Hub) Recent(limit int) []Alert {
	h.mu.Lock()
	defer h.mu.Unlock()
	if limit <= 0 || limit > len(h.items) {
		limit = len(h.items)
	}
	out := make([]Alert, 0, limit)
	for i := len(h.items) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, h.items[i])
	}
	return out
}

// run 是 webhook 发送循环。
func (h *Hub) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case a := <-h.sendCh:
			if err := h.send(ctx, a); err != nil {
				// webhook 失败不告警：告警通道自己挂了再发告警只会递归。
				h.opts.Logf("企业微信告警发送失败: %v", err)
			}
		}
	}
}

// send 把告警推给企业微信群机器人。
func (h *Hub) send(ctx context.Context, a Alert) error {
	if h.opts.WebhookSecret == nil {
		return nil
	}
	url, err := h.opts.WebhookSecret()
	if err != nil {
		return fmt.Errorf("读取 webhook 密钥失败: %w", err)
	}
	if url == "" {
		return nil
	}

	text := a.Title
	if a.Detail != "" {
		text += "\n" + a.Detail
	}
	if a.Count > 1 {
		text += fmt.Sprintf("\n（同类告警在窗口期内共 %d 次）", a.Count)
	}
	body, err := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": text},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.opts.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook 返回 %d: %s", resp.StatusCode, string(raw))
	}
	// 企业微信在 HTTP 200 里用 errcode 表示业务失败。
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &out); err == nil && out.ErrCode != 0 {
		return fmt.Errorf("webhook 业务错误 %d: %s", out.ErrCode, out.ErrMsg)
	}
	return nil
}

// seqID 生成告警 ID 的随机后缀。
func (h *Hub) seqID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Kinds 返回全部告警类别，便于测试锁定集合。
func Kinds() []Kind {
	out := []Kind{
		KindRecovery, KindEnroll, KindBackupFailed,
		KindRemoteRewritten, KindRenderSecret, KindDeploy, KindLocalTLS,
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
