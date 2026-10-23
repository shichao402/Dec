package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// 同步主循环（需求 2.4、4.2、5.1、5.4、6.2、19.5）。
//
// 代理的全部在线行为都在这里：握手、对账、落地、回执、长轮询。
// 没有自更新，没有本地清单，也没有任何“把本地改动推回云端”的路径。

// Options 是代理运行参数。
type Options struct {
	// ServerURL 是服务地址。
	ServerURL string
	// Version 是代理版本。
	Version string
	// StateDir 是代理的本地状态目录（身份、配置）。
	StateDir string
	// DeviceName 是上报给服务端的可读名字。
	DeviceName string
	// HomeDir 覆盖用户目录，为空表示取当前用户的真实目录。
	//
	// 存在的理由只有一个：落点全都在用户目录之下，测试必须能换一个目录，
	// 否则跑一次用例就会把真实机器上的 IDE 配置改掉。
	HomeDir string
	// LongPoll 是长轮询等待秒数。
	LongPoll time.Duration
	// Heartbeat 是空闲时的最长等待，也是哈希上报的周期。
	Heartbeat time.Duration
	// DisconnectAlert 是断连多久后发本机通知（需求 12.3）。
	DisconnectAlert time.Duration
	// WatchInterval 是监听落点变化的轮询间隔（需求 4.7）。
	WatchInterval time.Duration
	// Notifier 用于发系统通知，可为空。
	Notifier Notifier
	// Logf 是日志出口，可为空。
	Logf func(format string, args ...any)
	// Insecure 仅用于本机调试。
	Insecure bool
	// LocalTLS 是本机证书存储，为空表示不接收证书（agent-tray-access 需求 4.5）。
	LocalTLS *LocalTLSStore
	// OnLocalTLS 在新证书落盘后调用，用于热加载 HTTPS。
	OnLocalTLS func()
}

// Notifier 是系统通知接口，便于在无桌面环境下降级为空实现。
type Notifier interface {
	// Notify 发一条本机通知。
	Notify(title, body string) error
}

// Syncer 是同步主循环。
type Syncer struct {
	opts     Options
	client   *Client
	identity *IdentityStore
	writer   *Writer
	resolver *Resolver

	mu       sync.Mutex
	projects []Project
	// checks 是上一轮云端给出的核对清单，本轮回填哈希。
	checks []proto.ManifestEntry
	// lastOK 是最后一次成功对账的时间，用于断连告警。
	lastOK time.Time
	// 状态展示。
	lastErr     error
	lastSync    time.Time
	aligned     bool
	serverVer   string
	requiredVer string
	maintenance bool
	syncedRev   string
	// lastLoggedErr 是上一次写入日志的同步失败原因，用于去重。
	lastLoggedErr string
}

// NewSyncer 构造同步器。
func NewSyncer(opts Options, id *IdentityStore) (*Syncer, error) {
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.LongPoll <= 0 {
		opts.LongPoll = 30 * time.Second
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 10 * time.Minute
	}
	if opts.DisconnectAlert <= 0 {
		opts.DisconnectAlert = 30 * time.Minute
	}
	if opts.WatchInterval <= 0 {
		opts.WatchInterval = time.Second
	}
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	home, err := opts.home()
	if err != nil {
		return nil, err
	}
	client, err := NewClient(ClientOptions{
		BaseURL:  opts.ServerURL,
		Version:  opts.Version,
		Identity: id.Load2,
		Insecure: opts.Insecure,
	})
	if err != nil {
		return nil, err
	}
	resolver := NewResolver(home, nil)
	return &Syncer{
		opts:     opts,
		client:   client,
		identity: id,
		writer:   NewWriter(resolver, home),
		resolver: resolver,
	}, nil
}

// SetProjects 更新本机项目登记投影，供落点展开使用。
func (s *Syncer) SetProjects(projects []Project) {
	s.mu.Lock()
	s.projects = append([]Project(nil), projects...)
	s.mu.Unlock()
	s.rebuildResolver()
}

// Client 返回底层协议客户端。
//
// 回环入口的管理面转发、登记、恢复、卸载都要用同一个客户端：
// 它持有服务地址、版本号与身份来源，重建一个会和主循环的状态脱节。
func (s *Syncer) Client() *Client { return s.client }

// home 返回生效的用户目录。
func (o Options) home() (string, error) {
	if o.HomeDir != "" {
		return o.HomeDir, nil
	}
	return HomeDir()
}

// rebuildResolver 用当前 home 与项目重建展开器。
func (s *Syncer) rebuildResolver() {
	home, err := s.opts.home()
	if err != nil {
		return
	}
	s.mu.Lock()
	projects := append([]Project(nil), s.projects...)
	s.mu.Unlock()
	resolver := NewResolver(home, projects)
	s.mu.Lock()
	s.resolver = resolver
	s.writer = NewWriter(resolver, home)
	s.mu.Unlock()
}

// Status 是代理当前状态快照，供本机页面展示。
type Status struct {
	ServerURL     string
	Version       string
	DeviceID      string
	Aligned       bool
	ServerVersion string
	RequiredVer   string
	Maintenance   bool
	LastSync      time.Time
	LastError     string
	Revision      string
	DownloadURL   string
}

// Status 返回状态快照。
func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{
		ServerURL:     s.client.BaseURL(),
		Version:       s.opts.Version,
		Aligned:       s.aligned,
		ServerVersion: s.serverVer,
		RequiredVer:   s.requiredVer,
		Maintenance:   s.maintenance,
		LastSync:      s.lastSync,
		Revision:      s.syncedRev,
		DownloadURL:   s.client.DownloadURL(),
	}
	if s.lastErr != nil {
		st.LastError = s.lastErr.Error()
	}
	if id, err := s.identity.Load(); err == nil {
		st.DeviceID = id.DeviceID
	}
	return st
}

// Run 启动主循环，阻塞到 ctx 结束。
//
// 这是“长连接”的实现：HTTP 上不存在真正的长连接，长轮询就是它的等价物。
// 一次 sync 挂起 LongPoll 秒，服务端一有变化立刻返回，等于推送。
func (s *Syncer) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := s.tick(ctx)
		if err == nil {
			backoff = time.Second
			s.mu.Lock()
			s.lastLoggedErr = ""
			s.mu.Unlock()
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
		s.setErr(err)
		s.logSyncFailure(err)
		// 版本不对齐与身份被拒都不该高频重试：它们需要人处理。
		if errors.Is(err, ErrNotAligned) {
			s.notifyVersionMismatch()
			if !sleepCtx(ctx, s.opts.Heartbeat) {
				return nil
			}
			continue
		}
		if errors.Is(err, ErrIdentityRejected) || errors.Is(err, ErrNoIdentity) {
			s.notifyIdentityProblem(err)
			if !sleepCtx(ctx, s.opts.Heartbeat) {
				return nil
			}
			continue
		}
		// 离线降级：保持已落地文件不变，只重连（需求 5.1）。
		s.maybeNotifyDisconnect()
		if !sleepCtx(ctx, backoff) {
			return nil
		}
		backoff *= 2
		if backoff > timeoutCap {
			backoff = timeoutCap
		}
	}
}

// timeoutCap 是退避上限。
const timeoutCap = 2 * time.Minute

// tick 完成一轮握手与对账。
func (s *Syncer) tick(ctx context.Context) error {
	hello, err := s.hello(ctx)
	if err != nil {
		return err
	}
	if !hello.Aligned {
		return fmt.Errorf("%w: 需要 %s", ErrNotAligned, hello.RequiredVersion)
	}
	if hello.Maintenance {
		// 维护状态不改动任何落点（需求 21.4）。
		s.setMaintenance(true)
		return fmt.Errorf("%w: %s", ErrMaintenance, "服务端正在部署")
	}
	s.setMaintenance(false)

	// 未登记则直接返回：登记要由用户在本机页面上完成。
	if _, err := s.identity.Load(); err != nil {
		return err
	}
	return s.syncOnce(ctx)
}

// hello 做一次握手。
func (s *Syncer) hello(ctx context.Context) (*proto.HelloResponse, error) {
	osName, arch := OSArch()
	in := proto.HelloRequest{
		AgentVersion: s.opts.Version,
		OS:           osName,
		Arch:         arch,
		DeviceName:   s.opts.DeviceName,
	}
	out, err := s.client.Hello(ctx, in)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.aligned = out.Aligned
	s.serverVer = out.ServerVersion
	s.requiredVer = out.RequiredVersion
	s.maintenance = out.Maintenance
	s.mu.Unlock()
	return out, nil
}

// syncOnce 发起一次对账并落地。
func (s *Syncer) syncOnce(ctx context.Context) error {
	s.mu.Lock()
	checks := append([]proto.ManifestEntry(nil), s.checks...)
	projects := append([]Project(nil), s.projects...)
	rev := s.syncedRev
	s.mu.Unlock()

	// 按云端上一轮给的形状现场算哈希（需求 5.2）。
	manifest := BuildManifest(checks)

	in := proto.SyncRequest{
		Revision:    rev,
		Manifest:    manifest,
		Projects:    LocalProjects(projects),
		WaitSeconds: int(s.opts.LongPoll / time.Second),
	}
	if s.opts.LocalTLS != nil {
		in.LocalTLSSerial = s.opts.LocalTLS.Serial()
	}

	// 长轮询会挂起最多 LongPoll 秒。这期间如果用户在本机改了落点，
	// 等满超时才上报就等于“改动要等半分钟才被记录”，所以挂一个监听：
	// 一旦发现哈希变了就取消本次等待，让下一轮立刻把新哈希报上去（需求 4.7）。
	watchCtx, stopWatch := context.WithCancel(ctx)
	changed := make(chan struct{}, 1)
	go s.watchLocal(watchCtx, checks, manifest, changed)

	resp, err := s.client.Sync(watchCtx, in)
	// 拿到响应就停止监听：本轮自己写下的文件不该被当成用户改动。
	stopWatch()
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		// 本地变化引起的取消不是故障：这一轮什么都不用做，直接重来。
		select {
		case <-changed:
			return nil
		default:
		}
		return err
	}

	// 记住本轮核对清单，下一轮回填哈希。
	s.mu.Lock()
	s.checks = resp.Checks
	s.mu.Unlock()

	// 本机证书与资产下发互不影响：落盘失败只记日志，下一轮会重新下发。
	s.applyLocalTLS(resp.LocalTLS)

	if resp.Batch == nil {
		s.markOK("")
		s.deliverAlerts(resp.Alerts)
		return nil
	}

	results, rev := s.applyBatch(*resp.Batch)
	s.markOK(rev)
	s.deliverAlerts(resp.Alerts)

	// 回执失败不回滚已落地内容：下次对账会重发，内容幂等（需求 4.4）。
	if err := s.client.Ack(ctx, proto.AckRequest{
		Revision: rev,
		Results:  results,
	}); err != nil {
		return err
	}
	return nil
}

// watchLocal 监听落点变化，发现变化就通知调用方结束长轮询（需求 4.7）。
//
// 用轮询而不是文件系统事件：三个平台的 inotify / FSEvents / ReadDirectoryChangesW
// 语义各不相同，而代理只需要“变化被发现得足够快”。轮询间隔默认 1 秒，
// 代价是每次对每个落点做一次 stat 加一次读取哈希，对几十个文件可以忽略。
//
// 只在长轮询挂起期间跑，两轮之间不占用任何资源。
func (s *Syncer) watchLocal(ctx context.Context, checks, initial []proto.ManifestEntry, changed chan<- struct{}) {
	if len(checks) == 0 {
		return
	}
	base := hashIndex(initial)
	ticker := time.NewTicker(s.opts.WatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := hashIndex(BuildManifest(checks))
			if sameIndex(base, now) {
				continue
			}
			// 只通知一次：上面的 select 有缓冲通道，重复通知也不会阻塞。
			select {
			case changed <- struct{}{}:
			default:
			}
			return
		}
	}
}

// hashIndex 把清单投影成 ID -> 哈希，便于比较。
func hashIndex(entries []proto.ManifestEntry) map[string]string {
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		out[e.ID] = e.Hash
	}
	return out
}

// sameIndex 比较两份哈希索引是否一致。
func sameIndex(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// applyBatch 落地一个 batch，返回回执结果与 revision。
func (s *Syncer) applyBatch(b proto.Batch) ([]proto.AckResult, string) {
	results := make([]proto.AckResult, 0, len(b.Files)+len(b.Deletes))
	for _, f := range b.Files {
		res, err := s.writer.Apply(f)
		if err != nil {
			s.opts.Logf("写入 %s 失败: %v", f.ID, err)
			results = append(results, proto.AckResult{
				ID:    f.ID,
				OK:    false,
				Error: err.Error(),
			})
			continue
		}
		results = append(results, proto.AckResult{
			ID:    res.ID,
			OK:    true,
			Hash:  res.Hash,
			Paths: res.Paths,
		})
	}
	for _, f := range b.Deletes {
		if _, err := s.writer.Remove(f); err != nil {
			s.opts.Logf("删除 %s 失败: %v", f.ID, err)
			results = append(results, proto.AckResult{
				ID:    f.ID,
				OK:    false,
				Error: err.Error(),
			})
			continue
		}
		// 删除的 Hash 为空：服务端据此把条目从状态里移除。
		results = append(results, proto.AckResult{ID: f.ID, OK: true})
	}
	return results, b.Revision
}

// markOK 记录一次成功对账。
func (s *Syncer) markOK(revision string) {
	s.mu.Lock()
	s.lastErr = nil
	s.lastSync = time.Now()
	s.lastOK = s.lastSync
	if revision != "" {
		s.syncedRev = revision
	}
	s.mu.Unlock()
}

// setErr 记录错误。
func (s *Syncer) setErr(err error) {
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
}

// applyLocalTLS 落盘云端下发的本机证书（agent-tray-access 需求 4.5）。
func (s *Syncer) applyLocalTLS(b *proto.LocalTLSBundle) {
	if b == nil || s.opts.LocalTLS == nil {
		return
	}
	if err := s.opts.LocalTLS.Save(b); err != nil {
		s.opts.Logf("落盘本机证书失败（序列号 %s）: %v", b.Serial, err)
		return
	}
	s.opts.Logf("本机证书已更新：%s，序列号 %s，到期 %s", b.Domain, b.Serial, b.NotAfter.Format(time.RFC3339))
	if s.opts.OnLocalTLS != nil {
		s.opts.OnLocalTLS()
	}
}

// logSyncFailure 记一条同步失败日志（需求 5.4）：原因与当前 revision，时间由日志出口加。
//
// 同一原因连续出现只记第一次，避免离线时每次退避重试都刷一行；
// 原因变化或恢复后再次失败会重新记录。
func (s *Syncer) logSyncFailure(err error) {
	s.mu.Lock()
	rev := s.syncedRev
	msg := err.Error()
	repeat := msg == s.lastLoggedErr
	s.lastLoggedErr = msg
	s.mu.Unlock()
	if repeat {
		return
	}
	if rev == "" {
		rev = "（尚未同步）"
	}
	s.opts.Logf("同步失败: %s（revision %s）", msg, rev)
}

// setMaintenance 记录维护状态。
func (s *Syncer) setMaintenance(on bool) {
	s.mu.Lock()
	s.maintenance = on
	s.mu.Unlock()
}

// deliverAlerts 把云端告警转成本机通知（需求 12.1 第二条通道）。
func (s *Syncer) deliverAlerts(alerts []proto.Alert) {
	if s.opts.Notifier == nil {
		return
	}
	for _, a := range alerts {
		body := a.Detail
		if a.Count > 1 {
			body = fmt.Sprintf("%s（共 %d 次）", body, a.Count)
		}
		if err := s.opts.Notifier.Notify(a.Title, body); err != nil {
			s.opts.Logf("发送通知失败: %v", err)
		}
	}
}

// notifyVersionMismatch 提示更新（需求 19.5）。
func (s *Syncer) notifyVersionMismatch() {
	s.mu.Lock()
	need := s.requiredVer
	s.mu.Unlock()
	if s.opts.Notifier == nil {
		return
	}
	body := fmt.Sprintf("需要更新到版本 %s，下载：%s", need, s.client.DownloadURL())
	if err := s.opts.Notifier.Notify("Dec 代理需要更新", body); err != nil {
		s.opts.Logf("发送通知失败: %v", err)
	}
}

// notifyIdentityProblem 提示身份问题。
func (s *Syncer) notifyIdentityProblem(err error) {
	if s.opts.Notifier == nil {
		return
	}
	title := "Dec 代理未登记"
	body := "请打开本机页面完成设备登记。"
	if errors.Is(err, ErrIdentityRejected) {
		title = "Dec 设备身份被拒绝"
		body = "设备可能已被吊销或证书已过期，请在服务端确认后重新登记。"
	}
	if e := s.opts.Notifier.Notify(title, body); e != nil {
		s.opts.Logf("发送通知失败: %v", e)
	}
}

// maybeNotifyDisconnect 断连超过阈值时发通知（需求 12.3）。
func (s *Syncer) maybeNotifyDisconnect() {
	if s.opts.Notifier == nil {
		return
	}
	s.mu.Lock()
	last := s.lastOK
	s.mu.Unlock()
	if last.IsZero() {
		return
	}
	if time.Since(last) < s.opts.DisconnectAlert {
		return
	}
	if err := s.opts.Notifier.Notify("Dec 代理已断连",
		fmt.Sprintf("已超过 %s 无法连接服务端，已落地文件保持不变。", s.opts.DisconnectAlert)); err != nil {
		s.opts.Logf("发送通知失败: %v", err)
	}
}

// sleepCtx 睡眠，ctx 结束则提前返回 false。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
