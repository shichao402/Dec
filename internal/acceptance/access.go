package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/agent"
)

// 本机入口验收（agent-tray-access 需求 4.1、5.1）。
//
// 两种访问方式各验一遍，而且都要真的发请求：
//
//   - admin_url：托盘窗口加载的就是它，必须真能打开管理面；
//   - http://dec.localhost:<端口>：解析由浏览器按 RFC 6761 强制落到回环，
//     这里同样钉到回环，验的是代理的 Host 白名单真的放行了它；
//   - 日志：只写 logs/agent.log（及其轮转文件）。

// accessStatus 是 /api/status 中本机入口相关的字段。
type accessStatus struct {
	DeviceID string            `json:"device_id"`
	HTTPAddr string            `json:"http_addr"`
	URLs     []agent.AccessURL `json:"urls"`
	AdminURL string            `json:"admin_url"`
	LogPath  string            `json:"log_path"`
}

// fileStamp 是日志目录里一个文件的大小与修改时间。
type fileStamp struct {
	size  int64
	mtime time.Time
}

// checkAccess 依次验证两种访问地址，并给日志目录拍快照。
func (r *runner) checkAccess(ctx context.Context) {
	r.section("一（续）、本机入口")
	code, body, err := r.get(ctx, "/api/status")
	if err != nil || code != http.StatusOK {
		r.step("本机入口字段", false, fmt.Sprintf("读取 /api/status 失败：%d %v", code, err))
		return
	}
	var st accessStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		r.step("本机入口字段", false, "解析失败："+err.Error())
		return
	}
	if len(st.URLs) == 0 || st.HTTPAddr == "" {
		// 旧版代理没有这些字段：说明本机还没装上含托盘功能的版本。
		r.step("本机入口字段", false, "status 不含 urls/http_addr，本机代理版本过旧")
		return
	}
	r.step("本机入口字段", true, "首选 "+st.AdminURL)

	r.checkAdminURL(ctx, st)
	r.checkDotLocalhost(ctx, st)

	r.logPath = st.LogPath
	r.logSnap = snapshotLogs(st.LogPath)
}

// checkAdminURL 验证 admin_url 指向回环且真能打开管理面。
//
// 托盘窗口与「打开管理面」都直接加载这个地址，它错了用户就进不去。
func (r *runner) checkAdminURL(ctx context.Context, st accessStatus) {
	base, ok := strings.CutSuffix(st.AdminURL, "/admin")
	if !ok || !strings.HasPrefix(base, "http://127.0.0.1:") {
		r.step("管理面地址可访问", false, "admin_url 不是回环地址："+st.AdminURL)
		return
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	got, err := fetchDeviceID(ctx, client, base+"/api/status")
	if err != nil {
		r.step("管理面地址可访问", false, err.Error())
		return
	}
	if got != st.DeviceID {
		r.step("管理面地址可访问", false, "应答来自别的进程（device_id="+got+"）")
		return
	}
	r.step("管理面地址可访问", true, st.AdminURL)
}

// checkDotLocalhost 验证 http://dec.localhost:<端口> 可访问（需求 4.1）。
//
// 不依赖本机解析器：Go 与部分系统解析器不一定把 *.localhost 落到回环，
// 但浏览器一定会（RFC 6761）。这里把连接钉到实际监听地址，Host 头保持
// dec.localhost，验的正是代理那一侧的白名单。
func (r *runner) checkDotLocalhost(ctx context.Context, st accessStatus) {
	_, port, err := net.SplitHostPort(st.HTTPAddr)
	if err != nil {
		r.step("dec.localhost 可访问", false, "http_addr 不合法："+st.HTTPAddr)
		return
	}
	target := "http://dec.localhost:" + port
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, st.HTTPAddr)
			},
		},
	}
	got, err := fetchDeviceID(ctx, client, target+"/api/status")
	if err != nil {
		r.step("dec.localhost 可访问", false, err.Error())
		return
	}
	if got != st.DeviceID {
		r.step("dec.localhost 可访问", false, "应答来自别的进程（device_id="+got+"）")
		return
	}
	r.step("dec.localhost 可访问", true, target)
}

// fetchDeviceID 请求一个 /api/status 地址并返回其中的 device_id。
func fetchDeviceID(ctx context.Context, client *http.Client, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s 返回 %d", url, resp.StatusCode)
	}
	var out struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("%s 不是 status JSON: %w", url, err)
	}
	return out.DeviceID, nil
}

// snapshotLogs 记录日志目录下每个文件的大小与修改时间。
func snapshotLogs(logPath string) map[string]fileStamp {
	out := map[string]fileStamp{}
	if logPath == "" {
		return out
	}
	entries, err := os.ReadDir(filepath.Dir(logPath))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out[e.Name()] = fileStamp{size: info.Size(), mtime: info.ModTime()}
	}
	return out
}

// isAgentLog 报告文件名是否是唯一日志或它的轮转文件。
func isAgentLog(name string) bool {
	if name == agent.LogFileName {
		return true
	}
	rest, ok := strings.CutPrefix(name, agent.LogFileName+".")
	if !ok || rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// checkSingleLog 验证验收期间只有 agent.log（及轮转）发生变化（需求 5.1）。
func (r *runner) checkSingleLog() {
	if r.logPath == "" {
		// checkAccess 没拿到入口字段时已经记过 FAIL，这里不重复。
		return
	}
	if filepath.Base(r.logPath) != agent.LogFileName {
		r.step("日志只写单文件", false, "status 报告的日志路径不是 "+agent.LogFileName+"："+r.logPath)
		return
	}
	after := snapshotLogs(r.logPath)
	var changed, legacy []string
	for name, now := range after {
		if isAgentLog(name) {
			continue
		}
		before, existed := r.logSnap[name]
		if !existed || before.size != now.size || !before.mtime.Equal(now.mtime) {
			changed = append(changed, name)
			continue
		}
		legacy = append(legacy, name)
	}
	sort.Strings(changed)
	sort.Strings(legacy)
	if len(changed) > 0 {
		r.step("日志只写单文件", false, "验收期间这些文件也在变化："+strings.Join(changed, ","))
		return
	}
	detail := r.logPath
	if len(legacy) > 0 {
		// 旧版本留下的文件不算失败（没再被写），但提示可以删掉。
		detail += "；旧文件 " + strings.Join(legacy, ",") + " 已不再写入，可删除"
	}
	r.step("日志只写单文件", true, detail)
}
