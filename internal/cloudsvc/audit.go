package cloudsvc

import (
	"net/http"

	"github.com/shichao402/Dec/internal/cloud/alert"
	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 审计与冷备的统一埋点（需求 15.3、18.11、22.3）。
//
// 做成集中一处的辅助而不是散落在各方法里，原因有两个：
//
//  1. 审计条目必须一致地带设备 ID 与来源入口，分散写很容易漏。
//  2. “写完就触发推送”是个固定搭配，漏掉一次就少一份冷备。

// record 记一条审计。失败只写日志，不影响业务结果。
//
// 这是有意的取舍：审计写不进去是严重问题，但它不应该让用户的写入操作失败——
// 那会造成“数据没存上，但你以为存上了”的更坏结果。审计失败通过日志暴露。
func (s *Server) record(r *http.Request, event audit.Event, dev ssot.Device, req *proto.Request, subject, detail string, ok bool) {
	if s.audit == nil {
		return
	}
	entry := audit.Entry{
		Event:    event,
		DeviceID: dev.ID,
		Subject:  subject,
		Detail:   detail,
		OK:       ok,
	}
	if req != nil {
		entry.Source = string(req.Source)
	}
	if r != nil {
		entry.RemoteIP = clientIP(r)
	}
	if err := s.audit.Append(entry); err != nil {
		s.logf("写审计失败: %v", err)
	}
}

// recordRevision 记一条带业务 revision 的审计，并触发冷备推送。
//
// 只有真正改了内容才调用：读操作不需要推冷备（审计仓库自己也有一份推送节奏）。
func (s *Server) recordRevision(r *http.Request, event audit.Event, dev ssot.Device, req *proto.Request, subject, detail, revision string, ok bool) {
	if s.audit != nil {
		entry := audit.Entry{
			Event:    event,
			DeviceID: dev.ID,
			Subject:  subject,
			Detail:   detail,
			OK:       ok,
		}
		if req != nil {
			entry.Source = string(req.Source)
		}
		if r != nil {
			entry.RemoteIP = clientIP(r)
		}
		if err := s.audit.AppendCommit(entry, revision); err != nil {
			s.logf("写审计失败: %v", err)
		}
	}
	if ok {
		// 内容变了就推冷备。非阻塞：写入路径不能被网络拖慢（需求 15.1）。
		s.triggerBackup()
	}
}

// triggerBackup 通知冷备有新内容。
func (s *Server) triggerBackup() {
	if s.backup != nil {
		s.backup.Trigger()
	}
}

// raise 发一条告警（需求 12.2）。
func (s *Server) raise(kind alert.Kind, title, detail string) {
	if s.alerts == nil {
		return
	}
	s.alerts.Raise(kind, title, detail)
}
