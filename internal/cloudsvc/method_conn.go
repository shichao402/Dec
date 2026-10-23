package cloudsvc

import (
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
func (s *Server) recordSync(dev ssot.Device, in proto.SyncRequest, resp *proto.SyncResponse) {
	detail := fmt.Sprintf("下发 %d 个文件、删除 %d 个", len(resp.Batch.Files), len(resp.Batch.Deletes))
	subject := resp.Batch.Revision
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
	// 下发把密钥送出了服务端，单独记一条便于吊销后追溯（需求 18.11）。
	if len(resp.Batch.Files) > 0 && hasSecretFiles(resp.Batch.Files) {
		if err := s.audit.Append(audit.Entry{
			Event:    audit.EventSecretDeliver,
			DeviceID: dev.ID,
			Source:   string(proto.SourceAgent),
			Subject:  subject,
			Detail:   "下发内容包含密钥",
			OK:       true,
		}); err != nil {
			s.logf("写审计失败: %v", err)
		}
	}
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
