package cloudsvc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 连接、对账与回执。

// mHello 处理握手。
//
// 永不破坏兼容（需求 19.7）：即使版本不对齐、甚至没有有效证书，
// 也必须能返回“服务端要求的版本”。这是旧代理唯一的自救通道。
func (s *Server) mHello(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.HelloRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "hello 载荷类型错误")
	}
	aligned := s.aligned(in.AgentVersion)

	// 有身份时顺便更新“最近上报版本”，让管理面看到版本分布。
	// 这里不落盘：FlushSeen 由后台定期调用，避免每个 hello 都产生一次 commit。
	if dev.ID != "" {
		s.reg.Seen(dev.ID, in.AgentVersion)
		s.notify.MarkDevice(dev.ID, time.Now())
	}

	return okPayload(req.ID, &proto.HelloResponse{
		ServerVersion:   s.cfg.ServerVersion,
		RequiredVersion: s.cfg.RequiredVersion,
		ProtocolVersion: proto.Version,
		Maintenance:     s.maintenance.Load(),
		Aligned:         aligned,
		DeviceID:        dev.ID,
	})
}

// mSync 处理对账（需求 4.2、4.3、5.2、6.2）。
//
// 长轮询在这里实现：没有差异且请求带 WaitSeconds 时挂起，直到内容变化或超时。
// 这样“提交后立即推送”在单向请求/响应的传输上也能成立。
func (s *Server) mSync(r *http.Request, req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.SyncRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "sync 载荷类型错误")
	}

	resp, err := s.render.Sync(dev, *in)
	if err != nil {
		return failFromError(req.ID, err)
	}
	resp.Alerts = s.pendingAlerts(dev.ID)
	resp.LocalTLS = s.localTLSFor(in.LocalTLSSerial)
	hasWork := resp.Batch != nil && (len(resp.Batch.Files) > 0 || len(resp.Batch.Deletes) > 0)
	if hasWork || resp.LocalTLS != nil {
		s.notify.MarkDevice(dev.ID, time.Now())
		if hasWork {
			s.recordSync(dev, *in, resp)
		}
		s.recordLocalTLS(dev, resp.LocalTLS)
		return okPayload(req.ID, resp)
	}

	// 没有差异：如果有等待预算，挂起等变更。
	wait := waitBudget(in.WaitSeconds)
	if wait == 0 {
		s.notify.MarkDevice(dev.ID, time.Now())
		return okPayload(req.ID, resp)
	}

	waited := make(chan bool, 1)
	go func() { waited <- s.notify.Wait(wait) }()

	select {
	case <-waited:
		// 等到变化或超时，都在下面重算一次。
	case <-r.Context().Done():
		// 代理断开：返回空响应，让它自己重连。
		return okPayload(req.ID, &proto.SyncResponse{Accepted: true})
	}

	s.notify.MarkDevice(dev.ID, time.Now())
	// 被唤醒后重算一次。可能仍然没有差异（别人触发的变化与本设备无关），
	// 那就返回空 batch，让代理继续下一次长轮询。
	again, err := s.render.Sync(dev, proto.SyncRequest{
		Revision: in.Revision,
		Manifest: in.Manifest,
		Projects: in.Projects,
	})
	if err != nil {
		return failFromError(req.ID, err)
	}
	again.Alerts = s.pendingAlerts(dev.ID)
	again.LocalTLS = s.localTLSFor(in.LocalTLSSerial)
	if again.Batch != nil && (len(again.Batch.Files) > 0 || len(again.Batch.Deletes) > 0) {
		s.recordSync(dev, *in, again)
	}
	s.recordLocalTLS(dev, again.LocalTLS)
	return okPayload(req.ID, again)
}

// pendingAlerts 取走该设备尚未看到的告警。
func (s *Server) pendingAlerts(deviceID string) []proto.Alert {
	if s.alerts == nil {
		return nil
	}
	items := s.alerts.Pending(deviceID)
	if len(items) == 0 {
		return nil
	}
	out := make([]proto.Alert, 0, len(items))
	for _, a := range items {
		out = append(out, proto.Alert{
			ID:     a.ID,
			Kind:   string(a.Kind),
			Title:  a.Title,
			Detail: a.Detail,
			Count:  a.Count,
			At:     a.At,
		})
	}
	return out
}

// recordSync 记一条下发审计（需求 15.3 明确要求覆盖下发）。
//
// 只记数量与 revision，不记文件内容：审计要回答“推了什么给哪台机器”，
// 内容本身已经在 SSOT 的那个 commit 里了。
//
// 同一台设备、同一组内容在短时间内反复下发时只记第一条。幂等重发本身是
// 正常行为（长轮询超时、代理重启都会触发），但它和"对账 bug 导致无限重写"
// 在日志里长得一模一样——2026-10 那次故障正是被 8.4 万条这样的记录淹掉的，
// 真正有用的信息（每小时多出几千条）反而没人看见。降噪之后，同一签名
// 只在达到时间间隔时再记一条，量级差异一眼可见。
func (s *Server) recordSync(dev ssot.Device, in proto.SyncRequest, resp *proto.SyncResponse) {
	detail := fmt.Sprintf("下发 %d 个文件、删除 %d 个", len(resp.Batch.Files), len(resp.Batch.Deletes))
	subject := resp.Batch.Revision

	sig := syncSignature(dev.ID, resp.Batch)
	if s.suppressSync(sig) {
		// 被降噪：仍然要把密钥下发记下来，那是安全追溯用的，不能省。
		s.recordSecretDelivery(dev, resp)
		return
	}

	if err := s.audit.Append(audit.Entry{
		Event:    audit.EventSync,
		DeviceID: dev.ID,
		Source:   string(proto.SourceAgent),
		Subject:  subject,
		Detail:   detail,
		OK:       true,
	}); err != nil {
		s.logf("写审计失败: %v", err)
	}
	s.recordSecretDelivery(dev, resp)
}

// recordSecretDelivery 记一条密钥随下发离开服务端（需求 18.11）。
//
// 这条不参与降噪：吊销设备后要能回答"哪些内容曾经送出服务端"，
// 少记一条就可能漏掉一次密钥外流。
func (s *Server) recordSecretDelivery(dev ssot.Device, resp *proto.SyncResponse) {
	if len(resp.Batch.Files) == 0 || !hasSecretFiles(resp.Batch.Files) {
		return
	}
	if err := s.audit.Append(audit.Entry{
		Event:    audit.EventSecretDeliver,
		DeviceID: dev.ID,
		Source:   string(proto.SourceAgent),
		Subject:  resp.Batch.Revision,
		Detail:   "下发内容包含密钥",
		OK:       true,
	}); err != nil {
		s.logf("写审计失败: %v", err)
	}
}

// syncSignature 是"这次下发的内容"的指纹。
//
// 用文件 ID 与内容哈希拼成，不参与降噪的字段（revision）刻意排除：
// 幂等重发的 revision 可能因为别的设备写入而变，但它推的内容完全相同，
// 在审计里属于同一条记录。
func syncSignature(deviceID string, b *proto.Batch) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", deviceID)
	for _, f := range b.Files {
		fmt.Fprintf(h, "F %s %x %s\n", f.ID, sha256.Sum256([]byte(f.Content)), f.Key)
	}
	for _, f := range b.Deletes {
		fmt.Fprintf(h, "D %s\n", f.ID)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// syncQuiet 是同一签名重复下发的最小记录间隔。
//
// 30 分钟：正常使用下（改资产、装新设备）不会在半小时内重复推同一批内容，
// 所以这个间隔不会掩盖真实操作；而故障级重写（每秒一轮）会在日志里留下
// 每小时两条的痕迹——足以看出问题，又不至于把日志刷爆。
const syncQuiet = 30 * time.Minute

// suppressSync 报告这次下发是否应当跳过审计。
func (s *Server) suppressSync(sig string) bool {
	now := time.Now()
	s.syncLogMu.Lock()
	defer s.syncLogMu.Unlock()
	if s.syncLogged == nil {
		s.syncLogged = map[string]time.Time{}
	}
	// 顺手清理过期条目：设备数量很少，但服务可能连续运行几个月，
	// 不清会随内容变化无限增长。
	if len(s.syncLogged) > 512 {
		for k, at := range s.syncLogged {
			if now.Sub(at) > syncQuiet*4 {
				delete(s.syncLogged, k)
			}
		}
	}
	if last, ok := s.syncLogged[sig]; ok && now.Sub(last) < syncQuiet {
		return true
	}
	s.syncLogged[sig] = now
	return false
}

// hasSecretFiles 报告下发里是否含密钥。
func hasSecretFiles(files []proto.File) bool {
	for _, f := range files {
		if f.Secret {
			return true
		}
	}
	return false
}

// mAck 处理落地回执（需求 4.4、6.2）。
func (s *Server) mAck(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.AckRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "ack 载荷类型错误")
	}
	if err := s.render.States().RecordAck(dev.ID, in.Revision, in.Results); err != nil {
		return failFromError(req.ID, err)
	}
	// 代理上报的覆盖记录：这里只落库，不覆盖本地内容（需求 6.1 明确不做双向同步）。
	if len(in.Overrides) > 0 {
		if err := s.render.States().AddOverrides(dev.ID, in.Overrides); err != nil {
			return failFromError(req.ID, err)
		}
	}
	s.notify.MarkDevice(dev.ID, time.Now())
	return okPayload(req.ID, &proto.AckResponse{Revision: s.render.CurrentRevision()})
}

// mStatus 返回服务状态快照，管理面首屏用它（需求 14.1）。
func (s *Server) mStatus(req *proto.Request, dev ssot.Device) *proto.Response {
	// 状态里含内部信息，只给用户本人（管理面 / CLI）。
	if !identityCanSeeStatus(req.Source) {
		return proto.Fail(req.ID, proto.ErrForbidden, "该来源不能查看服务状态")
	}
	st := s.boot.State()

	out := &proto.StatusResponse{
		ServerVersion:   s.cfg.ServerVersion,
		RequiredVersion: s.cfg.RequiredVersion,
		ProtocolVersion: proto.Version,
		Maintenance:     s.maintenance.Load(),
		BootstrapOpen:   st.BootstrapOpen,
		RecoveryMode:    st.RecoveryMode,
		Revision:        s.render.CurrentRevision(),
		RootKeyID:       st.KeyID,
	}
	s.backupMu.RLock()
	bt, ok := s.backupCheckedAt["backup"]
	if ok {
		v := bt
		out.BackupCheckedAt = &v
	}
	s.backupMu.RUnlock()
	return okPayload(req.ID, out)
}

// identityCanSeeStatus 限定状态查询来源。
func identityCanSeeStatus(src proto.Source) bool {
	switch src {
	case proto.SourceAdmin, proto.SourceHostCLI, proto.SourceAgent:
		return true
	}
	return false
}
